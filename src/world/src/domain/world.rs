//! El valle y su paso de simulación (escritor único, Capa 2).

use std::collections::{BTreeMap, HashSet};

use uuid::Uuid;

use super::catalog::Catalog;
use super::command::{
    Command, CommandKind, CommandOutcome, CommandResult, RejectReason, StepOutcome,
};
use super::growth;
use super::limits::{RECENT_ACTIONS_WINDOW_TICKS, TICKS_PER_SECOND, VALLEY_HEIGHT, VALLEY_WIDTH};
use super::player::{AppliedAction, Player};
use super::tile::Tile;

/// Cada cuántos ticks se toma una instantánea (Capa 5).
pub const SNAPSHOT_INTERVAL_TICKS: u64 = 50;

/// Jugadores que puede tener el mundo como mucho. Acota la memoria del
/// simulador y el tamaño de la instantánea y del `WorldState` que se envía
/// en cada tick; con un valle de 32 × 32 casillas, 10.000 jugadores ya es
/// mucho más de lo que el juego puede aprovechar. Un jugador nuevo que
/// llegue con el mundo lleno recibe `RejectReason::WorldFull`.
pub const MAX_PLAYERS: usize = 10_000;

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
    catalog: Catalog,
    tick: u64,
    epoch: u32,
    rng_state: Vec<u8>,
    demo_mode: bool,
    humidity_contributed: u64,
    tiles: Vec<Tile>,
    players: BTreeMap<String, PlayerSlot>,
    /// Catálogo ofrecido a los reinicios del tick en curso; solo existe
    /// durante `step_with_reset_catalog`. No viaja en la instantánea.
    reset_catalog: Option<Catalog>,
}

impl World {
    /// Valle vacío en el tick 0 (LoadSnapshot devolvió NOT_FOUND).
    pub fn new(catalog: Catalog) -> Self {
        let tile_count = tile_count(VALLEY_WIDTH, VALLEY_HEIGHT);
        Self {
            catalog,
            tick: 0,
            epoch: 0,
            // TODO(rng): generador con semilla cuyo estado viaja en la
            // instantánea (regla 6); llegará con los incendios.
            rng_state: Vec::new(),
            demo_mode: false,
            humidity_contributed: 0,
            tiles: vec![Tile::default(); tile_count],
            players: BTreeMap::new(),
            reset_catalog: None,
        }
    }

    /// Reconstruye el mundo a partir de una instantánea.
    ///
    /// El mundo restaurado aplica el catálogo vigente (`catalog`), no el que
    /// estaba en vigor al tomar la instantánea: así un cambio de cifras
    /// surte efecto al reiniciar. `parts.catalog_version` solo sirve para
    /// avisar de que cambió; la siguiente instantánea lleva la versión
    /// vigente.
    pub fn restore(catalog: Catalog, parts: WorldParts) -> Result<Self, RestoreError> {
        let expected = (VALLEY_WIDTH, VALLEY_HEIGHT);
        let found = (parts.width, parts.height);
        if expected != found {
            return Err(RestoreError::Dimensions { expected, found });
        }
        let expected_tiles = tile_count(VALLEY_WIDTH, VALLEY_HEIGHT);
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
        Ok(Self {
            catalog,
            tick: parts.tick,
            epoch: parts.epoch,
            rng_state: parts.rng_state,
            demo_mode: parts.demo_mode,
            humidity_contributed: parts.humidity_contributed,
            tiles: parts.tiles,
            players,
            reset_catalog: None,
        })
    }

    /// Copia del estado completo, para la instantánea.
    pub fn parts(&self) -> WorldParts {
        WorldParts {
            tick: self.tick,
            epoch: self.epoch,
            catalog_version: self.catalog.version.clone(),
            rng_state: self.rng_state.clone(),
            demo_mode: self.demo_mode,
            humidity_contributed: self.humidity_contributed,
            width: VALLEY_WIDTH,
            height: VALLEY_HEIGHT,
            tiles: self.tiles.clone(),
            players: self.players().cloned().collect(),
        }
    }

    /// Aplica los comandos en orden y avanza el valle un tick.
    ///
    /// Los comandos se aplican con el número del tick nuevo `t`: la
    /// instantánea de ese tick los incluye. El orden dentro del tick es fijo:
    ///
    /// 1. Olvida los `action_id` fuera de la ventana de 600 ticks.
    /// 2. Comandos, en orden de admisión. El crecimiento se resuelve al
    ///    sumar la humedad de cada gota (así las hojas son de quien la
    ///    empujó al umbral) y, por tanto, antes de la evaporación del mismo
    ///    tick.
    /// 3. Recarga de los depósitos, si `t` es múltiplo del intervalo del
    ///    catálogo en ticks.
    /// 4. Evaporación, si `t` es múltiplo de [`TICKS_PER_SECOND`] (salvo en
    ///    modo demo).
    /// 5. Marchitamiento: un tick más de cuenta a cada planta seca (salvo en
    ///    modo demo).
    /// 6. El tick pasa a `t`.
    ///
    /// Todo es aritmética entera y no hay azar: mismo estado y mismos
    /// comandos dan el mismo valle.
    pub fn step(&mut self, commands: &[Command]) -> StepOutcome {
        self.step_with_reset_catalog(commands, None)
    }

