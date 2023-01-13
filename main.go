package main

import (
	"fmt"
	"os"

	"github.com/DefiantLabs/RedpointSwap/api/middleware"
	"github.com/DefiantLabs/RedpointSwap/config"
	"github.com/DefiantLabs/RedpointSwap/osmosis"
	"github.com/DefiantLabs/RedpointSwap/zenith"
	"go.uber.org/zap"
)

func main() {
	conf := "config.toml"
	if len(os.Args) > 1 {
		conf = os.Args[1]
	}

	var err error
	config.Conf, err = config.GetConfig(conf)
	if err != nil {
		fmt.Println("Error getting config file. Err: ", err)
		os.Exit(1)
	}

	logLevel := config.Conf.Api.LogLevel
	logPath := config.Conf.Api.LogPath
	config.DoConfigureLogger([]string{logPath, "stdout"}, logLevel)

	//It is insecure to configure a SHA256 key with less than a 32 byte secret key
	if len(config.Conf.JWT.SecretKey) < 32 {
		config.Logger.Error("Insecure JWT configuration", zap.Int("Secret key length", len(config.Conf.JWT.SecretKey)))
		os.Exit(1)
	}

	newBlocks := make(chan int64)
	done := make(chan struct{})

	//Detect when new blocks are produced on the chain
	go func() {
		defer close(done)
		osmosis.TrackBlocks(config.Conf.Api.Websocket, newBlocks)
	}()

	//Track average time between blocks and notify Zenith when a new block is available
	go func() {
		defer close(done)
		osmosis.ProcessNewBlock(newBlocks, []func(int64, int64){zenith.ZenithBlockNotificationHandler})
	}()

	//Initialize the REST API for calculating arbitrage opportunities
	go func() {
		defer close(done)
		middleware.InitializeRestApi()
	}()
}
