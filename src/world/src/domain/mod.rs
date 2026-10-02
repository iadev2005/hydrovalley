//! Núcleo del simulador: reglas puras, sin gRPC ni red.

pub mod catalog;
pub mod command;
pub mod fire;
pub mod growth;
pub mod limits;
pub mod player;
pub mod ranking;
pub mod rng;
pub mod tile;
pub mod world;

pub use catalog::Catalog;
pub use command::{Command, CommandKind, CommandOutcome, CommandResult, RejectReason, StepOutcome};
pub use player::{AppliedAction, Player};
pub use ranking::{MAX_RANKING_ENTRIES, RankedPlayer, Score, rank_players};
pub use tile::{Fire, Installation, InstallationKind, Tile, TileStage, UpgradeKind};
pub use world::{
    MAX_PLAYERS, RestoreError, SNAPSHOT_INTERVAL_TICKS, World, WorldParts, is_snapshot_tick,
};
