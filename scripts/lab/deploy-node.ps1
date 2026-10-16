<#
.SYNOPSIS
  Descarga y levanta (o baja) un nodo del plan A en esta maquina.

.DESCRIPTION
  Comprueba Docker y los archivos de variables, descarga las imagenes de
  ghcr.io (si los paquetes son privados pide `docker login ghcr.io` con un PAT
  de solo lectura, deploy\README.md), ejecuta `docker compose up -d` con el
  archivo deploy\nodes\<nodo>.yml y espera a que los contenedores esten sanos.
  Nada se construye aqui. Los nodos pueden levantarse en cualquier orden: uno
  puede quedar "sin salud" hasta que aparezcan los demas (RNF-09).

.PARAMETER Node
  db, persistence, world, hub o proxy.

.PARAMETER Addresses
  Archivo de direcciones (lab-addresses.local).

.PARAMETER Secrets
  Archivo de secretos (secrets.local) con SOLO las claves de este nodo. world no lo necesita.

.PARAMETER Tag
  Etiqueta de las imagenes (dev o vX.Y); sustituye a HV_TAG del archivo de direcciones.

.PARAMETER Down
  Baja el nodo en lugar de levantarlo. Conserva los datos de la base.

.PARAMETER Volumes
  Con -Down, borra tambien los volumenes (en el nodo db, borra la base de datos).

.PARAMETER ComposeDir
  Carpeta con los archivos <nodo>.yml. Por defecto, deploy\nodes junto a scripts\lab.

.EXAMPLE
  powershell -ExecutionPolicy Bypass -File scripts\lab\deploy-node.ps1 -Node world -Addresses deploy\nodes\lab-addresses.local -Tag dev

.EXAMPLE
  powershell -ExecutionPolicy Bypass -File scripts\lab\deploy-node.ps1 -Node proxy -Addresses deploy\nodes\lab-addresses.local -Secrets deploy\nodes\secrets.local -Down
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][ValidateSet('db', 'persistence', 'world', 'hub', 'proxy')][string]$Node,
    [Parameter(Mandatory = $true)][string]$Addresses,
    [string]$Secrets,
    [string]$Tag,
    [switch]$Down,
    [switch]$Volumes,
    [string]$ComposeDir,
    [int]$HealthTimeoutSec = 180
)

# Los programas nativos escriben su avance por stderr: no son errores.
$ErrorActionPreference = 'Continue'
. (Join-Path $PSScriptRoot 'common.ps1')

function Stop-Deploy {
    param([string]$Message, [int]$Code = 1)
    Write-Host $Message -ForegroundColor Red
    exit $Code
}

# Variables que necesita cada nodo: direcciones (obligatorias, con IP real) y
# secretos (obligatorios, no vacios). Coincide con los `:?` de cada .yml.
$needsAddresses = @{
    db          = @()
    persistence = @('LAB_DB_HOST')
    world       = @('LAB_PERSISTENCE_HOST')
    hub         = @('LAB_WORLD_HOST', 'LAB_PROXY_HOST')
    proxy       = @('LAB_PERSISTENCE_HOST', 'LAB_WORLD_HOST', 'LAB_HUB_HOST', 'LAB_PROXY_HOST')
}
$needsSecrets = @{
    db          = @('POSTGRES_USER', 'POSTGRES_PASSWORD', 'POSTGRES_DB')
    persistence = @('POSTGRES_USER', 'POSTGRES_PASSWORD', 'POSTGRES_DB')
    world       = @()
    hub         = @('TOKEN_PUBLIC_KEY')
    proxy       = @('TOKEN_PRIVATE_KEY')
}

# 1. Archivos.
if (-not $ComposeDir) {
    $candidates = @(
        (Join-Path $PSScriptRoot '..\..\deploy\nodes'),
        (Join-Path $PSScriptRoot 'nodes'),
        $PSScriptRoot
    )
    foreach ($c in $candidates) {
        if (Test-Path -LiteralPath (Join-Path $c "$Node.yml")) { $ComposeDir = $c; break }
    }
}
if (-not $ComposeDir -or -not (Test-Path -LiteralPath (Join-Path $ComposeDir "$Node.yml"))) {
    Stop-Deploy "No se encuentra $Node.yml. Copie deploy\nodes junto a scripts\lab o indique -ComposeDir."
}
$composeFile = (Resolve-Path -LiteralPath (Join-Path $ComposeDir "$Node.yml")).Path
try {
    $addressFile = (Resolve-Path -LiteralPath $Addresses).Path
    $addressValues = Read-LabEnvFile -Path $addressFile
} catch {
    Stop-Deploy "No se pudo leer el archivo de direcciones: $($_.Exception.Message)"
}

$secretFile = $null
$secretValues = @{}
if ($Secrets) {
    try {
        $secretFile = (Resolve-Path -LiteralPath $Secrets).Path
        $secretValues = Read-LabEnvFile -Path $secretFile
    } catch {
        Stop-Deploy "No se pudo leer el archivo de secretos: $($_.Exception.Message)"
    }
}

