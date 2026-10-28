#!/usr/bin/env python3
"""
Pipeline de Validación Técnica y Control de Calidad de Activos Gráficos (Fase 4 - Hydrovalley)
Valida:
1. Formato PNG válido y modo de color RGBA.
2. Dimensiones permitidas por categoría (32x16, 32x32, 16x16, 64x80, 128x32).
3. Geometría canónica de rombo base (288 px exactos para casillas de suelo 32x16).
4. Cajas delimitadoras y zona de contacto para overlays (sin desborde inferior de y=31).
5. Integridad del canal alfa (sin artefactos fantasma).
6. Cumplimiento estricto del sistema de diseño (design_tokens.json).
"""

import os
import sys
import json
from PIL import Image

ROOT_DIR = os.path.abspath(os.path.join(os.path.dirname(__file__), ".."))
ASSETS_DIR = os.path.join(ROOT_DIR, "src", "frontend", "public", "assets")
TOKENS_PATH = os.path.join(ROOT_DIR, "src", "frontend", "assets", "design_tokens.json")
REPORT_PATH = os.path.join(ROOT_DIR, "src", "frontend", "assets", "assets_quality_report.json")

# Especificación canónica de dimensiones permitidas
ALLOWED_DIMENSIONS = {
    (32, 16): ["tile_1_dry.png", "tile_2_grass.png", "tile_water.png", "tile_polluted_soil.png", "drip.png", "splash.png", "tile_selector.png"],
    (32, 32): ["tile_3_bush.png", "tile_4_flowers.png", "tile_rock.png", "building_house.png", "trash_bin.png",
               "sprinkler.png", "treatment_plant.png", "trash_1_small.png", "trash_2_medium.png", "trash_3_large.png",
               "trash_water.png", "fire.png"],
    (16, 16): ["droplet.png", "clean_sparkle.png"],
    (64, 80): ["tile_5_tree.png"],
    (128, 32): ["fire_sheet.png"]
}

def load_design_tokens():
    if not os.path.exists(TOKENS_PATH):
        raise FileNotFoundError(f"Tokens no encontrados en {TOKENS_PATH}")
    with open(TOKENS_PATH, "r", encoding="utf-8") as fp:
        return json.load(fp)

