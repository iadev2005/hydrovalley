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

// subscriberBuffer es la capacidad del canal de cada suscriptor.
const subscriberBuffer = 128

// Subscriber representa una sesión WebSocket activa que recibe mensajes.
type Subscriber struct {
	send chan []byte
}

// Hub gestiona los suscriptores conectados para la entrega en tiempo real.
type Hub struct {
	// publishMu serializa "asignar seq + encolar a los suscriptores" para que
	// todos reciban los mensajes en orden estrictamente creciente de seq.
	// Nunca se mantiene durante una escritura de red: solo cubre el Append en
	// memoria y envíos no bloqueantes a canales amortiguados.
	publishMu   sync.Mutex
	mu          sync.RWMutex
	subscribers map[*Subscriber]struct{}
	serverEpoch string
	bufferSize  int
}

// NewHub inicializa el despachador Pub/Sub.
func NewHub(serverEpoch string) *Hub {
	return &Hub{
		subscribers: make(map[*Subscriber]struct{}),
		serverEpoch: serverEpoch,
		bufferSize:  subscriberBuffer,
	}
}

// Subscribe registra un suscriptor y devuelve un canal con capacidad amortiguada.
func (h *Hub) Subscribe() *Subscriber {
	sub := &Subscriber{
		send: make(chan []byte, h.bufferSize),
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

// Publish ejecuta append (que asigna el seq) y difunde el mensaje nuevo bajo un
// mismo candado, de modo que el orden de entrega a cada suscriptor coincide con
// el orden de seq. Es el único camino de publicación para WebSocket y gRPC.
func (h *Hub) Publish(append func() (*chatv1.ChatMessage, bool, error)) (*chatv1.ChatMessage, bool, error) {
	h.publishMu.Lock()
	defer h.publishMu.Unlock()

	msg, isNew, err := append()
	if err != nil || !isNew {
		return msg, isNew, err
	}
	h.Broadcast(msg)
	return msg, isNew, nil
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
// El envío no bloquea: un suscriptor con el búfer lleno se expulsa (se cierra su
// canal) para que su cliente reconecte y se resincronice con after_seq, en lugar
// de perder mensajes en silencio y dejar un hueco de seq sin detectar.
func (h *Hub) BroadcastRaw(data []byte) {
	var slow []*Subscriber

	h.mu.RLock()
	for sub := range h.subscribers {
		select {
		case sub.send <- data:
		default:
			slow = append(slow, sub)
		}
	}
	h.mu.RUnlock()

	for _, sub := range slow {
		h.Unsubscribe(sub)
	}
}

// SubscribersCount devuelve la cantidad de conexiones activas actualmente.
func (h *Hub) SubscribersCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subscribers)
}
