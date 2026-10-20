//! Prueba de conformidad: los mismos guiones dan el mismo estado en Rust y en
//! Go (`tests/conformance/` en la raíz del repositorio).
//!
//! Cada guion de `scenarios/*.json` (semilla, catálogo y comandos por tick) se
//! ejecuta con el simulador y su resultado se compara con el archivo de
//! `expected/`. Esos archivos los escribe Rust y los lee la prueba de
//! `src/world-go`, así que si el comportamiento de Rust cambia, esta prueba
//! falla hasta que se regeneren a propósito:
//!
//! ```text
//! CONFORMANCE_REGENERATE=1 cargo test --locked --test conformance
//! ```
//!
//! El ejecutor pasa por el codec (`catalog_from_proto`, `command_from_proto`,
//! `result_to_proto`, `world_state`, `encode_snapshot` y `decode_snapshot`),
//! igual que el servidor. No hay dependencias nuevas: el JSON se lee con un
//! analizador mínimo (solo enteros sin signo, cadenas, listas, objetos y
//! booleanos, que es lo que usan los guiones y `catalog/catalog.json`).

use std::collections::BTreeMap;
use std::fmt::Write as _;
use std::fs;
use std::path::{Path, PathBuf};

use prost::Message;
use world::codec;
use world::domain::{Catalog, Command, CommandKind, CommandOutcome, TileStage, World};
use world::proto::persistence::v1 as persistence_pb;
use world::proto::world::v1 as pb;

// ---- JSON mínimo ---------------------------------------------------------

#[derive(Debug, Clone, PartialEq)]
enum Json {
    Bool(bool),
    Num(u64),
    Str(String),
    Arr(Vec<Json>),
    Obj(Vec<(String, Json)>),
}

impl Json {
    fn parse(text: &str) -> Json {
        let mut parser = Parser {
            bytes: text.as_bytes(),
            at: 0,
        };
        let value = parser.value();
        parser.skip_space();
        assert_eq!(parser.at, parser.bytes.len(), "sobra texto tras el JSON");
        value
    }

    fn get(&self, key: &str) -> Option<&Json> {
        match self {
            Json::Obj(fields) => fields.iter().find(|(k, _)| k == key).map(|(_, v)| v),
            _ => None,
        }
    }

    fn field(&self, key: &str) -> &Json {
        self.get(key)
            .unwrap_or_else(|| panic!("falta el campo {key}"))
    }

    fn u64(&self) -> u64 {
        match self {
            Json::Num(n) => *n,
            other => panic!("se esperaba un entero y hay {other:?}"),
        }
    }

    fn u32(&self) -> u32 {
        u32::try_from(self.u64()).expect("cabe en 32 bits")
    }

    fn str(&self) -> &str {
        match self {
            Json::Str(s) => s,
            other => panic!("se esperaba una cadena y hay {other:?}"),
        }
    }

    fn bool(&self) -> bool {
        match self {
            Json::Bool(b) => *b,
            other => panic!("se esperaba un booleano y hay {other:?}"),
        }
    }

    fn arr(&self) -> &[Json] {
        match self {
            Json::Arr(items) => items,
            other => panic!("se esperaba una lista y hay {other:?}"),
        }
    }

    /// Sigue una ruta con puntos (`rules.fire.ignition_chance_ppm`).
    fn path(&self, path: &str) -> &Json {
        path.split('.').fold(self, |node, key| node.field(key))
    }

    /// Cambia el entero de una ruta que ya existe (un nombre mal escrito en un
    /// guion es un error, no un campo nuevo).
    fn set(&mut self, path: &str, value: u64) {
        let (parent, last) = match path.rsplit_once('.') {
            Some((parent, last)) => (Some(parent), last),
            None => (None, path),
        };
        let mut node = self;
        if let Some(parent) = parent {
            for key in parent.split('.') {
                node = match node {
                    Json::Obj(fields) => fields
                        .iter_mut()
                        .find(|(k, _)| k == key)
                        .map(|(_, v)| v)
                        .unwrap_or_else(|| panic!("la ruta {path} no existe en el catálogo")),
                    _ => panic!("la ruta {path} no existe en el catálogo"),
                };
            }
        }
        match node {
            Json::Obj(fields) => {
                let slot = fields
                    .iter_mut()
                    .find(|(k, _)| k == last)
                    .unwrap_or_else(|| panic!("la ruta {path} no existe en el catálogo"));
                assert!(matches!(slot.1, Json::Num(_)), "{path} no es un entero");
                slot.1 = Json::Num(value);
            }
            _ => panic!("la ruta {path} no existe en el catálogo"),
        }
    }
}