    /// Como [`step`](Self::step), pero ofrece `catalog` a los reinicios del
    /// tick: el mundo lo adopta solo cuando un `ResetValley` se **aplica**
    /// (un reinicio duplicado o rechazado no cambia nada) y, si se aplica,
    /// el propio reinicio ya usa las cifras nuevas. Sin reinicio aplicado,
    /// el catálogo se descarta.
    pub fn step_with_reset_catalog(
        &mut self,
        commands: &[Command],
        catalog: Option<Catalog>,
    ) -> StepOutcome {
        self.reset_catalog = catalog;
        let outcome = self.advance(commands);
        self.reset_catalog = None;
        outcome
    }

    fn advance(&mut self, commands: &[Command]) -> StepOutcome {
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

        // TODO(simulation): aspersores, goteo, planta de tratamiento e
        // incendios (D-01); se colgarán de esta función.
        self.advance_valley(tick);
        self.tick = tick;
        StepOutcome { tick, results }
    }

    fn apply(&mut self, command: &Command, tick: u64) -> CommandOutcome {
        if !is_uuid(&command.action_id) || !is_uuid(&command.player_id) {
            return CommandOutcome::Rejected(RejectReason::InvalidCommand);
        }
        let already_applied = self
            .players
            .get(&command.player_id)
            .is_some_and(|slot| slot.recent_ids.contains(&command.action_id));
        if already_applied {
            return CommandOutcome::Duplicate;
        }
        let outcome = match &command.kind {
            None => CommandOutcome::Rejected(RejectReason::InvalidCommand),
            Some(CommandKind::Drop { x, y }) => self.drop(&command.player_id, *x, *y),
            Some(CommandKind::SetDemoMode { enabled }) => {
                self.demo_mode = *enabled;
                CommandOutcome::Applied
            }
            Some(CommandKind::ResetValley) => {
                self.reset_valley();
                CommandOutcome::Applied
            }
            // TODO(commands): instalaciones y mejoras (RF-04, RF-07).
            Some(CommandKind::PlaceInstallation | CommandKind::BuyUpgrade) => {
                CommandOutcome::Rejected(RejectReason::Unsupported)
            }
        };
        if outcome == CommandOutcome::Applied {
            // El comando aplicado ya dio de alta al jugador si era nuevo.
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

    /// Gota sobre (x, y): cobra el agua (salvo en modo demo), humedece la
    /// casilla y, si con ella llega al umbral, la hace crecer y paga las
    /// hojas a este jugador (el que empujó la casilla, en orden de admisión).
    ///
    /// TODO(simulation): gota más grande (3 x 3) y gotas que apagan fuegos.
    fn drop(&mut self, player_id: &str, x: u32, y: u32) -> CommandOutcome {
        let Some(index) = self.tile_index(x, y) else {
            return CommandOutcome::Rejected(RejectReason::OutOfBounds);
        };
        let (humidity, cost) = if self.demo_mode {
            (self.catalog.drop.demo_humidity, 0)
        } else {
            (self.catalog.drop.humidity, self.catalog.drop.water_cost)
        };
        let known = self.players.get(player_id).map(|slot| slot.record.water);
        if known.is_none() && self.players.len() >= MAX_PLAYERS {
            return CommandOutcome::Rejected(RejectReason::WorldFull);
        }
        if known.unwrap_or(self.catalog.tank.capacity) < cost {
            return CommandOutcome::Rejected(RejectReason::InsufficientWater);
        }
        // Solo aquí, con la gota ya aceptada, entra un jugador nuevo.
        let leaves = growth::add_humidity(&mut self.tiles[index], humidity, &self.catalog.growth);
        self.humidity_contributed = self.humidity_contributed.saturating_add(humidity);

        let player = &mut self.player_slot(player_id).record;
        player.water -= cost;
        player.water_spent = player.water_spent.saturating_add(cost);
        player.leaves = player.leaves.saturating_add(leaves);
        player.leaves_earned = player.leaves_earned.saturating_add(leaves);
        CommandOutcome::Applied
    }

    /// Recarga el depósito de cada jugador con `tank.refill_amount` cada
    /// `tank.refill_interval_seconds` (en ticks, sin decimales), sin pasar de
    /// la capacidad. Solo cuenta como recargado (RF-27) lo que cabe.
    fn refill_tanks(&mut self, tick: u64) {
        let interval =
            u64::from(self.catalog.tank.refill_interval_seconds).saturating_mul(TICKS_PER_SECOND);
        if !tick.is_multiple_of(interval) {
            return;
        }
        let amount = self.catalog.tank.refill_amount;
        for slot in self.players.values_mut() {
            let player = &mut slot.record;
            let added = amount.min(player.tank_capacity.saturating_sub(player.water));
            player.water = player.water.saturating_add(added);
            player.water_refilled = player.water_refilled.saturating_add(added);
        }
    }

    /// Lo que cambia solo con el paso del tiempo, después de los comandos.
    fn advance_valley(&mut self, tick: u64) {
        self.refill_tanks(tick);
        // El modo demo congela el valle: sin evaporación ni marchitamiento
        // (y, cuando lleguen, sin incendios).
        if self.demo_mode {
            return;
        }
        // La cifra del catálogo es por segundo: se aplica entera una vez por
        // segundo (cada TICKS_PER_SECOND ticks), nunca como fracción.
        if tick.is_multiple_of(TICKS_PER_SECOND) {
            let amount = self.catalog.evaporation.humidity_per_second;
            for tile in &mut self.tiles {
                growth::evaporate(tile, amount);
            }
        }
        for tile in &mut self.tiles {
            growth::wither(tile, &self.catalog.evaporation);
        }
    }

    /// Reinicio del valle (RF-31): el mundo vuelve a su estado inicial y los
    /// jugadores a los valores de arranque del catálogo, con los contadores
    /// de RF-27 y de las 3R a cero.
    ///
    /// No toca el tick, el modo demo, el generador de azar ni
    /// `humidity_contributed` (que solo crece), y conserva a los jugadores y
    /// su ventana de `action_id`. Incrementa la época.
    fn reset_valley(&mut self) {
        if let Some(catalog) = self.reset_catalog.take() {
            self.catalog = catalog;
        }
        self.tiles.fill(Tile::default());
        self.epoch = self.epoch.saturating_add(1);
        let tank = self.catalog.tank.capacity;
        for slot in self.players.values_mut() {
            slot.record = Player::starting(&slot.record.player_id, tank)
                .keeping_actions_of(std::mem::take(&mut slot.record));
        }
    }

    /// Devuelve el jugador y lo da de alta si no existía, con los valores
    /// iniciales del catálogo. Solo se llama al aplicar un comando: los
    /// rechazados, mal formados o no soportados no crean jugadores.
    fn player_slot(&mut self, player_id: &str) -> &mut PlayerSlot {
        let tank = self.catalog.tank.capacity;
        self.players
            .entry(player_id.to_string())
            .or_insert_with(|| PlayerSlot {
                record: Player::starting(player_id, tank),
                recent_ids: HashSet::new(),
            })
    }

    /// Olvida los `action_id` aplicados hace más de la ventana (Capa 3).
    fn forget_old_actions(&mut self, tick: u64) {
        let window = RECENT_ACTIONS_WINDOW_TICKS;
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
        (x < VALLEY_WIDTH && y < VALLEY_HEIGHT)
            .then(|| y as usize * VALLEY_WIDTH as usize + x as usize)
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

    /// Atajo de las pruebas que no quieren gastar un tick: en producción el
    /// modo demo solo cambia con `CommandKind::SetDemoMode`.
    #[cfg(test)]
    pub(crate) fn set_demo_mode(&mut self, enabled: bool) {
        self.demo_mode = enabled;
    }

    pub fn humidity_contributed(&self) -> u64 {
        self.humidity_contributed
    }

    pub fn width(&self) -> u32 {
        VALLEY_WIDTH
    }

    pub fn height(&self) -> u32 {
        VALLEY_HEIGHT
    }

    pub fn catalog_version(&self) -> &str {
        &self.catalog.version
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
        World::new(Catalog::d01())
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
        assert_eq!(ana.water_refilled, 20, "the full tank counts as refilled");
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
        assert!(
            w.player(ANA).is_none(),
            "a rejected command does not bring the player in"
        );
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
        assert_eq!(w.humidity_contributed(), 20 * 25);

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
        assert_eq!(w.humidity_contributed(), 50);
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
            w.player(ANA).is_none(),
            "a command without kind does not bring the player in"
        );
        assert_eq!(w.humidity_contributed(), 0);
    }

    #[test]
    fn installation_and_upgrade_commands_are_rejected_as_unsupported_for_now() {
        let mut w = world();
        let kinds = [CommandKind::PlaceInstallation, CommandKind::BuyUpgrade];
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
            vec![CommandOutcome::Rejected(RejectReason::Unsupported); 2]
        );
        assert!(w.player(ANA).is_none());
    }

    #[test]
    fn a_player_enters_the_world_with_the_first_applied_command() {
        let mut w = world();
        w.step(&[drop_cmd(&action(1), ANA, 40, 0)]);
        assert!(w.player(ANA).is_none());
        let out = w.step(&[drop_cmd(&action(2), ANA, 1, 1)]);
        assert_eq!(outcomes(&out), vec![CommandOutcome::Applied]);
        assert_eq!(w.player(ANA).unwrap().water, 19);
    }

    #[test]
    fn new_players_beyond_the_cap_are_rejected() {
        let mut w = world();
        let fill: Vec<Command> = (0..MAX_PLAYERS as u32)
            .map(|n| {
                drop_cmd(
                    &action(n),
                    &format!("00000000-0000-4000-9000-{n:012}"),
                    0,
                    0,
                )
            })
            .collect();
        let out = w.step(&fill);
        assert!(
            out.results
                .iter()
                .all(|r| r.outcome == CommandOutcome::Applied)
        );
        assert_eq!(w.players().count(), MAX_PLAYERS);

        let out = w.step(&[
            drop_cmd(&action(1), ANA, 1, 1),
            drop_cmd(&action(2), "00000000-0000-4000-9000-000000000000", 1, 1),
        ]);
        assert_eq!(
            outcomes(&out),
            vec![
                CommandOutcome::Rejected(RejectReason::WorldFull),
                CommandOutcome::Applied
            ]
        );
        assert!(w.player(ANA).is_none());
        assert_eq!(w.players().count(), MAX_PLAYERS);
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

        let restored = World::restore(Catalog::d01(), parts.clone()).unwrap();
        assert_eq!(restored.parts(), parts);
    }

    #[test]
    fn restored_world_continues_the_tick_and_keeps_the_duplicate_window() {
        let mut w = world();
        w.step(&[drop_cmd(&action(1), ANA, 3, 4)]);
        let mut restored = World::restore(Catalog::d01(), w.parts()).unwrap();

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
            World::restore(Catalog::d01(), parts).unwrap_err(),
            RestoreError::Dimensions {
                expected: (32, 32),
                found: (16, 32)
            }
        );

        let mut parts = world().parts();
        parts.tiles.pop();
        assert_eq!(
            World::restore(Catalog::d01(), parts).unwrap_err(),
            RestoreError::TileCount {
                expected: 1024,
                found: 1023
            }
        );
    }

