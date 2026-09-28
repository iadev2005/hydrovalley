//! Jugadores presentes en el mundo.

use super::tile::{InstallationKind, UpgradeKind};

/// `action_id` aplicado y el tick en que se aplicó (Capa 3).
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AppliedAction {
    pub action_id: String,
    pub tick: u64,
}

/// Todo lo que el simulador guarda de cada jugador (`PlayerRecord`).
#[derive(Debug, Clone, PartialEq, Eq, Default)]
pub struct Player {
    pub player_id: String,
    pub water: u64,
    pub tank_capacity: u64,
    pub leaves: u64,
    pub water_refilled: u64,
    pub water_spent: u64,
    pub leaves_earned: u64,
    pub leaves_spent: u64,
    pub water_reduced: u64,
    pub water_reused: u64,
    pub water_recycled: u64,
    pub purchases: Vec<(InstallationKind, u32)>,
    pub upgrades: Vec<UpgradeKind>,
    pub last_action_tick: u64,
    /// Aplicados dentro de la ventana, en orden de tick.
    pub recent_actions: Vec<AppliedAction>,
}
