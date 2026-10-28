package domain

import (
	"slices"
	"sort"
)

// SnapshotIntervalTicks es cada cuántos ticks se toma una instantánea
// (Capa 5).
const SnapshotIntervalTicks uint64 = 50

// MaxPlayers son los jugadores que puede tener el mundo como mucho. Acota la
// memoria del simulador y el tamaño de la instantánea y del WorldState que se
// envía en cada tick; con un valle de 32 x 32 casillas, 10.000 jugadores ya
// es mucho más de lo que el juego puede aprovechar. Un jugador nuevo que
// llegue con el mundo lleno recibe WorldFull.
const MaxPlayers = 10_000

// IsSnapshotTick dice si tras calcular tick toca enviar una instantánea.
func IsSnapshotTick(tick uint64) bool {
	return tick != 0 && isMultiple(tick, SnapshotIntervalTicks)
}

// playerSlot es un jugador del mundo y su índice de duplicados.
type playerSlot struct {
	record Player
	// recentIDs es el índice de record.RecentActions para buscar duplicados.
	recentIDs map[string]struct{}
}

// World es el estado autoritativo del valle (escritor único, Capa 2).
// Port de World en world.rs. No es seguro para uso concurrente: quien lo
// llama serializa los pasos (regla 1).
type World struct {
	catalog             Catalog
	seed                uint64
	tick                uint64
	epoch               uint32
	demoMode            bool
	humidityContributed uint64
	// width y height son las dimensiones del valle: las del juego salvo en la
	// comparativa (NewWorldWithSize).
	width, height uint32
	tiles         []Tile
	players       map[string]*playerSlot
	// rng es el único azar del mundo (regla 6): decide la propagación y la ignición; su
	// estado viaja en la instantánea (world.rs:104).
	rng SplitMix64
	// resetCatalog es el catálogo ofrecido a los reinicios del tick en curso;
	// solo existe durante StepWithResetCatalog y no viaja en la instantánea
	// (world.rs:111).
	resetCatalog *Catalog
}

// NewWorld crea un valle vacío en el tick 0 (World::with_seed, world.rs:125;
// LoadSnapshot devolvió NOT_FOUND). La semilla fija toda la secuencia de
// incendios (regla 6): quien arranca el servicio la elige y la registra.
func NewWorld(catalog Catalog, seed uint64) *World {
	return NewWorldWithSize(catalog, seed, ValleyWidth, ValleyHeight)
}

// NewWorldWithSize crea un valle de width x height casillas, solo para
// la comparativa de rendimiento (tests/bench): el juego usa siempre 32 x 32
// (NewWorld). Un valle de otro tamaño no se puede restaurar con Restore, que
// exige el tamaño del juego.
func NewWorldWithSize(catalog Catalog, seed uint64, width, height uint32) *World {
	w := &World{
		catalog: catalog,
		seed:    seed,
		rng:     NewSplitMix64(seed),
		width:   width,
		height:  height,
		tiles:   make([]Tile, tileCount(width, height)),
		players: make(map[string]*playerSlot),
	}
	if seed != 0 {
		w.generateNaturalTerrain()
	}
	return w
}

// Step aplica los comandos en orden y avanza el valle un tick (world.rs:247).
//
// Los comandos se aplican con el número del tick nuevo t: la instantánea de
// ese tick los incluye. El orden dentro del tick es fijo y es el de Rust:
//
//  1. Olvida los action_id fuera de la ventana de 600 ticks.
//  2. Comandos, en orden de admisión. El crecimiento se resuelve al sumar la
//     humedad de cada gota (así las hojas son de quien la empujó al umbral)
//     y, por tanto, antes de la evaporación del mismo tick.
//  3. Recarga de los depósitos, si t es múltiplo del intervalo del catálogo
//     en ticks.
//  4. Plantas de tratamiento: litros extra de recarga a su dueño.
//  5. Aspersores: humedad a su área; apagan los fuegos de su área.
//  6. Fuego: un tick más a cada casilla que arde; a los 300 se consume y
//     vuelve a tierra seca. También en modo demo.
//  7. Evaporación, si t es múltiplo de TicksPerSecond (salvo en modo demo),
//     con el goteo dividiéndola en su área. También en las casillas que
//     arden.
//  8. Marchitamiento: un tick más de cuenta a cada planta seca (salvo en
//     modo demo y en las que arden).
//  9. Propagación y luego ignición al azar (salvo en modo demo): primero, un
//     sorteo por cada casilla elegible con alguna vecina ortogonal que ardía
//     al empezar el paso; después, un sorteo por cada casilla elegible, por
//     filas, con el generador con semilla.
//  10. El tick pasa a t.
//
// Las instalaciones (4, 5 y el goteo de 7) solo actúan mientras su dueño no
// lleve ausente más de OfflineInstallationSeconds (RF-25).
//
// Solo la propagación y la ignición usan el azar, y lo toman del generador con semilla del
// mundo, cuyo estado viaja en la instantánea: misma semilla y mismos comandos
// dan el mismo valle.
func (w *World) Step(commands []Command) StepOutcome {
	return w.StepWithResetCatalog(commands, nil)
}

