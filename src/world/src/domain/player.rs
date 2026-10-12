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

impl Player {
    /// Instalaciones de `kind` que el jugador ha comprado (fija el precio de
    /// la siguiente).
    pub fn purchases_of(&self, kind: InstallationKind) -> u32 {
        self.purchases
            .iter()
            .find(|(k, _)| *k == kind)
            .map_or(0, |(_, count)| *count)
    }

    pub(super) fn record_purchase(&mut self, kind: InstallationKind) {
        match self.purchases.iter_mut().find(|(k, _)| *k == kind) {
            Some((_, count)) => *count = count.saturating_add(1),
            None => self.purchases.push((kind, 1)),
        }
    }

    pub(super) fn spend_leaves(&mut self, amount: u64) {
        self.leaves = self.leaves.saturating_sub(amount);
        self.leaves_spent = self.leaves_spent.saturating_add(amount);
    }

    pub fn has_upgrade(&self, kind: UpgradeKind) -> bool {
        self.upgrades.contains(&kind)
    }

    /// Jugador recién llegado (o reiniciado): depósito lleno y todo a cero.
    /// El depósito inicial cuenta como recargado, para que se cumpla
    /// RF-27: `water = water_refilled - water_spent`.
    pub fn starting(player_id: &str, tank_capacity: u64) -> Self {
        Self {
            player_id: player_id.to_string(),
            water: tank_capacity,
            tank_capacity,
            water_refilled: tank_capacity,
            ..Self::default()
        }
    }

    /// Conserva de `previous` lo que no es estado de juego: el momento de su
    /// última acción y la ventana de `action_id` aplicados.
    pub fn keeping_actions_of(mut self, previous: Self) -> Self {
        self.last_action_tick = previous.last_action_tick;
        self.recent_actions = previous.recent_actions;
        self
    }
}
