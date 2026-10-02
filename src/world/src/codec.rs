//! Traducción entre los mensajes del contrato y el dominio.
//!
//! Es la frontera: el dominio no conoce ningún tipo generado desde
//! `proto/`, y aquí se decide cómo se representa cada cosa en el cable.

use prost::Message;

use crate::domain::catalog as dc;
use crate::domain::{
    self, AppliedAction, Catalog, Command, CommandKind, CommandOutcome, CommandResult, Fire,
    Installation, InstallationKind, Player, RejectReason, Tile, TileStage, UpgradeKind, World,
    WorldParts,
};
use crate::proto::persistence::v1 as persistence_pb;
use crate::proto::world::v1 as pb;

/// La instantánea recibida no se puede convertir en un mundo.
#[derive(Debug)]
pub enum DecodeError {
    /// `world_state` no es un `WorldSnapshot` válido.
    Malformed(prost::DecodeError),
    /// `Snapshot.tick` y `WorldSnapshot.tick` no coinciden.
    TickMismatch {
        row: u64,
        world: u64,
    },
    UnknownEnum {
        field: &'static str,
        value: i32,
    },
    Restore(domain::RestoreError),
    /// El catálogo de la persistencia no sirve para jugar.
    InvalidCatalog(String),
}

impl std::fmt::Display for DecodeError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Malformed(error) => write!(f, "world_state mal formado: {error}"),
            Self::TickMismatch { row, world } => {
                write!(f, "la fila es del tick {row} y el mundo del tick {world}")
            }
            Self::UnknownEnum { field, value } => {
                write!(f, "valor desconocido en {field}: {value}")
            }
            Self::Restore(error) => write!(f, "{error}"),
            Self::InvalidCatalog(reason) => write!(f, "catálogo no válido: {reason}"),
        }
    }
}

impl std::error::Error for DecodeError {}

/// Convierte la respuesta de `GetCatalog` en el catálogo del dominio.
///
/// Rechaza un catálogo al que le falte un grupo de cifras o cuyas cifras
/// harían el juego imposible (divisiones por cero, intervalos o áreas
/// vacíos, versión vacía): mejor no arrancar que simular mal.
pub fn catalog_from_proto(catalog: persistence_pb::Catalog) -> Result<Catalog, DecodeError> {
    fn section<T>(value: Option<T>, name: &str) -> Result<T, DecodeError> {
        value.ok_or_else(|| DecodeError::InvalidCatalog(format!("falta {name}")))
    }
    fn positive(value: impl Into<u64>, name: &str) -> Result<(), DecodeError> {
        if value.into() == 0 {
            return Err(DecodeError::InvalidCatalog(format!(
                "{name} debe ser mayor que 0"
            )));
        }
        Ok(())
    }
    if catalog.catalog_version.is_empty() {
        return Err(DecodeError::InvalidCatalog(
            "catalog_version está vacía".to_string(),
        ));
    }
    let drop = section(catalog.drop, "drop")?;
    let tank = section(catalog.tank, "tank")?;
    let growth = section(catalog.growth, "growth")?;
    let evaporation = section(catalog.evaporation, "evaporation")?;
    let fire = section(catalog.fire, "fire")?;
    let sprinkler = section(catalog.sprinkler, "sprinkler")?;
    let drip = section(catalog.drip, "drip")?;
    let plant = section(catalog.treatment_plant, "treatment_plant")?;
    let bigger_drop = section(catalog.bigger_drop, "bigger_drop")?;
    let bigger_tank = section(catalog.bigger_tank, "bigger_tank")?;

    positive(drop.demo_humidity, "drop.demo_humidity")?;
    positive(tank.refill_interval_seconds, "tank.refill_interval_seconds")?;
    positive(growth.humidity_per_stage, "growth.humidity_per_stage")?;
    positive(
        evaporation.wither_after_ticks,
        "evaporation.wither_after_ticks",
    )?;
    positive(fire.drops_to_extinguish, "fire.drops_to_extinguish")?;
    positive(fire.burn_out_ticks, "fire.burn_out_ticks")?;
    positive(sprinkler.area_side, "sprinkler.area_side")?;
    positive(sprinkler.interval_seconds, "sprinkler.interval_seconds")?;
    positive(drip.area_side, "drip.area_side")?;
    positive(drip.evaporation_divisor, "drip.evaporation_divisor")?;
    positive(plant.interval_seconds, "treatment_plant.interval_seconds")?;
    positive(bigger_drop.area_side, "bigger_drop.area_side")?;

    Ok(Catalog {
        version: catalog.catalog_version,
        drop: dc::DropRules {
            water_cost: drop.water_cost,
            humidity: drop.humidity,
            demo_humidity: drop.demo_humidity,
        },
        tank: dc::TankRules {
            capacity: tank.capacity,
            refill_amount: tank.refill_amount,
            refill_interval_seconds: tank.refill_interval_seconds,
        },
        growth: dc::GrowthRules {
            humidity_per_stage: growth.humidity_per_stage,
            leaves_grass: growth.leaves_grass,
            leaves_shrub: growth.leaves_shrub,
            leaves_flowers: growth.leaves_flowers,
            leaves_tree: growth.leaves_tree,
            tree_max_humidity: growth.tree_max_humidity,
        },
        evaporation: dc::EvaporationRules {
            humidity_per_second: evaporation.humidity_per_second,
            wither_after_ticks: evaporation.wither_after_ticks,
        },
        fire: dc::FireRules {
            drops_to_extinguish: fire.drops_to_extinguish,
            extinguish_bonus_leaves: fire.extinguish_bonus_leaves,
            burn_out_ticks: fire.burn_out_ticks,
            ignition_chance_ppm: fire.ignition_chance_ppm,
        },
        sprinkler: dc::SprinklerRules {
            price: sprinkler.price,
            area_side: sprinkler.area_side,
            humidity: sprinkler.humidity,
            interval_seconds: sprinkler.interval_seconds,
        },
        drip: dc::DripRules {
            price: drip.price,
            area_side: drip.area_side,
            evaporation_divisor: drip.evaporation_divisor,
        },
        treatment_plant: dc::TreatmentPlantRules {
            price: plant.price,
            refill_amount: plant.refill_amount,
            interval_seconds: plant.interval_seconds,
        },
        installation_price_increase_percent: catalog.installation_price_increase_percent,
        bigger_drop: dc::BiggerDropRules {
            price: bigger_drop.price,
            area_side: bigger_drop.area_side,
        },
        bigger_tank: dc::BiggerTankRules {
            price: bigger_tank.price,
            extra_capacity: bigger_tank.extra_capacity,
        },
        offline_installation_seconds: catalog.offline_installation_seconds,
    })
}

