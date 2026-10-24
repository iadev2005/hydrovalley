"""Pruebas del dominio del catálogo: lectura, validación y versión."""

import copy
import json
import re
from pathlib import Path

import pytest

from persistence.catalog import (
    CatalogEntry,
    CatalogError,
    catalog_from_entries,
    load_catalog_file,
    parse_catalog,
)

REPO_CATALOG = Path(__file__).resolve().parents[3] / "catalog" / "catalog.json"


@pytest.fixture
def document() -> dict:
    return json.loads(REPO_CATALOG.read_text(encoding="utf-8"))


def test_the_repository_catalog_is_valid_and_carries_the_d01_figures(document):
    catalog = parse_catalog(document)
    values = {entry.key: entry.value for entry in catalog.entries}
    assert values["drop"] == {
        "water_cost_liters": 1,
        "humidity_points": 25,
        "demo_humidity_points": 1,
    }
    assert values["tank"]["capacity_liters"] == 20
    assert values["growth"]["humidity_per_stage_points"] == 100
    assert values["sprinkler"]["price_leaves"] == 10
    assert values["drip"]["area_side_tiles"] == 5
    assert values["treatment_plant"]["price_leaves"] == 40
    assert values["bigger_drop"]["price_leaves"] == 30
    assert values["bigger_tank"]["extra_capacity_liters"] == 10
    assert values["installation_price_increase_percent"] == 15
    assert values["offline_installation_seconds"] == 7200
    assert values["fire"]["ignition_chance_ppm"] == 50
    assert values["fire"]["propagation_chance_ppm"] == 2000


def test_every_entry_has_the_kind_of_its_group(document):
    kinds = {entry.key: entry.kind for entry in parse_catalog(document).entries}
    assert kinds["drop"] == "rule"
    assert kinds["installation_price_increase_percent"] == "rule"
    assert kinds["sprinkler"] == "installation"
    assert kinds["bigger_tank"] == "upgrade"
    assert len(kinds) == 12


def test_version_is_a_sha256_of_the_canonical_json(document):
    version = parse_catalog(document).version
    assert re.fullmatch(r"[0-9a-f]{64}", version)


def test_version_ignores_key_order_and_formatting(document):
    reordered = json.loads(json.dumps(document, sort_keys=True))
    assert parse_catalog(reordered).version == parse_catalog(document).version


def test_version_changes_with_any_figure(document):
    changed = copy.deepcopy(document)
    changed["installations"]["sprinkler"]["price_leaves"] = 11
    assert parse_catalog(changed).version != parse_catalog(document).version


def test_entries_round_trip_to_the_same_catalog(document):
    catalog = parse_catalog(document)
    rebuilt = catalog_from_entries(list(catalog.entries))
    assert rebuilt == catalog
    assert rebuilt.version == catalog.version


def test_entries_with_another_kind_than_expected_are_rejected(document):
    entries = [
        CatalogEntry(e.key, "upgrade" if e.key == "drop" else e.kind, e.value)
        for e in parse_catalog(document).entries
    ]
    with pytest.raises(CatalogError, match="drop"):
        catalog_from_entries(entries)


def test_an_empty_catalog_is_rejected():
    with pytest.raises(CatalogError):
        catalog_from_entries([])


def mutate(document, path, value):
    changed = copy.deepcopy(document)
    target = changed
    for step in path[:-1]:
        target = target[step]
    if value is KeyError:
        del target[path[-1]]
    else:
        target[path[-1]] = value
    return changed


@pytest.mark.parametrize(
    ("path", "value"),
    [
        (("rules", "drop", "water_cost_liters"), KeyError),  # falta una cifra
        (("rules", "drop", "extra"), 1),  # cifra desconocida
        (("rules", "drop", "humidity_points"), 2.5),  # decimal
        (("rules", "drop", "humidity_points"), 25.0),  # decimal aunque sea entero
        (("rules", "drop", "humidity_points"), True),  # booleano
        (("rules", "drop", "humidity_points"), "25"),  # texto
        (("rules", "drop", "humidity_points"), None),  # nulo donde no se admite
        (("rules", "drop", "demo_humidity_points"), 1.5),  # decimal
        (("rules", "drop", "demo_humidity_points"), -1),  # negativo
        (("rules", "drop", "demo_humidity_points"), 0),  # la gota demo suma algo
        (("rules", "drop", "humidity_points"), -1),  # negativo
        (("rules", "tank", "refill_interval_seconds"), 2**32),  # no cabe en uint32
        (("rules", "tank", "refill_interval_seconds"), 0),  # intervalo cero
        (("rules", "growth", "humidity_per_stage_points"), 0),  # umbral cero
        (("rules", "fire", "ignition_chance_ppm"), 1_000_001),  # más de 100 %
        (("rules", "fire", "ignition_chance_ppm"), None),  # ya es obligatoria
        (("rules", "fire", "ignition_chance_ppm"), -1),  # negativa
        (("rules", "fire", "ignition_chance_ppm"), 0.5),  # decimal
        (("rules", "installation_price_increase_percent"), {"percent": 15}),
        (("rules", "drop"), 7),  # una cifra escalar donde va un grupo
        (("rules", "sprinkler"), {"price_leaves": 1}),  # elemento en el grupo equivocado
        (("installations", "sprinkler", "area_side_tiles"), 0),  # área vacía
        (("installations", "drip", "evaporation_divisor"), 0),  # división por cero
        (("upgrades",), []),  # grupo que no es objeto
        (("shop",), {}),  # grupo desconocido
        (("installations", "sprinkler"), KeyError),  # falta un elemento
        (("rules", "offline_installation_seconds"), 2**64),  # no cabe en uint64
    ],
)
def test_invalid_documents_are_rejected(document, path, value):
    with pytest.raises(CatalogError):
        parse_catalog(mutate(document, path, value))


@pytest.mark.parametrize("ppm", [0, 500, 1_000_000])
def test_the_ignition_chance_accepts_zero_up_to_a_million(document, ppm):
    with_value = mutate(document, ("rules", "fire", "ignition_chance_ppm"), ppm)
    catalog = parse_catalog(with_value)
    assert {e.key: e.value for e in catalog.entries}["fire"]["ignition_chance_ppm"] == ppm


def test_the_error_message_names_the_problem_but_not_the_value(document):
    with pytest.raises(CatalogError) as error:
        parse_catalog(mutate(document, ("rules", "drop", "humidity_points"), "secreto"))
    assert "drop.humidity_points" in str(error.value)
    assert "secreto" not in str(error.value)


def test_load_catalog_file_reads_the_repository_catalog():
    assert load_catalog_file(REPO_CATALOG).version == parse_catalog(
        json.loads(REPO_CATALOG.read_text(encoding="utf-8"))
    ).version


def test_load_catalog_file_reports_a_missing_file(tmp_path):
    with pytest.raises(CatalogError, match="no se pudo leer"):
        load_catalog_file(tmp_path / "no-existe.json")


def test_load_catalog_file_reports_invalid_json(tmp_path):
    path = tmp_path / "catalog.json"
    path.write_text("{ no es json", encoding="utf-8")
    with pytest.raises(CatalogError, match="JSON"):
        load_catalog_file(path)
