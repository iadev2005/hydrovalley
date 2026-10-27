#!/usr/bin/env python3
"""
Pipeline de Generación Reproducible de Sprites (Fase 4 - Hydrovalley)
Permite regenerar determinísticamente los 10 nuevos sprites del Catálogo Técnico (Sección 3)
directamente desde código fuente, asegurando reproducibilidad y trazabilidad en el repositorio.
"""

import os
import sys
import argparse
from PIL import Image

ROOT_DIR = os.path.abspath(os.path.join(os.path.dirname(__file__), ".."))
DEFAULT_OUT_DIR = os.path.join(ROOT_DIR, "src", "frontend", "public", "assets")

# Tokens Cromáticos Canónicos
C_OUTLINE      = (12, 11, 17, 255)      # #0c0b11 (contorno canónico)
C_SHADOW_CORE  = (37, 39, 54, 255)      # #252736 (sombra profunda)
C_STEEL_LIT    = (136, 143, 153, 255)  # #888f99 (acero luz)
C_STEEL_MID    = (97, 100, 109, 255)   # #61646d (acero medio)
C_STEEL_SHD    = (76, 74, 85, 255)     # #4c4a55 (acero sombra)
C_CONCRETE_LIT = (163, 152, 150, 255)  # #a39896 (hormigón / piedra luz)
C_CONCRETE_MID = (151, 148, 165, 255)  # #9794a5 (piedra medio)
C_CONCRETE_SHD = (108, 110, 123, 255)  # #6c6e7b (piedra sombra)
C_CONCRETE_DRK = (100, 94, 106, 255)  # #645e6a (sombra piedra)
C_METAL_PALE   = (198, 200, 213, 255)  # #c6c8d5 (metal claro)

C_WATER_DEEP   = (59, 88, 146, 255)    # #3b5892 (agua profunda)
C_WATER_MID    = (64, 137, 190, 255)   # #4089be (agua media)
C_WATER_CYAN   = (3, 169, 244, 255)    # #03a9f4 (cian brillante)
C_WATER_RIPPLE = (129, 212, 250, 255)  # #81d4fa (onda hídrica)
C_WATER_FOAM   = (225, 245, 254, 255)  # #e1f5fe (espuma clara)
C_WHITE        = (255, 255, 255, 255)  # #ffffff (brillo puro)

C_MOSS_LIT     = (140, 253, 203, 255)  # #8cfdcb (pasto menta)
C_MOSS_MID     = (89, 178, 70, 255)    # #59b246 (verde válvula)
C_MOSS_DARK    = (50, 122, 100, 255)   # #327a64 (verde sombra)
C_ECO_GREEN_M  = (61, 140, 60, 255)    # #3d8c3c (verde medio cesto)

C_EARTH_BASE   = (142, 90, 76, 255)    # #8e5a4c (tierra quemada)
C_EARTH_WARM   = (199, 98, 65, 255)    # #c76241 (tierra rojiza)
C_EARTH_DARK   = (105, 14, 20, 255)    # #690e14 (sombra corteza)
C_EARTH_DRY    = (237, 158, 82, 255)   # #ed9e52 (tierra seca sombra)
C_EARTH_MID    = (156, 61, 23, 255)    # #9c3d17 (marrón medio)
C_WALL_LIT     = (255, 213, 154, 255)  # #ffd59a (pared luz)
C_WALL_MID     = (245, 187, 114, 255)  # #f5bb72 (pared media)

C_TOXIC_LIT    = (184, 210, 149, 255)  # #b8d295 (vapor tóxico luz)
C_TOXIC_MID    = (165, 180, 135, 255)  # #a5b487 (vapor tóxico sombra)
C_SMOKE        = (153, 145, 158, 255)  # #99919e (humo)
C_CAN_RED      = (244, 67, 54, 255)    # #f44336 (rojo lata)
C_RUST_ORANGE  = (196, 103, 51, 255)   # #c46733 (óxido bidón)
C_GOLD         = (255, 214, 0, 255)    # #ffd600 (oro)

def get_base_diamond():
    diamond = []
    for y in range(16):
        if y <= 7:
            x_start = 14 - 2 * y
            x_end = 17 + 2 * y
        else:
            x_start = 2 * (y - 8)
            x_end = 31 - 2 * (y - 8)
        for x in range(x_start, x_end + 1):
            diamond.append((x, y))
    return diamond

