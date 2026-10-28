//! El valle y su paso de simulación (escritor único, Capa 2).

use std::collections::{BTreeMap, HashSet};

use uuid::Uuid;

use super::catalog::Catalog;
use super::command::{
    Command, CommandKind, CommandOutcome, CommandResult, RejectReason, StepOutcome,
};
use super::fire;
use super::growth;
use super::limits::{RECENT_ACTIONS_WINDOW_TICKS, TICKS_PER_SECOND, VALLEY_HEIGHT, VALLEY_WIDTH};
use super::player::{AppliedAction, OfflineSummary, Player};
use super::rng::SplitMix64;
use super::tile::{Installation, InstallationKind, Tile, TileStage, UpgradeKind};

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
    /// `rng_state` no vacío que no mide los 8 bytes del generador.
    RngState {
        len: usize,
    },
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
            Self::RngState { len } => write!(
                f,
                "el estado del generador mide {len} bytes y debería medir {}",
                super::rng::STATE_LEN
            ),
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
    seed: u64,
    tick: u64,
    epoch: u32,
    rng: SplitMix64,
    demo_mode: bool,
    humidity_contributed: u64,
    /// Dimensiones del valle: siempre las del juego salvo en la comparativa
    /// (`with_size`).
    width: u32,
    height: u32,
    tiles: Vec<Tile>,
    players: BTreeMap<String, PlayerSlot>,
    /// Catálogo ofrecido a los reinicios del tick en curso; solo existe
    /// durante `step_with_reset_catalog`. No viaja en la instantánea.
    reset_catalog: Option<Catalog>,
}

impl World {
    /// Valle vacío en el tick 0 con la semilla 0, solo para las pruebas.
    #[cfg(test)]
    pub fn new(catalog: Catalog) -> Self {
        Self::with_seed(catalog, 0)
    }

    /// Valle vacío en el tick 0 (LoadSnapshot devolvió NOT_FOUND). La
    /// semilla fija toda la secuencia de incendios (regla 6): quien arranca
    /// el servicio la elige y la registra.
    pub fn with_seed(catalog: Catalog, seed: u64) -> Self {
        Self::with_size(catalog, seed, VALLEY_WIDTH, VALLEY_HEIGHT)
    }

