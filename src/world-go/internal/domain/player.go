package domain

// AppliedAction es un action_id aplicado y el tick en que se aplicó
// (Capa 3).
type AppliedAction struct {
	ActionID string
	Tick     uint64
}

// Purchase cuenta las instalaciones de un tipo que ha comprado un jugador.
type Purchase struct {
	Kind  InstallationKind
	Count uint32
}

// Player es todo lo que el simulador guarda de cada jugador
// (PlayerRecord).
type Player struct {
	PlayerID      string
	Water         uint64
	TankCapacity  uint64
	Leaves        uint64
	WaterRefilled uint64
	WaterSpent    uint64
	LeavesEarned  uint64
	LeavesSpent   uint64
	WaterReduced  uint64
	WaterReused   uint64
	WaterRecycled uint64
	Purchases     []Purchase
	Upgrades      []UpgradeKind
	// LastActionTick es el tick de su última acción aplicada.
	LastActionTick uint64
	// RecentActions son los aplicados dentro de la ventana, en orden de tick.
	RecentActions []AppliedAction
}

// PurchasesOf son las instalaciones de kind que el jugador ha comprado (fija
// el precio de la siguiente).
func (p *Player) PurchasesOf(kind InstallationKind) uint32 {
	for _, purchase := range p.Purchases {
		if purchase.Kind == kind {
			return purchase.Count
		}
	}
	return 0
}

// HasUpgrade dice si el jugador ya compró la mejora.
func (p *Player) HasUpgrade(kind UpgradeKind) bool {
	for _, upgrade := range p.Upgrades {
		if upgrade == kind {
			return true
		}
	}
	return false
}
