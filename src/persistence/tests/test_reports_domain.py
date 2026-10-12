"""Pruebas de la lógica pura de los reportes (RF-12): periodo y totales."""

import pytest

from persistence.domain import InvalidReportPeriodError, ReportTotals
from persistence.reports import resolve_period

TICKS = [100, 150, 200, 250]  # instantáneas de la época actual


def test_default_period_is_epoch_start_to_latest():
    assert resolve_period(TICKS, None, None) == (None, 250)


def test_zero_from_tick_means_epoch_start():
    assert resolve_period(TICKS, 0, None) == (None, 250)


def test_ticks_resolve_to_the_snapshot_at_or_before():
    assert resolve_period(TICKS, 160, 249) == (150, 200)


def test_exact_ticks_use_that_very_snapshot():
    assert resolve_period(TICKS, 150, 200) == (150, 200)


def test_to_tick_after_the_latest_uses_the_latest():
    assert resolve_period(TICKS, None, 10_000) == (None, 250)


def test_from_tick_after_the_latest_gives_an_empty_period():
    assert resolve_period(TICKS, 9_000, None) == (250, 250)


def test_to_tick_before_the_first_snapshot_of_the_epoch_is_invalid():
    with pytest.raises(InvalidReportPeriodError):
        resolve_period(TICKS, None, 99)


def test_from_tick_before_the_first_snapshot_of_the_epoch_is_invalid():
    with pytest.raises(InvalidReportPeriodError):
        resolve_period(TICKS, 50, None)


def test_from_tick_greater_than_to_tick_is_invalid():
    with pytest.raises(InvalidReportPeriodError):
        resolve_period(TICKS, 200, 150)


def totals(**kwargs) -> ReportTotals:
    return ReportTotals(**{**dict.fromkeys(ReportTotals.__dataclass_fields__, 0), **kwargs})


def test_minus_subtracts_field_by_field():
    result = totals(water_reduced=10, leaves_earned=7).minus(totals(water_reduced=4))
    assert result == totals(water_reduced=6, leaves_earned=7)


def test_minus_never_goes_below_zero():
    assert totals(water_reused=1).minus(totals(water_reused=5)) == totals()