# 1. tile_water.png
def gen_tile_water():
    im = Image.new("RGBA", (32, 16), (0, 0, 0, 0))
    diamond = get_base_diamond()
    for (x, y) in diamond:
        im.putpixel((x, y), C_WATER_MID)
        if (x + y * 2) % 6 in (0, 1):
            im.putpixel((x, y), C_WATER_DEEP)
            
    for dx in range(4): im.putpixel((14 + dx, 3), C_WATER_RIPPLE)
    im.putpixel((13, 3), C_WATER_CYAN); im.putpixel((18, 3), C_WATER_CYAN)
    for dx in range(5): im.putpixel((7 + dx, 7), C_WATER_RIPPLE)
    im.putpixel((6, 7), C_WATER_CYAN); im.putpixel((12, 7), C_WATER_CYAN)
    for dx in range(5): im.putpixel((19 + dx, 8), C_WATER_RIPPLE)
    im.putpixel((18, 8), C_WATER_CYAN); im.putpixel((24, 8), C_WATER_CYAN)
    for dx in range(4): im.putpixel((13 + dx, 12), C_WATER_RIPPLE)
    im.putpixel((12, 12), C_WATER_CYAN)

    im.putpixel((16, 4), C_WHITE); im.putpixel((21, 9), C_WHITE)

    for y in range(8, 16):
        im.putpixel((2 * (y - 8), y), C_WATER_CYAN)
        im.putpixel((31 - 2 * (y - 8), y), C_SHADOW_CORE)
    return im

# 2. tile_rock.png
def gen_tile_rock():
    im = Image.new("RGBA", (32, 32), (0, 0, 0, 0))
    rock_map = [
        "              ####              ", "             ######             ",
        "            ########            ", "           ##########           ",
        "          ############          ", "        ################        ",
        "      ####################      ", "     ######################     ",
        "    ########################    ", "   ##########################   ",
        "   ##########################   ", "  ############################  ",
        "  ############################  ", "   ##########################   ",
        "     ######################     ", "       ##################       ",
        "         ##############         ", "           ##########           ",
        "             ######             "
    ]
    for idx, row in enumerate(rock_map):
        y = 12 + idx
        for x, ch in enumerate(row):
            if ch == "#": im.putpixel((x, y), C_CONCRETE_SHD)

    for y in range(32):
        for x in range(32):
            if im.getpixel((x, y))[3] > 0:
                is_edge = any(nx < 0 or nx >= 32 or ny < 0 or ny >= 32 or im.getpixel((nx, ny))[3] == 0
                              for nx, ny in [(x-1, y), (x+1, y), (x, y-1), (x, y+1)])
                if is_edge: im.putpixel((x, y), C_OUTLINE)

    for y in range(13, 29):
        for x in range(1, 31):
            if im.getpixel((x, y)) == C_CONCRETE_SHD:
                if y <= 16 and x <= 18: im.putpixel((x, y), C_CONCRETE_LIT)
                elif y <= 19 and x <= 15: im.putpixel((x, y), C_CONCRETE_LIT)
                elif y <= 21 and x <= 12: im.putpixel((x, y), C_CONCRETE_MID)
                elif x > 17 and y >= 20: im.putpixel((x, y), C_SHADOW_CORE)
                elif x > 15 and y >= 18: im.putpixel((x, y), C_CONCRETE_DRK)
                elif x <= 14: im.putpixel((x, y), C_CONCRETE_MID)

    for px, py in [(15, 13), (16, 13), (14, 14), (13, 15), (12, 16), (10, 18), (8, 20)]:
        im.putpixel((px, py), C_METAL_PALE)

    for gy, gx in [(17, 16), (18, 16), (19, 17), (20, 17), (21, 18), (22, 18), (23, 19)]:
        im.putpixel((gx, gy), C_OUTLINE); im.putpixel((gx + 1, gy), C_SHADOW_CORE)

    moss = [(13, 17, C_MOSS_MID), (14, 17, C_MOSS_LIT), (14, 18, C_MOSS_DARK),
            (6, 21, C_MOSS_MID), (7, 21, C_MOSS_LIT), (6, 22, C_MOSS_DARK), (7, 22, C_MOSS_MID),
            (21, 23, C_MOSS_MID), (22, 23, C_MOSS_LIT), (23, 23, C_MOSS_MID), (22, 24, C_MOSS_DARK),
            (12, 26, C_MOSS_MID), (13, 26, C_MOSS_LIT), (14, 26, C_MOSS_DARK), (15, 27, C_MOSS_MID)]
    for mx, my, col in moss:
        if im.getpixel((mx, my)) != C_OUTLINE: im.putpixel((mx, my), col)
    return im

