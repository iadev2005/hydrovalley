//! Reglas de una casilla que arde (diagrama 05, región «Fuego»).
//!
//! Funciones puras sobre una [`Tile`], sin azar: el sorteo de la ignición lo
//! hace el [`World`](super::World) con su generador y solo llama a
//! [`ignite`] cuando sale. Mientras arde, el crecimiento y el marchitamiento
//! quedan congelados (ver `growth`), pero la evaporación sigue.

use super::catalog::FireRules;
use super::tile::{Fire, Tile, TileStage};

/// ¿Puede arder? Casilla con planta, sin humedad y que no arde ya. Sirve igual
/// para la ignición al azar y para la propagación a vecinas.
pub fn is_eligible(tile: &Tile) -> bool {
    tile.stage != TileStage::DrySoil
        && !tile.stage.is_natural()
        && !tile.has_building
        && tile.trash_level == 0
        && tile.humidity == 0
        && tile.fire.is_none()
}

/// Enciende la casilla: empieza sin gotas y con la cuenta de ticks a 0.
pub fn ignite(tile: &mut Tile) {
    tile.fire = Some(Fire {
        drops_received: 0,
        burning_ticks: 0,
    });
}

/// Cuenta una gota recibida por una casilla ardiendo. Con la que completa
/// `drops_to_extinguish` el fuego se apaga y devuelve `true`; las gotas que
/// llegan después ya no encuentran fuego. Una casilla que no arde no cuenta
/// nada y devuelve `false`.
pub fn receive_drop(tile: &mut Tile, rules: &FireRules) -> bool {
    let Some(fire) = tile.fire.as_mut() else {
        return false;
    };
    fire.drops_received = fire.drops_received.saturating_add(1);
    if fire.drops_received >= rules.drops_to_extinguish {
        tile.fire = None;
        return true;
    }
    false
}

/// Apaga el fuego (aspersor). Devuelve `true` si ardía.
pub fn extinguish(tile: &mut Tile) -> bool {
    tile.fire.take().is_some()
}

/// Un tick más de fuego. Al llegar a `burn_out_ticks` se consume: el fuego
/// termina y la casilla vuelve a tierra seca desde cualquier etapa, con la
/// humedad y la instalación intactas. Devuelve `true` si se consumió.
pub fn burn(tile: &mut Tile, rules: &FireRules) -> bool {
    let Some(fire) = tile.fire.as_mut() else {
        return false;
    };
    fire.burning_ticks = fire.burning_ticks.saturating_add(1);
    if fire.burning_ticks < rules.burn_out_ticks {
        return false;
    }
    tile.fire = None;
    tile.stage = TileStage::DrySoil;
    tile.dry_ticks = 0;
    true
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::domain::Catalog;

    fn rules() -> FireRules {
        Catalog::d01().fire
    }

    fn grass(humidity: u64) -> Tile {
        Tile {
            stage: TileStage::Grass,
            humidity,
            ..Tile::default()
        }
    }

    #[test]
    fn eligibility_needs_a_plant_no_humidity_and_no_fire() {
        assert!(is_eligible(&grass(0)));
        assert!(!is_eligible(&grass(1)));
        assert!(!is_eligible(&Tile::default()));
        let mut burning = grass(0);
        ignite(&mut burning);
        assert!(!is_eligible(&burning));
    }

    #[test]
    fn the_third_drop_puts_the_fire_out_and_later_drops_find_none() {
        let mut t = grass(0);
        ignite(&mut t);
        assert!(!receive_drop(&mut t, &rules()));
        assert!(!receive_drop(&mut t, &rules()));
        assert!(receive_drop(&mut t, &rules()));
        assert_eq!(t.fire, None);
        assert!(!receive_drop(&mut t, &rules()));
    }

    #[test]
    fn burning_out_drops_the_tile_to_dry_soil_keeping_humidity_and_installation() {
        let mut t = Tile {
            humidity: 40,
            dry_ticks: 5,
            installation: Some(crate::domain::Installation {
                kind: crate::domain::InstallationKind::Drip,
                owner_id: "ana".into(),
                remaining_durability: 30,
            }),
            ..grass(0)
        };
        ignite(&mut t);
        for _ in 0..299 {
            assert!(!burn(&mut t, &rules()));
        }
        assert!(burn(&mut t, &rules()));
        assert_eq!(
            (t.stage, t.humidity, t.dry_ticks),
            (TileStage::DrySoil, 40, 0)
        );
        assert!(t.fire.is_none() && t.installation.is_some());
        assert!(
            !burn(&mut t, &rules()),
            "sin fuego no hay nada que consumir"
        );
    }

    #[test]
    fn extinguishing_reports_whether_it_was_burning() {
        let mut t = grass(0);
        assert!(!extinguish(&mut t));
        ignite(&mut t);
        assert!(extinguish(&mut t));
    }
}
