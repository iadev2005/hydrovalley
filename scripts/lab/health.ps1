<#
.SYNOPSIS
  Muestra el estado de los cinco servicios segun el proxy (GET /api/system).

.PARAMETER Addresses
  Archivo de direcciones (deploy\nodes\lab-addresses.local).

.EXAMPLE
  powershell -ExecutionPolicy Bypass -File scripts\lab\health.ps1 -Addresses deploy\nodes\lab-addresses.local

.NOTES
  El proxy usa un certificado autofirmado: aqui se acepta sin verificar porque
  solo se lee el estado publico del panel. Sale con 0 si los cinco estan en up.
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$Addresses,
    [switch]$Standby
)

$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'common.ps1')

$values = Read-LabEnvFile -Path $Addresses
$targetHost = $values['LAB_PROXY_HOST']
$targetPort = if ($values.ContainsKey('HV_PROXY_PORT')) { $values['HV_PROXY_PORT'] } else { '8080' }
if ($Standby) {
    if (-not $values.ContainsKey('LAB_STANDBY_HOST') -or (Test-LabPlaceholder $values['LAB_STANDBY_HOST'])) {
        Write-Error "LAB_STANDBY_HOST no esta configurado en $Addresses"
        exit 2
    }
    $targetHost = $values['LAB_STANDBY_HOST']
    if ($values.ContainsKey('HV_STANDBY_PROXY_PORT')) { $targetPort = $values['HV_STANDBY_PROXY_PORT'] }
} elseif (Test-LabPlaceholder $targetHost) {
    Write-Error "LAB_PROXY_HOST sigue sin rellenar en $Addresses"
    exit 2
}

$uri = "https://${targetHost}:${targetPort}/api/system"
Write-Host "Consultando $uri"
try {
    $system = Invoke-LabJson -Uri $uri
} catch {
    Write-Host "No se pudo consultar el proxy: $($_.Exception.Message)" -ForegroundColor Red
    Write-Host 'Compruebe que el nodo proxy esta levantado y que el cortafuegos deja pasar el puerto (diagnose.ps1).'
    exit 1
}

$system.services | Select-Object @{ n = 'Servicio'; e = { $_.name } }, @{ n = 'Estado'; e = { $_.status } }, @{ n = 'Detalle'; e = { $_.detail } } | Format-Table -AutoSize
if ($system.hub) {
    $bootInfo = if ($system.hub.boot_id) { ", boot_id $($system.hub.boot_id), restaurado en tick $($system.hub.restored_tick)" } else { '' }
    Write-Host ("Hub: tick {0}, circuito {1}, cola {2}, sesiones {3}{4}" -f $system.hub.tick, $system.hub.breaker, $system.hub.queue, $system.hub.sessions, $bootInfo)
}

$standbyFailed = 0
if ($values.ContainsKey('LAB_STANDBY_HOST') -and -not (Test-LabPlaceholder $values['LAB_STANDBY_HOST'])) {
    $sbHost = $values['LAB_STANDBY_HOST']
    $sbProxyPort = if ($values.ContainsKey('HV_STANDBY_PROXY_PORT') -and $values['HV_STANDBY_PROXY_PORT'] -match '^\d+$') { [int]$values['HV_STANDBY_PROXY_PORT'] } elseif ($values.ContainsKey('HV_PROXY_PORT') -and $values['HV_PROXY_PORT'] -match '^\d+$') { [int]$values['HV_PROXY_PORT'] } else { 8080 }
    $sbHubPort   = if ($values.ContainsKey('HV_STANDBY_HUB_PORT')   -and $values['HV_STANDBY_HUB_PORT']   -match '^\d+$') { [int]$values['HV_STANDBY_HUB_PORT']   } elseif ($values.ContainsKey('HV_HUB_PORT')   -and $values['HV_HUB_PORT']   -match '^\d+$') { [int]$values['HV_HUB_PORT']   } else { 8081 }
    $sbWorldPort = if ($values.ContainsKey('HV_STANDBY_WORLD_PORT') -and $values['HV_STANDBY_WORLD_PORT'] -match '^\d+$') { [int]$values['HV_STANDBY_WORLD_PORT'] } elseif ($values.ContainsKey('HV_WORLD_PORT') -and $values['HV_WORLD_PORT'] -match '^\d+$') { [int]$values['HV_WORLD_PORT'] } else { 50051 }

    Write-Host ''
    Write-Host "Comprobando componentes de relevo en standby ($sbHost)..."
    $sbRows = @()

    # 1. proxy-standby (:8080)
    $sbProxyState = 'CAIDO'
    try {
        $sbHealth = Invoke-LabJson -Uri "https://${sbHost}:${sbProxyPort}/api/health" -TimeoutSec 5
        if ($sbHealth.status -eq 'ok') { $sbProxyState = 'listo (up)' }
    } catch {
        $sbProxyState = 'NO RESPONDE'
        $standbyFailed++
    }
    $sbRows += New-Object PSObject -Property @{ Componente = 'proxy-standby'; Endpoint = "${sbHost}:${sbProxyPort}"; Estado = $sbProxyState }

    # 2. hub-standby (:8081)
    $sbHubState = 'CAIDO'
    try {
        $hubResp = Invoke-WebRequest -Uri "http://${sbHost}:${sbHubPort}/readyz" -UseBasicParsing -TimeoutSec 5 -ErrorAction Stop
        $hubJson = $hubResp.Content | ConvertFrom-Json
        if ($hubJson.ready) { $sbHubState = "ACTIVO (tick $($hubJson.tick))" } else { $sbHubState = 'listo (standby)' }
    } catch {
        $errResp = $_.Exception.Response
        if ($errResp -and [int]$errResp.StatusCode -eq 503) {
            $sbHubState = 'en espera (standby listo para relevo)'
        } else {
            $sbHubState = 'NO RESPONDE'
            $standbyFailed++
        }
    }
    $sbRows += New-Object PSObject -Property @{ Componente = 'hub-standby'; Endpoint = "${sbHost}:${sbHubPort}"; Estado = $sbHubState }

    # 3. world-standby (:50051)
    $worldTcpOk = Test-NetConnection -ComputerName $sbHost -Port $sbWorldPort -InformationLevel Quiet -WarningAction SilentlyContinue
    $sbWorldState = if ($worldTcpOk) { 'escuchando (listo para relevo)' } else { $standbyFailed++; 'NO RESPONDE' }
    $sbRows += New-Object PSObject -Property @{ Componente = 'world-standby'; Endpoint = "${sbHost}:${sbWorldPort}"; Estado = $sbWorldState }

    $sbRows | Select-Object Componente, Endpoint, Estado | Format-Table -AutoSize
}

$down = @($system.services | Where-Object { $_.status -ne 'up' })
if ($down.Count -gt 0 -or @($system.services).Count -ne 5) {
    Write-Host ("Servicios que no estan en up: {0}" -f (($down | ForEach-Object { $_.name }) -join ', ')) -ForegroundColor Yellow
    exit 1
}
if ($standbyFailed -gt 0) {
    Write-Host ("Atencion: $standbyFailed componente(s) del nodo standby no responden. Revise docker compose en la maquina standby y el cortafuegos.") -ForegroundColor Yellow
    exit 1
}
Write-Host 'Los cinco servicios estan en up.' -ForegroundColor Green
