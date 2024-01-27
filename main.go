package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"
	
	"github.com/BurntSushi/toml"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Configuration struct {
	Directory   string 	`toml:"directory"`
	Port        int 	`toml:"port"`
	AWSProfile  string	`toml:"profile"` 
	Bucket      string	`toml:"bucket"`
	Region      string	`toml:"region"`
	Regexp      string	`toml:"regexp"`
	ExpireDays 	int 	`toml:"expire_days"`
}

type BucketBasics struct {
	S3Client *s3.Client
}

type Downloader struct {
	Cfg    Configuration
	Bucket BucketBasics
}

var (
	totalCached = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "clauganxo",
		Name:      "total_cached",
		Help:      "The total number of cached objects",
	})
	servedCache = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "clauganxo",
		Name:      "served_cache",
		Help:      "Total files served from cache",
	})
	missCache = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "clauganxo",
		Name:      "missed_cache",
		Help:      "Total files missed from cache",
	})
	failedRequests = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "clauganxo",
		Name:      "failed_requests",
		Help:      "Requests that failed to serve",
	})
	cacheResponseTime = promauto.NewSummary(prometheus.SummaryOpts{
		Namespace:  "clauganxo",
		Name:       "cache_response_time",
		Help:       "Duration of the login request.",
		Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
	})
)

func checkPath(filePath string) bool {
	_, err := os.Stat(filePath)
	if err == nil {
		return true
	}
	return false
}

func checkAndCreateDir(filePath string, file bool) string {

	var path string
	path = filePath

	if file {
		path = filepath.Dir(filePath)
	}

	// log.Println("Checking if path %s exists", path)
	if !checkPath(path) {
		log.Printf("Path %s doesn't exist, creating...", path)
		err := os.MkdirAll(path, 0755)
		if err != nil {
			log.Fatal(err)
		}
	}
	return path
}

func checkAllowedObjects(object string, regex string) bool {
	match, _ := regexp.Match(regex, []byte(object))
	return match
}

func getContentType(filePath string) string {
	buf, err := ioutil.ReadFile(filePath)
	if err != nil {
		log.Fatal(err)
	}
	mimetype := http.DetectContentType(buf)

	return mimetype
}

func serveContent(filePath string, w http.ResponseWriter) {
	mimetype := getContentType(filePath)
	buf, err := ioutil.ReadFile(filePath)
	if err != nil {
		log.Fatal(err)
	}
	w.Header().Set("Content-Type", mimetype)
	w.Write(buf)
	log.Printf("[200] - Served %s", filePath)
	servedCache.Inc()
}

// Get object from S3
func (basics BucketBasics) DownloadFile(bucketName string, objectKey string, fileName string) error {
	result, err := basics.S3Client.GetObject(context.TODO(), &s3.GetObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(objectKey),
	})
	if err != nil {
		log.Printf("Couldn't get object %v:%v. -> %v\n", bucketName, objectKey, err)
		return err
	}
	defer result.Body.Close()
	file, err := os.Create(fileName)
	if err != nil {
		log.Printf("Couldn't create file %v. -> %v\n", fileName, err)
		return err
	}
	defer file.Close()
	body, err := io.ReadAll(result.Body)
	if err != nil {
		log.Printf("Couldn't read object body from %v. -> %v\n", objectKey, err)
	}
	_, err = file.Write(body)
	return err
}

func (d Downloader) Serve(bucket_object string, local_object string, w http.ResponseWriter) {
	if checkPath(local_object) {
		serveContent(local_object, w)
	} else {
		checkAndCreateDir(local_object, true)
		log.Printf("File %s not detected at local cache", local_object)
		missCache.Inc()
		dload := d.Bucket.DownloadFile(
			d.Cfg.Bucket,
			bucket_object,
			local_object,
		)
		if dload != nil {
			log.Printf("[404] - File was not found at S3 Bucket")
			w.WriteHeader(http.StatusNotFound)
			failedRequests.Inc()
		} else {
			log.Printf("Downloaded and cached %s", local_object)
			serveContent(local_object, w)
			totalCached.Inc()
		}
	}
}

func (d Downloader) Flush(local_object string, w http.ResponseWriter) {
	if checkPath(local_object) {
		err := os.Remove(local_object)
		if err != nil {
			fmt.Println(err)
		}
		log.Printf("FLUSH - Object Flushed %s", local_object)
		w.WriteHeader(http.StatusOK)
	} else {
		log.Printf("FLUSH - Object not found, ignoring")
		w.WriteHeader(http.StatusNotFound)
	}
}

