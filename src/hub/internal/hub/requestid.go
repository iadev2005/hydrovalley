package hub

import (
	"strconv"

	worldv1 "github.com/iadev2005/hydrovalley/gen/go/hydrovalley/world/v1"
)

// requestIDKey es el metadato gRPC con el identificador de petición
// (RNF-07). El simulador lo lee y lo pone en todas las líneas del Step.
const requestIDKey = "x-request-id"

// stepRequestID es el identificador del Step que calculará el tick
// siguiente al último conocido: «tick-<n>». Si Rust se reinició desde una
// instantánea anterior, el tick real puede diferir; el id solo sirve para
// encontrar el mismo Step en los registros del hub y del simulador.
func stepRequestID(lastTick uint64) string {
	return "tick-" + strconv.FormatUint(lastTick+1, 10)
}

// logOutcome registra, a nivel debug, la decisión de admisión de una acción
// con su action_id (el identificador de petición del juego, RNF-07). A info
// no se escribe nada por acción: cien gotas por segundo inundarían el
// registro.
func (h *Hub) logOutcome(playerID, actionID string, o Outcome) {
	if !h.debug {
		return
	}
	switch o {
	case Admitted:
		h.log.Debug("action admitted", "action_id", actionID, "player_id", playerID)
	case IgnoredPending:
		h.log.Debug("action ignored", "action_id", actionID, "player_id", playerID, "outcome", o.String())
	default:
		h.log.Debug("action rejected", "action_id", actionID, "player_id", playerID, "outcome", o.String())
	}
}

// logResult registra, a nivel debug, el resultado final de una acción.
func (h *Hub) logResult(playerID, actionID, status string, reason worldv1.RejectReason) {
	if !h.debug {
		return
	}
	if reason == worldv1.RejectReason_REJECT_REASON_UNSPECIFIED {
		h.log.Debug("action result", "action_id", actionID, "player_id", playerID, "status", status, "tick", h.tick)
		return
	}
	h.log.Debug("action result", "action_id", actionID, "player_id", playerID, "status", status, "reason", reason.String(), "tick", h.tick)
}
