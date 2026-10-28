#!/usr/bin/env python3
"""Genera los guiones de scenarios/*.json.

Los guiones son datos: Rust y Go los leen tal cual. Este programa solo los
escribe, de forma determinista (sin `random`; un generador xorshift propio),
para no teclear a mano miles de comandos. Si se cambia, hay que regenerar los
guiones y, después, los resultados esperados (ver README.md).

Uso, desde la raíz del repositorio:

    python3 tests/conformance/generate_scenarios.py
"""

import json
import pathlib

OUT = pathlib.Path(__file__).parent / "scenarios"

ANA = "6f1c1a52-2d0e-4f4e-9c1e-0c6c2d9b8a01"
BETO = "9a0b8c7d-6e5f-4a3b-8c2d-1e0f9a8b7c02"
CARO = "c3a5e2f0-1b4d-4c6e-8f7a-2d9b0c1e3f03"
DANI = "d4b6f3a1-2c5e-4d7f-9a8b-3e0c1d2f4a04"
ELI = "e5c7a4b2-3d6f-4e8a-8b9c-4f1d2e3a5b05"
FRAN = "f6d8b5c3-4e7a-4f9b-9cad-5a2e3f4b6c06"
ADMIN = "aaaaaaaa-0000-4000-8000-000000000001"
PLAYERS = {
    "ana": ANA,
    "beto": BETO,
    "caro": CARO,
    "dani": DANI,
    "eli": ELI,
    "fran": FRAN,
    "admin": ADMIN,
}


class Rng:
    """xorshift64*: la misma secuencia en cualquier versión de Python."""

    def __init__(self, seed):
        self.state = (seed * 0x9E3779B97F4A7C15 + 1) & (2**64 - 1)

    def next(self):
        x = self.state
        x ^= x >> 12
        x ^= (x << 25) & (2**64 - 1)
        x ^= x >> 27
        self.state = x
        return (x * 0x2545F4914F6CDD1D) & (2**64 - 1)

    def below(self, n):
        return (self.next() >> 11) % n

    def chance(self, percent):
        return self.below(100) < percent


class Scenario:
    def __init__(self, name, description, seed, total_ticks, catalog_set=None, reset_catalog=None, restore_at=()):
        self.name = name
        self.description = description
        self.seed = seed
        self.total_ticks = total_ticks
        self.catalog_set = catalog_set or {}
        self.reset_catalog = reset_catalog
        self.restore_at = list(restore_at)
        self.ticks = {}
        self.action = 0
        self.rng = Rng(seed ^ 0xC0FFEE)

    def next_action(self):
        self.action += 1
        return self.action

    def add(self, tick, command):
        assert 1 <= tick <= self.total_ticks, tick
        self.ticks.setdefault(tick, []).append(command)

    # Constructores de comandos. `action=None` toma un action_id nuevo.
    def cmd(self, player, action, kind=None):
        command = {"player": player, "action": self.next_action() if action is None else action}
        if kind:
            command.update(kind)
        return command

    def drop(self, tick, player, x, y, action=None):
        self.add(tick, self.cmd(player, action, {"drop": {"x": x, "y": y}}))

    def place(self, tick, player, kind, x, y, action=None):
        self.add(tick, self.cmd(player, action, {"place_installation": {"kind": kind, "x": x, "y": y}}))

    def upgrade(self, tick, player, kind, action=None):
        self.add(tick, self.cmd(player, action, {"buy_upgrade": {"kind": kind}}))

    def demo(self, tick, enabled, action=None, player="admin"):
        self.add(tick, self.cmd(player, action, {"set_demo_mode": {"enabled": enabled}}))

    def reset(self, tick, action=None, player="admin"):
        self.add(tick, self.cmd(player, action, {"reset_valley": {}}))

    def bare(self, tick, player, action=None):
        """Comando sin tipo: el simulador lo rechaza como INVALID_COMMAND."""
        self.add(tick, self.cmd(player, action))

    def write(self):
        document = {
            "description": self.description,
            "seed": self.seed,
            "catalog": {"file": "catalog/catalog.json", "version": "conformance-" + self.name, "set": self.catalog_set},
        }
        if self.reset_catalog is not None:
            document["reset_catalog"] = self.reset_catalog
        document["players"] = PLAYERS
        document["total_ticks"] = self.total_ticks
        document["restore_at"] = self.restore_at
        lines = ["{"]
        for key in ("description", "seed", "catalog", "reset_catalog", "players", "total_ticks", "restore_at"):
            if key in document:
                lines.append("  %s: %s," % (json.dumps(key), json.dumps(document[key], ensure_ascii=False)))
        lines.append('  "ticks": [')
        entries = [
            "    " + json.dumps({"tick": tick, "commands": self.ticks[tick]}, separators=(",", ":"))
            for tick in sorted(self.ticks)
        ]
        lines.append(",\n".join(entries))
        lines.append("  ]")
        lines.append("}")
        (OUT / (self.name + ".json")).write_text("\n".join(lines) + "\n")
        count = sum(len(c) for c in self.ticks.values())
        print("%-40s %5d ticks, %5d comandos" % (self.name, self.total_ticks, count))


