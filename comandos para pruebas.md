# Comandos para pruebas E2E
#
# HV_HTTP_PORT=8085  → evita conflicto si el puerto 8080 ya está ocupado
# E2E_SKIP_FAULTS=1  → omite los faults*.test.mjs al final (más rápido)
#
# Todos los comandos individuales usan ambas variables.
# Para los faults se quita E2E_SKIP_FAULTS porque precisamente prueban fallos.

# Solo smoke (CP-01-lite, RF-01, RF-08 básico, RNF-10…)
sudo E2E_SKIP_FAULTS=1 HV_HTTP_PORT=8085 tests/e2e/run.sh tests/e2e/smoke.test.mjs

# Solo CP-01 — 100 gotas simultáneas, 2 bots
sudo E2E_SKIP_FAULTS=1 HV_HTTP_PORT=8085 tests/e2e/run.sh tests/e2e/cp01-concurrent-drops.test.mjs

# Solo CP-04 — bomberos contra incendios reales (catálogo con alta ignición)
sudo CATALOG_PATH=/app/catalog/catalog.test-fire.json E2E_SKIP_FAULTS=1 HV_HTTP_PORT=8085 tests/e2e/run.sh tests/e2e/cp04-firefighter.test.mjs

# Solo fallos de servicios (CP-05, CP-06, CP-07, CP-12)
sudo HV_HTTP_PORT=8085 tests/e2e/run.sh tests/e2e/faults.test.mjs

# Solo fallos + persistencia hub (CP-09, CP-10, CP-11)
sudo HV_HTTP_PORT=8085 tests/e2e/run.sh tests/e2e/faults-hub-persistence.test.mjs

# Solo fallos de arranque (CP-08)
sudo HV_HTTP_PORT=8085 tests/e2e/run.sh tests/e2e/faults-startup.test.mjs

# Toda la suite sin faults (smoke + cp01; más rápida)
sudo E2E_SKIP_FAULTS=1 HV_HTTP_PORT=8085 tests/e2e/run.sh

# Toda la suite completa (incluye faults; tarda varios minutos)
sudo HV_HTTP_PORT=8085 tests/e2e/run.sh
