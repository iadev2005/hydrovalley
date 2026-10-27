//! Pruebas de los incendios (D-01, RF-28, RF-29, RF-30, CP-04, CP-10).

use super::*;
use crate::domain::{CommandKind, Fire, InstallationKind, TileStage};

const ANA: &str = "6f1c1a52-2d0e-4f4e-9c1e-0c6c2d9b8a01";
const BETO: &str = "9a0b8c7d-6e5f-4a3b-8c2d-1e0f9a8b7c02";
const CARO: &str = "c3a5e2f0-1b4d-4c6e-8f7a-2d9b0c1e3f03";

fn action(n: u32) -> String {
    format!("00000000-0000-4000-8000-{n:012}")
}

fn drop_cmd(n: u32, player: &str, x: u32, y: u32) -> Command {
    Command {
        action_id: action(n),
        player_id: player.to_string(),
        kind: Some(CommandKind::Drop { x, y }),
    }
}

fn catalog_with_ppm(ppm: u32) -> Catalog {
    let mut catalog = Catalog::d01();
    catalog.fire.ignition_chance_ppm = ppm;
    catalog
}

fn catalog_with_propagation_ppm(ppm: u32) -> Catalog {
    let mut catalog = Catalog::d01();
    catalog.fire.ignition_chance_ppm = 0;
    catalog.fire.propagation_chance_ppm = ppm;
    catalog
}

/// Mundo con las casillas dadas ya en su estado y jugadores sin hojas.
fn world_with(catalog: Catalog, seed: u64, setup: impl FnOnce(&mut WorldParts)) -> World {
    let mut parts = World::with_seed(catalog.clone(), seed).parts();
    setup(&mut parts);
    World::restore(catalog, parts).unwrap()
}

fn index(x: u32, y: u32) -> usize {
    (y * 32 + x) as usize
}

fn burning(drops_received: u32, burning_ticks: u32) -> Option<Fire> {
    Some(Fire {
        drops_received,
        burning_ticks,
    })
}

/// Pasto seco que arde, con la humedad indicada.
fn burning_grass(humidity: u64) -> Tile {
    Tile {
        stage: TileStage::Grass,
        humidity,
        fire: burning(0, 0),
        ..Tile::default()
    }
}

fn dry_grass() -> Tile {
    Tile {
        stage: TileStage::Grass,
        ..Tile::default()
    }
}

fn idle(w: &mut World, count: u64) {
    for _ in 0..count {
        w.step(&[]);
    }
}

fn fire_at(w: &World, x: u32, y: u32) -> Option<Fire> {
    w.tile(x, y).unwrap().fire
}

fn leaves(w: &World, player: &str) -> u64 {
    w.player(player).map_or(0, |p| p.leaves)
}

// ---- Aparición ---------------------------------------------------------

#[test]
fn a_dry_tile_with_a_plant_ignites_when_the_chance_is_certain() {
    let mut w = world_with(catalog_with_ppm(1_000_000), 1, |p| {
        p.tiles[index(5, 5)] = dry_grass();
    });
    w.step(&[]);
    assert_eq!(fire_at(&w, 5, 5), burning(0, 0));
}

#[test]
fn only_dry_tiles_with_a_plant_are_eligible() {
    let mut w = world_with(catalog_with_ppm(1_000_000), 1, |p| {
        p.tiles[index(1, 1)] = Tile {
            humidity: 5,
            ..dry_grass()
        };
        p.tiles[index(2, 2)] = Tile::default(); // tierra seca
        p.tiles[index(3, 3)] = dry_grass();
    });
    w.step(&[]);
    assert_eq!(fire_at(&w, 1, 1), None, "con humedad no arde");
    assert_eq!(fire_at(&w, 2, 2), None, "sin planta no arde");
    assert_eq!(fire_at(&w, 3, 3), burning(0, 0));
}

