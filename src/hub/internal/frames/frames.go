// Package frames traduce los mensajes JSON del WebSocket /play
// (hydrovalley.play.v1) a y desde los tipos del hub.
//
// Convención de play.proto: cada mensaje es protojson plano con los nombres
// del .proto y un campo de texto `type`. Se decodifica primero como
// Envelope, se lee `type` y luego se decodifica como ese mensaje.
package frames

import (
	"encoding/json"
	"strings"

	playv1 "github.com/iadev2005/hydrovalley/gen/go/hydrovalley/play/v1"
	worldv1 "github.com/iadev2005/hydrovalley/gen/go/hydrovalley/world/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// Valores de `type`.
const (
	TypeDrop       = "drop"
	TypeResult     = "result"
	TypeError      = "error"
	TypeState      = "state"
	TypeCleanTrash = "clean_trash"
)

// Valores de ResultMessage.status.
const (
	StatusApplied     = "applied"
	StatusDuplicate   = "duplicate"
	StatusRejected    = "rejected"
	StatusExpired     = "expired"
	StatusRateLimited = "rate_limited"
	StatusPaused      = "paused"
	StatusForbidden   = "forbidden"
)

// Valores de ErrorMessage.code (RF-06).
const (
	CodeInvalidMessage = "invalid_message"
	CodeUnknownType    = "unknown_type"
)

// Valores de `type` de los comandos de administración (RF-31).
const (
	TypeSetDemoMode = "set_demo_mode"
	TypeResetValley = "reset_valley"
)

// AdminKind distingue los comandos de administración.
type AdminKind int

// Comandos de administración de play.proto.
const (
	AdminSetDemoMode AdminKind = iota + 1
	AdminResetValley
)

// Admin es un comando de administración ya validado (action_id es un UUID).
// Enabled solo tiene sentido con AdminSetDemoMode.
type Admin struct {
	ActionID string
	Kind     AdminKind
	Enabled  bool
}

// Valores de `type` de las compras (RF-07).
const (
	TypePlaceInstallation = "place_installation"
	TypeBuyUpgrade        = "buy_upgrade"
)

// Command es un mensaje del navegador ya validado: exactamente uno de los
// campos está presente.
type Command struct {
	Drop       *Drop
	Admin      *Admin
	Place      *Place
	Buy        *Buy
	CleanTrash *CleanTrash
}

// CleanTrash es la limpieza de una casilla de basura validada (action_id es un UUID).
type CleanTrash struct {
	ActionID string
	X, Y     uint32
}

// Place es la compra y colocación de una instalación ya validada: action_id
// es un UUID y Kind uno de los tres tipos del contrato. Que las coordenadas
// estén dentro del valle lo decide el simulador.
type Place struct {
	ActionID string
	Kind     worldv1.InstallationKind
	X, Y     uint32
}

// Buy es la compra de una mejora ya validada: action_id es un UUID y Kind
// una de las dos mejoras del contrato.
type Buy struct {
	ActionID string
	Kind     worldv1.UpgradeKind
}

// Nombres de las instalaciones y de las mejoras en JSON (play.proto); el hub
// los traduce a los enums de world.proto.
var (
	installationKinds = map[string]worldv1.InstallationKind{
		"sprinkler":       worldv1.InstallationKind_INSTALLATION_KIND_SPRINKLER,
		"drip":            worldv1.InstallationKind_INSTALLATION_KIND_DRIP,
		"treatment_plant": worldv1.InstallationKind_INSTALLATION_KIND_TREATMENT_PLANT,
		"trash_bin":       worldv1.InstallationKind_INSTALLATION_KIND_TRASH_BIN,
	}
	upgradeKinds = map[string]worldv1.UpgradeKind{
		"bigger_drop": worldv1.UpgradeKind_UPGRADE_KIND_BIGGER_DROP,
		"bigger_tank": worldv1.UpgradeKind_UPGRADE_KIND_BIGGER_TANK,
	}
)

// Drop es una gota ya validada: action_id es un UUID.
type Drop struct {
	ActionID string
	X, Y     uint32
}

var (
	envelopeDecoder = protojson.UnmarshalOptions{DiscardUnknown: true}
	// Estricto: un campo desconocido en un comando es un mensaje mal formado.
	commandDecoder = protojson.UnmarshalOptions{}
	encoder        = protojson.MarshalOptions{UseProtoNames: true}
)

// DecodeClient interpreta un mensaje del navegador. Devuelve el comando o el
// mensaje de error que hay que responder (nunca los dos).
//
// Se admiten `drop`, las compras (`place_installation` y `buy_upgrade`) y los
// comandos de administración (`set_demo_mode` y `reset_valley`); si el
// jugador puede ejecutarlos lo decide el hub con el rol del token, no este
// decodificador. El nombre del tipo de instalación o de mejora debe ser
// exactamente uno de los del contrato (en minúsculas); si falta o es otro, el
// mensaje es `invalid_message`.
func DecodeClient(data []byte) (Command, *playv1.ErrorMessage) {
	var env playv1.Envelope
	if err := envelopeDecoder.Unmarshal(data, &env); err != nil || env.GetType() == "" {
		return Command{}, Error(CodeInvalidMessage, peekActionID(data))
	}
	invalid := func() (Command, *playv1.ErrorMessage) {
		return Command{}, Error(CodeInvalidMessage, peekActionID(data))
	}
	switch env.GetType() {
	case TypeDrop:
		var msg playv1.DropMessage
		if err := commandDecoder.Unmarshal(data, &msg); err != nil || !IsUUID(msg.GetActionId()) {
			return invalid()
		}
		return Command{Drop: &Drop{ActionID: msg.GetActionId(), X: msg.GetX(), Y: msg.GetY()}}, nil
	case TypePlaceInstallation:
		var msg playv1.PlaceInstallationMessage
		if err := commandDecoder.Unmarshal(data, &msg); err != nil || !IsUUID(msg.GetActionId()) {
			return invalid()
		}
		kind, ok := installationKinds[msg.GetInstallation()]
		if !ok {
			return invalid()
		}
		return Command{Place: &Place{ActionID: msg.GetActionId(), Kind: kind, X: msg.GetX(), Y: msg.GetY()}}, nil
	case TypeBuyUpgrade:
		var msg playv1.BuyUpgradeMessage
		if err := commandDecoder.Unmarshal(data, &msg); err != nil || !IsUUID(msg.GetActionId()) {
			return invalid()
		}
		kind, ok := upgradeKinds[msg.GetUpgrade()]
		if !ok {
			return invalid()
		}
		return Command{Buy: &Buy{ActionID: msg.GetActionId(), Kind: kind}}, nil
	case TypeSetDemoMode:
		var msg playv1.SetDemoModeMessage
		if err := commandDecoder.Unmarshal(data, &msg); err != nil || !IsUUID(msg.GetActionId()) {
			return invalid()
		}
		return Command{Admin: &Admin{ActionID: msg.GetActionId(), Kind: AdminSetDemoMode, Enabled: msg.GetEnabled()}}, nil
	case TypeResetValley:
		var msg playv1.ResetValleyMessage
		if err := commandDecoder.Unmarshal(data, &msg); err != nil || !IsUUID(msg.GetActionId()) {
			return invalid()
		}
		return Command{Admin: &Admin{ActionID: msg.GetActionId(), Kind: AdminResetValley}}, nil
	case TypeCleanTrash:
		var raw struct {
			ActionID string `json:"action_id"`
			X        uint32 `json:"x"`
			Y        uint32 `json:"y"`
		}
		if err := json.Unmarshal(data, &raw); err != nil || !IsUUID(raw.ActionID) {
			return invalid()
		}
		return Command{CleanTrash: &CleanTrash{ActionID: raw.ActionID, X: raw.X, Y: raw.Y}}, nil
	default:
		return Command{}, Error(CodeUnknownType, peekActionID(data))
	}
}

// peekActionID lee action_id cuando es un texto, aunque el resto del
// mensaje sea inválido (ErrorMessage lo lleva «cuando se pudo leer»).
func peekActionID(data []byte) string {
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil {
		return ""
	}
	var id string
	if json.Unmarshal(fields["action_id"], &id) != nil {
		return ""
	}
	return id
}

// Error construye un ErrorMessage (RF-06); actionID puede ir vacío.
func Error(code, actionID string) *playv1.ErrorMessage {
	return &playv1.ErrorMessage{Type: TypeError, ActionId: actionID, Code: code}
}

// Result construye un ResultMessage. reason solo se incluye si no es
// UNSPECIFIED (va con "rejected").
func Result(actionID, status string, reason worldv1.RejectReason) *playv1.ResultMessage {
	return &playv1.ResultMessage{Type: TypeResult, ActionId: actionID, Status: status, Reason: ReasonName(reason)}
}

// ReasonName traduce el motivo de rechazo a su texto en snake_case
// ("insufficient_water"); UNSPECIFIED da "".
func ReasonName(r worldv1.RejectReason) string {
	return enumName(r.String(), "REJECT_REASON_")
}

func installationName(k worldv1.InstallationKind) string {
	return enumName(k.String(), "INSTALLATION_KIND_")
}

func upgradeName(k worldv1.UpgradeKind) string {
	return enumName(k.String(), "UPGRADE_KIND_")
}

// enumName quita el prefijo del enum y pasa a minúsculas; UNSPECIFIED y los
// valores desconocidos (que protobuf imprime como números) dan "".
func enumName(full, prefix string) string {
	name, ok := strings.CutPrefix(full, prefix)
	if !ok || name == "UNSPECIFIED" {
		return ""
	}
	return strings.ToLower(name)
}

// Marshal codifica un mensaje del hub con los nombres del .proto.
func Marshal(m proto.Message) ([]byte, error) {
	return encoder.Marshal(m)
}

// IsUUID dice si s es un UUID en su forma canónica de texto
// (8-4-4-4-12 dígitos hexadecimales, en mayúsculas o minúsculas).
func IsUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i := range len(s) {
		c := s[i]
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !isHex(c) {
				return false
			}
		}
	}
	return true
}

func isHex(c byte) bool {
	return ('0' <= c && c <= '9') || ('a' <= c && c <= 'f') || ('A' <= c && c <= 'F')
}