    fn custom_catalog() -> Catalog {
        let mut catalog = Catalog::d01();
        catalog.version = "custom".to_string();
        catalog.drop.water_cost = 2;
        catalog.drop.humidity = 40;
        catalog.tank.capacity = 30;
        catalog
    }

    #[test]
    fn a_drop_uses_the_figures_of_the_catalog() {
        let mut w = World::new(custom_catalog());
        let out = w.step(&[drop_cmd(&action(1), ANA, 1, 1)]);

        assert_eq!(outcomes(&out), vec![CommandOutcome::Applied]);
        let player = w.players().next().unwrap();
        assert_eq!((player.water, player.tank_capacity), (28, 30));
        assert_eq!(player.water_spent, 2);
        assert_eq!(w.tile(1, 1).unwrap().humidity, 40);
        assert_eq!(w.humidity_contributed(), 40);
    }

    #[test]
    fn a_new_player_starts_with_the_full_tank_of_the_catalog() {
        let mut catalog = Catalog::d01();
        catalog.tank.capacity = 1;
        catalog.drop.water_cost = 2;
        let mut w = World::new(catalog);
        let out = w.step(&[drop_cmd(&action(1), ANA, 1, 1)]);

        assert_eq!(
            outcomes(&out),
            vec![CommandOutcome::Rejected(RejectReason::InsufficientWater)]
        );
        assert_eq!(w.players().count(), 0);
    }

