# B1: despliegue repartido en el laboratorio (plan A)

## Objetivo

Dejar listo el despliegue del plan A (cinco máquinas del laboratorio) para que el día del ensayo solo haga falta poner las direcciones IP: un Compose por máquina con las mismas imágenes de `ghcr.io`, scripts de diagnóstico y despliegue para Windows, y una simulación del plan A en una sola máquina que lo valide sin el laboratorio.

## Problema y por qué

`deploy/nodes/` solo tiene un README; no hay archivos por máquina ni scripts. El cortafuegos del laboratorio sigue cerrado entre máquinas (falta la regla del encargado), y el ensayo es la semana 6. El profesor apagará una máquina en la defensa (RES-07, CP-05 a CP-08).

## Alcance

- `deploy/nodes/<nodo>.yml` (db, persistence, world, hub, proxy) según el diagrama 08, sobre las mismas imágenes; solo cambian direcciones y puertos (regla 6 de portabilidad).
- Archivo de direcciones de ejemplo y archivo local de secretos (fuera del repositorio).
- Scripts en PowerShell para Windows (cuenta no administradora, Docker Desktop con WSL 2): diagnóstico de red entre máquinas, despliegue y comprobación de salud por máquina.
- Simulación del plan A en una máquina: cinco proyectos de Compose que se hablan solo por puertos publicados del anfitrión, como si fueran máquinas distintas, con una prueba que lo verifica.
- Guía paso a paso para el día del laboratorio.

Fuera de alcance: orquestación con réplicas (B8), chat (B2).

## Restricciones

- Nunca `localhost` entre servicios; nada se construye en el laboratorio; ningún secreto en el repositorio.
- Los archivos de variables no se llaman `.env*` (convención de este flujo de trabajo); se pasan con `--env-file`.

## Modo de trabajo

- Verificación: `docker compose config` por nodo, la simulación del plan A con la e2e o un humo propio, y los casos de fallos si se pueden apuntar a la simulación.
- Ruta: delegada.

## Tareas

- [x] T1 · Compose por máquina, direcciones, secretos y simulación local del plan A. Evidencia: `docker compose config` de los cinco nodos con valores de prueba; `deploy/nodes/simulate.sh` (cinco proyectos que solo se hablan por puertos del anfitrión): todo en verde, acceso, catálogo y una gota por WSS; con el nodo de Python parado, 5 gotas `applied` y recuperación completa (repetido por el orquestador: 9 s en total, con caché); shellcheck limpio.
- [x] T2 · Scripts de PowerShell (diagnóstico, despliegue, salud) y guía del día del laboratorio. Evidencia: los cuatro scripts se analizan sin errores con el analizador de pwsh 7; partes ejecutadas en pwsh sobre Linux contra la simulación. Sin probar: Windows PowerShell 5.1, `Test-NetConnection` y el laboratorio real (cortafuegos, máquina apagada). Pendiente: las IP de cada nodo (`[DATO]` en `lab-addresses.example`).

## Progreso

- T1 en `feature/b1-lab-plan-a` (ruta delegada).
