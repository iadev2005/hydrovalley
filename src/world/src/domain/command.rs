//! Comandos que el hub admite y resultados que decide el simulador.
//!
//! Tipos del dominio, sin nada de gRPC: la traducción desde y hacia los
//! mensajes del contrato vive en `crate::codec`.

use super::tile::{InstallationKind, UpgradeKind};

/// Comando admitido por el hub para este tick.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Command {
    pub action_id: String,
    pub player_id: String,
    /// `None` cuando el mensaje llegó sin tipo.
    pub kind: Option<CommandKind>,
}

/// Tipos de comando del contrato.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum CommandKind {
    Drop {
        x: u32,
        y: u32,
    },
    PlaceInstallation {
        kind: InstallationKind,
        x: u32,
        y: u32,
    },
    BuyUpgrade {
        kind: UpgradeKind,
    },
    SetDemoMode {
        enabled: bool,
    },
    ResetValley,
}

/// Resultado final de un comando.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum CommandOutcome {
    Applied,
    /// Ya se había aplicado dentro de la ventana de `action_id` recientes.
    Duplicate,
    /// No se aplicó y no se cobró nada.
    Rejected(RejectReason),
}

/// Motivo del rechazo.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum RejectReason {
    InsufficientWater,
    OutOfBounds,
    /// No alcanzan las hojas para la instalación o la mejora.
    InsufficientLeaves,
    /// La casilla ya tiene una instalación (como mucho una por casilla).
    TileOccupied,
    /// La mejora (de compra única) ya se había comprado.
    UpgradeAlreadyOwned,
    /// Sin tipo, con un valor de enum desconocido o con identificadores que
    /// no son UUID.
    InvalidCommand,
    /// El mundo ya tiene `MAX_PLAYERS` jugadores y el comando daría de alta
    /// a uno nuevo.
    WorldFull,
}

/// Resultado de un comando, en el mismo orden que la petición.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CommandResult {
    pub action_id: String,
    pub player_id: String,
    pub outcome: CommandOutcome,
}

/// Lo que produce un paso del valle.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct StepOutcome {
    /// Tick calculado en este paso.
    pub tick: u64,
    pub results: Vec<CommandResult>,
}
