//! Constantes de estructura del simulador.
//!
//! No son cifras de ajuste del juego (esas viven en `catalog/` y llegan en
//! el [`Catalog`](super::Catalog)): son la forma del mundo y del protocolo.

/// Ancho y alto del valle en casillas (D-01).
pub const VALLEY_WIDTH: u32 = 32;
pub const VALLEY_HEIGHT: u32 = 32;

/// Ticks durante los que se recuerda un `action_id` aplicado (Capa 3).
pub const RECENT_ACTIONS_WINDOW_TICKS: u64 = 600;

/// Ticks por segundo: el tick dura 100 ms (regla 3 de `CLAUDE.md`). Convierte
/// las cifras del catálogo expresadas en segundos a ticks sin decimales.
pub const TICKS_PER_SECOND: u64 = 10;

/// Topes de las cifras de terreno y presión urbana del catálogo: una cifra
/// mayor se rechaza al cargar el catálogo (la persistencia aplica los mismos).
pub const MAX_RIVER_WIDTH: u32 = 8;
pub const MAX_ROCK_COUNT: u32 = 1024;
pub const MAX_BUILDINGS: u32 = 1024;
