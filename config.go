package main

import (
	"github.com/BurntSushi/toml"
	"flag"
	"log"
)

type Cfg struct {
	Directory   string 	`toml:"directory"`
	Port        int 	`toml:"port"`
	AWSProfile  string	`toml:"profile"` 
	Bucket      string	`toml:"bucket"`
	Region      string	`toml:"region"`
	Regexp      string	`toml:"regexp"`
	ExpireDays 	int 	`toml:"expire_days"`
}

func loadConfig() *Cfg {
	var configFile string 
	flag.StringVar(&configFile, "config", "config.toml", "TOML Config file to pass to clauganxo")
	flag.Parse()

	conf := new(Cfg)
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

	return conf
}