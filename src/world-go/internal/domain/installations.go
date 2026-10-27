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
	if w.tiles[index].Installation != nil || w.tiles[index].Stage.IsNatural() || w.tiles[index].HasBuilding || w.tiles[index].TrashLevel > 0 {
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
	w.tiles[index].Installation = &Installation{
		Kind:                kind,
		OwnerID:             playerID,
		RemainingDurability: w.catalog.InstallationBaseDurability(kind),
	}
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
// Además emite un pulso periódico 3x3 que sube +1 etapa vegetal a las
// casillas vecinas y acredita las hojas al propietario.
func (w *World) runTreatmentPlants(tick uint64) {
	rules := w.catalog.TreatmentPlant
	interval := satMul(uint64(rules.IntervalSeconds), TicksPerSecond)
	if !isMultiple(tick, interval) {
		return
	}
	for _, plant := range w.activeInstallations(TreatmentPlant, tick) {
		x, y := w.tileXY(plant.index)
		area := areaIndices(x, y, 3, w.width, w.height)
		var leavesEarned uint64
		var tilesGrown uint64
		for _, neighborIdx := range area {
			if neighborIdx == plant.index {
				continue
			}
			tile := &w.tiles[neighborIdx]
			if tile.Fire != nil || tile.Stage.IsNatural() {
				continue
			}
			if next, ok := tile.Stage.Next(); ok {
				tile.Stage = next
				tile.DryTicks = 0
				if tile.Stage == Tree {
					tile.Humidity = minU64(tile.Humidity, w.catalog.Growth.TreeMaxHumidity)
				}
				earned := w.catalog.Growth.LeavesFor(next)
				leavesEarned = satAdd(leavesEarned, earned)
				tilesGrown = satAdd(tilesGrown, 1)
			}
		}

		player := &w.playerSlot(plant.owner).record
		added := minU64(rules.RefillAmount, satSub(player.TankCapacity, player.Water))
		player.Water = satAdd(player.Water, added)
		player.WaterRefilled = satAdd(player.WaterRefilled, added)
		player.WaterRecycled = satAdd(player.WaterRecycled, added)

		player.Leaves = satAdd(player.Leaves, leavesEarned)
		player.LeavesEarned = satAdd(player.LeavesEarned, leavesEarned)
		player.TilesGrown = satAdd(player.TilesGrown, tilesGrown)

		if inst := w.tiles[plant.index].Installation; inst != nil {
			if inst.RemainingDurability > 0 {
				inst.RemainingDurability--
			}
			if inst.RemainingDurability == 0 {
				w.tiles[plant.index].Installation = nil
			}
		}
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
//
// Lo que hace cada aspersor se suma además al resumen al volver de su dueño
// (RF-25) y a sus acumulados de la época: una casilla cuenta una vez por
// riego aunque sume en dos fases (el riego y el apagado).
func (w *World) runSprinklers(tick uint64) {
	rules := w.catalog.Sprinkler
	interval := satMul(uint64(rules.IntervalSeconds), TicksPerSecond)
	if !isMultiple(tick, interval) {
		return
	}
	for _, sprinkler := range w.activeInstallations(Sprinkler, tick) {
		x, y := w.tileXY(sprinkler.index)
		area := areaIndices(x, y, rules.AreaSide, w.width, w.height)
		var effect OfflineSummary
		for _, index := range area {
			tile := &w.tiles[index]
			if tile.Stage.IsNatural() {
				continue
			}
			stageBefore := tile.Stage
			effect.LeavesEarned = satAdd(effect.LeavesEarned, AddHumidity(tile, rules.Humidity, w.catalog.Growth))
			if Extinguish(tile) {
				effect.FiresExtinguished = satAdd(effect.FiresExtinguished, 1)
				// El crecimiento que quedó pendiente se resuelve ahora.
				effect.LeavesEarned = satAdd(effect.LeavesEarned, AddHumidity(tile, 0, w.catalog.Growth))
			}
			if tile.Stage != stageBefore {
				effect.TilesGrown = satAdd(effect.TilesGrown, 1)
			}
		}
		effect.WaterReused = satMul(rules.Humidity, uint64(len(area)))
		player := &w.playerSlot(sprinkler.owner).record
		player.Leaves = satAdd(player.Leaves, effect.LeavesEarned)
		player.LeavesEarned = satAdd(player.LeavesEarned, effect.LeavesEarned)
		player.WaterReused = satAdd(player.WaterReused, effect.WaterReused)
		player.FiresExtinguished = satAdd(player.FiresExtinguished, effect.FiresExtinguished)
		player.TilesGrown = satAdd(player.TilesGrown, effect.TilesGrown)
		player.OfflineSummary.add(effect)

		if inst := w.tiles[sprinkler.index].Installation; inst != nil {
			if inst.RemainingDurability > 0 {
				inst.RemainingDurability--
			}
			if inst.RemainingDurability == 0 {
				w.tiles[sprinkler.index].Installation = nil
			}
		}
	}
}

// runDrips descuenta durabilidad al goteo activo cada 2 segundos (20 ticks).
// Al llegar a cero se destruye y desaparece.
func (w *World) runDrips(tick uint64) {
	interval := satMul(2, TicksPerSecond)
	if !isMultiple(tick, interval) {
		return
	}
	for _, drip := range w.activeInstallations(Drip, tick) {
		if inst := w.tiles[drip.index].Installation; inst != nil {
			if inst.RemainingDurability > 0 {
				inst.RemainingDurability--
			}
			if inst.RemainingDurability == 0 {
				w.tiles[drip.index].Installation = nil
			}
		}
	}
}

// runTrashBins es el Cesto de Basura / Reciclaje (Fase 4): cada 2 segundos (20 ticks) reduce 1 nivel
// de basura en su radio 3 x 3 y descuenta 1 punto de su durabilidad.
func (w *World) runTrashBins(tick uint64) {
	rules := w.catalog.TrashBin
	interval := satMul(uint64(rules.IntervalSeconds), TicksPerSecond)
	if !isMultiple(tick, interval) {
		return
	}
	side := rules.AreaSide
	for _, bin := range w.activeInstallations(TrashBin, tick) {
		x, y := w.tileXY(bin.index)
		for _, tileIdx := range areaIndices(x, y, side, w.width, w.height) {
			tile := &w.tiles[tileIdx]
			if tile.TrashLevel > 0 {
				tile.TrashLevel--
				if tile.TrashLevel == 0 {
					tile.Stage = DrySoil
				}
			}
		}
		if inst := w.tiles[bin.index].Installation; inst != nil {
			if inst.RemainingDurability > 0 {
				inst.RemainingDurability--
			}
			if inst.RemainingDurability == 0 {
				w.tiles[bin.index].Installation = nil
			}
		}
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
func (w *World) evaporateValley(tick uint64, shoreline []bool) {
	amount := w.catalog.Evaporation.HumidityPerSecond
	reduced := amount / max(uint64(w.catalog.Drip.EvaporationDivisor), 1)
	shorelineAmount := amount / 2
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
		for _, index := range areaIndices(x, y, side, w.width, w.height) {
			if cover[index] < 0 {
				cover[index] = n
			}
		}
	}
	saved := make([]uint64, len(drips))
	for i := range w.tiles {
		tile := &w.tiles[i]
		if tile.Stage.IsNatural() {
			continue
		}
		baseEvap := amount
		if shoreline[i] {
			baseEvap = shorelineAmount
		}
		n := cover[i]
		if n < 0 {
			Evaporate(tile, baseEvap)
			continue
		}
		effectiveReduced := reduced
		if shoreline[i] {
			effectiveReduced = minU64(reduced, shorelineAmount)
		}
		h := tile.Humidity
		saved[n] = satAdd(saved[n], minU64(h, baseEvap)-minU64(h, effectiveReduced))
		Evaporate(tile, effectiveReduced)
	}
	for n, drip := range drips {
		player := &w.playerSlot(drip.owner).record
		player.WaterReduced = satAdd(player.WaterReduced, saved[n])
	}
}

// tileXY son las coordenadas de la casilla de índice index (por filas)
// (world.rs:712).
func (w *World) tileXY(index int) (x, y uint32) {
	width := int(w.width)
	return uint32(index % width), uint32(index / width)
}
