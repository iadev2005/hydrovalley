package transport

import (
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"testing"

	chatv1 "github.com/iadev2005/hydrovalley/gen/go/hydrovalley/chat/v1"
	"github.com/iadev2005/hydrovalley/src/chat/internal/store"
)

// publishFunc abstrae el camino de publicación para poder reproducir el
// comportamiento anterior (Append y luego Broadcast sin candado común).
type publishFunc func(h *Hub, st *store.Store, req *chatv1.SendMessageRequest)

func publishViaHub(h *Hub, st *store.Store, req *chatv1.SendMessageRequest) {
	_, _, _ = h.Publish(func() (*chatv1.ChatMessage, bool, error) {
		return st.Append(req, nil)
	})
}

func seqOf(t *testing.T, data []byte) uint64 {
	t.Helper()
	var f struct {
		Seq string `json:"seq"`
	}
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("unmarshal frame: %v", err)
	}
	n, err := strconv.ParseUint(f.Seq, 10, 64)
	if err != nil {
		t.Fatalf("parse seq %q: %v", f.Seq, err)
	}
	return n
}

func runConcurrentPublish(t *testing.T, publish publishFunc) {
	t.Helper()
	const (
		senders     = 8
		perSender   = 125 // 1000 mensajes en total
		total       = senders * perSender
		subscribers = 4
	)

	st, err := store.New("")
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	h := NewHub(st.ServerEpoch())
	h.bufferSize = total // el test mide orden, no la expulsión de lentos

	// Cada suscriptor tiene un lector propio que drena su canal, como writeLoop.
	got := make([][]uint64, subscribers)
	var readers sync.WaitGroup
	subs := make([]*Subscriber, subscribers)
	for i := range subs {
		subs[i] = h.Subscribe()
		readers.Add(1)
		go func(i int) {
			defer readers.Done()
			for data := range subs[i].send {
				got[i] = append(got[i], seqOf(t, data))
			}
		}(i)
	}

	var senderWG sync.WaitGroup
	for s := 0; s < senders; s++ {
		senderWG.Add(1)
		go func(s int) {
			defer senderWG.Done()
			for m := 0; m < perSender; m++ {
				publish(h, st, &chatv1.SendMessageRequest{
					MessageId: fmt.Sprintf("00000000-0000-4000-8000-%012d", s*perSender+m+1),
					SenderId:  fmt.Sprintf("00000000-0000-4000-9000-%012d", s+1),
					Username:  fmt.Sprintf("p%d", s),
					Text:      "hola",
					Kind:      chatv1.MessageKind_MESSAGE_KIND_PLAYER,
				})
			}
		}(s)
	}
	senderWG.Wait()

	for _, sub := range subs {
		h.Unsubscribe(sub)
	}
	readers.Wait()

	for i, seqs := range got {
		if len(seqs) != total {
			t.Fatalf("subscriber %d received %d messages, want %d", i, len(seqs), total)
		}
		for j := 1; j < len(seqs); j++ {
			if seqs[j] <= seqs[j-1] {
				t.Fatalf("subscriber %d: seq %d arrived after %d (position %d)", i, seqs[j], seqs[j-1], j)
			}
		}
	}
}

func TestHub_Publish_DeliversInSeqOrderToEverySubscriber(t *testing.T) {
	runConcurrentPublish(t, publishViaHub)
}

func TestHub_SlowSubscriberIsEvictedWithoutBlockingOthers(t *testing.T) {
	h := NewHub("00000000-0000-4000-8000-000000000000")
	slow := h.Subscribe() // nadie lee su canal
	h.bufferSize = 1000   // el rápido tiene holgura para toda la ráfaga
	fast := h.Subscribe()

	var received int
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range fast.send {
			received++
		}
	}()

	const total = 1000
	for i := 0; i < total; i++ {
		h.BroadcastRaw([]byte(`{"seq":"1"}`))
	}

	if h.SubscribersCount() != 1 {
		t.Fatalf("slow subscriber must be evicted, subscribers = %d", h.SubscribersCount())
	}

	// El canal del lento queda cerrado tras vaciar lo ya encolado.
	drained := 0
	for range slow.send {
		drained++
	}
	if drained != cap(slow.send) {
		t.Errorf("slow subscriber drained %d queued frames, want %d", drained, cap(slow.send))
	}

	// Un Unsubscribe posterior (defer del handler) no debe provocar pánico.
	h.Unsubscribe(slow)

	h.Unsubscribe(fast)
	wg.Wait()
	if received != total {
		t.Errorf("fast subscriber received %d frames, want %d", received, total)
	}
}
