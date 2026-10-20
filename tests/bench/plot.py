#!/usr/bin/env python3
"""Gráfico de la comparativa Go vs. Rust (D-03), en SVG y solo con la biblioteca estándar.

Uso:

    python3 tests/bench/plot.py tests/bench/results/<fecha>-<máquina>.csv docs/informe/comparativa-go-rust.svg

Lee el CSV de `run.sh` y el `.json` con los datos de la máquina que está al
lado. Eje x: tamaño del valle en casillas; eje y: microsegundos por paso. Una
línea continua por lenguaje con la mediana y una discontinua con el p95. Con
varias ejecuciones por configuración se toma la mediana de las medianas y la
mediana de los p95. Los dos ejes son logarítmicos porque el rango es de 3
órdenes de magnitud; las dos líneas se leen en la misma escala.
"""

import csv
import json
import math
import statistics
import sys
from collections import defaultdict
from pathlib import Path

# Categórica 1 y 2 de la paleta de referencia (validadas en claro y oscuro).
SERIES = {
    "rust": {"label": "Rust", "light": "#2a78d6", "dark": "#3987e5"},
    "go": {"label": "Go", "light": "#eb6834", "dark": "#d95926"},
}


def load(csv_path):
    """Devuelve {(impl, lado): (mediana, p95, ejecuciones)} y las huellas por tamaño."""
    medians, p95s = defaultdict(list), defaultdict(list)
    hashes = defaultdict(set)
    with open(csv_path, newline="", encoding="utf-8") as handle:
        for row in csv.DictReader(handle):
            key = (row["impl"], int(row["width"]))
            medians[key].append(float(row["median_us"]))
            p95s[key].append(float(row["p95_us"]))
            hashes[int(row["width"]), row["run"]].add(row["state_fnv1a"])
    summary = {k: (statistics.median(medians[k]), statistics.median(p95s[k]), len(medians[k])) for k in medians}
    return summary, hashes


def machine_label(csv_path):
    meta = Path(csv_path).with_suffix(".json")
    if not meta.exists():
        return Path(csv_path).stem
    data = json.loads(meta.read_text(encoding="utf-8"))
    memory = data.get("memory_bytes", 0)
    ram = f", {round(memory / 2**30)} GB" if memory else ""
    return f'{data["cpu"]} ({data["logical_cores"]} núcleos{ram}), {data["os"]}, {data["arch"]}'


def fmt_us(value):
    if value >= 1000:
        return f"{value / 1000:.1f} ms".replace(".", ",")
    return f"{value:.0f} µs" if value >= 100 else f"{value:.1f} µs".replace(".", ",")


