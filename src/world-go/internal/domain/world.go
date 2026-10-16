package domain

import "sort"

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
	tick                uint64
	epoch               uint32
	demoMode            bool
	humidityContributed uint64
	tiles               []Tile
	players             map[string]*playerSlot
	// TODO(T4): rng SplitMix64 con semilla (world.rs:104, 125); su estado
	// viaja en la instantánea.
	// TODO(T5): resetCatalog, el catálogo ofrecido a los reinicios del tick
	// en curso (world.rs:111, 256).
}

// NewWorld crea un valle vacío en el tick 0.
//
// TODO(T4): recibirá la semilla del generador, como World::with_seed
// (world.rs:125); hasta entonces no hay azar que sembrar.
func NewWorld(catalog Catalog) *World {
	return &World{
		catalog: catalog,
		tiles:   make([]Tile, tileCount(ValleyWidth, ValleyHeight)),
		players: make(map[string]*playerSlot),
	}
}

// Step aplica los comandos en orden y avanza el valle un tick (world.rs:247).
//
// Los comandos se aplican con el número del tick nuevo t: la instantánea de
// ese tick los incluye. El orden dentro del tick es fijo y es el de Rust:
//
//  1. Olvida los action_id fuera de la ventana de 600 ticks (T5).
//  2. Comandos, en orden de admisión. El crecimiento se resuelve al sumar la
//     humedad de cada gota (así las hojas son de quien la empujó al umbral)
//     y, por tanto, antes de la evaporación del mismo tick.
//  3. Recarga de los depósitos, si t es múltiplo del intervalo del catálogo
//     en ticks.
//  4. Plantas de tratamiento: litros extra de recarga a su dueño (T3).
//  5. Aspersores: humedad a su área; apagan los fuegos de su área (T3).
//  6. Fuego: un tick más a cada casilla que arde; a los 300 se consume y
//     vuelve a tierra seca. También en modo demo.
//  7. Evaporación, si t es múltiplo de TicksPerSecond (salvo en modo demo),
//     con el goteo dividiéndola en su área (T3). También en las casillas que
//     arden.
//  8. Marchitamiento: un tick más de cuenta a cada planta seca (salvo en
//     modo demo y en las que arden).
//  9. Ignición al azar (salvo en modo demo) (T4).
//  10. El tick pasa a t.
//
// TODO(T5): StepWithResetCatalog (world.rs:256), que ofrece un catálogo a los
// reinicios del tick; Step es el caso sin catálogo.
func (w *World) Step(commands []Command) StepOutcome {
	tick := satAdd(w.tick, 1)
	// TODO(T5): w.forgetOldActions(tick) (world.rs:269, 692).

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
		outcome = w.drop(command.PlayerID, kind.X, kind.Y)
	case SetDemoModeCommand:
		// TODO(T5): w.demoMode = kind.Enabled; Applied (world.rs:296-299).
		outcome = RejectedOutcome(InvalidCommand)
	case ResetValleyCommand:
		// TODO(T5): w.resetValley(); Applied (world.rs:300-303).
		outcome = RejectedOutcome(InvalidCommand)
	case PlaceInstallationCommand:
		// TODO(T3): w.placeInstallation(...) (world.rs:304-306, 402).
		outcome = RejectedOutcome(InvalidCommand)
	case BuyUpgradeCommand:
		// TODO(T3): w.buyUpgrade(...) (world.rs:307, 437).
		outcome = RejectedOutcome(InvalidCommand)
	default:
		outcome = RejectedOutcome(InvalidCommand)
	}

	if outcome == AppliedOutcome {
		// El comando aplicado ya dio de alta al jugador si era nuevo.
		slot := w.playerSlot(command.PlayerID)
		slot.record.LastActionTick = tick
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
func (w *World) drop(playerID string, x, y uint32) CommandOutcome {
	if _, inside := w.tileIndex(x, y); !inside {
		return RejectedOutcome(OutOfBounds)
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
	area := areaIndices(x, y, side)
	var leaves uint64
	for _, index := range area {
		tile := &w.tiles[index]
		leaves = satAdd(leaves, AddHumidity(tile, humidity, w.catalog.Growth))
		if ReceiveDrop(tile, w.catalog.Fire) {
			leaves = satAdd(leaves, AddHumidity(tile, 0, w.catalog.Growth))
			leaves = satAdd(leaves, w.catalog.Fire.ExtinguishBonusLeaves)
		}
	}
	w.humidityContributed = satAdd(w.humidityContributed, satMul(humidity, uint64(len(area))))

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
	// TODO(T3): w.runTreatmentPlants(tick) (world.rs:509) y
	// w.runSprinklers(tick) (world.rs:537), en este orden y antes del fuego.

	// Los fuegos avanzan también en modo demo: un fuego siempre se resuelve
	// (gotas, aspersor o consumirse).
	w.burnFires()
	// El modo demo congela el valle: sin evaporación, marchitamiento ni
	// incendios nuevos. Las instalaciones de arriba siguen actuando; el goteo
	// no tiene qué frenar.
	if w.demoMode {
		return
	}
	// La cifra del catálogo es por segundo: se aplica entera una vez por
	// segundo (cada TicksPerSecond ticks), nunca como fracción.
	if isMultiple(tick, TicksPerSecond) {
		w.evaporateValley()
	}
	for i := range w.tiles {
		Wither(&w.tiles[i], w.catalog.Evaporation)
	}
	// TODO(T4): w.igniteFires() (world.rs:649): un sorteo por casilla
	// elegible, por filas, al final del tick.
}

// evaporateValley es la evaporación del segundo (world.rs:579).
//
// TODO(T3): el riego por goteo (Reducir) divide la evaporación en su área de
// DripRules.AreaSide casillas de lado (world.rs:579-606): el divisor se
// aplica una sola vez por casilla, el ahorro se acredita al primer goteo en
// orden de casilla y suma a WaterReduced de su dueño. Sin goteos, que es el
// caso de T2, toda casilla pierde la cifra entera.
func (w *World) evaporateValley() {
	amount := w.catalog.Evaporation.HumidityPerSecond
	for i := range w.tiles {
		Evaporate(&w.tiles[i], amount)
	}
}

// burnFires cuenta un tick más de fuego en cada casilla que arde; las que
// llegan a Fire.BurnOutTicks (300) se consumen y vuelven a tierra seca
// (world.rs:636).
func (w *World) burnFires() {
	for i := range w.tiles {
		Burn(&w.tiles[i], w.catalog.Fire)
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
	if x >= ValleyWidth || y >= ValleyHeight {
		return 0, false
	}
	return int(y)*int(ValleyWidth) + int(x), true
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
func (w *World) Width() uint32 { return ValleyWidth }

// Height es el alto del valle en casillas.
func (w *World) Height() uint32 { return ValleyHeight }

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
func areaIndices(x, y, side uint32) []int {
	before := satSub32(side, 1) / 2
	after := side / 2
	firstColumn := satSub32(x, before)
	lastColumn := min(satAdd32(x, after), ValleyWidth-1)
	firstRow := satSub32(y, before)
	lastRow := min(satAdd32(y, after), ValleyHeight-1)

	indices := make([]int, 0, int(lastColumn-firstColumn+1)*int(lastRow-firstRow+1))
	for row := firstRow; row <= lastRow; row++ {
		for column := firstColumn; column <= lastColumn; column++ {
			indices = append(indices, int(row)*int(ValleyWidth)+int(column))
		}
	}
	return indices
}

func tileCount(width, height uint32) int {
	return int(width) * int(height)
}