// StepWithResetCatalog es como Step, pero ofrece catalog a los reinicios del
// tick (world.rs:256): el mundo lo adopta solo cuando un ResetValley se
// aplica (un reinicio duplicado o rechazado no cambia nada) y, si se aplica,
// el propio reinicio ya usa las cifras nuevas. Sin reinicio aplicado, el
// catálogo se descarta. catalog nil es el caso sin oferta.
func (w *World) StepWithResetCatalog(commands []Command, catalog *Catalog) StepOutcome {
	w.resetCatalog = catalog
	outcome := w.advance(commands)
	w.resetCatalog = nil
	return outcome
}

func (w *World) advance(commands []Command) StepOutcome {
	tick := satAdd(w.tick, 1)
	w.forgetOldActions(tick)

	results := make([]CommandResult, len(commands))
	for i := range commands {
		command := &commands[i]
		results[i] = CommandResult{
			ActionID: command.ActionID,
			PlayerID: command.PlayerID,
			Outcome:  w.apply(command, tick),
		}
	}

	w.advanceValley(tick)
	w.tick = tick
	return StepOutcome{Tick: tick, Results: results}
}

// apply aplica un comando (world.rs:285). El orden de las comprobaciones es
// el de Rust: identificadores válidos, duplicado, y solo entonces el
// comando.
func (w *World) apply(command *Command, tick uint64) CommandOutcome {
	if !isUUID(command.ActionID) || !isUUID(command.PlayerID) {
		return RejectedOutcome(InvalidCommand)
	}
	if slot, known := w.players[command.PlayerID]; known {
		if _, applied := slot.recentIDs[command.ActionID]; applied {
			return DuplicateOutcome
		}
	}

	var outcome CommandOutcome
	switch kind := command.Kind.(type) {
	case nil:
		outcome = RejectedOutcome(InvalidCommand)
	case DropCommand:
		outcome = w.drop(command.PlayerID, kind.X, kind.Y, tick)
	case SetDemoModeCommand:
		w.demoMode = kind.Enabled
		outcome = AppliedOutcome
	case ResetValleyCommand:
		w.resetValley()
		outcome = AppliedOutcome
	case PlaceInstallationCommand:
		if kind.Kind > TrashBin {
			// Go-only: Rust no puede representar un tipo desconocido.
			outcome = RejectedOutcome(InvalidCommand)
		} else {
			outcome = w.placeInstallation(command.PlayerID, kind.Kind, kind.X, kind.Y)
		}
	case BuyUpgradeCommand:
		if kind.Kind > BiggerTank {
			outcome = RejectedOutcome(InvalidCommand)
		} else {
			outcome = w.buyUpgrade(command.PlayerID, kind.Kind)
		}
	case CleanTrashCommand:
		outcome = w.cleanTrash(command.PlayerID, kind.X, kind.Y)
	default:
		outcome = RejectedOutcome(InvalidCommand)
	}

	if outcome == AppliedOutcome {
		// El comando aplicado ya dio de alta al jugador si era nuevo.
		slot := w.playerSlot(command.PlayerID)
		slot.record.LastActionTick = tick
		slot.record.OfflineSummary = OfflineSummary{}
		slot.record.RecentActions = append(slot.record.RecentActions, AppliedAction{
			ActionID: command.ActionID,
			Tick:     tick,
		})
		slot.recentIDs[command.ActionID] = struct{}{}
	}
	return outcome
}