#[test]
fn nothing_ignites_without_chance_and_burning_tiles_are_not_ignited_again() {
    let mut w = world_with(catalog_with_ppm(0), 1, |p| {
        p.tiles[index(5, 5)] = dry_grass();
    });
    idle(&mut w, 50);
    assert_eq!(fire_at(&w, 5, 5), None);

    let mut w = world_with(catalog_with_ppm(1_000_000), 1, |p| {
        p.tiles[index(5, 5)] = Tile {
            fire: burning(2, 7),
            ..dry_grass()
        };
    });
    w.step(&[]);
    assert_eq!(fire_at(&w, 5, 5), burning(2, 8), "sigue su cuenta");
}

#[test]
fn demo_mode_has_no_new_fires() {
    let mut w = world_with(catalog_with_ppm(1_000_000), 1, |p| {
        p.demo_mode = true;
        p.tiles[index(5, 5)] = dry_grass();
    });
    idle(&mut w, 20);
    assert_eq!(fire_at(&w, 5, 5), None);
}

#[test]
fn the_ignition_draws_one_number_per_eligible_tile_in_row_major_order() {
    // Dos casillas elegibles: la de la fila 0 gasta el primer número y la de
    // la fila 3 el segundo, sea cual sea el orden en el que se crearon.
    let mut w = world_with(catalog_with_ppm(500_000), 11, |p| {
        p.tiles[index(9, 3)] = dry_grass();
        p.tiles[index(20, 0)] = dry_grass();
    });
    let mut rng = SplitMix64::new(11);
    let first = rng.chance_ppm(500_000);
    let second = rng.chance_ppm(500_000);
    w.step(&[]);
    assert_eq!(fire_at(&w, 20, 0).is_some(), first);
    assert_eq!(fire_at(&w, 9, 3).is_some(), second);
    assert_eq!(w.parts().rng_state, rng.to_bytes());
}

#[test]
fn a_tile_that_withers_to_dry_soil_in_the_tick_is_not_eligible() {
    let mut w = world_with(catalog_with_ppm(1_000_000), 1, |p| {
        p.tiles[index(4, 4)] = Tile {
            dry_ticks: 99,
            ..dry_grass()
        };
    });
    w.step(&[]); // el marchitamiento la deja en tierra seca antes del sorteo
    assert_eq!(w.tile(4, 4).unwrap().stage, TileStage::DrySoil);
    assert_eq!(fire_at(&w, 4, 4), None);
}

// ---- Propagación a vecinas (nivel 2, Tarea B3) -------------------------

#[test]
fn fire_spreads_to_orthogonal_eligible_neighbor_when_chance_is_certain() {
    let mut w = world_with(catalog_with_propagation_ppm(1_000_000), 1, |p| {
        p.tiles[index(5, 5)] = burning_grass(0);
        p.tiles[index(5, 6)] = dry_grass();
    });
    w.step(&[]);
    assert_eq!(fire_at(&w, 5, 6), burning(0, 0));
}

#[test]
fn fire_does_not_spread_without_chance() {
    let mut w = world_with(catalog_with_propagation_ppm(0), 1, |p| {
        p.tiles[index(5, 5)] = burning_grass(0);
        p.tiles[index(5, 6)] = dry_grass();
    });
    idle(&mut w, 20);
    assert_eq!(fire_at(&w, 5, 6), None);
}

#[test]
fn fire_does_not_spread_to_ineligible_neighbors() {
    let mut w = world_with(catalog_with_propagation_ppm(1_000_000), 1, |p| {
        p.tiles[index(5, 5)] = burning_grass(0);
        p.tiles[index(5, 4)] = Tile {
            humidity: 10,
            ..dry_grass()
        }; // con humedad
        p.tiles[index(4, 5)] = Tile::default(); // tierra seca (sin planta)
        p.tiles[index(6, 5)] = dry_grass(); // elegible
    });
    w.step(&[]);
    assert_eq!(
        fire_at(&w, 5, 4),
        None,
        "con humedad no prende por propagación"
    );
    assert_eq!(
        fire_at(&w, 4, 5),
        None,
        "tierra seca no prende por propagación"
    );
    assert_eq!(
        fire_at(&w, 6, 5),
        burning(0, 0),
        "la vecina elegible sí prende"
    );
}

