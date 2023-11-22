package main

import (
	"flag"
	"github.com/spf13/viper"
	"log"
	"strings"
)

type Config struct {
	Host          string
	Port          string
	Authorization string
	JarPath       string
	ProjectIdList []string
	BaseUrl       string
	Debug         bool
}

var config = Config{}

const defaultConfigName = "config.ini"

var configName string

func init() {
	flag.StringVar(&configName, "c", defaultConfigName, "config file name")
	flag.Parse()
	log.Printf("use config: %s\n", configName)

	viper.SetConfigFile(configName)
	if err := viper.ReadInConfig(); err != nil {
		log.Fatal(err)
	}
	config.Host = viper.GetString("server.Host")
	config.Port = viper.GetString("server.Port")
	config.Authorization = viper.GetString("server.Authorization")
	config.JarPath = viper.GetString("client.JarPath")
	config.ProjectIdList = strings.Split(viper.GetString("client.ProjectId"), ",")
	config.Debug = viper.GetBool("client.Debug")

	config.BaseUrl = "http://" + config.Host + ":" + config.Port + "/api"
	log.Printf("%#v\n", config)
}
