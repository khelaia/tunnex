package main

import (
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

var (
	ws           *websocket.Conn
	streams             = map[uint64]net.Conn{}
	lastStreamId uint64 = 0
	wsMu         sync.Mutex
	mu           sync.RWMutex
)

func emitToWebsocket(ws *websocket.Conn, message string) {
	if ws != nil {
		wsMu.Lock()
		defer wsMu.Unlock()
		err := ws.WriteMessage(websocket.TextMessage, []byte(message))
		if err != nil {
			return
		}
	}
}
func main() {
	http.HandleFunc("/start", func(w http.ResponseWriter, r *http.Request) {
		wsInstance, err := upgrader.Upgrade(w, r, nil)

		if err != nil {
			fmt.Println(err)
			return
		}

		wsMu.Lock()
		ws = wsInstance
		wsMu.Unlock()
		go func() {
			defer func(wsInstance *websocket.Conn) {
				wsMu.Lock()
				err := wsInstance.Close()
				if err != nil {
					fmt.Println(err)
				}
				ws = nil
				wsMu.Unlock()

				mu.Lock()
				for id, conn := range streams {
					_ = conn.Close()
					delete(streams, id)
				}
				mu.Unlock()
			}(wsInstance)
			for {
				_, data, err := wsInstance.ReadMessage()
				if err != nil {
					fmt.Println("connection closed:", err)
					return
				}

				message := string(data)

				parts := strings.Split(message, ":")
				if len(parts) != 3 {
					continue
				}
				streamId, streamIdError := strconv.ParseUint(parts[1], 0, 64)

				if streamIdError != nil {
					fmt.Println("Invalid Stream Id:", streamIdError)
					continue
				}

				currentStream := streams[streamId]
				if currentStream == nil {
					fmt.Println("public connection closed")
					continue
				}

				switch {
				case strings.HasPrefix(message, "msg:"):
					decodedMsg, decodeError := hex.DecodeString(parts[2])
					if decodeError != nil {
						fmt.Println("Invalid Hex:", decodeError)
						continue
					}

					_, _ = currentStream.Write(decodedMsg)
				case strings.HasPrefix(message, "close:"):
					if streams[streamId] != nil {
						mu.Lock()
						_ = streams[streamId].Close()
						delete(streams, streamId)
						mu.Unlock()
					}
				}
			}
		}()

		return
	})

	go func() {
		fmt.Println("HTTP server listening on :8888")
		log.Fatal(http.ListenAndServe(":8888", nil))
	}()

	//Network Listener
	nl, err := net.Listen("tcp", ":5000")

	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Network Listening on :5000")

	for {
		netConn, cError := nl.Accept()
		if cError != nil {
			fmt.Println("Error accepting connection:", cError)
			continue
		}

		lastStreamId = lastStreamId + 1
		mu.Lock()
		streams[lastStreamId] = netConn
		mu.Unlock()

		if ws == nil {
			fmt.Println("WS is not active to send traffic")
			_ = netConn.Close()
			continue
		}

		emitToWebsocket(ws, fmt.Sprintf("start:%d", lastStreamId))

		go func(netConn net.Conn, lastStreamId uint64) {
			defer func() {
				mu.Lock()
				_ = netConn.Close()
				delete(streams, lastStreamId)
				mu.Unlock()
			}()
			for {
				buf := make([]byte, 100*1024)
				dataLen, readError := netConn.Read(buf)

				if dataLen > 0 {
					messageHex := fmt.Sprintf("msg:%d:%s", lastStreamId, hex.EncodeToString(buf[0:dataLen]))

					emitToWebsocket(ws, messageHex)
				}

				if readError == io.EOF {
					return
				}
				if readError != nil {
					fmt.Println("Can't read from local network", readError)
					return
				}
			}
		}(netConn, lastStreamId)
	}
}
