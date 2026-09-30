//! Cifras del juego que aplica el simulador (D-01), tal como las entrega
//! `CatalogService.GetCatalog`.
//!
//! Las cifras viven en `catalog/` y se ajustan sin tocar código: el
//! simulador las pide al arrancar y al reiniciar el valle, y las pasa al
//! [`World`](super::World). Aquí no hay valores por omisión: un mundo solo
//! existe con el catálogo que entregó la persistencia.
//!
//! Las unidades son las del contrato: litros, hojas y puntos de humedad;
//! los intervalos `_seconds` cuentan segundos y las duraciones `_ticks`,
//! ticks de 100 ms.

/// Catálogo completo, con el hash que lo identifica (`catalog_version`).
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Catalog {
    /// SHA-256 del contenido; va en cada instantánea.
    pub version: String,
    pub drop: DropRules,
    pub tank: TankRules,
    pub growth: GrowthRules,
    pub evaporation: EvaporationRules,
    pub fire: FireRules,
    pub sprinkler: SprinklerRules,
    pub drip: DripRules,
    pub treatment_plant: TreatmentPlantRules,
    /// Aumento de precio por cada compra del mismo tipo de instalación.
    pub installation_price_increase_percent: u32,
    pub bigger_drop: BiggerDropRules,
    pub bigger_tank: BiggerTankRules,
    /// Segundos que las instalaciones siguen trabajando tras la última
    /// acción de su dueño.
    pub offline_installation_seconds: u64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct DropRules {
    /// Litros que cuesta una gota fuera del modo demo.
    pub water_cost: u64,
    /// Humedad que suma una gota fuera del modo demo.
    pub humidity: u64,
    /// Humedad que suma una gota en modo demo (gratis y de una sola casilla).
    pub demo_humidity: u64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct TankRules {
    /// Capacidad inicial; un jugador nuevo entra con el depósito lleno.
    pub capacity: u64,
    pub refill_amount: u64,
    pub refill_interval_seconds: u32,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct GrowthRules {
    pub humidity_per_stage: u64,
    pub leaves_grass: u64,
    pub leaves_shrub: u64,
    pub leaves_flowers: u64,
    pub leaves_tree: u64,
    pub tree_max_humidity: u64,
}

impl GrowthRules {
    /// Hojas que gana quien hace llegar una casilla a `stage` (0 en tierra
    /// seca: nunca se llega a ella creciendo).
    pub fn leaves_for(&self, stage: super::TileStage) -> u64 {
        use super::TileStage::*;
        match stage {
            DrySoil => 0,
            Grass => self.leaves_grass,
            Shrub => self.leaves_shrub,
            Flowers => self.leaves_flowers,
            Tree => self.leaves_tree,
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct EvaporationRules {
    pub humidity_per_second: u64,
    pub wither_after_ticks: u32,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct FireRules {
    pub drops_to_extinguish: u32,
    pub extinguish_bonus_leaves: u64,
    pub burn_out_ticks: u32,
    /// Partes por millón por tick. 0 mientras D-01 no la fije.
    pub ignition_chance_ppm: u32,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SprinklerRules {
    pub price: u64,
    pub area_side: u32,
    pub humidity: u64,
    pub interval_seconds: u32,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct DripRules {
    pub price: u64,
    pub area_side: u32,
    pub evaporation_divisor: u32,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct TreatmentPlantRules {
    pub price: u64,
    pub refill_amount: u64,
    pub interval_seconds: u32,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct BiggerDropRules {
    pub price: u64,
    pub area_side: u32,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct BiggerTankRules {
    pub price: u64,
    pub extra_capacity: u64,
}

#[cfg(test)]
impl Catalog {
    /// Versión del catálogo de pruebas.
    pub const D01_VERSION: &'static str = "d01-fixture";

    /// Cifras de arranque de D-01, solo para las pruebas: en producción las
    /// entrega la persistencia.
    pub fn d01() -> Self {
        Self {
            version: Self::D01_VERSION.to_string(),
            drop: DropRules {
                water_cost: 1,
                humidity: 25,
                demo_humidity: 1,
            },
            tank: TankRules {
                capacity: 20,
                refill_amount: 1,
                refill_interval_seconds: 2,
            },
            growth: GrowthRules {
                humidity_per_stage: 100,
                leaves_grass: 1,
                leaves_shrub: 2,
                leaves_flowers: 4,
                leaves_tree: 8,
                tree_max_humidity: 100,
            },
            evaporation: EvaporationRules {
                humidity_per_second: 2,
                wither_after_ticks: 100,
            },
            fire: FireRules {
                drops_to_extinguish: 3,
                extinguish_bonus_leaves: 5,
                burn_out_ticks: 300,
                ignition_chance_ppm: 0,
            },
            sprinkler: SprinklerRules {
                price: 10,
                area_side: 3,
                humidity: 25,
                interval_seconds: 2,
            },
            drip: DripRules {
                price: 25,
                area_side: 5,
                evaporation_divisor: 2,
            },
            treatment_plant: TreatmentPlantRules {
                price: 40,
                refill_amount: 1,
                interval_seconds: 2,
            },
            installation_price_increase_percent: 15,
            bigger_drop: BiggerDropRules {
                price: 30,
                area_side: 3,
            },
            bigger_tank: BiggerTankRules {
                price: 20,
                extra_capacity: 10,
            },
            offline_installation_seconds: 7200,
        }
    }
}