// drop suelta una gota sobre (x, y) (world.rs:350): cobra el agua (salvo en
// modo demo), humedece la casilla y, si con ella llega al umbral, la hace
// crecer y paga las hojas a este jugador (el que empujó la casilla, en orden
// de admisión).
//
// Con la mejora de gota más grande la gota humedece el cuadrado de
// BiggerDrop.AreaSide casillas de lado centrado en (x, y), recortado por los
// bordes del valle; cada casilla recibe la humedad entera de la gota, cuesta
// el mismo litro y las hojas de todas las que crezcan son de quien la soltó
// (casillas en orden de fila). humidityContributed suma la humedad que la
// gota entrega en total (humedad x casillas del valle alcanzadas), crezcan o
// no. En modo demo la gota es de una sola casilla aunque el jugador tenga la
// mejora (D-01), así que cada gota aceptada suma exactamente DemoHumidity y
// el invariante de RF-30 se cumple con cualquier mejora.
//
// Una casilla ardiendo recibe la gota igual (suma humedad, pero no crece) y
// la cuenta: con la que completa DropsToExtinguish (3) el fuego se apaga, el
// crecimiento que quedó pendiente se resuelve en ese momento y el jugador de
// esa gota gana ExtinguishBonusLeaves (suma a Leaves y a LeavesEarned). Con
// la gota más grande cuenta como una gota para cada casilla ardiendo que
// alcanza, y paga el bono una vez por cada fuego apagado.
//
// Orden de las comprobaciones (importa para el motivo del rechazo): casilla
// dentro del valle, mundo con sitio para un jugador nuevo, agua suficiente.
// Un jugador que aún no existe se mide con la capacidad inicial del catálogo.
// activeDripCoverage devuelve un slice booleano de 1024 casillas que indica cuáles
// están en el área cuadrada 5x5 de al menos un riego por goteo activo en tick.
func (w *World) activeDripCoverage(tick uint64) []bool {
	covered := make([]bool, len(w.tiles))
	side := w.catalog.Drip.AreaSide
	for _, drip := range w.activeInstallations(Drip, tick) {
		x, y := w.tileXY(drip.index)
		for _, idx := range areaIndices(x, y, side, w.width, w.height) {
			covered[idx] = true
		}
	}
	return covered
}

// drop suelta una gota sobre (x, y) (world.rs:350): cobra el agua (salvo en
// modo demo), humedece la casilla y, si con ella llega al umbral, la hace
// crecer y paga las hojas a este jugador (el que empujó la casilla, en orden
// de admisión).
//
// Con la mejora de gota más grande la gota humedece el cuadrado de
// BiggerDrop.AreaSide casillas de lado centrado en (x, y), recortado por los
// bordes del valle; cada casilla recibe la humedad entera de la gota, cuesta
// el mismo litro y las hojas de todas las que crezcan son de quien la soltó
// (casillas en orden de fila). humidityContributed suma la humedad que la
// gota entrega en total (humedad x casillas del valle alcanzadas), crezcan o
// no. En modo demo la gota es de una sola casilla aunque el jugador tenga la
// mejora (D-01), así que cada gota aceptada suma exactamente DemoHumidity y
// el invariante de RF-30 se cumple con cualquier mejora.
//
// En el área 5x5 de un riego por goteo activo, el aporte de humedad por gota se
// duplica (salvo en modo demo).
//
// Una casilla ardiendo recibe la gota igual (suma humedad, pero no crece) y
// la cuenta: con la que completa DropsToExtinguish (3) el fuego se apaga, el
// crecimiento que quedó pendiente se resuelve en ese momento y el jugador de
// esa gota gana ExtinguishBonusLeaves (suma a Leaves y a LeavesEarned). Con
// la gota más grande cuenta como una gota para cada casilla ardiendo que
// alcanza, y paga el bono una vez por cada fuego apagado.
//
// Orden de las comprobaciones (importa para el motivo del rechazo): casilla
// dentro del valle, mundo con sitio para un jugador nuevo, agua suficiente.
// Un jugador que aún no existe se mide con la capacidad inicial del catálogo.
func (w *World) drop(playerID string, x, y uint32, tick uint64) CommandOutcome {
	centerIndex, inside := w.tileIndex(x, y)
	if !inside {
		return RejectedOutcome(OutOfBounds)
	}
	if w.tiles[centerIndex].Stage.IsNatural() || w.tiles[centerIndex].HasBuilding {
		return RejectedOutcome(TileOccupied)
	}
	humidity, cost := w.catalog.Drop.Humidity, w.catalog.Drop.WaterCost
	if w.demoMode {
		humidity, cost = w.catalog.Drop.DemoHumidity, 0
	}
	var known *Player
	if slot, ok := w.players[playerID]; ok {
		known = &slot.record
	}
	if known == nil && len(w.players) >= MaxPlayers {
		return RejectedOutcome(WorldFull)
	}
	water := w.catalog.Tank.Capacity
	if known != nil {
		water = known.Water
	}
	if water < cost {
		return RejectedOutcome(InsufficientWater)
	}
	side := uint32(1)
	if !w.demoMode && known != nil && known.HasUpgrade(BiggerDrop) {
		side = w.catalog.BiggerDrop.AreaSide
	}

	// Solo aquí, con la gota ya aceptada, entra un jugador nuevo.
	dripCovered := w.activeDripCoverage(tick)
	area := areaIndices(x, y, side, w.width, w.height)
	var leaves uint64
	var totalHumidity uint64
	for _, index := range area {
		tile := &w.tiles[index]
		if tile.Stage.IsNatural() || tile.HasBuilding || tile.TrashLevel > 0 {
			continue
		}
		effectiveHumidity := humidity
		if !w.demoMode && dripCovered[index] {
			effectiveHumidity = satMul(humidity, 2)
		}
		totalHumidity = satAdd(totalHumidity, effectiveHumidity)
		leaves = satAdd(leaves, AddHumidity(tile, effectiveHumidity, w.catalog.Growth))
		if ReceiveDrop(tile, w.catalog.Fire) {
			leaves = satAdd(leaves, AddHumidity(tile, 0, w.catalog.Growth))
			leaves = satAdd(leaves, w.catalog.Fire.ExtinguishBonusLeaves)
		}
	}
	w.humidityContributed = satAdd(w.humidityContributed, totalHumidity)

	player := &w.playerSlot(playerID).record
	// Sin saturar, como Rust: water >= cost está comprobado arriba (con el
	// depósito inicial para un jugador nuevo).
	player.Water -= cost
	player.WaterSpent = satAdd(player.WaterSpent, cost)
	player.Leaves = satAdd(player.Leaves, leaves)
	player.LeavesEarned = satAdd(player.LeavesEarned, leaves)
	return AppliedOutcome
}