def render(summary, machine, runs):
    width, height = 860, 520
    left, right, top, bottom = 78, 150, 96, 80
    plot_w, plot_h = width - left - right, height - top - bottom
    sizes = sorted({size for (_, size) in summary})
    values = [v for (m, p, _) in summary.values() for v in (m, p)]
    y_min = 10 ** math.floor(math.log10(min(values)))
    y_max = 10 ** math.ceil(math.log10(max(values)))
    x_lo, x_hi = math.log10(sizes[0] ** 2), math.log10(sizes[-1] ** 2)

    def px(side):
        return left + (math.log10(side * side) - x_lo) / (x_hi - x_lo) * plot_w

    def py(value):
        return top + (1 - (math.log10(value) - math.log10(y_min)) / (math.log10(y_max) - math.log10(y_min))) * plot_h

    out = []
    add = out.append
    add(f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {width} {height}" width="{width}" height="{height}" '
        f'role="img" aria-labelledby="t d" font-family="system-ui, -apple-system, Segoe UI, sans-serif">')
    add("<title id=\"t\">Tiempo por paso del valle: Go frente a Rust</title>")
    parts = []
    for impl in ("rust", "go"):
        parts.append(f'{SERIES[impl]["label"]}: ' + ", ".join(
            f"{s}×{s} {fmt_us(summary[impl, s][0])}" for s in sizes))
    add(f'<desc id="d">Mediana de microsegundos por paso según el tamaño del valle. {"; ".join(parts)}.</desc>')
    add("<style>")
    add(":root{--surface:#fcfcfb;--ink:#0b0b0b;--ink2:#52514e;--grid:#e4e3df;"
        f"--rust:{SERIES['rust']['light']};--go:{SERIES['go']['light']}}}")
    add("@media (prefers-color-scheme: dark){:root{--surface:#1a1a19;--ink:#ffffff;--ink2:#c3c2b7;--grid:#383835;"
        f"--rust:{SERIES['rust']['dark']};--go:{SERIES['go']['dark']}}}}}")
    add(".t{fill:var(--ink)}.t2{fill:var(--ink2)}.g{stroke:var(--grid);stroke-width:1}"
        ".ax{stroke:var(--ink2);stroke-width:1}.ln{fill:none;stroke-width:2;stroke-linecap:round;stroke-linejoin:round}"
        ".p95{stroke-dasharray:6 4}.rust{stroke:var(--rust)}.go{stroke:var(--go)}"
        ".mrust{fill:var(--rust)}.mgo{fill:var(--go)}.ring{stroke:var(--surface);stroke-width:2}")
    add("</style>")
    add(f'<rect width="{width}" height="{height}" fill="var(--surface)"/>')
    add(f'<text class="t" x="{left}" y="30" font-size="17" font-weight="600">Tiempo de un paso del valle, Go frente a Rust</text>')
    add(f'<text class="t2" x="{left}" y="50" font-size="12">Microsegundos por paso (<tspan font-family="monospace">World::step</tspan> / '
        f'<tspan font-family="monospace">World.Step</tspan>), mediana de {runs} ejecuciones; menor es mejor</text>')
    add(f'<text class="t2" x="{left}" y="67" font-size="12">Máquina: {machine}</text>')

    # Leyenda (siempre presente con 2 series).
    lx = left
    for impl in ("rust", "go"):
        for dash, text in ((False, "mediana"), (True, "p95")):
            cls = f'ln {impl}' + (" p95" if dash else "")
            add(f'<line class="{cls}" x1="{lx}" y1="86" x2="{lx + 26}" y2="86"/>')
            add(f'<text class="t2" x="{lx + 32}" y="90" font-size="12">{SERIES[impl]["label"]}, {text}</text>')
            lx += 130

    # Rejilla y ejes.
    decade = y_min
    while decade <= y_max * 1.0001:
        y = py(decade)
        add(f'<line class="g" x1="{left}" y1="{y:.1f}" x2="{left + plot_w}" y2="{y:.1f}"/>')
        add(f'<text class="t2" x="{left - 8}" y="{y + 4:.1f}" font-size="11" text-anchor="end">{fmt_us(decade)}</text>')
        decade *= 10
    for size in sizes:
        x = px(size)
        add(f'<line class="g" x1="{x:.1f}" y1="{top}" x2="{x:.1f}" y2="{top + plot_h}"/>')
        add(f'<text class="t2" x="{x:.1f}" y="{top + plot_h + 18}" font-size="11" text-anchor="middle">{size}×{size}</text>')
        add(f'<text class="t2" x="{x:.1f}" y="{top + plot_h + 32}" font-size="11" text-anchor="middle">{f"{size * size:,}".replace(",", ".")}</text>')
    add(f'<line class="ax" x1="{left}" y1="{top + plot_h}" x2="{left + plot_w}" y2="{top + plot_h}"/>')
    add(f'<text class="t2" x="{left + plot_w / 2:.0f}" y="{height - 16}" font-size="12" text-anchor="middle">'
        "Tamaño del valle (casillas; ejes logarítmicos). El del juego es 32×32.</text>")

    # Series: p95 primero (debajo), luego la mediana.
    for impl in ("rust", "go"):
        for index, dash in ((1, True), (0, False)):
            points = " ".join(f"{px(s):.1f},{py(summary[impl, s][index]):.1f}" for s in sizes)
            add(f'<polyline class="ln {impl}{" p95" if dash else ""}" points="{points}"/>')
    for impl in ("rust", "go"):
        for s in sizes:
            add(f'<circle class="m{impl} ring" cx="{px(s):.1f}" cy="{py(summary[impl, s][0]):.1f}" r="4.5"/>')
    # Etiquetas directas al final de la mediana, separadas si chocan.
    ends = sorted(((py(summary[i, sizes[-1]][0]), i) for i in ("rust", "go")))
    last_y = -100
    for y, impl in ends:
        y = max(y, last_y + 15)
        last_y = y
        add(f'<text class="t" x="{left + plot_w + 10}" y="{y + 4:.1f}" font-size="12" font-weight="600">'
            f'{SERIES[impl]["label"]} {fmt_us(summary[impl, sizes[-1]][0])}</text>')
    add("</svg>")
    return "\n".join(out) + "\n"


def main(argv):
    if len(argv) != 3:
        print("uso: plot.py <resultados.csv> <salida.svg>", file=sys.stderr)
        return 2
    summary, hashes = load(argv[1])
    for (side, run), found in sorted(hashes.items()):
        if len(found) != 1:
            print(f"aviso: huellas distintas en {side}×{side}, ejecución {run}", file=sys.stderr)
    runs = max(r for (_, _, r) in summary.values())
    Path(argv[2]).parent.mkdir(parents=True, exist_ok=True)
    Path(argv[2]).write_text(render(summary, machine_label(argv[1]), runs), encoding="utf-8")
    print(f"gráfico: {argv[2]}")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