struct Parser<'a> {
    bytes: &'a [u8],
    at: usize,
}

impl Parser<'_> {
    fn skip_space(&mut self) {
        while self.at < self.bytes.len() && self.bytes[self.at].is_ascii_whitespace() {
            self.at += 1;
        }
    }

    fn eat(&mut self, byte: u8) {
        self.skip_space();
        assert_eq!(
            self.bytes.get(self.at),
            Some(&byte),
            "en el byte {}",
            self.at
        );
        self.at += 1;
    }

    fn peek(&mut self) -> u8 {
        self.skip_space();
        self.bytes[self.at]
    }

    fn value(&mut self) -> Json {
        match self.peek() {
            b'{' => {
                self.eat(b'{');
                let mut fields = Vec::new();
                if self.peek() == b'}' {
                    self.eat(b'}');
                    return Json::Obj(fields);
                }
                loop {
                    let key = self.string();
                    self.eat(b':');
                    fields.push((key, self.value()));
                    if self.peek() == b',' {
                        self.eat(b',');
                    } else {
                        self.eat(b'}');
                        return Json::Obj(fields);
                    }
                }
            }
            b'[' => {
                self.eat(b'[');
                let mut items = Vec::new();
                if self.peek() == b']' {
                    self.eat(b']');
                    return Json::Arr(items);
                }
                loop {
                    items.push(self.value());
                    if self.peek() == b',' {
                        self.eat(b',');
                    } else {
                        self.eat(b']');
                        return Json::Arr(items);
                    }
                }
            }
            b'"' => Json::Str(self.string()),
            b't' => self.literal("true", Json::Bool(true)),
            b'f' => self.literal("false", Json::Bool(false)),
            b'0'..=b'9' => {
                let start = self.at;
                while self.at < self.bytes.len() && self.bytes[self.at].is_ascii_digit() {
                    self.at += 1;
                }
                let text = std::str::from_utf8(&self.bytes[start..self.at]).unwrap();
                Json::Num(text.parse().expect("un entero de 64 bits sin signo"))
            }
            other => panic!("JSON no admitido en el byte {}: {}", self.at, other as char),
        }
    }

    fn literal(&mut self, word: &str, value: Json) -> Json {
        assert!(self.bytes[self.at..].starts_with(word.as_bytes()));
        self.at += word.len();
        value
    }

    fn string(&mut self) -> String {
        self.eat(b'"');
        let mut out = Vec::new();
        loop {
            let byte = self.bytes[self.at];
            self.at += 1;
            match byte {
                b'"' => return String::from_utf8(out).expect("UTF-8"),
                b'\\' => {
                    let escaped = self.bytes[self.at];
                    self.at += 1;
                    match escaped {
                        b'"' | b'\\' | b'/' => out.push(escaped),
                        b'n' => out.push(b'\n'),
                        b't' => out.push(b'\t'),
                        b'u' => {
                            let hex =
                                std::str::from_utf8(&self.bytes[self.at..self.at + 4]).unwrap();
                            self.at += 4;
                            let code = u32::from_str_radix(hex, 16).unwrap();
                            let ch = char::from_u32(code).expect("un carácter, no un sustituto");
                            let mut buffer = [0; 4];
                            out.extend_from_slice(ch.encode_utf8(&mut buffer).as_bytes());
                        }
                        other => panic!("escape no admitido: \\{}", other as char),
                    }
                }
                other => out.push(other),
            }
        }
    }
}

// ---- Guion ----------------------------------------------------------------

fn root() -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR")).join("../..")
}

fn conformance_dir() -> PathBuf {
    root().join("tests/conformance")
}

