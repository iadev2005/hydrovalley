//! Traducción entre los mensajes del contrato y el dominio.
//!
//! Es la frontera: el dominio no conoce ningún tipo generado desde
//! `proto/`, y aquí se decide cómo se representa cada cosa en el cable.

use prost::Message;

use crate::domain::{
    self, AppliedAction, Command, CommandKind, CommandOutcome, CommandResult, Fire, Installation,
    InstallationKind, Player, RejectReason, Rules, Tile, TileStage, UpgradeKind, World, WorldParts,
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
        }
    }
}

impl std::error::Error for DecodeError {}

pub fn command_from_proto(command: pb::Command) -> Command {
    use pb::command::Kind;
    let kind = command.kind.map(|kind| match kind {
        Kind::Drop(drop) => CommandKind::Drop {
            x: drop.x,
            y: drop.y,
        },
        Kind::PlaceInstallation(_) => CommandKind::PlaceInstallation,
        Kind::BuyUpgrade(_) => CommandKind::BuyUpgrade,
        Kind::SetDemoMode(demo) => CommandKind::SetDemoMode {
            enabled: demo.enabled,
        },
        Kind::ResetValley(_) => CommandKind::ResetValley,
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
        RejectReason::InvalidCommand => pb::RejectReason::InvalidCommand,
        // El contrato no tiene un motivo "todavía no soportado"; el más
        // cercano es INVALID_COMMAND (el simulador no sabe aplicarlo).
        RejectReason::Unsupported => pb::RejectReason::InvalidCommand,
    }
}

/// Estado que el hub difunde (RF-03).
pub fn world_state(world: &World) -> pb::WorldState {
    pb::WorldState {
        width: world.width(),
        height: world.height(),
        tiles: world.tiles().iter().map(tile_to_proto).collect(),
        players: world.players().map(player_state).collect(),
        demo_mode: world.demo_mode(),
        humidity_contributed: world.humidity_contributed(),
        epoch: world.epoch(),
    }
}

fn player_state(player: &Player) -> pb::PlayerState {
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
        // TODO(catalog): precio de la siguiente instalación de cada tipo
        // (RF-07), cuando existan las instalaciones y CatalogService.
        next_prices: Vec::new(),
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
    rules: Rules,
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
    World::restore(rules, parts).map_err(DecodeError::Restore)
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
                Some(Kind::PlaceInstallation(
                    pb::PlaceInstallationCommand::default(),
                )),
                Some(CommandKind::PlaceInstallation),
            ),
            (
                Some(Kind::BuyUpgrade(pb::BuyUpgradeCommand::default())),
                Some(CommandKind::BuyUpgrade),
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
            // El contrato no tiene un motivo "no soportado": se usa el más cercano.
            (
                CommandOutcome::Rejected(RejectReason::Unsupported),
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
        let mut world = World::new(Rules::initial());
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
        let mut world = World::new(Rules::initial());
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
        World::restore(Rules::initial(), parts).unwrap()
    }

    #[test]
    fn snapshot_round_trips_through_the_wire_format() {
        let world = rich_world();
        let snapshot = encode_snapshot(&world);

        assert_eq!(snapshot.tick, 1);
        assert_eq!(snapshot.epoch, 3);
        assert_eq!(snapshot.catalog_version, "");
        assert_eq!(snapshot.players.len(), 1);
        assert_eq!(snapshot.players[0].player_id, ANA);
        assert_eq!(snapshot.players[0].leaves_earned, 12);
        let inner = pb::WorldSnapshot::decode(snapshot.world_state.as_slice()).unwrap();
        assert_eq!(inner.tick, 1);
        assert_eq!(inner.players[0].recent_actions.len(), 2);

        let restored = decode_snapshot(snapshot, Rules::initial()).unwrap();
        assert_eq!(restored.parts(), world.parts());
    }

    #[test]
    fn decode_rejects_garbage_and_inconsistent_rows() {
        let world = World::new(Rules::initial());

        let mut garbage = encode_snapshot(&world);
        garbage.world_state = vec![0xff, 0xff, 0xff];
        assert!(matches!(
            decode_snapshot(garbage, Rules::initial()),
            Err(DecodeError::Malformed(_))
        ));

        let mut mismatch = encode_snapshot(&world);
        mismatch.tick = 50;
        assert!(matches!(
            decode_snapshot(mismatch, Rules::initial()),
            Err(DecodeError::TickMismatch { row: 50, world: 0 })
        ));

        let mut inner =
            pb::WorldSnapshot::decode(encode_snapshot(&world).world_state.as_slice()).unwrap();
        inner.tiles[0].stage = 99;
        let mut unknown = encode_snapshot(&world);
        unknown.world_state = inner.encode_to_vec();
        assert!(matches!(
            decode_snapshot(unknown, Rules::initial()),
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
            decode_snapshot(short, Rules::initial()),
            Err(DecodeError::Restore(_))
        ));
    }
}
