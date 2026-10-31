package domain

// Command es un comando admitido por el hub para este tick.
//
// Tipos del dominio, sin nada de gRPC: la traducción desde y hacia los
// mensajes del contrato vive en internal/codec.
type Command struct {
	ActionID string
	PlayerID string
	// Kind es nil cuando el mensaje llegó sin tipo.
	Kind CommandKind
}

// CommandKind son los tipos de comando del contrato: DropCommand,
// PlaceInstallationCommand, BuyUpgradeCommand, SetDemoModeCommand y
// ResetValleyCommand.
type CommandKind interface {
	isCommandKind()
}

// DropCommand suelta una gota sobre la casilla (X, Y).
type DropCommand struct{ X, Y uint32 }

// PlaceInstallationCommand compra y coloca una instalación.
type PlaceInstallationCommand struct {
	Kind InstallationKind
	X, Y uint32
}

// BuyUpgradeCommand compra una mejora.
type BuyUpgradeCommand struct{ Kind UpgradeKind }

// SetDemoModeCommand enciende o apaga el modo demo (administración).
type SetDemoModeCommand struct{ Enabled bool }

// ResetValleyCommand reinicia el valle (administración).
type ResetValleyCommand struct{}

// CleanTrashCommand limpia basura en la casilla (X, Y).
type CleanTrashCommand struct{ X, Y uint32 }

func (DropCommand) isCommandKind()              {}
func (PlaceInstallationCommand) isCommandKind() {}
func (BuyUpgradeCommand) isCommandKind()        {}
func (SetDemoModeCommand) isCommandKind()       {}
func (ResetValleyCommand) isCommandKind()       {}
func (CleanTrashCommand) isCommandKind()        {}

// OutcomeKind es el resultado final de un comando.
type OutcomeKind uint8

// Los resultados finales de un comando.
const (
	// Applied: el comando se aplicó.
	Applied OutcomeKind = iota
	// Duplicate: ya se había aplicado dentro de la ventana de action_id
	// recientes.
	Duplicate
	// Rejected: no se aplicó y no se cobró nada; el motivo va en Reason.
	Rejected
)

// RejectReason es el motivo del rechazo. El valor cero (NoReason) acompaña a
// los resultados que no son Rejected.
type RejectReason uint8

// Los motivos de rechazo.
const (
	NoReason RejectReason = iota
	InsufficientWater
	OutOfBounds
	// InsufficientLeaves: no alcanzan las hojas para la instalación o la
	// mejora.
	InsufficientLeaves
	// TileOccupied: la casilla ya tiene una instalación (como mucho una por
	// casilla).
	TileOccupied
	// UpgradeAlreadyOwned: la mejora (de compra única) ya se había comprado.
	UpgradeAlreadyOwned
	// InvalidCommand: sin tipo, con un valor de enum desconocido o con
	// identificadores que no son UUID.
	InvalidCommand
	// WorldFull: el mundo ya tiene MaxPlayers jugadores y el comando daría
	// de alta a uno nuevo.
	WorldFull
)

// CommandOutcome es el resultado de aplicar un comando.
type CommandOutcome struct {
	Kind   OutcomeKind
	Reason RejectReason
}

// Los resultados que no llevan motivo, y el constructor de los rechazos.
var (
	AppliedOutcome   = CommandOutcome{Kind: Applied}
	DuplicateOutcome = CommandOutcome{Kind: Duplicate}
)

// RejectedOutcome es el resultado de un comando rechazado por reason.
func RejectedOutcome(reason RejectReason) CommandOutcome {
	return CommandOutcome{Kind: Rejected, Reason: reason}
}

// CommandResult es el resultado de un comando, en el mismo orden que la
// petición.
type CommandResult struct {
	ActionID string
	PlayerID string
	Outcome  CommandOutcome
}

// StepOutcome es lo que produce un paso del valle.
type StepOutcome struct {
	// Tick es el tick calculado en este paso.
	Tick    uint64
	Results []CommandResult
}
