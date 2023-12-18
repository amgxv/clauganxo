package main

import (
	"context"
	"log"
	"io"
	"os"
	"flag"
	"fmt"
    "net/http"

    "github.com/gorilla/mux"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
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

// DownloadFile gets an object from a bucket and stores it in a local file.
func (basics BucketBasics) DownloadFile(bucketName string, objectKey string, fileName string) error {
	result, err := basics.S3Client.GetObject(context.TODO(), &s3.GetObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(objectKey),
	})
	if err != nil {
		log.Printf("Couldn't get object %v:%v. Here's why: %v\n", bucketName, objectKey, err)
		return err
	}
	defer result.Body.Close()
	file, err := os.Create(fileName)
	if err != nil {
		log.Printf("Couldn't create file %v. Here's why: %v\n", fileName, err)
		return err
	}
	defer file.Close()
	body, err := io.ReadAll(result.Body)
	if err != nil {
		log.Printf("Couldn't read object body from %v. Here's why: %v\n", objectKey, err)
	}
	_, err = file.Write(body)
	return err
}

func hello(w http.ResponseWriter, r *http.Request){
	vars := mux.Vars(r)
	object, ok := vars["object"]
	if !ok {
		log.Fatal("object missing")
	}

	fmt.Fprintf(w, "holi %s", object)
}

func ReturnLocalObject(object string){
	
}

func GetObject(w http.ResponseWriter, r *http.Request){
	vars := mux.Vars(r)
	object, ok := vars["object"]
	if !ok {
		log.Fatal("object missing")
	}

	fmt.Fprintf(w, "holi %s", object)
}

func main() {

	conf := new(Configuration)
	flag.StringVar(&conf.Directory, "dir", "cache", "Dir where things are cached")
	flag.IntVar(&conf.Port, "p", 8000, "Port where HTTP port will listen")
	flag.StringVar(&conf.AWSProfile, "profile", "default", "AWS Profile to be used")
	flag.StringVar(&conf.Bucket, "bucket", "", "Bucket to cache")
	flag.StringVar(&conf.Region, "region", "eu-west-1", "Default AWS Region")
	flag.Parse()

	fmt.Println(conf)

	// Load the Shared AWS Configuration (~/.aws/config)
	cfg, err := config.LoadDefaultConfig(
		context.TODO(),
		config.WithRegion(conf.Region),
		config.WithSharedConfigProfile(conf.AWSProfile),
	)
	if err != nil {
		log.Fatal(err)
	}

    r := mux.NewRouter()
	r.HandleFunc("/c/{object}", hello)
	http.ListenAndServe(fmt.Sprintf(":%d", conf.Port), r)


	// Create an Amazon S3 service client
	client := BucketBasics{S3Client: s3.NewFromConfig(cfg)}

	client.DownloadFile(
		conf.Bucket,
		"public_backup/profile-64f9ddc4a6ba925e9afd7148-yl2qMt8kAK42kAYkkUB7x",
		"profile-64f9ddc4a6ba925e9afd7148-yl2qMt8kAK42kAYkkUB7x",
	)
	
	// // Get the first page of results for ListObjectsV2 for a bucket
	// output, err := client.ListObjectsV2(context.TODO(), &s3.ListObjectsV2Input{
	// 	Bucket: aws.String("nooubox-s3-amplify152159-staging"),
	// })
	// if err != nil {
	// 	log.Fatal(err)
	// }

	// log.Println("first page results:")
	// for _, object := range output.Contents {
	// 	log.Printf("key=%s size=%d", aws.ToString(object.Key), object.Size)
	// }
}
