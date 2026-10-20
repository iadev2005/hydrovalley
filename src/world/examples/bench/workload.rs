//! Carga de trabajo de la comparativa y su generador de comandos.
//!
//! La carga se define una vez, en `tests/bench/workloads/*.json`, y Rust y Go
//! la leen y generan los mismos comandos (lo comprueba una huella fija en las
//! pruebas de los dos lados). El algoritmo está descrito en
//! `tests/bench/README.md`.

use world::domain::rng::SplitMix64;
use world::domain::{Command, CommandKind, InstallationKind};

use crate::json_catalog::Json;

/// Una carga: tamaño del valle, jugadores, duración y comandos por tick.
#[derive(Debug, Clone, PartialEq)]
pub struct Workload {
    pub name: String,
    pub seed: u64,
    pub width: u32,
    pub height: u32,
    pub players: u32,
    pub warmup_ticks: u64,
    pub measured_ticks: u64,
    pub drops_per_1000_tiles: u64,
    pub hot_percent: u64,
    pub installations_per_1000_tiles: u64,
    /// Catálogo base, relativo a la raíz del repositorio.
    pub catalog_file: String,
    /// Cifras del catálogo que cambia la carga (ruta con puntos y valor).
    pub catalog_set: Vec<(String, u64)>,
}

impl Workload {
    pub fn from_json(text: &str) -> Result<Self, String> {
        let doc = std::panic::catch_unwind(|| Json::parse(text))
            .map_err(|_| "la carga no es JSON válido".to_string())?;
        let number = |key: &str| match doc.get(key) {
            Some(Json::Num(n)) => Ok(*n),
            _ => Err(format!("falta el entero `{key}`")),
        };
        let text_field = |key: &str| match doc.get(key) {
            Some(Json::Str(s)) => Ok(s.clone()),
            _ => Err(format!("falta la cadena `{key}`")),
        };
        let small = |key: &str| {
            u32::try_from(number(key)?).map_err(|_| format!("`{key}` no cabe en 32 bits"))
        };
        let catalog_set = match doc.get("catalog_set") {
            Some(Json::Obj(fields)) => fields
                .iter()
                .map(|(path, value)| match value {
                    Json::Num(n) => Ok((path.clone(), *n)),
                    _ => Err(format!("`catalog_set.{path}` no es un entero")),
                })
                .collect::<Result<Vec<_>, _>>()?,
            _ => return Err("falta el objeto `catalog_set`".to_string()),
        };
        let workload = Self {
            name: text_field("name")?,
            seed: number("seed")?,
            width: small("width")?,
            height: small("height")?,
            players: small("players")?,
            warmup_ticks: number("warmup_ticks")?,
            measured_ticks: number("measured_ticks")?,
            drops_per_1000_tiles: number("drops_per_1000_tiles")?,
            hot_percent: number("hot_percent")?,
            installations_per_1000_tiles: number("installations_per_1000_tiles")?,
            catalog_file: text_field("catalog_file")?,
            catalog_set,
        };
        if workload.width < 2 || workload.height < 2 {
            return Err("el valle mide al menos 2x2".to_string());
        }
        if workload.players == 0 {
            return Err("hace falta al menos un jugador".to_string());
        }
        if workload.hot_percent > 100 {
            return Err("`hot_percent` va de 0 a 100".to_string());
        }
        if workload.warmup_ticks < 2 || workload.measured_ticks == 0 {
            return Err("hace falta calentamiento (2 ticks o más) y ticks medidos".to_string());
        }
        Ok(workload)
    }

    fn area(&self) -> u64 {
        u64::from(self.width) * u64::from(self.height)
    }

    /// Gotas por tick: crecen con el área del valle.
    pub fn drops_per_tick(&self) -> u64 {
        self.area() * self.drops_per_1000_tiles / 1000
    }

    /// Instalaciones que se colocan durante el calentamiento.
    pub fn installations_total(&self) -> u64 {
        self.area() * self.installations_per_1000_tiles / 1000
    }

    /// Primer tick y duración de la ventana de colocación: la segunda mitad
    /// del calentamiento, cuando los jugadores ya tienen hojas.
    fn placement_window(&self) -> (u64, u64) {
        let half = self.warmup_ticks / 2;
        (half + 1, self.warmup_ticks - half)
    }
}

/// Generador de comandos. Hay que pedir los ticks en orden (1, 2, 3...): el
/// azar y el contador de acciones avanzan con cada llamada.
pub struct Generator<'a> {
    workload: &'a Workload,
    rng: SplitMix64,
    next_action: u64,
    players: Vec<String>,
}

impl<'a> Generator<'a> {
    pub fn new(workload: &'a Workload) -> Self {
        Self {
            workload,
            // Distinto del generador del mundo (que usa `seed` tal cual).
            rng: SplitMix64::new(workload.seed ^ 0x9E37_79B9_7F4A_7C15),
            next_action: 1,
            players: (0..workload.players)
                .map(|n| format!("00000000-0000-4000-8001-{n:012}"))
                .collect(),
        }
    }

    fn below(&mut self, bound: u64) -> u64 {
        self.rng.next_u64() % bound
    }

    fn command(&mut self, player: u64, kind: CommandKind) -> Command {
        let action_id = format!("00000000-0000-4000-8000-{:012}", self.next_action);
        self.next_action += 1;
        Command {
            action_id,
            player_id: self.players[player as usize].clone(),
            kind: Some(kind),
        }
    }

    /// Comandos del tick `tick`: primero las colocaciones que toquen y
    /// después las gotas.
    pub fn commands(&mut self, tick: u64) -> Vec<Command> {
        let workload = self.workload;
        let (width, height) = (u64::from(workload.width), u64::from(workload.height));
        let mut commands = Vec::new();

        let (first, span) = workload.placement_window();
        if (first..first + span).contains(&tick) {
            let mut index = tick - first;
            while index < workload.installations_total() {
                let player = self.below(u64::from(workload.players));
                let (x, y) = (self.below(width), self.below(height));
                let kind = match index % 3 {
                    0 => InstallationKind::Sprinkler,
                    1 => InstallationKind::Drip,
                    _ => InstallationKind::TreatmentPlant,
                };
                let kind = CommandKind::PlaceInstallation {
                    kind,
                    x: x as u32,
                    y: y as u32,
                };
                commands.push(self.command(player, kind));
                index += span;
            }
        }

        for _ in 0..workload.drops_per_tick() {
            // Siempre cuatro sorteos por gota, caiga donde caiga.
            let player = self.below(u64::from(workload.players));
            let hot = self.below(100) < workload.hot_percent;
            let (raw_x, raw_y) = (self.rng.next_u64(), self.rng.next_u64());
            let (x, y) = if hot {
                (
                    width / 4 + raw_x % (width / 2),
                    height / 4 + raw_y % (height / 2),
                )
            } else {
                (raw_x % width, raw_y % height)
            };
            let kind = CommandKind::Drop {
                x: x as u32,
                y: y as u32,
            };
            commands.push(self.command(player, kind));
        }
        commands
    }
}
