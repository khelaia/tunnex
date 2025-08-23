package main

import (
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

var (
	ws *websocket.Conn
	nc net.Conn
)

func main() {
	http.HandleFunc("/start", func(w http.ResponseWriter, r *http.Request) {
		wsInstance, err := upgrader.Upgrade(w, r, nil)

		if err != nil {
			fmt.Println(err)
			return
		}

		ws = wsInstance

		go func() {
			defer func(wsInstance *websocket.Conn) {
				err := wsInstance.Close()
				if err != nil {
					fmt.Println(err)
				}
				ws = nil
			}(wsInstance)
			for {
				_, data, err := wsInstance.ReadMessage()
				if err != nil {
					fmt.Println("connection closed:", err)
					return
				}

				message := string(data)

				switch {
				case strings.HasPrefix(message, "msg:"):
					parts := strings.Split(message, ":")
					if len(parts) != 2 {
						continue
					}
					decodedMsg, decodeError := hex.DecodeString(parts[1])
					if decodeError != nil {
						fmt.Println("Invalid Hex:", decodeError)
						continue
					}
					if nc == nil {
						fmt.Println("public connection closed")
						continue
					}
					_, _ = nc.Write(decodedMsg)

				case strings.HasPrefix(message, "close"):
					return
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

		nc = netConn

		if ws == nil {
			fmt.Println("WS is not active to send traffic")
			_ = netConn.Close()
			continue
		}

		for {
			buf := make([]byte, 10*1024)
			dataLen, readError := netConn.Read(buf)

			if dataLen > 0 {
				messageHex := fmt.Sprintf("msg:%s", hex.EncodeToString(buf[0:dataLen]))

				err := ws.WriteMessage(websocket.TextMessage, []byte(messageHex))
				if err != nil {
					return
				}
			}

			if readError == io.EOF {
				return
			}
			if readError != nil {
				fmt.Println("Can't read from local network", readError)
				return
			}
		}
	}
}
