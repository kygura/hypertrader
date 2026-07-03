package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

// Tuning constants for the WS push stream. wsSendBuffer bounds how many
// undelivered frames a slow client can accrue before the oldest is dropped —
// the same non-blocking, drop-oldest semantics bus.topic.publish uses for its
// subscribers, applied one layer further out to WS clients.
const (
	wsSendBuffer  = 64
	wsPingPeriod  = 30 * time.Second
	wsReadTimeout = 90 * time.Second
)

// wsFrame is the envelope every WS message uses: {"topic":"...","data":...}.
type wsFrame struct {
	Topic string `json:"topic"`
	Data  any    `json:"data"`
}

// wsClient is one connected WS client: a connection plus a buffered outbound
// queue drained by its own writer goroutine, so one slow reader can never
// stall the broadcast loop (runCaches) that enqueues into it.
type wsClient struct {
	conn *websocket.Conn
	send chan []byte
	done chan struct{}
}

// enqueue delivers b to the client's send buffer without blocking. If the
// buffer is full, the oldest queued frame is dropped to make room for the
// newest — mirroring bus.topic.publish's backpressure policy.
func (c *wsClient) enqueue(b []byte) {
	select {
	case c.send <- b:
	default:
		select {
		case <-c.send:
		default:
		}
		select {
		case c.send <- b:
		default:
		}
	}
}

// wsUpgrader builds an Upgrader whose CheckOrigin matches the server's CORS
// allowlist. A browser always sends an Origin header on a WS handshake; a
// missing Origin (curl, a test client, a non-browser agent) is allowed
// through, same as gorilla's own zero-value default when Origin is absent.
func (s *Server) wsUpgrader() websocket.Upgrader {
	return websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			origin := r.Header.Get("Origin")
			if origin == "" {
				return true
			}
			return s.originAllowed(origin)
		},
	}
}

// handleWS upgrades the connection and registers the client, then runs its
// read and write pumps until the connection dies. Auth for this route is
// enforced by authMiddleware (Bearer header or ?token= query param) before
// this handler ever runs.
func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	upgrader := s.wsUpgrader()
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return // Upgrade already wrote an HTTP error response.
	}

	client := &wsClient{
		conn: conn,
		send: make(chan []byte, wsSendBuffer),
		done: make(chan struct{}),
	}
	s.registerClient(client)

	go s.writePump(client)
	s.readPump(client) // blocks until the connection dies
}

// registerClient adds a client to the shared registry under the server's one
// state mutex.
func (s *Server) registerClient(c *wsClient) {
	s.state.mu.Lock()
	s.state.wsClients[c] = struct{}{}
	s.state.mu.Unlock()
}

// unregisterClient removes a client and closes its done channel exactly once,
// signalling the writer goroutine to exit.
func (s *Server) unregisterClient(c *wsClient) {
	s.state.mu.Lock()
	if _, ok := s.state.wsClients[c]; ok {
		delete(s.state.wsClients, c)
		close(c.done)
	}
	s.state.mu.Unlock()
}

// readPump keeps the connection alive (pong resets the read deadline) and
// blocks on ReadMessage purely to detect a dead/closed connection — this
// server never expects incoming client frames. On any read error the client
// is unregistered and the connection closed.
func (s *Server) readPump(c *wsClient) {
	defer func() {
		s.unregisterClient(c)
		c.conn.Close()
	}()
	c.conn.SetReadDeadline(time.Now().Add(wsReadTimeout))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(wsReadTimeout))
		return nil
	})
	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			return
		}
	}
}

// writePump drains the client's send buffer to the connection and pings
// periodically. It is the one goroutine per client that owns writes to the
// underlying connection (gorilla/websocket forbids concurrent writers).
func (s *Server) writePump(c *wsClient) {
	ticker := time.NewTicker(wsPingPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-c.done:
			return
		case b := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.TextMessage, b); err != nil {
				s.unregisterClient(c)
				c.conn.Close()
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				s.unregisterClient(c)
				c.conn.Close()
				return
			}
		}
	}
}

// broadcast frames data under topic and enqueues it to every registered WS
// client. Marshal failures are dropped silently — a malformed frame isn't
// worth taking the server down for, and every producer here is an internal
// bus event, not user input.
func (s *Server) broadcast(topic string, data any) {
	b, err := json.Marshal(wsFrame{Topic: topic, Data: data})
	if err != nil {
		return
	}
	s.state.mu.RLock()
	defer s.state.mu.RUnlock()
	for c := range s.state.wsClients {
		c.enqueue(b)
	}
}