func (d Downloader) GetAndCache(w http.ResponseWriter, r *http.Request) {

	now := time.Now()
	vars := mux.Vars(r)
	object, ok := vars["object"]
	if !ok {
		log.Fatal("Could not process object, request might be wrong??")
	}

	bucket_object := fmt.Sprintf("%s", object)
	local_object := fmt.Sprintf("%s/%s", d.Cfg.Directory, object)

	if d.Cfg.Regexp != "" {
		switch checkAllowedObjects(bucket_object, d.Cfg.Regexp) {
		case true:
			log.Printf("Regex found for object %s", bucket_object)
			d.Serve(bucket_object, local_object, w)
		case false:
			log.Printf("[404] - Regex FAILED for object %s", bucket_object)
			w.WriteHeader(http.StatusNotFound)
			failedRequests.Inc()
		}
	} else {
		d.Serve(bucket_object, local_object, w)
	}

	cacheResponseTime.Observe(time.Since(now).Seconds())
}

func CheckAndExpire(dir string, expire_days int) {
	err := filepath.Walk(dir,
		func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() {
				current_date := time.Now()
				date_file := info.ModTime()
				days := time.Hour * time.Duration(24*expire_days)
				expected_expire := date_file.Add(days)
				if expected_expire.Before(current_date) {
					fmt.Println(path, current_date, date_file, expected_expire)
				}
			}
			return nil
		})
	if err != nil {
		log.Println(err)
	}
}

func RunCheckAndExpire(conf *Configuration, t int) {
	for range time.Tick(time.Hour * time.Duration(t)) {
		CheckAndExpire(fmt.Sprintf("%s/", conf.Directory), conf.ExpireDays)
	}
}

func (d Downloader) FlushObject(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	object, ok := vars["object"]
	if !ok {
		log.Fatal("Could not process object, request might be wrong??")
	}

	bucket_object := fmt.Sprintf("%s", object)
	local_object := fmt.Sprintf("%s/%s", d.Cfg.Directory, object)

	if d.Cfg.Regexp != "" {
		switch checkAllowedObjects(bucket_object, d.Cfg.Regexp) {
		case true:
			log.Printf("FLUSH - Regex found for object %s", bucket_object)
			d.Flush(local_object, w)
		case false:
			log.Printf("FLUSH - [404] - Regex FAILED for object %s", bucket_object)
			w.WriteHeader(http.StatusNotFound)
		}
	} else {
		d.Flush(local_object, w)
	}
}

func main() {

	// Read TOML file for config
	var configFile string 
	flag.StringVar(&configFile, "config", "config.toml", "TOML Config file to pass to clauganxo")
	flag.Parse()

	conf := new(Configuration)
	_, err := toml.DecodeFile(configFile, &conf)
	if err != nil {
		log.Printf("Config file %s not detected", configFile)
		panic(err)
	}

	// Init message
	log.Printf("Starting clauganxo :)")
	log.Printf("------------")
	log.Printf("Local cache path -> %s", conf.Directory)
	log.Printf("Listening on port -> %d", conf.Port)
	log.Printf("AWS Profile -> %s", conf.AWSProfile)
	log.Printf("AWS Bucket to be cached -> %s", conf.Bucket)
	log.Printf("Configured AWS Region -> %s", conf.Region)
	if conf.Regexp != "" {
		log.Printf("Regex -> %s", conf.Regexp)
	}
	if conf.ExpireDays > 0 {
		log.Printf("Days to expire files -> %d", conf.ExpireDays)
	}
	log.Printf("------------")

	// Create local cache directory
	checkAndCreateDir(conf.Directory, false)

	if conf.ExpireDays > 0 {
		// run goroutine every hour
		go RunCheckAndExpire(conf, 1)
	}

	// Load AWS config from Profile
	cfg, err := config.LoadDefaultConfig(
		context.TODO(),
		config.WithRegion(conf.Region),
		config.WithSharedConfigProfile(conf.AWSProfile),
	)
	if err != nil {
		log.Fatal(err)
	}

	client := BucketBasics{S3Client: s3.NewFromConfig(cfg)}
	handlers := Downloader{Cfg: *conf, Bucket: client}

	r := mux.NewRouter()
	r.HandleFunc("/c/{object:.*}", handlers.GetAndCache).Methods("GET")
	r.HandleFunc("/flush/{object:.*}", handlers.FlushObject).Methods("DELETE")
	r.Handle("/metrics", promhttp.Handler())
	http.ListenAndServe(fmt.Sprintf(":%d", conf.Port), r)
}