pub fn command_from_proto(command: pb::Command) -> Command {
    use pb::command::Kind;
    // Un tipo de instalación o de mejora desconocido deja el comando sin
    // tipo: el simulador lo rechaza como INVALID_COMMAND.
    let kind = command.kind.and_then(|kind| match kind {
        Kind::Drop(drop) => Some(CommandKind::Drop {
            x: drop.x,
            y: drop.y,
        }),
        Kind::PlaceInstallation(place) => Some(CommandKind::PlaceInstallation {
            kind: installation_from_proto(place.kind, "PlaceInstallationCommand.kind").ok()?,
            x: place.x,
            y: place.y,
        }),
        Kind::BuyUpgrade(buy) => Some(CommandKind::BuyUpgrade {
            kind: upgrade_from_proto(buy.kind).ok()?,
        }),
        Kind::SetDemoMode(demo) => Some(CommandKind::SetDemoMode {
            enabled: demo.enabled,
        }),
        Kind::ResetValley(_) => Some(CommandKind::ResetValley),
    });
    Command {
        action_id: command.action_id,
        player_id: command.player_id,
        kind,
    }
}

pub fn result_to_proto(result: CommandResult) -> pb::CommandResult {
    let (status, reason) = match result.outcome {
        CommandOutcome::Applied => (pb::CommandStatus::Applied, pb::RejectReason::Unspecified),
        CommandOutcome::Duplicate => (pb::CommandStatus::Duplicate, pb::RejectReason::Unspecified),
        CommandOutcome::Rejected(reason) => (pb::CommandStatus::Rejected, reject_reason(reason)),
    };
    pb::CommandResult {
        action_id: result.action_id,
        player_id: result.player_id,
        status: status.into(),
        reason: reason.into(),
    }
}

fn reject_reason(reason: RejectReason) -> pb::RejectReason {
    match reason {
        RejectReason::InsufficientWater => pb::RejectReason::InsufficientWater,
        RejectReason::OutOfBounds => pb::RejectReason::OutOfBounds,
        RejectReason::InsufficientLeaves => pb::RejectReason::InsufficientLeaves,
        RejectReason::TileOccupied => pb::RejectReason::TileOccupied,
        RejectReason::UpgradeAlreadyOwned => pb::RejectReason::UpgradeAlreadyOwned,
        RejectReason::InvalidCommand => pb::RejectReason::InvalidCommand,
        // Tampoco hay uno para «mundo lleno»: INVALID_COMMAND es el más
        // cercano (el comando no se puede aplicar tal como llega).
        RejectReason::WorldFull => pb::RejectReason::InvalidCommand,
    }
}

/// Estado que el hub difunde (RF-03).
pub fn world_state(world: &World) -> pb::WorldState {
    pb::WorldState {
        width: world.width(),
        height: world.height(),
        tiles: world.tiles().iter().map(tile_to_proto).collect(),
        players: world
            .players()
            .map(|player| player_state(world, player))
            .collect(),
        demo_mode: world.demo_mode(),
        humidity_contributed: world.humidity_contributed(),
        epoch: world.epoch(),
    }
}

