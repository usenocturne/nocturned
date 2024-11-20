package ws

import (
	"log"
	"sync"

	"github.com/gorilla/websocket"

	"github.com/usenocturne/nocturned/utils"
)

type WebSocketHub struct {
	clients map[*websocket.Conn]bool
	mu      sync.Mutex
}

func NewWebSocketHub() *WebSocketHub {
	return &WebSocketHub{
		clients: make(map[*websocket.Conn]bool),
	}
}

func (h *WebSocketHub) AddClient(conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clients[conn] = true
}

func (h *WebSocketHub) RemoveClient(conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.clients[conn]; ok {
		delete(h.clients, conn)
		conn.Close()
	}
}

func (h *WebSocketHub) Broadcast(event utils.WebSocketEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for conn := range h.clients {
		if err := conn.WriteJSON(event); err != nil {
			log.Printf("Error broadcasting to client: %v", err)
			h.RemoveClient(conn)
		}
	}
}
