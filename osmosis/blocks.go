package osmosis

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/gorilla/websocket"
)

type Result struct {
	Data Data
}

type Data struct {
	Value EventDataNewBlockHeader
}

type EventDataNewBlockHeader struct {
	Header Header `json:"header"`
	NumTxs string `json:"num_txs"` // Number of txs in a block
}

type Header struct {
	// basic block info
	ChainID string    `json:"chain_id"`
	Height  string    `json:"height"`
	Time    time.Time `json:"time"`
}

type TendermintNewBlockHeader struct {
	Result Result
}

func AwaitBlocks(wssHost string, height chan int64, exitAfterFails int) {
	fails := 0

	//Open websocket connection to get notified on each new block publish
	subscribeMsg := "{\"jsonrpc\": \"2.0\",\"method\": \"subscribe\",\"id\": 1,\"params\": {\"query\": \"tm.event='NewBlockHeader'\"}}"
	u := url.URL{Scheme: "wss", Host: wssHost, Path: "/websocket"}
	c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		fmt.Println(err.Error())
		fails = fails + 1
		if fails >= exitAfterFails {
			return
		}
	}

	defer c.Close()
	c.WriteMessage(websocket.BinaryMessage, []byte(subscribeMsg))

	for {
		_, message, err := c.ReadMessage()
		if err != nil {
			fmt.Println(err.Error())
			fails = fails + 1
			if fails >= exitAfterFails {
				return
			}
		}

		var bh TendermintNewBlockHeader
		err = json.Unmarshal(message, &bh)
		if err != nil {
			fmt.Println(err.Error())
			fails = fails + 1
			if fails >= exitAfterFails {
				return
			}
		}

		blockHeight, err := strconv.ParseInt(bh.Result.Data.Value.Header.Height, 10, 64)
		if err == nil {
			height <- blockHeight
		}
	}
}
