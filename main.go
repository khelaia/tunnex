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
	"sync/atomic"

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
	wsMu.Lock()
	defer wsMu.Unlock()
	if ws != nil {
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
		if ws != nil {
			_ = ws.Close()
		}
		ws = wsInstance
		wsMu.Unlock()

		go func() {
			defer func() {

				mu.Lock()
				for id, conn := range streams {
					_ = conn.Close()
					delete(streams, id)
				}
				mu.Unlock()

				wsMu.Lock()
				if ws == wsInstance {
					ws = nil
				}
				wsMu.Unlock()

				err := wsInstance.Close()
				if err != nil {
					fmt.Println(err)
				}
			}()
			for {
				_, data, err := wsInstance.ReadMessage()
				if err != nil {
					fmt.Println("connection closed:", err)
					return
				}

				message := string(data)

				parts := strings.Split(message, ":")
				if len(parts) < 2 {
					continue
				}
				streamId, streamIdError := strconv.ParseUint(parts[1], 0, 64)

				if streamIdError != nil {
					fmt.Println("Invalid Stream Id:", streamIdError)
					continue
				}

				mu.RLock()
				currentStream := streams[streamId]
				mu.RUnlock()
				if currentStream == nil {
					fmt.Println("public connection closed")
					continue
				}

				messageType := parts[0]

				switch {
				case strings.EqualFold(messageType, "msg"):
					if len(parts) != 3 {
						continue
					}
					decodedMsg, decodeError := hex.DecodeString(parts[2])
					if decodeError != nil {
						fmt.Println("Invalid Hex:", decodeError)
						continue
					}

					_, writeError := currentStream.Write(decodedMsg)
					if writeError != nil {
						fmt.Println("Write Error:", writeError)
					}
				case strings.EqualFold(messageType, "close"):
					mu.Lock()
					if currentStream != nil {
						_ = currentStream.Close()
						delete(streams, streamId)
					}
					mu.Unlock()

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

		wsMu.Lock()
		currentWs := ws
		wsMu.Unlock()
		if currentWs == nil {
			fmt.Println("WS is not active to send traffic")
			_ = netConn.Close()
			continue
		}

		id := atomic.AddUint64(&lastStreamId, 1)
		mu.Lock()
		streams[id] = netConn
		mu.Unlock()

		emitToWebsocket(ws, fmt.Sprintf("start:%d", id))

		go func(netConn net.Conn, id uint64) {
			defer func() {
				_ = netConn.Close()

				mu.Lock()
				if streams[id] == netConn {
					delete(streams, id)
				}
				mu.Unlock()
				emitToWebsocket(ws, fmt.Sprintf("close:%d", id))
			}()
			buf := make([]byte, 100*1024)

			for {
				dataLen, readError := netConn.Read(buf)

				if dataLen > 0 {
					messageHex := fmt.Sprintf("msg:%d:%s", id, hex.EncodeToString(buf[0:dataLen]))

					emitToWebsocket(ws, messageHex)
				} else {
					return
				}

				if readError == io.EOF {
					return
				}
				if readError != nil {
					fmt.Println("Can't read from local network", readError)
					return
				}
			}
		}(netConn, id)
	}
}
