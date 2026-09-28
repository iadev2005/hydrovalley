//! El valle y su paso de simulación (escritor único, Capa 2).

use std::collections::{BTreeMap, HashSet};

use uuid::Uuid;

use super::command::{
    Command, CommandKind, CommandOutcome, CommandResult, RejectReason, StepOutcome,
};
use super::player::{AppliedAction, Player};
use super::rules::Rules;
use super::tile::Tile;

/// Cada cuántos ticks se toma una instantánea (Capa 5).
pub const SNAPSHOT_INTERVAL_TICKS: u64 = 50;

/// `true` si tras calcular `tick` toca enviar una instantánea.
pub fn is_snapshot_tick(tick: u64) -> bool {
    tick != 0 && tick.is_multiple_of(SNAPSHOT_INTERVAL_TICKS)
}

/// Todo el estado del mundo en campos planos: lo que viaja en la
/// instantánea.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct WorldParts {
    pub tick: u64,
    pub epoch: u32,
    pub catalog_version: String,
    pub rng_state: Vec<u8>,
    pub demo_mode: bool,
    pub humidity_contributed: u64,
    pub width: u32,
    pub height: u32,
    /// Por filas: índice = y * width + x.
    pub tiles: Vec<Tile>,
    /// Ordenados por `player_id`.
    pub players: Vec<Player>,
}

/// La instantánea no encaja con las reglas vigentes.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum RestoreError {
    Dimensions {
        expected: (u32, u32),
        found: (u32, u32),
    },
    TileCount {
        expected: usize,
        found: usize,
    },
    DuplicatePlayer(String),
}

impl std::fmt::Display for RestoreError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Dimensions { expected, found } => write!(
                f,
                "el valle mide {}x{} y la instantánea {}x{}",
                expected.0, expected.1, found.0, found.1
            ),
            Self::TileCount { expected, found } => {
                write!(f, "se esperaban {expected} casillas y hay {found}")
            }
            Self::DuplicatePlayer(id) => write!(f, "jugador repetido: {id}"),
        }
    }
}

impl std::error::Error for RestoreError {}

#[derive(Debug, Clone)]
struct PlayerSlot {
    record: Player,
    /// Índice de `record.recent_actions` para buscar duplicados.
    recent_ids: HashSet<String>,
}

/// Estado autoritativo del valle.
#[derive(Debug, Clone)]
pub struct World {
    rules: Rules,
    tick: u64,
    epoch: u32,
    rng_state: Vec<u8>,
    demo_mode: bool,
    humidity_contributed: u64,
    tiles: Vec<Tile>,
    players: BTreeMap<String, PlayerSlot>,
}

impl World {
    /// Valle vacío en el tick 0 (LoadSnapshot devolvió NOT_FOUND).
    pub fn new(rules: Rules) -> Self {
        let tile_count = tile_count(rules.width, rules.height);
        Self {
            rules,
            tick: 0,
            epoch: 0,
            // TODO(rng): generador con semilla cuyo estado viaja en la
            // instantánea (regla 6); llegará con los incendios.
            rng_state: Vec::new(),
            demo_mode: false,
            humidity_contributed: 0,
            tiles: vec![Tile::default(); tile_count],
            players: BTreeMap::new(),
        }
    }

