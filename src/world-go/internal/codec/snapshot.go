package codec

import (
	persistencev1 "github.com/iadev2005/hydrovalley/gen/go/hydrovalley/persistence/v1"
	worldv1 "github.com/iadev2005/hydrovalley/gen/go/hydrovalley/world/v1"
	"google.golang.org/protobuf/proto"

	"github.com/iadev2005/hydrovalley/src/world-go/internal/domain"
)

// WorldState es el estado que el hub difunde (world_state de Rust; RF-03).
func WorldState(w *domain.World) *worldv1.WorldState {
	tiles := make([]*worldv1.Tile, len(w.Tiles()))
	for i := range w.Tiles() {
		tiles[i] = tileToProto(&w.Tiles()[i])
	}
	players := make([]*worldv1.PlayerState, 0, len(w.Players()))
	for _, player := range w.Players() {
		players = append(players, playerState(w, player))
	}
	return &worldv1.WorldState{
		Width:               w.Width(),
		Height:              w.Height(),
		Tiles:               tiles,
		Players:             players,
		DemoMode:            w.DemoMode(),
		HumidityContributed: w.HumidityContributed(),
		Epoch:               w.Epoch(),
	}
}

func playerState(w *domain.World, player *domain.Player) *worldv1.PlayerState {
	upgrades := make([]worldv1.UpgradeKind, len(player.Upgrades))
	for i, upgrade := range player.Upgrades {
		upgrades[i] = upgradeToProto(upgrade)
	}
	// Precio de la siguiente instalación de cada tipo (RF-07), calculado por
	// las reglas del simulador: el hub solo lo reenvía.
	kinds := []domain.InstallationKind{domain.Sprinkler, domain.Drip, domain.TreatmentPlant}
	prices := make([]*worldv1.InstallationPrice, len(kinds))
	for i, kind := range kinds {
		prices[i] = &worldv1.InstallationPrice{
			Kind:   installationToProto(kind),
			Leaves: w.NextPrice(player, kind),
		}
	}
	return &worldv1.PlayerState{
		PlayerId:     player.PlayerID,
		Water:        player.Water,
		TankCapacity: player.TankCapacity,
		Leaves:       player.Leaves,
		Upgrades:     upgrades,
		NextPrices:   prices,
		// Un resumen vacío no viaja.
		OfflineSummary: offlineSummaryToProto(player.OfflineSummary),
	}
}

// EncodeSnapshot es la fila de la instantánea con el WorldSnapshot
// serializado dentro (encode_snapshot de Rust). Las filas de jugador son los
// contadores tipados que la persistencia guarda en columnas.
func EncodeSnapshot(w *domain.World) (*persistencev1.Snapshot, error) {
	return EncodeParts(w.Parts())
}

// EncodeParts codifica una copia del estado (World.Parts). El simulador toma
// la copia con el bloqueo del mundo y la codifica fuera de él, en segundo
// plano: Step no espera ni a la serialización ni a la persistencia.
func EncodeParts(parts domain.WorldParts) (*persistencev1.Snapshot, error) {
	rows := make([]*persistencev1.SnapshotPlayer, len(parts.Players))
	records := make([]*worldv1.PlayerRecord, len(parts.Players))
	for i := range parts.Players {
		rows[i] = snapshotPlayer(&parts.Players[i])
		records[i] = playerRecord(&parts.Players[i])
	}
	tiles := make([]*worldv1.Tile, len(parts.Tiles))
	for i := range parts.Tiles {
		tiles[i] = tileToProto(&parts.Tiles[i])
	}
	// Los campos van en orden de número y no hay mapas: los mismos datos dan
	// siempre los mismos bytes, que además son los que escribe prost.
	worldState, err := proto.MarshalOptions{Deterministic: true}.Marshal(&worldv1.WorldSnapshot{
		Tick:                parts.Tick,
		Epoch:               parts.Epoch,
		CatalogVersion:      parts.CatalogVersion,
		RngState:            parts.RngState,
		DemoMode:            parts.DemoMode,
		HumidityContributed: parts.HumidityContributed,
		Width:               parts.Width,
		Height:              parts.Height,
		Tiles:               tiles,
		Players:             records,
	})
	if err != nil {
		return nil, err
	}
	return &persistencev1.Snapshot{
		Tick:           parts.Tick,
		Epoch:          parts.Epoch,
		CatalogVersion: parts.CatalogVersion,
		WorldState:     worldState,
		Players:        rows,
	}, nil
}

