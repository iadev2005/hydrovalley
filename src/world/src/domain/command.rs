//! Comandos que el hub admite y resultados que decide el simulador.
//!
//! Tipos del dominio, sin nada de gRPC: la traducción desde y hacia los
//! mensajes del contrato vive en `crate::codec`.

/// Comando admitido por el hub para este tick.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Command {
    pub action_id: String,
    pub player_id: String,
    /// `None` cuando el mensaje llegó sin tipo.
    pub kind: Option<CommandKind>,
}

/// Tipos de comando del contrato. Por ahora el simulador solo aplica
/// `Drop`; el resto se rechaza con [`RejectReason::Unsupported`].
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum CommandKind {
    Drop { x: u32, y: u32 },
    PlaceInstallation,
    BuyUpgrade,
    SetDemoMode { enabled: bool },
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
    /// Sin tipo o con identificadores que no son UUID.
    InvalidCommand,
    /// Tipo de comando que el simulador todavía no implementa.
    Unsupported,
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