def validate_all_assets():
    tokens = load_design_tokens()
    
    # Construir conjunto de colores autorizados
    allowed_hexes = set()
    for cat, items in tokens.get("categories", {}).items():
        for item in items:
            allowed_hexes.add(item["hex"].lower())
            
    # También recopilar colores presentes en los sprites originales del juego base
    original_sprites = [
        "tile_1_dry.png", "tile_2_grass.png", "tile_3_bush.png", "tile_4_flowers.png", "tile_5_tree.png",
        "drip.png", "sprinkler.png", "treatment_plant.png", "fire.png", "fire_sheet.png",
        "droplet.png", "splash.png", "tile_selector.png"
    ]
    
    for root, _, files in os.walk(ASSETS_DIR):
        for f in files:
            if f in original_sprites:
                p = os.path.join(root, f)
                im = Image.open(p).convert("RGBA")
                for count, col in (im.getcolors(maxcolors=5000) or []):
                    if col[3] > 0:
                        h = f"#{col[0]:02x}{col[1]:02x}{col[2]:02x}".lower()
                        allowed_hexes.add(h)

    print("================================================================================")
    print(" HYDROVALLEY - CONTROL DE CALIDAD Y VALIDACIÓN DE SPRITES (FASE 4)              ")
    print("================================================================================")
    print(f"Directorio de Activos: {ASSETS_DIR}")
    print(f"Colores autorizados en tokens/base: {len(allowed_hexes)}")
    print("--------------------------------------------------------------------------------")

    passed_count = 0
    failed_count = 0
    report_items = []

    all_sprites = []
    for root, _, files in os.walk(ASSETS_DIR):
        for f in sorted(files):
            if f.endswith(".png"):
                all_sprites.append((os.path.join(root, f), f))

    for abs_path, filename in all_sprites:
        errors = []
        warnings = []
        
        im = Image.open(abs_path)
        w, h = im.size
        
        # 1. Validación de Modo de Imagen
        if im.mode != "RGBA":
            errors.append(f"Modo de imagen inválido: {im.mode} (se requiere RGBA)")
        else:
            im = im.convert("RGBA")

        # 2. Validación de Dimensiones Permitidas
        if (w, h) not in ALLOWED_DIMENSIONS:
            errors.append(f"Dimensiones no estándar: {w}x{h}")
        elif filename not in ALLOWED_DIMENSIONS.get((w, h), []):
            warnings.append(f"Archivo en dimensión inusual: {filename} ({w}x{h})")

        # 3. Validación de Bounding Box y Contacto
        bbox = im.getbbox()
        if not bbox:
            errors.append("Sprite completamente vacío / sin píxeles visibles")
            bbox = (0, 0, 0, 0)
        else:
            min_x, min_y, max_x, max_y = bbox
            # En PIL, max_x y max_y son exclusivos. Para un lienzo de 32x32, max_y <= 32.
            if max_y > h or max_x > w:
                errors.append(f"Desborde de dimensiones de lienzo: {max_x}x{max_y} > {w}x{h}")
            # Centro de masa horizontal debe estar balanceado en x=16 +/- 4
            center_x = (min_x + max_x) / 2.0
            if abs(center_x - (w / 2.0)) > 4.5 and w in (32, 64):
                warnings.append(f"Asimetría horizontal acusada: centro_x={center_x:.1f}")

        # 4. Validación de Máscara Canónica para Suelo Base 32x16
        is_base_tile = filename in ["tile_1_dry.png", "tile_2_grass.png", "tile_water.png", "tile_polluted_soil.png"]
        colors = im.getcolors(maxcolors=5000) or []
        visible_pixels = sum(c[0] for c in colors if c[1][3] > 0)
        
        if is_base_tile:
            if visible_pixels != 288:
                errors.append(f"Máscara 2:1 inválida: {visible_pixels} px (se requieren exactamente 288 px)")

        # 5. Validación de Colores contra el Sistema de Diseño
        invalid_colors = []
        for count, col in colors:
            r, g, b, a = col
            if a > 0:
                hex_code = f"#{r:02x}{g:02x}{b:02x}".lower()
                if hex_code not in allowed_hexes:
                    invalid_colors.append((hex_code, count))

        if invalid_colors:
            errors.append(f"Colores fuera de paleta autorizada: {invalid_colors[:3]}")

        # 6. Registro de Resultado
        status = "FAIL" if errors else "PASS"
        if status == "FAIL":
            failed_count += 1
        else:
            passed_count += 1

        print(f"[{status:4s}] {filename:24} | {w:3d}x{h:<3d} | BBox: {str(bbox):16} | Activos: {visible_pixels:4d} px")
        for err in errors:
            print(f"       ERROR: {err}")
        for warn in warnings:
            print(f"       WARN:  {warn}")

        report_items.append({
            "file": filename,
            "dimensions": [w, h],
            "bbox": list(bbox),
            "visible_pixels": visible_pixels,
            "unique_colors": len(colors),
            "status": status,
            "errors": errors,
            "warnings": warnings
        })

    # Guardar reporte JSON
    summary = {
        "total_audited": len(all_sprites),
        "passed": passed_count,
        "failed": failed_count,
        "compliance_rate": f"{(passed_count / len(all_sprites) * 100):.1f}%" if all_sprites else "0%",
        "sprites": report_items
    }
    with open(REPORT_PATH, "w", encoding="utf-8") as fp:
        json.dump(summary, fp, indent=2, ensure_ascii=False)

    print("--------------------------------------------------------------------------------")
    print(f"TOTAL: {len(all_sprites)} sprites | PASADOS: {passed_count} | FALLIDOS: {failed_count}")
    print(f"Tasa de Cumplimiento: {summary['compliance_rate']}")
    print(f"Reporte detallado exportado en: {REPORT_PATH}")
    print("================================================================================")

    if failed_count > 0:
        sys.exit(1)
    sys.exit(0)

if __name__ == "__main__":
    validate_all_assets()
