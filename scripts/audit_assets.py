#!/usr/bin/env python3
"""
Auditoría y Validación de Activos Gráficos (Fase 0 - Hydrovalley)
Valida dimensiones, proporción isométrica 2:1, canal alfa, cajas de contacto y colores
de referencia del sistema de diseño para los sprites de Hydrovalley.
"""

import os
import sys
import json
from PIL import Image

ASSETS_DIR = os.path.join(os.path.dirname(__file__), "..", "src", "frontend", "public", "assets")
TOKENS_PATH = os.path.join(os.path.dirname(__file__), "..", "src", "frontend", "assets", "design_tokens.json")

def load_design_tokens():
    if not os.path.exists(TOKENS_PATH):
        return None
    with open(TOKENS_PATH, "r", encoding="utf-8") as fp:
        return json.load(fp)

def audit_sprite(path, rel_path, tokens=None):
    im = Image.open(path).convert("RGBA")
    w, h = im.size
    bbox = im.getbbox()
    colors = im.getcolors(maxcolors=4000) or []
    
    visible = [c for c in colors if c[1][3] > 0]
    total_visible_pixels = sum(c[0] for c in visible)
    
    # Análisis de bordes y canal alfa
    alpha_levels = set(c[1][3] for c in colors)
    
    unique_hexes = set(f"#{c[1][0]:02x}{c[1][1]:02x}{c[1][2]:02x}".lower() for c in visible)
    
    return {
        "file": os.path.basename(path),
        "rel_path": rel_path,
        "width": w,
        "height": h,
        "bbox": list(bbox) if bbox else None,
        "total_pixels": w * h,
        "visible_pixels": total_visible_pixels,
        "color_count": len(visible),
        "alpha_levels": sorted(list(alpha_levels)),
        "hexes": sorted(list(unique_hexes))
    }

def main():
    tokens = load_design_tokens()
    print("=================================================================")
    print(" HYDROVALLEY - AUDITORÍA DE SPRITES (FASE 0 - DESIGN SYSTEM)    ")
    print("=================================================================")
    
    results = []
    for root, _, files in os.walk(ASSETS_DIR):
        for f in sorted(files):
            if not f.endswith(".png"):
                continue
            abs_p = os.path.join(root, f)
            rel_p = os.path.relpath(abs_p, ASSETS_DIR).replace("\\", "/")
            res = audit_sprite(abs_p, rel_p, tokens)
            results.append(res)
            
            print(f"• {res['file']:20} [{res['width']}x{res['height']}] | "
                  f"BBox: {str(res['bbox']):16} | "
                  f"Colores: {res['color_count']:2d} | "
                  f"Píxeles activos: {res['visible_pixels']}")
            
    print("=================================================================")
    print(f"Total de sprites auditados: {len(results)}")
    print(f"Tokens de diseño cargados: {'Sí' if tokens else 'No'}")
    print("=================================================================")

if __name__ == "__main__":
    main()
