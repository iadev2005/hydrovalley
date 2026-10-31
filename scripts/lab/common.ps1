# Funciones comunes de los scripts del laboratorio (plan A). Se cargan con
# `. "$PSScriptRoot\common.ps1"`; no se ejecuta sola.
#
# Compatible con Windows PowerShell 5.1 (el del laboratorio) y con PowerShell 7.
# El texto va sin tildes a proposito: PowerShell 5.1 lee como ANSI un archivo
# UTF-8 sin BOM y las desfiguraria.

# Los cinco nodos del plan A, con su variable de direccion y de puerto
# (deploy/nodes/lab-addresses.example) y el puerto por defecto.
$script:LabNodes = @(
    @{ Name = 'db';              HostKey = 'LAB_DB_HOST';          PortKey = 'HV_DB_PORT';            DefaultPort = 5433  },
    @{ Name = 'persistence';     HostKey = 'LAB_PERSISTENCE_HOST'; PortKey = 'HV_PERSISTENCE_PORT';   DefaultPort = 50051 },
    @{ Name = 'world';           HostKey = 'LAB_WORLD_HOST';       PortKey = 'HV_WORLD_PORT';         DefaultPort = 50051 },
    @{ Name = 'hub';             HostKey = 'LAB_HUB_HOST';         PortKey = 'HV_HUB_PORT';           DefaultPort = 8081  },
    @{ Name = 'proxy';           HostKey = 'LAB_PROXY_HOST';       PortKey = 'HV_PROXY_PORT';         DefaultPort = 8080  },
    @{ Name = 'standby (proxy)'; HostKey = 'LAB_STANDBY_HOST';     PortKey = 'HV_STANDBY_PROXY_PORT'; DefaultPort = 8080;  Optional = $true },
    @{ Name = 'standby (hub)';   HostKey = 'LAB_STANDBY_HOST';     PortKey = 'HV_STANDBY_HUB_PORT';   DefaultPort = 8081;  Optional = $true },
    @{ Name = 'standby (world)'; HostKey = 'LAB_STANDBY_HOST';     PortKey = 'HV_STANDBY_WORLD_PORT'; DefaultPort = 50051; Optional = $true }
)

# Regla de cortafuegos que debe crear el encargado del laboratorio, como
# administrador, en cada maquina que use el equipo (docs/documento-inicial.md).
$script:FirewallCommand = 'New-NetFirewallRule -DisplayName "hydrovalley (proyecto Sistemas Distribuidos)" -Direction Inbound -Protocol TCP -LocalPort 8080,8081,8082,50051,5433 -RemoteAddress 10.68.17.0/24 -Action Allow -Profile Domain,Private'

# Lee un archivo KEY=VALUE (direcciones o secretos) y devuelve una tabla hash.
# Ignora lineas vacias y comentarios; quita comillas simples o dobles de los
# valores. No interpreta `$` ni expansiones: el valor se toma tal cual.
function Read-LabEnvFile {
    param([Parameter(Mandatory = $true)][string]$Path)
    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
        throw "No existe el archivo: $Path"
    }
    $values = @{}
    foreach ($line in (Get-Content -LiteralPath $Path)) {
        $text = $line.Trim()
        if ($text -eq '' -or $text.StartsWith('#')) { continue }
        $eq = $text.IndexOf('=')
        if ($eq -lt 1) { continue }
        $key = $text.Substring(0, $eq).Trim()
        $value = $text.Substring($eq + 1).Trim()
        if ($value.Length -ge 2) {
            $first = $value[0]
            $last = $value[$value.Length - 1]
            if (($first -eq '"' -and $last -eq '"') -or ($first -eq "'" -and $last -eq "'")) {
                $value = $value.Substring(1, $value.Length - 2)
            }
        }
        $values[$key] = $value
    }
    return $values
}

# Un valor sin rellenar: vacio o todavia con el marcador [DATO: ...].
function Test-LabPlaceholder {
    param([string]$Value)
    return ([string]::IsNullOrWhiteSpace($Value) -or $Value.Contains('[DATO'))
}

# Direccion y puerto de un nodo segun el archivo de direcciones.
function Get-LabNodeEndpoint {
    param([hashtable]$Node, [hashtable]$Addresses)
    $port = $Node.DefaultPort
    if ($Addresses.ContainsKey($Node.PortKey) -and $Addresses[$Node.PortKey] -match '^\d+$') {
        $port = [int]$Addresses[$Node.PortKey]
    }
    $address = ''
    if ($Addresses.ContainsKey($Node.HostKey)) { $address = $Addresses[$Node.HostKey] }
    return @{ Name = $Node.Name; Address = $address; Port = $port }
}

# Acepta el certificado autofirmado del proxy (solo para consultar el estado).
# PowerShell 7 usa -SkipCertificateCheck; Windows PowerShell 5.1 no lo tiene y
# necesita una politica de certificados y TLS 1.2 (el proxy no admite menos).
function Initialize-LabInsecureTls {
    if ($PSVersionTable.PSVersion.Major -ge 7) { return }
    if (-not ('LabTrustAllCertsPolicy' -as [type])) {
        Add-Type -TypeDefinition @'
using System.Net;
using System.Security.Cryptography.X509Certificates;
public class LabTrustAllCertsPolicy : ICertificatePolicy {
    public bool CheckValidationResult(ServicePoint sp, X509Certificate cert, WebRequest req, int problem) { return true; }
}
'@
    }
    [System.Net.ServicePointManager]::CertificatePolicy = New-Object LabTrustAllCertsPolicy
    [System.Net.ServicePointManager]::SecurityProtocol = [System.Net.SecurityProtocolType]::Tls12
}

# GET JSON del proxy con el certificado autofirmado aceptado.
function Invoke-LabJson {
    param([Parameter(Mandatory = $true)][string]$Uri, [int]$TimeoutSec = 10)
    Initialize-LabInsecureTls
    if ($PSVersionTable.PSVersion.Major -ge 7) {
        return Invoke-RestMethod -Uri $Uri -TimeoutSec $TimeoutSec -SkipCertificateCheck
    }
    return Invoke-RestMethod -Uri $Uri -TimeoutSec $TimeoutSec
}
