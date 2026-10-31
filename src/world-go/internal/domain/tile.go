package domain

// TileStage es la etapa de una casilla (diagrama 05).
type TileStage uint8

// Las etapas, en orden de crecimiento. DrySoil es el valor cero: una casilla
// nueva es tierra seca.
const (
	DrySoil TileStage = iota
	Grass
	Shrub
	Flowers
	Tree
	Water
	Rock
)

// Next es la etapa a la que sube la casilla al acumular la humedad
// necesaria; ok es false en el árbol, agua y roca.
func (s TileStage) Next() (next TileStage, ok bool) {
	if s >= Tree {
		return s, false
	}
	return s + 1, true
}

// Previous es la etapa a la que baja al marchitarse; ok es false en tierra
// seca, agua y roca.
func (s TileStage) Previous() (previous TileStage, ok bool) {
	if s == DrySoil || s == Water || s == Rock {
		return s, false
	}
	return s - 1, true
}

// IsNatural dice si la casilla es un elemento de terreno natural (agua o roca).
func (s TileStage) IsNatural() bool {
	return s == Water || s == Rock
}

// InstallationKind es el tipo de instalación de las 3R.
type InstallationKind uint8

// Los tipos de instalación.
const (
	Sprinkler InstallationKind = iota
	Drip
	TreatmentPlant
	TrashBin
)

// UpgradeKind es el tipo de mejora (compra única por jugador).
type UpgradeKind uint8

// Los tipos de mejora.
const (
	BiggerDrop UpgradeKind = iota
	BiggerTank
)

// Fire es un incendio en curso: gotas recibidas desde que arde y ticks
// ardiendo.
type Fire struct {
	DropsReceived uint32
	BurningTicks  uint32
}

// Installation es una instalación colocada en una casilla.
type Installation struct {
	Kind                InstallationKind
	OwnerID             string
	RemainingDurability uint32
}

// IsBlocked dice si la casilla no admite plantas ni humedad: terreno natural,
// edificio o basura encima.
func (t *Tile) IsBlocked() bool {
	return t.Stage.IsNatural() || t.HasBuilding || t.TrashLevel > 0
}

// Tile es una casilla del valle. El valor cero es tierra seca sin humedad,
// sin fuego y sin instalación. Fire e Installation son nil cuando no hay.
type Tile struct {
	Stage        TileStage
	Humidity     uint64
	DryTicks     uint32
	Fire         *Fire
	Installation *Installation
	HasBuilding  bool
	TrashLevel   uint32
}
