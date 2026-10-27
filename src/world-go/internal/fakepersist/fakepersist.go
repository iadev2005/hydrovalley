// Package fakepersist es una persistencia falsa para las pruebas: sirve
// PersistenceService y CatalogService con gRPC de verdad, en un puerto
// efímero del propio proceso de la prueba. Es el equivalente de los servidores
// falsos de src/world/tests (grpc_server.rs y persistence_store.rs).
//
// Solo lo importan pruebas; no forma parte del binario.
package fakepersist

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	persistencev1 "github.com/iadev2005/hydrovalley/gen/go/hydrovalley/persistence/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// CatalogVersion es la versión del catálogo falso.
const CatalogVersion = "test-catalog-v1"

// Catalog es un catálogo válido con las cifras de arranque de D-01 (la
// ignición en 0 ppm). Solo para las pruebas: en producción lo entrega
// CatalogService.
func Catalog() *persistencev1.Catalog {
	return &persistencev1.Catalog{
		CatalogVersion: CatalogVersion,
		Drop:           &persistencev1.DropRules{WaterCost: 1, Humidity: 25, DemoHumidity: 1},
		Tank:           &persistencev1.TankRules{Capacity: 20, RefillAmount: 1, RefillIntervalSeconds: 2},
		Growth: &persistencev1.GrowthRules{
			HumidityPerStage: 100, LeavesGrass: 1, LeavesShrub: 2, LeavesFlowers: 4, LeavesTree: 8, TreeMaxHumidity: 100,
		},
		Evaporation: &persistencev1.EvaporationRules{HumidityPerSecond: 2, WitherAfterTicks: 100},
		Fire: &persistencev1.FireRules{
			DropsToExtinguish: 3, ExtinguishBonusLeaves: 5, BurnOutTicks: 300, IgnitionChancePpm: 0,
		},
		Sprinkler:      &persistencev1.SprinklerRules{Price: 10, AreaSide: 3, Humidity: 25, IntervalSeconds: 2, Durability: 30},
		Drip:           &persistencev1.DripRules{Price: 25, AreaSide: 5, EvaporationDivisor: 2, Durability: 30, WearIntervalSeconds: 2},
		TreatmentPlant: &persistencev1.TreatmentPlantRules{Price: 40, RefillAmount: 1, IntervalSeconds: 2, Durability: 30},
		TrashBin:       &persistencev1.TrashBinRules{Price: 15, AreaSide: 3, IntervalSeconds: 2, Durability: 30},
		Terrain:        &persistencev1.TerrainRules{},
		Urban: &persistencev1.UrbanRules{
			BuildingTreeThreshold: 2, BuildingHumidityThreshold: 500, MaxBuildings: 2,
			TrashIntervalSeconds: 5, TrashSpreadIntervalSeconds: 10, CleanRewardLeaves: 2,
		},
		InstallationPriceIncreasePercent: 15,
		BiggerDrop:                       &persistencev1.BiggerDropRules{Price: 30, AreaSide: 3},
		BiggerTank:                       &persistencev1.BiggerTankRules{Price: 20, ExtraCapacity: 10},
		OfflineInstallationSeconds:       7200,
	}
}

// Step es una respuesta programada: se consume una por llamada, de cualquier
// método. Agotado el guion, las llamadas siguen el comportamiento normal.
type Step struct {
	// Delay espera antes de responder (si Code es OK, responde con éxito
	// después de esa demora).
	Delay time.Duration
	// Code distinto de OK hace fallar la llamada con ese código.
	Code codes.Code
}

// Call es una llamada recibida.
type Call struct {
	Method string
	// RequestID es el metadato x-request-id; vacío si no vino.
	RequestID string
}

// Fake es la persistencia falsa. Todos sus métodos son seguros entre
// goroutines.
type Fake struct {
	persistencev1.UnimplementedPersistenceServiceServer
	persistencev1.UnimplementedCatalogServiceServer

	mu                 sync.Mutex
	script             []Step
	loadSnapshot       *persistencev1.Snapshot
	loadUnavailable    bool
	catalogUnavailable bool
	catalog            *persistencev1.Catalog
	saveCode           codes.Code
	calls              []Call
	saved              []*persistencev1.Snapshot
}

// New crea una persistencia sin instantáneas (LoadSnapshot da NOT_FOUND) y con
// el catálogo de Catalog.
func New() *Fake { return &Fake{catalog: Catalog()} }

// Script programa las próximas respuestas, una por llamada.
func (f *Fake) Script(steps ...Step) *Fake {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.script = append([]Step(nil), steps...)
	return f
}