/// El catálogo de un guion: el archivo base con las cifras cambiadas
/// (`set`), convertido en el mensaje de `GetCatalog` y validado por el codec.
fn catalog_from_spec(spec: &Json) -> Catalog {
    let file = spec.field("file").str();
    let mut doc = Json::parse(&fs::read_to_string(root().join(file)).expect("catalog.json"));
    if let Some(Json::Obj(changes)) = spec.get("set") {
        for (path, value) in changes {
            doc.set(path, value.u64());
        }
    }
    codec::catalog_from_proto(catalog_message(&doc, spec.field("version").str()))
        .expect("el catálogo del guion es válido")
}

fn catalog_message(doc: &Json, version: &str) -> persistence_pb::Catalog {
    use persistence_pb as p;
    let n = |path: &str| doc.path(path).u64();
    let n32 = |path: &str| doc.path(path).u32();
    p::Catalog {
        catalog_version: version.to_string(),
        drop: Some(p::DropRules {
            water_cost: n("rules.drop.water_cost_liters"),
            humidity: n("rules.drop.humidity_points"),
            demo_humidity: n("rules.drop.demo_humidity_points"),
        }),
        tank: Some(p::TankRules {
            capacity: n("rules.tank.capacity_liters"),
            refill_amount: n("rules.tank.refill_liters"),
            refill_interval_seconds: n32("rules.tank.refill_interval_seconds"),
        }),
        growth: Some(p::GrowthRules {
            humidity_per_stage: n("rules.growth.humidity_per_stage_points"),
            leaves_grass: n("rules.growth.leaves_grass"),
            leaves_shrub: n("rules.growth.leaves_shrub"),
            leaves_flowers: n("rules.growth.leaves_flowers"),
            leaves_tree: n("rules.growth.leaves_tree"),
            tree_max_humidity: n("rules.growth.tree_max_humidity_points"),
        }),
        evaporation: Some(p::EvaporationRules {
            humidity_per_second: n("rules.evaporation.humidity_per_second_points"),
            wither_after_ticks: n32("rules.evaporation.wither_after_ticks"),
        }),
        fire: Some(p::FireRules {
            drops_to_extinguish: n32("rules.fire.drops_to_extinguish"),
            extinguish_bonus_leaves: n("rules.fire.extinguish_bonus_leaves"),
            burn_out_ticks: n32("rules.fire.burn_out_ticks"),
            ignition_chance_ppm: n32("rules.fire.ignition_chance_ppm"),
        }),
        sprinkler: Some(p::SprinklerRules {
            price: n("installations.sprinkler.price_leaves"),
            area_side: n32("installations.sprinkler.area_side_tiles"),
            humidity: n("installations.sprinkler.humidity_points"),
            interval_seconds: n32("installations.sprinkler.interval_seconds"),
        }),
        drip: Some(p::DripRules {
            price: n("installations.drip.price_leaves"),
            area_side: n32("installations.drip.area_side_tiles"),
            evaporation_divisor: n32("installations.drip.evaporation_divisor"),
        }),
        treatment_plant: Some(p::TreatmentPlantRules {
            price: n("installations.treatment_plant.price_leaves"),
            refill_amount: n("installations.treatment_plant.refill_liters"),
            interval_seconds: n32("installations.treatment_plant.interval_seconds"),
        }),
        installation_price_increase_percent: n32("rules.installation_price_increase_percent"),
        bigger_drop: Some(p::BiggerDropRules {
            price: n("upgrades.bigger_drop.price_leaves"),
            area_side: n32("upgrades.bigger_drop.area_side_tiles"),
        }),
        bigger_tank: Some(p::BiggerTankRules {
            price: n("upgrades.bigger_tank.price_leaves"),
            extra_capacity: n("upgrades.bigger_tank.extra_capacity_liters"),
        }),
        offline_installation_seconds: n("rules.offline_installation_seconds"),
    }
}

/// Un guion leído del disco.
struct Scenario {
    name: String,
    seed: u64,
    catalog: Catalog,
    reset_catalog: Option<Catalog>,
    total_ticks: u64,
    restore_at: Vec<u64>,
    /// Comandos por tick (los ticks sin entrada avanzan sin comandos).
    commands: BTreeMap<u64, Vec<pb::Command>>,
}

