# Guía del laboratorio para la defensa

Qué hacer en las máquinas del laboratorio para tener la versión de `dev` del 30 de octubre: interfaz nueva, río y rocas, y cifras del juego ajustadas tras la predefensa.

## Qué cambió

| Cambio | PR | Nodo que hay que actualizar |
| --- | --- | --- |
| Interfaz compacta, clic contextual y herramientas tipo Paint | [#80](https://github.com/iadev2005/hydrovalley/pull/80) | `proxy` |
| El reinicio del valle genera el río, el lago, la laguna y las rocas si faltan | [#81](https://github.com/iadev2005/hydrovalley/pull/81) | `world` |
| Menos basura y fuego; las instalaciones suben un 5 % en vez de un 15 % | [#81](https://github.com/iadev2005/hydrovalley/pull/81) | `persistence` (y reiniciar `world` después) |

Las imágenes `:dev` con estos cambios ya están publicadas en `ghcr.io/iadev2005`. No hay que compilar nada en el laboratorio.

### Por qué no salían el río ni las rocas

El terreno solo se generaba cuando el valle nacía en el tick 0. La base de datos del laboratorio ya tenía instantáneas de antes de la expansión de jugabilidad, sin terreno, y el simulador las restaura tal cual. «Reiniciar valle» conservaba el terreno existente, que era ninguno. Desde el PR #81, el reinicio lo genera con la semilla del valle si no hay ninguna casilla de agua ni de roca. Un valle que ya tiene terreno no cambia.

## Reparto de máquinas

| PC | IP | Nodo |
| --- | --- | --- |
| compu 3 | 10.68.17.3 | `db` |
| compu 9 | 10.68.17.9 | `persistence` |
| compu 10 | 10.68.17.10 | `world` |
| compu 12 | 10.68.17.12 | `hub` (y chat) |
| compu 11 | 10.68.17.80 | `proxy` |

`deploy/nodes/lab-addresses.local` es el mismo en las cinco PC y no cambia.

## Credenciales de cada nodo

Van en `deploy/nodes/secrets.local` de cada PC. **Son las mismas de la predefensa: no hay que generar nada nuevo.** Nunca se suben al repositorio ni se pegan en chats.

| Nodo | Qué lleva su `secrets.local` |
| --- | --- |
| `db` | `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB` |
| `persistence` | Las tres de PostgreSQL (iguales que en `db`), `ADMIN_USERNAME` y `ADMIN_PASSWORD_HASH` (el hash entre comillas simples) |
| `world` | Nada (no usa `-Secrets`) |
| `hub` | `TOKEN_PUBLIC_KEY` (y `HV_ADMIN_USERNAME` y `HV_ADMIN_PASSWORD` solo si se usan bots) |
| `proxy` | `TOKEN_PRIVATE_KEY` |

Para entrar al juego como administrador hacen falta el usuario `ADMIN_USERNAME` y la contraseña **en claro** con la que se generó el hash. La tiene quien la eligió.

## Paso a paso

### 0. En cada PC, antes de empezar

1. Abrir **Docker Desktop** y esperar a que diga *Engine running*.
2. Abrir un **PowerShell normal** (no de administrador) en la carpeta del proyecto y traer los cambios:

   ```powershell
   cd $HOME\Desktop\hydrovalley
   git pull
   ```

3. Si una descarga de imágenes responde `denied` o `unauthorized`:

   ```powershell
   docker login ghcr.io
   ```

   Usuario: la cuenta de GitHub. Contraseña: un token clásico con solo el permiso `read:packages`. Al terminar el día: `docker logout ghcr.io`.

### 1. compu 9 · `persistence` (catálogo nuevo)

```powershell
powershell -ExecutionPolicy Bypass -File scripts\lab\deploy-node.ps1 -Node persistence -Addresses deploy\nodes\lab-addresses.local -Secrets deploy\nodes\secrets.local -Tag dev
```

Al arrancar, la persistencia actualiza el catálogo en la base de datos con las cifras nuevas.

### 2. compu 10 · `world` (arreglo del terreno)

Hacerlo **después** del paso 1, para que el simulador cargue el catálogo nuevo al arrancar.

```powershell
powershell -ExecutionPolicy Bypass -File scripts\lab\deploy-node.ps1 -Node world -Addresses deploy\nodes\lab-addresses.local -Tag dev
```

### 3. compu 11 · `proxy` (interfaz nueva)

```powershell
powershell -ExecutionPolicy Bypass -File scripts\lab\deploy-node.ps1 -Node proxy -Addresses deploy\nodes\lab-addresses.local -Secrets deploy\nodes\secrets.local -Tag dev
```

`db` (compu 3) y `hub` (compu 12) **no se tocan**.

### 4. Comprobar desde cualquier PC

```powershell
powershell -ExecutionPolicy Bypass -File scripts\lab\health.ps1 -Addresses deploy\nodes\lab-addresses.local
```

Todos los servicios deben salir en `up`.

### 5. Hacer aparecer el río y las rocas

1. Abrir `https://10.68.17.80:8080` y recargar con **Ctrl+F5**. Si el navegador avisa del certificado: *Avanzado › Continuar*.
2. Entrar con el usuario administrador.
3. Arriba a la derecha, **menú de la cuenta › Administración › Reiniciar valle › Sí, reiniciar**.
4. En uno o dos segundos aparecen el río (lado derecho), el lago, la laguna del sur y las rocas.

El reinicio borra el progreso de todos los jugadores (casillas, hojas e instalaciones), pero conserva las cuentas y el tick. **Hacerlo antes de la demostración.**

## Si algo falla

| Síntoma | Qué hacer |
| --- | --- |
| Tras reiniciar sigue sin río | El nodo `world` no tiene la imagen nueva. Repetir el paso 2 y comprobar la fecha de la imagen con `docker image ls ghcr.io/iadev2005/hydrovalley-world`. Después, reiniciar el valle otra vez |
| La interfaz es la vieja (botones «Soltar gota») | Repetir el paso 3 y recargar con Ctrl+F5 |
| Los precios siguen subiendo un 15 % | Repetir el paso 1 y después el paso 2 (el simulador lee el catálogo al arrancar) |
| `health.ps1` muestra un servicio en `down` | Mirar los registros en esa PC: `docker logs --tail 50 <contenedor>` (`docker ps` da los nombres). Volver a ejecutar `deploy-node.ps1` de ese nodo |
| Último recurso si no aparece el terreno | En la compu 3: `deploy-node.ps1 -Node db … -Down -Volumes` y volver a levantar `db`. **Borra toda la base de datos**, incluidas las cuentas; el valle nace de cero con terreno |

## Al terminar

Las máquinas son compartidas: dejarlas como estaban.

```powershell
powershell -ExecutionPolicy Bypass -File scripts\lab\deploy-node.ps1 -Node <nodo> -Addresses deploy\nodes\lab-addresses.local -Secrets deploy\nodes\secrets.local -Down
docker logout ghcr.io
```

Después, borrar `deploy\nodes\secrets.local` de cada PC. En la compu 10 (`world`), `-Down` va sin `-Secrets`.
