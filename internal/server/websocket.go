package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"

	"github.com/coder/websocket"
	"github.com/jamesruggles/reconsuite/internal/tools"
)

// Hub manages WebSocket clients subscribed to scan output.
type Hub struct {
	mu      sync.RWMutex
	clients map[int64]map[*websocket.Conn]struct{}
}

func NewHub() *Hub {
	return &Hub{
		clients: make(map[int64]map[*websocket.Conn]struct{}),
	}
}

func (h *Hub) Subscribe(scanID int64, conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.clients[scanID] == nil {
		h.clients[scanID] = make(map[*websocket.Conn]struct{})
	}
	h.clients[scanID][conn] = struct{}{}
}

func (h *Hub) Unsubscribe(scanID int64, conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if conns, ok := h.clients[scanID]; ok {
		delete(conns, conn)
		if len(conns) == 0 {
			delete(h.clients, scanID)
		}
	}
}

func (h *Hub) Broadcast(scanID int64, line tools.OutputLine) {
	h.mu.RLock()
	conns := h.clients[scanID]
	h.mu.RUnlock()

	data, err := json.Marshal(line)
	if err != nil {
		return
	}

	for conn := range conns {
		err := conn.Write(context.Background(), websocket.MessageText, data)
		if err != nil {
			slog.Debug("ws write error", "error", err)
			h.Unsubscribe(scanID, conn)
			conn.Close(websocket.StatusNormalClosure, "")
		}
	}
}

type wsSubscribeMsg struct {
	ScanID int64 `json:"scan_id"`
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{"*"},
	})
	if err != nil {
		slog.Error("ws accept error", "error", err)
		return
	}
	defer conn.CloseNow()

	// Read subscribe message
	_, data, err := conn.Read(r.Context())
	if err != nil {
		return
	}

	var msg wsSubscribeMsg
	if err := json.Unmarshal(data, &msg); err != nil || msg.ScanID == 0 {
		conn.Close(websocket.StatusInvalidFramePayloadData, "invalid subscribe message")
		return
	}

	s.hub.Subscribe(msg.ScanID, conn)
	defer s.hub.Unsubscribe(msg.ScanID, conn)

	// Keep connection alive — wait for close or context cancellation
	for {
		_, _, err := conn.Read(r.Context())
		if err != nil {
			return
		}
	}
}
