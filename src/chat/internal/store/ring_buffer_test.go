package store

import (
	"fmt"
	"sync"
	"testing"

	chatv1 "github.com/iadev2005/hydrovalley/gen/go/hydrovalley/chat/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func uuidN(n int) string {
	return fmt.Sprintf("00000000-0000-4000-8000-%012d", n)
}

func TestStore_AppendAndSeqMonotonic(t *testing.T) {
	s, err := New("")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	for i := 1; i <= 5; i++ {
		req := &chatv1.SendMessageRequest{
			MessageId: uuidN(i),
			SenderId:  uuidN(100),
			Username:  "ana",
			Text:      fmt.Sprintf("message %d", i),
			Kind:      chatv1.MessageKind_MESSAGE_KIND_PLAYER,
		}
		msg, isNew, err := s.Append(req, timestamppb.Now())
		if err != nil {
			t.Fatalf("Append(%d): %v", i, err)
		}
		if !isNew {
			t.Errorf("Append(%d): expected isNew = true", i)
		}
		if msg.GetSeq() != uint64(i) {
			t.Errorf("Append(%d): got seq %d, want %d", i, msg.GetSeq(), i)
		}
	}

	if s.LastSeq() != 5 {
		t.Errorf("LastSeq: got %d, want 5", s.LastSeq())
	}
	if s.Count() != 5 {
		t.Errorf("Count: got %d, want 5", s.Count())
	}
}

func TestStore_Idempotency(t *testing.T) {
	s, err := New("")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	req := &chatv1.SendMessageRequest{
		MessageId: uuidN(1),
		SenderId:  uuidN(100),
		Username:  "ana",
		Text:      "first message",
		Kind:      chatv1.MessageKind_MESSAGE_KIND_PLAYER,
	}

	msg1, isNew1, err := s.Append(req, nil)
	if err != nil || !isNew1 {
		t.Fatalf("first Append: msg=%v, isNew=%v, err=%v", msg1, isNew1, err)
	}

	// Reintento con el mismo message_id
	msg2, isNew2, err := s.Append(req, nil)
	if err != nil {
		t.Fatalf("second Append: %v", err)
	}
	if isNew2 {
		t.Errorf("second Append: expected isNew = false for duplicate message_id")
	}
	if msg2.GetSeq() != msg1.GetSeq() {
		t.Errorf("second Append: seq changed from %d to %d", msg1.GetSeq(), msg2.GetSeq())
	}
	if s.LastSeq() != 1 {
		t.Errorf("LastSeq: got %d, want 1", s.LastSeq())
	}
	if s.Count() != 1 {
		t.Errorf("Count: got %d, want 1", s.Count())
	}
}

func TestStore_CapacityEviction(t *testing.T) {
	s, err := New("")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	total := 250
	for i := 1; i <= total; i++ {
		req := &chatv1.SendMessageRequest{
			MessageId: uuidN(i),
			SenderId:  uuidN(100),
			Username:  "ana",
			Text:      fmt.Sprintf("msg %d", i),
		}
		if _, _, err := s.Append(req, nil); err != nil {
			t.Fatalf("Append(%d): %v", i, err)
		}
	}

	if s.Count() != Capacity {
		t.Errorf("Count: got %d, want %d", s.Count(), Capacity)
	}
	if s.LastSeq() != uint64(total) {
		t.Errorf("LastSeq: got %d, want %d", s.LastSeq(), total)
	}

	// Mensajes 1 a 50 deben haber sido descartados
	for i := 1; i <= 50; i++ {
		if _, found := s.GetByID(uuidN(i)); found {
			t.Errorf("message %d should have been evicted", i)
		}
	}

	// Mensajes 51 a 250 deben existir en el buffer
	for i := 51; i <= total; i++ {
		if _, found := s.GetByID(uuidN(i)); !found {
			t.Errorf("message %d should be in buffer", i)
		}
	}

	// ListAfter(0, 0) debe devolver los 200 mensajes en orden ascendente
	messages, lastSeq, epoch := s.ListAfter(0, 0)
	if len(messages) != Capacity {
		t.Fatalf("ListAfter: got %d messages, want %d", len(messages), Capacity)
	}
	if lastSeq != uint64(total) {
		t.Errorf("lastSeq: got %d, want %d", lastSeq, total)
	}
	if epoch != s.ServerEpoch() {
		t.Errorf("epoch mismatch")
	}

	expectedSeq := uint64(51)
	for idx, m := range messages {
		if m.GetSeq() != expectedSeq {
			t.Errorf("msg[%d]: got seq %d, want %d", idx, m.GetSeq(), expectedSeq)
		}
		expectedSeq++
	}
}