    #[test]
    fn a_demo_drop_uses_the_demo_humidity_of_the_catalog() {
        let mut catalog = custom_catalog();
        catalog.drop.demo_humidity = 3;
        let mut w = World::new(catalog);
        w.set_demo_mode(true);
        w.step(&[drop_cmd(&action(1), ANA, 1, 1)]);

        assert_eq!(w.tile(1, 1).unwrap().humidity, 3);
        assert_eq!(w.humidity_contributed(), 3);
        assert_eq!(w.players().next().unwrap().water, 30);
    }

    #[test]
    fn the_world_reports_the_version_of_its_catalog() {
        let w = World::new(custom_catalog());
        assert_eq!(w.catalog_version(), "custom");
        assert_eq!(w.parts().catalog_version, "custom");
    }

    #[test]
    fn restore_applies_the_current_catalog_and_not_the_one_of_the_snapshot() {
        let old = World::new(Catalog::d01()).parts();
        assert_eq!(old.catalog_version, Catalog::D01_VERSION);

        let mut restored = World::restore(custom_catalog(), old).unwrap();
        assert_eq!(restored.catalog_version(), "custom");
        assert_eq!(restored.parts().catalog_version, "custom");
        restored.step(&[drop_cmd(&action(1), ANA, 0, 0)]);
        assert_eq!(restored.tile(0, 0).unwrap().humidity, 40);
    }

    // ---- Reglas por tick (T2) -------------------------------------------

    const ADMIN: &str = "aaaaaaaa-0000-4000-8000-000000000001";

    fn demo_cmd(action_id: &str, enabled: bool) -> Command {
        Command {
            action_id: action_id.to_string(),
            player_id: ADMIN.to_string(),
            kind: Some(CommandKind::SetDemoMode { enabled }),
        }
    }

    fn reset_cmd(action_id: &str) -> Command {
        Command {
            action_id: action_id.to_string(),
            player_id: ADMIN.to_string(),
            kind: Some(CommandKind::ResetValley),
        }
    }

    /// Avanza `count` ticks sin comandos.
    fn idle(w: &mut World, count: u64) {
        for _ in 0..count {
            w.step(&[]);
        }
    }

    /// Avanza hasta que el mundo quede en `tick`.
    fn advance_to(w: &mut World, tick: u64) {
        assert!(w.tick() <= tick);
        idle(w, tick - w.tick());
    }

    /// `n` gotas del jugador sobre (x, y), con action_id distintos a partir
    /// de `first`.
    fn drops(first: u32, n: u32, player: &str, x: u32, y: u32) -> Vec<Command> {
        (first..first + n)
            .map(|k| drop_cmd(&action(k), player, x, y))
            .collect()
    }

    /// RF-27: agua = recargada − gastada y hojas = ganadas − gastadas.
    fn assert_rf27(w: &World) {
        for p in w.players() {
            assert_eq!(
                p.water,
                p.water_refilled - p.water_spent,
                "water {}",
                p.player_id
            );
            assert_eq!(
                p.leaves,
                p.leaves_earned - p.leaves_spent,
                "leaves {}",
                p.player_id
            );
        }
    }

    #[test]
    fn the_tank_refills_one_litre_every_20_ticks() {
        let mut w = world();
        w.step(&drops(1, 5, ANA, 0, 0)); // tick 1: 15 L
        advance_to(&mut w, 19);
        assert_eq!(w.player(ANA).unwrap().water, 15);
        advance_to(&mut w, 20);
        assert_eq!(w.player(ANA).unwrap().water, 16);
        advance_to(&mut w, 39);
        assert_eq!(w.player(ANA).unwrap().water, 16);
        advance_to(&mut w, 40);
        assert_eq!(w.player(ANA).unwrap().water, 17);
        assert_rf27(&w);
    }

