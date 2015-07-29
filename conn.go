// Copyright 2013 The Gorilla WebSocket Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	models "github.com/bitrise-io/bitrise-cli/models/models_1_0_0"
	"github.com/gorilla/websocket"
)

// initMessage ...
type initMessage struct {
	Type string                  `json:"type"`
	Msg  models.BitriseDataModel `json:"msg"`
}

//Message ...
type Message struct {
	Type string `json:"type"`
	Msg  string `json:"msg"`
}

func sendMessage(Type string, msg string) {
	var m = &Message{}
	m.Type = Type
	m.Msg = msg
	byteArr, err := json.Marshal(m)
	printError("Json encoding:", err)
	h.broadcast <- byteArr
}

const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer.
	maxMessageSize = 512
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

// connection is an middleman between the websocket connection and the hub.
type connection struct {
	// The websocket connection.
	ws *websocket.Conn

	// Buffered channel of outbound messages.
	send chan []byte
}

// readPump pumps messages from the websocket connection to the hub.
func (c *connection) readPump() {
	defer func() {
		h.unregister <- c
		c.ws.Close()
	}()
	c.ws.SetReadLimit(maxMessageSize)
	c.ws.SetReadDeadline(time.Now().Add(pongWait))
	c.ws.SetPongHandler(func(string) error { c.ws.SetReadDeadline(time.Now().Add(pongWait)); return nil })
	for {
		_, message, err := c.ws.ReadMessage()
		if err != nil {
			break
		}
		//handle messages
		var dat = &Message{}
		json.Unmarshal(message, &dat)
		if dat.Type == "init" {
			message = readYAMLToBytes()
			h.broadcast <- message
		} else if dat.Type == "build" && !testRunning {
			go runCommand(c, dat.Msg)
			sendMessage("info", "$bitrise-cli run "+dat.Msg+"\n")
		} else if dat.Type == "abort" && testRunning {
			abort <- "Aborting build"
			sendMessage("info", "Aborting build\n")
		}
	}
}

// write writes a message with the given message type and payload.
func (c *connection) write(mt int, payload []byte) error {
	c.ws.SetWriteDeadline(time.Now().Add(writeWait))
	return c.ws.WriteMessage(mt, payload)
}

// writePump pumps messages from the hub to the websocket connection.
func (c *connection) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.ws.Close()
	}()
	for {
		select {
		case message, ok := <-c.send:
			if !ok {
				c.write(websocket.CloseMessage, []byte{})
				return
			}

			if err := c.write(websocket.TextMessage, message); err != nil {
				return
			}
		case <-ticker.C:
			if err := c.write(websocket.PingMessage, []byte{}); err != nil {
				return
			}
		}
	}
}

func (c *connection) sendHistory() {
	for _, val := range history {
		//c.send <- val
		sendMessage("info", (string)(val))
		time.Sleep(time.Millisecond * 1)
	}
}

// serverWs handles websocket requests from the peer.
func serveWs(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", 405)
		return
	}
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}
	c := &connection{send: make(chan []byte, 256), ws: ws}
	h.register <- c
	go c.writePump()
	c.sendHistory()
	c.readPump()
}
