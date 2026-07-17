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
	EpisodeID     string  `json:"episode_id,omitempty"`
	ShotNumbers    []int             `json:"shot_numbers,omitempty"`    // empty = all shots
	WorkflowAction string            `json:"workflow_action,omitempty"`
	WorkflowParams map[string]string `json:"workflow_params,omitempty"`
	ClipID         string            `json:"clip_id,omitempty"`
	// ConfirmKeyframeAnomalies allows video generation to proceed after preflight warnings.
	ConfirmKeyframeAnomalies bool `json:"confirm_keyframe_anomalies,omitempty"`
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
	userIDs    map[*websocket.Conn]string
	mu         sync.RWMutex
	broadcast  chan WSResponse
	pongWait   time.Duration
	generation *GenerationService
	workflow   *WorkflowService
}

// SetGenerationService attaches the generation handler.
func (cm *ConnManager) SetGenerationService(gs *GenerationService) {
	cm.generation = gs
}

// SetWorkflowService attaches the workflow handler.
func (cm *ConnManager) SetWorkflowService(wfs *WorkflowService) {
	cm.workflow = wfs
}

// NewConnManager creates a new connection manager.
func NewConnManager() *ConnManager {
	return &ConnManager{
		clients:   make(map[*websocket.Conn]bool),
		userIDs:   make(map[*websocket.Conn]string),
		broadcast: make(chan WSResponse, 100),
		pongWait:  60 * time.Second,
	}
}

// Register adds a new WebSocket connection for a user.
func (cm *ConnManager) Register(conn *websocket.Conn, userID string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.clients[conn] = true
	cm.userIDs[conn] = userID
}

// Unregister removes a WebSocket connection.
func (cm *ConnManager) Unregister(conn *websocket.Conn) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if _, ok := cm.clients[conn]; ok {
		delete(cm.clients, conn)
		delete(cm.userIDs, conn)
		conn.Close()
	}
}

// UserID returns the authenticated user for a connection.
func (cm *ConnManager) UserID(conn *websocket.Conn) string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.userIDs[conn]
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
func (cm *ConnManager) ServeHTTP(w http.ResponseWriter, r *http.Request, userID string) {
	conn, err := Upgrader.Upgrade(w, r, nil)
	if err != nil {
		http.Error(w, "upgrade failed", http.StatusBadRequest)
		return
	}

	cm.Register(conn, userID)
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

		handleAction(cm, conn, &req)
	}
}

func handleAction(cm *ConnManager, conn *websocket.Conn, req *WSRequest) {
	userID := cm.UserID(conn)
	switch req.Action {
	case "ping":
		cm.Broadcast(WSResponse{Code: 0, Msg: "pong"})
	case "start_generate":
		if cm.generation != nil {
			cm.generation.handleStartGenerate(cm, userID, req)
		} else {
			cm.Broadcast(WSResponse{Code: 1, Msg: "generation service unavailable", Step: "error"})
		}
	case "cancel_generate":
		cm.Broadcast(WSResponse{Code: 0, Msg: "已取消"})
	case "run_workflow":
		if cm.workflow != nil {
			cm.workflow.handleRunWorkflow(cm, userID, req)
		} else {
			cm.Broadcast(WSResponse{Code: 1, Msg: "workflow service unavailable", Step: "workflow_error"})
		}
	case "pause_episode_pipeline":
		if cm.workflow != nil {
			cm.workflow.HandlePauseEpisodePipeline(cm, req)
		} else {
			cm.Broadcast(WSResponse{Code: 1, Msg: "workflow service unavailable", Step: "workflow_error"})
		}
	case "resume_episode_pipeline":
		if cm.workflow != nil {
			cm.workflow.HandleResumeEpisodePipeline(cm, userID, req)
		} else {
			cm.Broadcast(WSResponse{Code: 1, Msg: "workflow service unavailable", Step: "workflow_error"})
		}
	default:
		cm.Broadcast(WSResponse{
			Code: 1, Msg: fmt.Sprintf("unknown action: %s", req.Action), Step: "error",
		})
	}
}

func init() {
	rand.Seed(time.Now().UnixNano())
}
