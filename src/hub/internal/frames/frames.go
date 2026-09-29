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
	TypeDrop   = "drop"
	TypeResult = "result"
	TypeError  = "error"
	TypeState  = "state"
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

// Command es un mensaje del navegador ya validado: exactamente uno de los
// dos campos está presente.
type Command struct {
	Drop  *Drop
	Admin *Admin
}

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
// Se admiten `drop` y los comandos de administración (`set_demo_mode` y
// `reset_valley`); si el jugador puede ejecutarlos lo decide el hub con el
// rol del token, no este decodificador. Los demás comandos de play.proto
// (place_installation, buy_upgrade) responden `unknown_type` hasta que el hub
// los soporte: el contrato no tiene un código para «no soportado».
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