if (-not $Down) {
    foreach ($key in $needsAddresses[$Node]) {
        if (-not $addressValues.ContainsKey($key) -or (Test-LabPlaceholder $addressValues[$key])) {
            Stop-Deploy "Falta $key en $addressFile (vacio o todavia [DATO: ...])."
        }
    }
    foreach ($key in $needsSecrets[$Node]) {
        if (-not $secretValues.ContainsKey($key) -or [string]::IsNullOrWhiteSpace($secretValues[$key])) {
            Stop-Deploy "Falta $key en el archivo de secretos (-Secrets). El nodo $Node lo necesita."
        }
    }
}

# 2. Docker Desktop encendido (en cada visita hay que abrirlo primero).
& docker info *> $null
if ($LASTEXITCODE -ne 0) {
    Stop-Deploy 'Docker no responde. Abra Docker Desktop, espere a que diga "Engine running" y repita.'
}

if ($Tag) { $env:HV_TAG = $Tag }

$composeArgs = @('compose', '-p', "hv-$Node", '-f', $composeFile, '--env-file', $addressFile)
if ($secretFile) { $composeArgs += @('--env-file', $secretFile) }

# 3. Bajar el nodo.
if ($Down) {
    $downArgs = $composeArgs + @('down')
    if ($Volumes) { $downArgs += '-v' }
    & docker @downArgs
    if ($LASTEXITCODE -ne 0) { Stop-Deploy "docker compose down fallo (codigo $LASTEXITCODE)." }
    Write-Host "Nodo $Node detenido." -ForegroundColor Green
    exit 0
}

# 4. Descargar las imagenes. Si los paquetes de ghcr.io son privados (D1), la
# descarga falla con "denied" o "unauthorized": se pide el inicio de sesion y
# se repite una vez. Con paquetes publicos no hace falta nada.
Write-Host "Descargando las imagenes del nodo $Node..."
$pullOutput = (& docker @($composeArgs + @('pull')) 2>&1 | ForEach-Object { "$_" }) -join "`n"
$pullCode = $LASTEXITCODE
if ($pullCode -ne 0 -and $pullOutput -match 'denied|unauthorized|authentication required') {
    Write-Host ''
    Write-Host 'Las imagenes de ghcr.io parecen privadas. Inicie sesion con un PAT clasico de solo lectura (read:packages):' -ForegroundColor Yellow
    & docker login ghcr.io
    if ($LASTEXITCODE -ne 0) { Stop-Deploy 'docker login ghcr.io fallo.' }
    $pullOutput = (& docker @($composeArgs + @('pull')) 2>&1 | ForEach-Object { "$_" }) -join "`n"
    $pullCode = $LASTEXITCODE
}
if ($pullCode -ne 0) {
    Write-Host $pullOutput
    Stop-Deploy "docker compose pull fallo (codigo $pullCode). Compruebe la etiqueta -Tag y la conexion a ghcr.io."
}

# 5. Levantar.
& docker @($composeArgs + @('up', '-d'))
if ($LASTEXITCODE -ne 0) { Stop-Deploy "docker compose up fallo (codigo $LASTEXITCODE)." }

# 6. Esperar la salud de los contenedores del nodo.
Write-Host "Esperando la salud del nodo $Node (hasta $HealthTimeoutSec s)..."
$deadline = (Get-Date).AddSeconds($HealthTimeoutSec)
$states = @{}
do {
    $ids = @(& docker @($composeArgs + @('ps', '-q')) 2>$null | Where-Object { $_ })
    $states = @{}
    foreach ($id in $ids) {
        $name = (& docker inspect -f '{{.Name}}' $id).TrimStart('/')
        $state = & docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' $id
        $states[$name] = $state
    }
    $pending = @($states.GetEnumerator() | Where-Object { $_.Value -ne 'healthy' -and $_.Value -ne 'running' })
    if ($ids.Count -gt 0 -and $pending.Count -eq 0) { break }
    Start-Sleep -Seconds 3
} while ((Get-Date) -lt $deadline)

$states.GetEnumerator() | Sort-Object Name | ForEach-Object { Write-Host ("  {0}: {1}" -f $_.Name, $_.Value) }
if ($states.Count -eq 0 -or @($states.GetEnumerator() | Where-Object { $_.Value -ne 'healthy' -and $_.Value -ne 'running' }).Count -gt 0) {
    Write-Host "El nodo $Node esta arriba pero no sano todavia. Es normal si faltan otros nodos (se reconecta solo)." -ForegroundColor Yellow
    Write-Host 'Cuando esten todos, compruebe con health.ps1; para ver el motivo: docker compose -p hv-<nodo> logs.'
    exit 2
}
Write-Host "Nodo $Node sano." -ForegroundColor Green
exit 0