    /// Valle vacío de `width` × `height` casillas, **solo para la
    /// comparativa de rendimiento** (`examples/bench.rs`): el juego usa
    /// siempre 32 × 32 (`with_seed`). Un valle de otro tamaño no se puede
    /// restaurar con [`restore`](Self::restore), que exige el tamaño del juego.
    pub fn with_size(catalog: Catalog, seed: u64, width: u32, height: u32) -> Self {
        let tile_count = tile_count(width, height);
        let mut tiles = vec![Tile::default(); tile_count];
        if seed != 0 {
            generate_natural_terrain(&mut tiles, width, height, seed);
        }
        Self {
            catalog,
            seed,
            tick: 0,
            epoch: 0,
            rng: SplitMix64::new(seed),
            demo_mode: false,
            humidity_contributed: 0,
            width,
            height,
            tiles,
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
        // Una instantánea anterior a los incendios no trae estado del
        // generador: se siembra con su tick, que es determinista.
        let rng = if parts.rng_state.is_empty() {
            SplitMix64::new(parts.tick)
        } else {
            SplitMix64::from_bytes(&parts.rng_state).ok_or(RestoreError::RngState {
                len: parts.rng_state.len(),
            })?
        };
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
            seed: parts.tick,
            tick: parts.tick,
            epoch: parts.epoch,
            rng,
            demo_mode: parts.demo_mode,
            humidity_contributed: parts.humidity_contributed,
            width: VALLEY_WIDTH,
            height: VALLEY_HEIGHT,
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
            rng_state: self.rng.to_bytes(),
            demo_mode: self.demo_mode,
            humidity_contributed: self.humidity_contributed,
            width: self.width,
            height: self.height,
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
    /// 4. Plantas de tratamiento: litros extra de recarga a su dueño, si `t`
    ///    es múltiplo de su intervalo.
    /// 5. Aspersores: humedad a su área (y hojas a su dueño si algo crece),
    ///    si `t` es múltiplo de su intervalo. Apagan los fuegos de su área.
    /// 6. Fuego: un tick más a cada casilla que arde; a los 300 se consume
    ///    y vuelve a tierra seca. También en modo demo.
    /// 7. Evaporación, si `t` es múltiplo de [`TICKS_PER_SECOND`] (salvo en
    ///    modo demo), con el goteo dividiéndola en su área. También en las
    ///    casillas que arden.
    /// 8. Marchitamiento: un tick más de cuenta a cada planta seca (salvo en
    ///    modo demo y en las que arden).
    /// 9. Propagación y luego ignición al azar (salvo en modo demo): primero,
    ///    un sorteo por cada casilla elegible con alguna vecina ortogonal que
    ///    ardía al empezar el paso; después, un sorteo por cada casilla
    ///    elegible, por filas, con el generador con semilla.
    /// 10. El tick pasa a `t`.
    ///
    /// Las instalaciones (4, 5 y el goteo de 7) solo actúan mientras su dueño
    /// no lleve ausente más de `offline_installation_seconds` (RF-25).
    ///
    /// Solo la propagación y la ignición usan el azar, y lo toman del generador con semilla del
    /// mundo, cuyo estado viaja en la instantánea: misma semilla y mismos
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
            Some(CommandKind::Drop { x, y }) => self.drop(&command.player_id, *x, *y, tick),
            Some(CommandKind::SetDemoMode { enabled }) => {
                self.demo_mode = *enabled;
                CommandOutcome::Applied
            }
            Some(CommandKind::ResetValley) => {
                self.reset_valley();
                CommandOutcome::Applied
            }
            Some(CommandKind::PlaceInstallation { kind, x, y }) => {
                self.place_installation(&command.player_id, *kind, *x, *y)
            }
            Some(CommandKind::BuyUpgrade { kind }) => self.buy_upgrade(&command.player_id, *kind),
            Some(CommandKind::CleanTrash { x, y }) => {
                self.clean_trash(&command.player_id, *x, *y)
            }
        };
        if outcome == CommandOutcome::Applied {
            // El comando aplicado ya dio de alta al jugador si era nuevo.
            let slot = self.player_slot(&command.player_id);
            slot.record.last_action_tick = tick;
            slot.record.offline_summary = OfflineSummary::default();
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
    /// Con la mejora de gota más grande la gota humedece el cuadrado de
    /// `bigger_drop.area_side` casillas de lado centrado en (x, y), recortado
    /// por los bordes del valle; cada casilla recibe la humedad entera de la
    /// gota, cuesta el mismo litro y las hojas de todas las que crezcan son
    /// de quien la soltó (casillas en orden de fila). `humidity_contributed`
    /// suma la humedad que la gota entrega en total (humedad × casillas del
    /// valle alcanzadas), crezcan o no. En modo demo la gota es de una sola
    /// casilla aunque el jugador tenga la mejora (D-01), así que cada gota
    /// aceptada suma exactamente `demo_humidity` y el invariante de RF-30
    /// se cumple con cualquier mejora.
    ///
    /// En el área 5x5 de un riego por goteo activo, el aporte de humedad por
    /// gota se duplica (salvo en modo demo).
    ///
    /// Una casilla ardiendo recibe la gota igual (suma humedad, pero no crece)
    /// y la cuenta: con la que completa `fire.drops_to_extinguish` (3) el
    /// fuego se apaga, el crecimiento que quedó pendiente se resuelve en ese
    /// momento y el jugador de esa gota gana `fire.extinguish_bonus_leaves`
    /// (suma a `leaves` y a `leaves_earned`). Como los comandos se aplican en
    /// orden de admisión, con varias gotas en el mismo tick el fuego se apaga
    /// una sola vez y el bono es de quien soltó la tercera. Con la gota más
    /// grande cuenta como una gota para cada casilla ardiendo que alcanza.
    /// Si completa la tercera gota de varios fuegos, paga el bono una vez por
    /// cada fuego apagado (cada uno es una extinción distinta).
    fn drop(&mut self, player_id: &str, x: u32, y: u32, tick: u64) -> CommandOutcome {
        let Some(center_index) = self.tile_index(x, y) else {
            return CommandOutcome::Rejected(RejectReason::OutOfBounds);
        };
        if self.tiles[center_index].stage.is_natural() || self.tiles[center_index].has_building {
            return CommandOutcome::Rejected(RejectReason::TileOccupied);
        }
        let (humidity, cost) = if self.demo_mode {
            (self.catalog.drop.demo_humidity, 0)
        } else {
            (self.catalog.drop.humidity, self.catalog.drop.water_cost)
        };
        let known = self.players.get(player_id).map(|slot| &slot.record);
        if known.is_none() && self.players.len() >= MAX_PLAYERS {
            return CommandOutcome::Rejected(RejectReason::WorldFull);
        }
        if known.map_or(self.catalog.tank.capacity, |p| p.water) < cost {
            return CommandOutcome::Rejected(RejectReason::InsufficientWater);
        }
        let side =
            if !self.demo_mode && known.is_some_and(|p| p.has_upgrade(UpgradeKind::BiggerDrop)) {
                self.catalog.bigger_drop.area_side
            } else {
                1
            };
        // Solo aquí, con la gota ya aceptada, entra un jugador nuevo.
        let drip_covered = self.active_drip_coverage(tick);
        let area = area_indices(x, y, side, self.width, self.height);
        let mut leaves = 0u64;
        let mut total_humidity = 0u64;
        for &index in &area {
            let tile = &mut self.tiles[index];
            if tile.stage.is_natural() || tile.has_building || tile.trash_level > 0 {
                continue;
            }
            let effective_humidity = if !self.demo_mode && drip_covered[index] {
                humidity.saturating_mul(2)
            } else {
                humidity
            };
            total_humidity = total_humidity.saturating_add(effective_humidity);
            leaves = leaves.saturating_add(growth::add_humidity(
                tile,
                effective_humidity,
                &self.catalog.growth,
            ));
            if fire::receive_drop(tile, &self.catalog.fire) {
                leaves = leaves
                    .saturating_add(growth::add_humidity(tile, 0, &self.catalog.growth))
                    .saturating_add(self.catalog.fire.extinguish_bonus_leaves);
            }
        }
        self.humidity_contributed = self.humidity_contributed.saturating_add(total_humidity);

        let player = &mut self.player_slot(player_id).record;
        player.water -= cost;
        player.water_spent = player.water_spent.saturating_add(cost);
        player.leaves = player.leaves.saturating_add(leaves);
        player.leaves_earned = player.leaves_earned.saturating_add(leaves);
        CommandOutcome::Applied
    }

    /// Compra y coloca una instalación (RF-04, RF-07). Se comprueba en este
    /// orden, y un rechazo no cobra nada ni da de alta al jugador: casilla
    /// dentro del valle, casilla libre (una instalación por casilla, de
    /// cualquier jugador), mundo con sitio para un jugador nuevo y hojas
    /// suficientes para el precio del catálogo con el aumento por compras.
    fn place_installation(
        &mut self,
        player_id: &str,
        kind: InstallationKind,
        x: u32,
        y: u32,
    ) -> CommandOutcome {
        let Some(index) = self.tile_index(x, y) else {
            return CommandOutcome::Rejected(RejectReason::OutOfBounds);
        };
        if self.tiles[index].installation.is_some()
            || self.tiles[index].stage.is_natural()
            || self.tiles[index].has_building
            || self.tiles[index].trash_level > 0
        {
            return CommandOutcome::Rejected(RejectReason::TileOccupied);
        }
        let known = self.players.get(player_id).map(|slot| &slot.record);
        if known.is_none() && self.players.len() >= MAX_PLAYERS {
            return CommandOutcome::Rejected(RejectReason::WorldFull);
        }
        let price = self
            .catalog
            .installation_price(kind, known.map_or(0, |p| p.purchases_of(kind)));
        if known.map_or(0, |p| p.leaves) < price {
            return CommandOutcome::Rejected(RejectReason::InsufficientLeaves);
        }
        let player = &mut self.player_slot(player_id).record;
        player.spend_leaves(price);
        player.record_purchase(kind);
        let durability = self.catalog.installation_base_durability(kind);
        self.tiles[index].installation = Some(Installation {
            kind,
            owner_id: player_id.to_string(),
            remaining_durability: durability,
        });
        CommandOutcome::Applied
    }

    /// Compra una mejora, que es única por jugador (RF-07): ya comprada,
    /// mundo lleno para un jugador nuevo y hojas insuficientes, en ese orden.
    fn buy_upgrade(&mut self, player_id: &str, kind: UpgradeKind) -> CommandOutcome {
        let known = self.players.get(player_id).map(|slot| &slot.record);
        if known.is_some_and(|p| p.has_upgrade(kind)) {
            return CommandOutcome::Rejected(RejectReason::UpgradeAlreadyOwned);
        }
        if known.is_none() && self.players.len() >= MAX_PLAYERS {
            return CommandOutcome::Rejected(RejectReason::WorldFull);
        }
        let price = self.catalog.upgrade_price(kind);
        if known.map_or(0, |p| p.leaves) < price {
            return CommandOutcome::Rejected(RejectReason::InsufficientLeaves);
        }
        let extra_capacity = self.catalog.bigger_tank.extra_capacity;
        let player = &mut self.player_slot(player_id).record;
        player.spend_leaves(price);
        player.upgrades.push(kind);
        if kind == UpgradeKind::BiggerTank {
            player.tank_capacity = player.tank_capacity.saturating_add(extra_capacity);
        }
        CommandOutcome::Applied
    }

    /// Limpia un nivel de basura de una casilla (Fase 4).
    /// Cada ejecución reduce en 1 el trash_level. Al llegar a 0, la casilla
    /// retorna a tierra seca y otorga hojas de incentivo (+2).
    fn clean_trash(&mut self, player_id: &str, x: u32, y: u32) -> CommandOutcome {
        let Some(index) = self.tile_index(x, y) else {
            return CommandOutcome::Rejected(RejectReason::OutOfBounds);
        };
        let tile = &mut self.tiles[index];
        if tile.trash_level == 0 {
            return CommandOutcome::Rejected(RejectReason::InvalidCommand);
        }
        tile.trash_level = tile.trash_level.saturating_sub(1);
        let mut incentive = 0u64;
        if tile.trash_level == 0 {
            tile.stage = TileStage::DrySoil;
            incentive = 1;
        }
        let player = &mut self.player_slot(player_id).record;
        player.leaves = player.leaves.saturating_add(incentive);
        player.leaves_earned = player.leaves_earned.saturating_add(incentive);
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

    /// ¿Trabajan las instalaciones de `owner` en el tick `tick`? Sí mientras
    /// no hayan pasado `offline_installation_seconds` (7.200 s = 72.000
    /// ticks) desde su última acción aplicada, ese límite incluido (RF-25).
    /// Una acción nueva del dueño las reactiva.
    fn owner_is_active(&self, owner: &str, tick: u64) -> bool {
        let window = self
            .catalog
            .offline_installation_seconds
            .saturating_mul(TICKS_PER_SECOND);
        self.players
            .get(owner)
            .is_some_and(|slot| tick.saturating_sub(slot.record.last_action_tick) <= window)
    }

    /// Instalaciones de `kind` que trabajan en `tick`, en orden de casilla
    /// (fila a fila): índice de la casilla y dueño.
    fn active_installations(&self, kind: InstallationKind, tick: u64) -> Vec<(usize, String)> {
        self.tiles
            .iter()
            .enumerate()
            .filter_map(|(index, tile)| {
                let installation = tile.installation.as_ref()?;
                (installation.kind == kind && self.owner_is_active(&installation.owner_id, tick))
                    .then(|| (index, installation.owner_id.clone()))
            })
            .collect()
    }

    /// Casillas cubiertas por al menos un riego por goteo activo en `tick`.
    fn active_drip_coverage(&self, tick: u64) -> Vec<bool> {
        let mut covered = vec![false; self.tiles.len()];
        let side = self.catalog.drip.area_side;
        for (index, _) in self.active_installations(InstallationKind::Drip, tick) {
            let (x, y) = self.tile_xy(index);
            for tile_idx in area_indices(x, y, side, self.width, self.height) {
                covered[tile_idx] = true;
            }
        }
        covered
    }

    /// Planta de tratamiento (Reciclar): cada `treatment_plant.interval_seconds`
    /// suma `treatment_plant.refill_amount` litros extra al depósito de su
    /// dueño, sin pasar de la capacidad. Solo lo que cabe cuenta como
    /// recargado (RF-27) y como `water_recycled`.
    /// Además emite un pulso periódico 3x3 que sube +1 etapa vegetal a las
    /// casillas vecinas y acredita las hojas al propietario.
    fn run_treatment_plants(&mut self, tick: u64) {
        let rules = &self.catalog.treatment_plant;
        let interval = u64::from(rules.interval_seconds).saturating_mul(TICKS_PER_SECOND);
        if !tick.is_multiple_of(interval) {
            return;
        }
        let amount = rules.refill_amount;
        for (index, owner) in self.active_installations(InstallationKind::TreatmentPlant, tick) {
            let (x, y) = self.tile_xy(index);
            let area = area_indices(x, y, 3, self.width, self.height);
            let mut leaves_earned = 0u64;
            let mut tiles_grown = 0u64;
            for &neighbor_idx in &area {
                if neighbor_idx == index {
                    continue;
                }
                let tile = &mut self.tiles[neighbor_idx];
                if tile.fire.is_some() || tile.stage.is_natural() {
                    continue;
                }
                if let Some(next) = tile.stage.next() {
                    tile.stage = next;
                    tile.dry_ticks = 0;
                    if tile.stage == TileStage::Tree {
                        tile.humidity = tile.humidity.min(self.catalog.growth.tree_max_humidity);
                    }
                    let earned = self.catalog.growth.leaves_for(next);
                    leaves_earned = leaves_earned.saturating_add(earned);
                    tiles_grown = tiles_grown.saturating_add(1);
                }
            }

            let player = &mut self.player_slot(&owner).record;
            let added = amount.min(player.tank_capacity.saturating_sub(player.water));
            player.water = player.water.saturating_add(added);
            player.water_refilled = player.water_refilled.saturating_add(added);
            player.water_recycled = player.water_recycled.saturating_add(added);

            player.leaves = player.leaves.saturating_add(leaves_earned);
            player.leaves_earned = player.leaves_earned.saturating_add(leaves_earned);
            player.tiles_grown = player.tiles_grown.saturating_add(tiles_grown);

            if let Some(inst) = self.tiles[index].installation.as_mut() {
                inst.remaining_durability = inst.remaining_durability.saturating_sub(1);
                if inst.remaining_durability == 0 {
                    self.tiles[index].installation = None;
                }
            }
        }
    }

    /// Aspersor de agua gris (Reutilizar): cada `sprinkler.interval_seconds`
    /// suma `sprinkler.humidity` a cada casilla de su cuadrado de
    /// `sprinkler.area_side` casillas de lado (recortado por los bordes).
    /// No gasta agua del depósito. Si una casilla crece, las hojas son del
    /// dueño del aspersor. Su `water_reused` suma la humedad entregada
    /// (humedad × casillas del valle alcanzadas). Sigue actuando en modo
    /// demo y no toca `humidity_contributed`, que es solo de las gotas.
    ///
    /// Al regar apaga los fuegos de su zona (D-01). Es un apagado sin bono:
    /// el bono es de quien suelta la tercera gota, y el aspersor no cuenta
    /// gotas. Si el fuego tenía crecimiento pendiente, se resuelve ahí y las
    /// hojas son del dueño, como con cualquier riego.
    ///
    /// Lo que hace cada aspersor se suma además al resumen al volver de su
    /// dueño (RF-25) y a sus acumulados de la época. Una casilla cuenta una
    /// vez por riego aunque suba en las dos fases (riego y apagado).
    fn run_sprinklers(&mut self, tick: u64) {
        let rules = &self.catalog.sprinkler;
        let interval = u64::from(rules.interval_seconds).saturating_mul(TICKS_PER_SECOND);
        if !tick.is_multiple_of(interval) {
            return;
        }
        let (side, humidity) = (rules.area_side, rules.humidity);
        for (index, owner) in self.active_installations(InstallationKind::Sprinkler, tick) {
            let (x, y) = self.tile_xy(index);
            let area = area_indices(x, y, side, self.width, self.height);
            let mut effect = OfflineSummary::default();
            for &index in &area {
                let tile = &mut self.tiles[index];
                if tile.stage.is_natural() {
                    continue;
                }
                let stage_before = tile.stage;
                effect.leaves_earned = effect.leaves_earned.saturating_add(growth::add_humidity(
                    tile,
                    humidity,
                    &self.catalog.growth,
                ));
                if fire::extinguish(tile) {
                    effect.fires_extinguished = effect.fires_extinguished.saturating_add(1);
                    // El crecimiento que quedó pendiente se resuelve ahora.
                    effect.leaves_earned = effect
                        .leaves_earned
                        .saturating_add(growth::add_humidity(tile, 0, &self.catalog.growth));
                }
                if tile.stage != stage_before {
                    effect.tiles_grown = effect.tiles_grown.saturating_add(1);
                }
            }
            effect.water_reused = humidity.saturating_mul(area.len() as u64);
            let player = &mut self.player_slot(&owner).record;
            player.leaves = player.leaves.saturating_add(effect.leaves_earned);
            player.leaves_earned = player.leaves_earned.saturating_add(effect.leaves_earned);
            player.water_reused = player.water_reused.saturating_add(effect.water_reused);
            player.fires_extinguished = player
                .fires_extinguished
                .saturating_add(effect.fires_extinguished);
            player.tiles_grown = player.tiles_grown.saturating_add(effect.tiles_grown);
            player.offline_summary.add(&effect);

            if let Some(inst) = self.tiles[index].installation.as_mut() {
                inst.remaining_durability = inst.remaining_durability.saturating_sub(1);
                if inst.remaining_durability == 0 {
                    self.tiles[index].installation = None;
                }
            }
        }
    }

    /// Riego por goteo: cada 2 segundos (20 ticks) de trabajo activo descuenta
    /// 1 punto de durabilidad. Al llegar a cero se destruye y desaparece.
    fn run_drips(&mut self, tick: u64) {
        let interval = 2 * TICKS_PER_SECOND;
        if !tick.is_multiple_of(interval) {
            return;
        }
        for (index, _) in self.active_installations(InstallationKind::Drip, tick) {
            if let Some(inst) = self.tiles[index].installation.as_mut() {
                inst.remaining_durability = inst.remaining_durability.saturating_sub(1);
                if inst.remaining_durability == 0 {
                    self.tiles[index].installation = None;
                }
            }
        }
    }

    /// Cesto de Basura / Reciclaje (Fase 4): cada 2 segundos (20 ticks) reduce 1 nivel
    /// de basura en su radio 3 x 3 y descuenta 1 punto de su durabilidad.
    fn run_trash_bins(&mut self, tick: u64) {
        let rules = &self.catalog.trash_bin;
        let interval = u64::from(rules.interval_seconds).saturating_mul(TICKS_PER_SECOND);
        if !tick.is_multiple_of(interval) {
            return;
        }
        let side = rules.area_side;
        for (index, _) in self.active_installations(InstallationKind::TrashBin, tick) {
            let (x, y) = self.tile_xy(index);
            for tile_idx in area_indices(x, y, side, self.width, self.height) {
                let tile = &mut self.tiles[tile_idx];
                if tile.trash_level > 0 {
                    tile.trash_level = tile.trash_level.saturating_sub(1);
                    if tile.trash_level == 0 {
                        tile.stage = TileStage::DrySoil;
                    }
                }
            }
            if let Some(inst) = self.tiles[index].installation.as_mut() {
                inst.remaining_durability = inst.remaining_durability.saturating_sub(1);
                if inst.remaining_durability == 0 {
                    self.tiles[index].installation = None;
                }
            }
        }
    }

    /// Generación de estructuras de 1 casilla tras alcanzar umbral de progreso (Fase 4).
    /// Inmunes a fuego y basura.
    fn spawn_urban_buildings(&mut self) {
        let tree_count = self.tiles.iter().filter(|t| t.stage == TileStage::Tree).count();
        if tree_count < 2 && self.humidity_contributed < 500 {
            return;
        }
        let building_count = self.tiles.iter().filter(|t| t.has_building).count();
        if building_count >= 2 {
            return;
        }
        let (width, height) = (self.width, self.height);
        let mut perimeter = Vec::new();
        for x in 0..width {
            perimeter.push((x, 0));
            perimeter.push((x, height - 1));
        }
        for y in 1..height - 1 {
            perimeter.push((0, y));
            perimeter.push((width - 1, y));
        }
        for (x, y) in perimeter {
            let Some(idx) = self.tile_index(x, y) else {
                continue;
            };
            let tile = &self.tiles[idx];
            if !tile.stage.is_natural()
                && !tile.has_building
                && tile.stage == TileStage::DrySoil
                && tile.installation.is_none()
                && tile.fire.is_none()
                && tile.trash_level == 0
            {
                self.tiles[idx].has_building = true;
                break;
            }
        }
    }

    /// En ciclos fijos (cada 60 ticks = 6 s), el edificio suma +1 a trash_level
    /// en una casilla vecina (3 x 3), eliminando plantas e instalaciones preexistentes (Fase 4).
    fn emit_trash(&mut self, tick: u64) {
        let interval = 6 * TICKS_PER_SECOND;
        if !tick.is_multiple_of(interval) {
            return;
        }
        let building_indices: Vec<usize> = self
            .tiles
            .iter()
            .enumerate()
            .filter(|(_, t)| t.has_building)
            .map(|(i, _)| i)
            .collect();

        for b_idx in building_indices {
            let (bx, by) = self.tile_xy(b_idx);
            let area = area_indices(bx, by, 3, self.width, self.height);
            let candidate = area
                .iter()
                .copied()
                .find(|&idx| {
                    idx != b_idx
                        && !self.tiles[idx].stage.is_natural()
                        && !self.tiles[idx].has_building
                        && self.tiles[idx].trash_level < 3
                })
                .or_else(|| {
                    area.into_iter().find(|&idx| {
                        idx != b_idx
                            && !self.tiles[idx].stage.is_natural()
                            && !self.tiles[idx].has_building
                    })
                });
            if let Some(target_idx) = candidate {
                let tile = &mut self.tiles[target_idx];
                tile.trash_level = (tile.trash_level + 1).min(3);
                tile.stage = TileStage::DrySoil;
                tile.humidity = 0;
                tile.installation = None;
                tile.fire = None;
            }
        }
    }

    /// Una casilla con trash_level >= 2 incrementa en +1 su propia basura (hasta 3)
    /// y propaga +1 de basura a sus casillas adyacentes no protegidas cada 8 s (Fase 4).
    fn propagate_trash(&mut self, tick: u64) {
        let interval = 8 * TICKS_PER_SECOND;
        if !tick.is_multiple_of(interval) {
            return;
        }
        let mut polluted_count = self.tiles.iter().filter(|t| t.trash_level > 0).count();
        if polluted_count >= 60 {
            return;
        }
        let trash_bins = self.active_installations(InstallationKind::TrashBin, tick);
        let mut protected = vec![false; self.tiles.len()];
        for (tb_idx, _) in trash_bins {
            let (tx, ty) = self.tile_xy(tb_idx);
            for p_idx in area_indices(tx, ty, 3, self.width, self.height) {
                protected[p_idx] = true;
            }
        }

        let source_indices: Vec<usize> = self
            .tiles
            .iter()
            .enumerate()
            .filter(|(_, t)| t.trash_level >= 1)
            .map(|(i, _)| i)
            .collect();

        for s_idx in source_indices {
            if protected[s_idx] {
                continue;
            }
            let current_level = self.tiles[s_idx].trash_level;
            if current_level < 3 {
                self.tiles[s_idx].trash_level = current_level + 1;
            }
            if current_level >= 2 {
                let (x, y) = self.tile_xy(s_idx);
                for n_idx in orthogonal_neighbor_indices(x, y, self.width, self.height) {
                    let tile = &mut self.tiles[n_idx];
                    if !protected[n_idx]
                        && !tile.stage.is_natural()
                        && !tile.has_building
                        && tile.trash_level < 3
                    {
                        if tile.trash_level == 0 {
                            if polluted_count >= 60 {
                                continue;
                            }
                            polluted_count += 1;
                        }
                        tile.trash_level += 1;
                        tile.stage = TileStage::DrySoil;
                        tile.humidity = 0;
                        tile.installation = None;
                        tile.fire = None;
                    }
                }
            }
        }
    }

    /// Evaporación del segundo, con el goteo (Reducir): en el cuadrado de
    /// `drip.area_side` casillas de lado de un riego por goteo la casilla
    /// pierde `humidity_per_second / evaporation_divisor` (división entera,
    /// hacia abajo) en vez de `humidity_per_second`. Varios goteos que cubren
    /// la misma casilla no se acumulan: el divisor se aplica una sola vez y
    /// el ahorro se acredita al primero en orden de casilla. `water_reduced`
    /// suma la humedad que la casilla habría perdido de más sin goteo:
    /// `min(humedad, evaporación) - min(humedad, evaporación reducida)`. Una
    /// casilla ardiendo se evapora (y ahorra) como cualquier otra: el fuego
    /// congela la etapa, no la humedad.
    fn shoreline_mask(&self) -> Vec<bool> {
        let (width, height) = (self.width, self.height);
        let mut mask = vec![false; self.tiles.len()];
        for y in 0..height {
            for x in 0..width {
                let idx = y as usize * width as usize + x as usize;
                if self.tiles[idx].stage != TileStage::Water {
                    mask[idx] = orthogonal_neighbor_indices(x, y, width, height)
                        .any(|n| self.tiles[n].stage == TileStage::Water);
                }
            }
        }
        mask
    }

    /// Evaporación del segundo, con el goteo (Reducir) y la orilla de agua: en el cuadrado de
    /// `drip.area_side` casillas de lado de un riego por goteo la casilla
    /// pierde `humidity_per_second / evaporation_divisor`. En la orilla de agua,
    /// la pérdida se reduce a la mitad.
    fn evaporate_valley(&mut self, tick: u64, shoreline: &[bool]) {
        let amount = self.catalog.evaporation.humidity_per_second;
        let reduced = amount / u64::from(self.catalog.drip.evaporation_divisor).max(1);
        let shoreline_amount = amount / 2;
        let drips = self.active_installations(InstallationKind::Drip, tick);
        let side = self.catalog.drip.area_side;
        // Para cada casilla, qué goteo la cubre (el primero en orden).
        let mut cover: Vec<Option<usize>> = vec![None; self.tiles.len()];
        for (n, (index, _)) in drips.iter().enumerate() {
            let (x, y) = self.tile_xy(*index);
            for tile in area_indices(x, y, side, self.width, self.height) {
                cover[tile].get_or_insert(n);
            }
        }
        let mut saved = vec![0u64; drips.len()];
        for (idx, (tile, covered_by)) in self.tiles.iter_mut().zip(cover).enumerate() {
            if tile.stage.is_natural() {
                continue;
            }
            let base_evap = if shoreline[idx] { shoreline_amount } else { amount };
            let Some(n) = covered_by else {
                growth::evaporate(tile, base_evap);
                continue;
            };
            let effective_reduced = if shoreline[idx] {
                reduced.min(shoreline_amount)
            } else {
                reduced
            };
            let h = tile.humidity;
            saved[n] = saved[n].saturating_add(h.min(base_evap) - h.min(effective_reduced));
            growth::evaporate(tile, effective_reduced);
        }
        for ((_, owner), amount) in drips.iter().zip(saved) {
            let player = &mut self.player_slot(owner).record;
            player.water_reduced = player.water_reduced.saturating_add(amount);
        }
    }

    /// Lo que cambia solo con el paso del tiempo, después de los comandos
    /// (ver el orden en la documentación de [`World::step`]).
    fn advance_valley(&mut self, tick: u64) {
        self.refill_tanks(tick);
        self.run_treatment_plants(tick);
        self.run_sprinklers(tick);
        self.run_drips(tick);
        self.run_trash_bins(tick);
        // Los fuegos avanzan también en modo demo: un fuego siempre se
        // resuelve (gotas, aspersor o consumirse).
        self.burn_fires();
        // El modo demo congela el valle: sin evaporación, marchitamiento ni
        // incendios nuevos. Las instalaciones de arriba siguen actuando; el
        // goteo no tiene qué frenar.
        if self.demo_mode {
            return;
        }
        self.spawn_urban_buildings();
        self.emit_trash(tick);
        self.propagate_trash(tick);
        let drip_covered = self.active_drip_coverage(tick);
        let shoreline = self.shoreline_mask();
        // La cifra del catálogo es por segundo: se aplica entera una vez por
        // segundo (cada TICKS_PER_SECOND ticks), nunca como fracción.
        if tick.is_multiple_of(TICKS_PER_SECOND) {
            self.evaporate_valley(tick, &shoreline);
        }
        for (index, tile) in self.tiles.iter_mut().enumerate() {
            if !drip_covered[index] && !shoreline[index] {
                growth::wither(tile, &self.catalog.evaporation);
            }
        }
        self.propagate_fires(&drip_covered, &shoreline);
        self.ignite_fires(&drip_covered, &shoreline);
    }

    /// Un tick más de fuego en cada casilla que arde; las que llegan a
    /// `fire.burn_out_ticks` (300) se consumen y vuelven a tierra seca.
    fn burn_fires(&mut self) {
        for tile in &mut self.tiles {
            fire::burn(tile, &self.catalog.fire);
        }
    }

    /// Propagación del fuego a casillas vecinas (nivel 2, Tarea B3).
    ///
    /// Recorre las casillas por filas. Para cada casilla elegible (con planta,
    /// humedad 0 y sin fuego) que tenga al menos una vecina ortogonal que ya
    /// estuviera ardiendo al inicio de la fase, hace un sorteo con
    /// `fire.propagation_chance_ppm`. Si sale, la casilla se enciende. Solo
    /// gasta un número la casilla elegible con vecina en llamas.
    /// Casillas bajo el área 5x5 de un goteo activo y orillas de agua están protegidas.
    /// Solo propaga desde los incendios que ya ardían al entrar al paso,
    /// evitando cascadas instantáneas en un mismo tick.
    fn propagate_fires(&mut self, drip_covered: &[bool], shoreline: &[bool]) {
        let ppm = self.catalog.fire.propagation_chance_ppm;
        let burning_at_start: Vec<bool> = self.tiles.iter().map(|t| t.fire.is_some()).collect();
        if !burning_at_start.contains(&true) {
            return;
        }

        let (width, height) = (self.width, self.height);
        for y in 0..height {
            for x in 0..width {
                let index = y as usize * width as usize + x as usize;
                let tile = &mut self.tiles[index];
                if !drip_covered[index]
                    && !shoreline[index]
                    && fire::is_eligible(tile)
                    && orthogonal_neighbor_indices(x, y, width, height).any(|n| burning_at_start[n])
                    && self.rng.chance_ppm(ppm)
                {
                    fire::ignite(tile);
                }
            }
        }
    }

    /// Ignición al azar (RF-29): recorre las casillas por filas y, por cada
    /// una elegible (con planta, humedad 0 y sin fuego), hace un sorteo con
    /// `fire.ignition_chance_ppm` partes por millón. Casillas bajo el área
    /// 5x5 de un goteo activo y orillas de agua tienen la ignición suprimida.
    fn ignite_fires(&mut self, drip_covered: &[bool], shoreline: &[bool]) {
        let ppm = self.catalog.fire.ignition_chance_ppm;
        for (index, tile) in self.tiles.iter_mut().enumerate() {
            if !drip_covered[index] && !shoreline[index] && fire::is_eligible(tile) && self.rng.chance_ppm(ppm) {
                fire::ignite(tile);
            }
        }
    }

    /// Reinicio del valle (RF-31): el mundo vuelve a su estado inicial y los
    /// jugadores a los valores de arranque del catálogo, con los contadores
    /// de RF-27 y de las 3R a cero. Regenera de forma determinista el terreno natural.
    fn reset_valley(&mut self) {
        if let Some(catalog) = self.reset_catalog.take() {
            self.catalog = catalog;
        }
        self.tiles.fill(Tile::default());
        if self.seed != 0 {
            generate_natural_terrain(&mut self.tiles, self.width, self.height, self.seed);
        }
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
        (x < self.width && y < self.height).then(|| y as usize * self.width as usize + x as usize)
    }

    /// Coordenadas de la casilla de índice `index` (por filas).
    fn tile_xy(&self, index: usize) -> (u32, u32) {
        let width = self.width as usize;
        ((index % width) as u32, (index / width) as u32)
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
        self.width
    }

    pub fn height(&self) -> u32 {
        self.height
    }

    /// Precio de la siguiente instalación de `kind` para `player` (RF-07):
    /// el del catálogo con el aumento por las que ya compró del mismo tipo.
    pub fn next_price(&self, player: &Player, kind: InstallationKind) -> u64 {
        self.catalog
            .installation_price(kind, player.purchases_of(kind))
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
    /// Copia de las hojas ganadas de cada jugador, para armar el ranking
    /// fuera del bloqueo del mundo.
    pub fn scores(&self) -> Vec<super::ranking::Score> {
        self.players()
            .map(|p| super::ranking::Score {
                player_id: p.player_id.clone(),
                leaves_earned: p.leaves_earned,
            })
            .collect()
    }

    pub fn players(&self) -> impl Iterator<Item = &Player> {
        self.players.values().map(|slot| &slot.record)
    }
}

/// Índices (por filas) de las casillas del cuadrado de `side` casillas de
/// lado centrado en (x, y), recortado por los bordes del valle. Con un lado
/// par el centro queda una casilla más cerca del extremo de arriba y de la
/// izquierda: `(side - 1) / 2` casillas antes y `side / 2` después (lado 3 →
/// 1 y 1; lado 4 → 1 y 2).
fn area_indices(x: u32, y: u32, side: u32, width: u32, height: u32) -> Vec<usize> {
    let before = side.saturating_sub(1) / 2;
    let after = side / 2;
    let columns = x.saturating_sub(before)..=x.saturating_add(after).min(width - 1);
    let rows = y.saturating_sub(before)..=y.saturating_add(after).min(height - 1);
    rows.flat_map(|row| {
        columns
            .clone()
            .map(move |column| row as usize * width as usize + column as usize)
    })
    .collect()
}

/// Índices de las casillas vecinas ortogonales (norte, sur, oeste, este)
/// dentro de un valle de `width` × `height`.
fn orthogonal_neighbor_indices(
    x: u32,
    y: u32,
    width: u32,
    height: u32,
) -> impl Iterator<Item = usize> {
    let (x, y, width) = (x as usize, y as usize, width as usize);
    let mut neighbors = [0usize; 4];
    let mut count = 0;
    if y > 0 {
        neighbors[count] = (y - 1) * width + x;
        count += 1;
    }
    if y + 1 < height as usize {
        neighbors[count] = (y + 1) * width + x;
        count += 1;
    }
    if x > 0 {
        neighbors[count] = y * width + (x - 1);
        count += 1;
    }
    if x + 1 < width {
        neighbors[count] = y * width + (x + 1);
        count += 1;
    }
    neighbors.into_iter().take(count)
}

/// Genera de forma determinista franjas de río/lago y dispersión de rocas (Fase 3).
pub fn generate_natural_terrain(tiles: &mut [Tile], width: u32, height: u32, seed: u64) {
    if width < 20 || height < 20 {
        return;
    }
    let mut trng = SplitMix64::new(seed ^ 0x5445_5252_4149_4E5F);

    // 1. Franja de río / lago (alrededor de x=24 en 32x32 para no colisionar con zonas de prueba x<=15)
    let base_x = 24i32 + ((trng.next_u64() % 3) as i32 - 1);
    for y in 0..height {
        let offset = ((y as i32 / 4) % 3) - 1;
        let rx = (base_x + offset).clamp(18, (width - 4) as i32) as u32;
        let idx1 = (y * width + rx) as usize;
        let idx2 = (y * width + rx + 1) as usize;
        if idx1 < tiles.len() {
            tiles[idx1].stage = TileStage::Water;
        }
        if idx2 < tiles.len() {
            tiles[idx2].stage = TileStage::Water;
        }
    }

    // Lago a media altura (y: 12..=16) ensanchando el río
    for ly in 12..=16u32 {
        if ly < height {
            let offset = ((ly as i32 / 4) % 3) - 1;
            let rx = (base_x + offset).clamp(18, (width - 4) as i32) as u32;
            for lx in rx.saturating_sub(1)..=(rx + 2).min(width - 2) {
                let idx = (ly * width + lx) as usize;
                if idx < tiles.len() {
                    tiles[idx].stage = TileStage::Water;
                }
            }
        }
    }

    // Lago / laguna sur independiente (x: 17..=19, y: 26..=28)
    for ly in 26..=28u32 {
        if ly < height {
            for lx in 17..=19u32 {
                if lx < width {
                    let idx = (ly * width + lx) as usize;
                    if idx < tiles.len() {
                        tiles[idx].stage = TileStage::Water;
                    }
                }
            }
        }
    }

    // 2. Dispersión de rocas (obstáculos naturales no transitables)
    let num_rocks = 12;
    let mut placed = 0;
    let mut attempts = 0;
    while placed < num_rocks && attempts < 100 {
        attempts += 1;
        let rx = 17 + (trng.next_u64() % u64::from(width.saturating_sub(20).max(1))) as u32;
        let ry = (trng.next_u64() % u64::from(height)) as u32;
        if rx == ry || (rx == 20 && ry == 20) || (rx == 31 && ry == 31) {
            continue;
        }
        let idx = (ry * width + rx) as usize;
        if idx < tiles.len() && tiles[idx].stage == TileStage::DrySoil {
            tiles[idx].stage = TileStage::Rock;
            placed += 1;
        }
    }
}

fn tile_count(width: u32, height: u32) -> usize {
    width as usize * height as usize
}

fn is_uuid(text: &str) -> bool {
    Uuid::try_parse(text).is_ok()
}

#[cfg(test)]
#[path = "world_fire_tests.rs"]
mod fire_tests;

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
    fn with_size_builds_a_valley_of_the_requested_size() {
        let mut w = World::with_size(Catalog::d01(), 0, 64, 48);
        assert_eq!((w.width(), w.height()), (64, 48));
        assert_eq!(w.tiles().len(), 64 * 48);
        assert!(w.tile(63, 47).is_some());
        assert!(w.tile(64, 0).is_none());
        assert!(w.tile(0, 48).is_none());
        w.step(&[drop_cmd(&action(1), ANA, 63, 47)]);
        assert_eq!(w.tiles()[47 * 64 + 63].humidity, 25);
        assert_eq!((w.parts().width, w.parts().height), (64, 48));
    }

    #[test]
    fn with_size_at_the_game_size_is_the_same_valley_as_with_seed() {
        let a = World::with_seed(Catalog::d01(), 9);
        let b = World::with_size(Catalog::d01(), 9, 32, 32);
        assert_eq!(a.parts(), b.parts());
    }

    #[test]
    fn a_snapshot_of_a_custom_size_valley_is_not_restorable_in_the_game() {
        let w = World::with_size(Catalog::d01(), 0, 64, 64);
        let err = World::restore(Catalog::d01(), w.parts()).unwrap_err();
        assert!(matches!(err, RestoreError::Dimensions { .. }));
    }

    #[test]
    fn area_indices_are_clipped_by_the_given_valley_size() {
        assert_eq!(
            area_indices(63, 47, 3, 64, 48),
            vec![46 * 64 + 62, 46 * 64 + 63, 47 * 64 + 62, 47 * 64 + 63]
        );
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
    fn a_burning_tile_freezes_its_stage_but_keeps_evaporating() {
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
        advance_to(&mut w, 20);
        let burning = w.tile(0, 0).unwrap();
        assert_eq!((burning.humidity, burning.stage), (6, TileStage::Grass));
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

    // ---- Instalaciones y mejoras (T3) -----------------------------------

    use crate::domain::InstallationKind::{Drip, Sprinkler, TreatmentPlant};
    use crate::domain::{InstallationKind, UpgradeKind};

    /// Mundo con jugadores que ya tienen `leaves` hojas (ganadas), para
    /// probar las compras sin tener que hacer crecer medio valle.
    fn rich_world(catalog: Catalog, players: &[(&str, u64)]) -> World {
        let tank = catalog.tank.capacity;
        let mut parts = World::new(catalog.clone()).parts();
        for (id, leaves) in players {
            parts.players.push(Player {
                leaves: *leaves,
                leaves_earned: *leaves,
                ..Player::starting(id, tank)
            });
        }
        parts.players.sort_by(|a, b| a.player_id.cmp(&b.player_id));
        World::restore(catalog, parts).unwrap()
    }

    fn rich(players: &[(&str, u64)]) -> World {
        rich_world(Catalog::d01(), players)
    }

    fn place_cmd(action_id: &str, player: &str, kind: InstallationKind, x: u32, y: u32) -> Command {
        Command {
            action_id: action_id.to_string(),
            player_id: player.to_string(),
            kind: Some(CommandKind::PlaceInstallation { kind, x, y }),
        }
    }

    fn buy_cmd(action_id: &str, player: &str, kind: UpgradeKind) -> Command {
        Command {
            action_id: action_id.to_string(),
            player_id: player.to_string(),
            kind: Some(CommandKind::BuyUpgrade { kind }),
        }
    }

    fn installation_at(w: &World, x: u32, y: u32) -> Option<(InstallationKind, String)> {
        w.tile(x, y)
            .unwrap()
            .installation
            .as_ref()
            .map(|i| (i.kind, i.owner_id.clone()))
    }

    fn humidity(w: &World, x: u32, y: u32) -> u64 {
        w.tile(x, y).unwrap().humidity
    }

    fn set_humidity_with(w: &mut World, catalog: &Catalog, x: u32, y: u32, value: u64) {
        let mut parts = w.parts();
        parts.tiles[(y * 32 + x) as usize].humidity = value;
        *w = World::restore(catalog.clone(), parts).unwrap();
    }

    fn set_humidity(w: &mut World, x: u32, y: u32, value: u64) {
        set_humidity_with(w, &Catalog::d01(), x, y, value);
    }

    #[test]
    fn the_price_grows_15_percent_per_purchase_rounding_up() {
        let catalog = Catalog::d01();
        let prices = |kind, n: u32| {
            (0..n)
                .map(|k| catalog.installation_price(kind, k))
                .collect::<Vec<_>>()
        };
        // Cada precio es el anterior más un 15 %, redondeado hacia arriba.
        assert_eq!(prices(Sprinkler, 6), vec![10, 12, 14, 17, 20, 23]);
        assert_eq!(prices(Drip, 4), vec![25, 29, 34, 40]);
        assert_eq!(prices(TreatmentPlant, 3), vec![40, 46, 53]);
    }

    #[test]
    fn the_price_uses_the_percentage_of_the_catalog_and_never_overflows() {
        let mut catalog = Catalog::d01();
        catalog.installation_price_increase_percent = 0;
        assert_eq!(catalog.installation_price(Sprinkler, u32::MAX), 10);
        catalog.installation_price_increase_percent = 100;
        assert_eq!(catalog.installation_price(Sprinkler, 3), 80);
        catalog.installation_price_increase_percent = 15;
        assert_eq!(catalog.installation_price(Sprinkler, u32::MAX), u64::MAX);
    }

    #[test]
    fn placing_an_installation_spends_the_leaves_and_records_the_owner() {
        let mut w = rich(&[(ANA, 50)]);
        let out = w.step(&[place_cmd(&action(1), ANA, Sprinkler, 5, 6)]);

        assert_eq!(outcomes(&out), vec![CommandOutcome::Applied]);
        assert_eq!(
            installation_at(&w, 5, 6),
            Some((Sprinkler, ANA.to_string()))
        );
        let ana = w.player(ANA).unwrap();
        assert_eq!(
            (ana.leaves, ana.leaves_spent, ana.leaves_earned),
            (40, 10, 50)
        );
        assert_eq!(ana.purchases, vec![(Sprinkler, 1)]);
        assert_eq!(ana.last_action_tick, 1);
        assert_rf27(&w);
        let again = w.step(&[place_cmd(&action(1), ANA, Sprinkler, 5, 6)]);
        assert_eq!(outcomes(&again), vec![CommandOutcome::Duplicate]);
    }

    #[test]
    fn each_purchase_of_a_kind_makes_the_next_one_dearer_but_not_the_other_kinds() {
        let mut w = rich(&[(ANA, 200)]);
        w.step(&[
            place_cmd(&action(1), ANA, Sprinkler, 0, 0),
            place_cmd(&action(2), ANA, Sprinkler, 1, 0),
            place_cmd(&action(3), ANA, Drip, 2, 0),
        ]);
        let ana = w.player(ANA).unwrap();
        assert_eq!(ana.leaves_spent, 10 + 12 + 25);
        assert_eq!(w.next_price(ana, Sprinkler), 14);
        assert_eq!(w.next_price(ana, Drip), 29);
        assert_eq!(w.next_price(ana, TreatmentPlant), 40);
        assert_rf27(&w);
    }

    #[test]
    fn placing_out_of_bounds_is_rejected_without_charging() {
        let mut w = rich(&[(ANA, 50)]);
        let out = w.step(&[
            place_cmd(&action(1), ANA, Sprinkler, 32, 0),
            place_cmd(&action(2), ANA, Sprinkler, 0, 32),
        ]);
        assert_eq!(
            outcomes(&out),
            vec![CommandOutcome::Rejected(RejectReason::OutOfBounds); 2]
        );
        let ana = w.player(ANA).unwrap();
        assert_eq!((ana.leaves, ana.leaves_spent), (50, 0));
        assert!(ana.purchases.is_empty());
    }

    #[test]
    fn one_installation_per_tile_even_from_another_player() {
        let mut w = rich(&[(ANA, 50), (BETO, 50)]);
        let out = w.step(&[
            place_cmd(&action(1), ANA, Drip, 3, 3),
            place_cmd(&action(2), BETO, Sprinkler, 3, 3),
            place_cmd(&action(3), ANA, Sprinkler, 3, 3),
        ]);
        assert_eq!(
            outcomes(&out),
            vec![
                CommandOutcome::Applied,
                CommandOutcome::Rejected(RejectReason::TileOccupied),
                CommandOutcome::Rejected(RejectReason::TileOccupied),
            ]
        );
        assert_eq!(installation_at(&w, 3, 3), Some((Drip, ANA.to_string())));
        assert_eq!(w.player(BETO).unwrap().leaves, 50);
        assert_eq!(w.player(ANA).unwrap().purchases, vec![(Drip, 1)]);
    }

    #[test]
    fn placing_without_enough_leaves_is_rejected_without_charging() {
        let mut w = rich(&[(ANA, 9)]);
        let out = w.step(&[place_cmd(&action(1), ANA, Sprinkler, 1, 1)]);
        assert_eq!(
            outcomes(&out),
            vec![CommandOutcome::Rejected(RejectReason::InsufficientLeaves)]
        );
        assert_eq!(w.player(ANA).unwrap().leaves, 9);
        assert_eq!(installation_at(&w, 1, 1), None);
        // Con exactamente el precio alcanza.
        let mut w = rich(&[(ANA, 10)]);
        let out = w.step(&[place_cmd(&action(1), ANA, Sprinkler, 1, 1)]);
        assert_eq!(outcomes(&out), vec![CommandOutcome::Applied]);
        assert_eq!(w.player(ANA).unwrap().leaves, 0);
    }

    #[test]
    fn the_price_of_the_next_one_decides_the_rejection() {
        let mut w = rich(&[(ANA, 21)]);
        let out = w.step(&[
            place_cmd(&action(1), ANA, Sprinkler, 0, 0), // 10
            place_cmd(&action(2), ANA, Sprinkler, 1, 0), // 12 > 11
        ]);
        assert_eq!(
            outcomes(&out),
            vec![
                CommandOutcome::Applied,
                CommandOutcome::Rejected(RejectReason::InsufficientLeaves)
            ]
        );
        assert_eq!(w.player(ANA).unwrap().leaves, 11);
    }

    #[test]
    fn a_new_player_cannot_buy_and_a_rejection_does_not_bring_them_in() {
        let mut w = world();
        let out = w.step(&[
            place_cmd(&action(1), ANA, Sprinkler, 1, 1),
            buy_cmd(&action(2), ANA, UpgradeKind::BiggerTank),
        ]);
        assert_eq!(
            outcomes(&out),
            vec![CommandOutcome::Rejected(RejectReason::InsufficientLeaves); 2]
        );
        assert!(w.player(ANA).is_none());
    }

    #[test]
    fn structural_rejections_come_before_the_lack_of_leaves() {
        let mut w = rich(&[(ANA, 45), (BETO, 0)]);
        w.step(&[place_cmd(&action(1), ANA, Drip, 4, 4)]); // 25
        let out = w.step(&[
            place_cmd(&action(2), BETO, Sprinkler, 99, 0),
            place_cmd(&action(3), BETO, Sprinkler, 4, 4),
            buy_cmd(&action(4), ANA, UpgradeKind::BiggerTank), // 20
            buy_cmd(&action(5), ANA, UpgradeKind::BiggerTank), // ya la tiene
        ]);
        assert_eq!(
            outcomes(&out),
            vec![
                CommandOutcome::Rejected(RejectReason::OutOfBounds),
                CommandOutcome::Rejected(RejectReason::TileOccupied),
                CommandOutcome::Applied,
                CommandOutcome::Rejected(RejectReason::UpgradeAlreadyOwned),
            ]
        );
    }

    #[test]
    fn installations_cost_leaves_in_demo_mode_too() {
        let mut w = rich(&[(ANA, 10)]);
        w.set_demo_mode(true);
        let out = w.step(&[place_cmd(&action(1), ANA, Sprinkler, 1, 1)]);
        assert_eq!(outcomes(&out), vec![CommandOutcome::Applied]);
        assert_eq!(w.player(ANA).unwrap().leaves, 0);
    }

    #[test]
    fn a_newcomer_cannot_enter_a_full_world_even_with_a_free_price() {
        // Un precio 0 del catálogo dejaría entrar a un jugador nuevo con una
        // compra; con el mundo lleno se rechaza como en las gotas.
        let mut catalog = Catalog::d01();
        catalog.sprinkler.price = 0;
        let mut parts = World::new(catalog.clone()).parts();
        parts.players = (0..MAX_PLAYERS)
            .map(|n| Player::starting(&format!("00000000-0000-4000-9000-{n:012}"), 20))
            .collect();
        let mut w = World::restore(catalog, parts).unwrap();
        let out = w.step(&[place_cmd(&action(1), ANA, Sprinkler, 1, 1)]);
        assert_eq!(
            outcomes(&out),
            vec![CommandOutcome::Rejected(RejectReason::WorldFull)]
        );
        assert!(w.player(ANA).is_none());
    }

    // -- Aspersor (Reutilizar)

    #[test]
    fn a_sprinkler_waters_its_3x3_area_every_20_ticks_without_touching_the_rest() {
        let mut w = rich(&[(ANA, 10)]);
        w.step(&[place_cmd(&action(1), ANA, Sprinkler, 10, 10)]); // tick 1
        advance_to(&mut w, 19);
        assert_eq!(humidity(&w, 10, 10), 0, "not yet");
        advance_to(&mut w, 20);
        // +25 y, en el mismo tick, la evaporación de 2 (tick múltiplo de 10).
        for y in 9..=11 {
            for x in 9..=11 {
                assert_eq!(humidity(&w, x, y), 23, "({x},{y})");
            }
        }
        assert_eq!(humidity(&w, 12, 10), 0);
        assert_eq!(humidity(&w, 10, 12), 0);
        // Tick 30: -2. Tick 40: +25 y -2.
        advance_to(&mut w, 40);
        assert_eq!(humidity(&w, 10, 10), 23 - 2 + 25 - 2);
    }

    #[test]
    fn a_sprinkler_is_clipped_at_the_edges_of_the_valley() {
        let mut w = rich(&[(ANA, 10)]);
        w.step(&[place_cmd(&action(1), ANA, Sprinkler, 0, 0)]);
        advance_to(&mut w, 20);
        let watered = w.tiles().iter().filter(|t| t.humidity > 0).count();
        assert_eq!(watered, 4);
        assert_eq!(w.player(ANA).unwrap().water_reused, 4 * 25);
    }

    #[test]
    fn a_sprinkler_does_not_use_the_tank_nor_the_humidity_contributed_by_drops() {
        let mut w = rich(&[(ANA, 10)]);
        w.step(&[place_cmd(&action(1), ANA, Sprinkler, 10, 10)]);
        advance_to(&mut w, 100);
        let ana = w.player(ANA).unwrap();
        assert_eq!((ana.water, ana.water_spent), (20, 0));
        assert_eq!(w.humidity_contributed(), 0);
        assert_rf27(&w);
    }

    #[test]
    fn the_leaves_of_a_tile_grown_by_a_sprinkler_go_to_its_owner() {
        let mut w = rich(&[(ANA, 10)]);
        w.step(&[place_cmd(&action(1), ANA, Sprinkler, 10, 10)]);
        // Beto deja 75 en una casilla del área; la evaporación la baja y el
        // aspersor de Ana (tick 40) la empuja al umbral.
        w.step(&drops(10, 3, BETO, 10, 11));
        advance_to(&mut w, 39);
        assert_eq!(w.tile(10, 11).unwrap().stage, TileStage::DrySoil);
        advance_to(&mut w, 40);
        assert_eq!(w.tile(10, 11).unwrap().stage, TileStage::Grass);
        let ana = w.player(ANA).unwrap();
        assert_eq!(
            (ana.leaves_earned, ana.leaves, ana.leaves_spent),
            (11, 1, 10)
        );
        assert_eq!(w.player(BETO).unwrap().leaves, 0);
        assert_rf27(&w);
    }

    #[test]
    fn overlapping_sprinklers_add_up_and_count_as_reused_water() {
        let mut w = rich(&[(ANA, 30), (BETO, 30)]);
        w.step(&[
            place_cmd(&action(1), ANA, Sprinkler, 10, 10),
            place_cmd(&action(2), BETO, Sprinkler, 11, 10),
        ]);
        advance_to(&mut w, 20);
        assert_eq!(humidity(&w, 10, 10), 48, "two sprinklers: 50 - 2");
        assert_eq!(humidity(&w, 9, 10), 23);
        assert_eq!(humidity(&w, 12, 10), 23);
        assert_eq!(w.player(ANA).unwrap().water_reused, 9 * 25);
        assert_eq!(w.player(BETO).unwrap().water_reused, 9 * 25);
    }

    #[test]
    fn a_sprinkler_uses_the_area_the_interval_and_the_humidity_of_the_catalog() {
        let mut catalog = Catalog::d01();
        catalog.sprinkler.area_side = 4; // 1 casilla antes y 2 después
        catalog.sprinkler.humidity = 10;
        catalog.sprinkler.interval_seconds = 1;
        let mut w = rich_world(catalog, &[(ANA, 10)]);
        w.step(&[place_cmd(&action(1), ANA, Sprinkler, 10, 10)]);
        advance_to(&mut w, 10);
        // Cuadrado de lado 4: x e y de 9 a 12.
        let mut watered = Vec::new();
        for y in 0..32 {
            for x in 0..32 {
                if humidity(&w, x, y) > 0 {
                    watered.push((x, y));
                }
            }
        }
        assert_eq!(watered.len(), 16);
        assert_eq!(watered.first(), Some(&(9, 9)));
        assert_eq!(watered.last(), Some(&(12, 12)));
        assert_eq!(
            humidity(&w, 9, 9),
            10 - 2,
            "10 and the evaporation of tick 10"
        );
    }

    #[test]
    fn sprinklers_keep_working_in_demo_mode_without_breaking_the_drop_invariant() {
        let mut w = rich(&[(ANA, 10)]);
        w.step(&[demo_cmd(&action(1), true)]);
        w.step(&[place_cmd(&action(2), ANA, Sprinkler, 10, 10)]);
        w.step(&drops(10, 3, ANA, 3, 3));
        advance_to(&mut w, 41);
        assert_eq!(humidity(&w, 10, 10), 50, "two waterings and no evaporation");
        assert_eq!(w.humidity_contributed(), 3, "only the drops count");
    }

    // -- Goteo (Reducir)

    #[test]
    fn drip_halves_the_evaporation_inside_its_5x5_area_only() {
        let mut w = rich(&[(ANA, 25)]);
        w.step(&[place_cmd(&action(1), ANA, Drip, 10, 10)]);
        for (x, y) in [(10, 10), (8, 8), (12, 12), (7, 10), (13, 10)] {
            set_humidity(&mut w, x, y, 10);
        }
        advance_to(&mut w, 10);
        assert_eq!(humidity(&w, 10, 10), 9);
        assert_eq!(humidity(&w, 8, 8), 9);
        assert_eq!(humidity(&w, 12, 12), 9);
        assert_eq!(humidity(&w, 7, 10), 8, "outside the area");
        assert_eq!(humidity(&w, 13, 10), 8, "outside the area");
    }

    #[test]
    fn drip_counts_the_humidity_it_saved_as_reduced_water() {
        let mut w = rich(&[(ANA, 25)]);
        w.step(&[place_cmd(&action(1), ANA, Drip, 10, 10)]);
        // Con 10 y con 2 se ahorra 1 (pierde 1 en vez de 2). Con 1 pierde 1
        // igual y con 0 no hay nada que ahorrar.
        set_humidity(&mut w, 10, 10, 10);
        set_humidity(&mut w, 11, 10, 2);
        set_humidity(&mut w, 12, 10, 1);
        advance_to(&mut w, 10);
        assert_eq!(w.player(ANA).unwrap().water_reduced, 2);
        // Quedan 9, 1 y 0: solo la primera ahorra algo.
        advance_to(&mut w, 20);
        assert_eq!(w.player(ANA).unwrap().water_reduced, 2 + 1);
    }

    #[test]
    fn drip_does_nothing_in_demo_mode() {
        let mut w = rich(&[(ANA, 25)]);
        w.set_demo_mode(true);
        w.step(&[place_cmd(&action(1), ANA, Drip, 10, 10)]);
        set_humidity(&mut w, 10, 10, 10);
        advance_to(&mut w, 100);
        assert_eq!(humidity(&w, 10, 10), 10);
        assert_eq!(w.player(ANA).unwrap().water_reduced, 0);
    }

    #[test]
    fn overlapping_drips_do_not_stack_and_the_first_one_gets_the_credit() {
        let mut w = rich(&[(ANA, 25), (BETO, 25)]);
        w.step(&[
            place_cmd(&action(1), BETO, Drip, 11, 10),
            place_cmd(&action(2), ANA, Drip, 10, 10),
        ]);
        set_humidity(&mut w, 10, 10, 10);
        advance_to(&mut w, 10);
        assert_eq!(humidity(&w, 10, 10), 9, "halved once, not quartered");
        // El primero en el orden de casillas (fila a fila) es el de Ana.
        assert_eq!(w.player(ANA).unwrap().water_reduced, 1);
        assert_eq!(w.player(BETO).unwrap().water_reduced, 0);
    }

    #[test]
    fn drip_divides_the_evaporation_by_the_catalog_divisor_rounding_down() {
        let mut catalog = Catalog::d01();
        catalog.evaporation.humidity_per_second = 3;
        catalog.drip.evaporation_divisor = 2;
        let mut w = rich_world(catalog.clone(), &[(ANA, 25)]);
        w.step(&[place_cmd(&action(1), ANA, Drip, 10, 10)]);
        set_humidity_with(&mut w, &catalog, 10, 10, 10);
        advance_to(&mut w, 10);
        assert_eq!(humidity(&w, 10, 10), 9, "3 / 2 = 1");
        assert_eq!(w.player(ANA).unwrap().water_reduced, 2);

        catalog.drip.evaporation_divisor = 4;
        let mut w = rich_world(catalog.clone(), &[(ANA, 25)]);
        w.step(&[place_cmd(&action(1), ANA, Drip, 10, 10)]);
        set_humidity_with(&mut w, &catalog, 10, 10, 10);
        advance_to(&mut w, 10);
        assert_eq!(humidity(&w, 10, 10), 10, "3 / 4 = 0");
    }

    #[test]
    fn drip_also_saves_on_a_burning_tile_because_it_still_evaporates() {
        let mut w = rich(&[(ANA, 25)]);
        w.step(&[place_cmd(&action(1), ANA, Drip, 10, 10)]);
        let mut parts = w.parts();
        parts.tiles[10 * 32 + 10].humidity = 10;
        parts.tiles[10 * 32 + 10].fire = Some(crate::domain::Fire {
            drops_received: 0,
            burning_ticks: 0,
        });
        w = World::restore(Catalog::d01(), parts).unwrap();
        advance_to(&mut w, 10);
        assert_eq!(humidity(&w, 10, 10), 9);
        assert_eq!(w.player(ANA).unwrap().water_reduced, 1);
    }

    // -- Planta de tratamiento (Reciclar)

    #[test]
    fn a_treatment_plant_adds_an_extra_litre_every_20_ticks() {
        let mut w = rich(&[(ANA, 40)]);
        w.step(&[place_cmd(&action(1), ANA, TreatmentPlant, 2, 2)]);
        w.step(&drops(10, 10, ANA, 20, 20)); // 10 L fuera: quedan 10
        advance_to(&mut w, 19);
        assert_eq!(w.player(ANA).unwrap().water, 10);
        advance_to(&mut w, 20);
        let ana = w.player(ANA).unwrap();
        assert_eq!(ana.water, 12, "the base refill and the plant, 1 L each");
        assert_eq!(ana.water_recycled, 1);
        assert_eq!(ana.water_refilled, 20 + 2);
        assert_rf27(&w);
    }

    #[test]
    fn a_plant_only_counts_the_litres_that_fit_in_the_tank() {
        let mut w = rich(&[(ANA, 40)]);
        w.step(&[place_cmd(&action(1), ANA, TreatmentPlant, 2, 2)]);
        advance_to(&mut w, 200);
        let ana = w.player(ANA).unwrap();
        assert_eq!((ana.water, ana.water_recycled), (20, 0), "full tank");

        w.step(&drops(10, 1, ANA, 20, 20)); // 19
        advance_to(&mut w, 220);
        let ana = w.player(ANA).unwrap();
        assert_eq!(ana.water, 20, "the base refill filled it first");
        assert_eq!(ana.water_recycled, 0);
        assert_rf27(&w);
    }

    #[test]
    fn several_plants_add_up_and_each_credits_its_owner() {
        let mut w = rich(&[(ANA, 100), (BETO, 100)]);
        w.step(&[
            place_cmd(&action(1), ANA, TreatmentPlant, 2, 2),
            place_cmd(&action(2), ANA, TreatmentPlant, 3, 2),
            place_cmd(&action(3), BETO, TreatmentPlant, 4, 2),
        ]);
        w.step(&drops(10, 12, ANA, 20, 20)); // 8 L
        w.step(&drops(30, 12, BETO, 20, 21)); // 8 L
        advance_to(&mut w, 20);
        let ana = w.player(ANA).unwrap();
        assert_eq!((ana.water, ana.water_recycled), (8 + 1 + 2, 2));
        let beto = w.player(BETO).unwrap();
        assert_eq!((beto.water, beto.water_recycled), (8 + 1 + 1, 1));
        assert_rf27(&w);
    }

    #[test]
    fn a_plant_uses_the_interval_and_the_litres_of_the_catalog() {
        let mut catalog = Catalog::d01();
        catalog.treatment_plant.interval_seconds = 1;
        catalog.treatment_plant.refill_amount = 3;
        let mut w = rich_world(catalog, &[(ANA, 40)]);
        w.step(&[place_cmd(&action(1), ANA, TreatmentPlant, 2, 2)]);
        w.step(&drops(10, 15, ANA, 20, 20)); // 5 L
        advance_to(&mut w, 10);
        let ana = w.player(ANA).unwrap();
        assert_eq!((ana.water, ana.water_recycled), (8, 3));
    }

    // -- Instalaciones de un jugador ausente (RF-25)

    #[test]
    fn installations_stop_when_their_owner_is_away_for_too_long_and_resume_with_an_action() {
        let mut catalog = Catalog::d01();
        catalog.offline_installation_seconds = 3; // 30 ticks
        let mut w = rich_world(catalog, &[(ANA, 100)]);
        w.step(&[
            place_cmd(&action(1), ANA, Sprinkler, 10, 10),
            place_cmd(&action(2), ANA, TreatmentPlant, 20, 20),
        ]); // tick 1
        w.step(&drops(10, 10, ANA, 5, 5)); // tick 2: última acción
        advance_to(&mut w, 20);
        assert!(humidity(&w, 10, 10) > 0, "still within the time limit");
        assert_eq!(w.player(ANA).unwrap().water_recycled, 1);
        assert_eq!(w.player(ANA).unwrap().water_reused, 9 * 25);
        let after_20 = humidity(&w, 10, 10);

        // Tick 40: 38 ticks desde la última acción (2) > 30. El aspersor y
        // la planta ya no actúan; la recarga base y la evaporación, sí.
        advance_to(&mut w, 40);
        assert_eq!(w.player(ANA).unwrap().water_recycled, 1);
        assert_eq!(w.player(ANA).unwrap().water_reused, 9 * 25);
        assert_eq!(humidity(&w, 10, 10), after_20 - 2 * 2, "only evaporation");

        // Una acción del dueño las reactiva a partir de ese momento.
        w.step(&drops(100, 1, ANA, 6, 6)); // tick 41
        advance_to(&mut w, 60);
        assert_eq!(w.player(ANA).unwrap().water_reused, 18 * 25);
    }

    // -- Mejoras

    #[test]
    fn the_bigger_tank_adds_capacity_for_the_catalog_price_once() {
        let mut w = rich(&[(ANA, 30)]);
        let out = w.step(&[buy_cmd(&action(1), ANA, UpgradeKind::BiggerTank)]);
        assert_eq!(outcomes(&out), vec![CommandOutcome::Applied]);
        let ana = w.player(ANA).unwrap();
        assert_eq!(ana.tank_capacity, 30);
        assert_eq!(ana.water, 20, "the new space is not filled for free");
        assert_eq!((ana.leaves, ana.leaves_spent), (10, 20));
        assert_eq!(ana.upgrades, vec![UpgradeKind::BiggerTank]);

        let out = w.step(&[buy_cmd(&action(2), ANA, UpgradeKind::BiggerTank)]);
        assert_eq!(
            outcomes(&out),
            vec![CommandOutcome::Rejected(RejectReason::UpgradeAlreadyOwned)]
        );
        assert_eq!(w.player(ANA).unwrap().leaves, 10);
        advance_to(&mut w, 40);
        assert_eq!(w.player(ANA).unwrap().water, 22, "now it refills beyond 20");
        assert_rf27(&w);
    }

    #[test]
    fn an_upgrade_without_enough_leaves_is_rejected_without_charging() {
        let mut w = rich(&[(ANA, 29)]);
        let out = w.step(&[buy_cmd(&action(1), ANA, UpgradeKind::BiggerDrop)]);
        assert_eq!(
            outcomes(&out),
            vec![CommandOutcome::Rejected(RejectReason::InsufficientLeaves)]
        );
        let ana = w.player(ANA).unwrap();
        assert_eq!((ana.leaves, ana.leaves_spent), (29, 0));
        assert!(ana.upgrades.is_empty());
    }

    #[test]
    fn the_bigger_drop_humidifies_3x3_for_one_litre_and_counts_every_tile() {
        let mut w = rich(&[(ANA, 30)]);
        w.step(&[buy_cmd(&action(1), ANA, UpgradeKind::BiggerDrop)]);
        let out = w.step(&drops(10, 1, ANA, 10, 10));
        assert_eq!(outcomes(&out), vec![CommandOutcome::Applied]);
        for y in 9..=11 {
            for x in 9..=11 {
                assert_eq!(humidity(&w, x, y), 25, "({x},{y})");
            }
        }
        assert_eq!(humidity(&w, 8, 10), 0);
        let ana = w.player(ANA).unwrap();
        assert_eq!((ana.water, ana.water_spent), (19, 1));
        assert_eq!(w.humidity_contributed(), 9 * 25);
    }

    #[test]
    fn the_bigger_drop_is_clipped_at_the_edges_and_needs_an_in_bounds_centre() {
        let mut w = rich(&[(ANA, 30)]);
        w.step(&[buy_cmd(&action(1), ANA, UpgradeKind::BiggerDrop)]);
        w.step(&drops(10, 1, ANA, 0, 0));
        assert_eq!(w.humidity_contributed(), 4 * 25);
        let out = w.step(&drops(20, 1, ANA, 32, 0));
        assert_eq!(
            outcomes(&out),
            vec![CommandOutcome::Rejected(RejectReason::OutOfBounds)]
        );
        assert_eq!(w.humidity_contributed(), 4 * 25);
        assert_eq!(w.player(ANA).unwrap().water, 19);
    }

    #[test]
    fn the_bigger_drop_pays_every_leaf_of_the_tiles_it_grows_to_the_dropper() {
        let mut w = rich(&[(ANA, 30), (BETO, 0)]);
        w.step(&[buy_cmd(&action(1), ANA, UpgradeKind::BiggerDrop)]);
        w.step(&drops(10, 3, BETO, 10, 10)); // 75 en el centro
        w.step(&drops(20, 3, BETO, 11, 10)); // 75 a la derecha
        let before = w.player(ANA).unwrap().leaves;
        w.step(&drops(30, 1, ANA, 10, 10));
        assert_eq!(w.tile(10, 10).unwrap().stage, TileStage::Grass);
        assert_eq!(w.tile(11, 10).unwrap().stage, TileStage::Grass);
        assert_eq!(w.player(ANA).unwrap().leaves, before + 2);
        assert_rf27(&w);
    }

    #[test]
    fn in_demo_mode_the_bigger_drop_still_humidifies_one_tile_so_the_invariant_holds() {
        let mut w = rich(&[(ANA, 30)]);
        w.step(&[buy_cmd(&action(1), ANA, UpgradeKind::BiggerDrop)]);
        w.step(&[demo_cmd(&action(2), true)]);
        let before = w.humidity_contributed();
        let out = w.step(&drops(10, 5, ANA, 10, 10));
        let accepted = out
            .results
            .iter()
            .filter(|r| r.outcome == CommandOutcome::Applied)
            .count() as u64;
        assert_eq!(accepted, 5);
        assert_eq!(w.humidity_contributed() - before, accepted);
        assert_eq!(humidity(&w, 10, 10), 5);
        assert_eq!(humidity(&w, 9, 10), 0);
        assert_eq!(w.player(ANA).unwrap().water_spent, 0);
    }

    #[test]
    fn the_bigger_drop_uses_the_area_of_the_catalog() {
        let mut catalog = Catalog::d01();
        catalog.bigger_drop.area_side = 5;
        let mut w = rich_world(catalog, &[(ANA, 30)]);
        w.step(&[buy_cmd(&action(1), ANA, UpgradeKind::BiggerDrop)]);
        w.step(&drops(10, 1, ANA, 10, 10));
        assert_eq!(w.humidity_contributed(), 25 * 25);
        assert_eq!(humidity(&w, 8, 8), 25);
    }

    #[test]
    fn reset_valley_removes_installations_purchases_and_upgrades() {
        let mut w = rich(&[(ANA, 100)]);
        w.step(&[
            place_cmd(&action(1), ANA, Sprinkler, 1, 1),
            buy_cmd(&action(2), ANA, UpgradeKind::BiggerTank),
            buy_cmd(&action(3), ANA, UpgradeKind::BiggerDrop),
        ]);
        advance_to(&mut w, 40);
        w.step(&[reset_cmd(&action(4))]);
        assert_eq!(installation_at(&w, 1, 1), None);
        let ana = w.player(ANA).unwrap();
        assert!(ana.purchases.is_empty() && ana.upgrades.is_empty());
        assert_eq!((ana.tank_capacity, ana.leaves_spent), (20, 0));
        assert_eq!(
            (ana.water_reused, ana.water_reduced, ana.water_recycled),
            (0, 0, 0)
        );
        assert_eq!(w.next_price(ana, Sprinkler), 10);
    }

    #[test]
    fn the_snapshot_carries_installations_purchases_upgrades_and_the_3r_counters() {
        let mut w = rich(&[(ANA, 200)]);
        w.step(&[
            place_cmd(&action(1), ANA, Sprinkler, 10, 10),
            place_cmd(&action(2), ANA, Drip, 12, 10),
            place_cmd(&action(3), ANA, TreatmentPlant, 2, 2),
            buy_cmd(&action(4), ANA, UpgradeKind::BiggerTank),
            buy_cmd(&action(5), ANA, UpgradeKind::BiggerDrop),
        ]);
        w.step(&drops(10, 8, ANA, 11, 10));
        advance_to(&mut w, 60);
        let parts = w.parts();
        let ana = parts.players.iter().find(|p| p.player_id == ANA).unwrap();
        assert!(ana.water_reused > 0 && ana.water_reduced > 0 && ana.water_recycled > 0);
        assert_eq!(ana.upgrades.len(), 2);
        let restored = World::restore(Catalog::d01(), parts.clone()).unwrap();
        assert_eq!(restored.parts(), parts);
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
        // Compras en el guion: unas se aplican y otras se rechazan según las
        // hojas y las casillas libres, pero igual en cada ejecución.
        match tick {
            3 => commands.push(place_cmd(&action(9101), ANA, Sprinkler, 1, 1)),
            5 => commands.push(place_cmd(&action(9102), BETO, Drip, 2, 1)),
            8 => commands.push(place_cmd(&action(9103), ANA, TreatmentPlant, 9, 9)),
            9 => commands.push(place_cmd(&action(9104), ANA, Sprinkler, 1, 1)),
            12 => commands.push(buy_cmd(&action(9105), BETO, UpgradeKind::BiggerDrop)),
            14 => commands.push(buy_cmd(&action(9106), ANA, UpgradeKind::BiggerTank)),
            210 => commands.push(place_cmd(&action(9107), ANA, Sprinkler, 3, 3)),
            _ => {}
        }
        commands
    }

    /// Valle del guion: Ana y Beto empiezan con hojas para poder comprar.
    fn script_world() -> World {
        rich(&[(ANA, 100), (BETO, 100)])
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
        let mut first = script_world();
        let mut second = script_world();
        run(&mut first, 1, 400);
        run(&mut second, 1, 400);
        assert_eq!(first.parts(), second.parts());
        assert_eq!(first.epoch(), 1);
        assert!(first.humidity_contributed() > 0);

        // Las compras del guion se ejercitaron de verdad antes del reinicio.
        let mut probe = script_world();
        run(&mut probe, 1, 199);
        let ana = probe.player(ANA).unwrap();
        assert!(ana.leaves_spent > 0 && !ana.purchases.is_empty());
        assert!(ana.water_reused > 0);
        let beto = probe.player(BETO).unwrap();
        assert_eq!(beto.upgrades, vec![UpgradeKind::BiggerDrop]);
    }

    #[test]
    fn restoring_a_snapshot_mid_run_does_not_change_the_outcome() {
        let mut straight = script_world();
        run(&mut straight, 1, 400);

        let mut interrupted = script_world();
        run(&mut interrupted, 1, 150);
        let mut resumed = World::restore(Catalog::d01(), interrupted.parts()).unwrap();
        run(&mut resumed, 151, 400);
        assert_eq!(resumed.parts(), straight.parts());
    }

    #[test]
    fn offline_summary_accumulates_while_player_is_away_and_clears_on_new_action() {
        let mut w = rich(&[(ANA, 100)]);
        // Ana coloca un aspersor en el tick 1.
        w.step(&[place_cmd(&action(1), ANA, Sprinkler, 5, 5)]);
        assert_eq!(w.player(ANA).unwrap().last_action_tick, 1);
        assert!(w.player(ANA).unwrap().offline_summary.is_empty());

        // Colocar pasto y un fuego dentro del área del aspersor para probar crecimiento y extinción.
        let target_idx = w.tile_index(5, 6).unwrap();
        w.tiles[target_idx].stage = TileStage::Grass;
        w.tiles[target_idx].humidity = 10;
        let fire_idx = w.tile_index(6, 5).unwrap();
        w.tiles[fire_idx].fire = Some(crate::domain::Fire {
            drops_received: 0,
            burning_ticks: 10,
        });

        // El aspersor activa cada 2 s (20 ticks). Para que la casilla
        // alcance el umbral de crecimiento y genere hojas, hace falta más de
        // dos activaciones: con humedad inicial 10, el primer crecimiento real
        // llega al cuarto riego (20, 40, 60, 80 ticks).
        for _ in 2..=100 {
            w.step(&[]);
        }

        let ana = w.player(ANA).unwrap();
        assert_eq!(ana.last_action_tick, 1);
        assert!(ana.offline_summary.water_reused > 0);
        assert!(ana.offline_summary.leaves_earned > 0);
        assert_eq!(ana.offline_summary.fires_extinguished, 1);
        assert!(ana.offline_summary.tiles_grown >= 1);

        // Ana vuelve y suelta una gota: su offline_summary se limpia.
        w.step(&[drop_cmd(&action(2), ANA, 0, 0)]);
        let ana_after = w.player(ANA).unwrap();
        assert_eq!(ana_after.last_action_tick, 101);
        assert!(ana_after.offline_summary.is_empty());
    }

    /// Aspersor de Ana en (5, 5) con pasto en (5, 6) a punto de crecer (90 de
    /// 100 de humedad: el primer riego de 25 lo sube de etapa) y un
    /// fuego en (6, 5); devuelve el mundo justo después de colocarlo.
    fn world_with_a_sprinkler_and_a_fire() -> World {
        let mut w = rich(&[(ANA, 100)]);
        w.step(&[place_cmd(&action(1), ANA, Sprinkler, 5, 5)]);
        let grass = w.tile_index(5, 6).unwrap();
        w.tiles[grass].stage = TileStage::Grass;
        w.tiles[grass].humidity = 90;
        let burning = w.tile_index(6, 5).unwrap();
        w.tiles[burning].fire = Some(crate::domain::Fire {
            drops_received: 0,
            burning_ticks: 10,
        });
        w
    }

    #[test]
    fn the_summary_counts_what_the_sprinklers_did_after_the_last_action() {
        let mut w = world_with_a_sprinkler_and_a_fire();
        assert!(w.player(ANA).unwrap().offline_summary.is_empty());
        advance_to(&mut w, 20);
        let ana = w.player(ANA).unwrap();
        // Un riego: 25 de humedad en las 9 casillas del cuadrado.
        assert_eq!(ana.offline_summary.water_reused, 9 * 25);
        assert_eq!(ana.offline_summary.fires_extinguished, 1);
        // Solo crece el pasto en (5, 6); la casilla del fuego no llega al umbral.
        assert_eq!(ana.offline_summary.tiles_grown, 1);
        assert_eq!(ana.tiles_grown, 1);
        // `rich` da 100 hojas de entrada, que no cuentan como ganadas por riego.
        assert!(ana.offline_summary.leaves_earned > 0);
        assert_eq!(ana.offline_summary.leaves_earned, ana.leaves_earned - 100);
    }

    #[test]
    fn the_summary_keeps_adding_up_and_the_next_applied_action_clears_it() {
        let mut w = world_with_a_sprinkler_and_a_fire();
        advance_to(&mut w, 20);
        let first = w.player(ANA).unwrap().offline_summary.clone();
        advance_to(&mut w, 40);
        let second = w.player(ANA).unwrap().offline_summary.clone();
        assert_eq!(second.water_reused, first.water_reused + 9 * 25);

        w.step(&[drop_cmd(&action(2), ANA, 0, 0)]);
        let ana = w.player(ANA).unwrap();
        assert!(ana.offline_summary.is_empty());
        // El acumulado de la época no se borra con la acción.
        assert_eq!(ana.fires_extinguished, 1);
        assert!(ana.tiles_grown >= 1);
    }

    #[test]
    fn a_rejected_action_does_not_clear_the_summary() {
        let mut w = world_with_a_sprinkler_and_a_fire();
        advance_to(&mut w, 20);
        // Fuera del valle: se rechaza y no cuenta como acción.
        w.step(&[drop_cmd(&action(2), ANA, 99, 99)]);
        assert!(!w.player(ANA).unwrap().offline_summary.is_empty());
    }

    #[test]
    fn a_sprinkler_effect_in_the_tick_of_the_action_counts_after_it() {
        let mut w = world_with_a_sprinkler_and_a_fire();
        advance_to(&mut w, 19);
        // En el tick 20 Ana actúa y el aspersor riega después de su comando.
        w.step(&[drop_cmd(&action(2), ANA, 0, 0)]);
        let ana = w.player(ANA).unwrap();
        assert_eq!(ana.last_action_tick, 20);
        assert_eq!(ana.offline_summary.water_reused, 9 * 25);
    }

    #[test]
    fn drip_and_treatment_plants_do_not_feed_the_summary() {
        let mut w = rich(&[(ANA, 100)]);
        w.step(&[
            place_cmd(&action(1), ANA, Drip, 10, 10),
            place_cmd(&action(2), ANA, TreatmentPlant, 1, 1),
        ]);
        // Con el depósito lleno la planta no recicla nada: se gasta antes.
        for n in 3..8 {
            w.step(&[drop_cmd(&action(n), ANA, 20, 20)]);
        }
        set_humidity(&mut w, 10, 10, 10);
        advance_to(&mut w, 100);
        let ana = w.player(ANA).unwrap();
        assert!(ana.water_reduced > 0 && ana.water_recycled > 0);
        assert!(ana.offline_summary.is_empty());
    }

    #[test]
    fn the_summary_stops_growing_when_the_sprinklers_stop() {
        let mut w = world_with_a_sprinkler_and_a_fire();
        // Pasadas las 2 h sin acciones el aspersor se para (RF-25).
        advance_to(&mut w, 72_001 + 20);
        let frozen = w.player(ANA).unwrap().offline_summary.clone();
        assert!(!frozen.is_empty());
        advance_to(&mut w, 72_001 + 100);
        assert_eq!(w.player(ANA).unwrap().offline_summary, frozen);
    }

    #[test]
    fn reset_valley_clears_the_summary_and_the_sprinkler_counters() {
        let mut w = world_with_a_sprinkler_and_a_fire();
        advance_to(&mut w, 20);
        w.step(&[reset_cmd(&action(2))]);
        let ana = w.player(ANA).unwrap();
        assert!(ana.offline_summary.is_empty());
        assert_eq!((ana.fires_extinguished, ana.tiles_grown), (0, 0));
    }

    // ---- Pruebas de Fase 1 (Riego por Goteo y Planta de Tratamiento) ----

    #[test]
    fn drip_shield_pauses_dry_ticks_and_prevents_withering() {
        let mut w = rich(&[(ANA, 100)]);
        // Colocar goteo en (5, 5)
        w.step(&[place_cmd(&action(1), ANA, Drip, 5, 5)]);
        // Hacer crecer una casilla en su área 5x5: (6, 6) a Pasto
        w.step(&[drop_cmd(&action(2), ANA, 6, 6)]);
        w.step(&[drop_cmd(&action(3), ANA, 6, 6)]);
        assert_eq!(w.tile(6, 6).unwrap().stage, TileStage::Grass);
        // Dejarla con humedad 0
        w.tiles[index(6, 6)].humidity = 0;
        // Avanzar más ticks que wither_after_ticks (100)
        advance_to(&mut w, 200);
        // Debe conservar la etapa Pasto y dry_ticks debe haber quedado pausado en 0
        assert_eq!(w.tile(6, 6).unwrap().stage, TileStage::Grass);
        assert_eq!(w.tile(6, 6).unwrap().dry_ticks, 0);
    }

    #[test]
    fn drip_shield_suppresses_fire_ignition() {
        let mut catalog = Catalog::d01();
        catalog.fire.ignition_chance_ppm = 1_000_000; // Cierto al 100%
        let mut w = rich_world(catalog, &[(ANA, 100)]);
        w.step(&[place_cmd(&action(1), ANA, Drip, 5, 5)]);
        // Casilla con planta y humedad 0 dentro del área 5x5: (6, 6)
        w.tiles[index(6, 6)].stage = TileStage::Grass;
        w.tiles[index(6, 6)].humidity = 0;
        // Casilla con planta y humedad 0 fuera del área: (20, 20)
        w.tiles[index(20, 20)].stage = TileStage::Grass;
        w.tiles[index(20, 20)].humidity = 0;

        w.step(&[]);
        // La protegida no debe arder
        assert!(w.tile(6, 6).unwrap().fire.is_none());
        // La desprotegida sí debe arder
        assert!(w.tile(20, 20).unwrap().fire.is_some());
    }

    #[test]
    fn drip_shield_doubles_drop_humidity() {
        let mut w = rich(&[(ANA, 100)]);
        w.step(&[place_cmd(&action(1), ANA, Drip, 10, 10)]);
        // Gota fuera del área de goteo: (0, 0)
        w.step(&[drop_cmd(&action(2), ANA, 0, 0)]);
        assert_eq!(w.tile(0, 0).unwrap().humidity, 25);
        // Gota dentro del área de goteo: (11, 11)
        w.step(&[drop_cmd(&action(3), ANA, 11, 11)]);
        assert_eq!(w.tile(11, 11).unwrap().humidity, 50, "aporte duplicado bajo goteo");
    }

    #[test]
    fn treatment_plant_pulses_growth_to_neighbors_and_credits_owner() {
        let mut w = rich(&[(ANA, 100)]);
        w.step(&[place_cmd(&action(1), ANA, TreatmentPlant, 5, 5)]);
        let leaves_before = w.player(ANA).unwrap().leaves;
        // Avanzar al tick 20 (intervalo 2 s = 20 ticks)
        advance_to(&mut w, 20);
        // Las casillas vecinas 3x3 deben haber avanzado de Tierra Seca a Pasto
        assert_eq!(w.tile(4, 4).unwrap().stage, TileStage::Grass);
        assert_eq!(w.tile(5, 4).unwrap().stage, TileStage::Grass);
        assert_eq!(w.tile(6, 6).unwrap().stage, TileStage::Grass);
        // La casilla central (la planta en 5, 5) no avanza
        assert_eq!(w.tile(5, 5).unwrap().stage, TileStage::DrySoil);
        // Ana debe haber ganado hojas por las casillas que crecieron
        let ana = w.player(ANA).unwrap();
        assert!(ana.leaves > leaves_before);
        assert!(ana.tiles_grown > 0);
    }

    // ---- Pruebas de Fase 2 (Durabilidad de Instalaciones) ----

    #[test]
    fn installation_placed_initializes_with_catalog_durability() {
        let mut w = rich(&[(ANA, 100)]);
        w.step(&[place_cmd(&action(1), ANA, Sprinkler, 5, 5)]);
        let inst = w.tile(5, 5).unwrap().installation.unwrap();
        assert_eq!(inst.remaining_durability, 30);
    }

    #[test]
    fn installation_durability_decrements_per_cycle_and_uninstalls_at_zero() {
        let mut w = rich(&[(ANA, 100)]);
        w.step(&[place_cmd(&action(1), ANA, Sprinkler, 5, 5)]);
        // Forzar durabilidad restante a 2 para probar la transición
        w.tiles[index(5, 5)].installation.as_mut().unwrap().remaining_durability = 2;

        // Avanzar 1 ciclo (20 ticks)
        advance_to(&mut w, 20);
        let inst = w.tile(5, 5).unwrap().installation.as_ref().unwrap();
        assert_eq!(inst.remaining_durability, 1);

        // Avanzar 2do ciclo (40 ticks): la durabilidad llega a cero y se desinstala
        advance_to(&mut w, 40);
        assert!(w.tile(5, 5).unwrap().installation.is_none(), "la instalación debe desinstalarse al llegar a 0");
    }

    #[test]
    fn drip_durability_decrements_and_uninstalls_at_zero() {
        let mut w = rich(&[(ANA, 100)]);
        w.step(&[place_cmd(&action(1), ANA, Drip, 8, 8)]);
        w.tiles[index(8, 8)].installation.as_mut().unwrap().remaining_durability = 1;

        // Avanzar 1 ciclo (20 ticks)
        advance_to(&mut w, 20);
        assert!(w.tile(8, 8).unwrap().installation.is_none(), "el goteo debe desinstalarse al agotarse");
    }

    // ---- Pruebas de Fase 3 (Terreno Natural: Agua y Rocas) ----

    #[test]
    fn natural_terrain_rejects_drop_with_tile_occupied() {
        let mut w = World::with_seed(Catalog::d01(), 42);
        let water_pos = (0..w.tiles().len())
            .find(|&i| w.tiles()[i].stage == TileStage::Water)
            .map(|i| (i as u32 % 32, i as u32 / 32))
            .unwrap();
        let rock_pos = (0..w.tiles().len())
            .find(|&i| w.tiles()[i].stage == TileStage::Rock)
            .map(|i| (i as u32 % 32, i as u32 / 32))
            .unwrap();

        let out = w.step(&[
            drop_cmd(&action(1), ANA, water_pos.0, water_pos.1),
            drop_cmd(&action(2), ANA, rock_pos.0, rock_pos.1),
        ]);

        assert_eq!(
            outcomes(&out),
            vec![
                CommandOutcome::Rejected(RejectReason::TileOccupied),
                CommandOutcome::Rejected(RejectReason::TileOccupied),
            ]
        );
    }

    #[test]
    fn natural_terrain_rejects_place_installation_with_tile_occupied() {
        let mut w = World::with_seed(Catalog::d01(), 42);
        w.player_slot(ANA).record.leaves = 100;
        let water_pos = (0..w.tiles().len())
            .find(|&i| w.tiles()[i].stage == TileStage::Water)
            .map(|i| (i as u32 % 32, i as u32 / 32))
            .unwrap();
        let rock_pos = (0..w.tiles().len())
            .find(|&i| w.tiles()[i].stage == TileStage::Rock)
            .map(|i| (i as u32 % 32, i as u32 / 32))
            .unwrap();

        let out = w.step(&[
            place_cmd(&action(1), ANA, Sprinkler, water_pos.0, water_pos.1),
            place_cmd(&action(2), ANA, Sprinkler, rock_pos.0, rock_pos.1),
        ]);

        assert_eq!(
            outcomes(&out),
            vec![
                CommandOutcome::Rejected(RejectReason::TileOccupied),
                CommandOutcome::Rejected(RejectReason::TileOccupied),
            ]
        );
    }

    #[test]
    fn shoreline_suppresses_fire_ignition_and_propagation() {
        let mut w = World::with_seed(Catalog::d01(), 42);
        let water_idx = (0..w.tiles().len())
            .find(|&i| w.tiles()[i].stage == TileStage::Water)
            .unwrap();
        let (wx, wy) = (water_idx as u32 % 32, water_idx as u32 / 32);
        let neighbor_idx = orthogonal_neighbor_indices(wx, wy, 32, 32)
            .find(|&n| w.tiles()[n].stage == TileStage::DrySoil)
            .unwrap();

        w.tiles[neighbor_idx].stage = TileStage::Grass;
        w.tiles[neighbor_idx].humidity = 0;

        let mut catalog = Catalog::d01();
        catalog.fire.ignition_chance_ppm = 1_000_000;
        catalog.fire.propagation_chance_ppm = 1_000_000;
        w.catalog = catalog;

        w.step(&[]);
        assert!(w.tiles[neighbor_idx].fire.is_none(), "la orilla de agua debe suprimir el fuego");
    }

    #[test]
    fn reset_valley_regenerates_natural_terrain_deterministically() {
        let mut w = World::with_seed(Catalog::d01(), 42);
        let water_count_before = w.tiles().iter().filter(|t| t.stage == TileStage::Water).count();
        let rock_count_before = w.tiles().iter().filter(|t| t.stage == TileStage::Rock).count();

        assert!(water_count_before > 0);
        assert!(rock_count_before > 0);

        w.step(&[reset_cmd(&action(1))]);

        let water_count_after = w.tiles().iter().filter(|t| t.stage == TileStage::Water).count();
        let rock_count_after = w.tiles().iter().filter(|t| t.stage == TileStage::Rock).count();

        assert_eq!(water_count_before, water_count_after);
        assert_eq!(rock_count_before, rock_count_after);
    }

    // ---- Pruebas de Fase 4 (Presión Urbana, Basura y Cesto de Reciclaje) ----

    #[test]
    fn clean_trash_reduces_level_and_pays_incentive_leaves() {
        let mut w = rich(&[(ANA, 10)]);
        w.tiles[index(2, 2)].trash_level = 2;
        w.tiles[index(2, 2)].stage = TileStage::DrySoil;

        // Primer clean_trash: nivel 2 -> 1 (sin hojas de incentivo aún)
        let out1 = w.step(&[Command {
            action_id: action(1),
            player_id: ANA.to_string(),
            kind: Some(CommandKind::CleanTrash { x: 2, y: 2 }),
        }]);
        assert_eq!(outcomes(&out1), vec![CommandOutcome::Applied]);
        assert_eq!(w.tile(2, 2).unwrap().trash_level, 1);
        assert_eq!(w.player(ANA).unwrap().leaves, 10);

        // Segundo clean_trash: nivel 1 -> 0 (+1 hoja de incentivo)
        let out2 = w.step(&[Command {
            action_id: action(2),
            player_id: ANA.to_string(),
            kind: Some(CommandKind::CleanTrash { x: 2, y: 2 }),
        }]);
        assert_eq!(outcomes(&out2), vec![CommandOutcome::Applied]);
        assert_eq!(w.tile(2, 2).unwrap().trash_level, 0);
        assert_eq!(w.player(ANA).unwrap().leaves, 11);
        assert_eq!(w.player(ANA).unwrap().leaves_earned, 11);

        // Tercer clean_trash sobre casilla sin basura: rechazado
        let out3 = w.step(&[Command {
            action_id: action(3),
            player_id: ANA.to_string(),
            kind: Some(CommandKind::CleanTrash { x: 2, y: 2 }),
        }]);
        assert_eq!(outcomes(&out3), vec![CommandOutcome::Rejected(RejectReason::InvalidCommand)]);
    }

    #[test]
    fn urban_building_spawns_and_is_immune_to_fire() {
        let mut w = rich(&[(ANA, 100)]);
        // Simular progreso alto
        w.humidity_contributed = 600;
        advance_to(&mut w, 1);

        let building_idx = w.tiles().iter().position(|t| t.has_building);
        assert!(building_idx.is_some(), "un edificio debe aparecer tras superar el umbral de progreso");

        let b_idx = building_idx.unwrap();
        let (bx, by) = (b_idx as u32 % 32, b_idx as u32 / 32);

        // Inmune a fuego
        assert!(!fire::is_eligible(&w.tiles[b_idx]));

        // Intento de colocar instalación sobre edificio rechazado
        let out = w.step(&[place_cmd(&action(1), ANA, Sprinkler, bx, by)]);
        assert_eq!(outcomes(&out), vec![CommandOutcome::Rejected(RejectReason::TileOccupied)]);
    }

    #[test]
    fn trash_bin_cleans_trash_and_consumes_durability() {
        let mut w = rich(&[(ANA, 100)]);
        // Colocar cesto de reciclaje en (4, 4)
        w.step(&[Command {
            action_id: action(1),
            player_id: ANA.to_string(),
            kind: Some(CommandKind::PlaceInstallation {
                kind: InstallationKind::TrashBin,
                x: 4,
                y: 4,
            }),
        }]);

        // Sembrar basura nivel 2 en (5, 5) dentro del radio 3x3 del cesto
        w.tiles[index(5, 5)].trash_level = 2;

        let initial_durability = w.tile(4, 4).unwrap().installation.as_ref().unwrap().remaining_durability;
        assert_eq!(initial_durability, 30);

        // Avanzar 1 ciclo de trabajo (20 ticks = 2 s)
        advance_to(&mut w, 20);

        // El cesto debe haber reducido 1 nivel de basura y 1 de durabilidad
        assert_eq!(w.tile(5, 5).unwrap().trash_level, 1);
        let updated_durability = w.tile(4, 4).unwrap().installation.as_ref().unwrap().remaining_durability;
        assert_eq!(updated_durability, 29);
    }
}
