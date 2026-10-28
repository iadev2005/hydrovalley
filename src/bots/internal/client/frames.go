package client

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// Los marcos de /play son JSON plano con un campo `type` (ver
// proto/hydrovalley/play/v1/play.proto). Se escriben a mano en vez de
// generarlos desde el .proto: son pocos y planos, y así el módulo de los
// bots no depende de buf, de gen/ ni de protobuf, y su imagen se construye
// sin la etapa de generación de código.

// Tipos de comando de administración: no se reenvían al reconectarse.
const (
	typeDrop        = "drop"
	typePlace       = "place_installation"
	typeUpgrade     = "buy_upgrade"
	typeSetDemoMode = "set_demo_mode"
	typeResetValley = "reset_valley"
)

func isAdminType(t string) bool { return t == typeSetDemoMode || t == typeResetValley }

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err) // solo estructuras propias, siempre serializables
	}
	return b
}

func encodeDrop(id string, x, y uint32) []byte {
	return mustJSON(struct {
		Type     string `json:"type"`
		ActionID string `json:"action_id"`
		X        uint32 `json:"x"`
		Y        uint32 `json:"y"`
	}{typeDrop, id, x, y})
}

func encodePlace(id, installation string, x, y uint32) []byte {
	return mustJSON(struct {
		Type         string `json:"type"`
		ActionID     string `json:"action_id"`
		Installation string `json:"installation"`
		X            uint32 `json:"x"`
		Y            uint32 `json:"y"`
	}{typePlace, id, installation, x, y})
}

func encodeUpgrade(id, upgrade string) []byte {
	return mustJSON(struct {
		Type     string `json:"type"`
		ActionID string `json:"action_id"`
		Upgrade  string `json:"upgrade"`
	}{typeUpgrade, id, upgrade})
}

func encodeDemoMode(id string, enabled bool) []byte {
	return mustJSON(struct {
		Type     string `json:"type"`
		ActionID string `json:"action_id"`
		Enabled  bool   `json:"enabled"`
	}{typeSetDemoMode, id, enabled})
}

func encodeReset(id string) []byte {
	return mustJSON(struct {
		Type     string `json:"type"`
		ActionID string `json:"action_id"`
	}{typeResetValley, id})
}

// U64 es un entero sin signo de 64 bits que viaja como texto en JSON
// (regla 7 de CLAUDE.md); también acepta un número.
type U64 uint64

// UnmarshalJSON lee "123" o 123.
func (u *U64) UnmarshalJSON(b []byte) error {
	s := string(b)
	if len(s) >= 2 && s[0] == '"' {
		var err error
		if s, err = strconv.Unquote(s); err != nil {
			return err
		}
	}
	v, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid uint64 %q", string(b))
	}
	*u = U64(v)
	return nil
}

// Fire es una casilla ardiendo.
type Fire struct {
	X             uint32 `json:"x"`
	Y             uint32 `json:"y"`
	DropsReceived uint32 `json:"drops_received"`
}

// Installation es una instalación colocada.
type Installation struct {
	X            uint32 `json:"x"`
	Y            uint32 `json:"y"`
	Installation string `json:"installation"`
	OwnerID      string `json:"owner_id"`
}

// Player es la vista del propio jugador dentro de un State.
type Player struct {
	Water                   U64      `json:"water"`
	TankCapacity            U64      `json:"tank_capacity"`
	Leaves                  U64      `json:"leaves"`
	Upgrades                []string `json:"upgrades"`
	NextSprinklerPrice      U64      `json:"next_sprinkler_price"`
	NextDripPrice           U64      `json:"next_drip_price"`
	NextTreatmentPlantPrice U64      `json:"next_treatment_plant_price"`
}

// State es el StateMessage que el hub difunde una vez por segundo. Stages y
// Humidity van por filas (índice = y*Width + x) y solo llegan con map=true.
type State struct {
	Tick          U64            `json:"tick"`
	Paused        bool           `json:"paused"`
	DemoMode      bool           `json:"demo_mode"`
	Width         uint32         `json:"width"`
	Height        uint32         `json:"height"`
	Stages        []uint32       `json:"stages"`
	Humidity      []U64          `json:"humidity"`
	Fires         []Fire         `json:"fires"`
	Installations []Installation `json:"installations"`
	// Buildings y Trash marcan, por casilla, los edificios y el nivel de
	// basura (0 a 3); también solo llegan con map=true.
	Buildings []bool   `json:"buildings"`
	Trash     []uint32 `json:"trash"`
	Player    *Player  `json:"player"`
	// HumidityContributed y AcceptedDrops son los contadores del modo demo
	// (RF-30): llegan siempre, también con map=false, y un cero se omite.
	HumidityContributed U64 `json:"humidity_contributed"`
	AcceptedDrops       U64 `json:"accepted_drops"`
}

type envelope struct {
	Type string `json:"type"`
}

type resultFrame struct {
	ActionID string `json:"action_id"`
	Status   string `json:"status"`
	Reason   string `json:"reason"`
}

type errorFrame struct {
	ActionID string `json:"action_id"`
	Code     string `json:"code"`
}

func jsonUnmarshal(data []byte, v any) error { return json.Unmarshal(data, v) }

// Etapas de hydrovalley.world.v1.TileStage que el simulador trata como
// terreno natural.
const (
	stageWater = 6
	stageRock  = 7
)

// AcceptsDrop indica si el simulador admitiría una gota en la casilla i
// (índice = y*Width + x): el agua, la roca, los edificios y las casillas con
// basura la rechazan con `tile_occupied`. Sin mapa no se sabe, y devuelve true.
func (s State) AcceptsDrop(i int) bool {
	if i < len(s.Stages) && (s.Stages[i] == stageWater || s.Stages[i] == stageRock) {
		return false
	}
	if i < len(s.Buildings) && s.Buildings[i] {
		return false
	}
	return i >= len(s.Trash) || s.Trash[i] == 0
}
