"""Catálogo del juego (D-01): lectura, validación y versión.

El archivo `catalog/catalog.json` tiene tres grupos (`rules`, `installations`
y `upgrades`), que son el `kind` de las filas de `catalog_entries`. Cada
elemento es una fila: un objeto de cifras (`drop`, `sprinkler`…) o una cifra
suelta (`installation_price_increase_percent`). Aquí se valida la forma y el
rango de cada cifra, y se calcula `catalog_version`.

Este módulo no conoce gRPC ni PostgreSQL: el servidor traduce a los mensajes
del contrato con la tabla `SECTIONS`, que también dice cómo se llama cada
cifra en `catalog.proto`.
"""

import hashlib
import json
from collections.abc import Iterable
from dataclasses import dataclass
from pathlib import Path

U32_MAX = 2**32 - 1
U64_MAX = 2**64 - 1
PPM_MAX = 1_000_000

# Grupo del archivo -> `kind` de catalog_entries (diagrama 07).
GROUP_KIND = {"rules": "rule", "installations": "installation", "upgrades": "upgrade"}
KIND_GROUP = {kind: group for group, kind in GROUP_KIND.items()}


class CatalogError(Exception):
    """El catálogo no es válido. El mensaje nombra la cifra, nunca su valor."""


@dataclass(frozen=True)
class Field:
    """Una cifra: su nombre en el archivo, su campo en `catalog.proto` y su
    rango."""

    name: str
    proto: str
    maximum: int = U64_MAX
    minimum: int = 0


@dataclass(frozen=True)
class Section:
    """Un elemento del catálogo. Con `fields`, un objeto de cifras; sin ellos,
    `scalar` describe la cifra suelta."""

    kind: str
    fields: tuple[Field, ...] = ()
    scalar: Field | None = None


def _u64(name: str, proto: str, minimum: int = 0) -> Field:
    return Field(name, proto, U64_MAX, minimum)


def _u32(name: str, proto: str, minimum: int = 0, maximum: int = U32_MAX) -> Field:
    return Field(name, proto, maximum, minimum)


# Clave de la fila -> su forma. La clave es la de `catalog_entries.key`.
SECTIONS: dict[str, Section] = {
    "drop": Section(
        "rule",
        (
            _u64("water_cost_liters", "water_cost"),
            _u64("humidity_points", "humidity"),
            _u64("demo_humidity_points", "demo_humidity", minimum=1),
        ),
    ),
    "tank": Section(
        "rule",
        (
            _u64("capacity_liters", "capacity"),
            _u64("refill_liters", "refill_amount"),
            _u32("refill_interval_seconds", "refill_interval_seconds", minimum=1),
        ),
    ),
    "growth": Section(
        "rule",
        (
            _u64("humidity_per_stage_points", "humidity_per_stage", minimum=1),
            _u64("leaves_grass", "leaves_grass"),
            _u64("leaves_shrub", "leaves_shrub"),
            _u64("leaves_flowers", "leaves_flowers"),
            _u64("leaves_tree", "leaves_tree"),
            _u64("tree_max_humidity_points", "tree_max_humidity"),
        ),
    ),
    "evaporation": Section(
        "rule",
        (
            _u64("humidity_per_second_points", "humidity_per_second"),
            _u32("wither_after_ticks", "wither_after_ticks", minimum=1),
        ),
    ),
    "fire": Section(
        "rule",
        (
            _u32("drops_to_extinguish", "drops_to_extinguish", minimum=1),
            _u64("extinguish_bonus_leaves", "extinguish_bonus_leaves"),
            _u32("burn_out_ticks", "burn_out_ticks", minimum=1),
            Field("ignition_chance_ppm", "ignition_chance_ppm", PPM_MAX),
            Field("propagation_chance_ppm", "propagation_chance_ppm", PPM_MAX),
        ),
    ),
    "terrain": Section(
        "rule",
        (
            _u32("river_width_tiles", "river_width", maximum=8),
            _u32("rock_count", "rock_count", maximum=1024),
        ),
    ),
    "urban": Section(
        "rule",
        (
            _u32("building_tree_threshold", "building_tree_threshold"),
            _u64("building_humidity_points_threshold", "building_humidity_threshold"),
            _u32("max_buildings", "max_buildings", maximum=1024),
            _u32("trash_interval_seconds", "trash_interval_seconds", minimum=1),
            _u32("trash_spread_interval_seconds", "trash_spread_interval_seconds", minimum=1),
            _u64("clean_reward_leaves", "clean_reward_leaves"),
        ),
    ),
    "installation_price_increase_percent": Section(
        "rule", scalar=_u32("installation_price_increase_percent", "installation_price_increase_percent")
    ),
    "offline_installation_seconds": Section(
        "rule", scalar=_u64("offline_installation_seconds", "offline_installation_seconds")
    ),
    "sprinkler": Section(
        "installation",
        (
            _u64("price_leaves", "price"),
            _u32("area_side_tiles", "area_side", minimum=1),
            _u64("humidity_points", "humidity"),
            _u32("interval_seconds", "interval_seconds", minimum=1),
            _u32("durability", "durability", minimum=1),
        ),
    ),
    "drip": Section(
        "installation",
        (
            _u64("price_leaves", "price"),
            _u32("area_side_tiles", "area_side", minimum=1),
            _u32("evaporation_divisor", "evaporation_divisor", minimum=1),
            _u32("durability", "durability", minimum=1),
            _u32("wear_interval_seconds", "wear_interval_seconds", minimum=1),
        ),
    ),
    "treatment_plant": Section(
        "installation",
        (
            _u64("price_leaves", "price"),
            _u64("refill_liters", "refill_amount"),
            _u32("interval_seconds", "interval_seconds", minimum=1),
            _u32("durability", "durability", minimum=1),
        ),
    ),
    "trash_bin": Section(
        "installation",
        (
            _u64("price_leaves", "price"),
            _u32("area_side_tiles", "area_side", minimum=1),
            _u32("interval_seconds", "interval_seconds", minimum=1),
            _u32("durability", "durability", minimum=1),
        ),
    ),
    "bigger_drop": Section(
        "upgrade",
        (_u64("price_leaves", "price"), _u32("area_side_tiles", "area_side", minimum=1)),
    ),
    "bigger_tank": Section(
        "upgrade",
        (_u64("price_leaves", "price"), _u64("extra_capacity_liters", "extra_capacity")),
    ),
}


