<#
  NetScope-Agent fuer Windows installieren (von der NetScope-Instanz ausgeliefert).

    & ([scriptblock]::Create((irm '<URL>/agent/install.ps1'))) -Token nse_...
    & ([scriptblock]::Create((irm '<URL>/agent/install.ps1'))) -Uninstall

  In einer PowerShell als Administrator ausfuehren. Der Agent laeuft als Dienst NetScopeAgent
  unter dem virtuellen Konto "NT SERVICE\NetScopeAgent" (ohne Administratorrechte), liest nur
  und verbindet sich von sich aus mit NetScope. -RunAsSystem laesst ihn als LocalSystem laufen
  (noetig fuer die DHCP-Leases auf einem Domaenencontroller). -Fingerprint pinnt ein
  selbst signiertes Zertifikat der Instanz (SHA-256).
#>
param(
    [string]$Token = '',
    [string]$Url = '@@URL@@',
    [string]$Fingerprint = '',
    [switch]$RunAsSystem,
    [switch]$Uninstall
)
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

$Name = 'NetScopeAgent'
$Account = "NT SERVICE\$Name"
$ProgramDir = Join-Path $env:ProgramFiles 'NetScope Agent'
$DataDir = Join-Path $env:ProgramData 'NetScope Agent'
$Exe = Join-Path $ProgramDir 'netscope-agent.exe'
$Conf = Join-Path $DataDir 'agent.json'

$me = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())
if (-not $me.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    throw 'Bitte in einer PowerShell als Administrator ausfuehren.'
}

function Stop-Agent {
    $svc = Get-Service -Name $Name -ErrorAction SilentlyContinue
    if ($svc -and $svc.Status -ne 'Stopped') {
        Stop-Service -Name $Name -Force
        $svc.WaitForStatus('Stopped', [TimeSpan]::FromSeconds(30))
    }
}

if ($Uninstall) {
    Stop-Agent
    if (Get-Service -Name $Name -ErrorAction SilentlyContinue) { & sc.exe delete $Name | Out-Null }
    Remove-Item -Recurse -Force $ProgramDir, $DataDir -ErrorAction SilentlyContinue
    Write-Host 'NetScope-Agent entfernt. In NetScope unter Agents kann der Eintrag geloescht werden.'
    return
}

if ($Token -notlike 'nse_*') { throw '-Token nse_... fehlt (Installationsbefehl aus NetScope unter Agents kopieren).' }
$Url = $Url.TrimEnd('/')

$arch = $env:PROCESSOR_ARCHITEW6432
if (-not $arch) { $arch = $env:PROCESSOR_ARCHITECTURE }
switch ($arch) {
    'AMD64' { $platform = 'windows-amd64' }
    'ARM64' { $platform = 'windows-arm64' }
    default { throw "Architektur $arch wird nicht unterstuetzt (amd64, arm64)." }
}

$oldProtocol = [Net.ServicePointManager]::SecurityProtocol
$oldCallback = [Net.ServicePointManager]::ServerCertificateValidationCallback
$tmp = Join-Path ([IO.Path]::GetTempPath()) ('netscope-agent-' + [guid]::NewGuid().ToString('N'))
try {
    [Net.ServicePointManager]::SecurityProtocol = $oldProtocol -bor [Net.SecurityProtocolType]::Tls12
    if ($Fingerprint) {
        # self-signed certificate of the instance: accept exactly the pinned one
        $pin = ($Fingerprint -replace '[:\s]', '').ToUpperInvariant()
        if ($pin -notmatch '^[0-9A-F]{64}$') { throw '-Fingerprint erwartet den SHA-256 des Zertifikats (64 Hex-Zeichen).' }
        [Net.ServicePointManager]::ServerCertificateValidationCallback = [scriptblock]::Create(
            "param(`$s, `$cert) ([BitConverter]::ToString([Security.Cryptography.SHA256]::Create().ComputeHash(`$cert.GetRawCertData())) -replace '-', '') -eq '$pin'")
    }
    New-Item -ItemType Directory -Path $tmp | Out-Null
    Write-Host "Lade NetScope-Agent ($platform) von $Url ..."
    Invoke-WebRequest -UseBasicParsing -Uri "$Url/agent/bin/$platform" -OutFile (Join-Path $tmp 'agent.exe')
    $sum = Invoke-WebRequest -UseBasicParsing -Uri "$Url/agent/bin/$platform.sha256"
    $text = if ($sum.Content -is [byte[]]) { [Text.Encoding]::ASCII.GetString($sum.Content) } else { [string]$sum.Content }
    $want = ($text.Trim() -split '\s+')[0]
    $got = (Get-FileHash -Algorithm SHA256 (Join-Path $tmp 'agent.exe')).Hash
    if ($got -ne $want) { throw 'Pruefsumme passt nicht - Download beschaedigt?' }

    Stop-Agent
    New-Item -ItemType Directory -Force -Path $ProgramDir, $DataDir | Out-Null
    Copy-Item -Force (Join-Path $tmp 'agent.exe') $Exe
} finally {
    [Net.ServicePointManager]::SecurityProtocol = $oldProtocol
    [Net.ServicePointManager]::ServerCertificateValidationCallback = $oldCallback
    Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}

