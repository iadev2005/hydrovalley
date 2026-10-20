//! Pruebas de la carga de trabajo y de las estadísticas de la comparativa
//! (`examples/bench`, `tests/bench/` en la raíz del repositorio). La medición
//! en sí no se prueba aquí: se verifica ejecutándola.
#![allow(dead_code)] // el ejemplo usa más cosas de las que prueba este archivo

#[path = "common/json_catalog.rs"]
mod json_catalog;
#[path = "../examples/bench/stats.rs"]
mod stats;
#[path = "../examples/bench/workload.rs"]
mod workload;

use std::fs;
use std::path::Path;

use workload::{Generator, Workload};
use world::domain::{Command, CommandKind, InstallationKind};

fn workload_file(name: &str) -> String {
    let path = Path::new(env!("CARGO_MANIFEST_DIR"))
        .join("../../tests/bench/workloads")
        .join(name);
    fs::read_to_string(path).expect("la carga existe")
}

fn small() -> Workload {
    Workload::from_json(
        r#"{"name": "t", "seed": 7, "width": 8, "height": 8, "players": 3,
            "warmup_ticks": 10, "measured_ticks": 5,
            "drops_per_1000_tiles": 250, "hot_percent": 50,
            "installations_per_1000_tiles": 100,
            "catalog_file": "catalog/catalog.json", "catalog_set": {}}"#,
    )
    .expect("la carga es válida")
}

fn fnv1a(text: &str) -> u64 {
    text.bytes().fold(0xcbf2_9ce4_8422_2325, |hash, byte| {
        (hash ^ u64::from(byte)).wrapping_mul(0x0000_0100_0000_01b3)
    })
}

fn describe(command: &Command) -> String {
    let kind = match command.kind.as_ref().expect("siempre con tipo") {
        CommandKind::Drop { x, y } => format!("D {x} {y}"),
        CommandKind::PlaceInstallation { kind, x, y } => {
            let code = match kind {
                InstallationKind::Sprinkler => 0,
                InstallationKind::Drip => 1,
                InstallationKind::TreatmentPlant => 2,
            };
            format!("P {code} {x} {y}")
        }
        other => panic!("la carga no genera {other:?}"),
    };
    format!("{} {} {kind}\n", command.action_id, command.player_id)
}

#[test]
fn the_workload_reads_its_parameters_and_derives_the_per_tick_load() {
    let w = small();
    assert_eq!((w.width, w.height, w.players), (8, 8, 3));
    assert_eq!((w.warmup_ticks, w.measured_ticks), (10, 5));
    assert_eq!(w.drops_per_tick(), 16); // 64 casillas * 250 / 1000
    assert_eq!(w.installations_total(), 6); // 64 * 100 / 1000
}

#[test]
fn a_workload_with_a_missing_or_impossible_field_is_refused() {
    assert!(Workload::from_json("{}").is_err());
    let text = r#"{"name": "t", "seed": 7, "width": 0, "height": 8, "players": 3,
        "warmup_ticks": 10, "measured_ticks": 5, "drops_per_1000_tiles": 1,
        "hot_percent": 50, "installations_per_1000_tiles": 1,
        "catalog_file": "c", "catalog_set": {}}"#;
    assert!(Workload::from_json(text).is_err());
    let over = text
        .replace("\"width\": 0", "\"width\": 8")
        .replace("50", "101");
    assert!(Workload::from_json(&over).is_err());
}

#[test]
fn the_same_workload_always_generates_the_same_commands() {
    let w = small();
    let (mut a, mut b) = (Generator::new(&w), Generator::new(&w));
    for tick in 1..=30 {
        assert_eq!(a.commands(tick), b.commands(tick));
    }
}

#[test]
fn every_tick_has_the_scaled_number_of_drops_inside_the_valley() {
    let w = small();
    let mut generator = Generator::new(&w);
    for tick in 1..=30 {
        let commands = generator.commands(tick);
        let drops: Vec<_> = commands
            .iter()
            .filter_map(|c| match c.kind {
                Some(CommandKind::Drop { x, y }) => Some((x, y)),
                _ => None,
            })
            .collect();
        assert_eq!(drops.len() as u64, w.drops_per_tick());
        assert!(drops.iter().all(|&(x, y)| x < 8 && y < 8));
    }
}