#[test]
fn fire_does_not_spread_diagonally() {
    let mut w = world_with(catalog_with_propagation_ppm(1_000_000), 1, |p| {
        p.tiles[index(5, 5)] = burning_grass(0);
        p.tiles[index(6, 6)] = dry_grass(); // diagonal
    });
    w.step(&[]);
    assert_eq!(
        fire_at(&w, 6, 6),
        None,
        "la 4-vecindad ortogonal no incluye diagonales"
    );
}

#[test]
fn propagation_is_synchronous_one_step_per_tick() {
    // Casillas (5, 5), (6, 5) y (7, 5) en línea.
    // Solo (5, 5) arde al inicio; (6, 5) y (7, 5) son pasto seco.
    // En el tick 1 debe prender (6, 5), pero (7, 5) NO debe prender en el mismo tick.
    // En el tick 2, (7, 5) prende desde (6, 5).
    let mut w = world_with(catalog_with_propagation_ppm(1_000_000), 1, |p| {
        p.tiles[index(5, 5)] = burning_grass(0);
        p.tiles[index(6, 5)] = dry_grass();
        p.tiles[index(7, 5)] = dry_grass();
    });
    w.step(&[]);
    assert!(
        fire_at(&w, 6, 5).is_some(),
        "tick 1: el fuego salta a (6, 5)"
    );
    assert_eq!(
        fire_at(&w, 7, 5),
        None,
        "tick 1: (7, 5) no prende en cascada"
    );

    w.step(&[]);
    assert!(
        fire_at(&w, 7, 5).is_some(),
        "tick 2: el fuego salta a (7, 5)"
    );
}

#[test]
fn fire_does_not_propagate_in_demo_mode() {
    let mut w = world_with(catalog_with_propagation_ppm(1_000_000), 1, |p| {
        p.demo_mode = true;
        p.tiles[index(5, 5)] = burning_grass(0);
        p.tiles[index(5, 6)] = dry_grass();
    });
    idle(&mut w, 20);
    assert_eq!(fire_at(&w, 5, 6), None);
}

#[test]
fn fire_spreads_across_a_valley_of_another_size() {
    // La comparativa usa valles de otros tamaños (`with_size`): la
    // propagación debe usar las dimensiones del mundo, no las del juego.
    let mut w = World::with_size(catalog_with_propagation_ppm(1_000_000), 1, 64, 8);
    let at = |x: u32, y: u32| (y * 64 + x) as usize;
    w.tiles[at(40, 2)] = burning_grass(0);
    w.tiles[at(41, 2)] = dry_grass();
    w.tiles[at(40, 3)] = dry_grass();
    w.tiles[at(63, 7)] = burning_grass(0);
    w.tiles[at(0, 0)] = dry_grass(); // no es vecina de ningún fuego
    w.step(&[]);
    assert_eq!(w.tile(41, 2).unwrap().fire, burning(0, 0));
    assert_eq!(w.tile(40, 3).unwrap().fire, burning(0, 0));
    assert_eq!(w.tile(0, 0).unwrap().fire, None);
}

#[test]
fn propagation_draws_in_row_major_order() {
    // Dos casillas elegibles adyacentes a incendios:
    // (5, 0) adyacente a fuego en (4, 0) -> fila 0
    // (2, 4) adyacente a fuego en (2, 3) -> fila 4
    let mut w = world_with(catalog_with_propagation_ppm(500_000), 42, |p| {
        p.tiles[index(4, 0)] = burning_grass(0);
        p.tiles[index(5, 0)] = dry_grass();
        p.tiles[index(2, 3)] = burning_grass(0);
        p.tiles[index(2, 4)] = dry_grass();
    });
    let mut rng = SplitMix64::new(42);
    let first = rng.chance_ppm(500_000);
    let second = rng.chance_ppm(500_000);
    w.step(&[]);
    assert_eq!(fire_at(&w, 5, 0).is_some(), first);
    assert_eq!(fire_at(&w, 2, 4).is_some(), second);
    // Las casillas que no hayan prendido en propagación siguen siendo
    // elegibles para la ignición espontánea que ocurre a continuación.
    if !first {
        let _ = rng.chance_ppm(0);
    }
    if !second {
        let _ = rng.chance_ppm(0);
    }
    assert_eq!(w.parts().rng_state, rng.to_bytes());
}