// DecodeSnapshot reconstruye el mundo desde la fila de la instantánea
// (decode_snapshot de Rust). El mundo aplica el catálogo vigente, no el de la
// instantánea.
func DecodeSnapshot(snapshot *persistencev1.Snapshot, catalog domain.Catalog) (*domain.World, error) {
	message := &worldv1.WorldSnapshot{}
	if err := proto.Unmarshal(snapshot.GetWorldState(), message); err != nil {
		return nil, &MalformedError{Err: err}
	}
	if message.GetTick() != snapshot.GetTick() {
		return nil, &TickMismatchError{Row: snapshot.GetTick(), World: message.GetTick()}
	}
	tiles := make([]domain.Tile, len(message.GetTiles()))
	for i, tile := range message.GetTiles() {
		var err error
		if tiles[i], err = tileFromProto(tile); err != nil {
			return nil, err
		}
	}
	players := make([]domain.Player, len(message.GetPlayers()))
	for i, record := range message.GetPlayers() {
		var err error
		if players[i], err = playerFromRecord(record); err != nil {
			return nil, err
		}
	}
	world, err := domain.Restore(catalog, domain.WorldParts{
		Tick:                message.GetTick(),
		Epoch:               message.GetEpoch(),
		CatalogVersion:      message.GetCatalogVersion(),
		RngState:            message.GetRngState(),
		DemoMode:            message.GetDemoMode(),
		HumidityContributed: message.GetHumidityContributed(),
		Width:               message.GetWidth(),
		Height:              message.GetHeight(),
		Tiles:               tiles,
		Players:             players,
	})
	if err != nil {
		return nil, &RestoreFailure{Err: err}
	}
	return world, nil
}

func tileToProto(tile *domain.Tile) *worldv1.Tile {
	message := &worldv1.Tile{
		Stage:    stageToProto(tile.Stage),
		Humidity: tile.Humidity,
		DryTicks: tile.DryTicks,
	}
	if tile.Fire != nil {
		message.Fire = &worldv1.Fire{
			DropsReceived: tile.Fire.DropsReceived,
			BurningTicks:  tile.Fire.BurningTicks,
		}
	}
	if tile.Installation != nil {
		message.Installation = &worldv1.Installation{
			Kind:    installationToProto(tile.Installation.Kind),
			OwnerId: tile.Installation.OwnerID,
		}
	}
	return message
}

func tileFromProto(message *worldv1.Tile) (domain.Tile, error) {
	// Mismo orden de comprobaciones que Rust: primero la instalación.
	var installation *domain.Installation
	if message.GetInstallation() != nil {
		kind, err := installationFromProto(message.GetInstallation().GetKind(), "Installation.kind")
		if err != nil {
			return domain.Tile{}, err
		}
		installation = &domain.Installation{Kind: kind, OwnerID: message.GetInstallation().GetOwnerId()}
	}
	stage, err := stageFromProto(message.GetStage())
	if err != nil {
		return domain.Tile{}, err
	}
	tile := domain.Tile{
		Stage:        stage,
		Humidity:     message.GetHumidity(),
		DryTicks:     message.GetDryTicks(),
		Installation: installation,
	}
	if fire := message.GetFire(); fire != nil {
		tile.Fire = &domain.Fire{DropsReceived: fire.GetDropsReceived(), BurningTicks: fire.GetBurningTicks()}
	}
	return tile, nil
}

