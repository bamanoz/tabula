package kernel

import (
	"encoding/json"
	"sync"

	"github.com/gorilla/websocket"
)

const sendBufSize = 64

// ClientState represents the protocol lifecycle state of a client.
type ClientState string

const (
	ClientSocketConnected ClientState = "socket_connected"
	ClientProtocolReady   ClientState = "protocol_ready"
	ClientJoined          ClientState = "joined"
	ClientClosed          ClientState = "closed"
)

// Client represents a single WebSocket connection.
type Client struct {
	hub            *Hub
	conn           *websocket.Conn
	name           string
	session        string
	id             int
	depth          int
	sends          map[string]bool
	receives       map[string]bool
	receivesGlobal map[string]bool // message types to receive from all sessions
	hooks          []HookSubscription
	sendCh         chan []byte
	sendMu         sync.Mutex
	sendClosed     bool
	state          ClientState
	recvCh         chan *Message // if set, messages go here instead of WebSocket
	done           chan struct{} // closed when the client disconnects
	doneOnce       sync.Once
}

// Done returns a channel that is closed when the client disconnects.
// Used by the hook engine to cancel pending interactive hooks (timeout=0)
// when the responding client goes away.
func (c *Client) Done() <-chan struct{} {
	return c.done
}

// NewClient creates a client and starts its pumps.
// Returns nil if the hub is at capacity.
func NewClient(hub *Hub, conn *websocket.Conn) *Client {
	c := &Client{
		hub:    hub,
		conn:   conn,
		sendCh: make(chan []byte, sendBufSize),
		state:  ClientSocketConnected,
		done:   make(chan struct{}),
	}
	if !hub.Register(c) {
		conn.Close()
		return nil
	}
	go c.writePump()
	go c.readPump()
	return c
}

// transition moves the client to a new state, returning false if invalid.
func (c *Client) transition(to ClientState) bool {
	valid := map[ClientState]map[ClientState]bool{
		ClientSocketConnected: {ClientProtocolReady: true, ClientClosed: true},
		ClientProtocolReady:   {ClientJoined: true, ClientClosed: true},
		ClientJoined:          {ClientClosed: true},
		ClientClosed:          {},
	}
	if !valid[c.state][to] {
		return false
	}
	c.state = to
	return true
}

// MarkProtocolReady transitions from socket_connected to protocol_ready.
func (c *Client) MarkProtocolReady() bool {
	return c.transition(ClientProtocolReady)
}

// MarkJoined transitions to joined state.
func (c *Client) MarkJoined() bool {
	return c.transition(ClientJoined)
}

// MarkClosed transitions to closed state.
func (c *Client) MarkClosed() {
	c.transition(ClientClosed)
	c.doneOnce.Do(func() { close(c.done) })
}

// IsConnected returns true if the client is in a live state.
func (c *Client) IsConnected() bool {
	return c.state != ClientClosed
}

func (c *Client) IsBusy() bool { return false }

// Name returns the client's name. Implements HookSubscriber.
func (c *Client) Name() string { return c.name }

// Session returns the client's session id (empty for global subscribers).
// Implements HookSubscriber.
func (c *Client) Session() string { return c.session }

// Hooks returns the client's hook subscriptions. Implements HookSubscriber.
func (c *Client) Hooks() []HookSubscription { return c.hooks }

func (c *Client) canSend(msgType string) bool {
	return c.sends[msgType]
}

func (c *Client) canReceive(msgType string) bool {
	return c.receives[msgType]
}

// canReceiveGlobal returns true if this client wants to receive the given
// message type from all sessions (regardless of session membership).
func (c *Client) canReceiveGlobal(msgType string) bool {
	return c.receivesGlobal[msgType]
}

// SendMsg marshals and queues a message for sending.
func (c *Client) SendMsg(msg *Message) {
	if msg != nil && msg.V == 0 {
		msg.V = ProtocolVersion
	}
	if c.recvCh != nil {
		// Internal client: send directly to receive channel.
		select {
		case c.recvCh <- msg:
		default:
			c.hub.Logger.Warn("dropping message to slow internal client", "client", c.name)
		}
		return
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	c.SendRaw(data)
}

// SendRaw queues raw JSON bytes for sending.
func (c *Client) SendRaw(data []byte) {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	if c.sendClosed {
		return
	}
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
		c.closeSend()
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

		if err := validateMessage(&msg); err != nil {
			c.hub.Logger.Warn("invalid message from client", "client", c.name, "error", err)
			c.SendMsg(&Message{Type: string(MsgError), Text: err.Error()})
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

func (c *Client) closeSend() {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	if c.sendClosed {
		return
	}
	c.sendClosed = true
	close(c.sendCh)
	if c.recvCh != nil {
		close(c.recvCh)
	}
	c.doneOnce.Do(func() { close(c.done) })
}
