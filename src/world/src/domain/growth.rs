//! Reglas de una casilla: crecimiento, evaporación y marchitamiento
//! (diagrama 05, región «Crecimiento»).
//!
//! Funciones puras sobre una [`Tile`], con enteros sin signo y sin azar. El
//! [`World`](super::World) decide cuándo llamarlas (ver el orden del tick en
//! su documentación) y reparte las hojas.

use super::catalog::{EvaporationRules, GrowthRules};
use super::tile::{Tile, TileStage};

/// Suma `amount` de humedad y hace crecer la casilla las veces que alcance.
/// Devuelve las hojas ganadas (la suma de las de cada etapa alcanzada).
///
/// Cada ascenso gasta `humidity_per_stage`. Con las cifras de D-01 una gota
/// sube como mucho una etapa; el bucle solo cubre un catálogo con cifras
/// que permitan más. En árbol la humedad queda limitada a
/// `tree_max_humidity`. Una casilla ardiendo guarda la humedad pero no
/// crece (guarda `[sin fuego]` del diagrama 05).
pub fn add_humidity(tile: &mut Tile, amount: u64, growth: &GrowthRules) -> u64 {
    tile.humidity = tile.humidity.saturating_add(amount);
    if tile.fire.is_some() {
        return 0;
    }
    let mut leaves = 0u64;
    while let Some(next) = tile.stage.next() {
        if tile.humidity < growth.humidity_per_stage {
            break;
        }
        tile.humidity -= growth.humidity_per_stage;
        tile.stage = next;
        tile.dry_ticks = 0;
        leaves = leaves.saturating_add(growth.leaves_for(next));
    }
    if tile.stage == TileStage::Tree {
        tile.humidity = tile.humidity.min(growth.tree_max_humidity);
    }
    leaves
}

/// Resta `amount` de humedad, sin bajar de 0. También una casilla ardiendo:
/// el fuego congela la etapa, no la evaporación (diagrama 05).
pub fn evaporate(tile: &mut Tile, amount: u64) {
    tile.humidity = tile.humidity.saturating_sub(amount);
}