func playerRecord(player *domain.Player) *worldv1.PlayerRecord {
	purchases := make([]*worldv1.InstallationCount, len(player.Purchases))
	for i, purchase := range player.Purchases {
		purchases[i] = &worldv1.InstallationCount{Kind: installationToProto(purchase.Kind), Count: purchase.Count}
	}
	upgrades := make([]worldv1.UpgradeKind, len(player.Upgrades))
	for i, upgrade := range player.Upgrades {
		upgrades[i] = upgradeToProto(upgrade)
	}
	recent := make([]*worldv1.AppliedAction, len(player.RecentActions))
	for i, applied := range player.RecentActions {
		recent[i] = &worldv1.AppliedAction{ActionId: applied.ActionID, Tick: applied.Tick}
	}
	return &worldv1.PlayerRecord{
		PlayerId:       player.PlayerID,
		Water:          player.Water,
		TankCapacity:   player.TankCapacity,
		Leaves:         player.Leaves,
		WaterRefilled:  player.WaterRefilled,
		WaterSpent:     player.WaterSpent,
		LeavesEarned:   player.LeavesEarned,
		LeavesSpent:    player.LeavesSpent,
		WaterReduced:   player.WaterReduced,
		WaterReused:    player.WaterReused,
		WaterRecycled:  player.WaterRecycled,
		Purchases:      purchases,
		Upgrades:       upgrades,
		LastActionTick: player.LastActionTick,
		RecentActions:  recent,
		// Un resumen vacío no se escribe: los mundos sin resumen conservan
		// los mismos bytes de instantánea.
		OfflineSummary:    offlineSummaryToProto(player.OfflineSummary),
		FiresExtinguished: player.FiresExtinguished,
		TilesGrown:        player.TilesGrown,
	}
}

func offlineSummaryToProto(summary domain.OfflineSummary) *worldv1.OfflineSummary {
	if summary.IsEmpty() {
		return nil
	}
	return &worldv1.OfflineSummary{
		LeavesEarned:      summary.LeavesEarned,
		WaterReused:       summary.WaterReused,
		FiresExtinguished: summary.FiresExtinguished,
		TilesGrown:        summary.TilesGrown,
	}
}

func playerFromRecord(record *worldv1.PlayerRecord) (domain.Player, error) {
	player := domain.Player{
		PlayerID:       record.GetPlayerId(),
		Water:          record.GetWater(),
		TankCapacity:   record.GetTankCapacity(),
		Leaves:         record.GetLeaves(),
		WaterRefilled:  record.GetWaterRefilled(),
		WaterSpent:     record.GetWaterSpent(),
		LeavesEarned:   record.GetLeavesEarned(),
		LeavesSpent:    record.GetLeavesSpent(),
		WaterReduced:   record.GetWaterReduced(),
		WaterReused:    record.GetWaterReused(),
		WaterRecycled:  record.GetWaterRecycled(),
		LastActionTick: record.GetLastActionTick(),
		OfflineSummary: domain.OfflineSummary{
			LeavesEarned:      record.GetOfflineSummary().GetLeavesEarned(),
			WaterReused:       record.GetOfflineSummary().GetWaterReused(),
			FiresExtinguished: record.GetOfflineSummary().GetFiresExtinguished(),
			TilesGrown:        record.GetOfflineSummary().GetTilesGrown(),
		},
		FiresExtinguished: record.GetFiresExtinguished(),
		TilesGrown:        record.GetTilesGrown(),
	}
	for _, count := range record.GetPurchases() {
		kind, err := installationFromProto(count.GetKind(), "InstallationCount.kind")
		if err != nil {
			return domain.Player{}, err
		}
		player.Purchases = append(player.Purchases, domain.Purchase{Kind: kind, Count: count.GetCount()})
	}
	for _, value := range record.GetUpgrades() {
		upgrade, err := upgradeFromProto(value)
		if err != nil {
			return domain.Player{}, err
		}
		player.Upgrades = append(player.Upgrades, upgrade)
	}
	for _, applied := range record.GetRecentActions() {
		player.RecentActions = append(player.RecentActions, domain.AppliedAction{
			ActionID: applied.GetActionId(),
			Tick:     applied.GetTick(),
		})
	}
	return player, nil
}

func snapshotPlayer(player *domain.Player) *persistencev1.SnapshotPlayer {
	return &persistencev1.SnapshotPlayer{
		PlayerId:          player.PlayerID,
		Water:             player.Water,
		Leaves:            player.Leaves,
		WaterRefilled:     player.WaterRefilled,
		WaterSpent:        player.WaterSpent,
		LeavesEarned:      player.LeavesEarned,
		LeavesSpent:       player.LeavesSpent,
		WaterReduced:      player.WaterReduced,
		WaterReused:       player.WaterReused,
		WaterRecycled:     player.WaterRecycled,
		FiresExtinguished: player.FiresExtinguished,
		TilesGrown:        player.TilesGrown,
	}
}