fn load_scenario(path: &Path) -> Scenario {
    let doc = Json::parse(&fs::read_to_string(path).expect("el guion"));
    let players: BTreeMap<String, String> = match doc.field("players") {
        Json::Obj(fields) => fields
            .iter()
            .map(|(name, id)| (name.clone(), id.str().to_string()))
            .collect(),
        other => panic!("players no es un objeto: {other:?}"),
    };
    let mut commands = BTreeMap::new();
    for entry in doc.field("ticks").arr() {
        let tick = entry.field("tick").u64();
        let list: Vec<pb::Command> = entry
            .field("commands")
            .arr()
            .iter()
            .map(|command| command_message(command, &players))
            .collect();
        assert!(
            commands.insert(tick, list).is_none(),
            "tick repetido: {tick}"
        );
    }
    Scenario {
        name: path.file_stem().unwrap().to_string_lossy().into_owned(),
        seed: doc.field("seed").u64(),
        catalog: catalog_from_spec(doc.field("catalog")),
        reset_catalog: doc.get("reset_catalog").map(catalog_from_spec),
        total_ticks: doc.field("total_ticks").u64(),
        restore_at: doc
            .field("restore_at")
            .arr()
            .iter()
            .map(Json::u64)
            .collect(),
        commands,
    }
}

fn command_message(json: &Json, players: &BTreeMap<String, String>) -> pb::Command {
    use pb::command::Kind;
    let player = json.field("player").str();
    let action = match json.field("action") {
        Json::Num(n) => format!("00000000-0000-4000-8000-{n:012}"),
        Json::Str(text) => text.clone(),
        other => panic!("action no es un número ni una cadena: {other:?}"),
    };
    let installation = |node: &Json| match node.get("kind") {
        None => 0,
        Some(Json::Num(n)) => i32::try_from(*n).unwrap(),
        Some(Json::Str(name)) => match name.as_str() {
            "sprinkler" => pb::InstallationKind::Sprinkler as i32,
            "drip" => pb::InstallationKind::Drip as i32,
            "treatment_plant" => pb::InstallationKind::TreatmentPlant as i32,
            other => panic!("tipo de instalación desconocido en el guion: {other}"),
        },
        Some(other) => panic!("kind no válido: {other:?}"),
    };
    let upgrade = |node: &Json| match node.get("kind") {
        None => 0,
        Some(Json::Num(n)) => i32::try_from(*n).unwrap(),
        Some(Json::Str(name)) => match name.as_str() {
            "bigger_drop" => pb::UpgradeKind::BiggerDrop as i32,
            "bigger_tank" => pb::UpgradeKind::BiggerTank as i32,
            other => panic!("tipo de mejora desconocido en el guion: {other}"),
        },
        Some(other) => panic!("kind no válido: {other:?}"),
    };
    let kind = if let Some(node) = json.get("drop") {
        Some(Kind::Drop(pb::DropCommand {
            x: node.field("x").u32(),
            y: node.field("y").u32(),
        }))
    } else if let Some(node) = json.get("place_installation") {
        Some(Kind::PlaceInstallation(pb::PlaceInstallationCommand {
            kind: installation(node),
            x: node.field("x").u32(),
            y: node.field("y").u32(),
        }))
    } else if let Some(node) = json.get("buy_upgrade") {
        Some(Kind::BuyUpgrade(pb::BuyUpgradeCommand {
            kind: upgrade(node),
        }))
    } else if let Some(node) = json.get("set_demo_mode") {
        Some(Kind::SetDemoMode(pb::SetDemoModeCommand {
            enabled: node.field("enabled").bool(),
        }))
    } else if json.get("reset_valley").is_some() {
        Some(Kind::ResetValley(pb::ResetValleyCommand {}))
    } else {
        None
    };
    pb::Command {
        action_id: action,
        player_id: players.get(player).cloned().unwrap_or(player.to_string()),
        kind,
    }
}

// ---- Ejecución ------------------------------------------------------------

/// FNV-1a de 64 bits: huella corta de los bytes de una instantánea.
fn fnv1a(bytes: &[u8]) -> String {
    let mut hash: u64 = 0xcbf2_9ce4_8422_2325;
    for byte in bytes {
        hash ^= u64::from(*byte);
        hash = hash.wrapping_mul(0x0000_0100_0000_01b3);
    }
    format!("{hash:016x}")
}

fn hex(bytes: &[u8]) -> String {
    let mut out = String::with_capacity(bytes.len() * 2);
    for byte in bytes {
        write!(out, "{byte:02x}").unwrap();
    }
    out
}

