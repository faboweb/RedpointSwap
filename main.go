package main

import (
	"os"
	"sync"

	"github.com/DefiantLabs/RedpointSwap/api/middleware"
)

func main() {
	conf := "config.toml"
	if len(os.Args) > 1 {
		conf = os.Args[1]
	}

	wg := new(sync.WaitGroup)
	wg.Add(1)

	//Initialize the REST API for calculating arbitrage opportunities
	go middleware.InitializeRestApi(conf)
	wg.Wait()
}