// refillTanks recarga el depósito de cada jugador con Tank.RefillAmount cada
// Tank.RefillIntervalSeconds (en ticks, sin decimales), sin pasar de la
// capacidad (world.rs:462). Solo cuenta como recargado (RF-27) lo que cabe.
//
// Con intervalo 0 no se recarga nunca: u64::is_multiple_of(0) solo es cierto
// para el 0 y el tick que se calcula nunca es 0.
func (w *World) refillTanks(tick uint64) {
	interval := satMul(uint64(w.catalog.Tank.RefillIntervalSeconds), TicksPerSecond)
	if !isMultiple(tick, interval) {
		return
	}
	amount := w.catalog.Tank.RefillAmount
	for _, slot := range w.players {
		player := &slot.record
		added := minU64(amount, satSub(player.TankCapacity, player.Water))
		player.Water = satAdd(player.Water, added)
		player.WaterRefilled = satAdd(player.WaterRefilled, added)
	}
}

// advanceValley es lo que cambia solo con el paso del tiempo, después de los
// comandos (world.rs:610; el orden está en la documentación de Step).
func (w *World) advanceValley(tick uint64) {
	w.refillTanks(tick)
	w.runTreatmentPlants(tick)
	w.runSprinklers(tick)
	w.runDrips(tick)
	w.runTrashBins(tick)

	// Los fuegos avanzan también en modo demo: un fuego siempre se resuelve
	// (gotas, aspersor o consumirse).
	w.burnFires()
	// El modo demo congela el valle: sin evaporación, marchitamiento ni
	// incendios nuevos. Las instalaciones de arriba siguen actuando; el goteo
	// no tiene qué frenar.
	if w.demoMode {
		return
	}
	w.spawnUrbanBuildings()
	w.emitTrash(tick)
	w.propagateTrash(tick)
	dripCovered := w.activeDripCoverage(tick)
	shoreline := w.shorelineMask()
	// La cifra del catálogo es por segundo: se aplica entera una vez por
	// segundo (cada TicksPerSecond ticks), nunca como fracción.
	if isMultiple(tick, TicksPerSecond) {
		w.evaporateValley(tick, shoreline)
	}
	for i := range w.tiles {
		if !dripCovered[i] && !shoreline[i] {
			Wither(&w.tiles[i], w.catalog.Evaporation)
		}
	}
	w.propagateFires(dripCovered, shoreline)
	w.igniteFires(dripCovered, shoreline)
}

// propagateFires es la propagación a vecinas (world.rs propagate_fires; B3):
// recorre las casillas por filas y, por cada una elegible con al menos una
// vecina ortogonal que ya ardiera al empezar el paso, hace un sorteo con
// PropagationChancePPM; si sale, la enciende. Solo propagan los fuegos
// presentes al entrar, así no hay cascadas dentro de un mismo tick.
// Casillas bajo el área 5x5 de un goteo activo y orillas de agua están protegidas.
func (w *World) propagateFires(dripCovered, shoreline []bool) {
	burningAtStart := make([]bool, len(w.tiles))
	anyBurning := false
	for i := range w.tiles {
		if w.tiles[i].Fire != nil {
			burningAtStart[i] = true
			anyBurning = true
		}
	}
	if !anyBurning {
		return
	}

	ppm := w.catalog.Fire.PropagationChancePPM
	for y := range w.height {
		for x := range w.width {
			index := int(y)*int(w.width) + int(x)
			tile := &w.tiles[index]
			// && en corto: solo una casilla elegible con vecina en llamas
			// gasta un número.
			if !dripCovered[index] && !shoreline[index] && IsEligible(tile) && hasBurningNeighbor(burningAtStart, x, y, w.width, w.height) && w.rng.ChancePPM(ppm) {
				Ignite(tile)
			}
		}
	}
}

