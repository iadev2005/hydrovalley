#para cualquier prueba en local que no levanmte 

# Solo smoke (CP-01-lite, RF-01, RF-08 básico, RNF-10…)
tests/e2e/run.sh tests/e2e/smoke.test.mjs

# Solo CP-01 — 100 gotas simultáneas, 2 bots
E2E_SKIP_FAULTS=1 HV_HTTP_PORT=8085 tests/e2e/run.sh tests/e2e/cp01-concurrent-drops.test.mjs

# Solo fallos de servicios (CP-05, CP-06, CP-07, CP-12)
tests/e2e/run.sh tests/e2e/faults.test.mjs

# Solo fallos + persistencia hub (CP-09, CP-10, CP-11)
tests/e2e/run.sh tests/e2e/faults-hub-persistence.test.mjs

# Solo fallos de arranque (CP-08)
tests/e2e/run.sh tests/e2e/faults-startup.test.mjs

# Toda la suite (sin argumentos)
tests/e2e/run.sh
