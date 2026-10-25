<#
.SYNOPSIS
  Comprueba desde esta maquina que se alcanzan los puertos de los cinco nodos.

.DESCRIPTION
  Para cada nodo del archivo de direcciones hace una prueba TCP
  (Test-NetConnection) y muestra una tabla. Si algun puerto no responde,
  explica las causas probables y muestra la regla de cortafuegos que debe crear
  el encargado del laboratorio.

  Antes de levantar nada, "False" no distingue cortafuegos de "nadie escucha
  todavia". Para probar solo el cortafuegos, en la maquina de un nodo ejecute
  este script con -Listen (abre un puerto de prueba) y compruebe desde otra
  maquina. Cuenta no administradora: no cambia ninguna configuracion.

.PARAMETER Addresses
  Archivo de direcciones (deploy\nodes\lab-addresses.local).

.PARAMETER Listen
  Puertos TCP a abrir en esta maquina durante -ListenSeconds para que otra
  maquina los pruebe (por ejemplo 8081,50051).

.EXAMPLE
  powershell -ExecutionPolicy Bypass -File scripts\lab\diagnose.ps1 -Addresses deploy\nodes\lab-addresses.local

.EXAMPLE
  powershell -ExecutionPolicy Bypass -File scripts\lab\diagnose.ps1 -Addresses deploy\nodes\lab-addresses.local -Listen 8081
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$Addresses,
    [int[]]$Listen = @(),
    [int]$ListenSeconds = 300
)

$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'common.ps1')

if ($Listen.Count -gt 0) {
    $listeners = @()
    foreach ($port in $Listen) {
        $l = New-Object System.Net.Sockets.TcpListener([System.Net.IPAddress]::Any, $port)
        $l.Start()
        $listeners += $l
    }
    Write-Host ("Escuchando en los puertos {0} durante {1} s. Pruebe desde otra maquina con:" -f ($Listen -join ', '), $ListenSeconds)
    Write-Host '  Test-NetConnection -ComputerName <IP de esta maquina> -Port <puerto>'
    $deadline = (Get-Date).AddSeconds($ListenSeconds)
    try {
        while ((Get-Date) -lt $deadline) {
            foreach ($l in $listeners) {
                while ($l.Pending()) { $l.AcceptTcpClient().Close() }
            }
            Start-Sleep -Milliseconds 200
        }
    } finally {
        foreach ($l in $listeners) { $l.Stop() }
    }
    Write-Host 'Puertos de prueba cerrados.'
    return
}

$values = Read-LabEnvFile -Path $Addresses
$rows = @()
$failed = 0
$unset = 0
foreach ($node in $script:LabNodes) {
    $endpoint = Get-LabNodeEndpoint -Node $node -Addresses $values
    if (Test-LabPlaceholder $endpoint.Address) {
        if ($node.Optional) {
            $result = 'NO CONFIGURADO'
        } else {
            $result = 'SIN IP'
            $unset++
        }
    } else {
        $ok = Test-NetConnection -ComputerName $endpoint.Address -Port $endpoint.Port -InformationLevel Quiet -WarningAction SilentlyContinue
        if ($ok) { $result = 'abierto' } else { $result = 'BLOQUEADO'; $failed++ }
    }
    $rows += New-Object PSObject -Property @{
        Nodo = $endpoint.Name; Direccion = $endpoint.Address; Puerto = $endpoint.Port; Resultado = $result
    }
}

$rows | Select-Object Nodo, Direccion, Puerto, Resultado | Format-Table -AutoSize

if ($unset -gt 0) {
    Write-Warning "Faltan $unset direcciones en $Addresses (todavia dicen [DATO: ...]). Rellenelas y repita."
}
if ($failed -gt 0) {
    Write-Host ''
    Write-Host "No se alcanzaron $failed de los puertos. Causas probables:" -ForegroundColor Yellow
    Write-Host '  1. Falta la regla de entrada del cortafuegos en la maquina de destino: el primer'
    Write-Host '     reconocimiento mostro que el cortafuegos bloquea las conexiones entrantes entre'
    Write-Host '     maquinas. Esta cuenta no es administradora: debe crearla el encargado del'
    Write-Host '     laboratorio, en cada maquina que use el equipo, con este comando (como administrador):'
    Write-Host ''
    Write-Host "     $($script:FirewallCommand)"
    Write-Host ''
    Write-Host '  2. El nodo todavia no esta levantado (nadie escucha en ese puerto). Compruebelo con'
    Write-Host '     -Listen en la maquina de destino, o levante el nodo y repita.'
    Write-Host '  3. La IP del archivo no es la de esa maquina.'
    Write-Host '  Si la regla existe y sigue fallando, una directiva de grupo del dominio puede anularla:'
    Write-Host '  preguntelo al encargado. Mientras tanto, plan B: todo en una sola maquina (deploy\compose.yml).'
    exit 1
}
if ($unset -eq 0) {
    Write-Host 'Todos los puertos responden.' -ForegroundColor Green
}
