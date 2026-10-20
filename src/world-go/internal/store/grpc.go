package store

import (
	"context"
	"log/slog"
	"time"

	persistencev1 "github.com/iadev2005/hydrovalley/gen/go/hydrovalley/persistence/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"github.com/iadev2005/hydrovalley/src/world-go/internal/codec"
	"github.com/iadev2005/hydrovalley/src/world-go/internal/domain"
	"github.com/iadev2005/hydrovalley/src/world-go/internal/requestid"
)

// GRPCStore es el adaptador gRPC de Store.
type GRPCStore struct {
	conn     *grpc.ClientConn
	snapshot persistencev1.PersistenceServiceClient
	catalog  persistencev1.CatalogServiceClient
	policy   CallPolicy
	log      *slog.Logger
}

var _ Store = (*GRPCStore)(nil)

// NewGRPC crea el adaptador para addr (host:puerto). No conecta todavía: el
// canal se abre con la primera llamada y se reconecta solo (regla 5 de
// portabilidad). log puede ser nil. extra se añade a las opciones propias.
func NewGRPC(addr string, policy CallPolicy, log *slog.Logger, extra ...grpc.DialOption) (*GRPCStore, error) {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	opts := []grpc.DialOption{
		// gRPC interno sin cifrar dentro de la red del laboratorio (CLAUDE.md).
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		// Espera de reconexión acotada a 1 s: la persistencia que vuelve tras
		// un reinicio se encuentra enseguida, no a los 120 s por omisión.
		grpc.WithConnectParams(grpc.ConnectParams{
			Backoff: backoff.Config{
				BaseDelay:  100 * time.Millisecond,
				Multiplier: 1.6,
				Jitter:     0.2,
				MaxDelay:   time.Second,
			},
			MinConnectTimeout: policy.Deadline,
		}),
	}
	conn, err := grpc.NewClient(addr, append(opts, extra...)...)
	if err != nil {
		return nil, err
	}
	return &GRPCStore{
		conn:     conn,
		snapshot: persistencev1.NewPersistenceServiceClient(conn),
		catalog:  persistencev1.NewCatalogServiceClient(conn),
		policy:   policy,
		log:      log,
	}, nil
}

// Close cierra el canal.
func (s *GRPCStore) Close() error { return s.conn.Close() }

// call ejecuta attempt con el plazo de la política y como mucho tantos
// reintentos como esperas tenga, solo ante UNAVAILABLE o DEADLINE_EXCEEDED.
//
// RNF-07: cada llamada genera un id (world-<uuid>, el mismo en todos sus
// reintentos) que va en el metadato x-request-id; así se correlaciona con el
// registro de la persistencia. Se registra a nivel debug; un fallo final, a
// warn. Si el contexto de quien llama termina, no se reintenta.
func call[T any](ctx context.Context, s *GRPCStore, method string, attempt func(context.Context) (T, error)) (T, error) {
	id := requestid.NewOutgoing()
	for retries := 0; ; retries++ {
		attemptCtx, cancel := context.WithTimeout(requestid.Tag(ctx, id), s.policy.Deadline)
		result, err := attempt(attemptCtx)
		cancel()
		if err == nil {
			s.log.Debug("llamada a la persistencia", "method", method, "request_id", id)
			return result, nil
		}
		code := status.Code(err)
		if ctx.Err() != nil || !retryable(code) || retries >= len(s.policy.RetryDelays) {
			if retryable(code) {
				s.log.Warn("llamada a la persistencia fallida", "method", method, "request_id", id, "code", code.String())
			}
			return result, err
		}
		delay := s.policy.RetryDelays[retries]
		s.log.Debug("reintento", "method", method, "request_id", id, "code", code.String(), "delay", delay)
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return result, ctx.Err()
		}
	}
}

func toError(err error) error {
	if err == nil {
		return nil
	}
	st := status.Convert(err)
	return newError(st.Code(), st.Message())
}

// GetCatalog implementa Store.
func (s *GRPCStore) GetCatalog(ctx context.Context) (domain.Catalog, error) {
	response, err := call(ctx, s, "GetCatalog", func(ctx context.Context) (*persistencev1.GetCatalogResponse, error) {
		return s.catalog.GetCatalog(ctx, &persistencev1.GetCatalogRequest{})
	})
	if err != nil {
		return domain.Catalog{}, toError(err)
	}
	if response.GetCatalog() == nil {
		return domain.Catalog{}, &Error{Kind: KindFailed, Message: "GetCatalog sin catálogo"}
	}
	catalog, err := codec.CatalogFromProto(response.GetCatalog())
	if err != nil {
		return domain.Catalog{}, &Error{Kind: KindFailed, Message: err.Error()}
	}
	return catalog, nil
}

// Load implementa Store.
func (s *GRPCStore) Load(ctx context.Context) (*persistencev1.Snapshot, error) {
	response, err := call(ctx, s, "LoadSnapshot", func(ctx context.Context) (*persistencev1.LoadSnapshotResponse, error) {
		return s.snapshot.LoadSnapshot(ctx, &persistencev1.LoadSnapshotRequest{})
	})
	if status.Code(err) == codes.NotFound {
		return nil, nil
	}
	if err != nil {
		return nil, toError(err)
	}
	if response.GetSnapshot() == nil {
		return nil, &Error{Kind: KindFailed, Message: "LoadSnapshot sin snapshot"}
	}
	return response.GetSnapshot(), nil
}

// Save implementa Store.
func (s *GRPCStore) Save(ctx context.Context, snapshot *persistencev1.Snapshot) error {
	_, err := call(ctx, s, "SaveSnapshot", func(ctx context.Context) (*persistencev1.SaveSnapshotResponse, error) {
		return s.snapshot.SaveSnapshot(ctx, &persistencev1.SaveSnapshotRequest{Snapshot: snapshot})
	})
	return toError(err)
}
