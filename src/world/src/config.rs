//! Configuración por variables de entorno (regla 1 de portabilidad).

use std::net::SocketAddr;

pub const DEFAULT_LISTEN: &str = "0.0.0.0:50051";

/// Modo de arranque del simulador (`WORLD_MODE`, B8 W2):
/// - `Active`: carga catálogo e instantánea al arrancar el proceso.
/// - `Standby`: arranca en espera sin cargar la instantánea hasta recibir su
///   primer `Step` de promoción (evita quedar con un `tick` viejo).
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub enum WorldMode {
    #[default]
    Active,
    Standby,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Config {
    /// `WORLD_LISTEN`: dirección y puerto del gRPC.
    pub listen: SocketAddr,
    /// `PERSISTENCE_ADDR`: `host:puerto` de PersistenceService.
    pub persistence_addr: String,
    /// `WORLD_SEED` (u64): semilla del azar de los incendios cuando el valle
    /// empieza de cero. Sin ella se deriva de la hora de arranque.
    pub world_seed: Option<u64>,
    /// `WORLD_MODE` (`active` o `standby`; por defecto `active`).
    pub mode: WorldMode,
}

/// Semilla derivada de la hora de arranque, para cuando no hay `WORLD_SEED`.
/// Quien la use debe registrarla para poder repetir la partida.
pub fn seed_from_clock() -> u64 {
    seed_from_nanos(
        std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .map_or(0, |elapsed| elapsed.as_nanos()),
    )
}

/// Pliega los nanosegundos desde la época Unix (128 bits) en 64 para no
/// perder el segundo ni el nanosegundo. Separada del reloj para probarla.
pub fn seed_from_nanos(nanos: u128) -> u64 {
    (nanos as u64) ^ ((nanos >> 64) as u64)
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum ConfigError {
    Missing(&'static str),
    Invalid { name: &'static str, value: String },
}

impl std::fmt::Display for ConfigError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Missing(name) => write!(f, "falta la variable de entorno {name}"),
            Self::Invalid { name, value } => write!(f, "{name} no es válida: {value:?}"),
        }
    }
}

impl std::error::Error for ConfigError {}

impl Config {
    pub fn from_env() -> Result<Self, ConfigError> {
        Self::from_lookup(|name| std::env::var(name).ok())
    }

    /// `lookup` devuelve el valor de una variable, si existe.
    pub fn from_lookup(lookup: impl Fn(&str) -> Option<String>) -> Result<Self, ConfigError> {
        let read = |name: &'static str| {
            lookup(name)
                .map(|v| v.trim().to_string())
                .filter(|v| !v.is_empty())
        };

        let listen_text = read("WORLD_LISTEN").unwrap_or_else(|| DEFAULT_LISTEN.to_string());
        let listen = listen_text.parse().map_err(|_| ConfigError::Invalid {
            name: "WORLD_LISTEN",
            value: listen_text.clone(),
        })?;
        let persistence_addr =
            read("PERSISTENCE_ADDR").ok_or(ConfigError::Missing("PERSISTENCE_ADDR"))?;
        let world_seed = read("WORLD_SEED")
            .map(|text| {
                text.parse::<u64>().map_err(|_| ConfigError::Invalid {
                    name: "WORLD_SEED",
                    value: text.clone(),
                })
            })
            .transpose()?;
        let mode = match read("WORLD_MODE").as_deref() {
            None => WorldMode::Active,
            Some(text) if text.eq_ignore_ascii_case("active") => WorldMode::Active,
            Some(text) if text.eq_ignore_ascii_case("standby") => WorldMode::Standby,
            Some(text) => {
                return Err(ConfigError::Invalid {
                    name: "WORLD_MODE",
                    value: text.to_string(),
                });
            }
        };
        Ok(Self {
            listen,
            persistence_addr,
            world_seed,
            mode,
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn env(pairs: &[(&str, &str)]) -> impl Fn(&str) -> Option<String> {
        let pairs: Vec<(String, String)> = pairs
            .iter()
            .map(|(k, v)| (k.to_string(), v.to_string()))
            .collect();
        move |name| {
            pairs
                .iter()
                .find(|(k, _)| k == name)
                .map(|(_, v)| v.clone())
        }
    }

    #[test]
    fn listen_defaults_to_all_interfaces_on_50051() {
        let config =
            Config::from_lookup(env(&[("PERSISTENCE_ADDR", "persistence:50051")])).unwrap();
        assert_eq!(config.listen, "0.0.0.0:50051".parse().unwrap());
        assert_eq!(config.persistence_addr, "persistence:50051");
    }

    #[test]
    fn listen_can_be_overridden() {
        let config = Config::from_lookup(env(&[
            ("WORLD_LISTEN", "127.0.0.1:6000"),
            ("PERSISTENCE_ADDR", "10.68.17.2:50051"),
        ]))
        .unwrap();
        assert_eq!(config.listen, "127.0.0.1:6000".parse().unwrap());
    }

    #[test]
    fn persistence_addr_is_required() {
        assert_eq!(
            Config::from_lookup(env(&[])).unwrap_err(),
            ConfigError::Missing("PERSISTENCE_ADDR")
        );
        assert_eq!(
            Config::from_lookup(env(&[("PERSISTENCE_ADDR", "  ")])).unwrap_err(),
            ConfigError::Missing("PERSISTENCE_ADDR")
        );
    }

    #[test]
    fn the_seed_is_optional_and_parsed_as_u64() {
        let base = [("PERSISTENCE_ADDR", "persistence:50051")];
        assert_eq!(Config::from_lookup(env(&base)).unwrap().world_seed, None);
        let with = |value| {
            Config::from_lookup(env(&[
                ("PERSISTENCE_ADDR", "persistence:50051"),
                ("WORLD_SEED", value),
            ]))
        };
        assert_eq!(with(" 42 ").unwrap().world_seed, Some(42));
        assert_eq!(
            with("18446744073709551615").unwrap().world_seed,
            Some(u64::MAX)
        );
        assert_eq!(with("").unwrap().world_seed, None, "vacía = sin definir");
        for bad in ["-1", "1.5", "abc", "18446744073709551616"] {
            assert_eq!(
                with(bad).unwrap_err(),
                ConfigError::Invalid {
                    name: "WORLD_SEED",
                    value: bad.to_string()
                }
            );
        }
    }

    #[test]
    fn the_clock_seed_folds_both_halves_of_the_nanoseconds_deterministically() {
        // Dos lecturas distintas (aunque solo difieran en 1 ns) dan semillas distintas.
        let base = 1_790_000_000_123_456_789u128;
        assert_ne!(seed_from_nanos(base), seed_from_nanos(base + 1));
        // La mitad alta también cuenta, y la misma entrada da la misma semilla.
        assert_ne!(seed_from_nanos(base), seed_from_nanos(base | (1u128 << 64)));
        assert_eq!(seed_from_nanos(base), seed_from_nanos(base));
        assert_eq!(seed_from_nanos(0), 0);
    }

    #[test]
    fn an_invalid_listen_address_is_reported() {
        let error = Config::from_lookup(env(&[
            ("WORLD_LISTEN", "world:50051"),
            ("PERSISTENCE_ADDR", "persistence:50051"),
        ]))
        .unwrap_err();
        assert_eq!(
            error,
            ConfigError::Invalid {
                name: "WORLD_LISTEN",
                value: "world:50051".to_string()
            }
        );
    }

    #[test]
    fn world_mode_defaults_to_active_and_accepts_standby() {
        let base = [("PERSISTENCE_ADDR", "persistence:50051")];
        assert_eq!(
            Config::from_lookup(env(&base)).unwrap().mode,
            WorldMode::Active
        );
        let with = |value| {
            Config::from_lookup(env(&[
                ("PERSISTENCE_ADDR", "persistence:50051"),
                ("WORLD_MODE", value),
            ]))
        };
        assert_eq!(with("active").unwrap().mode, WorldMode::Active);
        assert_eq!(with("STANDBY").unwrap().mode, WorldMode::Standby);
        assert_eq!(
            with("invalid").unwrap_err(),
            ConfigError::Invalid {
                name: "WORLD_MODE",
                value: "invalid".to_string()
            }
        );
    }
}
