// Package worldclient es el adaptador gRPC del puerto hub.WorldClient.
//
// El canal se crea sin conectar (grpc.NewClient): el hub arranca aunque el
// simulador no esté (regla de portabilidad 5) y se conecta con la primera
// llamada. Step nunca espera a que el canal esté listo (sin WaitForReady):
// si el simulador no responde, falla dentro de su plazo y el hub reencola.
// El transporte no reintenta Step (no hay política de reintentos en la
// configuración del servicio).
package worldclient

import (
	"context"
	"time"

	worldv1 "github.com/iadev2005/hydrovalley/gen/go/hydrovalley/world/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/credentials/insecure"
)

// Client llama a WorldService.
type Client struct {
	conn *grpc.ClientConn
	api  worldv1.WorldServiceClient
}

// New crea el cliente para addr (host:puerto). No abre la conexión. extra
// se añade a las opciones propias (las pruebas inyectan un marcador con
// grpc.WithContextDialer).
func New(addr string, extra ...grpc.DialOption) (*Client, error) {
	opts := []grpc.DialOption{
		// gRPC interno sin cifrar dentro de la red del laboratorio (CLAUDE.md).
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		// Espera de reconexión acotada a 1 s, la cadencia de prueba del
		// Circuit Breaker; la de gRPC por defecto llega a 120 s y retrasaría
		// la vuelta tras un reinicio de Rust.
		grpc.WithConnectParams(grpc.ConnectParams{
			Backoff: backoff.Config{
				BaseDelay:  100 * time.Millisecond,
				Multiplier: 1.6,
				Jitter:     0.2,
				MaxDelay:   time.Second,
			},
			MinConnectTimeout: time.Second,
		}),
	}
	conn, err := grpc.NewClient(addr, append(opts, extra...)...)
	if err != nil {
		return nil, err
	}
	return &Client{conn: conn, api: worldv1.NewWorldServiceClient(conn)}, nil
}

// Step llama a WorldService.Step. El plazo lo pone quien llama (200 ms).
func (c *Client) Step(ctx context.Context, req *worldv1.StepRequest) (*worldv1.StepResponse, error) {
	return c.api.Step(ctx, req)
}

// Close cierra el canal.
func (c *Client) Close() error { return c.conn.Close() }
