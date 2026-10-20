//! JSON mínimo y conversión del catálogo, compartidos por la prueba de
//! conformidad (`tests/conformance.rs`) y la comparativa
//! (`examples/bench`). Sin dependencias: solo enteros sin signo, cadenas,
//! listas, objetos y booleanos, que es lo que usan los guiones, las cargas de
//! trabajo y `catalog/catalog.json`.
#![allow(dead_code)] // cada uso emplea solo una parte

use world::proto::persistence::v1 as persistence_pb;

// ---- JSON mínimo ---------------------------------------------------------

#[derive(Debug, Clone, PartialEq)]
pub enum Json {
    Bool(bool),
    Num(u64),
    Str(String),
    Arr(Vec<Json>),
    Obj(Vec<(String, Json)>),
}

impl Json {
    pub fn parse(text: &str) -> Json {
        let mut parser = Parser {
            bytes: text.as_bytes(),
            at: 0,
        };
        let value = parser.value();
        parser.skip_space();
        assert_eq!(parser.at, parser.bytes.len(), "sobra texto tras el JSON");
        value
    }

    pub fn get(&self, key: &str) -> Option<&Json> {
        match self {
            Json::Obj(fields) => fields.iter().find(|(k, _)| k == key).map(|(_, v)| v),
            _ => None,
        }
    }

    pub fn field(&self, key: &str) -> &Json {
        self.get(key)
            .unwrap_or_else(|| panic!("falta el campo {key}"))
    }

    pub fn u64(&self) -> u64 {
        match self {
            Json::Num(n) => *n,
            other => panic!("se esperaba un entero y hay {other:?}"),
        }
    }

    pub fn u32(&self) -> u32 {
        u32::try_from(self.u64()).expect("cabe en 32 bits")
    }

    pub fn str(&self) -> &str {
        match self {
            Json::Str(s) => s,
            other => panic!("se esperaba una cadena y hay {other:?}"),
        }
    }

    pub fn bool(&self) -> bool {
        match self {
            Json::Bool(b) => *b,
            other => panic!("se esperaba un booleano y hay {other:?}"),
        }
    }

    pub fn arr(&self) -> &[Json] {
        match self {
            Json::Arr(items) => items,
            other => panic!("se esperaba una lista y hay {other:?}"),
        }
    }

    /// Sigue una ruta con puntos (`rules.fire.ignition_chance_ppm`).
    pub fn path(&self, path: &str) -> &Json {
        path.split('.').fold(self, |node, key| node.field(key))
    }

    /// Cambia el entero de una ruta que ya existe (un nombre mal escrito en un
    /// guion es un error, no un campo nuevo).
    pub fn set(&mut self, path: &str, value: u64) {
        let (parent, last) = match path.rsplit_once('.') {
            Some((parent, last)) => (Some(parent), last),
            None => (None, path),
        };
        let mut node = self;
        if let Some(parent) = parent {
            for key in parent.split('.') {
                node = match node {
                    Json::Obj(fields) => fields
                        .iter_mut()
                        .find(|(k, _)| k == key)
                        .map(|(_, v)| v)
                        .unwrap_or_else(|| panic!("la ruta {path} no existe en el catálogo")),
                    _ => panic!("la ruta {path} no existe en el catálogo"),
                };
            }
        }
        match node {
            Json::Obj(fields) => {
                let slot = fields
                    .iter_mut()
                    .find(|(k, _)| k == last)
                    .unwrap_or_else(|| panic!("la ruta {path} no existe en el catálogo"));
                assert!(matches!(slot.1, Json::Num(_)), "{path} no es un entero");
                slot.1 = Json::Num(value);
            }
            _ => panic!("la ruta {path} no existe en el catálogo"),
        }
    }
}

struct Parser<'a> {
    bytes: &'a [u8],
    at: usize,
}

