package server

import (
	"context"
	"errors"
	"log/slog"

	worldv1 "github.com/iadev2005/hydrovalley/gen/go/hydrovalley/world/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/iadev2005/hydrovalley/src/world-go/internal/codec"
	"github.com/iadev2005/hydrovalley/src/world-go/internal/domain"
	"github.com/iadev2005/hydrovalley/src/world-go/internal/requestid"
	"github.com/iadev2005/hydrovalley/src/world-go/internal/simulator"
)

// service implementa WorldService (la frontera gRPC). Solo traduce: mensajes
// del contrato, dominio, mensajes del contrato.
type service struct {
	worldv1.UnimplementedWorldServiceServer
	sim *simulator.Simulator
	log *slog.Logger
}

func notReady() error {
	return status.Error(codes.Unavailable, simulator.ErrNotReady.Error())
}

// Step aplica en orden los comandos de un tick y avanza el valle.
//
// El id de petición (RNF-07) es el metadato x-request-id que envía el hub
// (tick-<n>); va como campo request_id de cada línea del registro de la
// llamada. Todo el registro del camino caliente es a nivel debug: con info el
// Step no escribe nada.
func (s *service) Step(ctx context.Context, request *worldv1.StepRequest) (*worldv1.StepResponse, error) {
	id := requestid.FromContext(ctx)
	if err := s.sim.EnsureActiveAndFresh(request.GetMinExpectedTick()); errors.Is(err, simulator.ErrNotReady) {
		return nil, notReady()
	} else if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	commands := make([]domain.Command, len(request.GetCommands()))
	for i, command := range request.GetCommands() {
		commands[i] = codec.CommandFromProto(command)
	}
	// Solo con un ResetValley vuelve a pedir el catálogo (RF-31).
	fresh := s.sim.CatalogFor(ctx, commands)
	var state *worldv1.WorldState
	report, err := s.sim.Step(commands, fresh, func(world *domain.World) { state = codec.WorldState(world) })
	if errors.Is(err, simulator.ErrNotReady) {
		return nil, notReady()
	}
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	results := make([]*worldv1.CommandResult, len(report.Outcome.Results))
	for i, result := range report.Outcome.Results {
		results[i] = codec.ResultToProto(result)
	}
	s.logStep(ctx, id, report.Outcome.Tick, results)
	return &worldv1.StepResponse{
		Tick:         report.Outcome.Tick,
		RestoredTick: report.RestoredTick,
		Results:      results,
		State:        state,
		BootId:       report.BootID,
	}, nil
}

// logStep registra el paso y cada comando a nivel debug. Sin debug activado ni
// siquiera se recorren los resultados.
func (s *service) logStep(ctx context.Context, id string, tick uint64, results []*worldv1.CommandResult) {
	if !s.log.Enabled(ctx, slog.LevelDebug) {
		return
	}
	s.log.Debug("Step", "request_id", id, "tick", tick, "commands", len(results))
	for _, result := range results {
		s.log.Debug("comando", "request_id", id, "action_id", result.GetActionId(),
			"outcome", result.GetStatus().String(), "reason", result.GetReason().String())
	}
}

// GetRanking (RF-10 y RF-11) solo lee: copia los puntajes con el bloqueo
// tomado y ordena después, con el bloqueo ya suelto, así Step no espera.
func (s *service) GetRanking(ctx context.Context, request *worldv1.GetRankingRequest) (*worldv1.GetRankingResponse, error) {
	id := requestid.FromContext(ctx)
	s.log.Info("GetRanking", "request_id", id, "limit", request.GetLimit())
	var (
		tick   uint64
		scores []domain.Score
	)
	if err := s.sim.Read(func(world *domain.World) {
		tick, scores = world.Tick(), world.Scores()
	}); err != nil {
		return nil, notReady()
	}
	ranked := domain.RankPlayers(scores, request.GetLimit())
	entries := make([]*worldv1.RankingEntry, len(ranked))
	for i, entry := range ranked {
		entries[i] = &worldv1.RankingEntry{Rank: entry.Rank, PlayerId: entry.PlayerID, LeavesEarned: entry.LeavesEarned}
	}
	return &worldv1.GetRankingResponse{Tick: tick, Entries: entries}, nil
}
