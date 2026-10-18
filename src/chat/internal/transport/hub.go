// Package transport expone el servicio de chat por HTTP y WebSocket:
// las rutas /healthz, /readyz y el WebSocket /chat en tiempo real.
package transport

import (
	"sync"

	chatv1 "github.com/iadev2005/hydrovalley/gen/go/hydrovalley/chat/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

// Options define los ajustes del transporte HTTP y WebSocket.
type Options struct {
	// OriginPatterns son los orígenes permitidos además del actual.
	OriginPatterns []string
}

// Subscriber representa una sesión WebSocket activa que recibe mensajes.
type Subscriber struct {
	send chan []byte
}

// Hub gestiona los suscriptores conectados para la entrega en tiempo real.
type Hub struct {
	mu          sync.RWMutex
	subscribers map[*Subscriber]struct{}
	serverEpoch string
}

// NewHub inicializa el despachador Pub/Sub.
func NewHub(serverEpoch string) *Hub {
	return &Hub{
		subscribers: make(map[*Subscriber]struct{}),
		serverEpoch: serverEpoch,
	}
}

// Subscribe registra un suscriptor y devuelve un canal con capacidad amortiguada.
func (h *Hub) Subscribe() *Subscriber {
	sub := &Subscriber{
		send: make(chan []byte, 128),
	}
	h.mu.Lock()
	h.subscribers[sub] = struct{}{}
	h.mu.Unlock()
	return sub
}

// Unsubscribe elimina un suscriptor y cierra su canal.
func (h *Hub) Unsubscribe(sub *Subscriber) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.subscribers[sub]; ok {
		delete(h.subscribers, sub)
		close(sub.send)
	}
}

// Broadcast serializa un ChatMessage a MessageFrame y lo difunde a todos los clientes.
func (h *Hub) Broadcast(msg *chatv1.ChatMessage) {
	if msg == nil {
		return
	}

	kindStr := "player"
	if msg.GetKind() == chatv1.MessageKind_MESSAGE_KIND_SYSTEM {
		kindStr = "system"
	}

	frame := &chatv1.MessageFrame{
		Type:        "message",
		Seq:         msg.GetSeq(),
		MessageId:   msg.GetMessageId(),
		SenderId:    msg.GetSenderId(),
		Username:    msg.GetUsername(),
		Text:        msg.GetText(),
		Kind:        kindStr,
		SentAt:      msg.GetSentAt(),
		ServerEpoch: h.serverEpoch,
	}

	data, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(frame)
	if err != nil {
		return
	}

	h.BroadcastRaw(data)
}

// BroadcastRaw envía datos JSON serializados a todos los suscriptores activos.
func (h *Hub) BroadcastRaw(data []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for sub := range h.subscribers {
		select {
		case sub.send <- data:
		default:
			// Si el cliente no lee a tiempo su buffer, se descarta el mensaje
			// para no bloquear el dispatcher central.
		}
	}
}

// SubscribersCount devuelve la cantidad de conexiones activas actualmente.
func (h *Hub) SubscribersCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subscribers)
}
