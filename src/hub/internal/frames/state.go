package frames

import (
	playv1 "github.com/iadev2005/hydrovalley/gen/go/hydrovalley/play/v1"
	worldv1 "github.com/iadev2005/hydrovalley/gen/go/hydrovalley/world/v1"
)

// StateBuilder prepara una difusión de estado (RF-03): calcula los campos del
// mapa una sola vez y después arma el StateMessage de cada conexión con la
// vista de su jugador. Los mensajes comparten las listas del mapa, así que
// quien los recibe no debe modificarlas.
type StateBuilder struct {
	tick          uint64
	paused        bool
	world         *worldv1.WorldState
	acceptedDrops uint64

	stages        []uint32
	humidity      []uint64
	fires         []*playv1.FireView
	installations []*playv1.InstallationView
	players       map[string]*worldv1.PlayerState
}

// NewStateBuilder prepara el estado del tick `tick`. acceptedDrops es la
// cuenta de gotas aceptadas del hub. world puede ser nil (todavía no hubo
// ningún Step correcto): entonces solo van tick, paused y acceptedDrops.
func NewStateBuilder(tick uint64, paused bool, world *worldv1.WorldState, acceptedDrops uint64) *StateBuilder {
	b := &StateBuilder{tick: tick, paused: paused, world: world, acceptedDrops: acceptedDrops}
	if world == nil {
		return b
	}
	width := world.GetWidth()
	tiles := world.GetTiles()
	b.stages = make([]uint32, len(tiles))
	b.humidity = make([]uint64, len(tiles))
	for i, tile := range tiles {
		b.stages[i] = uint32(tile.GetStage())
		b.humidity[i] = tile.GetHumidity()
		if width == 0 {
			continue
		}
		x, y := uint32(i)%width, uint32(i)/width
		if fire := tile.GetFire(); fire != nil {
			b.fires = append(b.fires, &playv1.FireView{X: x, Y: y, DropsReceived: fire.GetDropsReceived()})
		}
		if inst := tile.GetInstallation(); inst != nil {
			b.installations = append(b.installations, &playv1.InstallationView{
				X:                   x,
				Y:                   y,
				Installation:        installationName(inst.GetKind()),
				OwnerId:             inst.GetOwnerId(),
				RemainingDurability: inst.GetRemainingDurability(),
			})
		}
	}
	b.players = make(map[string]*worldv1.PlayerState, len(world.GetPlayers()))
	for _, p := range world.GetPlayers() {
		b.players[p.GetPlayerId()] = p
	}
	return b
}

// For arma el StateMessage para `playerID`; withMap = false omite los
// campos del mapa (map=false en /play).
func (b *StateBuilder) For(playerID string, withMap bool) *playv1.StateMessage {
	msg := &playv1.StateMessage{Type: TypeState, Tick: b.tick, Paused: b.paused, AcceptedDrops: b.acceptedDrops}
	if b.world == nil {
		return msg
	}
	msg.DemoMode = b.world.GetDemoMode()
	msg.HumidityContributed = b.world.GetHumidityContributed()
	msg.Width = b.world.GetWidth()
	msg.Height = b.world.GetHeight()
	if withMap {
		msg.Stages = b.stages
		msg.Humidity = b.humidity
		msg.Fires = b.fires
		msg.Installations = b.installations
	}
	if p, ok := b.players[playerID]; ok {
		msg.Player = playerView(p)
	}
	return msg
}

// AttachOfflineSummary agrega a msg el resumen al volver del jugador (RF-25),
// si el mundo lo trae y no está vacío, y dice si lo agregó. El hub lo pide
// solo para el primer estado de cada conexión: el cliente lo guarda hasta que
// el jugador lo cierre. msg tiene que ser de este mismo builder y jugador.
func (b *StateBuilder) AttachOfflineSummary(msg *playv1.StateMessage, playerID string) bool {
	p, ok := b.players[playerID]
	summary := p.GetOfflineSummary()
	if !ok || msg.GetPlayer() == nil || summary == nil {
		return false
	}
	view := &playv1.OfflineSummaryView{
		LeavesEarned:      summary.GetLeavesEarned(),
		WaterReused:       summary.GetWaterReused(),
		FiresExtinguished: summary.GetFiresExtinguished(),
		TilesGrown:        summary.GetTilesGrown(),
	}
	if view.LeavesEarned == 0 && view.WaterReused == 0 && view.FiresExtinguished == 0 && view.TilesGrown == 0 {
		return false
	}
	msg.Player.OfflineSummary = view
	return true
}

func playerView(p *worldv1.PlayerState) *playv1.PlayerView {
	view := &playv1.PlayerView{Water: p.GetWater(), TankCapacity: p.GetTankCapacity(), Leaves: p.GetLeaves()}
	for _, u := range p.GetUpgrades() {
		if name := upgradeName(u); name != "" {
			view.Upgrades = append(view.Upgrades, name)
		}
	}
	for _, price := range p.GetNextPrices() {
		switch price.GetKind() {
		case worldv1.InstallationKind_INSTALLATION_KIND_SPRINKLER:
			view.NextSprinklerPrice = price.GetLeaves()
		case worldv1.InstallationKind_INSTALLATION_KIND_DRIP:
			view.NextDripPrice = price.GetLeaves()
		case worldv1.InstallationKind_INSTALLATION_KIND_TREATMENT_PLANT:
			view.NextTreatmentPlantPrice = price.GetLeaves()
		}
	}
	return view
}