// hasBurningNeighbor dice si alguna de las vecinas ortogonales (norte, sur,
// oeste, este) de (x, y) dentro de un valle de width x height está marcada.
func hasBurningNeighbor(burning []bool, x, y, width, height uint32) bool {
	stride := int(width)
	index := int(y)*stride + int(x)
	return (y > 0 && burning[index-stride]) ||
		(y+1 < height && burning[index+stride]) ||
		(x > 0 && burning[index-1]) ||
		(x+1 < width && burning[index+1])
}

// igniteFires es la ignición al azar (world.rs:649; RF-29): recorre las
// casillas por filas y, por cada una elegible (con planta, humedad 0 y sin
// fuego), hace un sorteo con Fire.IgnitionChancePPM partes por millón. Casillas
// bajo el área 5x5 de un goteo activo y orillas de agua tienen la ignición suprimida.
func (w *World) igniteFires(dripCovered, shoreline []bool) {
	ppm := w.catalog.Fire.IgnitionChancePPM
	for i := range w.tiles {
		tile := &w.tiles[i]
		// && en corto: solo una casilla elegible gasta un número (world.rs:653).
		if !dripCovered[i] && !shoreline[i] && IsEligible(tile) && w.rng.ChancePPM(ppm) {
			Ignite(tile)
		}
	}
}

// shorelineMask identifica las casillas terrestres adyacentes a agua.
func (w *World) shorelineMask() []bool {
	mask := make([]bool, len(w.tiles))
	stride := int(w.width)
	height := int(w.height)
	for y := 0; y < height; y++ {
		for x := 0; x < stride; x++ {
			idx := y*stride + x
			if w.tiles[idx].Stage != Water {
				isShore := (y > 0 && w.tiles[idx-stride].Stage == Water) ||
					(y+1 < height && w.tiles[idx+stride].Stage == Water) ||
					(x > 0 && w.tiles[idx-1].Stage == Water) ||
					(x+1 < stride && w.tiles[idx+1].Stage == Water)
				mask[idx] = isShore
			}
		}
	}
	return mask
}

// generateNaturalTerrain genera de forma determinista franjas de río/lago y dispersión de rocas (Fase 3).
func (w *World) generateNaturalTerrain() {
	if w.width < 20 || w.height < 20 {
		return
	}
	trng := NewSplitMix64(w.seed ^ 0x5445525241494E5F)

	// 1. Franja de río / lago (alrededor de x=24 en 32x32 para no colisionar con zonas de prueba x<=15)
	baseX := int32(24) + int32(trng.NextU64()%3) - 1
	for y := uint32(0); y < w.height; y++ {
		offset := (int32(y)/4)%3 - 1
		rx := baseX + offset
		if rx < 18 {
			rx = 18
		}
		if rx > int32(w.width-4) {
			rx = int32(w.width - 4)
		}
		urx := uint32(rx)
		idx1 := y*w.width + urx
		idx2 := y*w.width + urx + 1
		if int(idx1) < len(w.tiles) {
			w.tiles[idx1].Stage = Water
		}
		if int(idx2) < len(w.tiles) {
			w.tiles[idx2].Stage = Water
		}
	}

	// Lago a media altura (y: 12..=16) ensanchando el río
	for ly := uint32(12); ly <= 16; ly++ {
		if ly < w.height {
			offset := (int32(ly)/4)%3 - 1
			rx := baseX + offset
			if rx < 18 {
				rx = 18
			}
			if rx > int32(w.width-4) {
				rx = int32(w.width - 4)
			}
			minX := uint32(rx)
			if minX > 0 {
				minX--
			}
			maxX := uint32(rx + 2)
			if maxX > w.width-2 {
				maxX = w.width - 2
			}
			for lx := minX; lx <= maxX; lx++ {
				idx := ly*w.width + lx
				if int(idx) < len(w.tiles) {
					w.tiles[idx].Stage = Water
				}
			}
		}
	}

	// Lago / laguna sur independiente (x: 17..=19, y: 26..=28)
	for ly := uint32(26); ly <= 28; ly++ {
		if ly < w.height {
			for lx := uint32(17); lx <= 19; lx++ {
				if lx < w.width {
					idx := ly*w.width + lx
					if int(idx) < len(w.tiles) {
						w.tiles[idx].Stage = Water
					}
				}
			}
		}
	}

	// 2. Dispersión de rocas (obstáculos naturales no transitables)
	numRocks := 12
	placed := 0
	attempts := 0
	span := uint64(w.width - 20)
	if span < 1 {
		span = 1
	}
	for placed < numRocks && attempts < 100 {
		attempts++
		rx := 17 + uint32(trng.NextU64()%span)
		ry := uint32(trng.NextU64() % uint64(w.height))
		if rx == ry || (rx == 20 && ry == 20) || (rx == 31 && ry == 31) {
			continue
		}
		idx := ry*w.width + rx
		if int(idx) < len(w.tiles) && w.tiles[idx].Stage == DrySoil {
			w.tiles[idx].Stage = Rock
			placed++
		}
	}
}