    #[test]
    fn a_full_tank_does_not_overflow_and_counts_nothing() {
        let mut w = world();
        w.step(&drops(1, 1, ANA, 0, 0));
        advance_to(&mut w, 20); // vuelve a 20 L
        advance_to(&mut w, 100);
        let ana = w.player(ANA).unwrap();
        assert_eq!((ana.water, ana.water_refilled), (20, 21));
        assert_rf27(&w);
    }

    #[test]
    fn the_refill_is_capped_at_the_capacity_and_counts_only_what_fits() {
        let mut catalog = Catalog::d01();
        catalog.tank.refill_amount = 5;
        let mut w = World::new(catalog);
        w.step(&drops(1, 3, ANA, 0, 0)); // 17 L
        advance_to(&mut w, 20);
        let ana = w.player(ANA).unwrap();
        assert_eq!((ana.water, ana.water_refilled), (20, 23));
        assert_rf27(&w);
    }

    #[test]
    fn every_player_refills_even_when_idle() {
        let mut w = world();
        w.step(&[
            drop_cmd(&action(1), ANA, 0, 0),
            drop_cmd(&action(2), BETO, 1, 1),
        ]);
        advance_to(&mut w, 20);
        assert_eq!(w.player(ANA).unwrap().water, 20);
        assert_eq!(w.player(BETO).unwrap().water, 20);
    }

    #[test]
    fn the_refill_interval_comes_from_the_catalog() {
        let mut catalog = Catalog::d01();
        catalog.tank.refill_interval_seconds = 1;
        let mut w = World::new(catalog);
        w.step(&drops(1, 4, ANA, 0, 0));
        advance_to(&mut w, 9);
        assert_eq!(w.player(ANA).unwrap().water, 16);
        advance_to(&mut w, 10);
        assert_eq!(w.player(ANA).unwrap().water, 17);
        advance_to(&mut w, 20);
        assert_eq!(w.player(ANA).unwrap().water, 18);
    }

    #[test]
    fn a_tile_loses_the_catalog_humidity_every_second() {
        let mut w = world();
        w.step(&drops(1, 2, ANA, 4, 4)); // 50
        advance_to(&mut w, 9);
        assert_eq!(w.tile(4, 4).unwrap().humidity, 50);
        advance_to(&mut w, 10);
        assert_eq!(w.tile(4, 4).unwrap().humidity, 48);
        advance_to(&mut w, 20);
        assert_eq!(w.tile(4, 4).unwrap().humidity, 46);
        assert_eq!(w.humidity_contributed(), 50, "evaporation never touches it");
    }

    #[test]
    fn evaporation_never_goes_below_zero_and_uses_the_catalog() {
        let mut catalog = Catalog::d01();
        catalog.evaporation.humidity_per_second = 30;
        let mut w = World::new(catalog);
        w.step(&drops(1, 2, ANA, 4, 4)); // 50
        advance_to(&mut w, 10);
        assert_eq!(w.tile(4, 4).unwrap().humidity, 20);
        advance_to(&mut w, 20);
        assert_eq!(w.tile(4, 4).unwrap().humidity, 0);
        advance_to(&mut w, 30);
        assert_eq!(w.tile(4, 4).unwrap().humidity, 0);
    }

    #[test]
    fn the_valley_evolves_even_if_nobody_plays() {
        let mut w = world();
        w.step(&drops(1, 1, ANA, 7, 7));
        advance_to(&mut w, 100); // RF-28: solo pasa el tiempo
        assert_eq!(w.tile(7, 7).unwrap().humidity, 25 - 2 * 10);
    }

    #[test]
    fn the_drop_that_reaches_the_threshold_grows_the_tile_and_pays_its_player() {
        let mut w = world();
        w.step(&drops(1, 3, ANA, 2, 2));
        assert_eq!(w.tile(2, 2).unwrap().stage, TileStage::DrySoil);
        assert_eq!(w.player(ANA).unwrap().leaves, 0);

        w.step(&drops(4, 1, ANA, 2, 2));
        let tile = w.tile(2, 2).unwrap();
        assert_eq!((tile.stage, tile.humidity), (TileStage::Grass, 0));
        let ana = w.player(ANA).unwrap();
        assert_eq!((ana.leaves, ana.leaves_earned, ana.leaves_spent), (1, 1, 0));
        assert_eq!(w.humidity_contributed(), 100, "growth does not touch it");
        assert_rf27(&w);
    }

    #[test]
    fn the_leaves_go_to_the_player_whose_drop_pushed_the_tile_over() {
        let mut w = world();
        // Mismo tick y orden de admisión: Ana pone 75, Beto la empuja al 100.
        let mut commands = drops(1, 3, ANA, 2, 2);
        commands.push(drop_cmd(&action(10), BETO, 2, 2));
        commands.push(drop_cmd(&action(11), ANA, 2, 2)); // ya es de otra etapa
        w.step(&commands);
        assert_eq!(w.player(BETO).unwrap().leaves, 1);
        assert_eq!(w.player(ANA).unwrap().leaves, 0);
        assert_eq!(w.tile(2, 2).unwrap().humidity, 25);
        assert_rf27(&w);
    }

