package config

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/BurntSushi/toml"
	"github.com/imdario/mergo"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var Logger *zap.Logger //Global logger
var Conf Config        //Global config

type Config struct {
	JWT                jwt
	Zenith             zenith
	Api                api
	ConfigFileLocation string
}

type jwt struct {
	SecretKey string
}

type zenith struct {
	ChainID          string
	ZenithAuctionUrl string
	ZenithBidUrl     string
	HotWalletKey     string
}

type api struct {
	DefiantTrackingApi string //All user and arbitrage trades are POSTed to this HTTP endpoint for invoicing & tracking usage
	LogPath            string
	LogLevel           string
	AllowedCORSDomains string
	Production         bool //In production mode, client IPs will be tracked and rate limited
	ChainID            string
	KeyringHomeDir     string //This is just a directory where the keyring-backend will be found, you do not need to run a node
	KeyringBackend     string //Right now this pretty much has to be "test"
	Rpc                string
	Websocket          string //this should be something like rpc.osmosis.zone:443 (no protocol prefix)
	HotWalletKey       string
}

func DoConfigureLogger(logPath []string, logLevel string) {
	var logErr error
	cfg := zap.Config{
		OutputPaths: logPath, //stdout optional
		EncoderConfig: zapcore.EncoderConfig{
			MessageKey:  "message",
			LevelKey:    "level",
			EncodeLevel: zapcore.LowercaseLevelEncoder,
			EncodeTime:  zapcore.ISO8601TimeEncoder,
		},
		Encoding: "json",
		Level:    zap.NewAtomicLevel(),
	}

	al, logErr := zap.ParseAtomicLevel(logLevel)
	if logErr != nil {
		fmt.Println("logger setup failure. Err: ", logErr)
		os.Exit(1)
	}
	cfg.Level = al
	Logger, logErr = cfg.Build()
	if logErr != nil {
		fmt.Println("logger setup failure. Err: ", logErr)
		os.Exit(1)
	}
}

func GetConfig(configFileLocation string) (Config, error) {
	var conf Config
	_, err := toml.DecodeFile(configFileLocation, &conf)
	return conf, err
}

func MergeConfigs(def Config, overide Config) Config {

	mergo.Merge(&overide, def)

	return overide
}

func ParseArgs(w io.Writer, args []string) (Config, error) {

	c := Config{}
	fs := flag.NewFlagSet("config", flag.ContinueOnError)

	fs.SetOutput(w)
	fs.StringVar(&c.ConfigFileLocation, "config", "", "The file to load for configuration variables")

	err := fs.Parse(args)
	if err != nil {
		return c, err
	}

	return c, nil

}

func InitConfig() (*Config, error) {

	argConfig, err := ParseArgs(os.Stderr, os.Args[1:])

	if err != nil {
		return nil, err
	}

	var location string
	if argConfig.ConfigFileLocation != "" {
		location = argConfig.ConfigFileLocation
	} else {
		location = "./config.toml"
	}

	fileConfig, err := GetConfig(location)

	if err != nil {
		fmt.Println("Error opening configuration file", err)
		return nil, err
	}

	config := MergeConfigs(fileConfig, argConfig)
	return &config, nil
}