func TestStore_ListAfterFilters(t *testing.T) {
	s, err := New("")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	for i := 1; i <= 10; i++ {
		req := &chatv1.SendMessageRequest{
			MessageId: uuidN(i),
			Text:      fmt.Sprintf("msg %d", i),
		}
		_, _, _ = s.Append(req, nil)
	}

	// Filtro afterSeq = 7 -> mensajes 8, 9, 10
	msgs, lastSeq, _ := s.ListAfter(7, 0)
	if len(msgs) != 3 {
		t.Fatalf("got %d messages, want 3", len(msgs))
	}
	if msgs[0].GetSeq() != 8 || msgs[2].GetSeq() != 10 {
		t.Errorf("unexpected seq range: %d to %d", msgs[0].GetSeq(), msgs[2].GetSeq())
	}
	if lastSeq != 10 {
		t.Errorf("lastSeq: got %d, want 10", lastSeq)
	}

	// Filtro con limit = 2 -> mensajes 8, 9
	msgsLimit, _, _ := s.ListAfter(7, 2)
	if len(msgsLimit) != 2 {
		t.Fatalf("got %d messages, want 2", len(msgsLimit))
	}
	if msgsLimit[0].GetSeq() != 8 || msgsLimit[1].GetSeq() != 9 {
		t.Errorf("unexpected seq range with limit: %d to %d", msgsLimit[0].GetSeq(), msgsLimit[1].GetSeq())
	}
}

func TestStore_ConcurrentAppends(t *testing.T) {
	s, err := New("")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	concurrency := 20
	messagesPerWorker := 10
	totalMessages := concurrency * messagesPerWorker

	var wg sync.WaitGroup
	wg.Add(concurrency)

	for worker := 0; worker < concurrency; worker++ {
		go func(w int) {
			defer wg.Done()
			for i := 0; i < messagesPerWorker; i++ {
				idNum := w*messagesPerWorker + i + 1
				req := &chatv1.SendMessageRequest{
					MessageId: uuidN(idNum),
					Username:  fmt.Sprintf("user-%d", w),
					Text:      fmt.Sprintf("hello from worker %d msg %d", w, i),
				}
				if _, _, err := s.Append(req, nil); err != nil {
					t.Errorf("concurrent Append: %v", err)
				}
			}
		}(worker)
	}

	wg.Wait()

	if s.LastSeq() != uint64(totalMessages) {
		t.Errorf("LastSeq: got %d, want %d", s.LastSeq(), totalMessages)
	}
	if s.Count() != totalMessages {
		t.Errorf("Count: got %d, want %d", s.Count(), totalMessages)
	}

	// Validar que cada seq desde 1 a totalMessages es único
	msgs, _, _ := s.ListAfter(0, 0)
	seqs := make(map[uint64]bool, len(msgs))
	for _, m := range msgs {
		if seqs[m.GetSeq()] {
			t.Errorf("duplicate seq %d found", m.GetSeq())
		}
		seqs[m.GetSeq()] = true
	}
}

func TestStore_Validation(t *testing.T) {
	s, err := New("")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Invalid message_id
	_, _, err = s.Append(&chatv1.SendMessageRequest{
		MessageId: "not-a-uuid",
		Text:      "hello",
	}, nil)
	if err == nil {
		t.Error("expected error for invalid UUID")
	}

	// Empty text
	_, _, err = s.Append(&chatv1.SendMessageRequest{
		MessageId: uuidN(1),
		Text:      "   ",
	}, nil)
	if err == nil {
		t.Error("expected error for empty text")
	}
}
