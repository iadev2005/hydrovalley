//! Comparativa Go vs. Rust (D-03): mide el paso del dominio, `World::step`,
//! en un valle del tamaño que pida la carga.
//!
//! ```text
//! cargo run --release --locked --example bench -- ../../tests/bench/workloads/valley-256.json
//! cargo run --release --locked --example bench -- --csv-header
//! ```
//!
//! Imprime una fila CSV con las estadísticas por paso (µs) y la huella de la
//! instantánea final, que tiene que coincidir con la del ejecutor de Go. Solo
//! se cronometra `World::step`: la generación de comandos queda fuera. No hay
//! dependencias nuevas. Método y cautelas: `tests/bench/README.md`.

use std::fs;
use std::hint::black_box;
use std::path::{Path, PathBuf};
use std::process::ExitCode;
use std::time::Instant;

use prost::Message;
use world::codec;
use world::domain::{Catalog, CommandOutcome, TileStage, World};

#[path = "../../tests/common/json_catalog.rs"]
mod json_catalog;
mod stats;
mod workload;

use json_catalog::{Json, catalog_message};
use workload::{Generator, Workload};

const CSV_HEADER: &str = "impl,workload,width,height,players,warmup_ticks,measured_ticks,\
mean_us,median_us,p95_us,p99_us,min_us,max_us,\
applied,duplicate,rejected,installations,burning_tiles,max_burning_tiles,trees,state_fnv1a";

fn repo_root() -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR")).join("../..")
}

fn load_catalog(workload: &Workload) -> Result<Catalog, String> {
    let path = repo_root().join(&workload.catalog_file);
    let text = fs::read_to_string(&path).map_err(|e| format!("{}: {e}", path.display()))?;
    let mut doc = Json::parse(&text);
    for (path, value) in &workload.catalog_set {
        doc.set(path, *value);
    }
    codec::catalog_from_proto(catalog_message(&doc, "bench"))
        .map_err(|e| format!("catálogo inválido: {e:?}"))
}

fn fnv1a(bytes: &[u8]) -> u64 {
    bytes.iter().fold(0xcbf2_9ce4_8422_2325, |hash, &byte| {
        (hash ^ u64::from(byte)).wrapping_mul(0x0000_0100_0000_01b3)
    })
}

fn run(workload: &Workload) -> Result<String, String> {
    let catalog = load_catalog(workload)?;
    let mut world = World::with_size(catalog, workload.seed, workload.width, workload.height);
    let mut generator = Generator::new(workload);
    let (mut applied, mut duplicate, mut rejected) = (0u64, 0u64, 0u64);
    // Casillas que arden, muestreadas cada 50 ticks fuera del cronómetro.
    let mut max_burning = 0usize;
    let mut samples_ns = Vec::with_capacity(workload.measured_ticks as usize);

    for tick in 1..=workload.warmup_ticks + workload.measured_ticks {
        let commands = generator.commands(tick);
        let started = Instant::now();
        let outcome = world.step(&commands);
        let elapsed = started.elapsed();
        let outcome = black_box(outcome);
        if tick > workload.warmup_ticks {
            samples_ns.push(elapsed.as_nanos() as u64);
        }
        if tick % 50 == 0 {
            let burning = world.tiles().iter().filter(|t| t.fire.is_some()).count();
            max_burning = max_burning.max(burning);
        }
        for result in &outcome.results {
            match result.outcome {
                CommandOutcome::Applied => applied += 1,
                CommandOutcome::Duplicate => duplicate += 1,
                CommandOutcome::Rejected(_) => rejected += 1,
            }
        }
    }

    let stats = stats::summarize(&samples_ns).ok_or("no hay muestras")?;
    let installations = world
        .tiles()
        .iter()
        .filter(|t| t.installation.is_some())
        .count();
    let burning = world.tiles().iter().filter(|t| t.fire.is_some()).count();
    let trees = world
        .tiles()
        .iter()
        .filter(|t| t.stage == TileStage::Tree)
        .count();
    let snapshot = codec::encode_snapshot(&world).encode_to_vec();
    Ok(format!(
        "rust,{},{},{},{},{},{},{:.3},{:.3},{:.3},{:.3},{:.3},{:.3},{applied},{duplicate},\
{rejected},{installations},{burning},{max_burning},{trees},{:016x}",
        workload.name,
        workload.width,
        workload.height,
        workload.players,
        workload.warmup_ticks,
        workload.measured_ticks,
        stats.mean_us,
        stats.median_us,
        stats.p95_us,
        stats.p99_us,
        stats.min_us,
        stats.max_us,
        fnv1a(&snapshot),
    ))
}

fn main() -> ExitCode {
    let args: Vec<String> = std::env::args().skip(1).collect();
    if args.first().map(String::as_str) == Some("--csv-header") {
        println!("{CSV_HEADER}");
        return ExitCode::SUCCESS;
    }
    let [path] = args.as_slice() else {
        eprintln!("uso: bench <carga.json> | --csv-header");
        return ExitCode::from(2);
    };
    let result = fs::read_to_string(path)
        .map_err(|e| format!("{path}: {e}"))
        .and_then(|text| Workload::from_json(&text))
        .and_then(|workload| run(&workload));
    match result {
        Ok(row) => {
            println!("{row}");
            ExitCode::SUCCESS
        }
        Err(message) => {
            eprintln!("error: {message}");
            ExitCode::FAILURE
        }
    }
}