def random_drops(s, players, first, last, region, per_tick_percent=60, burst=3):
    """Gotas al azar de los jugadores dados, dentro de region = (x0, y0, x1, y1)."""
    x0, y0, x1, y1 = region
    for tick in range(first, last + 1):
        for player in players:
            if s.rng.chance(per_tick_percent):
                for _ in range(1 + s.rng.below(burst)):
                    s.drop(tick, player, x0 + s.rng.below(x1 - x0 + 1), y0 + s.rng.below(y1 - y0 + 1))


# ---------------------------------------------------------------------------


def real_catalog():
    s = Scenario(
        "01-catalogo-real",
        "Catálogo real (catalog/catalog.json, sin cambios), semilla 2026. Seis jugadores hacen crecer una casilla propia "
        "hasta árbol (hojas por etapa), compran aspersores, intentan comprar goteo y mejoras sin hojas, chocan con casillas ocupadas, se quedan sin agua y "
        "sin hojas, y el administrador enciende el modo demo y reinicia el valle. Cubre gotas, humedad, crecimiento, "
        "evaporación, marchitamiento, depósito, precios, jugadores nuevos y orden de los jugadores, con una "
        "restauración a mitad del guion.",
        2026, 800, restore_at=[400])
    names = ["ana", "beto", "caro", "dani", "eli", "fran"]
    homes = {n: (4 + 5 * i, 4 + 3 * (i % 3)) for i, n in enumerate(names)}
    for i, name in enumerate(names):
        x, y = homes[name]
        for tick in range(1, 5):
            for _ in range(4):
                s.drop(tick + i % 2, name, x, y)  # 16 gotas: pasto, arbusto, flores y árbol
        s.place(6, name, "sprinkler", x + 1, y + 1)
    s.place(7, "ana", "drip", 20, 20)  # sin hojas suficientes
    s.place(7, "beto", "sprinkler", *homes["beto"])  # misma casilla de la planta: se puede, está libre de instalación
    s.place(8, "beto", "sprinkler", homes["beto"][0] + 1, homes["beto"][1] + 1)  # ocupada
    s.upgrade(9, "caro", "bigger_drop")  # sin hojas
    s.place(10, "dani", "treatment_plant", 28, 28)  # sin hojas
    random_drops(s, names, 30, 700, (0, 0, 31, 31), per_tick_percent=8, burst=2)
    for tick in (120, 260):
        for name in names[:3]:
            for _ in range(4):
                s.drop(tick, name, *homes[name])
    s.demo(300, True)
    random_drops(s, names[:4], 301, 340, (10, 10, 20, 20), per_tick_percent=70, burst=3)
    s.demo(350, False)
    s.reset(600)
    random_drops(s, names, 601, 800, (0, 0, 31, 31), per_tick_percent=10, burst=2)
    s.write()


