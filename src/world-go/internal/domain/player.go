package domain

// AppliedAction es un action_id aplicado y el tick en que se aplicó
// (Capa 3).
type AppliedAction struct {
	ActionID string
	Tick     uint64
}

// OfflineSummary es lo que hicieron los aspersores de un jugador desde su
// última acción aplicada (RF-25, «resumen al volver»). Las demás
// instalaciones no aportan: el goteo y la planta de tratamiento no hacen
// crecer casillas ni apagan fuegos, y su agua ya va en los contadores de las
// 3R. Se borra con la siguiente acción aplicada del jugador.
type OfflineSummary struct {
	// LeavesEarned son las hojas ganadas por las casillas que crecieron con
	// sus aspersores.
	LeavesEarned uint64
	// WaterReused es la humedad entregada por sus aspersores.
	WaterReused uint64
	// FiresExtinguished son los fuegos que sus aspersores apagaron.
	FiresExtinguished uint64
	// TilesGrown son las casillas que subieron de etapa por sus aspersores.
	TilesGrown uint64
}

// IsEmpty dice si todavía no hay nada que contar.
func (s OfflineSummary) IsEmpty() bool { return s == OfflineSummary{} }

// add suma other con saturación.
func (s *OfflineSummary) add(other OfflineSummary) {
	s.LeavesEarned = satAdd(s.LeavesEarned, other.LeavesEarned)
	s.WaterReused = satAdd(s.WaterReused, other.WaterReused)
	s.FiresExtinguished = satAdd(s.FiresExtinguished, other.FiresExtinguished)
	s.TilesGrown = satAdd(s.TilesGrown, other.TilesGrown)
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
	// OfflineSummary es lo que hicieron sus aspersores desde esa acción.
	OfflineSummary OfflineSummary
	// FiresExtinguished y TilesGrown son el acumulado de la época de lo que
	// hicieron sus aspersores.
	FiresExtinguished uint64
	TilesGrown        uint64
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

// startingPlayer es un jugador recién llegado (o reiniciado): depósito lleno
// y todo a cero. El depósito inicial cuenta como recargado, para que se
// cumpla RF-27: water = water_refilled - water_spent.
func startingPlayer(playerID string, tankCapacity uint64) Player {
	return Player{
		PlayerID:      playerID,
		Water:         tankCapacity,
		TankCapacity:  tankCapacity,
		WaterRefilled: tankCapacity,
	}
}

// recordPurchase suma una compra de kind (fija el precio de la siguiente).
func (p *Player) recordPurchase(kind InstallationKind) {
	for i := range p.Purchases {
		if p.Purchases[i].Kind == kind {
			p.Purchases[i].Count = satAdd32(p.Purchases[i].Count, 1)
			return
		}
	}
	p.Purchases = append(p.Purchases, Purchase{Kind: kind, Count: 1})
}

// spendLeaves gasta hojas, sin bajar de 0 (como Rust, saturando); lo gastado
// suma a LeavesSpent. El llamador ya comprobó que alcanzan.
func (p *Player) spendLeaves(amount uint64) {
	p.Leaves = satSub(p.Leaves, amount)
	p.LeavesSpent = satAdd(p.LeavesSpent, amount)
}
