package main

import (
	"net/http"
	"fmt"
	
	"github.com/aws/aws-sdk-go-v2/service/s3"	
	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)


func main() {

	//generate config
	conf := loadConfig()

	// Create local cache directory
	checkAndCreateDir(conf.Directory, false)

	if conf.ExpireDays > 0 {
		// run goroutine every hour
		go RunCheckAndExpire(conf, 1)
	}

	s3_cfg := loadS3config(conf)
	client := BucketBasics{S3Client: s3.NewFromConfig(s3_cfg)}
	handlers := Downloader{Cfg: *conf, Bucket: client}

	//initialize metrics
	countCached(conf.Directory)

	r := mux.NewRouter()
	r.HandleFunc("/c/{object:.*}", handlers.GetAndCache).Methods("GET")
	r.HandleFunc("/flush/{object:.*}", handlers.FlushObject).Methods("DELETE")
	r.Handle("/metrics", promhttp.Handler())
	http.ListenAndServe(fmt.Sprintf(":%d", conf.Port), r)
}
