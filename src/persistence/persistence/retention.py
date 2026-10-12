"""Regla de retención de instantáneas (Capa 5 y diagrama 07).

Se conservan todas las instantáneas de la última hora (720) y una por hora
de las anteriores; el resto se borra. Todo se cuenta en ticks, porque el
tick es el reloj lógico (Capa 4):

- "Última hora": ticks mayores que el último tick menos 36.000.
- "Una por hora": las anteriores se agrupan por hora de tick
  (tick // 36.000) y de cada grupo se queda la de menor tick. Como el tick
  solo crece, esa elegida no cambia nunca y la poda es idempotente.
"""

from collections.abc import Iterable

TICKS_PER_SECOND = 10
TICKS_PER_HOUR = 3600 * TICKS_PER_SECOND
SNAPSHOT_INTERVAL_TICKS = 50


def ticks_to_prune(ticks: Iterable[int]) -> set[int]:
    """Devuelve los ticks que la retención manda borrar."""
    all_ticks = set(ticks)
    if not all_ticks:
        return set()
    recent_after = max(all_ticks) - TICKS_PER_HOUR
    earliest_per_hour: dict[int, int] = {}
    for tick in all_ticks:
        if tick <= recent_after:
            hour = tick // TICKS_PER_HOUR
            earliest_per_hour[hour] = min(tick, earliest_per_hour.get(hour, tick))
    kept_older = set(earliest_per_hour.values())
    return {t for t in all_ticks if t <= recent_after and t not in kept_older}