// ---- Apagar con gotas --------------------------------------------------

#[test]
fn three_drops_put_the_fire_out_and_the_third_player_gets_the_bonus() {
    let mut w = world_with(catalog_with_ppm(0), 1, |p| {
        p.tiles[index(7, 7)] = burning_grass(0);
    });
    w.step(&[drop_cmd(1, ANA, 7, 7)]);
    assert_eq!(fire_at(&w, 7, 7), burning(1, 1));
    w.step(&[drop_cmd(2, BETO, 7, 7)]);
    assert_eq!(fire_at(&w, 7, 7), burning(2, 2));
    assert_eq!((leaves(&w, ANA), leaves(&w, BETO)), (0, 0));

    w.step(&[drop_cmd(3, CARO, 7, 7)]);
    assert_eq!(fire_at(&w, 7, 7), None);
    let caro = w.player(CARO).unwrap();
    assert_eq!((caro.leaves, caro.leaves_earned), (5, 5));
    assert_eq!((leaves(&w, ANA), leaves(&w, BETO)), (0, 0));
    assert_eq!(w.tile(7, 7).unwrap().stage, TileStage::Grass);
    assert_eq!(w.tile(7, 7).unwrap().humidity, 75);
}

#[test]
fn the_same_player_can_put_it_out_with_three_drops() {
    let mut w = world_with(catalog_with_ppm(0), 1, |p| {
        p.tiles[index(7, 7)] = burning_grass(0);
    });
    w.step(&[
        drop_cmd(1, ANA, 7, 7),
        drop_cmd(2, ANA, 7, 7),
        drop_cmd(3, ANA, 7, 7),
    ]);
    assert_eq!(fire_at(&w, 7, 7), None);
    assert_eq!(leaves(&w, ANA), 5);
}

#[test]
fn cp04_three_players_in_the_same_tick_put_it_out_once_with_one_bonus() {
    let mut w = world_with(catalog_with_ppm(0), 1, |p| {
        p.tiles[index(7, 7)] = burning_grass(0);
    });
    let out = w.step(&[
        drop_cmd(1, ANA, 7, 7),
        drop_cmd(2, BETO, 7, 7),
        drop_cmd(3, CARO, 7, 7),
    ]);
    assert!(
        out.results
            .iter()
            .all(|r| r.outcome == CommandOutcome::Applied)
    );
    assert_eq!(fire_at(&w, 7, 7), None);
    let bonus: u64 = [ANA, BETO, CARO]
        .iter()
        .map(|p| w.player(p).unwrap().leaves_earned)
        .sum();
    assert_eq!(bonus, 5, "un solo bono");
    assert_eq!(w.player(CARO).unwrap().leaves, 5, "la tercera gota");
    assert_eq!(w.tile(7, 7).unwrap().humidity, 75);
    assert_eq!(
        w.humidity_contributed(),
        3 * 25,
        "la humedad aportada suma lo que las gotas aceptadas"
    );

    // Una cuarta gota ya no encuentra fuego: es una gota normal.
    w.step(&[drop_cmd(4, ANA, 7, 7)]);
    assert_eq!(w.tile(7, 7).unwrap().stage, TileStage::Shrub);
    assert_eq!(w.player(ANA).unwrap().leaves_earned, 2, "solo el arbusto");
}

