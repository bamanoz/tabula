package kernel

import (
	"encoding/json"

	"github.com/gorilla/websocket"
)

const sendBufSize = 64

// Client represents a single WebSocket connection.
type Client struct {
	hub       *Hub
	conn      *websocket.Conn
	name      string
	session   string
	id        int
	depth     int
	sends     map[string]bool
	receives  map[string]bool
	hooks     []HookSubscription
	sendCh    chan []byte
	connected bool
}

// NewClient creates a client and starts its pumps.
// Returns nil if the hub is at capacity.
func NewClient(hub *Hub, conn *websocket.Conn) *Client {
	c := &Client{
		hub:    hub,
		conn:   conn,
		sendCh: make(chan []byte, sendBufSize),
	}
	if !hub.Register(c) {
		conn.Close()
		return nil
	}
	go c.writePump()
	go c.readPump()
	return c
}

func (c *Client) canSend(msgType string) bool {
	return c.sends[msgType]
}

func (c *Client) canReceive(msgType string) bool {
	return c.receives[msgType]
}

// SendMsg marshals and queues a message for sending.
func (c *Client) SendMsg(msg *Message) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	c.SendRaw(data)
}

// SendRaw queues raw JSON bytes for sending.
func (c *Client) SendRaw(data []byte) {
	select {
	case c.sendCh <- data:
	default:
		c.hub.Logger.Warn("dropping message to slow client", "client", c.name)
	}
}

// readPump reads messages from WebSocket and dispatches to hub.
func (c *Client) readPump() {
	defer func() {
		c.hub.Unregister(c)
		c.conn.Close()
		close(c.sendCh)
	}()

	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			break
		}

		var msg Message
		if err := json.Unmarshal(data, &msg); err != nil {
			c.hub.Logger.Warn("bad JSON from client", "client", c.name, "error", err)
			continue
		}

		c.hub.HandleMessage(c, &msg)
	}
}

// writePump writes messages from sendCh to the WebSocket.
func (c *Client) writePump() {
	for data := range c.sendCh {
		if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
			break
		}
	}
}
