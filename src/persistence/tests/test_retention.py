"""Pruebas de la regla de retención de instantáneas (Capa 5)."""

from persistence.retention import SNAPSHOT_INTERVAL_TICKS, TICKS_PER_HOUR, ticks_to_prune


def every_snapshot(first: int, last: int) -> list[int]:
    """Ticks de instantánea (múltiplos de 50) entre first y last, ambos incluidos."""
    return list(range(first, last + 1, SNAPSHOT_INTERVAL_TICKS))


def test_constants_match_the_document():
    # 10 ticks por segundo: una hora son 36.000 ticks y 720 instantáneas.
    assert TICKS_PER_HOUR == 36_000
    assert TICKS_PER_HOUR // SNAPSHOT_INTERVAL_TICKS == 720


def test_empty_table_prunes_nothing():
    assert ticks_to_prune([]) == set()


def test_keeps_every_snapshot_of_the_last_hour():
    ticks = every_snapshot(50, TICKS_PER_HOUR)  # 720 instantáneas
    assert len(ticks) == 720
    assert ticks_to_prune(ticks) == set()


def test_keeps_one_per_older_hour_and_prunes_the_rest():
    latest = 3 * TICKS_PER_HOUR
    ticks = every_snapshot(0, latest)
    pruned = ticks_to_prune(ticks)
    kept = set(ticks) - pruned
    recent = {t for t in ticks if t > latest - TICKS_PER_HOUR}
    assert len(recent) == 720
    # Una por hora anterior: la primera de cada hora (0, 36.000 y 72.000).
    assert kept == recent | {0, TICKS_PER_HOUR, 2 * TICKS_PER_HOUR}


def test_keeps_the_earliest_snapshot_of_each_older_hour_with_gaps():
    latest = 10 * TICKS_PER_HOUR
    ticks = [100, 150, 36_050, 40_000, 71_950, latest]
    kept = set(ticks) - ticks_to_prune(ticks)
    assert kept == {100, 36_050, latest}


def test_pruning_is_idempotent():
    ticks = every_snapshot(0, 5 * TICKS_PER_HOUR)
    remaining = set(ticks) - ticks_to_prune(ticks)
    assert ticks_to_prune(remaining) == set()


def test_works_with_the_full_u64_range():
    latest = 2**64 - 1
    hour_start = (latest // TICKS_PER_HOUR - 3) * TICKS_PER_HOUR
    ticks = [hour_start, hour_start + 50, latest]
    assert set(ticks) - ticks_to_prune(ticks) == {hour_start, latest}