#[test]
fn drops_on_a_burning_tile_add_humidity_without_growing_it() {
    let mut w = world_with(catalog_with_ppm(0), 1, |p| {
        p.tiles[index(7, 7)] = burning_grass(80);
    });
    w.step(&[drop_cmd(1, ANA, 7, 7)]); // 105 humedad, pero arde: no crece
    let tile = w.tile(7, 7).unwrap();
    assert_eq!((tile.stage, tile.humidity), (TileStage::Grass, 105));
    assert_eq!(fire_at(&w, 7, 7), burning(1, 1));
    assert_eq!(leaves(&w, ANA), 0);
}

#[test]
fn growth_that_was_pending_resolves_when_the_fire_goes_out_for_who_put_it_out() {
    let mut w = world_with(catalog_with_ppm(0), 1, |p| {
        p.tiles[index(7, 7)] = Tile {
            fire: burning(2, 5),
            ..burning_grass(80)
        };
    });
    w.step(&[drop_cmd(1, ANA, 7, 7)]);
    let tile = w.tile(7, 7).unwrap();
    assert_eq!(fire_at(&w, 7, 7), None);
    assert_eq!((tile.stage, tile.humidity), (TileStage::Shrub, 5));
    assert_eq!(leaves(&w, ANA), 5 + 2, "bono más hojas del arbusto");
}

#[test]
fn a_bigger_drop_counts_for_every_burning_tile_it_reaches() {
    let mut w = world_with(catalog_with_ppm(0), 1, |p| {
        p.tiles[index(7, 7)] = Tile {
            fire: burning(2, 5),
            ..burning_grass(0)
        };
        p.tiles[index(8, 7)] = burning_grass(0);
        let mut ana = crate::domain::Player::starting(ANA, 20);
        ana.upgrades.push(crate::domain::UpgradeKind::BiggerDrop);
        p.players.push(ana);
    });
    w.step(&[drop_cmd(1, ANA, 7, 7)]);
    assert_eq!(fire_at(&w, 7, 7), None);
    assert_eq!(fire_at(&w, 8, 7), burning(1, 1));
    assert_eq!(leaves(&w, ANA), 5);
}

#[test]
fn a_bigger_drop_that_completes_several_fires_pays_the_bonus_once_per_fire() {
    // Cada fuego apagado es una extinción distinta (decisión del equipo): la
    // gota más grande que completa la tercera gota de dos fuegos paga dos bonos.
    let mut w = world_with(catalog_with_ppm(0), 1, |p| {
        for (x, y) in [(7, 7), (8, 7)] {
            p.tiles[index(x, y)] = Tile {
                fire: burning(2, 5),
                ..burning_grass(0)
            };
        }
        let mut ana = crate::domain::Player::starting(ANA, 20);
        ana.upgrades.push(crate::domain::UpgradeKind::BiggerDrop);
        p.players.push(ana);
    });
    w.step(&[drop_cmd(1, ANA, 7, 7)]);
    assert_eq!(fire_at(&w, 7, 7), None);
    assert_eq!(fire_at(&w, 8, 7), None);
    assert_eq!(leaves(&w, ANA), 2 * 5, "un bono por cada fuego apagado");
}

// ---- Aspersor ----------------------------------------------------------

fn sprinkler_world(owner: &str) -> World {
    world_with(catalog_with_ppm(0), 1, |p| {
        p.tiles[index(10, 10)] = Tile {
            installation: Some(crate::domain::Installation {
                kind: InstallationKind::Sprinkler,
                owner_id: owner.to_string(),
                remaining_durability: 30,
            }),
            ..Tile::default()
        };
        p.tiles[index(11, 11)] = burning_grass(0);
        p.tiles[index(13, 13)] = burning_grass(0); // fuera de la zona 3 × 3
        let mut ana = crate::domain::Player::starting(owner, 20);
        ana.last_action_tick = 0;
        p.players.push(ana);
    })
}

