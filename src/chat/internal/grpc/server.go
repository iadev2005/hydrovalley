// Package grpcserver implementa el servidor gRPC ChatService definido en
// proto/hydrovalley/chat/v1/chat.proto (métodos unarios SendMessage y ListMessages).
package grpcserver

import (
	"context"

	chatv1 "github.com/iadev2005/hydrovalley/gen/go/hydrovalley/chat/v1"
	"github.com/iadev2005/hydrovalley/src/chat/internal/auth"
	"github.com/iadev2005/hydrovalley/src/chat/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Broadcaster publica mensajes nuevos a los clientes WebSocket. Publish ejecuta
// append (asignación de seq) y la difusión de forma atómica y ordenada.
type Broadcaster interface {
	Publish(append func() (*chatv1.ChatMessage, bool, error)) (*chatv1.ChatMessage, bool, error)
}

// Server implementa ChatServiceServer.
type Server struct {
	chatv1.UnimplementedChatServiceServer
	store       *store.Store
	broadcaster Broadcaster
}

// New crea una nueva instancia de Server.
func New(s *store.Store, b Broadcaster) *Server {
	return &Server{
		store:       s,
		broadcaster: b,
	}
}

// SendMessage publica un mensaje de forma idempotente.
// Si el mensaje es nuevo, lo difunde a los clientes WebSocket en vivo.
func (s *Server) SendMessage(ctx context.Context, req *chatv1.SendMessageRequest) (*chatv1.SendMessageResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request cannot be nil")
	}
	if !auth.IsUUID(req.GetMessageId()) {
		return nil, status.Error(codes.InvalidArgument, "message_id must be a valid UUID")
	}
	if req.GetText() == "" {
		return nil, status.Error(codes.InvalidArgument, "text cannot be empty")
	}

	appendFn := func() (*chatv1.ChatMessage, bool, error) {
		return s.store.Append(req, nil)
	}
	var msg *chatv1.ChatMessage
	var err error
	if s.broadcaster != nil {
		msg, _, err = s.broadcaster.Publish(appendFn)
	} else {
		msg, _, err = appendFn()
	}
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "append message: %v", err)
	}

	return &chatv1.SendMessageResponse{
		Message:     msg,
		ServerEpoch: s.store.ServerEpoch(),
	}, nil
}

// ListMessages devuelve los mensajes con seq > after_seq, del más antiguo al más reciente.
func (s *Server) ListMessages(ctx context.Context, req *chatv1.ListMessagesRequest) (*chatv1.ListMessagesResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request cannot be nil")
	}

	messages, lastSeq, epoch := s.store.ListAfter(req.GetAfterSeq(), req.GetLimit())

	return &chatv1.ListMessagesResponse{
		Messages:    messages,
		LastSeq:     lastSeq,
		ServerEpoch: epoch,
	}, nil
}
