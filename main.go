package main

import (
	"fmt"
	"os"
	"sync"

	"github.com/DefiantLabs/RedpointSwap/api/middleware"
	"github.com/DefiantLabs/RedpointSwap/config"
	"github.com/DefiantLabs/RedpointSwap/osmosis"
	"github.com/DefiantLabs/RedpointSwap/zenith"
)

func main() {
	conf := "config.toml"
	if len(os.Args) > 1 {
		conf = os.Args[1]
	}

	var err error
	config.Conf, err = config.GetConfig(conf)
	if err != nil {
		fmt.Println("Error gettting config file. Err: ", err)
		os.Exit(1)
	}

	wg := new(sync.WaitGroup)
	wg.Add(1)

	newBlocks := make(chan int64)
	//Detect when new blocks are produced on the chain
	go osmosis.TrackBlocks(config.Conf.Api.Websocket, newBlocks)
	//Track average time between blocks and notify Zenith when a new block is available
	go osmosis.ProcessNewBlock(newBlocks, []func(int64, int64){zenith.ZenithBlockNotificationHandler})

	//Initialize the REST API for calculating arbitrage opportunities
	go middleware.InitializeRestApi()
	wg.Wait()
}
