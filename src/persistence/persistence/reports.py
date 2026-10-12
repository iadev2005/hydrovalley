"""Lógica pura de los reportes de las 3R (RF-12).

No hay tabla de reportes: salen de las filas de `snapshot_players` de las
instantáneas de la época actual (la del último tick; cada reinicio del valle
sube la época y pone los contadores a cero, RF-31).

Elección de instantáneas ("en o antes de"): un tick pedido se resuelve a la
instantánea de mayor tick que sea menor o igual a él, dentro de la época
actual. Las instantáneas se toman cada 50 ticks, así que el reporte puede ir
hasta 5 s por detrás del valle; con la retención de instantáneas, un tick muy
antiguo puede resolverse a una anterior.
"""

from bisect import bisect_right
from collections.abc import Sequence

from persistence.domain import InvalidReportPeriodError


def resolve_period(
    epoch_ticks: Sequence[int], from_tick: int | None, to_tick: int | None
) -> tuple[int | None, int]:
    """Elige las instantáneas de un periodo.

    epoch_ticks son los ticks de las instantáneas de la época actual, en
    orden creciente y no vacíos. Devuelve (instantánea_inicial, instantánea_final):
    la inicial es None cuando el periodo arranca con los contadores a cero (sin
    from_tick, o from_tick = 0).

    Lanza InvalidReportPeriodError si from_tick > to_tick, si to_tick es
    anterior a la primera instantánea de la época o si from_tick > 0 lo es.
    """
    if from_tick is not None and to_tick is not None and from_tick > to_tick:
        raise InvalidReportPeriodError("from_tick es mayor que to_tick")
    if to_tick is None:
        end = epoch_ticks[-1]
    else:
        end = _at_or_before(epoch_ticks, to_tick, "to_tick")
    if not from_tick:
        return None, end
    return _at_or_before(epoch_ticks, from_tick, "from_tick"), end


def _at_or_before(epoch_ticks: Sequence[int], tick: int, name: str) -> int:
    index = bisect_right(epoch_ticks, tick) - 1
    if index < 0:
        raise InvalidReportPeriodError(f"{name} es anterior a la época actual")
    return epoch_ticks[index]
