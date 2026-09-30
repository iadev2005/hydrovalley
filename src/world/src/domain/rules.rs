//! Constantes de estructura del simulador.
//!
//! No son cifras de ajuste del juego (esas viven en `catalog/` y llegan en
//! el [`Catalog`](super::Catalog)): son la forma del mundo y del protocolo.

/// Ancho y alto del valle en casillas (D-01).
pub const VALLEY_WIDTH: u32 = 32;
pub const VALLEY_HEIGHT: u32 = 32;

/// Ticks durante los que se recuerda un `action_id` aplicado (Capa 3).
pub const RECENT_ACTIONS_WINDOW_TICKS: u64 = 600;

/// Humedad que suma una gota en modo demo (D-01: 1, para verificar el total
/// a ojo).
///
/// TODO(catalog): `DropRules` de `catalog.proto` no tiene campo para esta
/// cifra; cuando lo tenga, pasará al catálogo.
pub const DEMO_DROP_HUMIDITY: u64 = 1;