fn player_state(world: &World, player: &Player) -> pb::PlayerState {
    pb::PlayerState {
        player_id: player.player_id.clone(),
        water: player.water,
        tank_capacity: player.tank_capacity,
        leaves: player.leaves,
        upgrades: player
            .upgrades
            .iter()
            .map(|u| upgrade_to_proto(*u).into())
            .collect(),
        // Precio de la siguiente instalación de cada tipo (RF-07), calculado
        // por las reglas del simulador: el hub solo lo reenvía.
        next_prices: [
            InstallationKind::Sprinkler,
            InstallationKind::Drip,
            InstallationKind::TreatmentPlant,
        ]
        .into_iter()
        .map(|kind| pb::InstallationPrice {
            kind: installation_to_proto(kind).into(),
            leaves: world.next_price(player, kind),
        })
        .collect(),
    }
}

/// Fila de la instantánea con el `WorldSnapshot` serializado dentro.
pub fn encode_snapshot(world: &World) -> persistence_pb::Snapshot {
    let parts = world.parts();
    let players_row = parts.players.iter().map(snapshot_player).collect();
    let inner = pb::WorldSnapshot {
        tick: parts.tick,
        epoch: parts.epoch,
        catalog_version: parts.catalog_version.clone(),
        rng_state: parts.rng_state,
        demo_mode: parts.demo_mode,
        humidity_contributed: parts.humidity_contributed,
        width: parts.width,
        height: parts.height,
        tiles: parts.tiles.iter().map(tile_to_proto).collect(),
        players: parts.players.iter().map(player_record).collect(),
    };
    persistence_pb::Snapshot {
        tick: parts.tick,
        epoch: parts.epoch,
        catalog_version: parts.catalog_version,
        world_state: inner.encode_to_vec(),
        players: players_row,
    }
}

pub fn decode_snapshot(
    snapshot: persistence_pb::Snapshot,
    catalog: Catalog,
) -> Result<World, DecodeError> {
    let inner = pb::WorldSnapshot::decode(snapshot.world_state.as_slice())
        .map_err(DecodeError::Malformed)?;
    if inner.tick != snapshot.tick {
        return Err(DecodeError::TickMismatch {
            row: snapshot.tick,
            world: inner.tick,
        });
    }
    let parts = WorldParts {
        tick: inner.tick,
        epoch: inner.epoch,
        catalog_version: inner.catalog_version,
        rng_state: inner.rng_state,
        demo_mode: inner.demo_mode,
        humidity_contributed: inner.humidity_contributed,
        width: inner.width,
        height: inner.height,
        tiles: inner
            .tiles
            .into_iter()
            .map(tile_from_proto)
            .collect::<Result<_, _>>()?,
        players: inner
            .players
            .into_iter()
            .map(player_from_record)
            .collect::<Result<_, _>>()?,
    };
    World::restore(catalog, parts).map_err(DecodeError::Restore)
}

fn tile_to_proto(tile: &Tile) -> pb::Tile {
    pb::Tile {
        stage: stage_to_proto(tile.stage).into(),
        humidity: tile.humidity,
        dry_ticks: tile.dry_ticks,
        fire: tile.fire.map(|fire| pb::Fire {
            drops_received: fire.drops_received,
            burning_ticks: fire.burning_ticks,
        }),
        installation: tile
            .installation
            .as_ref()
            .map(|installation| pb::Installation {
                kind: installation_to_proto(installation.kind).into(),
                owner_id: installation.owner_id.clone(),
            }),
    }
}

fn tile_from_proto(tile: pb::Tile) -> Result<Tile, DecodeError> {
    let installation = match tile.installation {
        Some(installation) => Some(Installation {
            kind: installation_from_proto(installation.kind, "Installation.kind")?,
            owner_id: installation.owner_id,
        }),
        None => None,
    };
    Ok(Tile {
        stage: stage_from_proto(tile.stage)?,
        humidity: tile.humidity,
        dry_ticks: tile.dry_ticks,
        fire: tile.fire.map(|fire| Fire {
            drops_received: fire.drops_received,
            burning_ticks: fire.burning_ticks,
        }),
        installation,
    })
}

fn player_record(player: &Player) -> pb::PlayerRecord {
    pb::PlayerRecord {
        player_id: player.player_id.clone(),
        water: player.water,
        tank_capacity: player.tank_capacity,
        leaves: player.leaves,
        water_refilled: player.water_refilled,
        water_spent: player.water_spent,
        leaves_earned: player.leaves_earned,
        leaves_spent: player.leaves_spent,
        water_reduced: player.water_reduced,
        water_reused: player.water_reused,
        water_recycled: player.water_recycled,
        purchases: player
            .purchases
            .iter()
            .map(|(kind, count)| pb::InstallationCount {
                kind: installation_to_proto(*kind).into(),
                count: *count,
            })
            .collect(),
        upgrades: player
            .upgrades
            .iter()
            .map(|u| upgrade_to_proto(*u).into())
            .collect(),
        last_action_tick: player.last_action_tick,
        recent_actions: player
            .recent_actions
            .iter()
            .map(|applied| pb::AppliedAction {
                action_id: applied.action_id.clone(),
                tick: applied.tick,
            })
            .collect(),
    }
}

