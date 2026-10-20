package domain

import (
	"fmt"
	"slices"
)

// WorldParts es todo el estado del mundo en campos planos: lo que viaja en la
// instantánea (WorldParts de world.rs:36). Las colecciones vacías son nil, de
// modo que la igualdad profunda de dos WorldParts equivale a la de Rust.
type WorldParts struct {
	Tick                uint64
	Epoch               uint32
	CatalogVersion      string
	RngState            []byte
	DemoMode            bool
	HumidityContributed uint64
	Width               uint32
	Height              uint32
	// Tiles van por filas: índice = y * Width + x.
	Tiles []Tile
	// Players van ordenados por player_id.
	Players []Player
}

// DimensionsError: el valle de la instantánea no mide lo que el simulador
// (RestoreError::Dimensions de Rust).
type DimensionsError struct {
	ExpectedWidth, ExpectedHeight uint32
	FoundWidth, FoundHeight       uint32
}

func (e DimensionsError) Error() string {
	return fmt.Sprintf("el valle mide %dx%d y la instantánea %dx%d",
		e.ExpectedWidth, e.ExpectedHeight, e.FoundWidth, e.FoundHeight)
}

// TileCountError: la instantánea no trae las casillas del valle
// (RestoreError::TileCount).
type TileCountError struct{ Expected, Found int }

func (e TileCountError) Error() string {
	return fmt.Sprintf("se esperaban %d casillas y hay %d", e.Expected, e.Found)
}

// DuplicatePlayerError: un mismo player_id aparece dos veces
// (RestoreError::DuplicatePlayer).
type DuplicatePlayerError struct{ PlayerID string }

func (e DuplicatePlayerError) Error() string {
	return "jugador repetido: " + e.PlayerID
}

// RngStateError: el estado del generador no está vacío y no mide los
// RngStateLen bytes (RestoreError::RngState).
type RngStateError struct{ Len int }

func (e RngStateError) Error() string {
	return fmt.Sprintf("el estado del generador mide %d bytes y debería medir %d", e.Len, RngStateLen)
}

// Restore reconstruye el mundo a partir de una instantánea (World::restore,
// world.rs:147).
//
// El mundo restaurado aplica el catálogo vigente (catalog), no el que estaba
// en vigor al tomar la instantánea: así un cambio de cifras surte efecto al
// reiniciar. parts.CatalogVersion solo sirve para avisar de que cambió; la
// siguiente instantánea lleva la versión vigente.
//
// Un estado del generador vacío (una instantánea anterior a los incendios)
// siembra el generador con el tick, que es determinista; uno que no mide
// RngStateLen bytes se rechaza. Restore copia parts: no comparte memoria con
// quien se lo entrega (Rust lo mueve).
func Restore(catalog Catalog, parts WorldParts) (*World, error) {
	if parts.Width != ValleyWidth || parts.Height != ValleyHeight {
		return nil, DimensionsError{
			ExpectedWidth: ValleyWidth, ExpectedHeight: ValleyHeight,
			FoundWidth: parts.Width, FoundHeight: parts.Height,
		}
	}
	if expected := tileCount(ValleyWidth, ValleyHeight); len(parts.Tiles) != expected {
		return nil, TileCountError{Expected: expected, Found: len(parts.Tiles)}
	}
	rng := NewSplitMix64(parts.Tick)
	if len(parts.RngState) != 0 {
		var ok bool
		if rng, ok = SplitMix64FromBytes(parts.RngState); !ok {
			return nil, RngStateError{Len: len(parts.RngState)}
		}
	}
	players := make(map[string]*playerSlot, len(parts.Players))
	for _, source := range parts.Players {
		record := clonePlayer(source)
		// Estable, como sort_by_key de Rust: los empates conservan su orden.
		slices.SortStableFunc(record.RecentActions, func(a, b AppliedAction) int {
			switch {
			case a.Tick < b.Tick:
				return -1
			case a.Tick > b.Tick:
				return 1
			default:
				return 0
			}
		})
		recentIDs := make(map[string]struct{}, len(record.RecentActions))
		for _, applied := range record.RecentActions {
			recentIDs[applied.ActionID] = struct{}{}
		}
		if _, repeated := players[record.PlayerID]; repeated {
			return nil, DuplicatePlayerError{PlayerID: record.PlayerID}
		}
		players[record.PlayerID] = &playerSlot{record: record, recentIDs: recentIDs}
	}
	tiles := make([]Tile, len(parts.Tiles))
	for i, tile := range parts.Tiles {
		tiles[i] = cloneTile(tile)
	}
	return &World{
		catalog:             catalog,
		tick:                parts.Tick,
		epoch:               parts.Epoch,
		demoMode:            parts.DemoMode,
		humidityContributed: parts.HumidityContributed,
		width:               ValleyWidth,
		height:              ValleyHeight,
		tiles:               tiles,
		players:             players,
		rng:                 rng,
	}, nil
}

// Parts es una copia del estado completo, para la instantánea (World::parts,
// world.rs:196). No comparte memoria con el mundo.
func (w *World) Parts() WorldParts {
	tiles := make([]Tile, len(w.tiles))
	for i, tile := range w.tiles {
		tiles[i] = cloneTile(tile)
	}
	var players []Player
	for _, player := range w.Players() {
		players = append(players, clonePlayer(*player))
	}
	return WorldParts{
		Tick:                w.tick,
		Epoch:               w.epoch,
		CatalogVersion:      w.catalog.Version,
		RngState:            w.rng.Bytes(),
		DemoMode:            w.demoMode,
		HumidityContributed: w.humidityContributed,
		Width:               w.width,
		Height:              w.height,
		Tiles:               tiles,
		Players:             players,
	}
}

// cloneTile copia la casilla y lo que apunta (fuego e instalación).
func cloneTile(tile Tile) Tile {
	if tile.Fire != nil {
		fire := *tile.Fire
		tile.Fire = &fire
	}
	if tile.Installation != nil {
		installation := *tile.Installation
		tile.Installation = &installation
	}
	return tile
}

// clonePlayer copia al jugador y sus colecciones; las vacías pasan a nil.
func clonePlayer(player Player) Player {
	player.Purchases = nilIfEmpty(slices.Clone(player.Purchases))
	player.Upgrades = nilIfEmpty(slices.Clone(player.Upgrades))
	player.RecentActions = nilIfEmpty(slices.Clone(player.RecentActions))
	return player
}

func nilIfEmpty[S ~[]E, E any](items S) S {
	if len(items) == 0 {
		return nil
	}
	return items
}