    #[test]
    fn a_tile_climbs_through_all_the_stages_with_the_catalog_leaves() {
        let mut catalog = Catalog::d01();
        catalog.tank.capacity = 100;
        let mut w = World::new(catalog);
        let mut stages = Vec::new();
        for round in 0..4u32 {
            // Cuatro gotas por etapa, en ticks seguidos (antes de que
            // evaporar, que toca en el tick 10, reste nada).
            w.step(&drops(round * 10, 4, ANA, 3, 3));
            stages.push(w.tile(3, 3).unwrap().stage);
        }
        stages.insert(0, TileStage::DrySoil);
        assert_eq!(
            stages,
            vec![
                TileStage::DrySoil,
                TileStage::Grass,
                TileStage::Shrub,
                TileStage::Flowers,
                TileStage::Tree
            ]
        );
        // 1 + 2 + 4 + 8 hojas.
        assert_eq!(w.player(ANA).unwrap().leaves_earned, 15);
        assert_rf27(&w);
    }

    #[test]
    fn a_tree_keeps_its_humidity_at_the_catalog_cap() {
        let mut catalog = Catalog::d01();
        catalog.tank.capacity = 100;
        let mut w = World::new(catalog);
        w.step(&drops(1, 40, ANA, 0, 0)); // 1000 de humedad aportada
        let tile = w.tile(0, 0).unwrap();
        assert_eq!(tile.stage, TileStage::Tree);
        assert!(tile.humidity <= 100);
        assert_eq!(w.humidity_contributed(), 1000);
    }

    #[test]
    fn growth_is_resolved_before_evaporation_inside_the_tick() {
        let mut w = world();
        advance_to(&mut w, 9);
        // Tick 10 (toca evaporar): cuatro gotas suman 100 y la casilla crece
        // antes de que la evaporación le quite nada.
        w.step(&drops(1, 4, ANA, 5, 5));
        assert_eq!(w.tick(), 10);
        let tile = w.tile(5, 5).unwrap();
        assert_eq!((tile.stage, tile.humidity), (TileStage::Grass, 0));
        assert_eq!(w.player(ANA).unwrap().leaves, 1);
    }

    #[test]
    fn a_plant_without_humidity_withers_after_the_catalog_ticks() {
        let mut w = world();
        w.step(&drops(1, 4, ANA, 6, 6)); // tick 1: pasto con humedad 0
        assert_eq!(w.tile(6, 6).unwrap().stage, TileStage::Grass);
        advance_to(&mut w, 99);
        let tile = w.tile(6, 6).unwrap();
        assert_eq!((tile.stage, tile.dry_ticks), (TileStage::Grass, 99));
        advance_to(&mut w, 100); // 100 ticks seguidos con humedad 0
        let tile = w.tile(6, 6).unwrap();
        assert_eq!((tile.stage, tile.dry_ticks), (TileStage::DrySoil, 0));
    }

    #[test]
    fn a_drop_resets_the_dry_counter_of_a_plant() {
        let mut w = world();
        w.step(&drops(1, 4, ANA, 6, 6));
        advance_to(&mut w, 50);
        assert_eq!(w.tile(6, 6).unwrap().dry_ticks, 50);
        w.step(&drops(10, 1, ANA, 6, 6));
        assert_eq!(w.tile(6, 6).unwrap().dry_ticks, 0);
    }

    #[test]
    fn demo_mode_has_no_evaporation_and_no_wither() {
        let mut w = world();
        w.step(&drops(1, 4, ANA, 6, 6)); // pasto con humedad 0
        w.step(&drops(10, 1, ANA, 8, 8)); // 25
        w.set_demo_mode(true);
        let frozen_at = w.tile(6, 6).unwrap().dry_ticks;
        advance_to(&mut w, 300);
        assert_eq!(w.tile(8, 8).unwrap().humidity, 25);
        let tile = w.tile(6, 6).unwrap();
        assert_eq!((tile.stage, tile.dry_ticks), (TileStage::Grass, frozen_at));
    }

    #[test]
    fn a_burning_tile_is_frozen_by_the_tick_rules() {
        let mut w = world();
        let mut parts = w.parts();
        parts.tiles[0] = Tile {
            stage: TileStage::Grass,
            humidity: 10,
            dry_ticks: 0,
            fire: Some(crate::domain::Fire {
                drops_received: 0,
                burning_ticks: 0,
            }),
            installation: None,
        };
        parts.tiles[1] = Tile {
            stage: TileStage::Grass,
            humidity: 0,
            dry_ticks: 90,
            fire: parts.tiles[0].fire,
            installation: None,
        };
        w = World::restore(Catalog::d01(), parts).unwrap();
        advance_to(&mut w, 200);
        let burning = w.tile(0, 0).unwrap();
        assert_eq!((burning.humidity, burning.stage), (10, TileStage::Grass));
        let frozen = w.tile(1, 0).unwrap();
        assert_eq!((frozen.dry_ticks, frozen.stage), (90, TileStage::Grass));
    }

    // ---- Comandos de administración (RF-31) -----------------------------

    #[test]
    fn set_demo_mode_is_applied_and_switches_the_drop_rules() {
        let mut w = world();
        let out = w.step(&[demo_cmd(&action(1), true)]);
        assert_eq!(outcomes(&out), vec![CommandOutcome::Applied]);
        assert!(w.demo_mode());

        w.step(&drops(10, 1, ANA, 1, 1));
        assert_eq!(w.tile(1, 1).unwrap().humidity, 1);
        assert_eq!(w.player(ANA).unwrap().water, 20, "the demo drop is free");

        let out = w.step(&[demo_cmd(&action(2), false)]);
        assert_eq!(outcomes(&out), vec![CommandOutcome::Applied]);
        assert!(!w.demo_mode());
        w.step(&drops(20, 1, ANA, 1, 1));
        assert_eq!(w.player(ANA).unwrap().water, 19);
    }

