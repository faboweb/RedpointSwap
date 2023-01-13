package config

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
	"github.com/imdario/mergo"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var Logger *zap.Logger //Global logger
var Conf Config        //Global config

type Config struct {
	Authz  authz
	JWT    jwt
	Zenith zenith
	Api    api
}

type jwt struct {
	SecretKey string
}

type zenith struct {
	ZenithAuctionUrl string
	ZenithBidUrl     string
	HotWalletKey     string
}

type authz struct {
	MaximumAuthzGrantSeconds float64 //Maximum number of seconds an authz grant is allowed to be valid
}

type api struct {
	ChainID            string
	HotWalletKey       string
	DefiantTrackingApi string //All user and arbitrage trades are POSTed to this HTTP endpoint for invoicing & tracking usage
	LogPath            string
	LogLevel           string
	AllowedCORSDomains string
	Production         bool   //In production mode, client IPs will be tracked and rate limited
	KeyringHomeDir     string //This is just a directory where the keyring-backend will be found, you do not need to run a node
	KeyringBackend     string //Right now this pretty much has to be "test"
	Rpc                string
	Websocket          string //this should be something like rpc.osmosis.zone:443 (no protocol prefix)
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