    /// Reconstruye el mundo a partir de una instantánea.
    ///
    /// TODO(catalog): la versión del catálogo de la instantánea se conserva
    /// tal cual; cuando exista CatalogService habrá que decidir qué pasa si
    /// no coincide con la vigente.
    pub fn restore(mut rules: Rules, parts: WorldParts) -> Result<Self, RestoreError> {
        let expected = (rules.width, rules.height);
        let found = (parts.width, parts.height);
        if expected != found {
            return Err(RestoreError::Dimensions { expected, found });
        }
        let expected_tiles = tile_count(rules.width, rules.height);
        if parts.tiles.len() != expected_tiles {
            return Err(RestoreError::TileCount {
                expected: expected_tiles,
                found: parts.tiles.len(),
            });
        }
        let mut players = BTreeMap::new();
        for mut record in parts.players {
            record.recent_actions.sort_by_key(|applied| applied.tick);
            let recent_ids = record
                .recent_actions
                .iter()
                .map(|applied| applied.action_id.clone())
                .collect();
            let id = record.player_id.clone();
            if players
                .insert(id.clone(), PlayerSlot { record, recent_ids })
                .is_some()
            {
                return Err(RestoreError::DuplicatePlayer(id));
            }
        }
        rules.catalog_version = parts.catalog_version;
        Ok(Self {
            rules,
            tick: parts.tick,
            epoch: parts.epoch,
            rng_state: parts.rng_state,
            demo_mode: parts.demo_mode,
            humidity_contributed: parts.humidity_contributed,
            tiles: parts.tiles,
            players,
        })
    }

    /// Copia del estado completo, para la instantánea.
    pub fn parts(&self) -> WorldParts {
        WorldParts {
            tick: self.tick,
            epoch: self.epoch,
            catalog_version: self.rules.catalog_version.clone(),
            rng_state: self.rng_state.clone(),
            demo_mode: self.demo_mode,
            humidity_contributed: self.humidity_contributed,
            width: self.rules.width,
            height: self.rules.height,
            tiles: self.tiles.clone(),
            players: self.players().cloned().collect(),
        }
    }

    /// Aplica los comandos en orden y avanza el valle un tick.
    ///
    /// Los comandos se aplican con el número del tick nuevo: la instantánea
    /// de ese tick los incluye.
    pub fn step(&mut self, commands: &[Command]) -> StepOutcome {
        let tick = self.tick.saturating_add(1);
        self.forget_old_actions(tick);

        let results = commands
            .iter()
            .map(|command| CommandResult {
                action_id: command.action_id.clone(),
                player_id: command.player_id.clone(),
                outcome: self.apply(command, tick),
            })
            .collect();

        // TODO(simulation): crecimiento, recarga del depósito, aspersores,
        // evaporación, marchitamiento e incendios (D-01).
        self.tick = tick;
        StepOutcome { tick, results }
    }

    fn apply(&mut self, command: &Command, tick: u64) -> CommandOutcome {
        if !is_uuid(&command.action_id) || !is_uuid(&command.player_id) {
            return CommandOutcome::Rejected(RejectReason::InvalidCommand);
        }
        let slot = self.player_slot(&command.player_id);
        if slot.recent_ids.contains(&command.action_id) {
            return CommandOutcome::Duplicate;
        }
        let outcome = match &command.kind {
            None => CommandOutcome::Rejected(RejectReason::InvalidCommand),
            Some(CommandKind::Drop { x, y }) => self.drop(&command.player_id, *x, *y),
            // TODO(commands): instalaciones, mejoras, modo demo y reinicio
            // del valle (RF-04, RF-07, RF-31).
            Some(
                CommandKind::PlaceInstallation
                | CommandKind::BuyUpgrade
                | CommandKind::SetDemoMode { .. }
                | CommandKind::ResetValley,
            ) => CommandOutcome::Rejected(RejectReason::Unsupported),
        };
        if outcome == CommandOutcome::Applied {
            let slot = self.player_slot(&command.player_id);
            slot.record.last_action_tick = tick;
            slot.record.recent_actions.push(AppliedAction {
                action_id: command.action_id.clone(),
                tick,
            });
            slot.recent_ids.insert(command.action_id.clone());
        }
        outcome
    }

    /// Gota sobre (x, y): cobra el agua (salvo en modo demo) y humedece la
    /// casilla.
    ///
    /// TODO(simulation): gota más grande (3 x 3) y gotas que apagan fuegos.
    fn drop(&mut self, player_id: &str, x: u32, y: u32) -> CommandOutcome {
        let Some(index) = self.tile_index(x, y) else {
            return CommandOutcome::Rejected(RejectReason::OutOfBounds);
        };
        let (humidity, cost) = if self.demo_mode {
            (self.rules.demo_drop_humidity, 0)
        } else {
            (self.rules.drop_humidity, self.rules.drop_water_cost)
        };
        let player = &mut self.player_slot(player_id).record;
        if player.water < cost {
            return CommandOutcome::Rejected(RejectReason::InsufficientWater);
        }
        player.water -= cost;
        player.water_spent = player.water_spent.saturating_add(cost);

        let tile = &mut self.tiles[index];
        tile.humidity = tile.humidity.saturating_add(humidity);
        self.humidity_contributed = self.humidity_contributed.saturating_add(humidity);
        CommandOutcome::Applied
    }