fn player_from_record(record: pb::PlayerRecord) -> Result<Player, DecodeError> {
    Ok(Player {
        player_id: record.player_id,
        water: record.water,
        tank_capacity: record.tank_capacity,
        leaves: record.leaves,
        water_refilled: record.water_refilled,
        water_spent: record.water_spent,
        leaves_earned: record.leaves_earned,
        leaves_spent: record.leaves_spent,
        water_reduced: record.water_reduced,
        water_reused: record.water_reused,
        water_recycled: record.water_recycled,
        purchases: record
            .purchases
            .into_iter()
            .map(|count| {
                installation_from_proto(count.kind, "InstallationCount.kind")
                    .map(|kind| (kind, count.count))
            })
            .collect::<Result<_, _>>()?,
        upgrades: record
            .upgrades
            .into_iter()
            .map(upgrade_from_proto)
            .collect::<Result<_, _>>()?,
        last_action_tick: record.last_action_tick,
        recent_actions: record
            .recent_actions
            .into_iter()
            .map(|applied| AppliedAction {
                action_id: applied.action_id,
                tick: applied.tick,
            })
            .collect(),
    })
}

fn snapshot_player(player: &Player) -> persistence_pb::SnapshotPlayer {
    persistence_pb::SnapshotPlayer {
        player_id: player.player_id.clone(),
        water: player.water,
        leaves: player.leaves,
        water_refilled: player.water_refilled,
        water_spent: player.water_spent,
        leaves_earned: player.leaves_earned,
        leaves_spent: player.leaves_spent,
        water_reduced: player.water_reduced,
        water_reused: player.water_reused,
        water_recycled: player.water_recycled,
    }
}

fn stage_to_proto(stage: TileStage) -> pb::TileStage {
    match stage {
        TileStage::DrySoil => pb::TileStage::DrySoil,
        TileStage::Grass => pb::TileStage::Grass,
        TileStage::Shrub => pb::TileStage::Shrub,
        TileStage::Flowers => pb::TileStage::Flowers,
        TileStage::Tree => pb::TileStage::Tree,
    }
}

fn stage_from_proto(value: i32) -> Result<TileStage, DecodeError> {
    match pb::TileStage::try_from(value) {
        Ok(pb::TileStage::DrySoil) => Ok(TileStage::DrySoil),
        Ok(pb::TileStage::Grass) => Ok(TileStage::Grass),
        Ok(pb::TileStage::Shrub) => Ok(TileStage::Shrub),
        Ok(pb::TileStage::Flowers) => Ok(TileStage::Flowers),
        Ok(pb::TileStage::Tree) => Ok(TileStage::Tree),
        Ok(pb::TileStage::Unspecified) | Err(_) => Err(DecodeError::UnknownEnum {
            field: "Tile.stage",
            value,
        }),
    }
}

fn installation_to_proto(kind: InstallationKind) -> pb::InstallationKind {
    match kind {
        InstallationKind::Sprinkler => pb::InstallationKind::Sprinkler,
        InstallationKind::Drip => pb::InstallationKind::Drip,
        InstallationKind::TreatmentPlant => pb::InstallationKind::TreatmentPlant,
    }
}

fn installation_from_proto(
    value: i32,
    field: &'static str,
) -> Result<InstallationKind, DecodeError> {
    match pb::InstallationKind::try_from(value) {
        Ok(pb::InstallationKind::Sprinkler) => Ok(InstallationKind::Sprinkler),
        Ok(pb::InstallationKind::Drip) => Ok(InstallationKind::Drip),
        Ok(pb::InstallationKind::TreatmentPlant) => Ok(InstallationKind::TreatmentPlant),
        Ok(pb::InstallationKind::Unspecified) | Err(_) => {
            Err(DecodeError::UnknownEnum { field, value })
        }
    }
}

fn upgrade_to_proto(kind: UpgradeKind) -> pb::UpgradeKind {
    match kind {
        UpgradeKind::BiggerDrop => pb::UpgradeKind::BiggerDrop,
        UpgradeKind::BiggerTank => pb::UpgradeKind::BiggerTank,
    }
}

