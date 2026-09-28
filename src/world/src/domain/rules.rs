//! Cifras del juego que usa el simulador (D-01).
//!
//! TODO(catalog): cargar estas cifras con `CatalogService.GetCatalog` al
//! arrancar y al reiniciar el valle (CLAUDE.md, regla 8). CatalogService
//! todavía no existe, así que por ahora son constantes con los valores de
//! arranque de D-01.

/// Cifras que el simulador aplica en cada tick.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Rules {
    /// Versión del catálogo que se guarda en cada instantánea. Vacía hasta
    /// que exista CatalogService.
    pub catalog_version: String,
    /// Ancho y alto del valle en casillas.
    pub width: u32,
    pub height: u32,
    /// Litros que cuesta una gota fuera del modo demo.
    pub drop_water_cost: u64,
    /// Humedad que suma una gota fuera del modo demo.
    pub drop_humidity: u64,
    /// Humedad que suma una gota en modo demo.
    pub demo_drop_humidity: u64,
    /// Agua y capacidad del depósito con las que entra un jugador nuevo.
    pub initial_water: u64,
    pub initial_tank_capacity: u64,
    /// Ticks durante los que se recuerda un `action_id` aplicado (Capa 3).
    pub recent_actions_window_ticks: u64,
}

impl Rules {
    /// Cifras de arranque de D-01.
    pub fn initial() -> Self {
        Self {
            catalog_version: String::new(),
            width: 32,
            height: 32,
            drop_water_cost: 1,
            drop_humidity: 25,
            demo_drop_humidity: 1,
            initial_water: 20,
            initial_tank_capacity: 20,
            recent_actions_window_ticks: 600,
        }
    }
}

impl Default for Rules {
    fn default() -> Self {
        Self::initial()
    }
}