@dataclass(frozen=True)
class CatalogEntry:
    """Fila de `catalog_entries`. `value` es un objeto de cifras (dict) o una
    cifra suelta (int)."""

    key: str
    kind: str
    value: object


@dataclass(frozen=True)
class Catalog:
    """Catálogo validado: filas ordenadas por clave y su versión."""

    entries: tuple[CatalogEntry, ...]
    version: str

    def value(self, key: str) -> object:
        for entry in self.entries:
            if entry.key == key:
                return entry.value
        raise KeyError(key)


def _check_number(label: str, field: Field, value: object) -> int:
    if value is None:
        raise CatalogError(f"{label} no admite null")
    # bool es subclase de int; los decimales (aunque valgan 25.0) no son
    # enteros sin signo (regla 7 de CLAUDE.md).
    if isinstance(value, bool) or not isinstance(value, int):
        raise CatalogError(f"{label} debe ser un entero")
    if not field.minimum <= value <= field.maximum:
        raise CatalogError(f"{label} debe estar entre {field.minimum} y {field.maximum}")
    return value


def _check_section(key: str, section: Section, value: object) -> object:
    if section.scalar is not None:
        return _check_number(key, section.scalar, value)
    if not isinstance(value, dict):
        raise CatalogError(f"{key} debe ser un objeto")
    expected = {field.name for field in section.fields}
    unknown = sorted(set(value) - expected)
    if unknown:
        raise CatalogError(f"{key} tiene cifras desconocidas: {', '.join(map(str, unknown))}")
    missing = sorted(expected - set(value))
    if missing:
        raise CatalogError(f"{key} no tiene las cifras: {', '.join(missing)}")
    return {
        field.name: _check_number(f"{key}.{field.name}", field, value[field.name])
        for field in section.fields
    }


def _build(rows: Iterable[tuple[str, str, object]]) -> Catalog:
    """Valida filas (clave, kind, valor) y arma el catálogo con su versión."""
    seen: dict[str, CatalogEntry] = {}
    for key, kind, value in rows:
        section = SECTIONS.get(key)
        if section is None:
            raise CatalogError(f"elemento desconocido: {key}")
        if key in seen:
            raise CatalogError(f"elemento repetido: {key}")
        if kind != section.kind:
            raise CatalogError(f"{key} debe ser de tipo {section.kind}")
        seen[key] = CatalogEntry(key, kind, _check_section(key, section, value))
    missing = sorted(set(SECTIONS) - set(seen))
    if missing:
        raise CatalogError(f"faltan elementos: {', '.join(missing)}")
    entries = tuple(sorted(seen.values(), key=lambda entry: entry.key))
    return Catalog(entries, _version(entries))


def _version(entries: Iterable[CatalogEntry]) -> str:
    """SHA-256 del JSON canónico: el mismo documento del archivo (tres grupos),
    con claves ordenadas, sin espacios y solo ASCII. No depende del formato
    del archivo ni del orden de las filas."""
    document: dict[str, dict[str, object]] = {group: {} for group in GROUP_KIND}
    for entry in entries:
        document[KIND_GROUP[entry.kind]][entry.key] = entry.value
    canonical = json.dumps(document, sort_keys=True, separators=(",", ":"), ensure_ascii=True)
    return hashlib.sha256(canonical.encode("ascii")).hexdigest()


def parse_catalog(document: object) -> Catalog:
    """Valida el documento del archivo y lo convierte en un Catalog."""
    if not isinstance(document, dict):
        raise CatalogError("el catálogo debe ser un objeto JSON")
    unknown = sorted(set(document) - set(GROUP_KIND))
    if unknown:
        raise CatalogError(f"grupos desconocidos: {', '.join(map(str, unknown))}")
    rows: list[tuple[str, str, object]] = []
    for group, kind in GROUP_KIND.items():
        items = document.get(group)
        if not isinstance(items, dict):
            raise CatalogError(f"el grupo {group} debe ser un objeto")
        rows.extend((key, kind, value) for key, value in items.items())
    return _build(rows)


def catalog_from_entries(entries: Iterable[CatalogEntry]) -> Catalog:
    """Reconstruye y revalida el catálogo a partir de las filas guardadas."""
    return _build((entry.key, entry.kind, entry.value) for entry in entries)


def load_catalog_file(path: Path) -> Catalog:
    """Lee y valida un archivo de catálogo."""
    try:
        text = Path(path).read_text(encoding="utf-8")
    except (OSError, UnicodeDecodeError) as error:
        raise CatalogError(f"no se pudo leer el catálogo {path}: {error}") from error
    try:
        document = json.loads(text)
    except ValueError as error:
        raise CatalogError(f"el catálogo {path} no es JSON válido: {error}") from error
    return parse_catalog(document)