impl Parser<'_> {
    fn skip_space(&mut self) {
        while self.at < self.bytes.len() && self.bytes[self.at].is_ascii_whitespace() {
            self.at += 1;
        }
    }

    fn eat(&mut self, byte: u8) {
        self.skip_space();
        assert_eq!(
            self.bytes.get(self.at),
            Some(&byte),
            "en el byte {}",
            self.at
        );
        self.at += 1;
    }

    fn peek(&mut self) -> u8 {
        self.skip_space();
        self.bytes[self.at]
    }

    fn value(&mut self) -> Json {
        match self.peek() {
            b'{' => {
                self.eat(b'{');
                let mut fields = Vec::new();
                if self.peek() == b'}' {
                    self.eat(b'}');
                    return Json::Obj(fields);
                }
                loop {
                    let key = self.string();
                    self.eat(b':');
                    fields.push((key, self.value()));
                    if self.peek() == b',' {
                        self.eat(b',');
                    } else {
                        self.eat(b'}');
                        return Json::Obj(fields);
                    }
                }
            }
            b'[' => {
                self.eat(b'[');
                let mut items = Vec::new();
                if self.peek() == b']' {
                    self.eat(b']');
                    return Json::Arr(items);
                }
                loop {
                    items.push(self.value());
                    if self.peek() == b',' {
                        self.eat(b',');
                    } else {
                        self.eat(b']');
                        return Json::Arr(items);
                    }
                }
            }
            b'"' => Json::Str(self.string()),
            b't' => self.literal("true", Json::Bool(true)),
            b'f' => self.literal("false", Json::Bool(false)),
            b'0'..=b'9' => {
                let start = self.at;
                while self.at < self.bytes.len() && self.bytes[self.at].is_ascii_digit() {
                    self.at += 1;
                }
                let text = std::str::from_utf8(&self.bytes[start..self.at]).unwrap();
                Json::Num(text.parse().expect("un entero de 64 bits sin signo"))
            }
            other => panic!("JSON no admitido en el byte {}: {}", self.at, other as char),
        }
    }

    fn literal(&mut self, word: &str, value: Json) -> Json {
        assert!(self.bytes[self.at..].starts_with(word.as_bytes()));
        self.at += word.len();
        value
    }

    fn string(&mut self) -> String {
        self.eat(b'"');
        let mut out = Vec::new();
        loop {
            let byte = self.bytes[self.at];
            self.at += 1;
            match byte {
                b'"' => return String::from_utf8(out).expect("UTF-8"),
                b'\\' => {
                    let escaped = self.bytes[self.at];
                    self.at += 1;
                    match escaped {
                        b'"' | b'\\' | b'/' => out.push(escaped),
                        b'n' => out.push(b'\n'),
                        b't' => out.push(b'\t'),
                        b'u' => {
                            let hex =
                                std::str::from_utf8(&self.bytes[self.at..self.at + 4]).unwrap();
                            self.at += 4;
                            let code = u32::from_str_radix(hex, 16).unwrap();
                            let ch = char::from_u32(code).expect("un carácter, no un sustituto");
                            let mut buffer = [0; 4];
                            out.extend_from_slice(ch.encode_utf8(&mut buffer).as_bytes());
                        }
                        other => panic!("escape no admitido: \\{}", other as char),
                    }
                }
                other => out.push(other),
            }
        }
    }
}

pub fn catalog_message(doc: &Json, version: &str) -> persistence_pb::Catalog {
    use persistence_pb as p;
    let n = |path: &str| doc.path(path).u64();
    let n32 = |path: &str| doc.path(path).u32();
    p::Catalog {
        catalog_version: version.to_string(),
        drop: Some(p::DropRules {
            water_cost: n("rules.drop.water_cost_liters"),
            humidity: n("rules.drop.humidity_points"),
            demo_humidity: n("rules.drop.demo_humidity_points"),
        }),
        tank: Some(p::TankRules {
            capacity: n("rules.tank.capacity_liters"),
            refill_amount: n("rules.tank.refill_liters"),
            refill_interval_seconds: n32("rules.tank.refill_interval_seconds"),
        }),
        growth: Some(p::GrowthRules {
            humidity_per_stage: n("rules.growth.humidity_per_stage_points"),
            leaves_grass: n("rules.growth.leaves_grass"),
            leaves_shrub: n("rules.growth.leaves_shrub"),
            leaves_flowers: n("rules.growth.leaves_flowers"),
            leaves_tree: n("rules.growth.leaves_tree"),
            tree_max_humidity: n("rules.growth.tree_max_humidity_points"),
        }),
        evaporation: Some(p::EvaporationRules {
            humidity_per_second: n("rules.evaporation.humidity_per_second_points"),
            wither_after_ticks: n32("rules.evaporation.wither_after_ticks"),
        }),
        fire: Some(p::FireRules {
            drops_to_extinguish: n32("rules.fire.drops_to_extinguish"),
            extinguish_bonus_leaves: n("rules.fire.extinguish_bonus_leaves"),
            burn_out_ticks: n32("rules.fire.burn_out_ticks"),
            ignition_chance_ppm: n32("rules.fire.ignition_chance_ppm"),
        }),
        sprinkler: Some(p::SprinklerRules {
            price: n("installations.sprinkler.price_leaves"),
            area_side: n32("installations.sprinkler.area_side_tiles"),
            humidity: n("installations.sprinkler.humidity_points"),
            interval_seconds: n32("installations.sprinkler.interval_seconds"),
        }),
        drip: Some(p::DripRules {
            price: n("installations.drip.price_leaves"),
            area_side: n32("installations.drip.area_side_tiles"),
            evaporation_divisor: n32("installations.drip.evaporation_divisor"),
        }),
        treatment_plant: Some(p::TreatmentPlantRules {
            price: n("installations.treatment_plant.price_leaves"),
            refill_amount: n("installations.treatment_plant.refill_liters"),
            interval_seconds: n32("installations.treatment_plant.interval_seconds"),
        }),
        installation_price_increase_percent: n32("rules.installation_price_increase_percent"),
        bigger_drop: Some(p::BiggerDropRules {
            price: n("upgrades.bigger_drop.price_leaves"),
            area_side: n32("upgrades.bigger_drop.area_side_tiles"),
        }),
        bigger_tank: Some(p::BiggerTankRules {
            price: n("upgrades.bigger_tank.price_leaves"),
            extra_capacity: n("upgrades.bigger_tank.extra_capacity_liters"),
        }),
        offline_installation_seconds: n("rules.offline_installation_seconds"),
    }
}
