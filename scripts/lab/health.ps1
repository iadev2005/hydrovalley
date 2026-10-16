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
    [Parameter(Mandatory = $true)][string]$Addresses
)

$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'common.ps1')

$values = Read-LabEnvFile -Path $Addresses
$proxy = Get-LabNodeEndpoint -Node ($script:LabNodes | Where-Object { $_.Name -eq 'proxy' }) -Addresses $values
if (Test-LabPlaceholder $proxy.Address) {
    Write-Error "LAB_PROXY_HOST sigue sin rellenar en $Addresses"
    exit 2
}

$uri = "https://$($proxy.Address):$($proxy.Port)/api/system"
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
    Write-Host ("Hub: tick {0}, circuito {1}, cola {2}, sesiones {3}" -f $system.hub.tick, $system.hub.breaker, $system.hub.queue, $system.hub.sessions)
}

$down = @($system.services | Where-Object { $_.status -ne 'up' })
if ($down.Count -gt 0 -or @($system.services).Count -ne 5) {
    Write-Host ("Servicios que no estan en up: {0}" -f (($down | ForEach-Object { $_.name }) -join ', ')) -ForegroundColor Yellow
    exit 1
}
Write-Host 'Los cinco servicios estan en up.' -ForegroundColor Green
