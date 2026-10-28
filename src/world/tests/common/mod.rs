//! Utilidades compartidas por las pruebas de integración.

use world::proto::persistence::v1 as p;

/// Versión del catálogo falso.
pub const CATALOG_VERSION: &str = "test-catalog-v1";

/// Un catálogo válido con las cifras de arranque de D-01. Solo para las
/// pruebas: en producción las entrega `CatalogService`.
pub fn catalog() -> p::Catalog {
    p::Catalog {
        catalog_version: CATALOG_VERSION.to_string(),
        drop: Some(p::DropRules {
            water_cost: 1,
            humidity: 25,
            demo_humidity: 1,
        }),
        tank: Some(p::TankRules {
            capacity: 20,
            refill_amount: 1,
            refill_interval_seconds: 2,
        }),
        growth: Some(p::GrowthRules {
            humidity_per_stage: 100,
            leaves_grass: 1,
            leaves_shrub: 2,
            leaves_flowers: 4,
            leaves_tree: 8,
            tree_max_humidity: 100,
        }),
        evaporation: Some(p::EvaporationRules {
            humidity_per_second: 2,
            wither_after_ticks: 100,
        }),
        fire: Some(p::FireRules {
            drops_to_extinguish: 3,
            extinguish_bonus_leaves: 5,
            burn_out_ticks: 300,
            ignition_chance_ppm: 0,
            propagation_chance_ppm: 0,
        }),
        sprinkler: Some(p::SprinklerRules {
            price: 10,
            area_side: 3,
            humidity: 25,
            interval_seconds: 2,
            durability: 30,
        }),
        drip: Some(p::DripRules {
            price: 25,
            area_side: 5,
            evaporation_divisor: 2,
            durability: 30,
        }),
        treatment_plant: Some(p::TreatmentPlantRules {
            price: 40,
            refill_amount: 1,
            interval_seconds: 2,
            durability: 30,
        }),
        installation_price_increase_percent: 15,
        bigger_drop: Some(p::BiggerDropRules {
            price: 30,
            area_side: 3,
        }),
        bigger_tank: Some(p::BiggerTankRules {
            price: 20,
            extra_capacity: 10,
        }),
        trash_bin: Some(p::TrashBinRules {
            price: 15,
            area_side: 3,
            interval_seconds: 2,
            durability: 30,
        }),
        offline_installation_seconds: 7200,
    }
}
