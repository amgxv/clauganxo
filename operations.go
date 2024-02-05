package main

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"	
	"github.com/gorilla/mux"
)

type BucketBasics struct {
	S3Client *s3.Client
}

type Downloader struct {
	Cfg    Cfg
	Bucket BucketBasics
}

func loadS3config(conf *Cfg) aws.Config {
	// Load AWS config from Profile
	cfg, err := config.LoadDefaultConfig(
		context.TODO(),
		config.WithRegion(conf.Region),
		config.WithSharedConfigProfile(conf.AWSProfile),
	)
	if err != nil {
		log.Fatal(err)
	}	

	return cfg
}

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
	_, err = io.Copy(file, result.Body)
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
		totalCached.Dec()
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
					// fmt.Println(path, current_date, date_file, expected_expire)
					log.Printf("Expiring object -> %s [EXPECTED EXPIRE DATE : %s]", path, expected_expire)
					os.Remove(path)
					totalCached.Dec()
				}
			}
			return nil
		})
	if err != nil {
		log.Println(err)
	}
}

func RunCheckAndExpire(conf *Cfg, t int) {
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

func countCached(dir string) {
	count := 0
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			count++
		}
		return nil
	})

	if err != nil {
		log.Println(err)
	}
	totalCached.Add(float64(count))
}