#[test]
fn a_sprinkler_puts_out_the_fires_of_its_area_without_a_bonus() {
    let mut w = sprinkler_world(ANA);
    idle(&mut w, 19);
    assert_eq!(fire_at(&w, 11, 11), burning(0, 19), "todavía no riega");
    w.step(&[]); // tick 20: riega
    assert_eq!(fire_at(&w, 11, 11), None);
    // 25 del riego menos 2 de la evaporación, que también cae en el tick 20.
    assert_eq!(w.tile(11, 11).unwrap().humidity, 23);
    assert_eq!(
        leaves(&w, ANA),
        0,
        "sin bono: el aspersor no suelta la tercera gota"
    );
    assert!(
        fire_at(&w, 13, 13).is_some(),
        "fuera de su zona sigue ardiendo"
    );
}

#[test]
fn a_sprinkler_of_an_absent_owner_does_not_put_out_fires() {
    let mut w = sprinkler_world(ANA);
    let mut parts = w.parts();
    parts.tick = 80_000; // el dueño lleva más de 7.200 s sin actuar
    w = World::restore(Catalog::d01(), parts).unwrap();
    idle(&mut w, 20);
    assert!(fire_at(&w, 11, 11).is_some());
    assert_eq!(w.tile(11, 11).unwrap().humidity, 0);
}

// ---- Consumirse ----------------------------------------------------------

#[test]
fn an_unattended_fire_burns_out_after_the_catalog_ticks_into_dry_soil() {
    let mut w = world_with(catalog_with_ppm(0), 1, |p| {
        p.tiles[index(7, 7)] = Tile {
            stage: TileStage::Tree,
            humidity: 60,
            dry_ticks: 3,
            fire: burning(2, 0),
            installation: Some(crate::domain::Installation {
                kind: InstallationKind::Drip,
                owner_id: ANA.to_string(),
                remaining_durability: 30,
            }),
        };
    });
    idle(&mut w, 299);
    assert!(fire_at(&w, 7, 7).is_some());
    assert_eq!(w.tile(7, 7).unwrap().stage, TileStage::Tree);
    w.step(&[]); // tick 300
    let tile = w.tile(7, 7).unwrap();
    assert_eq!(tile.fire, None);
    assert_eq!(tile.stage, TileStage::DrySoil);
    assert_eq!(tile.dry_ticks, 0);
    assert!(tile.installation.is_some(), "la instalación se conserva");
}

#[test]
fn a_drop_on_the_last_tick_still_puts_it_out_before_it_burns_out() {
    let mut w = world_with(catalog_with_ppm(0), 1, |p| {
        p.tiles[index(7, 7)] = Tile {
            fire: burning(2, 299),
            ..burning_grass(0)
        };
    });
    w.step(&[drop_cmd(1, ANA, 7, 7)]); // los comandos van antes de consumir
    assert_eq!(fire_at(&w, 7, 7), None);
    assert_eq!(w.tile(7, 7).unwrap().stage, TileStage::Grass);
}

#[test]
fn burning_continues_in_demo_mode_so_that_the_fire_always_resolves() {
    let mut w = world_with(catalog_with_ppm(0), 1, |p| {
        p.demo_mode = true;
        p.tiles[index(7, 7)] = Tile {
            fire: burning(0, 298),
            ..burning_grass(0)
        };
    });
    idle(&mut w, 2);
    assert_eq!(w.tile(7, 7).unwrap().stage, TileStage::DrySoil);
}

#[test]
fn evaporation_continues_while_burning_but_growth_and_wither_are_frozen() {
    let mut w = world_with(catalog_with_ppm(0), 1, |p| {
        p.tiles[index(0, 0)] = burning_grass(10);
        p.tiles[index(1, 0)] = Tile {
            dry_ticks: 90,
            ..burning_grass(0)
        };
    });
    idle(&mut w, 30); // tres evaporaciones de 2
    assert_eq!(w.tile(0, 0).unwrap().humidity, 4);
    let frozen = w.tile(1, 0).unwrap();
    assert_eq!((frozen.dry_ticks, frozen.stage), (90, TileStage::Grass));
}

// ---- Azar: determinismo (CP-10) y persistencia -------------------------

