package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/imdario/mergo"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var Logger *zap.Logger //Global logger
var Conf Config        //Global config

var HotWalletAddress string
var HotWalletArbBalance sdk.Int

type Config struct {
	Authz  authz
	JWT    jwt
	Zenith zenith
	Api    api
}

type jwt struct {
	SecretKey string
	Issuer    string //Name of the company running this app, e.g. Defiant Labs
}

type zenith struct {
	ZenithAuctionUrl string
	ZenithBidUrl     string
	MaximumBidAmount string  //Any valid Coin. Denom MUST match the zenith bid denom. This will cap the BidPercentage (see below).
	BidPercentage    float64 //Float percentage of the arb profits that will be bid. Example: if arb profits are estimated as 10 OSMO, 0.1 will be 1 OSMO
}

type authz struct {
	MaximumAuthzGrantSeconds float64 //Maximum number of seconds an authz grant is allowed to be valid
}

type api struct {
	ChainID                   string
	HotWalletKey              string
	ArbitrageDenom            string //Right now, only uosmo is supported, so you must set this value to uosmo
	ArbitrageDenomMinAmount   int64  //uosmo is 10^6, so 1000 OSMO == 1000000000
	DefiantTrackingApi        string //All user and arbitrage trades are POSTed to this HTTP endpoint for invoicing & tracking usage
	LogPath                   string
	LogLevel                  string
	AllowedCORSDomains        string
	Port                      string //will default to port 80 if this is not set
	Production                bool   //In production mode, client IPs will be tracked and rate limited
	KeyringHomeDir            string //This is just a directory where the keyring-backend will be found, you do not need to run a node
	KeyringBackend            string //Right now this pretty much has to be "test"
	RpcSubmitTxEndpoints      string //Nodes where we can SUBMIT Txs. Only certain nodes allow 0 fee TXs. Comma separated.
	RpcSearchEndpoints        string //Nodes where we can SEARCH Txs. Comma separated.
	WebsocketEndpoints        string //comma separated. this should be something like rpc.osmosis.zone:443 (no protocol prefix)
	UserProfitSharePercentage float64
}

var lastWebsocketEndpointIndex = 0
var lastRpcSubmitEndpointIndex = 0
var lastRpcSsearchEndpointIndex = 0

// GetApiWebsocketEndpoint Round robin get websocket endpoint
func (conf *Config) GetApiWebsocketEndpoint() string {
	eps := strings.Split(conf.Api.WebsocketEndpoints, ",")
	if lastWebsocketEndpointIndex > len(eps)-1 {
		lastWebsocketEndpointIndex = 0
	}
	currentWse := eps[lastWebsocketEndpointIndex]
	lastWebsocketEndpointIndex += 1
	return currentWse
}

// GetApiRpcSubmitTxEndpoint Round robin get rpc endpoint to submit TXs
func (conf *Config) GetApiRpcSubmitTxEndpoint() string {
	eps := strings.Split(conf.Api.RpcSubmitTxEndpoints, ",")
	if lastRpcSubmitEndpointIndex > len(eps)-1 {
		lastRpcSubmitEndpointIndex = 0
	}
	currentRpc := eps[lastRpcSubmitEndpointIndex]
	lastRpcSubmitEndpointIndex += 1
	return currentRpc
}

// GetApiRpcSearchTxEndpoint Round robin get rpc endpoint to search TXs
func (conf *Config) GetApiRpcSearchTxEndpoint() string {
	eps := strings.Split(conf.Api.RpcSubmitTxEndpoints, ",")
	if lastRpcSubmitEndpointIndex > len(eps)-1 {
		lastRpcSubmitEndpointIndex = 0
	}
	currentRpc := eps[lastRpcSubmitEndpointIndex]
	lastRpcSubmitEndpointIndex += 1
	return currentRpc
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
