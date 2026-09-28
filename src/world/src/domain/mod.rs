//! Núcleo del simulador: reglas puras, sin gRPC ni red.

pub mod command;
pub mod player;
pub mod rules;
pub mod tile;
pub mod world;

pub use command::{Command, CommandKind, CommandOutcome, CommandResult, RejectReason, StepOutcome};
pub use player::{AppliedAction, Player};
pub use rules::Rules;
pub use tile::{Fire, Installation, InstallationKind, Tile, TileStage, UpgradeKind};
pub use world::{RestoreError, SNAPSHOT_INTERVAL_TICKS, World, WorldParts, is_snapshot_tick};
