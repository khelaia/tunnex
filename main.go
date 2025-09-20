package main

import (
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"tunnex/utils"

	"github.com/gorilla/websocket"
)

type Client struct {
	host         string
	ws           *websocket.Conn
	wsMux        sync.Mutex
	lastStreamId uint64
	mu           sync.RWMutex
	streams      map[uint64]net.Conn
}

func (c *Client) send(message string) error {
	c.wsMux.Lock()
	defer c.wsMux.Unlock()
	return c.ws.WriteMessage(websocket.TextMessage, []byte(message))
}

type Registry struct {
	mu      sync.RWMutex
	clients map[string]*Client
}

func NewRegistry() *Registry {
	return &Registry{
		clients: make(map[string]*Client),
	}
}

func (r *Registry) Set(token string, client *Client) {
	r.mu.Lock()
	r.clients[token] = client
	r.mu.Unlock()
}

func (r *Registry) Get(token string) (*Client, bool) {
	r.mu.RLock()
	client, ok := r.clients[token]
	r.mu.RUnlock()
	return client, ok
}

func (r *Registry) Del(token string) {
	r.mu.Lock()
	delete(r.clients, token)
	r.mu.Unlock()
}

var (
	upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}
	reg = NewRegistry()
)

func checkTokenValid(token string) (bool, error) {
	tokens, err := utils.LoadTokens()
	if err != nil {
		return false, err
	}

	for _, t := range tokens {
		if len(t) == len(token) &&
			subtle.ConstantTimeCompare([]byte(t), []byte(token)) == 1 {
			return true, nil
		}
	}
	return false, nil
}

func main() {
	http.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {

		host := r.URL.Query().Get("host")
		token := r.Header.Get("X-Tunnex-Token")

		if token == "" {
			http.Error(w, "token required", 401)
			return
		}

		if isValid, _ := checkTokenValid(token); isValid == false {
			http.Error(w, "invalid token provided", 401)
			return
		}

		fmt.Println("host:", host)

		ws, err := upgrader.Upgrade(w, r, nil)

		if err != nil {
			fmt.Println(err)
			return
		}

		c := &Client{host: host, ws: ws, streams: make(map[uint64]net.Conn), lastStreamId: 1}
		reg.Set(host, c)

		go clientListener(c)

		return
	})

	go func() {
		fmt.Println("HTTP server listening on :8888")
		log.Fatal(http.ListenAndServe(":8888", nil))
	}()

	http.HandleFunc("/", publicListener)

	//Network Listener
	log.Fatal(http.ListenAndServe(":5000", nil))

}

func clientListener(c *Client) {
	defer func() {
		c.mu.Lock()
		for id, conn := range c.streams {
			_ = conn.Close()
			delete(c.streams, id)
		}
		c.mu.Unlock()

		_ = c.ws.Close()
		reg.Del(c.host)
		fmt.Println("Client disconnected")
	}()
	for {
		_, data, err := c.ws.ReadMessage()
		if err != nil {
			fmt.Println("connection closed:", err)
			return
		}

		fmt.Println("read message")

		message := string(data)

		parts := strings.Split(message, ":")
		if len(parts) < 2 {
			continue
		}
		streamId, streamIdError := strconv.ParseUint(parts[1], 10, 64)

		if streamIdError != nil {
			fmt.Println("Invalid Stream Id:", streamIdError)
			continue
		}

		messageType := parts[0]
		fmt.Println(parts, "parts")

		switch {
		case strings.EqualFold(messageType, "msg"):
			fmt.Println("data message")

			if len(parts) != 3 {
				continue
			}
			decodedMsg, decodeError := hex.DecodeString(parts[2])
			if decodeError != nil {
				fmt.Println("Invalid Hex:", decodeError)
				continue
			}

			c.mu.RLock()
			currentStream := c.streams[streamId]
			c.mu.RUnlock()

			if currentStream != nil {
				if _, writeError := currentStream.Write(decodedMsg); writeError != nil {
					fmt.Println("Write Error:", writeError)
				}
			} else {
				_ = c.send(fmt.Sprintf("close:%d", streamId))
			}

		case strings.EqualFold(messageType, "close"):
			fmt.Println("c;lose message")
			c.mu.Lock()
			currentStream := c.streams[streamId]
			if currentStream != nil {
				_ = currentStream.Close()
				delete(c.streams, streamId)
			}
			c.mu.Unlock()

		}
	}
}

func prefixFromHost(host string) string {
	if i := strings.IndexByte(host, ':'); i >= 0 {
		host = host[:i]
	}
	parts := strings.Split(host, ".")
	if len(parts) < 3 {
		return ""
	}
	sub := parts[0]
	suffix := "-tunnex"
	if strings.HasSuffix(sub, suffix) {
		return strings.TrimSuffix(sub, suffix)
	}

	return ""
}

func publicListener(w http.ResponseWriter, r *http.Request) {
	host := r.Header.Get("X-Tunnex-Host")
	if host == "" {
		host = r.Host
	}

	prefix := prefixFromHost(host)
	if prefix == "" {
		http.Error(w, "no token host", 404)
		return
	}

	client, ok := reg.Get(prefix)
	if !ok {
		http.Error(w, "client not found for host", 404)
		return
	}

	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "HiJack is not supported", 500)
		return
	}

	conn, rw, err := hj.Hijack()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	id := atomic.AddUint64(&client.lastStreamId, 1)

	client.mu.Lock()
	client.streams[id] = conn
	client.mu.Unlock()

	_ = client.send(fmt.Sprintf("registered:%d", id))

	var firstRequestBuffer strings.Builder
	if err := r.Write(&firstRequestBuffer); err == nil {
		if rw != nil && rw.Reader != nil {
			if n := rw.Reader.Buffered(); n > 0 {
				extra := make([]byte, n)
				_, _ = rw.Reader.Read(extra)
				firstRequestBuffer.Write(extra)
			}
		}
		hexMsg := hex.EncodeToString([]byte(firstRequestBuffer.String()))
		msg := fmt.Sprintf("msg:%s:%s", strconv.FormatUint(id, 10), hexMsg)
		_ = client.send(msg)
	}

	go func() {
		defer func() {
			client.mu.Lock()
			if s := client.streams[id]; s != nil {
				_ = s.Close()
			}
			delete(client.streams, id)
			client.mu.Unlock()

			_ = client.send(fmt.Sprintf("close:%d", id))
			_ = conn.Close()
		}()

		buf := make([]byte, 100*1024)
		for {
			n, err := conn.Read(buf)
			if n > 0 {
				hexMsg := hex.EncodeToString(buf[:n])
				msg := fmt.Sprintf("msg:%d:%s", id, hexMsg)
				_ = client.send(msg)
			}
			if err != nil {
				if errors.Is(err, net.ErrClosed) ||
					strings.Contains(err.Error(), "use of closed network connection") ||
					err == io.EOF {
					return
				}
				var ne net.Error
				if errors.As(err, &ne) && ne.Timeout() {
					continue
				}
				return
			}
		}
	}()
}
