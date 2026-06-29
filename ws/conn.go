package ws

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Upgrader handles WebSocket upgrades.
var Upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// WSRequest represents a client-to-server message.
type WSRequest struct {
	Action        string  `json:"action"`
	Script        string  `json:"script,omitempty"`
	Style         string  `json:"style,omitempty"`
	FrameDuration float64 `json:"frame_duration,omitempty"`
	Resolution    string  `json:"resolution,omitempty"`
	FPS           int     `json:"fps,omitempty"`
	Mode          string  `json:"mode,omitempty"`       // full, parse, images, video
	ProjectID     string  `json:"project_id,omitempty"`
}

// WSResponse represents a server-to-client message.
type WSResponse struct {
	Code     int             `json:"code"`
	Msg      string          `json:"msg"`
	Step     string          `json:"step"`
	Progress float32         `json:"progress"`
	Data     json.RawMessage `json:"data,omitempty"`
}

// MustMarshalJSON marshals to JSON raw message. Panics on error.
func MustMarshalJSON(v interface{}) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}

// ConnManager manages WebSocket connections and message broadcasting.
type ConnManager struct {
	clients    map[*websocket.Conn]bool
	mu         sync.RWMutex
	broadcast  chan WSResponse
	pongWait   time.Duration
	generation *GenerationService
}

// SetGenerationService attaches the generation handler.
func (cm *ConnManager) SetGenerationService(gs *GenerationService) {
	cm.generation = gs
}

// NewConnManager creates a new connection manager.
func NewConnManager() *ConnManager {
	return &ConnManager{
		clients:   make(map[*websocket.Conn]bool),
		broadcast: make(chan WSResponse, 100),
		pongWait:  60 * time.Second,
	}
}

// Register adds a new WebSocket connection.
func (cm *ConnManager) Register(conn *websocket.Conn) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.clients[conn] = true
}

// Unregister removes a WebSocket connection.
func (cm *ConnManager) Unregister(conn *websocket.Conn) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if _, ok := cm.clients[conn]; ok {
		delete(cm.clients, conn)
		conn.Close()
	}
}

// Broadcast sends a message to all connected clients.
func (cm *ConnManager) Broadcast(msg WSResponse) {
	select {
	case cm.broadcast <- msg:
	default:
	}
}

// Run starts the broadcast loop and heartbeat.
func (cm *ConnManager) Run() {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case msg := <-cm.broadcast:
				cm.mu.RLock()
				clients := make([]*websocket.Conn, 0, len(cm.clients))
				for conn := range cm.clients {
					clients = append(clients, conn)
				}
				cm.mu.RUnlock()

				for _, conn := range clients {
					if err := conn.WriteJSON(msg); err != nil {
						cm.Unregister(conn)
					}
				}

			case <-ticker.C:
				cm.mu.RLock()
				clients := make([]*websocket.Conn, 0, len(cm.clients))
				for conn := range cm.clients {
					clients = append(clients, conn)
				}
				cm.mu.RUnlock()

				for _, conn := range clients {
					conn.SetWriteDeadline(time.Now().Add(cm.pongWait))
					if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
						cm.Unregister(conn)
					}
				}
			}
		}
	}()
}

// ServeHTTP upgrades the HTTP connection to WebSocket.
func (cm *ConnManager) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := Upgrader.Upgrade(w, r, nil)
	if err != nil {
		http.Error(w, "upgrade failed", http.StatusBadRequest)
		return
	}

	cm.Register(conn)
	defer cm.Unregister(conn)

	conn.SetReadLimit(4096)
	conn.SetReadDeadline(time.Now().Add(cm.pongWait))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(cm.pongWait))
		return nil
	})

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			break
		}

		var req WSRequest
		if err := json.Unmarshal(message, &req); err != nil {
			cm.Broadcast(WSResponse{Code: 1, Msg: "invalid JSON", Step: "error"})
			continue
		}

		handleAction(cm, &req)
	}
}

func handleAction(cm *ConnManager, req *WSRequest) {
	switch req.Action {
	case "ping":
		cm.Broadcast(WSResponse{Code: 0, Msg: "pong"})
	case "start_generate":
		if cm.generation != nil {
			cm.generation.handleStartGenerate(cm, req)
		} else {
			cm.Broadcast(WSResponse{Code: 1, Msg: "generation service unavailable", Step: "error"})
		}
	case "cancel_generate":
		cm.Broadcast(WSResponse{Code: 0, Msg: "已取消"})
	default:
		cm.Broadcast(WSResponse{
			Code: 1, Msg: fmt.Sprintf("unknown action: %s", req.Action), Step: "error",
		})
	}
}

func init() {
	rand.Seed(time.Now().UnixNano())
}