# service: automatic start, restart after errors and after self-updates (non-zero exit code)
$bin = '"' + $Exe + '" run --config "' + $Conf + '"'
if (Get-Service -Name $Name -ErrorAction SilentlyContinue) {
    Set-ItemProperty -Path "HKLM:\SYSTEM\CurrentControlSet\Services\$Name" -Name ImagePath -Value $bin
    Set-Service -Name $Name -StartupType Automatic
} else {
    New-Service -Name $Name -BinaryPathName $bin -DisplayName 'NetScope-Agent' -StartupType Automatic `
        -Description 'Liefert Inventar und Auslastung an NetScope (nur ausgehende Verbindungen).' | Out-Null
}
& sc.exe failure $Name reset= 86400 actions= restart/10000/restart/10000/restart/60000 | Out-Null
& sc.exe failureflag $Name 1 | Out-Null
if ($RunAsSystem) {
    & sc.exe --% config NetScopeAgent obj= LocalSystem password= "" | Out-Null
    $runAs = 'LocalSystem'
} else {
    & sc.exe sidtype $Name unrestricted | Out-Null
    # a virtual account has no password: it must be NULL (no password= at all), an empty
    # string is rejected with 1057 (ChangeServiceConfig)
    & sc.exe config $Name obj= $Account | Out-Null
    if ($LASTEXITCODE -ne 0) { throw "Dienstkonto $Account konnte nicht gesetzt werden (sc.exe $LASTEXITCODE)." }
    $runAs = $Account
}

# only SYSTEM and administrators may read the agent secret; the service account may also
# write its log and replace its own binary (self-update)
function Protect-Dir([string]$path, [bool]$private) {
    $acl = Get-Acl -Path $path
    $inherit = [Security.AccessControl.InheritanceFlags]'ContainerInherit, ObjectInherit'
    if ($private) {
        $acl = New-Object Security.AccessControl.DirectorySecurity
        $acl.SetAccessRuleProtection($true, $false)
        foreach ($sid in 'S-1-5-18', 'S-1-5-32-544') {
            $id = New-Object Security.Principal.SecurityIdentifier($sid)
            $acl.AddAccessRule((New-Object Security.AccessControl.FileSystemAccessRule($id, 'FullControl', $inherit, 'None', 'Allow')))
        }
    }
    if (-not $RunAsSystem) {
        $svcId = New-Object Security.Principal.NTAccount($Account)
        $acl.AddAccessRule((New-Object Security.AccessControl.FileSystemAccessRule($svcId, 'Modify', $inherit, 'None', 'Allow')))
    }
    Set-Acl -Path $path -AclObject $acl
}
Protect-Dir $DataDir $true
Protect-Dir $ProgramDir $false

# DHCP server: read-only access to the leases through the local group "DHCP Users"
if ((Get-Service -Name DHCPServer -ErrorAction SilentlyContinue) -and -not $RunAsSystem) {
    $role = (Get-CimInstance Win32_ComputerSystem).DomainRole
    $group = Get-LocalGroup -ErrorAction SilentlyContinue | Where-Object { $_.Name -in 'DHCP Users', 'DHCP-Benutzer' } | Select-Object -First 1
    if ($role -ge 4 -or -not $group) {
        Write-Warning 'DHCP-Server erkannt: fuer die Leases braucht der Agent Lesezugriff. Auf einem Domaenencontroller mit -RunAsSystem installieren.'
    } else {
        try { Add-LocalGroupMember -Group $group.Name -Member $Account -ErrorAction Stop }
        catch { if ($_.FullyQualifiedErrorId -notlike '*MemberExists*') { Write-Warning "Gruppe $($group.Name): $($_.Exception.Message)" } }
    }
}

$enroll = @('enroll', '--url', $Url, '--token', $Token, '--config', $Conf)
if ($Fingerprint) { $enroll += @('--fingerprint', $Fingerprint) }
& $Exe @enroll
if ($LASTEXITCODE -ne 0) { throw "Anmeldung bei NetScope fehlgeschlagen (Exit-Code $LASTEXITCODE)." }

Start-Service -Name $Name
Write-Host "Dienst $Name laeuft als $runAs (Protokoll: $DataDir\agent.log)."
Write-Host 'Fertig - das System erscheint in NetScope unter Agents und in der Geraeteliste.'