/// Todo lo que se compara entre implementaciones.
#[derive(Debug, PartialEq)]
struct Output {
    scenario: String,
    total_ticks: u64,
    final_tick: u64,
    humidity_contributed: Vec<u64>,
    burning_tiles: Vec<usize>,
    /// Fuegos que se acabaron con la planta en pie (gotas o aspersor).
    fires_put_out: u64,
    /// Fuegos que se acabaron con la casilla vuelta tierra seca (consumidos
    /// tras 300 ticks o borrados por un reinicio).
    fires_gone_to_dry_soil: u64,
    /// (tick, resultado de cada comando), solo de los ticks con comandos.
    results: Vec<(u64, Vec<String>)>,
    /// (tick, huella de la fila de instantánea, huella del WorldState) en los
    /// ticks de instantánea (`tick % 50 == 0`).
    checkpoints: Vec<(u64, String, String)>,
    /// (tick, fila `Snapshot` en hexadecimal) en cada restauración del guion.
    restores: Vec<(u64, String)>,
    final_snapshot_hex: String,
    final_world_state_hex: String,
}

fn result_code(result: &pb::CommandResult) -> String {
    let status = result
        .status()
        .as_str_name()
        .trim_start_matches("COMMAND_STATUS_")
        .to_lowercase();
    if result.status() == pb::CommandStatus::Rejected {
        let reason = result
            .reason()
            .as_str_name()
            .trim_start_matches("REJECT_REASON_")
            .to_lowercase();
        format!("{status}:{reason}")
    } else {
        status
    }
}

fn run(scenario: &Scenario, restore_at: &[u64]) -> Output {
    let mut world = World::with_seed(scenario.catalog.clone(), scenario.seed);
    // El catálogo vigente al restaurar: el del guion, o el ofrecido al
    // reinicio una vez que un reinicio se ha aplicado.
    let mut current = scenario.catalog.clone();
    let mut output = Output {
        scenario: scenario.name.clone(),
        total_ticks: scenario.total_ticks,
        final_tick: 0,
        humidity_contributed: Vec::new(),
        burning_tiles: Vec::new(),
        fires_put_out: 0,
        fires_gone_to_dry_soil: 0,
        results: Vec::new(),
        checkpoints: Vec::new(),
        restores: Vec::new(),
        final_snapshot_hex: String::new(),
        final_world_state_hex: String::new(),
    };
    let mut was_burning = vec![false; world.tiles().len()];
    for tick in 1..=scenario.total_ticks {
        let messages = scenario.commands.get(&tick).cloned().unwrap_or_default();
        let commands: Vec<Command> = messages
            .iter()
            .cloned()
            .map(codec::command_from_proto)
            .collect();
        let outcome = world.step_with_reset_catalog(&commands, scenario.reset_catalog.clone());
        assert_eq!(outcome.tick, tick);
        if let Some(offered) = &scenario.reset_catalog {
            let reset_applied = commands
                .iter()
                .zip(&outcome.results)
                .any(|(command, result)| {
                    matches!(command.kind, Some(CommandKind::ResetValley))
                        && result.outcome == CommandOutcome::Applied
                });
            if reset_applied {
                current = offered.clone();
            }
        }
        if !outcome.results.is_empty() {
            let codes = outcome
                .results
                .into_iter()
                .map(|result| result_code(&codec::result_to_proto(result)))
                .collect();
            output.results.push((tick, codes));
        }
        for (burning, tile) in was_burning.iter_mut().zip(world.tiles()) {
            if *burning && tile.fire.is_none() {
                if tile.stage == TileStage::DrySoil {
                    output.fires_gone_to_dry_soil += 1;
                } else {
                    output.fires_put_out += 1;
                }
            }
            *burning = tile.fire.is_some();
        }
        output
            .humidity_contributed
            .push(world.humidity_contributed());
        output
            .burning_tiles
            .push(world.tiles().iter().filter(|t| t.fire.is_some()).count());
        if tick % 50 == 0 {
            let snapshot = codec::encode_snapshot(&world).encode_to_vec();
            let state = codec::world_state(&world).encode_to_vec();
            output
                .checkpoints
                .push((tick, fnv1a(&snapshot), fnv1a(&state)));
        }
        if restore_at.contains(&tick) {
            let row = codec::encode_snapshot(&world);
            output.restores.push((tick, hex(&row.encode_to_vec())));
            world = codec::decode_snapshot(row, current.clone()).expect("la instantánea se lee");
        }
    }
    output.final_tick = world.tick();
    output.final_snapshot_hex = hex(&codec::encode_snapshot(&world).encode_to_vec());
    output.final_world_state_hex = hex(&codec::world_state(&world).encode_to_vec());
    output
}

