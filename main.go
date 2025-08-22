package main

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

var (
	ws *websocket.Conn
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
					fmt.Println(parts[1])
				case strings.HasPrefix(message, "close"):
					return
				}
			}
		}()

		return
	})

	go func() {
		for {
			time.Sleep(time.Second * 1)
			if ws != nil {
				message := fmt.Sprintf("Server Time: %s", time.Now().String())
				_ = ws.WriteMessage(websocket.TextMessage, []byte(message))
			}
		}
	}()

	fmt.Println("HTTP server listening on :8888")
	log.Fatal(http.ListenAndServe(":8888", nil))
}