# 3. building_house.png
def gen_building_house():
    im = Image.new("RGBA", (32, 32), (0, 0, 0, 0))
    im.putpixel((20, 1), (153, 145, 158, 150)); im.putpixel((19, 2), (153, 145, 158, 200))
    im.putpixel((20, 3), C_SMOKE); im.putpixel((19, 4), C_SMOKE)
    for cy in range(5, 9):
        im.putpixel((18, cy), C_OUTLINE); im.putpixel((19, cy), C_STEEL_LIT); im.putpixel((20, cy), C_OUTLINE)
    im.putpixel((19, 5), C_STEEL_LIT)

    # Rombo superior
    for ry in range(6, 13):
        rx1 = 16 - 2 * (ry - 6) if ry <= 9 else 10 + 2 * (ry - 9)
        rx2 = 16 + 2 * (ry - 6) if ry <= 9 else 22 - 2 * (ry - 9)
        for rx in range(rx1, rx2 + 1):
            if rx in (rx1, rx2) or ry in (6, 12): im.putpixel((rx, ry), C_OUTLINE)
            elif rx < 16: im.putpixel((rx, ry), C_CONCRETE_LIT)
            else: im.putpixel((rx, ry), C_CONCRETE_SHD)

    # Faldones
    for x in range(4, 17):
        y_eave = 15 + (x - 4) // 2
        y_top = 15 - (x - 4) if x <= 10 else 9 + (x - 10) // 2
        for y in range(y_top, y_eave + 1):
            if im.getpixel((x, y))[3] == 0:
                if x == 4 or y in (y_eave, y_top) or x == 16: im.putpixel((x, y), C_OUTLINE)
                else: im.putpixel((x, y), C_EARTH_WARM)

    for x in range(16, 29):
        y_eave = 21 - (x - 16) // 2
        y_top = 12 - (x - 16) // 2 if x <= 22 else 9 + (x - 22)
        for y in range(y_top, y_eave + 1):
            if im.getpixel((x, y))[3] == 0:
                if x == 28 or y in (y_eave, y_top) or x == 16: im.putpixel((x, y), C_OUTLINE)
                else: im.putpixel((x, y), C_EARTH_MID)

    for x in range(4, 17): im.putpixel((x, 15 + (x - 4) // 2), C_OUTLINE)
    for x in range(16, 29): im.putpixel((x, 21 - (x - 16) // 2), C_OUTLINE)
    for y in range(12, 22): im.putpixel((16, y), C_OUTLINE)

    # Paredes
    for x in range(5, 16):
        for y in range(15 + (x - 4) // 2 + 1, 22 + (x - 4) // 2 + 1):
            im.putpixel((x, y), C_OUTLINE if y == 22 + (x - 4) // 2 else C_WALL_LIT)

    for x in range(16, 28):
        for y in range(21 - (x - 16) // 2 + 1, 28 - (x - 16) // 2 + 1):
            im.putpixel((x, y), C_OUTLINE if y == 28 - (x - 16) // 2 else C_WALL_MID)

    for y in range(21, 29): im.putpixel((16, y), C_OUTLINE)
    for y in range(15, 23): im.putpixel((4, y), C_OUTLINE); im.putpixel((28, y), C_OUTLINE)

    # Puerta y ventana en perspectiva
    for x in range(7, 11):
        y_bot = 22 + (x - 4) // 2 - 1
        for y in range(y_bot - 4, y_bot + 1):
            if x in (7, 10) or y == y_bot - 4: im.putpixel((x, y), C_OUTLINE)
            else: im.putpixel((x, y), C_EARTH_BASE)
    im.putpixel((9, 21), C_GOLD)

    for x in range(19, 25):
        y_bot = 24 - (x - 16) // 2
        for y in range(y_bot - 4, y_bot + 1):
            if x in (19, 24, 22) or y in (y_bot - 4, y_bot): im.putpixel((x, y), C_OUTLINE)
            elif x < 22: im.putpixel((x, y), (151, 233, 245, 255))
            else: im.putpixel((x, y), (55, 164, 221, 255))
    return im

# 4. trash_bin.png
def gen_trash_bin():
    im = Image.new("RGBA", (32, 32), (0, 0, 0, 0))
    lid = [(9, 14, 18), (10, 12, 20), (11, 11, 21), (12, 10, 22), (13, 10, 22)]
    for ly, x1, x2 in lid:
        for lx in range(x1, x2 + 1):
            if lx in (x1, x2) or ly in (9, 13): im.putpixel((lx, ly), C_OUTLINE)
            elif lx <= 15: im.putpixel((lx, ly), C_CONCRETE_SHD)
            else: im.putpixel((lx, ly), C_SHADOW_CORE)

    im.putpixel((15, 7), C_OUTLINE); im.putpixel((16, 7), C_STEEL_LIT); im.putpixel((17, 7), C_OUTLINE); im.putpixel((16, 8), C_STEEL_LIT)

    for rx in range(10, 23):
        im.putpixel((rx, 14), C_OUTLINE if rx in (10, 22) else (C_STEEL_LIT if rx <= 16 else C_STEEL_MID))

    for by in range(15, 24):
        im.putpixel((10, by), C_OUTLINE); im.putpixel((22, by), C_OUTLINE)
        for bx in range(11, 22):
            im.putpixel((bx, by), C_MOSS_MID if bx <= 13 else (C_ECO_GREEN_M if bx <= 18 else C_MOSS_DARK))

    # Reciclaje
    im.putpixel((15, 17), C_WHITE); im.putpixel((16, 17), C_WHITE)
    im.putpixel((14, 18), (151, 233, 245, 255)); im.putpixel((17, 18), (151, 233, 245, 255))
    im.putpixel((15, 19), C_WHITE); im.putpixel((18, 19), (151, 233, 245, 255))
    im.putpixel((15, 20), C_WHITE); im.putpixel((16, 20), C_WHITE)

    for rx in range(10, 23):
        im.putpixel((rx, 24), C_OUTLINE if rx in (10, 22) else (C_STEEL_LIT if rx <= 16 else C_STEEL_MID))

    base = [(25, 11, 21), (26, 12, 20), (27, 14, 18)]
    for by, x1, x2 in base:
        for bx in range(x1, x2 + 1):
            if bx in (x1, x2) or by == 27: im.putpixel((bx, by), C_OUTLINE)
            elif bx <= 16: im.putpixel((bx, by), C_STEEL_MID)
            else: im.putpixel((bx, by), C_STEEL_SHD)

    im.putpixel((15, 26), C_STEEL_LIT); im.putpixel((16, 26), C_STEEL_LIT); im.putpixel((17, 26), C_OUTLINE); im.putpixel((16, 27), C_STEEL_LIT)
    return im

# 5. trash_1_small.png
def gen_trash_1_small():
    im = Image.new("RGBA", (32, 32), (0, 0, 0, 0))
    im.putpixel((11, 22), C_OUTLINE); im.putpixel((12, 22), C_STEEL_LIT)
    im.putpixel((10, 23), C_OUTLINE); im.putpixel((11, 23), C_CAN_RED); im.putpixel((12, 23), C_CAN_RED); im.putpixel((13, 23), C_OUTLINE)
    im.putpixel((10, 24), C_OUTLINE); im.putpixel((11, 24), C_STEEL_MID); im.putpixel((12, 24), C_STEEL_SHD); im.putpixel((13, 24), C_OUTLINE)
    im.putpixel((11, 25), C_OUTLINE); im.putpixel((12, 25), C_OUTLINE)

    cardboard = [(16, 21, C_WALL_MID), (17, 21, C_WALL_MID), (15, 22, C_WALL_MID), (16, 22, C_EARTH_DRY),
                 (17, 22, C_EARTH_DRY), (18, 22, C_EARTH_BASE), (16, 23, C_EARTH_DRY), (17, 23, C_EARTH_BASE),
                 (18, 23, C_EARTH_BASE), (19, 23, C_OUTLINE), (15, 24, C_OUTLINE), (16, 24, C_OUTLINE), (17, 24, C_OUTLINE),
                 (15, 21, C_OUTLINE), (18, 21, C_OUTLINE), (14, 22, C_OUTLINE), (19, 22, C_OUTLINE)]
    for cx, cy, col in cardboard: im.putpixel((cx, cy), col)

    im.putpixel((21, 23), C_WHITE); im.putpixel((20, 24), C_WATER_RIPPLE); im.putpixel((21, 24), C_WHITE); im.putpixel((22, 24), C_WATER_CYAN)
    im.putpixel((20, 25), C_OUTLINE); im.putpixel((21, 25), C_WATER_CYAN); im.putpixel((22, 25), C_WATER_CYAN); im.putpixel((23, 25), C_OUTLINE)
    im.putpixel((21, 26), C_OUTLINE); im.putpixel((22, 26), C_OUTLINE)

    im.putpixel((13, 26), C_WHITE); im.putpixel((14, 26), C_STEEL_LIT); im.putpixel((13, 27), C_OUTLINE); im.putpixel((14, 27), C_OUTLINE)
    return im

# 6. trash_2_medium.png
def gen_trash_2_medium():
    im = Image.new("RGBA", (32, 32), (0, 0, 0, 0))
    bag1 = [(12, 17), (13, 17), (11, 18), (12, 18), (13, 18), (14, 18),
            (10, 19), (11, 19), (12, 19), (13, 19), (14, 19), (15, 19),
            (9, 20), (10, 20), (11, 20), (12, 20), (13, 20), (14, 20), (15, 20), (16, 20),
            (9, 21), (10, 21), (11, 21), (12, 21), (13, 21), (14, 21), (15, 21), (16, 21),
            (10, 22), (11, 22), (12, 22), (13, 22), (14, 22), (15, 22),
            (10, 23), (11, 23), (12, 23), (13, 23), (14, 23), (11, 24), (12, 24), (13, 24)]
    for bx, by in bag1: im.putpixel((bx, by), C_SHADOW_CORE)
    for bx, by in [(12, 18), (11, 19), (12, 19), (11, 20)]: im.putpixel((bx, by), C_CONCRETE_SHD)
    for bx, by in [(14, 21), (15, 21), (13, 22), (14, 22)]: im.putpixel((bx, by), C_OUTLINE)
    im.putpixel((12, 16), C_OUTLINE); im.putpixel((13, 16), C_CONCRETE_SHD); im.putpixel((14, 16), C_OUTLINE)

    for cy in range(16, 23):
        for cx in range(16, 24):
            if cx in (16, 23) or cy in (16, 22): im.putpixel((cx, cy), C_OUTLINE)
            elif cx <= 19: im.putpixel((cx, cy), C_WALL_MID)
            else: im.putpixel((cx, cy), C_EARTH_DRY)
    im.putpixel((18, 15), C_OUTLINE); im.putpixel((19, 15), C_WALL_MID); im.putpixel((20, 15), C_OUTLINE)
    im.putpixel((19, 16), C_EARTH_DRY); im.putpixel((21, 19), C_EARTH_BASE)

    im.putpixel((14, 14), C_OUTLINE); im.putpixel((15, 14), C_CONCRETE_SHD); im.putpixel((16, 14), C_OUTLINE)
    im.putpixel((14, 15), C_SHADOW_CORE); im.putpixel((15, 15), C_SHADOW_CORE); im.putpixel((16, 15), C_OUTLINE)

    im.putpixel((8, 23), C_OUTLINE); im.putpixel((9, 23), C_STEEL_LIT); im.putpixel((7, 24), C_OUTLINE)
    im.putpixel((8, 24), C_CAN_RED); im.putpixel((9, 24), C_CAN_RED); im.putpixel((10, 24), C_OUTLINE)
    im.putpixel((8, 25), C_OUTLINE); im.putpixel((9, 25), C_STEEL_MID); im.putpixel((10, 25), C_OUTLINE)

    im.putpixel((19, 24), C_WATER_RIPPLE); im.putpixel((20, 24), C_WHITE); im.putpixel((21, 24), C_WATER_CYAN)
    im.putpixel((18, 25), C_OUTLINE); im.putpixel((19, 25), C_WATER_CYAN); im.putpixel((20, 25), C_OUTLINE)
    im.putpixel((22, 25), C_STEEL_LIT); im.putpixel((23, 25), C_STEEL_MID); im.putpixel((24, 25), C_OUTLINE)
    im.putpixel((21, 26), C_OUTLINE); im.putpixel((22, 26), C_STEEL_SHD); im.putpixel((23, 26), C_OUTLINE)

    for y in range(32):
        for x in range(32):
            if im.getpixel((x, y))[3] > 0 and im.getpixel((x, y)) != C_OUTLINE:
                if any(im.getpixel((nx, ny))[3] == 0 for nx, ny in [(x-1,y),(x+1,y),(x,y-1),(x,y+1)] if 0<=nx<32 and 0<=ny<32):
                    im.putpixel((x, y), C_OUTLINE)
    return im

# 7. trash_3_large.png
def gen_trash_3_large():
    im = Image.new("RGBA", (32, 32), (0, 0, 0, 0))
    barrel = [(8, 14), (9, 14), (10, 14), (7, 15), (8, 15), (9, 15), (10, 15), (11, 15),
              (6, 16), (7, 16), (8, 16), (9, 16), (10, 16), (11, 16), (12, 16),
              (6, 17), (7, 17), (8, 17), (9, 17), (10, 17), (11, 17), (12, 17), (13, 17),
              (5, 18), (6, 18), (7, 18), (8, 18), (9, 18), (10, 18), (11, 18), (12, 18),
              (6, 19), (7, 19), (8, 19), (9, 19), (10, 19), (11, 19),
              (6, 20), (7, 20), (8, 20), (9, 20), (10, 20), (7, 21), (8, 21), (9, 21)]
    for bx, by in barrel: im.putpixel((bx, by), C_EARTH_MID)
    for bx, by in [(8, 15), (9, 15), (7, 16), (8, 16), (9, 16), (7, 17), (8, 17)]: im.putpixel((bx, by), C_RUST_ORANGE)
    for bx in range(7, 12): im.putpixel((bx, 18), C_STEEL_MID)

    for hy in range(13, 28):
        hx_start = max(8, 18 - (hy - 13))
        hx_end = min(27, 18 + (hy - 13))
        for hx in range(hx_start, hx_end + 1):
            if (hx + hy) % 3 == 0: im.putpixel((hx, hy), C_SHADOW_CORE)
            elif (hx + hy) % 3 == 1: im.putpixel((hx, hy), C_EARTH_DRY)
            else: im.putpixel((hx, hy), C_CONCRETE_SHD)

    for ly in (27, 28):
        for lx in range(12, 20): im.putpixel((lx, ly), C_OUTLINE)

    vapors = [(15, 5, C_TOXIC_LIT), (16, 5, C_TOXIC_LIT), (14, 6, C_TOXIC_LIT), (15, 6, C_TOXIC_LIT),
              (16, 6, C_TOXIC_MID), (14, 7, C_TOXIC_MID), (15, 7, C_TOXIC_LIT), (16, 7, C_TOXIC_MID),
              (15, 8, C_TOXIC_MID), (16, 8, C_TOXIC_LIT), (19, 7, C_TOXIC_LIT), (20, 7, C_TOXIC_LIT),
              (18, 8, C_TOXIC_LIT), (19, 8, C_TOXIC_MID), (20, 8, C_TOXIC_LIT), (18, 9, C_TOXIC_MID),
              (19, 9, C_TOXIC_LIT), (20, 9, C_TOXIC_MID), (17, 10, C_TOXIC_LIT), (18, 10, C_TOXIC_MID)]
    for vx, vy, col in vapors: im.putpixel((vx, vy), col)

    im.putpixel((12, 8), C_OUTLINE); im.putpixel((22, 6), C_OUTLINE); im.putpixel((24, 11), C_OUTLINE)

    for y in range(12, 30):
        for x in range(32):
            if im.getpixel((x, y))[3] > 0 and im.getpixel((x, y)) not in (C_OUTLINE, C_TOXIC_LIT, C_TOXIC_MID):
                if any(im.getpixel((nx, ny))[3] == 0 for nx, ny in [(x-1,y),(x+1,y),(x,y-1),(x,y+1)] if 0<=nx<32 and 0<=ny<32):
                    im.putpixel((x, y), C_OUTLINE)
    return im

# 8. clean_sparkle.png
def gen_clean_sparkle():
    im = Image.new("RGBA", (16, 16), (0, 0, 0, 0))
    for y in range(3, 13):
        for x in (7, 8):
            if y in (6, 7, 8, 9): im.putpixel((x, y), C_WHITE)
            elif y in (5, 10): im.putpixel((x, y), C_WATER_FOAM)
            elif y in (4, 11): im.putpixel((x, y), C_WATER_RIPPLE)
            else: im.putpixel((x, y), C_WATER_CYAN)

    for x in range(3, 13):
        for y in (7, 8):
            if x in (6, 7, 8, 9): im.putpixel((x, y), C_WHITE)
            elif x in (5, 10): im.putpixel((x, y), C_WATER_FOAM)
            elif x in (4, 11): im.putpixel((x, y), C_WATER_RIPPLE)
            else: im.putpixel((x, y), C_WATER_CYAN)

    for px, py in [(6, 6), (9, 6), (6, 9), (9, 9)]: im.putpixel((px, py), C_WATER_FOAM)
    im.putpixel((3, 4), C_GOLD); im.putpixel((4, 3), C_WHITE); im.putpixel((12, 4), C_GOLD)
    im.putpixel((3, 11), C_GOLD); im.putpixel((12, 11), C_WHITE); im.putpixel((13, 12), C_GOLD)
    return im

# 9. tile_polluted_soil.png
def gen_tile_polluted_soil():
    im = Image.new("RGBA", (32, 16), (0, 0, 0, 0))
    diamond = get_base_diamond()
    for (x, y) in diamond:
        v = (x * 3 + y * 7 + x * y) % 5
        col = C_EARTH_BASE if v in (0, 4) else (C_EARTH_WARM if v == 1 else (C_EARTH_MID if v == 2 else C_EARTH_DRY))
        im.putpixel((x, y), col)

    green_stains = [(10, 5, C_TOXIC_MID), (11, 5, C_TOXIC_LIT), (9, 6, C_MOSS_DARK), (10, 6, C_TOXIC_MID),
                    (11, 6, C_TOXIC_MID), (12, 6, C_TOXIC_LIT), (9, 7, C_TOXIC_MID), (10, 7, C_MOSS_DARK),
                    (11, 7, C_TOXIC_MID), (10, 8, C_MOSS_DARK), (11, 8, C_TOXIC_MID), (18, 3, C_TOXIC_LIT),
                    (19, 3, C_TOXIC_MID), (17, 4, C_TOXIC_MID), (18, 4, C_MOSS_DARK), (19, 4, C_TOXIC_LIT),
                    (20, 4, C_TOXIC_MID), (18, 5, C_TOXIC_MID), (19, 5, C_MOSS_DARK), (20, 5, C_TOXIC_MID),
                    (15, 9, C_TOXIC_MID), (16, 9, C_TOXIC_LIT), (17, 9, C_TOXIC_MID), (15, 10, C_MOSS_DARK),
                    (16, 10, C_TOXIC_MID), (17, 10, C_MOSS_DARK), (18, 10, C_TOXIC_MID), (16, 11, C_TOXIC_MID),
                    (17, 11, C_TOXIC_LIT), (15, 12, C_MOSS_DARK), (16, 12, C_TOXIC_MID)]
    for gx, gy, col in green_stains:
        if (gx, gy) in diamond: im.putpixel((gx, gy), col)

    im.putpixel((13, 6), C_OUTLINE); im.putpixel((14, 6), C_METAL_PALE); im.putpixel((15, 6), C_RUST_ORANGE)
    im.putpixel((14, 7), C_RUST_ORANGE); im.putpixel((15, 7), C_OUTLINE)
    im.putpixel((22, 7), C_WHITE); im.putpixel((21, 8), C_WATER_RIPPLE); im.putpixel((22, 8), C_WATER_RIPPLE); im.putpixel((23, 8), C_OUTLINE)
    im.putpixel((8, 9), C_OUTLINE); im.putpixel((9, 9), C_METAL_PALE); im.putpixel((7, 10), C_OUTLINE); im.putpixel((8, 10), C_RUST_ORANGE)

    for y in range(8, 16):
        im.putpixel((2 * (y - 8), y), C_EARTH_WARM)
        im.putpixel((31 - 2 * (y - 8), y), C_EARTH_DARK)
    return im

# 10. trash_water.png
def gen_trash_water():
    im = Image.new("RGBA", (32, 32), (0, 0, 0, 0))
    oil = [(13, 21), (14, 21), (15, 21), (16, 21), (17, 21), (18, 21),
           (10, 22), (11, 22), (12, 22), (13, 22), (14, 22), (15, 22), (16, 22), (17, 22), (18, 22), (19, 22), (20, 22), (21, 22),
           (8, 23), (9, 23), (10, 23), (11, 23), (12, 23), (13, 23), (14, 23), (15, 23), (16, 23), (17, 23), (18, 23), (19, 23), (20, 23), (21, 23), (22, 23), (23, 23),
           (7, 24), (8, 24), (9, 24), (10, 24), (11, 24), (12, 24), (13, 24), (14, 24), (15, 24), (16, 24), (17, 24), (18, 24), (19, 24), (20, 24), (21, 24), (22, 24), (23, 24), (24, 24),
           (9, 25), (10, 25), (11, 25), (12, 25), (13, 25), (14, 25), (15, 25), (16, 25), (17, 25), (18, 25), (19, 25), (20, 25), (21, 25), (22, 25),
           (11, 26), (12, 26), (13, 26), (14, 26), (15, 26), (16, 26), (17, 26), (18, 26), (19, 26), (20, 26),
           (13, 27), (14, 27), (15, 27), (16, 27), (17, 27), (18, 27)]
    for ox, oy in oil:
        col = C_SHADOW_CORE if (ox + oy) % 3 == 0 else (C_SMOKE if (ox + oy) % 3 == 1 else C_TOXIC_MID)
        im.putpixel((ox, oy), col)

    barrel = [(12, 18), (13, 18), (14, 18), (11, 19), (12, 19), (13, 19), (14, 19), (15, 19),
              (10, 20), (11, 20), (12, 20), (13, 20), (14, 20), (15, 20), (16, 20),
              (10, 21), (11, 21), (12, 21), (13, 21), (14, 21), (15, 21),
              (11, 22), (12, 22), (13, 22), (14, 22)]
    for bx, by in barrel: im.putpixel((bx, by), C_RUST_ORANGE)
    im.putpixel((12, 18), C_STEEL_LIT); im.putpixel((13, 18), C_STEEL_LIT); im.putpixel((14, 18), C_STEEL_LIT)
    for bx, by in [(11, 18), (15, 18), (10, 19), (16, 19), (9, 20), (17, 20), (10, 22), (15, 22)]:
        im.putpixel((bx, by), C_OUTLINE)

    for px, py in [(19, 21), (20, 21), (18, 22), (19, 22), (20, 22), (21, 22),
                   (18, 23), (19, 23), (20, 23), (21, 23), (22, 23), (19, 24), (20, 24), (21, 24)]:
        im.putpixel((px, py), (255, 255, 255, 230))
    im.putpixel((19, 21), C_WATER_FOAM); im.putpixel((21, 22), C_WATER_FOAM)

    im.putpixel((14, 25), C_OUTLINE); im.putpixel((15, 25), C_CAN_RED); im.putpixel((16, 25), C_STEEL_LIT); im.putpixel((17, 25), C_OUTLINE)
    im.putpixel((15, 24), C_CAN_RED); im.putpixel((16, 24), C_OUTLINE); im.putpixel((15, 26), C_OUTLINE)

    ripples = [(9, 21, C_WATER_RIPPLE), (8, 22, C_WATER_CYAN), (9, 22, C_WATER_FOAM),
               (16, 22, C_WATER_FOAM), (17, 22, C_WATER_RIPPLE), (22, 22, C_WATER_RIPPLE),
               (23, 23, C_WATER_CYAN), (24, 23, C_WATER_RIPPLE), (17, 24, C_WATER_RIPPLE),
               (22, 24, C_WATER_CYAN), (12, 20, C_WATER_FOAM), (13, 20, C_WATER_FOAM),
               (12, 27, C_WATER_RIPPLE), (13, 27, C_WATER_RIPPLE), (18, 27, C_WATER_RIPPLE), (19, 27, C_WATER_CYAN)]
    for rx, ry, col in ripples:
        if im.getpixel((rx, ry))[3] == 0: im.putpixel((rx, ry), col)
    return im

GENERATORS = {
    "tile_water.png": (gen_tile_water, "tiles"),
    "tile_rock.png": (gen_tile_rock, "tiles"),
    "building_house.png": (gen_building_house, "tiles"),
    "trash_bin.png": (gen_trash_bin, "installations"),
    "trash_1_small.png": (gen_trash_1_small, "tiles"),
    "trash_2_medium.png": (gen_trash_2_medium, "tiles"),
    "trash_3_large.png": (gen_trash_3_large, "tiles"),
    "clean_sparkle.png": (gen_clean_sparkle, "effects"),
    "tile_polluted_soil.png": (gen_tile_polluted_soil, "tiles"),
    "trash_water.png": (gen_trash_water, "tiles"),
}

def main():
    parser = argparse.ArgumentParser(description="Generador Reproducible de Sprites de Hydrovalley")
    parser.add_argument("--out-dir", default=DEFAULT_OUT_DIR, help="Directorio destino de assets públicos")
    parser.add_argument("--verify", action="store_true", help="Ejecutar validación de calidad tras generar")
    args = parser.parse_args()

    print("================================================================================")
    print(" HYDROVALLEY - GENERADOR REPRODUCIBLE DE SPRITES (FASE 4)                       ")
    print("================================================================================")
    print(f"Directorio de Salida: {args.out_dir}")

    for filename, (gen_fn, subfolder) in GENERATORS.items():
        out_subfolder = os.path.join(args.out_dir, subfolder)
        os.makedirs(out_subfolder, exist_ok=True)
        out_path = os.path.join(out_subfolder, filename)
        
        img = gen_fn()
        img.save(out_path, format="PNG")
        print(f"• Generado: {subfolder}/{filename:22} [{img.size[0]}x{img.size[1]}]")

    print("--------------------------------------------------------------------------------")
    print(f"Generación completa: {len(GENERATORS)} sprites generados exitosamente.")
    print("================================================================================")

    if args.verify:
        print("\nEjecutando suite de validación de calidad...")
        script_dir = os.path.dirname(__file__)
        val_script = os.path.join(script_dir, "validate_assets.py")
        ret = os.system(f'python "{val_script}"')
        if ret != 0:
            sys.exit(1)

if __name__ == "__main__":
    main()
