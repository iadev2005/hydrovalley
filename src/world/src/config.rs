//! Configuración por variables de entorno (regla 1 de portabilidad).

use std::net::SocketAddr;

pub const DEFAULT_LISTEN: &str = "0.0.0.0:50051";

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Config {
    /// `WORLD_LISTEN`: dirección y puerto del gRPC.
    pub listen: SocketAddr,
    /// `PERSISTENCE_ADDR`: `host:puerto` de PersistenceService.
    pub persistence_addr: String,
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
        Ok(Self {
            listen,
            persistence_addr,
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
}
