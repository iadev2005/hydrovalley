package domain

// Constantes de estructura del simulador.
//
// No son cifras de ajuste del juego (esas viven en catalog/ y llegan en el
// Catalog): son la forma del mundo y del protocolo.
const (
	// ValleyWidth y ValleyHeight son el ancho y el alto del valle en
	// casillas (D-01).
	ValleyWidth  uint32 = 32
	ValleyHeight uint32 = 32

	// RecentActionsWindowTicks son los ticks durante los que se recuerda un
	// action_id aplicado (Capa 3).
	RecentActionsWindowTicks uint64 = 600

	// TicksPerSecond: el tick dura 100 ms (regla 3 de CLAUDE.md). Convierte
	// las cifras del catálogo expresadas en segundos a ticks sin decimales.
	TicksPerSecond uint64 = 10

	// Topes de las cifras de terreno y presión urbana del catálogo: una cifra
	// mayor se rechaza al cargar el catálogo (la persistencia aplica los
	// mismos).
	MaxRiverWidth uint64 = 8
	MaxRockCount  uint64 = 1024
	MaxBuildings  uint64 = 1024
)