fn upgrade_from_proto(value: i32) -> Result<UpgradeKind, DecodeError> {
    match pb::UpgradeKind::try_from(value) {
        Ok(pb::UpgradeKind::BiggerDrop) => Ok(UpgradeKind::BiggerDrop),
        Ok(pb::UpgradeKind::BiggerTank) => Ok(UpgradeKind::BiggerTank),
        Ok(pb::UpgradeKind::Unspecified) | Err(_) => Err(DecodeError::UnknownEnum {
            field: "PlayerRecord.upgrades",
            value,
        }),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    const ANA: &str = "6f1c1a52-2d0e-4f4e-9c1e-0c6c2d9b8a01";

    fn action(n: u32) -> String {
        format!("00000000-0000-4000-8000-{n:012}")
    }

    fn pb_command(kind: Option<pb::command::Kind>) -> pb::Command {
        pb::Command {
            action_id: action(1),
            player_id: ANA.to_string(),
            kind,
        }
    }

    fn drop(x: u32, y: u32) -> Command {
        Command {
            action_id: action(x * 100 + y),
            player_id: ANA.to_string(),
            kind: Some(CommandKind::Drop { x, y }),
        }
    }

    #[test]
    fn commands_map_every_oneof_kind() {
        use pb::command::Kind;
        let cases = [
            (
                Some(Kind::Drop(pb::DropCommand { x: 3, y: 4 })),
                Some(CommandKind::Drop { x: 3, y: 4 }),
            ),
            (
                Some(Kind::PlaceInstallation(pb::PlaceInstallationCommand {
                    kind: pb::InstallationKind::Drip.into(),
                    x: 5,
                    y: 6,
                })),
                Some(CommandKind::PlaceInstallation {
                    kind: InstallationKind::Drip,
                    x: 5,
                    y: 6,
                }),
            ),
            (
                Some(Kind::BuyUpgrade(pb::BuyUpgradeCommand {
                    kind: pb::UpgradeKind::BiggerTank.into(),
                })),
                Some(CommandKind::BuyUpgrade {
                    kind: UpgradeKind::BiggerTank,
                }),
            ),
            // Un tipo sin especificar o desconocido deja el comando sin tipo
            // (INVALID_COMMAND en el simulador).
            (
                Some(Kind::PlaceInstallation(pb::PlaceInstallationCommand {
                    kind: pb::InstallationKind::Unspecified.into(),
                    x: 1,
                    y: 1,
                })),
                None,
            ),
            (
                Some(Kind::PlaceInstallation(pb::PlaceInstallationCommand {
                    kind: 99,
                    x: 1,
                    y: 1,
                })),
                None,
            ),
            (
                Some(Kind::BuyUpgrade(pb::BuyUpgradeCommand { kind: 0 })),
                None,
            ),
            (
                Some(Kind::SetDemoMode(pb::SetDemoModeCommand { enabled: true })),
                Some(CommandKind::SetDemoMode { enabled: true }),
            ),
            (
                Some(Kind::ResetValley(pb::ResetValleyCommand {})),
                Some(CommandKind::ResetValley),
            ),
            (None, None),
        ];
        for (kind, expected) in cases {
            let command = command_from_proto(pb_command(kind));
            assert_eq!(command.action_id, action(1));
            assert_eq!(command.player_id, ANA);
            assert_eq!(command.kind, expected);
        }
    }

    #[test]
    fn results_map_status_and_reason() {
        let cases = [
            (
                CommandOutcome::Applied,
                pb::CommandStatus::Applied,
                pb::RejectReason::Unspecified,
            ),
            (
                CommandOutcome::Duplicate,
                pb::CommandStatus::Duplicate,
                pb::RejectReason::Unspecified,
            ),
            (
                CommandOutcome::Rejected(RejectReason::InsufficientWater),
                pb::CommandStatus::Rejected,
                pb::RejectReason::InsufficientWater,
            ),
            (
                CommandOutcome::Rejected(RejectReason::OutOfBounds),
                pb::CommandStatus::Rejected,
                pb::RejectReason::OutOfBounds,
            ),
            (
                CommandOutcome::Rejected(RejectReason::InvalidCommand),
                pb::CommandStatus::Rejected,
                pb::RejectReason::InvalidCommand,
            ),
            (
                CommandOutcome::Rejected(RejectReason::InsufficientLeaves),
                pb::CommandStatus::Rejected,
                pb::RejectReason::InsufficientLeaves,
            ),
            (
                CommandOutcome::Rejected(RejectReason::TileOccupied),
                pb::CommandStatus::Rejected,
                pb::RejectReason::TileOccupied,
            ),
            (
                CommandOutcome::Rejected(RejectReason::UpgradeAlreadyOwned),
                pb::CommandStatus::Rejected,
                pb::RejectReason::UpgradeAlreadyOwned,
            ),
            (
                CommandOutcome::Rejected(RejectReason::WorldFull),
                pb::CommandStatus::Rejected,
                pb::RejectReason::InvalidCommand,
            ),
        ];
        for (outcome, status, reason) in cases {
            let result = result_to_proto(CommandResult {
                action_id: action(7),
                player_id: ANA.to_string(),
                outcome,
            });
            assert_eq!(result.action_id, action(7));
            assert_eq!(result.player_id, ANA);
            assert_eq!(result.status(), status);
            assert_eq!(result.reason(), reason);
        }
    }

    #[test]
    fn world_state_carries_tiles_players_and_counters() {
        let mut world = World::new(Catalog::d01());
        world.step(&[drop(2, 1)]);
        let state = world_state(&world);

        assert_eq!((state.width, state.height), (32, 32));
        assert_eq!(state.tiles.len(), 1024);
        let tile = &state.tiles[32 + 2];
        assert_eq!(tile.stage(), pb::TileStage::DrySoil);
        assert_eq!(tile.humidity, 25);
        assert!(tile.fire.is_none() && tile.installation.is_none());
        assert_eq!(state.humidity_contributed, 25);
        assert!(!state.demo_mode);
        assert_eq!(state.epoch, 0);
        assert_eq!(state.players.len(), 1);
        assert_eq!(state.players[0].player_id, ANA);
        assert_eq!(state.players[0].water, 19);
        assert_eq!(state.players[0].tank_capacity, 20);
    }

    fn rich_world() -> World {
        let mut world = World::new(Catalog::d01());
        world.set_demo_mode(true);
        world.step(&[drop(0, 0), drop(31, 31)]);
        let mut parts = world.parts();
        parts.epoch = 3;
        parts.rng_state = vec![1, 2, 3];
        parts.tiles[5] = Tile {
            stage: TileStage::Flowers,
            humidity: 40,
            dry_ticks: 7,
            fire: Some(Fire {
                drops_received: 2,
                burning_ticks: 90,
            }),
            installation: Some(Installation {
                kind: InstallationKind::Drip,
                owner_id: ANA.to_string(),
            }),
        };
        let player = &mut parts.players[0];
        player.leaves = 9;
        player.leaves_earned = 12;
        player.purchases = vec![(InstallationKind::Sprinkler, 2)];
        player.upgrades = vec![UpgradeKind::BiggerTank];
        World::restore(Catalog::d01(), parts).unwrap()
    }

    #[test]
    fn snapshot_round_trips_through_the_wire_format() {
        let world = rich_world();
        let snapshot = encode_snapshot(&world);

        assert_eq!(snapshot.tick, 1);
        assert_eq!(snapshot.epoch, 3);
        assert_eq!(snapshot.catalog_version, Catalog::D01_VERSION);
        let inner = pb::WorldSnapshot::decode(snapshot.world_state.as_slice()).unwrap();
        assert_eq!(inner.catalog_version, Catalog::D01_VERSION);
        assert_eq!(snapshot.players.len(), 1);
        assert_eq!(snapshot.players[0].player_id, ANA);
        assert_eq!(snapshot.players[0].leaves_earned, 12);
        let inner = pb::WorldSnapshot::decode(snapshot.world_state.as_slice()).unwrap();
        assert_eq!(inner.tick, 1);
        assert_eq!(inner.players[0].recent_actions.len(), 2);

        let restored = decode_snapshot(snapshot, Catalog::d01()).unwrap();
        assert_eq!(restored.parts(), world.parts());
    }

    #[test]
    fn world_state_lists_the_next_price_of_every_installation_kind() {
        use pb::InstallationKind as Kind;
        let mut parts = World::new(Catalog::d01()).parts();
        parts.players.push(Player {
            leaves: 100,
            leaves_earned: 100,
            purchases: vec![
                (InstallationKind::Sprinkler, 2),
                (InstallationKind::Drip, 1),
            ],
            ..Player::starting(ANA, 20)
        });
        let world = World::restore(Catalog::d01(), parts).unwrap();
        let state = world_state(&world);
        let prices: Vec<(Kind, u64)> = state.players[0]
            .next_prices
            .iter()
            .map(|p| (p.kind(), p.leaves))
            .collect();
        // Sprinkler 10 -> 12 -> 14; drip 25 -> 29; plant sin compras.
        assert_eq!(
            prices,
            vec![
                (Kind::Sprinkler, 14),
                (Kind::Drip, 29),
                (Kind::TreatmentPlant, 40)
            ]
        );
    }

    #[test]
    fn installations_upgrades_and_3r_counters_produced_by_the_rules_survive_the_wire() {
        let command = |n: u32, kind: CommandKind| Command {
            action_id: format!("00000000-0000-4000-8000-{n:012}"),
            player_id: ANA.to_string(),
            kind: Some(kind),
        };
        let mut parts = World::new(Catalog::d01()).parts();
        parts.players.push(Player {
            leaves: 200,
            leaves_earned: 200,
            ..Player::starting(ANA, 20)
        });
        let mut world = World::restore(Catalog::d01(), parts).unwrap();
        world.step(&[
            command(
                1,
                CommandKind::PlaceInstallation {
                    kind: InstallationKind::Sprinkler,
                    x: 6,
                    y: 6,
                },
            ),
            command(
                2,
                CommandKind::PlaceInstallation {
                    kind: InstallationKind::Drip,
                    x: 8,
                    y: 6,
                },
            ),
            command(
                3,
                CommandKind::PlaceInstallation {
                    kind: InstallationKind::TreatmentPlant,
                    x: 1,
                    y: 1,
                },
            ),
            command(
                4,
                CommandKind::BuyUpgrade {
                    kind: UpgradeKind::BiggerDrop,
                },
            ),
            command(
                5,
                CommandKind::BuyUpgrade {
                    kind: UpgradeKind::BiggerTank,
                },
            ),
            command(6, CommandKind::Drop { x: 7, y: 6 }),
        ]);
        for _ in 0..60 {
            world.step(&[]);
        }
        let ana = world.player(ANA).unwrap();
        assert!(ana.water_reused > 0 && ana.water_reduced > 0 && ana.water_recycled > 0);

        let restored = decode_snapshot(encode_snapshot(&world), Catalog::d01()).unwrap();
        assert_eq!(restored.parts(), world.parts());
        let state = world_state(&restored);
        let installed: Vec<_> = state
            .tiles
            .iter()
            .filter_map(|t| t.installation.as_ref().map(|i| i.kind()))
            .collect();
        assert_eq!(installed.len(), 3);
        let p = &state.players[0];
        assert_eq!(p.tank_capacity, 30);
        assert_eq!(p.upgrades.len(), 2);
    }

    #[test]
    fn the_state_produced_by_the_tick_rules_survives_the_wire() {
        // dry_ticks, época, modo demo y contadores de RF-27 salen de las
        // reglas, no de una instantánea armada a mano.
        let admin = "aaaaaaaa-0000-4000-8000-000000000001";
        let command = |n: u32, player: &str, kind: CommandKind| Command {
            action_id: format!("00000000-0000-4000-8000-{n:012}"),
            player_id: player.to_string(),
            kind: Some(kind),
        };
        let mut world = World::new(Catalog::d01());
        world.step(&[command(1, admin, CommandKind::ResetValley)]);
        let growing: Vec<Command> = (10..14)
            .map(|n| command(n, ANA, CommandKind::Drop { x: 4, y: 4 }))
            .collect();
        world.step(&growing); // pasto con humedad 0: empieza a secarse
        for _ in 0..30 {
            world.step(&[]);
        }
        world.step(&[command(
            2,
            admin,
            CommandKind::SetDemoMode { enabled: true },
        )]);

        let restored = decode_snapshot(encode_snapshot(&world), Catalog::d01()).unwrap();
        assert_eq!(restored.parts(), world.parts());
        let parts = restored.parts();
        assert_eq!(parts.epoch, 1);
        assert!(parts.demo_mode);
        assert_eq!(parts.tiles[4 * 32 + 4].dry_ticks, 31);
        let ana = parts.players.iter().find(|p| p.player_id == ANA).unwrap();
        assert_eq!((ana.leaves, ana.leaves_earned), (1, 1));
        assert_eq!((ana.water_refilled, ana.water_spent), (21, 4));
    }

    #[test]
    fn decode_rejects_garbage_and_inconsistent_rows() {
        let world = World::new(Catalog::d01());

        let mut garbage = encode_snapshot(&world);
        garbage.world_state = vec![0xff, 0xff, 0xff];
        assert!(matches!(
            decode_snapshot(garbage, Catalog::d01()),
            Err(DecodeError::Malformed(_))
        ));

        let mut mismatch = encode_snapshot(&world);
        mismatch.tick = 50;
        assert!(matches!(
            decode_snapshot(mismatch, Catalog::d01()),
            Err(DecodeError::TickMismatch { row: 50, world: 0 })
        ));

        let mut inner =
            pb::WorldSnapshot::decode(encode_snapshot(&world).world_state.as_slice()).unwrap();
        inner.tiles[0].stage = 99;
        let mut unknown = encode_snapshot(&world);
        unknown.world_state = inner.encode_to_vec();
        assert!(matches!(
            decode_snapshot(unknown, Catalog::d01()),
            Err(DecodeError::UnknownEnum {
                field: "Tile.stage",
                value: 99
            })
        ));

        let mut inner =
            pb::WorldSnapshot::decode(encode_snapshot(&world).world_state.as_slice()).unwrap();
        inner.tiles.pop();
        let mut short = encode_snapshot(&world);
        short.world_state = inner.encode_to_vec();
        assert!(matches!(
            decode_snapshot(short, Catalog::d01()),
            Err(DecodeError::Restore(_))
        ));
    }

    /// El catálogo de D-01 tal como lo entrega `GetCatalog`. Se deriva de
    /// `Catalog::d01`, la única copia de las cifras para las pruebas
    /// unitarias.
    fn d01_message() -> persistence_pb::Catalog {
        use persistence_pb as p;
        let c = Catalog::d01();
        p::Catalog {
            catalog_version: c.version,
            drop: Some(p::DropRules {
                water_cost: c.drop.water_cost,
                humidity: c.drop.humidity,
                demo_humidity: c.drop.demo_humidity,
            }),
            tank: Some(p::TankRules {
                capacity: c.tank.capacity,
                refill_amount: c.tank.refill_amount,
                refill_interval_seconds: c.tank.refill_interval_seconds,
            }),
            growth: Some(p::GrowthRules {
                humidity_per_stage: c.growth.humidity_per_stage,
                leaves_grass: c.growth.leaves_grass,
                leaves_shrub: c.growth.leaves_shrub,
                leaves_flowers: c.growth.leaves_flowers,
                leaves_tree: c.growth.leaves_tree,
                tree_max_humidity: c.growth.tree_max_humidity,
            }),
            evaporation: Some(p::EvaporationRules {
                humidity_per_second: c.evaporation.humidity_per_second,
                wither_after_ticks: c.evaporation.wither_after_ticks,
            }),
            fire: Some(p::FireRules {
                drops_to_extinguish: c.fire.drops_to_extinguish,
                extinguish_bonus_leaves: c.fire.extinguish_bonus_leaves,
                burn_out_ticks: c.fire.burn_out_ticks,
                ignition_chance_ppm: c.fire.ignition_chance_ppm,
            }),
            sprinkler: Some(p::SprinklerRules {
                price: c.sprinkler.price,
                area_side: c.sprinkler.area_side,
                humidity: c.sprinkler.humidity,
                interval_seconds: c.sprinkler.interval_seconds,
            }),
            drip: Some(p::DripRules {
                price: c.drip.price,
                area_side: c.drip.area_side,
                evaporation_divisor: c.drip.evaporation_divisor,
            }),
            treatment_plant: Some(p::TreatmentPlantRules {
                price: c.treatment_plant.price,
                refill_amount: c.treatment_plant.refill_amount,
                interval_seconds: c.treatment_plant.interval_seconds,
            }),
            installation_price_increase_percent: c.installation_price_increase_percent,
            bigger_drop: Some(p::BiggerDropRules {
                price: c.bigger_drop.price,
                area_side: c.bigger_drop.area_side,
            }),
            bigger_tank: Some(p::BiggerTankRules {
                price: c.bigger_tank.price,
                extra_capacity: c.bigger_tank.extra_capacity,
            }),
            offline_installation_seconds: c.offline_installation_seconds,
        }
    }

    #[test]
    fn a_catalog_message_becomes_the_domain_catalog() {
        assert_eq!(catalog_from_proto(d01_message()).unwrap(), Catalog::d01());
    }

    #[test]
    fn the_demo_humidity_of_the_drop_is_carried_over() {
        let mut message = d01_message();
        message.drop.as_mut().unwrap().demo_humidity = 7;
        assert_eq!(catalog_from_proto(message).unwrap().drop.demo_humidity, 7);
    }

    #[test]
    fn the_ignition_chance_and_changed_figures_are_carried_over() {
        let mut message = d01_message();
        message.fire.as_mut().unwrap().ignition_chance_ppm = 500;
        message.sprinkler.as_mut().unwrap().price = 12;
        let catalog = catalog_from_proto(message).unwrap();
        assert_eq!(catalog.fire.ignition_chance_ppm, 500);
        assert_eq!(catalog.sprinkler.price, 12);
    }

    #[test]
    fn an_incomplete_or_impossible_catalog_is_rejected() {
        type Break = fn(&mut persistence_pb::Catalog);
        let breaks: [(&str, Break); 17] = [
            ("version", |c| c.catalog_version.clear()),
            ("drop", |c| c.drop = None),
            ("tank", |c| c.tank = None),
            ("growth", |c| c.growth = None),
            ("evaporation", |c| c.evaporation = None),
            ("fire", |c| c.fire = None),
            ("sprinkler", |c| c.sprinkler = None),
            ("drip", |c| c.drip = None),
            ("treatment_plant", |c| c.treatment_plant = None),
            ("bigger_drop", |c| c.bigger_drop = None),
            ("bigger_tank", |c| c.bigger_tank = None),
            ("stage", |c| {
                c.growth.as_mut().unwrap().humidity_per_stage = 0
            }),
            ("wither", |c| {
                c.evaporation.as_mut().unwrap().wither_after_ticks = 0
            }),
            ("sprinkler area", |c| {
                c.sprinkler.as_mut().unwrap().area_side = 0
            }),
            ("drip divisor", |c| {
                c.drip.as_mut().unwrap().evaporation_divisor = 0
            }),
            ("demo humidity", |c| {
                c.drop.as_mut().unwrap().demo_humidity = 0
            }),
            ("refill interval", |c| {
                c.tank.as_mut().unwrap().refill_interval_seconds = 0
            }),
        ];
        for (name, break_it) in breaks {
            let mut message = d01_message();
            break_it(&mut message);
            assert!(
                matches!(
                    catalog_from_proto(message),
                    Err(DecodeError::InvalidCatalog(_))
                ),
                "{name}"
            );
        }
    }
}