    /// Un jugador entra en el mundo con su primer comando, con los valores
    /// iniciales del catálogo.
    fn player_slot(&mut self, player_id: &str) -> &mut PlayerSlot {
        let rules = &self.rules;
        self.players
            .entry(player_id.to_string())
            .or_insert_with(|| PlayerSlot {
                record: Player {
                    player_id: player_id.to_string(),
                    water: rules.initial_water,
                    tank_capacity: rules.initial_tank_capacity,
                    ..Player::default()
                },
                recent_ids: HashSet::new(),
            })
    }

    /// Olvida los `action_id` aplicados hace más de la ventana (Capa 3).
    fn forget_old_actions(&mut self, tick: u64) {
        let window = self.rules.recent_actions_window_ticks;
        for slot in self.players.values_mut() {
            let keep_from = slot
                .record
                .recent_actions
                .partition_point(|applied| tick.saturating_sub(applied.tick) > window);
            for applied in slot.record.recent_actions.drain(..keep_from) {
                slot.recent_ids.remove(&applied.action_id);
            }
        }
    }

    fn tile_index(&self, x: u32, y: u32) -> Option<usize> {
        (x < self.rules.width && y < self.rules.height)
            .then(|| y as usize * self.rules.width as usize + x as usize)
    }

    pub fn tick(&self) -> u64 {
        self.tick
    }

    pub fn epoch(&self) -> u32 {
        self.epoch
    }

    pub fn demo_mode(&self) -> bool {
        self.demo_mode
    }

    /// TODO(admin): hoy solo lo usan las pruebas; llegará con
    /// `SetDemoModeCommand` (RF-31).
    pub fn set_demo_mode(&mut self, enabled: bool) {
        self.demo_mode = enabled;
    }

    pub fn humidity_contributed(&self) -> u64 {
        self.humidity_contributed
    }

    pub fn width(&self) -> u32 {
        self.rules.width
    }

    pub fn height(&self) -> u32 {
        self.rules.height
    }

    pub fn catalog_version(&self) -> &str {
        &self.rules.catalog_version
    }

    /// Por filas: índice = y * width + x.
    pub fn tiles(&self) -> &[Tile] {
        &self.tiles
    }

    pub fn tile(&self, x: u32, y: u32) -> Option<&Tile> {
        self.tile_index(x, y).map(|index| &self.tiles[index])
    }

    pub fn player(&self, player_id: &str) -> Option<&Player> {
        self.players.get(player_id).map(|slot| &slot.record)
    }

    /// Jugadores ordenados por `player_id`.
    pub fn players(&self) -> impl Iterator<Item = &Player> {
        self.players.values().map(|slot| &slot.record)
    }
}

fn tile_count(width: u32, height: u32) -> usize {
    width as usize * height as usize
}