/// Avanza un tick el marchitamiento. Devuelve `true` si la casilla bajó de
/// etapa.
///
/// `dry_ticks` cuenta los ticks seguidos con humedad 0 de una planta (tierra
/// seca no tiene qué marchitar); cualquier humedad lo reinicia. Al llegar a
/// `wither_after_ticks` baja una etapa y vuelve a contar desde 0. Una
/// casilla ardiendo queda congelada: ni cuenta ni se reinicia.
pub fn wither(tile: &mut Tile, rules: &EvaporationRules) -> bool {
    if tile.fire.is_some() {
        return false;
    }
    let Some(lower) = tile.stage.previous() else {
        tile.dry_ticks = 0;
        return false;
    };
    if tile.humidity > 0 {
        tile.dry_ticks = 0;
        return false;
    }
    tile.dry_ticks = tile.dry_ticks.saturating_add(1);
    if tile.dry_ticks < rules.wither_after_ticks {
        return false;
    }
    tile.stage = lower;
    tile.dry_ticks = 0;
    true
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::domain::{Catalog, Fire, TileStage};

    fn growth() -> GrowthRules {
        Catalog::d01().growth
    }

    fn evaporation() -> EvaporationRules {
        Catalog::d01().evaporation
    }

    fn tile(stage: TileStage, humidity: u64) -> Tile {
        Tile {
            stage,
            humidity,
            ..Tile::default()
        }
    }

    fn burning(mut tile: Tile) -> Tile {
        tile.fire = Some(Fire {
            drops_received: 0,
            burning_ticks: 0,
        });
        tile
    }

    #[test]
    fn humidity_below_the_threshold_only_accumulates() {
        let mut t = tile(TileStage::DrySoil, 50);
        let leaves = add_humidity(&mut t, 25, &growth());
        assert_eq!((t.stage, t.humidity, leaves), (TileStage::DrySoil, 75, 0));
    }

    #[test]
    fn reaching_the_threshold_grows_one_stage_and_spends_the_humidity() {
        let mut t = tile(TileStage::DrySoil, 75);
        let leaves = add_humidity(&mut t, 25, &growth());
        assert_eq!((t.stage, t.humidity, leaves), (TileStage::Grass, 0, 1));
    }

    #[test]
    fn the_leftover_above_the_threshold_stays_in_the_tile() {
        let mut t = tile(TileStage::Grass, 90);
        let leaves = add_humidity(&mut t, 25, &growth());
        assert_eq!((t.stage, t.humidity, leaves), (TileStage::Shrub, 15, 2));
    }

    #[test]
    fn every_stage_pays_its_own_leaves() {
        let expected = [
            (TileStage::DrySoil, TileStage::Grass, 1),
            (TileStage::Grass, TileStage::Shrub, 2),
            (TileStage::Shrub, TileStage::Flowers, 4),
            (TileStage::Flowers, TileStage::Tree, 8),
        ];
        for (from, to, leaves) in expected {
            let mut t = tile(from, 99);
            assert_eq!(add_humidity(&mut t, 1, &growth()), leaves, "{from:?}");
            assert_eq!(t.stage, to);
        }
    }

    #[test]
    fn a_huge_drop_climbs_several_stages_and_pays_each_one() {
        let mut t = tile(TileStage::DrySoil, 0);
        let leaves = add_humidity(&mut t, 250, &growth());
        assert_eq!((t.stage, t.humidity, leaves), (TileStage::Shrub, 50, 3));
    }

    #[test]
    fn a_tree_does_not_grow_and_its_humidity_is_capped() {
        let mut t = tile(TileStage::Tree, 90);
        let leaves = add_humidity(&mut t, 25, &growth());
        assert_eq!((t.stage, t.humidity, leaves), (TileStage::Tree, 100, 0));
    }

    #[test]
    fn growing_into_a_tree_caps_the_leftover_too() {
        let mut t = tile(TileStage::Flowers, 100);
        add_humidity(&mut t, 250, &growth());
        assert_eq!((t.stage, t.humidity), (TileStage::Tree, 100));
    }

    #[test]
    fn the_thresholds_come_from_the_catalog() {
        let mut rules = growth();
        rules.humidity_per_stage = 40;
        rules.leaves_grass = 5;
        let mut t = tile(TileStage::DrySoil, 30);
        assert_eq!(add_humidity(&mut t, 25, &rules), 5);
        assert_eq!((t.stage, t.humidity), (TileStage::Grass, 15));
    }

    #[test]
    fn a_burning_tile_takes_the_humidity_but_does_not_grow() {
        let mut t = burning(tile(TileStage::Grass, 90));
        let leaves = add_humidity(&mut t, 25, &growth());
        assert_eq!((t.stage, t.humidity, leaves), (TileStage::Grass, 115, 0));
    }

    #[test]
    fn growing_resets_the_dry_counter() {
        let mut t = tile(TileStage::Grass, 99);
        t.dry_ticks = 50;
        add_humidity(&mut t, 1, &growth());
        assert_eq!(t.dry_ticks, 0);
    }

    #[test]
    fn evaporation_subtracts_without_going_below_zero() {
        let mut t = tile(TileStage::Grass, 3);
        evaporate(&mut t, 2);
        assert_eq!(t.humidity, 1);
        evaporate(&mut t, 2);
        assert_eq!(t.humidity, 0);
    }

    #[test]
    fn a_burning_tile_still_evaporates() {
        let mut t = burning(tile(TileStage::Grass, 10));
        evaporate(&mut t, 2);
        assert_eq!(t.humidity, 8);
    }

    #[test]
    fn a_plant_withers_after_the_catalog_ticks_at_zero_humidity() {
        let rules = evaporation();
        let mut t = tile(TileStage::Shrub, 0);
        for _ in 0..rules.wither_after_ticks - 1 {
            assert!(!wither(&mut t, &rules));
        }
        assert_eq!((t.stage, t.dry_ticks), (TileStage::Shrub, 99));
        assert!(wither(&mut t, &rules));
        assert_eq!((t.stage, t.dry_ticks), (TileStage::Grass, 0));
    }

    #[test]
    fn every_stage_drops_one_stage_down_to_dry_soil() {
        let rules = evaporation();
        let mut t = tile(TileStage::Tree, 0);
        let mut seen = vec![t.stage];
        for _ in 0..4 * rules.wither_after_ticks {
            if wither(&mut t, &rules) {
                seen.push(t.stage);
            }
        }
        assert_eq!(
            seen,
            [
                TileStage::Tree,
                TileStage::Flowers,
                TileStage::Shrub,
                TileStage::Grass,
                TileStage::DrySoil
            ]
        );
    }

    #[test]
    fn any_humidity_resets_the_dry_counter() {
        let rules = evaporation();
        let mut t = tile(TileStage::Grass, 0);
        for _ in 0..60 {
            wither(&mut t, &rules);
        }
        assert_eq!(t.dry_ticks, 60);
        t.humidity = 1;
        assert!(!wither(&mut t, &rules));
        assert_eq!(t.dry_ticks, 0);
    }

    #[test]
    fn dry_soil_has_nothing_to_wither() {
        let rules = evaporation();
        let mut t = tile(TileStage::DrySoil, 0);
        for _ in 0..500 {
            assert!(!wither(&mut t, &rules));
        }
        assert_eq!((t.stage, t.dry_ticks), (TileStage::DrySoil, 0));
    }

    #[test]
    fn a_burning_tile_freezes_the_dry_counter() {
        let rules = evaporation();
        let mut t = burning(tile(TileStage::Grass, 0));
        t.dry_ticks = 98;
        for _ in 0..10 {
            assert!(!wither(&mut t, &rules));
        }
        assert_eq!((t.stage, t.dry_ticks), (TileStage::Grass, 98));
    }

    #[test]
    fn the_stage_ladder_is_consistent() {
        let mut stage = TileStage::DrySoil;
        while let Some(next) = stage.next() {
            assert_eq!(next.previous(), Some(stage));
            stage = next;
        }
        assert_eq!(stage, TileStage::Tree);
        assert_eq!(TileStage::DrySoil.previous(), None);
    }
}