#[test]
fn installations_are_placed_only_in_the_second_half_of_the_warm_up() {
    let w = small();
    let mut generator = Generator::new(&w);
    let mut per_kind = [0u32; 3];
    for tick in 1..=30 {
        let placements: Vec<_> = generator
            .commands(tick)
            .into_iter()
            .filter_map(|c| match c.kind {
                Some(CommandKind::PlaceInstallation { kind, .. }) => Some(kind),
                _ => None,
            })
            .collect();
        if !(6..=10).contains(&tick) {
            assert!(placements.is_empty(), "colocación fuera de plazo en {tick}");
        }
        for kind in placements {
            per_kind[kind as usize] += 1;
        }
    }
    assert_eq!(per_kind, [2, 2, 2]); // 6 en total, por turnos
}

#[test]
fn ids_are_unique_valid_uuids_and_players_stay_within_the_fixed_set() {
    let w = small();
    let mut generator = Generator::new(&w);
    let mut actions = std::collections::HashSet::new();
    let mut players = std::collections::HashSet::new();
    for tick in 1..=30 {
        for command in generator.commands(tick) {
            assert!(uuid::Uuid::try_parse(&command.action_id).is_ok());
            assert!(uuid::Uuid::try_parse(&command.player_id).is_ok());
            assert!(actions.insert(command.action_id));
            players.insert(command.player_id);
        }
    }
    assert_eq!(players.len(), 3);
}

#[test]
fn drops_concentrate_on_the_central_quarter_as_configured() {
    let text = r#"{"name": "t", "seed": 1, "width": 32, "height": 32, "players": 4,
        "warmup_ticks": 2, "measured_ticks": 2, "drops_per_1000_tiles": 1000,
        "hot_percent": 100, "installations_per_1000_tiles": 0,
        "catalog_file": "c", "catalog_set": {}}"#;
    let w = Workload::from_json(text).unwrap();
    let mut generator = Generator::new(&w);
    for command in generator.commands(1) {
        let Some(CommandKind::Drop { x, y }) = command.kind else {
            panic!("solo gotas")
        };
        assert!((8..24).contains(&x) && (8..24).contains(&y), "({x}, {y})");
    }
}

/// Huella de los comandos de los 250 primeros ticks de `valley-32.json`.
/// El mismo valor está en la prueba de Go: si los dos generadores dejan de
/// coincidir, falla una de las dos.
const VALLEY_32_COMMANDS_FNV1A: u64 = 0x900e8f411a60993b;

#[test]
fn the_command_stream_of_valley_32_matches_the_golden_fingerprint() {
    let w = Workload::from_json(&workload_file("valley-32.json")).unwrap();
    let mut generator = Generator::new(&w);
    let mut text = String::new();
    for tick in 1..=250 {
        for command in generator.commands(tick) {
            text.push_str(&describe(&command));
        }
    }
    assert_eq!(
        fnv1a(&text),
        VALLEY_32_COMMANDS_FNV1A,
        "{:#x}",
        fnv1a(&text)
    );
}

#[test]
fn every_committed_workload_is_valid() {
    for size in [32, 64, 128, 256, 512] {
        let w = Workload::from_json(&workload_file(&format!("valley-{size}.json"))).unwrap();
        assert_eq!((w.width, w.height), (size, size));
        assert_eq!(w.players, 64);
        assert_eq!(w.name, format!("valley-{size}"));
    }
}

#[test]
fn summary_of_one_to_one_hundred_microseconds() {
    let samples: Vec<u64> = (1..=100).map(|n| n * 1000).collect();
    let s = stats::summarize(&samples).unwrap();
    assert_eq!(s.count, 100);
    assert_eq!(s.mean_us, 50.5);
    assert_eq!(s.median_us, 50.5);
    assert_eq!(s.p95_us, 95.0);
    assert_eq!(s.p99_us, 99.0);
    assert_eq!((s.min_us, s.max_us), (1.0, 100.0));
}

#[test]
fn the_summary_does_not_depend_on_the_order_of_the_samples() {
    let s = stats::summarize(&[3000, 1000, 2000]).unwrap();
    assert_eq!((s.median_us, s.mean_us), (2.0, 2.0));
    assert_eq!((s.p95_us, s.p99_us), (3.0, 3.0));
}

#[test]
fn a_single_sample_is_every_percentile_and_no_samples_have_no_summary() {
    let s = stats::summarize(&[2500]).unwrap();
    assert_eq!(
        (s.median_us, s.p95_us, s.p99_us, s.mean_us),
        (2.5, 2.5, 2.5, 2.5)
    );
    assert!(stats::summarize(&[]).is_none());
}