// RngState es el estado del generador, para la instantánea (RngStateLen
// bytes).
func (w *World) RngState() []byte { return w.rng.Bytes() }

// SetRngState restaura el estado del generador; false (sin cambiar nada) si
// no mide RngStateLen bytes.
func (w *World) SetRngState(state []byte) bool {
	rng, ok := SplitMix64FromBytes(state)
	if ok {
		w.rng = rng
	}
	return ok
}

// burnFires cuenta un tick más de fuego en cada casilla que arde; las que
// llegan a Fire.BurnOutTicks (300) se consumen y vuelven a tierra seca
// (world.rs:636).
func (w *World) burnFires() {
	for i := range w.tiles {
		Burn(&w.tiles[i], w.catalog.Fire)
	}
}

// resetValley es el reinicio del valle (world.rs:665; RF-31): el mundo
// vuelve a su estado inicial y los jugadores a los valores de arranque del
// catálogo, con los contadores de RF-27 y de las 3R a cero.
func (w *World) resetValley() {
	if w.resetCatalog != nil {
		w.catalog = *w.resetCatalog
		w.resetCatalog = nil
	}
	clear(w.tiles)
	if w.seed != 0 {
		w.generateNaturalTerrain()
	}
	w.epoch = satAdd32(w.epoch, 1)
	tank := w.catalog.Tank.Capacity
	for _, slot := range w.players {
		previous := slot.record
		slot.record = startingPlayer(previous.PlayerID, tank)
		slot.record.LastActionTick = previous.LastActionTick
		slot.record.RecentActions = previous.RecentActions
	}
}

// forgetOldActions olvida los action_id aplicados hace más de la ventana
// (world.rs:692; Capa 3). Un action_id aplicado en el tick a se recuerda
// mientras tick - a <= RecentActionsWindowTicks, ese límite incluido: con
// la ventana de 600, una acción del tick 1 sigue en el tick 601 (duplicado)
// y cae en el 602. RecentActions está en orden de tick.
func (w *World) forgetOldActions(tick uint64) {
	for _, slot := range w.players {
		actions := slot.record.RecentActions
		expired := sort.Search(len(actions), func(i int) bool {
			return satSub(tick, actions[i].Tick) <= RecentActionsWindowTicks
		})
		for _, applied := range actions[:expired] {
			delete(slot.recentIDs, applied.ActionID)
		}
		slot.record.RecentActions = slices.Delete(actions, 0, expired)
	}
}

// playerSlot devuelve el jugador y lo da de alta si no existía, con los
// valores iniciales del catálogo (world.rs:681). Solo se llama al aplicar un
// comando: los rechazados, mal formados o no soportados no crean jugadores.
func (w *World) playerSlot(playerID string) *playerSlot {
	slot, ok := w.players[playerID]
	if !ok {
		slot = &playerSlot{
			record:    startingPlayer(playerID, w.catalog.Tank.Capacity),
			recentIDs: make(map[string]struct{}),
		}
		w.players[playerID] = slot
	}
	return slot
}

// tileIndex es el índice por filas de (x, y); ok es false fuera del valle
// (world.rs:705).
func (w *World) tileIndex(x, y uint32) (index int, ok bool) {
	if x >= w.width || y >= w.height {
		return 0, false
	}
	return int(y)*int(w.width) + int(x), true
}

// Tick es el último tick calculado.
func (w *World) Tick() uint64 { return w.tick }

// Epoch es la época del valle (cuántas veces se reinició).
func (w *World) Epoch() uint32 { return w.epoch }

// DemoMode dice si el modo demo está encendido.
func (w *World) DemoMode() bool { return w.demoMode }

