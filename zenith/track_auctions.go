package zenith

import (
	"sync"
	"time"
)

// Tracks upcoming blocks. Any blocks past the current chain height are removed.
// Each future block will be represented in the Map with Key: int64 block height and Value: *FutureBlock.
var zenithBlocks sync.Map

type FutureBlock struct {
	IsZenithBlock          bool      //whether this block will be submitted by Zenith
	Height                 int64     //the block height of the future block
	ProjectedBlocktime     time.Time //the time we think this block will happen on chain
	MillisecondsUntilBlock int64     //how many milliseconds until we think this block will happen
}

// TODO: notify the admin if the Zenith endpoint stops working
//
// This function is called for every new block produced on the chain.
// We query the Zenith auction endpoint (see https://meka.tech/zenith#get-_v0_auction)
// for the next 10 blocks (which is the max supported by Zenith).
//
// Overall, we are tracking available Zenith blocks using Mekatek's service endpoints.
// This function only tracks what blocks Zenith will produce -- it does not bid on auctions.
func ZenithBlockNotificationHandler(lastChainHeight int64, millisecondsBetweenBlocks int64) {
	// Remove any blocks that already happened
	zenithBlocks.Range(func(key, _ any) bool {
		if key.(int64) <= lastChainHeight {
			zenithBlocks.Delete(key)
		}
		return true
	})

	for height := lastChainHeight; height < lastChainHeight+10; height++ {
		//We have never queried Zenith for the given block
		if _, ok := zenithBlocks.Load(height); !ok {

		}
	}
}