// SetSnapshot hace que LoadSnapshot devuelva snapshot (nil: NOT_FOUND).
func (f *Fake) SetSnapshot(snapshot *persistencev1.Snapshot) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.loadSnapshot = snapshot
}

// SetLoadUnavailable hace que LoadSnapshot responda UNAVAILABLE.
func (f *Fake) SetLoadUnavailable(unavailable bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.loadUnavailable = unavailable
}

// SetCatalogUnavailable hace que GetCatalog responda UNAVAILABLE.
func (f *Fake) SetCatalogUnavailable(unavailable bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.catalogUnavailable = unavailable
}

// SetCatalog cambia lo que devuelve GetCatalog.
func (f *Fake) SetCatalog(catalog *persistencev1.Catalog) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.catalog = catalog
}

// SetSaveCode hace que SaveSnapshot falle con code (OK: guarda).
func (f *Fake) SetSaveCode(code codes.Code) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saveCode = code
}

// Count es el número de llamadas recibidas de method.
func (f *Fake) Count(method string) int { return len(f.Calls(method)) }

// Calls son las llamadas recibidas de method, en orden.
func (f *Fake) Calls(method string) []Call {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []Call
	for _, call := range f.calls {
		if call.Method == method {
			out = append(out, call)
		}
	}
	return out
}

// Saved son las instantáneas recibidas por SaveSnapshot.
func (f *Fake) Saved() []*persistencev1.Snapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*persistencev1.Snapshot(nil), f.saved...)
}

// SavedTicks son los ticks de las instantáneas recibidas, en orden.
func (f *Fake) SavedTicks() []uint64 {
	var ticks []uint64
	for _, snapshot := range f.Saved() {
		ticks = append(ticks, snapshot.GetTick())
	}
	return ticks
}

// begin registra la llamada y consume el siguiente paso del guion.
func (f *Fake) begin(ctx context.Context, method string) error {
	call := Call{Method: method}
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if ids := md.Get("x-request-id"); len(ids) > 0 {
			call.RequestID = ids[0]
		}
	}
	f.mu.Lock()
	f.calls = append(f.calls, call)
	var step *Step
	if len(f.script) > 0 {
		step = &f.script[0]
		f.script = f.script[1:]
	}
	f.mu.Unlock()
	if step == nil {
		return nil
	}
	if step.Delay > 0 {
		select {
		case <-time.After(step.Delay):
		case <-ctx.Done():
			return status.FromContextError(ctx.Err()).Err()
		}
	}
	if step.Code != codes.OK {
		return status.Error(step.Code, "fallo programado")
	}
	return nil
}

// SaveSnapshot implementa PersistenceService.
func (f *Fake) SaveSnapshot(ctx context.Context, request *persistencev1.SaveSnapshotRequest) (*persistencev1.SaveSnapshotResponse, error) {
	if err := f.begin(ctx, "SaveSnapshot"); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.saveCode != codes.OK {
		return nil, status.Error(f.saveCode, "fallo de guardado")
	}
	if request.GetSnapshot() == nil {
		return nil, status.Error(codes.InvalidArgument, "falta snapshot")
	}
	f.saved = append(f.saved, request.GetSnapshot())
	return &persistencev1.SaveSnapshotResponse{}, nil
}

// LoadSnapshot implementa PersistenceService.
func (f *Fake) LoadSnapshot(ctx context.Context, _ *persistencev1.LoadSnapshotRequest) (*persistencev1.LoadSnapshotResponse, error) {
	if err := f.begin(ctx, "LoadSnapshot"); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case f.loadUnavailable:
		return nil, status.Error(codes.Unavailable, "sin base de datos")
	case f.loadSnapshot == nil:
		return nil, status.Error(codes.NotFound, "no hay ninguna instantánea")
	default:
		return &persistencev1.LoadSnapshotResponse{Snapshot: f.loadSnapshot}, nil
	}
}

// GetCatalog implementa CatalogService.
func (f *Fake) GetCatalog(ctx context.Context, _ *persistencev1.GetCatalogRequest) (*persistencev1.GetCatalogResponse, error) {
	if err := f.begin(ctx, "GetCatalog"); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.catalogUnavailable {
		return nil, status.Error(codes.Unavailable, "sin base de datos")
	}
	return &persistencev1.GetCatalogResponse{Catalog: f.catalog}, nil
}

// Serve levanta f en un puerto efímero y devuelve su dirección. Se detiene al
// terminar la prueba.
func Serve(t testing.TB, f *Fake) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	persistencev1.RegisterPersistenceServiceServer(server, f)
	persistencev1.RegisterCatalogServiceServer(server, f)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	return listener.Addr().String()
}