// HumidityContributed es la humedad que han entregado las gotas aceptadas.
func (w *World) HumidityContributed() uint64 { return w.humidityContributed }

// Width es el ancho del valle en casillas.
func (w *World) Width() uint32 { return w.width }

// Height es el alto del valle en casillas.
func (w *World) Height() uint32 { return w.height }

// CatalogVersion es la versión del catálogo vigente.
func (w *World) CatalogVersion() string { return w.catalog.Version }

// Tiles son las casillas por filas: índice = y * width + x. Solo lectura.
func (w *World) Tiles() []Tile { return w.tiles }

// Tile devuelve la casilla (x, y); ok es false fuera del valle. Solo lectura.
func (w *World) Tile(x, y uint32) (tile *Tile, ok bool) {
	index, ok := w.tileIndex(x, y)
	if !ok {
		return nil, false
	}
	return &w.tiles[index], true
}

// Player devuelve el jugador; ok es false si no ha entrado al mundo. Solo
// lectura.
func (w *World) Player(playerID string) (player *Player, ok bool) {
	slot, ok := w.players[playerID]
	if !ok {
		return nil, false
	}
	return &slot.record, true
}

// Players devuelve los jugadores ordenados por player_id (como el BTreeMap
// de Rust). Solo lectura.
func (w *World) Players() []*Player {
	ids := make([]string, 0, len(w.players))
	for id := range w.players {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	players := make([]*Player, len(ids))
	for i, id := range ids {
		players[i] = &w.players[id].record
	}
	return players
}

// areaIndices son los índices (por filas) de las casillas del cuadrado de
// side casillas de lado centrado en (x, y), recortado por los bordes del
// valle (world.rs:793). Con un lado par el centro queda una casilla más cerca
// del extremo de arriba y de la izquierda: (side - 1) / 2 casillas antes y
// side / 2 después (lado 3 -> 1 y 1; lado 4 -> 1 y 2). Un lado 0 equivale a
// uno: Rust usa saturating_sub(1) / 2 = 0 antes y 0 / 2 = 0 después, y el
// rango incluye siempre el centro.
func areaIndices(x, y, side, width, height uint32) []int {
	before := satSub32(side, 1) / 2
	after := side / 2
	firstColumn := satSub32(x, before)
	lastColumn := min(satAdd32(x, after), width-1)
	firstRow := satSub32(y, before)
	lastRow := min(satAdd32(y, after), height-1)

	indices := make([]int, 0, int(lastColumn-firstColumn+1)*int(lastRow-firstRow+1))
	for row := firstRow; row <= lastRow; row++ {
		for column := firstColumn; column <= lastColumn; column++ {
			indices = append(indices, int(row)*int(width)+int(column))
		}
	}
	return indices
}

func tileCount(width, height uint32) int {
	return int(width) * int(height)
}

// cleanTrash limpia un nivel de basura de una casilla (Fase 4).
// Cada ejecución reduce en 1 el TrashLevel. Al llegar a 0, la casilla
// retorna a tierra seca y otorga hojas de incentivo (+1).
func (w *World) cleanTrash(playerID string, x, y uint32) CommandOutcome {
	index, inside := w.tileIndex(x, y)
	if !inside {
		return RejectedOutcome(OutOfBounds)
	}
	tile := &w.tiles[index]
	if tile.TrashLevel == 0 {
		return RejectedOutcome(InvalidCommand)
	}
	tile.TrashLevel--
	var incentive uint64
	if tile.TrashLevel == 0 {
		tile.Stage = DrySoil
		incentive = 1
	}
	player := &w.playerSlot(playerID).record
	player.Leaves = satAdd(player.Leaves, incentive)
	player.LeavesEarned = satAdd(player.LeavesEarned, incentive)
	return AppliedOutcome
}

// spawnUrbanBuildings genera estructuras de 1 casilla tras alcanzar umbral de progreso (Fase 4).
// Inmunes a fuego y basura.
func (w *World) spawnUrbanBuildings() {
	treeCount := 0
	for i := range w.tiles {
		if w.tiles[i].Stage == Tree {
			treeCount++
		}
	}
	if treeCount < 2 && w.humidityContributed < 500 {
		return
	}
	buildingCount := 0
	for i := range w.tiles {
		if w.tiles[i].HasBuilding {
			buildingCount++
		}
	}
	if buildingCount >= 2 {
		return
	}
	width, height := w.width, w.height
	type pt struct{ x, y uint32 }
	var perimeter []pt
	for x := uint32(0); x < width; x++ {
		perimeter = append(perimeter, pt{x, 0})
		perimeter = append(perimeter, pt{x, height - 1})
	}
	for y := uint32(1); y < height-1; y++ {
		perimeter = append(perimeter, pt{0, y})
		perimeter = append(perimeter, pt{width - 1, y})
	}
	for _, p := range perimeter {
		idx, _ := w.tileIndex(p.x, p.y)
		tile := &w.tiles[idx]
		if !tile.Stage.IsNatural() &&
			!tile.HasBuilding &&
			tile.Stage == DrySoil &&
			tile.Installation == nil &&
			tile.Fire == nil &&
			tile.TrashLevel == 0 {
			w.tiles[idx].HasBuilding = true
			break
		}
	}
}

// emitTrash: En ciclos fijos (cada 60 ticks = 6 s), el edificio suma +1 a TrashLevel
// en una casilla vecina (3 x 3), eliminando plantas e instalaciones preexistentes (Fase 4).
func (w *World) emitTrash(tick uint64) {
	interval := satMul(6, TicksPerSecond)
	if !isMultiple(tick, interval) {
		return
	}
	var buildingIndices []int
	for i := range w.tiles {
		if w.tiles[i].HasBuilding {
			buildingIndices = append(buildingIndices, i)
		}
	}
	for _, bIdx := range buildingIndices {
		bx, by := w.tileXY(bIdx)
		area := areaIndices(bx, by, 3, w.width, w.height)
		var targetIdx int = -1
		for _, idx := range area {
			if idx != bIdx && !w.tiles[idx].Stage.IsNatural() && !w.tiles[idx].HasBuilding && w.tiles[idx].TrashLevel < 3 {
				targetIdx = idx
				break
			}
		}
		if targetIdx == -1 {
			for _, idx := range area {
				if idx != bIdx && !w.tiles[idx].Stage.IsNatural() && !w.tiles[idx].HasBuilding {
					targetIdx = idx
					break
				}
			}
		}
		if targetIdx != -1 {
			tile := &w.tiles[targetIdx]
			if tile.TrashLevel < 3 {
				tile.TrashLevel++
			}
			tile.Stage = DrySoil
			tile.Humidity = 0
			tile.Installation = nil
			tile.Fire = nil
		}
	}
}

// propagateTrash: Una casilla con TrashLevel >= 2 incrementa su basura y propaga +1
// a casillas adyacentes no protegidas cada 8 s (Fase 4), acotada a 60 casillas.
func (w *World) propagateTrash(tick uint64) {
	interval := satMul(8, TicksPerSecond)
	if !isMultiple(tick, interval) {
		return
	}
	pollutedCount := 0
	for i := range w.tiles {
		if w.tiles[i].TrashLevel > 0 {
			pollutedCount++
		}
	}
	if pollutedCount >= 60 {
		return
	}
	trashBins := w.activeInstallations(TrashBin, tick)
	protected := make([]bool, len(w.tiles))
	for _, tb := range trashBins {
		tx, ty := w.tileXY(tb.index)
		for _, pIdx := range areaIndices(tx, ty, 3, w.width, w.height) {
			protected[pIdx] = true
		}
	}

	var sourceIndices []int
	for i := range w.tiles {
		if w.tiles[i].TrashLevel >= 1 {
			sourceIndices = append(sourceIndices, i)
		}
	}

	for _, sIdx := range sourceIndices {
		if protected[sIdx] {
			continue
		}
		currentLevel := w.tiles[sIdx].TrashLevel
		if currentLevel < 3 {
			w.tiles[sIdx].TrashLevel = currentLevel + 1
		}
		if currentLevel >= 2 {
			x, y := w.tileXY(sIdx)
			for _, nIdx := range orthogonalNeighborIndices(x, y, w.width, w.height) {
				tile := &w.tiles[nIdx]
				if !protected[nIdx] && !tile.Stage.IsNatural() && !tile.HasBuilding && tile.TrashLevel < 3 {
					if tile.TrashLevel == 0 {
						if pollutedCount >= 60 {
							continue
						}
						pollutedCount++
					}
					tile.TrashLevel++
					tile.Stage = DrySoil
					tile.Humidity = 0
					tile.Installation = nil
					tile.Fire = nil
				}
			}
		}
	}
}

func orthogonalNeighborIndices(x, y, width, height uint32) []int {
	var neighbors []int
	if y > 0 {
		neighbors = append(neighbors, int(y-1)*int(width)+int(x))
	}
	if y+1 < height {
		neighbors = append(neighbors, int(y+1)*int(width)+int(x))
	}
	if x > 0 {
		neighbors = append(neighbors, int(y)*int(width)+int(x-1))
	}
	if x+1 < width {
		neighbors = append(neighbors, int(y)*int(width)+int(x+1))
	}
	return neighbors
}
