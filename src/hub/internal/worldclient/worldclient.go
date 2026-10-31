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
	"errors"
	"strings"
	"sync"
	"time"

	worldv1 "github.com/iadev2005/hydrovalley/gen/go/hydrovalley/world/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

type endpoint struct {
	addr string
	conn *grpc.ClientConn
	api  worldv1.WorldServiceClient
}

// Client llama a WorldService en una o varias direcciones (separadas por
// comas en WORLD_ADDR) con afinidad al nodo activo y conmutación automática
// tras fallos consecutivos (B8 W2).
type Client struct {
	mu          sync.Mutex
	endpoints   []endpoint
	active      int
	consecFails int
}

const failoverAfterFailures = 3

// New crea el cliente para addr (host:puerto o lista separada por comas, p. ej.
// primario:50051,respaldo:50051). No abre la conexión. extra se añade a las
// opciones propias (las pruebas inyectan un marcador con
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
	allOpts := append(opts, extra...)
	var endpoints []endpoint
	seen := make(map[string]struct{})
	for _, part := range strings.Split(addr, ",") {
		target := strings.TrimSpace(part)
		if target == "" {
			continue
		}
		if _, ok := seen[target]; ok {
			continue
		}
		seen[target] = struct{}{}
		conn, err := grpc.NewClient(target, allOpts...)
		if err != nil {
			for _, ep := range endpoints {
				_ = ep.conn.Close()
			}
			return nil, err
		}
		endpoints = append(endpoints, endpoint{
			addr: target,
			conn: conn,
			api:  worldv1.NewWorldServiceClient(conn),
		})
	}
	if len(endpoints) == 0 {
		return nil, errors.New("world address is empty")
	}
	return &Client{endpoints: endpoints}, nil
}

// ActiveAddr devuelve la dirección del simulador actualmente seleccionado.
func (c *Client) ActiveAddr() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.endpoints[c.active].addr
}

// Step llama a WorldService.Step en el simulador activo. El plazo lo pone
// quien llama (200 ms). Tras 3 fallos de transporte/disponibilidad seguidos
// (el umbral del Circuit Breaker), rota al siguiente simulador de la lista.
func (c *Client) Step(ctx context.Context, req *worldv1.StepRequest) (*worldv1.StepResponse, error) {
	c.mu.Lock()
	idx := c.active
	api := c.endpoints[idx].api
	c.mu.Unlock()

	resp, err := api.Step(ctx, req)

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.active != idx {
		return resp, err
	}
	if err == nil {
		c.consecFails = 0
		return resp, nil
	}
	// FAILED_PRECONDITION indica fencing de hub (T3), no caída del simulador.
	if status.Code(err) == codes.FailedPrecondition {
		return nil, err
	}
	c.consecFails++
	if len(c.endpoints) > 1 && c.consecFails >= failoverAfterFailures {
		c.active = (c.active + 1) % len(c.endpoints)
		c.consecFails = 0
	}
	return nil, err
}

// Close cierra todos los canales abiertos.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	var firstErr error
	for _, ep := range c.endpoints {
		if err := ep.conn.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