fn is_uuid(text: &str) -> bool {
    Uuid::try_parse(text).is_ok()
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::domain::{AppliedAction, CommandKind, RejectReason, TileStage};

    const ANA: &str = "6f1c1a52-2d0e-4f4e-9c1e-0c6c2d9b8a01";
    const BETO: &str = "9a0b8c7d-6e5f-4a3b-8c2d-1e0f9a8b7c02";

    fn action(n: u32) -> String {
        format!("00000000-0000-4000-8000-{n:012}")
    }

    fn drop_cmd(action_id: &str, player_id: &str, x: u32, y: u32) -> Command {
        Command {
            action_id: action_id.to_string(),
            player_id: player_id.to_string(),
            kind: Some(CommandKind::Drop { x, y }),
        }
    }

    fn world() -> World {
        World::new(Rules::initial())
    }

    fn outcomes(step: &StepOutcome) -> Vec<CommandOutcome> {
        step.results.iter().map(|r| r.outcome).collect()
    }

    #[test]
    fn new_world_is_an_empty_32x32_valley_at_tick_zero() {
        let w = world();
        assert_eq!(w.tick(), 0);
        assert_eq!((w.width(), w.height()), (32, 32));
        assert_eq!(w.tiles().len(), 1024);
        assert!(w.tiles().iter().all(|t| *t == Tile::default()));
        assert_eq!(w.tile(0, 0).unwrap().stage, TileStage::DrySoil);
        assert_eq!(w.players().count(), 0);
        assert_eq!(w.humidity_contributed(), 0);
        assert!(!w.demo_mode());
    }

    #[test]
    fn tile_lookup_is_row_major_and_bounded() {
        let mut w = world();
        w.step(&[drop_cmd(&action(1), ANA, 3, 2)]);
        assert_eq!(w.tiles()[2 * 32 + 3].humidity, 25);
        assert_eq!(w.tile(3, 2).unwrap().humidity, 25);
        assert!(w.tile(32, 0).is_none());
        assert!(w.tile(0, 32).is_none());
    }

    #[test]
    fn every_step_advances_the_tick_by_one_even_without_commands() {
        let mut w = world();
        assert_eq!(w.step(&[]).tick, 1);
        assert_eq!(w.step(&[]).tick, 2);
        assert_eq!(w.tick(), 2);
    }

    #[test]
    fn drop_costs_one_litre_and_adds_25_humidity() {
        let mut w = world();
        let out = w.step(&[drop_cmd(&action(1), ANA, 14, 9)]);

        assert_eq!(out.tick, 1);
        assert_eq!(
            out.results,
            vec![CommandResult {
                action_id: action(1),
                player_id: ANA.to_string(),
                outcome: CommandOutcome::Applied,
            }]
        );
        let ana = w.player(ANA).unwrap();
        assert_eq!(ana.water, 19);
        assert_eq!(ana.tank_capacity, 20);
        assert_eq!(ana.water_spent, 1);
        assert_eq!(ana.last_action_tick, 1);
        assert_eq!(
            ana.recent_actions,
            vec![AppliedAction {
                action_id: action(1),
                tick: 1
            }]
        );
        assert_eq!(w.tile(14, 9).unwrap().humidity, 25);
        assert_eq!(w.humidity_contributed(), 25);
    }

    #[test]
    fn demo_drop_is_free_and_adds_one_humidity() {
        let mut w = world();
        w.set_demo_mode(true);
        let out = w.step(&[drop_cmd(&action(1), ANA, 0, 0)]);

        assert_eq!(outcomes(&out), vec![CommandOutcome::Applied]);
        let ana = w.player(ANA).unwrap();
        assert_eq!(ana.water, 20);
        assert_eq!(ana.water_spent, 0);
        assert_eq!(w.tile(0, 0).unwrap().humidity, 1);
        assert_eq!(w.humidity_contributed(), 1);
    }

    #[test]
    fn drop_out_of_bounds_is_rejected_without_charging() {
        let mut w = world();
        let out = w.step(&[
            drop_cmd(&action(1), ANA, 32, 0),
            drop_cmd(&action(2), ANA, 0, 32),
        ]);

        assert_eq!(
            outcomes(&out),
            vec![
                CommandOutcome::Rejected(RejectReason::OutOfBounds),
                CommandOutcome::Rejected(RejectReason::OutOfBounds),
            ]
        );
        let ana = w
            .player(ANA)
            .expect("the first command brings the player in");
        assert_eq!(ana.water, 20);
        assert!(ana.recent_actions.is_empty());
        assert_eq!(w.humidity_contributed(), 0);
    }

    #[test]
    fn drop_without_water_is_rejected_and_demo_mode_ignores_the_tank() {
        let mut w = world();
        let drops: Vec<Command> = (0..21).map(|n| drop_cmd(&action(n), ANA, 1, 1)).collect();
        let out = w.step(&drops);

        assert!(
            out.results[..20]
                .iter()
                .all(|r| r.outcome == CommandOutcome::Applied)
        );
        assert_eq!(
            out.results[20].outcome,
            CommandOutcome::Rejected(RejectReason::InsufficientWater)
        );
        assert_eq!(w.player(ANA).unwrap().water, 0);
        assert_eq!(w.tile(1, 1).unwrap().humidity, 20 * 25);

        w.set_demo_mode(true);
        let out = w.step(&[drop_cmd(&action(100), ANA, 1, 1)]);
        assert_eq!(outcomes(&out), vec![CommandOutcome::Applied]);
        assert_eq!(w.player(ANA).unwrap().water, 0);
    }

    #[test]
    fn results_follow_request_order_across_players() {
        let mut w = world();
        let out = w.step(&[
            drop_cmd(&action(1), BETO, 2, 2),
            drop_cmd(&action(2), ANA, 40, 2),
            drop_cmd(&action(3), ANA, 2, 2),
        ]);
        let ids: Vec<(&str, &str)> = out
            .results
            .iter()
            .map(|r| (r.action_id.as_str(), r.player_id.as_str()))
            .collect();
        assert_eq!(
            ids,
            vec![
                (action(1).as_str(), BETO),
                (action(2).as_str(), ANA),
                (action(3).as_str(), ANA)
            ]
        );
        assert_eq!(w.tile(2, 2).unwrap().humidity, 50);
    }

    #[test]
    fn repeated_action_id_in_the_same_tick_is_a_duplicate() {
        let mut w = world();
        let out = w.step(&[
            drop_cmd(&action(1), ANA, 5, 5),
            drop_cmd(&action(1), ANA, 5, 5),
        ]);
        assert_eq!(
            outcomes(&out),
            vec![CommandOutcome::Applied, CommandOutcome::Duplicate]
        );
        assert_eq!(w.tile(5, 5).unwrap().humidity, 25);
        assert_eq!(w.player(ANA).unwrap().water, 19);
    }

    #[test]
    fn applied_action_is_remembered_for_600_ticks() {
        let mut w = world();
        w.step(&[drop_cmd(&action(1), ANA, 5, 5)]); // tick 1
        for _ in 2..601 {
            w.step(&[]);
        }
        assert_eq!(w.tick(), 600);

        // Tick 601: 600 ticks después, todavía dentro de la ventana.
        let out = w.step(&[drop_cmd(&action(1), ANA, 5, 5)]);
        assert_eq!(outcomes(&out), vec![CommandOutcome::Duplicate]);

        // Tick 602: fuera de la ventana; se aplica como nuevo.
        let out = w.step(&[drop_cmd(&action(1), ANA, 5, 5)]);
        assert_eq!(outcomes(&out), vec![CommandOutcome::Applied]);
        assert_eq!(w.tile(5, 5).unwrap().humidity, 50);
    }

    #[test]
    fn old_actions_are_pruned_from_the_window() {
        let mut w = world();
        w.step(&[drop_cmd(&action(1), ANA, 5, 5)]); // tick 1
        for _ in 2..=601 {
            w.step(&[]);
        }
        w.step(&[drop_cmd(&action(2), ANA, 5, 5)]); // tick 602
        let recent = &w.player(ANA).unwrap().recent_actions;
        assert_eq!(
            recent,
            &vec![AppliedAction {
                action_id: action(2),
                tick: 602
            }]
        );
    }

    #[test]
    fn duplicates_are_tracked_per_player() {
        let mut w = world();
        let out = w.step(&[
            drop_cmd(&action(1), ANA, 5, 5),
            drop_cmd(&action(1), BETO, 5, 5),
        ]);
        assert_eq!(
            outcomes(&out),
            vec![CommandOutcome::Applied, CommandOutcome::Applied]
        );
    }

    #[test]
    fn rejected_actions_are_not_remembered() {
        let mut w = world();
        w.step(&[drop_cmd(&action(1), ANA, 99, 5)]);
        let out = w.step(&[drop_cmd(&action(1), ANA, 5, 5)]);
        assert_eq!(outcomes(&out), vec![CommandOutcome::Applied]);
    }

    #[test]
    fn malformed_commands_are_invalid_and_do_not_create_players() {
        let mut w = world();
        let out = w.step(&[
            drop_cmd("not-a-uuid", ANA, 1, 1),
            drop_cmd(&action(1), "not-a-uuid", 1, 1),
            Command {
                action_id: action(2),
                player_id: ANA.to_string(),
                kind: None,
            },
        ]);
        assert_eq!(
            outcomes(&out),
            vec![CommandOutcome::Rejected(RejectReason::InvalidCommand); 3]
        );
        assert!(w.player("not-a-uuid").is_none());
        assert!(
            w.player(ANA).is_some(),
            "a well-formed command without kind still brings the player in"
        );
        assert_eq!(w.humidity_contributed(), 0);
    }

    #[test]
    fn other_command_kinds_are_rejected_as_unsupported_for_now() {
        let mut w = world();
        let kinds = [
            CommandKind::PlaceInstallation,
            CommandKind::BuyUpgrade,
            CommandKind::SetDemoMode { enabled: true },
            CommandKind::ResetValley,
        ];
        let commands: Vec<Command> = kinds
            .into_iter()
            .enumerate()
            .map(|(n, kind)| Command {
                action_id: action(n as u32),
                player_id: ANA.to_string(),
                kind: Some(kind),
            })
            .collect();
        let out = w.step(&commands);
        assert_eq!(
            outcomes(&out),
            vec![CommandOutcome::Rejected(RejectReason::Unsupported); 4]
        );
        assert!(!w.demo_mode());
        assert_eq!(w.player(ANA).unwrap().water, 20);
    }

    #[test]
    fn snapshot_ticks_are_the_multiples_of_50() {
        assert!(!is_snapshot_tick(0));
        assert!(!is_snapshot_tick(1));
        assert!(!is_snapshot_tick(49));
        assert!(is_snapshot_tick(50));
        assert!(!is_snapshot_tick(51));
        assert!(is_snapshot_tick(100));
    }

    #[test]
    fn restore_round_trips_the_whole_world() {
        let mut w = world();
        w.set_demo_mode(true);
        w.step(&[
            drop_cmd(&action(1), ANA, 3, 4),
            drop_cmd(&action(2), BETO, 31, 31),
        ]);
        let parts = w.parts();
        assert_eq!(parts.tick, 1);
        assert_eq!(parts.players.len(), 2);
        assert!(parts.players[0].player_id < parts.players[1].player_id);

        let restored = World::restore(Rules::initial(), parts.clone()).unwrap();
        assert_eq!(restored.parts(), parts);
    }

    #[test]
    fn restored_world_continues_the_tick_and_keeps_the_duplicate_window() {
        let mut w = world();
        w.step(&[drop_cmd(&action(1), ANA, 3, 4)]);
        let mut restored = World::restore(Rules::initial(), w.parts()).unwrap();

        let out = restored.step(&[drop_cmd(&action(1), ANA, 3, 4)]);
        assert_eq!(out.tick, 2);
        assert_eq!(outcomes(&out), vec![CommandOutcome::Duplicate]);
        assert_eq!(restored.tile(3, 4).unwrap().humidity, 25);
    }

    #[test]
    fn restore_rejects_a_valley_of_another_size() {
        let mut parts = world().parts();
        parts.width = 16;
        assert_eq!(
            World::restore(Rules::initial(), parts).unwrap_err(),
            RestoreError::Dimensions {
                expected: (32, 32),
                found: (16, 32)
            }
        );

        let mut parts = world().parts();
        parts.tiles.pop();
        assert_eq!(
            World::restore(Rules::initial(), parts).unwrap_err(),
            RestoreError::TileCount {
                expected: 1024,
                found: 1023
            }
        );
    }
}
