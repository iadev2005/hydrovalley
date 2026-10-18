// Package store implementa el almacenamiento en memoria (ring buffer) de los
// últimos 200 mensajes del chat (reglas 4 y 5: sin base de datos, sin streams).
// Asigna el número de secuencia monótonamente creciente (`seq`), reloj lógico
// del chat (RF-14, CP-09).
package store

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	chatv1 "github.com/iadev2005/hydrovalley/gen/go/hydrovalley/chat/v1"
	"github.com/iadev2005/hydrovalley/src/chat/internal/auth"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Capacity es la capacidad máxima de mensajes retenidos en memoria (200).
const Capacity = 200

var (
	// ErrInvalidMessageID indica que el message_id no es un UUID válido.
	ErrInvalidMessageID = errors.New("message_id must be a valid UUID")
	// ErrEmptyText indica que el texto del mensaje está vacío.
	ErrEmptyText = errors.New("message text cannot be empty")
)

// Store gestiona los mensajes en memoria con un búfer circular concurrente.
type Store struct {
	mu          sync.RWMutex
	serverEpoch string
	lastSeq     uint64
	head        int
	count       int
	entries     [Capacity]*chatv1.ChatMessage
	byID        map[string]*chatv1.ChatMessage
}

// New crea una instancia de Store. Si serverEpoch está vacío, genera un UUID v4.
func New(serverEpoch string) (*Store, error) {
	epoch := strings.TrimSpace(serverEpoch)
	if epoch == "" {
		uuid, err := newUUID()
		if err != nil {
			return nil, fmt.Errorf("generate server_epoch: %w", err)
		}
		epoch = uuid
	} else if !auth.IsUUID(epoch) {
		return nil, fmt.Errorf("server_epoch %q is not a valid UUID", epoch)
	}

	return &Store{
		serverEpoch: epoch,
		byID:        make(map[string]*chatv1.ChatMessage, Capacity),
	}, nil
}

// ServerEpoch devuelve el UUID de la época de ejecución actual.
func (s *Store) ServerEpoch() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.serverEpoch
}

// LastSeq devuelve el mayor seq asignado hasta ahora (0 si no hay mensajes).
func (s *Store) LastSeq() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastSeq
}

// Append agrega un mensaje al buffer de forma idempotente.
// Si ya existe un mensaje con el mismo message_id, lo retorna con isNew = false
// sin incrementar seq ni duplicar el mensaje.
func (s *Store) Append(req *chatv1.SendMessageRequest, sentAt *timestamppb.Timestamp) (*chatv1.ChatMessage, bool, error) {
	if !auth.IsUUID(req.GetMessageId()) {
		return nil, false, ErrInvalidMessageID
	}
	if strings.TrimSpace(req.GetText()) == "" {
		return nil, false, ErrEmptyText
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Idempotencia: si ya existe entre los últimos 200, devolverlo tal cual.
	if existing, found := s.byID[req.GetMessageId()]; found {
		return existing, false, nil
	}

	if sentAt == nil {
		sentAt = timestamppb.Now()
	}

	s.lastSeq++
	msg := &chatv1.ChatMessage{
		Seq:       s.lastSeq,
		MessageId: req.GetMessageId(),
		SenderId:  req.GetSenderId(),
		Username:  req.GetUsername(),
		Text:      req.GetText(),
		Kind:      req.GetKind(),
		SentAt:    sentAt,
	}

	if s.count == Capacity {
		// Descartar el más antiguo (FIFO) del mapa de identificadores
		oldest := s.entries[s.head]
		delete(s.byID, oldest.GetMessageId())
		s.entries[s.head] = msg
		s.head = (s.head + 1) % Capacity
	} else {
		insertIdx := (s.head + s.count) % Capacity
		s.entries[insertIdx] = msg
		s.count++
	}

	s.byID[msg.GetMessageId()] = msg
	return msg, true, nil
}

// ListAfter devuelve los mensajes con seq > afterSeq en orden ascendente,
// hasta un máximo de limit (o 200 si limit == 0).
func (s *Store) ListAfter(afterSeq uint64, limit uint32) ([]*chatv1.ChatMessage, uint64, string) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	maxResults := int(limit)
	if maxResults <= 0 || maxResults > Capacity {
		maxResults = Capacity
	}

	result := make([]*chatv1.ChatMessage, 0, min(maxResults, s.count))
	for i := 0; i < s.count && len(result) < maxResults; i++ {
		idx := (s.head + i) % Capacity
		msg := s.entries[idx]
		if msg.GetSeq() > afterSeq {
			result = append(result, msg)
		}
	}

	return result, s.lastSeq, s.serverEpoch
}

// GetByID busca un mensaje por su message_id en los últimos 200 retenidos.
func (s *Store) GetByID(messageID string) (*chatv1.ChatMessage, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	msg, found := s.byID[messageID]
	return msg, found
}

// Count devuelve la cantidad de mensajes actualmente retenidos en el buffer (0 a 200).
func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.count
}

// newUUID genera un UUID v4 canónico (RFC 4122).
func newUUID() (string, error) {
	var b [16]byte
	if _, err := io.ReadFull(rand.Reader, b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40 // versión 4
	b[8] = (b[8] & 0x3f) | 0x80 // variante RFC 4122
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}