fn busy_world(seed: u64) -> World {
    world_with(catalog_with_ppm(5_000), seed, |p| {
        for x in 0..32 {
            for y in 0..4 {
                p.tiles[index(x, y)] = Tile {
                    stage: TileStage::Shrub,
                    ..Tile::default()
                };
            }
        }
    })
}

fn script(tick: u32) -> Vec<Command> {
    // Gotas de dos jugadores sobre casillas que van cambiando.
    let n = tick * 2;
    vec![
        drop_cmd(n + 1, ANA, tick % 32, tick % 4),
        drop_cmd(n + 2, BETO, (tick * 7) % 32, (tick * 3) % 4),
    ]
}

fn run(w: &mut World, from: u32, to: u32) {
    for t in from..to {
        w.step(&script(t));
    }
}

fn fire_map(w: &World) -> Vec<usize> {
    w.tiles()
        .iter()
        .enumerate()
        .filter(|(_, t)| t.fire.is_some())
        .map(|(i, _)| i)
        .collect()
}

#[test]
fn cp10_same_seed_and_same_commands_give_the_same_valley() {
    let mut a = busy_world(2026);
    let mut b = busy_world(2026);
    let mut ever_burned = false;
    for t in 0..600 {
        a.step(&script(t));
        b.step(&script(t));
        ever_burned |= !fire_map(&a).is_empty();
    }
    assert!(ever_burned, "la prueba tiene que ver fuego");
    assert_eq!(a.parts(), b.parts(), "incluye incendios y rng_state");
}

#[test]
fn another_seed_gives_another_fire_pattern() {
    let pattern = |seed| {
        let mut w = busy_world(seed);
        let mut seen = Vec::new();
        for t in 0..300 {
            w.step(&script(t));
            seen.push(fire_map(&w));
        }
        seen
    };
    assert_ne!(pattern(1), pattern(2));
}

#[test]
fn a_restored_world_continues_the_same_random_sequence() {
    let mut original = busy_world(77);
    run(&mut original, 0, 300);
    assert!(!original.parts().rng_state.is_empty());
    let mut restored = World::restore(catalog_with_ppm(5_000), original.parts()).unwrap();
    assert_eq!(restored.parts(), original.parts());
    run(&mut original, 300, 700);
    run(&mut restored, 300, 700);
    assert_eq!(restored.parts(), original.parts());
}

#[test]
fn the_initial_state_is_the_seed_and_it_travels_in_the_parts() {
    let w = World::with_seed(Catalog::d01(), 123);
    assert_eq!(w.parts().rng_state, SplitMix64::new(123).to_bytes());
}

#[test]
fn restoring_a_snapshot_without_rng_state_falls_back_to_a_seed_from_its_tick() {
    let mut parts = World::with_seed(Catalog::d01(), 5).parts();
    parts.tick = 40;
    parts.rng_state = Vec::new();
    let a = World::restore(Catalog::d01(), parts.clone()).unwrap();
    let b = World::restore(Catalog::d01(), parts).unwrap();
    assert_eq!(a.parts().rng_state, SplitMix64::new(40).to_bytes());
    assert_eq!(a.parts(), b.parts());
}

#[test]
fn restoring_a_snapshot_with_a_malformed_rng_state_is_refused() {
    let mut parts = World::with_seed(Catalog::d01(), 5).parts();
    parts.rng_state = vec![1, 2, 3];
    assert_eq!(
        World::restore(Catalog::d01(), parts).unwrap_err(),
        RestoreError::RngState { len: 3 }
    );
}

#[test]
fn resetting_the_valley_clears_the_fires_but_not_the_generator() {
    let mut w = world_with(catalog_with_ppm(0), 3, |p| {
        p.tiles[index(7, 7)] = burning_grass(0);
    });
    w.step(&[]);
    let before = w.parts().rng_state;
    w.step(&[Command {
        action_id: action(1),
        player_id: ANA.to_string(),
        kind: Some(CommandKind::ResetValley),
    }]);
    assert_eq!(fire_at(&w, 7, 7), None);
    assert_eq!(w.parts().rng_state, before);
}
