package osmosis

import (
	"os"
	"time"

	"github.com/DefiantLabs/RedpointSwap/config"
	"go.uber.org/zap"
)

// TODO: use more than once RPC endpoint, notify the admin if too many endpoints stop working
//
// Tracks what block the chain is currently on and how much time passes between each block (rolling average)
// websocketEndpoint should be a domain and port with no protocol, e.g. rpc.osmosis.zone:443
func TrackBlocks(websocketEndpoint string, newBlocks chan int64) {
	fails := 0
	for {
		AwaitBlocks(websocketEndpoint, newBlocks, 10)
		fails = fails + 1
		if fails == 10 {
			config.Logger.Error("RPC host failure (get blocks)", zap.String("host", websocketEndpoint))
			break
		}
	}

	os.Exit(1)
}

var averageBlockTime int64 = 0 //average number of milliseconds between each block

func ProcessNewBlock(height chan int64, subscribers []func(height int64, avgTimeBetweenBlocks int64)) {
	trackedBlockTimes := []int64{} //number of milliseconds between blocks

	// when the app starts, just get the newest block so we can start the rolling timer
	lastHeight := <-height
	lastBlockStart := time.Now()

	for {
		newHeight := <-height

		//The last two blocks are within 1 block of each other, so can be used towards the moving average
		if newHeight > lastHeight && newHeight-lastHeight == 1 {
			millisecondsBetween := time.Since(lastBlockStart).Milliseconds()
			trackedBlockTimes = append(trackedBlockTimes, millisecondsBetween)

			if len(trackedBlockTimes) > 10 {
				trackedBlockTimes = trackedBlockTimes[1:]
			}
		}

		//Calculate the moving average time between blocks
		var total int64
		for _, t := range trackedBlockTimes {
			total += t
		}
		averageBlockTime = total / int64(len(trackedBlockTimes))

		lastHeight = newHeight
		lastBlockStart = time.Now()

		go func() {
			//Notify subscribers about the new block
			for _, subscriber := range subscribers {
				subscriber(lastHeight, averageBlockTime)
			}
		}()
	}
}
