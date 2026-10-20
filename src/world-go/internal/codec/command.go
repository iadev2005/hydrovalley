package codec

import (
	worldv1 "github.com/iadev2005/hydrovalley/gen/go/hydrovalley/world/v1"

	"github.com/iadev2005/hydrovalley/src/world-go/internal/domain"
)

// CommandFromProto convierte un comando del contrato en el del dominio
// (command_from_proto de Rust).
//
// Un tipo de instalación o de mejora desconocido o sin especificar deja el
// comando sin tipo (Kind nil): el simulador lo rechaza como INVALID_COMMAND.
// Un mensaje sin la oneof también. Un mensaje interno ausente es el mensaje
// por omisión (como prost): una gota sin campos cae en (0, 0).
func CommandFromProto(message *worldv1.Command) domain.Command {
	command := domain.Command{
		ActionID: message.GetActionId(),
		PlayerID: message.GetPlayerId(),
	}
	switch kind := message.GetKind().(type) {
	case *worldv1.Command_Drop:
		command.Kind = domain.DropCommand{X: kind.Drop.GetX(), Y: kind.Drop.GetY()}
	case *worldv1.Command_PlaceInstallation:
		installation, err := installationFromProto(kind.PlaceInstallation.GetKind(), "PlaceInstallationCommand.kind")
		if err == nil {
			command.Kind = domain.PlaceInstallationCommand{
				Kind: installation,
				X:    kind.PlaceInstallation.GetX(),
				Y:    kind.PlaceInstallation.GetY(),
			}
		}
	case *worldv1.Command_BuyUpgrade:
		upgrade, err := upgradeFromProto(kind.BuyUpgrade.GetKind())
		if err == nil {
			command.Kind = domain.BuyUpgradeCommand{Kind: upgrade}
		}
	case *worldv1.Command_SetDemoMode:
		command.Kind = domain.SetDemoModeCommand{Enabled: kind.SetDemoMode.GetEnabled()}
	case *worldv1.Command_ResetValley:
		command.Kind = domain.ResetValleyCommand{}
	}
	return command
}

// ResultToProto convierte el resultado de un comando en el del contrato
// (result_to_proto de Rust).
func ResultToProto(result domain.CommandResult) *worldv1.CommandResult {
	status, reason := worldv1.CommandStatus_COMMAND_STATUS_UNSPECIFIED, worldv1.RejectReason_REJECT_REASON_UNSPECIFIED
	switch result.Outcome.Kind {
	case domain.Applied:
		status = worldv1.CommandStatus_COMMAND_STATUS_APPLIED
	case domain.Duplicate:
		status = worldv1.CommandStatus_COMMAND_STATUS_DUPLICATE
	case domain.Rejected:
		status = worldv1.CommandStatus_COMMAND_STATUS_REJECTED
		reason = rejectReason(result.Outcome.Reason)
	}
	return &worldv1.CommandResult{
		ActionId: result.ActionID,
		PlayerId: result.PlayerID,
		Status:   status,
		Reason:   reason,
	}
}

func rejectReason(reason domain.RejectReason) worldv1.RejectReason {
	switch reason {
	case domain.InsufficientWater:
		return worldv1.RejectReason_REJECT_REASON_INSUFFICIENT_WATER
	case domain.OutOfBounds:
		return worldv1.RejectReason_REJECT_REASON_OUT_OF_BOUNDS
	case domain.InsufficientLeaves:
		return worldv1.RejectReason_REJECT_REASON_INSUFFICIENT_LEAVES
	case domain.TileOccupied:
		return worldv1.RejectReason_REJECT_REASON_TILE_OCCUPIED
	case domain.UpgradeAlreadyOwned:
		return worldv1.RejectReason_REJECT_REASON_UPGRADE_ALREADY_OWNED
	case domain.InvalidCommand:
		return worldv1.RejectReason_REJECT_REASON_INVALID_COMMAND
	case domain.WorldFull:
		// Tampoco hay motivo para «mundo lleno»: INVALID_COMMAND es el más
		// cercano (el comando no se puede aplicar tal como llega).
		return worldv1.RejectReason_REJECT_REASON_INVALID_COMMAND
	default:
		return worldv1.RejectReason_REJECT_REASON_UNSPECIFIED
	}
}