    #[test]
    fn set_demo_mode_takes_effect_in_the_same_tick_for_the_commands_after_it() {
        let mut w = world();
        let out = w.step(&[
            drop_cmd(&action(1), ANA, 0, 0),
            demo_cmd(&action(2), true),
            drop_cmd(&action(3), ANA, 0, 0),
        ]);
        assert_eq!(outcomes(&out), vec![CommandOutcome::Applied; 3]);
        assert_eq!(w.tile(0, 0).unwrap().humidity, 26);
        assert_eq!(w.humidity_contributed(), 26);
    }

    #[test]
    fn a_repeated_admin_command_is_a_duplicate() {
        let mut w = world();
        w.step(&[demo_cmd(&action(1), true)]);
        let out = w.step(&[demo_cmd(&action(1), true)]);
        assert_eq!(outcomes(&out), vec![CommandOutcome::Duplicate]);
    }

    #[test]
    fn in_demo_mode_the_humidity_contributed_equals_the_accepted_drops() {
        // CP-01 / CP-11 / RF-30 sobre el dominio: varios jugadores, varios
        // ticks, muchas gotas sobre la misma casilla (que además crece).
        let mut w = world();
        w.step(&[demo_cmd(&action(1), true)]);
        let players = [ANA, BETO, ADMIN];
        let mut accepted = 0u64;
        let mut next = 100;
        for tick in 0..30u32 {
            let batch: Vec<Command> = (0..7)
                .map(|k| {
                    next += 1;
                    let player = players[((tick + k) % 3) as usize];
                    drop_cmd(&action(next), player, tick % 2, 0)
                })
                .collect();
            let out = w.step(&batch);
            accepted += out
                .results
                .iter()
                .filter(|r| r.outcome == CommandOutcome::Applied)
                .count() as u64;
        }
        assert_eq!(accepted, 210);
        assert_eq!(w.humidity_contributed(), accepted);
        for p in w.players() {
            assert_eq!(p.water, 20, "the tank does not change in demo mode");
            assert_eq!(p.water_spent, 0);
        }
        assert!(
            w.tiles().iter().any(|t| t.stage != TileStage::DrySoil),
            "the tiles grew while the total stayed exact"
        );
        assert_rf27(&w);
    }

    #[test]
    fn reset_valley_restores_the_initial_state_and_keeps_the_clock() {
        let mut w = world();
        w.step(&[demo_cmd(&action(1), false)]);
        w.step(&drops(10, 6, ANA, 2, 2));
        w.step(&drops(20, 4, BETO, 9, 9)); // Beto hace crecer una casilla
        advance_to(&mut w, 57);
        assert!(w.tiles().iter().any(|t| *t != Tile::default()));
        let contributed = w.humidity_contributed();

        let out = w.step(&[reset_cmd(&action(99))]);
        assert_eq!(out.tick, 58);
        assert_eq!(outcomes(&out), vec![CommandOutcome::Applied]);
        assert_eq!(w.tick(), 58, "the tick never goes back");
        assert_eq!(w.epoch(), 1);
        assert!(w.tiles().iter().all(|t| *t == Tile::default()));
        assert_eq!(w.humidity_contributed(), contributed);
        assert!(!w.demo_mode());
        idle(&mut w, 1);
        assert_eq!(w.tick(), 59);
    }

    #[test]
    fn reset_valley_resets_the_players_counters_but_keeps_the_players() {
        let mut w = world();
        w.step(&drops(10, 6, ANA, 2, 2));
        w.step(&drops(20, 4, BETO, 9, 9));
        assert_eq!(w.player(BETO).unwrap().leaves, 1);

        w.step(&[reset_cmd(&action(99))]);
        assert_eq!(w.players().count(), 3, "Ana, Beto and the admin remain");
        for p in [ANA, BETO] {
            let p = w.player(p).unwrap();
            assert_eq!((p.water, p.tank_capacity), (20, 20));
            assert_eq!((p.leaves, p.leaves_earned, p.leaves_spent), (0, 0, 0));
            assert_eq!((p.water_refilled, p.water_spent), (20, 0));
            assert_eq!(
                (p.water_reduced, p.water_reused, p.water_recycled),
                (0, 0, 0)
            );
            assert!(p.purchases.is_empty() && p.upgrades.is_empty());
        }
        assert_rf27(&w);
    }

    #[test]
    fn reset_valley_keeps_the_idempotency_window() {
        let mut w = world();
        w.step(&drops(10, 1, ANA, 2, 2));
        w.step(&[reset_cmd(&action(99))]);
        let out = w.step(&[drop_cmd(&action(10), ANA, 2, 2), reset_cmd(&action(99))]);
        assert_eq!(outcomes(&out), vec![CommandOutcome::Duplicate; 2]);
        assert_eq!(w.epoch(), 1);
        assert_eq!(w.humidity_contributed(), 25);
    }

    #[test]
    fn reset_valley_splits_the_tick_in_before_and_after() {
        let mut w = world();
        w.step(&[
            drop_cmd(&action(1), ANA, 3, 3),
            reset_cmd(&action(2)),
            drop_cmd(&action(3), ANA, 4, 4),
        ]);
        assert_eq!(w.tile(3, 3).unwrap().humidity, 0);
        assert_eq!(w.tile(4, 4).unwrap().humidity, 25);
        assert_eq!(w.player(ANA).unwrap().water, 19);
    }