fn numbers<T: std::fmt::Display>(values: &[T]) -> String {
    let items: Vec<String> = values.iter().map(ToString::to_string).collect();
    format!("[{}]", items.join(","))
}

/// El archivo esperado: un campo por línea, y una línea por tick con
/// comandos, para que un cambio se lea bien en un diff.
fn to_json(output: &Output) -> String {
    let mut out = String::from("{\n");
    writeln!(out, "  \"scenario\": \"{}\",", output.scenario).unwrap();
    writeln!(out, "  \"total_ticks\": {},", output.total_ticks).unwrap();
    writeln!(out, "  \"final_tick\": {},", output.final_tick).unwrap();
    writeln!(
        out,
        "  \"humidity_contributed\": {},",
        numbers(&output.humidity_contributed)
    )
    .unwrap();
    writeln!(
        out,
        "  \"burning_tiles\": {},",
        numbers(&output.burning_tiles)
    )
    .unwrap();
    writeln!(out, "  \"fires_put_out\": {},", output.fires_put_out).unwrap();
    writeln!(
        out,
        "  \"fires_gone_to_dry_soil\": {},",
        output.fires_gone_to_dry_soil
    )
    .unwrap();
    out.push_str("  \"results\": [\n");
    let lines: Vec<String> = output
        .results
        .iter()
        .map(|(tick, codes)| {
            let codes: Vec<String> = codes.iter().map(|c| format!("\"{c}\"")).collect();
            format!("    {{\"tick\":{tick},\"r\":[{}]}}", codes.join(","))
        })
        .collect();
    out.push_str(&lines.join(",\n"));
    out.push_str("\n  ],\n  \"checkpoints\": [\n");
    let lines: Vec<String> = output
        .checkpoints
        .iter()
        .map(|(tick, snapshot, state)| {
            format!(
                "    {{\"tick\":{tick},\"snapshot_fnv1a\":\"{snapshot}\",\"world_state_fnv1a\":\"{state}\"}}"
            )
        })
        .collect();
    out.push_str(&lines.join(",\n"));
    out.push_str("\n  ],\n  \"restores\": [\n");
    let lines: Vec<String> = output
        .restores
        .iter()
        .map(|(tick, snapshot)| format!("    {{\"tick\":{tick},\"snapshot_hex\":\"{snapshot}\"}}"))
        .collect();
    out.push_str(&lines.join(",\n"));
    out.push_str("\n  ],\n");
    writeln!(
        out,
        "  \"final_snapshot_hex\": \"{}\",",
        output.final_snapshot_hex
    )
    .unwrap();
    writeln!(
        out,
        "  \"final_world_state_hex\": \"{}\"",
        output.final_world_state_hex
    )
    .unwrap();
    out.push_str("}\n");
    out
}

fn scenario_files() -> Vec<PathBuf> {
    let mut files: Vec<PathBuf> = fs::read_dir(conformance_dir().join("scenarios"))
        .expect("tests/conformance/scenarios")
        .map(|entry| entry.unwrap().path())
        .filter(|path| path.extension().is_some_and(|ext| ext == "json"))
        .collect();
    files.sort();
    files
}

// ---- Pruebas --------------------------------------------------------------

