package hub

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	worldv1 "github.com/iadev2005/hydrovalley/gen/go/hydrovalley/world/v1"
	"github.com/iadev2005/hydrovalley/src/hub/internal/breaker"
	"github.com/iadev2005/hydrovalley/src/hub/internal/frames"
)

var errInvalidResponse = errors.New("invalid Step response")

// Tick ejecuta una vuelta del bucle:
//  1. responde "expired" a los comandos con más de Config.ExpireAfter en la
//     cola;
//  2. si el Circuit Breaker lo permite, saca hasta Config.BatchSize comandos
//     y llama a Step (sin el candado, con plazo Config.StepTimeout);
//  3. con éxito, reparte los resultados finales; con fallo, devuelve los
//     comandos al frente de la cola y pausa el valle;
//  4. difunde el estado si toca (cada Config.BroadcastEvery).
func (h *Hub) Tick(ctx context.Context) {
	h.mu.Lock()
	h.expireLocked()
	if !h.breaker.Allow(h.clock.Now()) {
		h.sweepIdlePlayersLocked()
		h.maybeBroadcastLocked()
		h.mu.Unlock()
		return
	}
	n := min(len(h.queue), h.cfg.BatchSize)
	batch := slices.Clone(h.queue[:n])
	h.queue = slices.Delete(h.queue, 0, n)
	h.mu.Unlock()

	cmds := make([]*worldv1.Command, len(batch))
	for i, q := range batch {
		cmds[i] = q.cmd
	}
	stepCtx, cancel := context.WithTimeout(ctx, h.cfg.StepTimeout)
	resp, err := h.world.Step(stepCtx, &worldv1.StepRequest{Commands: cmds})
	cancel()
	if err == nil {
		err = validateResponse(cmds, resp)
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	if err != nil {
		h.stepFailedLocked(batch, err)
	} else {
		h.stepSucceededLocked(batch, resp)
	}
	h.sweepIdlePlayersLocked()
	h.maybeBroadcastLocked()
}

// validateResponse comprueba el contrato: exactamente un resultado por
// comando, en el mismo orden y con un estado final conocido. Una respuesta
// que no lo cumple se trata como un Step fallido; si Rust ya había aplicado
// algo, el reintento le llegará como DUPLICATE.
func validateResponse(cmds []*worldv1.Command, resp *worldv1.StepResponse) error {
	if len(resp.GetResults()) != len(cmds) {
		return fmt.Errorf("%w: %d results for %d commands", errInvalidResponse, len(resp.GetResults()), len(cmds))
	}
	for i, r := range resp.GetResults() {
		if r.GetActionId() != cmds[i].GetActionId() || r.GetPlayerId() != cmds[i].GetPlayerId() {
			return fmt.Errorf("%w: result %d does not match its command", errInvalidResponse, i)
		}
		switch r.GetStatus() {
		case worldv1.CommandStatus_COMMAND_STATUS_APPLIED,
			worldv1.CommandStatus_COMMAND_STATUS_DUPLICATE,
			worldv1.CommandStatus_COMMAND_STATUS_REJECTED:
		default:
			return fmt.Errorf("%w: result %d has status %v", errInvalidResponse, i, r.GetStatus())
		}
	}
	return nil
}

// expireLocked quita de la cola los comandos con más de ExpireAfter desde
// su admisión y les responde "expired" (solo salen de pending).
func (h *Hub) expireLocked() {
	now := h.clock.Now()
	kept := h.queue[:0]
	for _, q := range h.queue {
		if now.Sub(q.admittedAt) > h.cfg.ExpireAfter {
			h.finishLocked(q.cmd.GetPlayerId(), q.cmd.GetActionId(), frames.StatusExpired, worldv1.RejectReason_REJECT_REASON_UNSPECIFIED)
			continue
		}
		kept = append(kept, q)
	}
	clear(h.queue[len(kept):])
	h.queue = kept
}

// stepFailedLocked devuelve el lote al frente de la cola (sigue en pending)
// y pausa el valle desde el primer fallo.
func (h *Hub) stepFailedLocked(batch []queued, err error) {
	for i := range batch {
		batch[i].inFailedStep = true
	}
	h.queue = append(batch, h.queue...)
	h.paused = true
	opened := h.breaker.Failure(h.clock.Now())
	h.log.Warn("Step failed", "err", err, "requeued", len(batch), "breaker", h.breaker.State().String())
	if opened {
		h.log.Error("circuit breaker open: Step is no longer called until a probe succeeds",
			"probe_interval", h.cfg.ProbeInterval)
		// Aviso a los jugadores: un estado con paused = true, sin esperar a
		// la difusión de cada segundo.
		h.broadcastLocked()
	}
}

// stepSucceededLocked reparte los resultados de un Step correcto. batch es
// el lote enviado; validateResponse ya comprobó que resp trae un resultado
// por comando y en el mismo orden.
//
// Regla del DUPLICATE tras un plazo vencido: si Step falla después de que
// Rust aplicara el lote (p. ej. vence el plazo de 200 ms), el reintento
// vuelve como DUPLICATE. Si el comando viajó en un Step fallido y el hub no
// lo tenía en confirmed, esa es su primera confirmación: se trata como
// APPLIED (el jugador recibe "applied", entra en confirmed con el tick de
// esta respuesta y se cuenta en las gotas aceptadas de ese tick). Un
// DUPLICATE de un comando que nunca estuvo en un Step fallido, o que el hub
// ya tenía en confirmed, sigue siendo "duplicate" y no se cuenta.
func (h *Hub) stepSucceededLocked(batch []queued, resp *worldv1.StepResponse) {
	if h.breaker.State() != breaker.Closed {
		h.log.Info("circuit breaker closed", "tick", resp.GetTick())
	}
	h.breaker.Success()
	h.paused = false
	h.succeeded = true
	h.observeBootLocked(resp.GetBootId(), resp.GetRestoredTick())
	h.tick = resp.GetTick()
	h.latestWorld = resp.GetState()

	for i, r := range resp.GetResults() {
		playerID, actionID := r.GetPlayerId(), r.GetActionId()
		status := r.GetStatus()
		if status == worldv1.CommandStatus_COMMAND_STATUS_DUPLICATE && batch[i].inFailedStep && !h.confirmedLocked(playerID, actionID) {
			status = worldv1.CommandStatus_COMMAND_STATUS_APPLIED
		}
		switch status {
		case worldv1.CommandStatus_COMMAND_STATUS_APPLIED:
			h.confirmLocked(playerID, actionID, h.tick)
			h.ledger[h.tick]++
			h.acceptedDrops++
			h.finishLocked(playerID, actionID, frames.StatusApplied, worldv1.RejectReason_REJECT_REASON_UNSPECIFIED)
		case worldv1.CommandStatus_COMMAND_STATUS_DUPLICATE:
			h.confirmLocked(playerID, actionID, h.tick)
			h.finishLocked(playerID, actionID, frames.StatusDuplicate, worldv1.RejectReason_REJECT_REASON_UNSPECIFIED)
		default: // REJECTED, ya validado.
			h.finishLocked(playerID, actionID, frames.StatusRejected, r.GetReason())
		}
	}
	h.pruneWindowsLocked()
}

// observeBootLocked aplica la recuperación una sola vez por arranque de
// Rust: el primer boot_id es la línea base; cuando cambia, se quitan de
// confirmed las entradas con tick > restored_tick y se restan las gotas
// aceptadas en esos ticks (CP-05).
func (h *Hub) observeBootLocked(bootID string, restored uint64) {
	if bootID == h.bootID {
		return
	}
	previous := h.bootID
	h.bootID = bootID
	if previous == "" {
		h.log.Info("world boot observed", "boot_id", bootID, "restored_tick", restored)
		return
	}
	pruned := 0
	kept := h.confirmLog[:0]
	for _, e := range h.confirmLog {
		if e.tick > restored {
			if p, ok := h.players[e.playerID]; ok && p.confirmed[e.actionID] == e.tick {
				delete(p.confirmed, e.actionID)
				h.dropIfIdleLocked(e.playerID, p)
			}
			pruned++
			continue
		}
		kept = append(kept, e)
	}
	clear(h.confirmLog[len(kept):])
	h.confirmLog = kept

	var lost uint64
	for t, n := range h.ledger {
		if t > restored {
			lost += n
			delete(h.ledger, t)
		}
	}
	h.acceptedDrops -= lost
	h.log.Warn("world restarted: recovery applied",
		"previous_boot_id", previous, "boot_id", bootID, "restored_tick", restored,
		"confirmed_pruned", pruned, "accepted_drops_lost", lost)
}

func (h *Hub) confirmedLocked(playerID, actionID string) bool {
	p, ok := h.players[playerID]
	return ok && hasKey(p.confirmed, actionID)
}

func (h *Hub) confirmLocked(playerID, actionID string, tick uint64) {
	p := h.playerLocked(playerID)
	p.confirmed[actionID] = tick
	h.confirmLog = append(h.confirmLog, confirmEntry{playerID: playerID, actionID: actionID, tick: tick})
}

// finishLocked envía un resultado final a todas las conexiones del jugador
// y quita la acción de pending.
func (h *Hub) finishLocked(playerID, actionID, status string, reason worldv1.RejectReason) {
	p, ok := h.players[playerID]
	if !ok {
		return
	}
	delete(p.pending, actionID)
	msg := frames.Result(actionID, status, reason)
	for s := range p.sessions {
		s.Send(msg)
	}
	h.dropIfIdleLocked(playerID, p)
}

// pruneWindowsLocked saca de confirmed lo aplicado hace más de 600 ticks
// (Rust lo considera DUPLICATE hasta t + 600 incluido) y de la cuenta por
// tick lo que quedó fuera de su ventana. confirmLog está en orden de tick.
func (h *Hub) pruneWindowsLocked() {
	i := 0
	for ; i < len(h.confirmLog); i++ {
		e := h.confirmLog[i]
		if e.tick+h.cfg.ConfirmedTicks >= h.tick {
			break
		}
		if p, ok := h.players[e.playerID]; ok && p.confirmed[e.actionID] == e.tick {
			delete(p.confirmed, e.actionID)
			h.dropIfIdleLocked(e.playerID, p)
		}
	}
	if i > 0 {
		h.confirmLog = slices.Delete(h.confirmLog, 0, i)
	}
	for t := range h.ledger {
		if t+h.cfg.LedgerTicks < h.tick {
			delete(h.ledger, t)
		}
	}
}

func (h *Hub) dropIfIdleLocked(id string, p *player) {
	if p.idle(h.clock.Now()) {
		delete(h.players, id)
	}
}

// sweepIdlePlayersLocked borra los registros que quedaron inactivos solo
// porque su ventana del límite se vació con el paso del tiempo. Recorre
// todos los jugadores bajo el candado, así que corre como mucho una vez por
// SweepInterval y no en cada tick; el resto de bajas las hace
// dropIfIdleLocked en cuanto ocurre el evento.
func (h *Hub) sweepIdlePlayersLocked() {
	now := h.clock.Now()
	if !h.lastSweep.IsZero() && now.Sub(h.lastSweep) < h.cfg.SweepInterval {
		return
	}
	h.lastSweep = now
	for id, p := range h.players {
		if p.idle(now) {
			delete(h.players, id)
		}
	}
}

func (h *Hub) maybeBroadcastLocked() {
	now := h.clock.Now()
	if !h.lastBroadcast.IsZero() && now.Sub(h.lastBroadcast) < h.cfg.BroadcastEvery {
		return
	}
	h.broadcastLocked()
}

// broadcastLocked difunde el estado a todas las conexiones (RF-03). El
// mapa se calcula una vez y cada conexión recibe la vista de su jugador. La
// cuenta de gotas aceptadas no tiene campo en StateMessage: se registra.
func (h *Hub) broadcastLocked() {
	h.lastBroadcast = h.clock.Now()
	builder := frames.NewStateBuilder(h.tick, h.paused, h.latestWorld)
	for _, p := range h.players {
		for s := range p.sessions {
			s.Send(builder.For(s.playerID, s.withMap))
		}
	}
	if h.acceptedDrops != h.loggedDrops {
		h.loggedDrops = h.acceptedDrops
		h.log.Info("accepted drops", "tick", h.tick, "accepted_drops", h.acceptedDrops,
			"humidity_contributed", h.latestWorld.GetHumidityContributed(), "demo_mode", h.latestWorld.GetDemoMode())
	}
	h.log.Debug("state broadcast", slog.Uint64("tick", h.tick), slog.Bool("paused", h.paused), slog.Int("queue", len(h.queue)))
}
