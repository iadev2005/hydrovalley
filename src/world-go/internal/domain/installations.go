package domain

// Instalaciones de las 3R y mejoras (T3). Port de world.rs:402-577.

// placeInstallation compra y coloca una instalación (world.rs:402; RF-04,
// RF-07). Se comprueba en este orden, y un rechazo no cobra nada ni da de
// alta al jugador: casilla dentro del valle, casilla libre (una instalación
// por casilla, de cualquier jugador), mundo con sitio para un jugador nuevo y
// hojas suficientes para el precio del catálogo con el aumento por compras.
func (w *World) placeInstallation(playerID string, kind InstallationKind, x, y uint32) CommandOutcome {
	index, inside := w.tileIndex(x, y)
	if !inside {
		return RejectedOutcome(OutOfBounds)
	}
	if w.tiles[index].Installation != nil {
		return RejectedOutcome(TileOccupied)
	}
	var purchased uint32
	var leaves uint64
	slot, known := w.players[playerID]
	if known {
		purchased, leaves = slot.record.PurchasesOf(kind), slot.record.Leaves
	}
	if !known && len(w.players) >= MaxPlayers {
		return RejectedOutcome(WorldFull)
	}
	price := w.catalog.InstallationPrice(kind, purchased)
	if leaves < price {
		return RejectedOutcome(InsufficientLeaves)
	}
	player := &w.playerSlot(playerID).record
	player.spendLeaves(price)
	player.recordPurchase(kind)
	w.tiles[index].Installation = &Installation{Kind: kind, OwnerID: playerID}
	return AppliedOutcome
}

// buyUpgrade compra una mejora, que es única por jugador (world.rs:437;
// RF-07): ya comprada, mundo lleno para un jugador nuevo y hojas
// insuficientes, en ese orden.
func (w *World) buyUpgrade(playerID string, kind UpgradeKind) CommandOutcome {
	var leaves uint64
	slot, known := w.players[playerID]
	if known {
		if slot.record.HasUpgrade(kind) {
			return RejectedOutcome(UpgradeAlreadyOwned)
		}
		leaves = slot.record.Leaves
	}
	if !known && len(w.players) >= MaxPlayers {
		return RejectedOutcome(WorldFull)
	}
	price := w.catalog.UpgradePrice(kind)
	if leaves < price {
		return RejectedOutcome(InsufficientLeaves)
	}
	extraCapacity := w.catalog.BiggerTank.ExtraCapacity
	player := &w.playerSlot(playerID).record
	player.spendLeaves(price)
	player.Upgrades = append(player.Upgrades, kind)
	if kind == BiggerTank {
		player.TankCapacity = satAdd(player.TankCapacity, extraCapacity)
	}
	return AppliedOutcome
}

// NextPrice es el precio de la siguiente instalación de kind para player
// (world.rs:765; RF-07): el del catálogo con el aumento por las que ya
// compró del mismo tipo.
func (w *World) NextPrice(player *Player, kind InstallationKind) uint64 {
	return w.catalog.InstallationPrice(kind, player.PurchasesOf(kind))
}

// ownerIsActive dice si trabajan las instalaciones de owner en el tick tick
// (world.rs:481): sí mientras no hayan pasado OfflineInstallationSeconds
// (7.200 s = 72.000 ticks) desde su última acción aplicada, ese límite
// incluido (RF-25). Una acción nueva del dueño las reactiva. Un dueño que no
// existe no las activa.
func (w *World) ownerIsActive(owner string, tick uint64) bool {
	window := satMul(w.catalog.OfflineInstallationSeconds, TicksPerSecond)
	slot, known := w.players[owner]
	return known && satSub(tick, slot.record.LastActionTick) <= window
}

// placedInstallation es una instalación que trabaja: su casilla (índice por
// filas) y su dueño.
type placedInstallation struct {
	index int
	owner string
}

// activeInstallations son las instalaciones de kind que trabajan en tick, en
// orden de casilla (fila a fila) (world.rs:493).
func (w *World) activeInstallations(kind InstallationKind, tick uint64) []placedInstallation {
	var active []placedInstallation
	for index := range w.tiles {
		installation := w.tiles[index].Installation
		if installation != nil && installation.Kind == kind && w.ownerIsActive(installation.OwnerID, tick) {
			active = append(active, placedInstallation{index: index, owner: installation.OwnerID})
		}
	}
	return active
}