    #[test]
    fn every_reset_increments_the_epoch() {
        let mut w = world();
        w.step(&[reset_cmd(&action(1))]);
        w.step(&[reset_cmd(&action(2))]);
        assert_eq!(w.epoch(), 2);
        assert_eq!(w.parts().epoch, 2);
    }

    fn next_catalog() -> Catalog {
        let mut next = custom_catalog(); // capacidad 30, gota de 40
        next.tank.refill_interval_seconds = 1;
        next
    }

    #[test]
    fn an_applied_reset_adopts_the_offered_catalog() {
        let mut w = world();
        w.step(&drops(1, 1, ANA, 0, 0));
        let out = w.step_with_reset_catalog(&[reset_cmd(&action(9))], Some(next_catalog()));
        assert_eq!(outcomes(&out), vec![CommandOutcome::Applied]);
        assert_eq!(w.catalog_version(), "custom");
        let ana = w.player(ANA).unwrap();
        assert_eq!((ana.water, ana.tank_capacity), (30, 30));
        advance_to(&mut w, 12); // recarga cada 10 ticks con el catálogo nuevo
        assert_eq!(w.player(ANA).unwrap().water_refilled, 30);
    }

    #[test]
    fn a_duplicate_reset_keeps_the_current_catalog() {
        let mut w = world();
        w.step(&[reset_cmd(&action(9))]);
        let out = w.step_with_reset_catalog(&[reset_cmd(&action(9))], Some(next_catalog()));
        assert_eq!(outcomes(&out), vec![CommandOutcome::Duplicate]);
        assert_eq!(w.catalog_version(), Catalog::D01_VERSION);
        assert_eq!(w.epoch(), 1);
    }

    #[test]
    fn a_rejected_reset_keeps_the_current_catalog() {
        let mut w = world();
        let out = w.step_with_reset_catalog(&[reset_cmd("not-a-uuid")], Some(next_catalog()));
        assert_eq!(
            outcomes(&out),
            vec![CommandOutcome::Rejected(RejectReason::InvalidCommand)]
        );
        assert_eq!(w.catalog_version(), Catalog::D01_VERSION);
        assert_eq!(w.epoch(), 0);
    }

    #[test]
    fn an_offered_catalog_without_a_reset_is_ignored() {
        let mut w = world();
        w.step_with_reset_catalog(&drops(1, 1, ANA, 0, 0), Some(next_catalog()));
        assert_eq!(w.catalog_version(), Catalog::D01_VERSION);
    }

    // ---- Instantánea y determinismo -------------------------------------

    #[test]
    fn the_snapshot_carries_the_state_of_the_new_rules() {
        let mut w = world();
        w.step(&[demo_cmd(&action(1), false)]);
        w.step(&drops(10, 4, ANA, 6, 6)); // pasto, humedad 0
        advance_to(&mut w, 30); // dry_ticks > 0
        w.step(&drops(50, 2, BETO, 7, 7));
        w.step(&[reset_cmd(&action(98))]);
        w.step(&drops(60, 4, ANA, 6, 6));
        advance_to(&mut w, 45);
        w.step(&[demo_cmd(&action(97), true)]);
        let parts = w.parts();
        assert_eq!(parts.epoch, 1);
        assert!(parts.demo_mode);
        assert!(parts.tiles[6 * 32 + 6].dry_ticks > 0);
        let restored = World::restore(Catalog::d01(), parts.clone()).unwrap();
        assert_eq!(restored.parts(), parts);
    }

    /// Guion fijo de comandos para las pruebas de determinismo.
    fn script(tick: u64) -> Vec<Command> {
        let n = (tick * 10) as u32;
        let mut commands = Vec::new();
        match tick % 7 {
            0 => commands.extend(drops(n, 5, ANA, (tick % 5) as u32, 1)),
            2 => commands.extend(drops(n, 3, BETO, (tick % 5) as u32, 1)),
            4 => commands.extend(drops(n, 4, ADMIN, 2, 2)),
            _ => {}
        }
        if tick == 50 {
            commands.push(demo_cmd(&action(9001), true));
        }
        if tick == 120 {
            commands.push(demo_cmd(&action(9002), false));
        }
        if tick == 200 {
            commands.push(reset_cmd(&action(9003)));
        }
        commands
    }

    fn run(w: &mut World, from: u64, to: u64) {
        for tick in from..=to {
            w.step(&script(tick));
        }
    }

    #[test]
    fn the_same_commands_always_produce_the_same_valley() {
        // CP-10 sin azar todavía: dos ejecuciones del mismo guion dan el
        // mismo valle.
        let mut first = world();
        let mut second = world();
        run(&mut first, 1, 400);
        run(&mut second, 1, 400);
        assert_eq!(first.parts(), second.parts());
        assert_eq!(first.epoch(), 1);
        assert!(first.humidity_contributed() > 0);
    }

    #[test]
    fn restoring_a_snapshot_mid_run_does_not_change_the_outcome() {
        let mut straight = world();
        run(&mut straight, 1, 400);

        let mut interrupted = world();
        run(&mut interrupted, 1, 150);
        let mut resumed = World::restore(Catalog::d01(), interrupted.parts()).unwrap();
        run(&mut resumed, 151, 400);
        assert_eq!(resumed.parts(), straight.parts());
    }
}
