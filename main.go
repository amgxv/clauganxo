package main

import (
	"context"
	"log"
	"io"
	"io/ioutil"
	"os"
	"flag"
	"fmt"
    "net/http"
	"path/filepath"
	"time"

    "github.com/gorilla/mux"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"	
)

type Configuration struct {
	Directory string
	Port int
	AWSProfile string
	Bucket string
	Region string
}

type BucketBasics struct {
	S3Client *s3.Client
}

type Downloader struct {
	Cfg Configuration
	Bucket BucketBasics
}

var (
	totalCached = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "clauganxo",
		Name: "total_cached",
		Help: "The total number of cached objects",
	})
	servedCache = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "clauganxo",
		Name: "served_cache",
		Help: "Total files served from cache",
	})
	missCache = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "clauganxo",
		Name: "missed_cache",
		Help: "Total files missed from cache",
	})
	failedRequests = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "clauganxo",
		Name: "failed_requests",
		Help: "Requests that failed to serve",
	})
	cacheResponseTime = promauto.NewSummary(prometheus.SummaryOpts{
		Namespace: "clauganxo",
		Name:       "cache_response_time",
		Help:       "Duration of the login request.",
		Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
	})
)


func checkPath(filePath string) bool {
	_, err := os.Stat(filePath)
	if err != nil {
		return false
	}
	return true
}

func checkAndCreateDir(filePath string, file bool) string {
	
	var path string
	path = filePath

	if file {
		path = filepath.Dir(filePath)
	} 

	// log.Println("Checking if path %s exists", path)
	if !checkPath(path){
		log.Printf("Path %s doesn't exist, creating...", path)
		err := os.MkdirAll(path, 0755)
		if err != nil {
			log.Fatal(err)
		}		
	}
	return path
}

func serveImage(filePath string, w http.ResponseWriter){
	buf, err := ioutil.ReadFile(filePath)
	if err != nil {
		log.Fatal(err)
	}
	w.Header().Set("Content-Type", "image/png")
	w.Write(buf)
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

func (d Downloader) GetAndCache(w http.ResponseWriter, r *http.Request){

	now := time.Now()
	vars := mux.Vars(r)
	object, ok := vars["object"]
	if !ok {
		log.Fatal("Could not process object, request might be wrong??")
	}

	public_object := fmt.Sprintf("%s", object)
	local_object := fmt.Sprintf("%s/%s", d.Cfg.Directory, object)

	if checkPath(local_object){
		log.Printf("Served : %s", local_object)
		serveImage(local_object, w)
	} else {
		checkAndCreateDir(local_object, true)
		log.Printf("File %s not detected at local cache", local_object)
		missCache.Inc()
		dload := d.Bucket.DownloadFile(
			d.Cfg.Bucket,
			public_object,
			local_object,
		)
		if dload != nil {
			w.WriteHeader(http.StatusNotFound)
			failedRequests.Inc()
		} else {
			log.Printf("Downloaded and cached %s", local_object)
			totalCached.Inc()
			serveImage(local_object, w)
		}
	}
	cacheResponseTime.Observe(time.Since(now).Seconds())
}

func main() {

	conf := new(Configuration)
	flag.StringVar(&conf.Directory, "dir", "cache", "Dir where things are cached")
	flag.IntVar(&conf.Port, "p", 8000, "Port where HTTP port will listen")
	flag.StringVar(&conf.AWSProfile, "profile", "default", "AWS Profile to be used")
	flag.StringVar(&conf.Bucket, "bucket", "", "Bucket to cache")
	flag.StringVar(&conf.Region, "region", "eu-west-1", "Default AWS Region")
	flag.Parse()

	// Init message
	log.Printf("Starting clauganxo :)")
	log.Printf("------------")
	log.Printf("Local cache path -> %s", conf.Directory)
	log.Printf("Listening on port -> %d", conf.Port)
	log.Printf("AWS Profile -> %s", conf.AWSProfile)
	log.Printf("AWS Bucket to be cached -> %s", conf.Bucket)
	log.Printf("Configured AWS Region -> %s", conf.Region)
	log.Printf("------------")

	// Create local cache directory
	checkAndCreateDir(conf.Directory, false)

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
	r.Handle("/metrics", promhttp.Handler())
	http.ListenAndServe(fmt.Sprintf(":%d", conf.Port), r)
}