// runTreatmentPlants es la planta de tratamiento (Reciclar) (world.rs:509):
// cada TreatmentPlant.IntervalSeconds suma TreatmentPlant.RefillAmount litros
// extra al depósito de su dueño, sin pasar de la capacidad. Solo lo que cabe
// cuenta como recargado (RF-27) y como WaterRecycled.
func (w *World) runTreatmentPlants(tick uint64) {
	rules := w.catalog.TreatmentPlant
	interval := satMul(uint64(rules.IntervalSeconds), TicksPerSecond)
	if !isMultiple(tick, interval) {
		return
	}
	for _, plant := range w.activeInstallations(TreatmentPlant, tick) {
		player := &w.playerSlot(plant.owner).record
		added := minU64(rules.RefillAmount, satSub(player.TankCapacity, player.Water))
		player.Water = satAdd(player.Water, added)
		player.WaterRefilled = satAdd(player.WaterRefilled, added)
		player.WaterRecycled = satAdd(player.WaterRecycled, added)
	}
}

// runSprinklers es el aspersor de agua gris (Reutilizar) (world.rs:537): cada
// Sprinkler.IntervalSeconds suma Sprinkler.Humidity a cada casilla de su
// cuadrado de Sprinkler.AreaSide casillas de lado (recortado por los bordes).
// No gasta agua del depósito. Si una casilla crece, las hojas son del dueño
// del aspersor. Su WaterReused suma la humedad entregada (humedad x casillas
// del valle alcanzadas). Sigue actuando en modo demo y no toca
// humidityContributed, que es solo de las gotas.
//
// Al regar apaga los fuegos de su zona (D-01). Es un apagado sin bono: el
// bono es de quien suelta la tercera gota, y el aspersor no cuenta gotas. Si
// el fuego tenía crecimiento pendiente, se resuelve ahí y las hojas son del
// dueño, como con cualquier riego.
func (w *World) runSprinklers(tick uint64) {
	rules := w.catalog.Sprinkler
	interval := satMul(uint64(rules.IntervalSeconds), TicksPerSecond)
	if !isMultiple(tick, interval) {
		return
	}
	for _, sprinkler := range w.activeInstallations(Sprinkler, tick) {
		x, y := w.tileXY(sprinkler.index)
		area := areaIndices(x, y, rules.AreaSide)
		var leaves uint64
		for _, index := range area {
			tile := &w.tiles[index]
			leaves = satAdd(leaves, AddHumidity(tile, rules.Humidity, w.catalog.Growth))
			if Extinguish(tile) {
				leaves = satAdd(leaves, AddHumidity(tile, 0, w.catalog.Growth))
			}
		}
		player := &w.playerSlot(sprinkler.owner).record
		player.Leaves = satAdd(player.Leaves, leaves)
		player.LeavesEarned = satAdd(player.LeavesEarned, leaves)
		player.WaterReused = satAdd(player.WaterReused, satMul(rules.Humidity, uint64(len(area))))
	}
}

// evaporateValley es la evaporación del segundo, con el goteo (Reducir)
// (world.rs:579): en el cuadrado de Drip.AreaSide casillas de lado de un
// riego por goteo la casilla pierde HumidityPerSecond / EvaporationDivisor
// (división entera, hacia abajo) en vez de HumidityPerSecond. Varios goteos
// que cubren la misma casilla no se acumulan: el divisor se aplica una sola
// vez y el ahorro se acredita al primero en orden de casilla. WaterReduced
// suma la humedad que la casilla habría perdido de más sin goteo:
// min(humedad, evaporación) - min(humedad, evaporación reducida). Una casilla
// ardiendo se evapora (y ahorra) como cualquier otra: el fuego congela la
// etapa, no la humedad.
func (w *World) evaporateValley(tick uint64) {
	amount := w.catalog.Evaporation.HumidityPerSecond
	reduced := amount / max(uint64(w.catalog.Drip.EvaporationDivisor), 1)
	drips := w.activeInstallations(Drip, tick)
	side := w.catalog.Drip.AreaSide
	// Para cada casilla, qué goteo la cubre (el primero en orden); -1 si
	// ninguno.
	cover := make([]int, len(w.tiles))
	for i := range cover {
		cover[i] = -1
	}
	for n, drip := range drips {
		x, y := w.tileXY(drip.index)
		for _, index := range areaIndices(x, y, side) {
			if cover[index] < 0 {
				cover[index] = n
			}
		}
	}
	saved := make([]uint64, len(drips))
	for i := range w.tiles {
		tile := &w.tiles[i]
		n := cover[i]
		if n < 0 {
			Evaporate(tile, amount)
			continue
		}
		h := tile.Humidity
		saved[n] = satAdd(saved[n], minU64(h, amount)-minU64(h, reduced))
		Evaporate(tile, reduced)
	}
	for n, drip := range drips {
		player := &w.playerSlot(drip.owner).record
		player.WaterReduced = satAdd(player.WaterReduced, saved[n])
	}
}

// tileXY son las coordenadas de la casilla de índice index (por filas)
// (world.rs:712).
func (w *World) tileXY(index int) (x, y uint32) {
	width := int(ValleyWidth)
	return uint32(index % width), uint32(index / width)
}