def fires():
    s = Scenario(
        "02-incendios",
        "Probabilidad de ignición alta (20.000 ppm por casilla seca con planta y tick) y gotas de 40 de humedad, semilla 7. "
        "Tres jugadores plantan un campo de 8 x 4 casillas, que se seca y arde. Cubre incendios (aparición con el "
        "generador, tres gotas para apagar con su bono, aspersores que apagan, fuegos que se consumen en 300 ticks), el "
        "orden de los sorteos por filas, la gota más grande contra casillas que arden y las compras con las hojas del bono.",
        7, 700,
        catalog_set={
            "rules.fire.ignition_chance_ppm": 20000,
            "rules.drop.humidity_points": 40,
            "rules.growth.humidity_per_stage_points": 40,
            "rules.tank.capacity_liters": 60,
            "rules.tank.refill_liters": 3,
        })
    field = [(x, y) for y in range(2, 6) for x in range(2, 10)]
    owners = ["ana", "beto", "caro"]
    for i, (x, y) in enumerate(field):
        s.drop(1 + i // 6, owners[i % 3], x, y)
    for tick in (8, 9):
        for i, (x, y) in enumerate(field[:12]):  # algunas casillas suben más de una etapa
            s.drop(tick, owners[i % 3], x, y)
    s.place(12, "ana", "sprinkler", 3, 3)
    s.place(12, "beto", "sprinkler", 8, 5)
    s.upgrade(14, "caro", "bigger_drop")
    # Barrido de gotas sobre la mitad izquierda del campo; la derecha arde sin atender.
    for tick in range(15, 450):
        for player in owners:
            if s.rng.chance(35):
                s.drop(tick, player, 2 + s.rng.below(5), 2 + s.rng.below(4))
    s.upgrade(200, "ana", "bigger_tank")
    s.place(220, "beto", "drip", 6, 3)
    s.place(230, "caro", "treatment_plant", 0, 0)
    for tick in range(450, 700):
        if s.rng.chance(20):
            s.drop(tick, "ana", 2 + s.rng.below(8), 2 + s.rng.below(4))
    s.write()


def fire_spread():
    s = Scenario(
        "08-propagacion",
        "Propagación del fuego (B3): ignición baja (200 ppm) y propagación alta (150.000 ppm por casilla elegible con "
        "vecina ardiendo y tick), semilla 31. Tres jugadores plantan un campo denso de 12 x 6 casillas y dos "
        "esquinas del valle (0, 0) y (31, 31), con gotas de 40 de humedad que dejan cada planta en pasto seco; el "
        "marchitamiento queda lejos para que no pase a tierra seca y los fuegos se consumen en 60 ticks. Cubre un "
        "sorteo por casilla elegible con vecina ardiendo (orden por filas, antes de la ignición), la 4-vecindad "
        "ortogonal sin diagonales ni desbordes en los bordes, el avance de un solo paso por tick, fuegos apagados con "
        "tres gotas en pleno avance, un aspersor cuya humedad deja fuera del fuego a su zona y una restauración con "
        "el frente encendido.",
        31, 400,
        catalog_set={
            "rules.fire.ignition_chance_ppm": 200,
            "rules.fire.propagation_chance_ppm": 150000,
            "rules.fire.burn_out_ticks": 60,
            "rules.drop.humidity_points": 40,
            "rules.growth.humidity_per_stage_points": 40,
            "rules.evaporation.wither_after_ticks": 100000,
            "rules.tank.capacity_liters": 200,
            "rules.tank.refill_liters": 5,
            "installations.sprinkler.price_leaves": 1,
        },
        restore_at=[200])
    owners = ["ana", "beto", "caro"]
    field = [(x, y) for y in range(10, 16) for x in range(10, 22)]
    corners = [(0, 0), (1, 0), (0, 1), (31, 31), (30, 31), (31, 30)]
    for i, (x, y) in enumerate(field + corners):
        s.drop(1 + i // 12, owners[i % 3], x, y)
    s.place(10, "ana", "sprinkler", 11, 11)
    # Tres gotas seguidas de un mismo jugador sobre una casilla del campo: si
    # arde, la apagan (con su bono); si no, la humedecen y la hacen crecer.
    for tick in range(150, 260):
        for player in owners:
            if s.rng.chance(6):
                x, y = 10 + s.rng.below(12), 10 + s.rng.below(6)
                for _ in range(3):
                    s.drop(tick, player, x, y)
    s.write()


def demo_and_resets():
    after = {
        "file": "catalog/catalog.json",
        "version": "conformance-despues-del-reinicio",
        "set": {
            "rules.tank.capacity_liters": 30,
            "rules.tank.refill_liters": 2,
            "rules.growth.leaves_tree": 12,
            "rules.fire.ignition_chance_ppm": 30000,
            "installations.sprinkler.price_leaves": 7,
            "rules.installation_price_increase_percent": 20,
        },
    }
    s = Scenario(
        "03-demo-y-reinicios",
        "Modo demo, reinicios del valle y épocas, semilla 99. El administrador enciende el modo demo (gotas gratis de 3 de "
        "humedad, de una sola casilla aunque haya mejora), lo apaga, y reinicia el valle tres veces: el primero adopta el "
        "catálogo nuevo que se ofrece al reinicio (otras cifras, otra versión y más hojas por árbol), el segundo es un duplicado y no cambia "
        "nada, el tercero llega de un jugador cualquiera. Cubre época, contadores de RF-27 y de las 3R a cero, "
        "humidity_contributed que solo crece, jugadores que se conservan con su ventana de action_id, y el generador "
        "que no se toca. Restaura a mitad del guion (tras el primer reinicio).",
        99, 500,
        catalog_set={"rules.drop.demo_humidity_points": 3, "rules.fire.ignition_chance_ppm": 30000},
        reset_catalog=after, restore_at=[250])
    names = ["ana", "beto", "caro", "dani"]
    for i, name in enumerate(names):
        for tick in range(1, 5):
            for _ in range(4):
                s.drop(tick, name, 2 + 3 * i, 2)  # 16 gotas: árbol con humedad 0, listo para arder
        s.place(8, name, "sprinkler", 3 + 3 * i, 3)
    s.upgrade(9, "ana", "bigger_tank")
    s.demo(20, True, action=9001)
    random_drops(s, names, 21, 140, (0, 0, 15, 15), per_tick_percent=50, burst=4)
    s.demo(21, True, action=9001)  # duplicado: el modo demo ya estaba encendido con ese action_id
    s.reset(150, action=9100)
    s.reset(151, action=9100)  # duplicado: no cambia nada ni vuelve a adoptar el catálogo
    random_drops(s, names + ["eli"], 152, 199, (0, 0, 31, 31), per_tick_percent=50, burst=3)
    s.demo(200, False, action=9002)
    for i, name in enumerate(names):
        for tick in range(201, 205):
            for _ in range(4):
                s.drop(tick, name, 2 + 3 * i, 2)
    s.place(210, "ana", "sprinkler", 12, 12)
    s.place(211, "ana", "sprinkler", 14, 12)
    random_drops(s, names + ["eli"], 212, 330, (0, 0, 31, 31), per_tick_percent=15, burst=3)
    s.reset(331, action=9101, player="beto")
    s.reset(332, action=9102, player="admin")
    s.demo(333, True, action=9003)
    random_drops(s, names, 334, 400, (5, 5, 25, 25), per_tick_percent=60, burst=4)
    s.demo(401, False, action=9004)
    random_drops(s, names + ["fran"], 402, 500, (0, 0, 31, 31), per_tick_percent=10, burst=2)
    s.write()


def invalid_and_duplicates():
    s = Scenario(
        "04-invalidos-y-duplicados",
        "Comandos mal formados, rechazados y repetidos, con el catálogo real y semilla 31. Cubre identificadores que no son "
        "UUID (formas simple, con llaves, urn y en mayúsculas, válidas; sin guiones donde van, con letras que no son "
        "hexadecimales, vacíos, de otro largo, inválidas), comandos sin tipo, tipos de instalación y de mejora "
        "desconocidos o sin especificar, coordenadas fuera del valle (incluida la mayor de 32 bits), agua y hojas "
        "insuficientes, casilla ocupada, mejora ya comprada, duplicados en el mismo tick y en otro, un rechazo que se "
        "reenvía con el mismo action_id y se aplica, y la ventana de 600 ticks de action_id: un action_id aplicado en "
        "el tick 1 sigue siendo duplicado en el 601 y se vuelve a aplicar en el 602.",
        31, 700)
    # Jugadores con formas válidas de UUID (el simulador solo comprueba que lo sean).
    forms = {
        "simple": "0123456789abcdef0123456789abcdef",
        "llaves": "{01234567-89ab-cdef-0123-456789abcdef}",
        "urn": "urn:uuid:01234567-89ab-cdef-0123-456789abcdef",
        "mayusculas": "01234567-89AB-CDEF-0123-456789ABCDEF",
    }
    bad = {
        "sin-guiones-donde-van": "0123456789abcdef-0123-4567-89ab-cdef0123",
        "no-hex": "0123456g-89ab-cdef-0123-456789abcdef",
        "vacio": "",
        "corto": "0123",
        "largo": "01234567-89ab-cdef-0123-456789abcdef0",
        "llave-sin-cerrar": "{01234567-89ab-cdef-0123-456789abcdef",
        "urn-en-mayusculas": "URN:UUID:01234567-89ab-cdef-0123-456789abcdef",
        "urn-sin-nombre": "urn:uuid:0123456789abcdef0123456789abcdef",
        "espacio": " 01234567-89ab-cdef-0123-456789abcdef",
        "ocho-guiones": "0123-4567-89ab-cdef-0123-4567-89ab-cdef",
    }
    # 1) Un jugador y una acción por cada forma válida de UUID.
    for i, (label, text) in enumerate(forms.items()):
        s.add(1, {"player": text, "action": text, "drop": {"x": i, "y": 0}})
        s.add(2, {"player": text, "action": text, "drop": {"x": i, "y": 0}})  # duplicado, la misma cadena
    s.add(2, {"player": ANA, "action": forms["mayusculas"].lower(), "drop": {"x": 9, "y": 9}})  # otra cadena: no es duplicado
    # 2) Inválidos: ni el jugador ni la acción.
    for i, (label, text) in enumerate(bad.items()):
        s.add(3, {"player": ANA, "action": text, "drop": {"x": i, "y": 1}})
        s.add(3, {"player": text, "action": s.next_action(), "drop": {"x": i, "y": 1}})
    # 3) Sin tipo, enums desconocidos.
    s.bare(4, "ana")
    s.add(4, {"player": "ana", "action": s.next_action(), "place_installation": {"kind": 0, "x": 1, "y": 1}})
    s.add(4, {"player": "ana", "action": s.next_action(), "place_installation": {"kind": 99, "x": 1, "y": 1}})
    s.add(4, {"player": "ana", "action": s.next_action(), "buy_upgrade": {"kind": 0}})
    s.add(4, {"player": "ana", "action": s.next_action(), "buy_upgrade": {"kind": 7}})
    s.add(4, {"player": "ana", "action": s.next_action(), "place_installation": {"x": 1, "y": 1}})
    # 4) Fuera del valle.
    for tick, (x, y) in enumerate([(32, 0), (0, 32), (32, 32), (4294967295, 0), (0, 4294967295), (31, 31)], start=5):
        s.drop(tick, "ana", x, y)
    s.place(11, "ana", "sprinkler", 32, 5)
    s.place(11, "ana", "drip", 5, 4294967295)
    # 5) Agua: vaciar el depósito (20 L), el 21 se rechaza, y se reenvía igual más tarde.
    for _ in range(20):
        s.drop(12, "beto", 10, 10)
    retry = s.next_action()
    s.drop(12, "beto", 10, 11, action=retry)  # rechazada: sin agua
    s.drop(13, "beto", 10, 11, action=retry)  # sigue sin agua (mismo action_id)
    s.drop(45, "beto", 10, 11, action=retry)  # con agua recargada, se aplica
    s.drop(46, "beto", 10, 11, action=retry)  # ahora sí es duplicado
    # 6) Hojas, casilla ocupada y mejora repetida.
    for tick in range(14, 18):
        for _ in range(4):
            s.drop(tick, "caro", 20, 20)  # 16 gotas: 15 hojas
    s.place(18, "caro", "sprinkler", 20, 21)
    s.place(19, "caro", "sprinkler", 20, 21)  # ocupada
    s.place(20, "caro", "drip", 20, 21)  # ocupada o sin hojas
    s.upgrade(21, "caro", "bigger_drop")  # sin hojas
    for tick in range(46, 52):
        for _ in range(3):
            s.drop(tick, "dani", 25, 25)
    s.upgrade(60, "dani", "bigger_tank")
    s.upgrade(61, "dani", "bigger_tank")  # ya la tiene o sin hojas
    # 7) Duplicados en el mismo tick y en otro, de un jugador que no existía.
    s.add(70, {"player": "fran", "action": 5000, "drop": {"x": 3, "y": 3}})
    s.add(70, {"player": "fran", "action": 5000, "drop": {"x": 3, "y": 3}})
    s.add(70, {"player": "eli", "action": 5000, "drop": {"x": 3, "y": 3}})  # el mismo action_id, otro jugador: no es duplicado
    s.add(71, {"player": "fran", "action": 5000, "drop": {"x": 3, "y": 3}})
    # 8) La ventana de 600 ticks: aplicado en el 1, duplicado en el 601, otra vez aplicado en el 602.
    s.drop(1, "ana", 7, 7, action=6000)
    s.drop(600, "ana", 7, 7, action=6000)
    s.drop(601, "ana", 7, 7, action=6000)
    s.drop(602, "ana", 7, 7, action=6000)
    s.drop(603, "ana", 7, 7, action=6000)
    # 9) Administración con identificadores inválidos y duplicada.
    s.add(80, {"player": ADMIN, "action": "no-es-uuid", "set_demo_mode": {"enabled": True}})
    s.add(80, {"player": "no-es-uuid", "action": s.next_action(), "reset_valley": {}})
    s.demo(81, True, action=7000)
    s.demo(82, True, action=7000)
    s.demo(83, False, action=7001)
    s.reset(300, action=7100)
    s.reset(300, action=7100)
    random_drops(s, ["ana", "beto"], 310, 560, (0, 0, 31, 31), per_tick_percent=6, burst=2)
    s.write()


def custom_figures():
    s = Scenario(
        "05-cifras-propias",
        "Cifras no estándar y economía fuerte, semilla 4242. Gotas de 50 de humedad y etapas de 50, depósito de 200 L, "
        "aspersores de 5 x 5, goteo de 7 x 7 con divisor 3, planta de 4 L por segundo, precios más bajos con un 25 % de "
        "aumento por compra, gota más grande de 5 x 5, hojas por etapa 2, 3, 5 y 9, instalaciones que solo trabajan 30 s "
        "tras la última acción de su dueño e ignición baja. Ana compra muchas instalaciones y se ausenta en el tick 200: "
        "sus instalaciones dejan de funcionar a los 300 ticks; Beto sigue activo. Cubre precios escalonados, mejoras, "
        "las tres instalaciones con sus áreas recortadas por los bordes, dueños inactivos (RF-25) y dos "
        "restauraciones.",
        4242, 1000,
        catalog_set={
            "rules.drop.humidity_points": 50,
            "rules.growth.humidity_per_stage_points": 50,
            "rules.growth.leaves_grass": 2,
            "rules.growth.leaves_shrub": 3,
            "rules.growth.leaves_flowers": 5,
            "rules.growth.leaves_tree": 9,
            "rules.growth.tree_max_humidity_points": 120,
            "rules.tank.capacity_liters": 200,
            "rules.tank.refill_liters": 5,
            "rules.tank.refill_interval_seconds": 1,
            "rules.evaporation.humidity_per_second_points": 3,
            "rules.evaporation.wither_after_ticks": 40,
            "rules.fire.ignition_chance_ppm": 2000,
            "rules.fire.extinguish_bonus_leaves": 9,
            "rules.installation_price_increase_percent": 25,
            "rules.offline_installation_seconds": 30,
            "installations.sprinkler.price_leaves": 6,
            "installations.sprinkler.area_side_tiles": 5,
            "installations.sprinkler.humidity_points": 30,
            "installations.sprinkler.interval_seconds": 1,
            "installations.drip.price_leaves": 9,
            "installations.drip.area_side_tiles": 7,
            "installations.drip.evaporation_divisor": 3,
            "installations.treatment_plant.price_leaves": 12,
            "installations.treatment_plant.refill_liters": 4,
            "installations.treatment_plant.interval_seconds": 1,
            "upgrades.bigger_drop.price_leaves": 8,
            "upgrades.bigger_drop.area_side_tiles": 5,
            "upgrades.bigger_tank.price_leaves": 10,
            "upgrades.bigger_tank.extra_capacity_liters": 25,
        },
        restore_at=[300, 700])
    # Ana: 40 gotas en 10 casillas = 190 hojas; luego compras escalonadas.
    for i in range(10):
        for _ in range(4):
            s.drop(1 + i // 3, "ana", 1 + 3 * (i % 5), 1 + 3 * (i // 5))
    sites = [(0, 0), (31, 0), (0, 31), (31, 31), (15, 15), (16, 15), (15, 16), (8, 8), (24, 8), (8, 24)]
    for i, (x, y) in enumerate(sites):
        s.place(10 + i, "ana", "sprinkler", x, y)  # 6, 8, 10, 13, 16, 20, 25... (+25 %, redondeo hacia arriba)
    s.upgrade(21, "ana", "bigger_drop")
    s.upgrade(22, "ana", "bigger_tank")
    s.upgrade(23, "ana", "bigger_tank")  # ya la tiene
    s.place(24, "ana", "drip", 12, 12)
    s.place(25, "ana", "treatment_plant", 20, 20)
    # Beto: más barato y activo todo el guion.
    for i in range(8):
        for _ in range(4):
            s.drop(1 + i // 3, "beto", 20 + 2 * (i % 4), 2 + 3 * (i // 4))
    for i, (x, y) in enumerate([(22, 4), (24, 4), (26, 4), (22, 7), (24, 7)]):
        s.place(12 + i, "beto", "sprinkler", x, y)
    s.place(30, "beto", "drip", 25, 5)
    s.place(31, "beto", "treatment_plant", 28, 28)
    s.upgrade(32, "beto", "bigger_drop")
    # Caro llega tarde y se queda sin hojas.
    for tick in range(100, 105):
        for _ in range(4):
            s.drop(tick, "caro", 28, 12)
    s.place(110, "caro", "sprinkler", 28, 13)
    s.place(111, "caro", "sprinkler", 29, 13)
    s.place(112, "caro", "sprinkler", 30, 13)
    # Ana actúa hasta el tick 200 y desaparece; Beto y Caro siguen.
    random_drops(s, ["ana"], 33, 200, (0, 0, 31, 31), per_tick_percent=30, burst=3)
    random_drops(s, ["beto", "caro"], 33, 1000, (0, 0, 31, 31), per_tick_percent=25, burst=3)
    s.write()


def restores_with_fire():
    s = Scenario(
        "06-restauraciones-con-fuego",
        "Cinco restauraciones desde una instantánea a lo largo de 800 ticks (en los ticks 100, 250, 400, 550 y 700), con "
        "incendios (10.000 ppm), semilla de 64 bits cercana al máximo, instalaciones, mejoras y un reinicio del valle "
        "en el medio. Debe dar lo mismo que sin restaurar: el generador sigue la misma secuencia, la ventana de "
        "action_id se conserva y los fuegos en curso siguen contando. Las instantáneas de cada restauración quedan "
        "en el archivo esperado, y Go las retoma desde ahí.",
        18446744073709551557, 800,
        catalog_set={
            "rules.fire.ignition_chance_ppm": 10000,
            "rules.drop.humidity_points": 50,
            "rules.growth.humidity_per_stage_points": 50,
            "rules.tank.capacity_liters": 80,
            "rules.tank.refill_liters": 2,
            "rules.evaporation.wither_after_ticks": 60,
        },
        restore_at=[100, 250, 400, 550, 700])
    names = ["ana", "beto", "caro", "dani"]
    for i, name in enumerate(names):
        for tick in range(1, 5):
            for _ in range(4):
                s.drop(tick, name, 3 + 4 * i, 3)
                s.drop(tick, name, 3 + 4 * i, 4)
        s.place(8, name, "sprinkler", 4 + 4 * i, 5)
        s.place(9, name, "drip", 5 + 4 * i, 8)
    s.upgrade(12, "ana", "bigger_drop")
    s.upgrade(12, "beto", "bigger_tank")
    random_drops(s, names, 13, 800, (0, 0, 20, 12), per_tick_percent=30, burst=3)
    s.reset(450, action=9900)
    for tick in range(451, 456):
        for name in names:
            for _ in range(4):
                s.drop(tick, name, 6, 6)
    s.place(460, "ana", "sprinkler", 6, 7)
    s.place(461, "caro", "treatment_plant", 12, 12)
    s.write()


def extreme_figures():
    big = 18446744073709551615
    s = Scenario(
        "07-cifras-extremas",
        "Cifras en el límite de 64 bits sin signo (regla 7), semilla 5. Humedad, hojas, agua, precios y áreas llegan a "
        "u64::MAX o u32::MAX, así que toda la aritmética satura en vez de desbordar: gotas que suman u64::MAX de "
        "humedad, depósitos y recargas enormes, un 4.294.967.295 % de aumento por compra, áreas de 4.294.967.295 "
        "casillas de lado recortadas por los bordes, ignición segura (ppm mayor que un millón) y un incendio que se "
        "apaga con una gota. Si Rust y Go saturan igual, el estado es el mismo.",
        5, 400,
        catalog_set={
            "rules.drop.humidity_points": big,
            "rules.drop.demo_humidity_points": big,
            "rules.tank.capacity_liters": big,
            "rules.tank.refill_liters": big,
            "rules.tank.refill_interval_seconds": 4294967295,
            "rules.growth.humidity_per_stage_points": 4611686018427387904,
            "rules.growth.leaves_grass": big - 5,
            "rules.growth.leaves_shrub": 1000,
            "rules.growth.leaves_flowers": big,
            "rules.growth.leaves_tree": big,
            "rules.growth.tree_max_humidity_points": big,
            "rules.evaporation.humidity_per_second_points": big,
            "rules.evaporation.wither_after_ticks": 1,
            "rules.fire.drops_to_extinguish": 1,
            "rules.fire.extinguish_bonus_leaves": big,
            "rules.fire.burn_out_ticks": 4294967295,
            "rules.fire.ignition_chance_ppm": 4294967295,
            "rules.installation_price_increase_percent": 4294967295,
            "rules.offline_installation_seconds": big,
            "installations.sprinkler.price_leaves": 4000000000000000000,
            "installations.sprinkler.area_side_tiles": 4294967295,
            "installations.sprinkler.humidity_points": big,
            "installations.sprinkler.interval_seconds": 1,
            "installations.drip.price_leaves": big // 3,
            "installations.drip.area_side_tiles": 4294967295,
            "installations.drip.evaporation_divisor": 4294967295,
            "installations.treatment_plant.price_leaves": 1,
            "installations.treatment_plant.refill_liters": big,
            "installations.treatment_plant.interval_seconds": 4294967295,
            "upgrades.bigger_drop.price_leaves": 1,
            "upgrades.bigger_drop.area_side_tiles": 4294967295,
            "upgrades.bigger_tank.price_leaves": 2,
            "upgrades.bigger_tank.extra_capacity_liters": big,
        },
        restore_at=[200])
    names = ["ana", "beto", "caro"]
    for tick in range(1, 40):
        for i, name in enumerate(names):
            s.drop(tick, name, (tick * 3 + i * 11) % 32, (tick + i * 7) % 32)
    s.place(41, "ana", "treatment_plant", 5, 5)
    s.upgrade(42, "ana", "bigger_tank")
    s.place(43, "ana", "sprinkler", 10, 10)
    s.place(44, "ana", "sprinkler", 12, 10)
    s.place(45, "ana", "drip", 14, 10)
    s.place(46, "beto", "treatment_plant", 6, 6)
    s.place(47, "beto", "treatment_plant", 7, 6)
    s.place(48, "beto", "treatment_plant", 8, 6)
    s.upgrade(49, "beto", "bigger_drop")
    s.upgrade(50, "caro", "bigger_drop")
    s.drop(51, "caro", 0, 0)  # con la gota más grande: casillas de lado 4.294.967.295 recortadas por el valle
    s.drop(52, "beto", 31, 31)
    s.demo(53, True)
    s.drop(54, "ana", 16, 16)  # en modo demo la gota es de una casilla
    s.demo(55, False)
    random_drops(s, names, 56, 399, (0, 0, 31, 31), per_tick_percent=40, burst=3)
    s.reset(300, action=9500)
    s.write()


def offline_summary():
    s = Scenario(
        "09-resumen-offline",
        "Resumen al volver (B6, RF-25): ignición alta (30.000 ppm por casilla seca con planta y tick), marchitamiento "
        "lejano e instalaciones que solo trabajan 20 s tras la última acción de su dueño, semilla 55. Ana planta un "
        "bloque de 3 x 3 casillas, coloca un aspersor en el centro (tick 10) y se ausenta: el aspersor apaga los fuegos "
        "que prenden en su zona antes del primer riego, hace crecer las casillas y se para a los 200 ticks, con su "
        "resumen sin borrar. Caro hace lo mismo en otra esquina, pero vuelve con una gota en el tick 250, que borra su "
        "resumen y reactiva su aspersor. Beto sigue activo todo el guion con goteo, planta de tratamiento y un "
        "aspersor sin fuegos que apagar: sus acumulados no cuentan fuegos y su resumen se borra con cada acción. "
        "Cubre la acumulación y el borrado del resumen, el acumulado de la época, el aspersor que se para y se "
        "reactiva, que el goteo y la planta no aportan al resumen, y su ida y vuelta por la instantánea (dos "
        "restauraciones).",
        55, 500,
        catalog_set={
            "rules.drop.humidity_points": 25,
            "rules.fire.ignition_chance_ppm": 30000,
            "rules.fire.propagation_chance_ppm": 0,
            "rules.evaporation.wither_after_ticks": 100000,
            "rules.offline_installation_seconds": 20,
            "rules.tank.capacity_liters": 80,
            "rules.tank.refill_liters": 4,
            "installations.sprinkler.price_leaves": 4,
            "installations.drip.price_leaves": 4,
            "installations.treatment_plant.price_leaves": 4,
        },
        restore_at=[150, 300])
    blocks = {"ana": (10, 10), "caro": (20, 20), "beto": (4, 24)}
    for tick, owner in enumerate(["ana", "caro", "beto"], start=1):
        x0, y0 = blocks[owner]
        for dx in range(3):
            for dy in range(3):
                for _ in range(4):  # cuatro gotas de 25 = una etapa; queda en pasto seco
                    s.drop(tick, owner, x0 + dx, y0 + dy)
    s.place(10, "ana", "sprinkler", 11, 11)
    s.place(11, "caro", "sprinkler", 21, 21)
    s.place(12, "beto", "drip", 5, 25)
    s.place(13, "beto", "treatment_plant", 0, 31)
    s.place(14, "beto", "sprinkler", 31, 0)
    # Beto actúa cada 15 ticks durante todo el guion; Ana no vuelve; Caro vuelve en el 250.
    for tick in range(20, 500, 15):
        s.drop(tick, "beto", 4 + s.rng.below(3), 24 + s.rng.below(3))
    s.drop(250, "caro", 20, 20)
    s.drop(420, "caro", 22, 22)
    s.write()


if __name__ == "__main__":
    OUT.mkdir(parents=True, exist_ok=True)
    real_catalog()
    fires()
    demo_and_resets()
    invalid_and_duplicates()
    custom_figures()
    restores_with_fire()
    extreme_figures()
    fire_spread()
    offline_summary()