/// Los archivos esperados son los que da Rust ahora. Con
/// `CONFORMANCE_REGENERATE=1` se reescriben en vez de comprobarse.
#[test]
fn the_committed_expected_outputs_are_what_rust_produces() {
    let regenerate = std::env::var_os("CONFORMANCE_REGENERATE").is_some();
    let expected_dir = conformance_dir().join("expected");
    let files = scenario_files();
    assert!(
        files.len() >= 4,
        "faltan guiones en tests/conformance/scenarios"
    );
    let mut stale = Vec::new();
    for path in &files {
        let scenario = load_scenario(path);
        let produced = to_json(&run(&scenario, &scenario.restore_at));
        let expected_path = expected_dir.join(format!("{}.json", scenario.name));
        if regenerate {
            fs::create_dir_all(&expected_dir).unwrap();
            fs::write(&expected_path, &produced).unwrap();
            continue;
        }
        match fs::read_to_string(&expected_path) {
            Ok(committed) if committed == produced => {}
            Ok(_) => stale.push(format!("{}: el resultado cambió", scenario.name)),
            Err(error) => stale.push(format!("{}: no se pudo leer ({error})", scenario.name)),
        }
    }
    assert!(
        stale.is_empty(),
        "los resultados esperados de tests/conformance/expected no coinciden con Rust:\n  {}\n\
         Si el cambio es a propósito, regenéralos con:\n  \
         CONFORMANCE_REGENERATE=1 cargo test --locked --test conformance\n\
         y revisa el diff antes de confirmarlo (Go los comprueba también).",
        stale.join("\n  ")
    );
}

/// Restaurar desde una instantánea a mitad del guion no cambia el resultado:
/// lo mismo que sin restaurar, en cada guion.
#[test]
fn restoring_from_a_snapshot_never_changes_the_outcome() {
    for path in scenario_files() {
        let scenario = load_scenario(&path);
        let straight = run(&scenario, &[]);
        // Con más restauraciones que las del guion, para no depender de ellas.
        let every_25: Vec<u64> = (25..scenario.total_ticks).step_by(25).collect();
        let restored = run(&scenario, &every_25);
        assert_eq!(restored.restores.len(), every_25.len(), "{}", scenario.name);
        assert_eq!(restored.results, straight.results, "{}", scenario.name);
        assert_eq!(
            restored.humidity_contributed, straight.humidity_contributed,
            "{}",
            scenario.name
        );
        assert_eq!(
            restored.burning_tiles, straight.burning_tiles,
            "{}",
            scenario.name
        );
        assert_eq!(
            restored.fires_put_out, straight.fires_put_out,
            "{}",
            scenario.name
        );
        assert_eq!(
            restored.fires_gone_to_dry_soil, straight.fires_gone_to_dry_soil,
            "{}",
            scenario.name
        );
        assert_eq!(
            restored.checkpoints, straight.checkpoints,
            "{}",
            scenario.name
        );
        assert_eq!(
            restored.final_snapshot_hex, straight.final_snapshot_hex,
            "{}",
            scenario.name
        );
        assert_eq!(
            restored.final_world_state_hex, straight.final_world_state_hex,
            "{}",
            scenario.name
        );
    }
}

/// Los guiones tienen que ejercitar de verdad las reglas: si un cambio en un
/// guion o en las cifras deja de producir fuegos, compras o reinicios, esta
/// prueba lo dice en vez de dar por buena una conformidad vacía.
#[test]
fn the_scenarios_exercise_the_rules_they_claim_to() {
    let mut codes = BTreeMap::<String, u64>::new();
    let mut max_burning = 0;
    let (mut put_out, mut gone) = (0, 0);
    for path in scenario_files() {
        let scenario = load_scenario(&path);
        let output = run(&scenario, &[]);
        assert!(output.total_ticks >= 300, "{}", scenario.name);
        for (_, results) in &output.results {
            for code in results {
                *codes.entry(code.clone()).or_default() += 1;
            }
        }
        max_burning = max_burning.max(output.burning_tiles.iter().copied().max().unwrap_or(0));
        put_out += output.fires_put_out;
        gone += output.fires_gone_to_dry_soil;
    }
    for code in [
        "applied",
        "duplicate",
        "rejected:insufficient_water",
        "rejected:insufficient_leaves",
        "rejected:out_of_bounds",
        "rejected:tile_occupied",
        "rejected:upgrade_already_owned",
        "rejected:invalid_command",
    ] {
        assert!(
            codes.contains_key(code),
            "ningún guion produce {code}: {codes:?}"
        );
    }
    assert!(
        max_burning >= 3,
        "los guiones apenas producen incendios: {max_burning}"
    );
    assert!(put_out >= 3, "ningún guion apaga incendios: {put_out}");
    assert!(gone >= 3, "ningún guion consume incendios: {gone}");
}
