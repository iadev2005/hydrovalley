package grpcserver

import (
	"context"
	"fmt"
	"testing"

	chatv1 "github.com/iadev2005/hydrovalley/gen/go/hydrovalley/chat/v1"
	"github.com/iadev2005/hydrovalley/src/chat/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type mockBroadcaster struct {
	messages []*chatv1.ChatMessage
}

func (m *mockBroadcaster) Broadcast(msg *chatv1.ChatMessage) {
	m.messages = append(m.messages, msg)
}

func uuidN(n int) string {
	return fmt.Sprintf("00000000-0000-4000-8000-%012d", n)
}

func TestServer_SendMessage_ValidAndIdempotent(t *testing.T) {
	st, err := store.New("")
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	mb := &mockBroadcaster{}
	srv := New(st, mb)
	ctx := context.Background()

	req := &chatv1.SendMessageRequest{
		MessageId: uuidN(1),
		SenderId:  uuidN(10),
		Username:  "ana",
		Text:      "hola a todos",
		Kind:      chatv1.MessageKind_MESSAGE_KIND_PLAYER,
	}

	resp, err := srv.SendMessage(ctx, req)
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if resp.GetMessage().GetSeq() != 1 {
		t.Errorf("got seq %d, want 1", resp.GetMessage().GetSeq())
	}
	if resp.GetServerEpoch() != st.ServerEpoch() {
		t.Errorf("server_epoch mismatch")
	}
	if len(mb.messages) != 1 {
		t.Errorf("broadcaster received %d messages, want 1", len(mb.messages))
	}

	// Reintento idempotente: mismo message_id
	resp2, err := srv.SendMessage(ctx, req)
	if err != nil {
		t.Fatalf("SendMessage duplicate: %v", err)
	}
	if resp2.GetMessage().GetSeq() != 1 {
		t.Errorf("got seq %d on duplicate, want 1", resp2.GetMessage().GetSeq())
	}
	// No debe difundir por duplicado
	if len(mb.messages) != 1 {
		t.Errorf("broadcaster received %d messages after duplicate, want 1", len(mb.messages))
	}
}

func TestServer_SendMessage_Invalid(t *testing.T) {
	st, _ := store.New("")
	srv := New(st, nil)
	ctx := context.Background()

	// Nil request
	if _, err := srv.SendMessage(ctx, nil); status.Code(err) != codes.InvalidArgument {
		t.Errorf("expected InvalidArgument on nil request, got %v", err)
	}

	// Invalid UUID
	req := &chatv1.SendMessageRequest{
		MessageId: "bad-id",
		Text:      "hola",
	}
	if _, err := srv.SendMessage(ctx, req); status.Code(err) != codes.InvalidArgument {
		t.Errorf("expected InvalidArgument on bad UUID, got %v", err)
	}

	// Empty text
	req2 := &chatv1.SendMessageRequest{
		MessageId: uuidN(1),
		Text:      "",
	}
	if _, err := srv.SendMessage(ctx, req2); status.Code(err) != codes.InvalidArgument {
		t.Errorf("expected InvalidArgument on empty text, got %v", err)
	}
}

func TestServer_ListMessages(t *testing.T) {
	st, _ := store.New("")
	srv := New(st, nil)
	ctx := context.Background()

	for i := 1; i <= 5; i++ {
		_, _ = srv.SendMessage(ctx, &chatv1.SendMessageRequest{
			MessageId: uuidN(i),
			Text:      fmt.Sprintf("msg %d", i),
		})
	}

	resp, err := srv.ListMessages(ctx, &chatv1.ListMessagesRequest{
		AfterSeq: 2,
		Limit:    10,
	})
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(resp.GetMessages()) != 3 {
		t.Fatalf("got %d messages, want 3", len(resp.GetMessages()))
	}
	if resp.GetLastSeq() != 5 {
		t.Errorf("got lastSeq %d, want 5", resp.GetLastSeq())
	}
	if resp.GetServerEpoch() != st.ServerEpoch() {
		t.Errorf("epoch mismatch")
	}
	if resp.GetMessages()[0].GetSeq() != 3 {
		t.Errorf("first message seq: got %d, want 3", resp.GetMessages()[0].GetSeq())
	}
}
