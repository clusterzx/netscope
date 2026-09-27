# NetScope agent: read-only inventory of a Windows host. A fixed list of queries (CIM,
# registry, NetTCPIP/NetAdapter cmdlets, Windows Update API, DhcpServer module); nothing here
# changes the system. The result is ONE JSON document on stdout, parsed by the instance
# (internal/agent/wininv). Every section catches its own errors into "errors".
#
# Keep this file ASCII: it reaches PowerShell through stdin.
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding $false
$collectPackages = @@PACKAGES@@
$timeoutSec = @@TIMEOUT@@

$out = [ordered]@{ format = 1; errors = [ordered]@{}; unavailable = New-Object System.Collections.Generic.List[string] }

function Section([string]$name, [scriptblock]$body) {
    try { & $body } catch { $out.errors[$name] = $_.Exception.Message }
}

function Iso($d) {
    if ($d -is [datetime] -and $d.Year -gt 1601) { return $d.ToUniversalTime().ToString('o') }
    return $null
}

function Has([string]$command) { return [bool](Get-Command $command -ErrorAction SilentlyContinue) }

$arch = switch ($env:PROCESSOR_ARCHITECTURE) { 'AMD64' { 'x64' } 'ARM64' { 'arm64' } default { 'x86' } }

Section 'os' {
    $os = Get-CimInstance Win32_OperatingSystem
    $cv = Get-ItemProperty 'HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion'
    $out.os = [ordered]@{
        caption          = [string]$os.Caption
        version          = [string]$os.Version
        build            = [string]$os.BuildNumber
        ubr              = $cv.UBR
        displayVersion   = [string]$cv.DisplayVersion
        releaseId        = [string]$cv.ReleaseId
        edition          = [string]$cv.EditionID
        installationType = [string]$cv.InstallationType
        productType      = [int]$os.ProductType
        architecture     = [string]$os.OSArchitecture
        installDate      = Iso $os.InstallDate
        lastBoot         = Iso $os.LastBootUpTime
        hostname         = [string]$env:COMPUTERNAME
        timezone         = [string](Get-CimInstance Win32_TimeZone).StandardName
    }
}

Section 'computer' {
    $cs = Get-CimInstance Win32_ComputerSystem
    $bios = Get-CimInstance Win32_BIOS
    $chassis = @(Get-CimInstance Win32_SystemEnclosure | ForEach-Object { $_.ChassisTypes } | ForEach-Object { [int]$_ })
    $out.computer = [ordered]@{
        manufacturer = [string]$cs.Manufacturer
        model        = [string]$cs.Model
        family       = [string]$cs.SystemFamily
        domain       = [string]$cs.Domain
        partOfDomain = [bool]$cs.PartOfDomain
        domainRole   = [int]$cs.DomainRole
        memoryBytes  = [int64]$cs.TotalPhysicalMemory
        serial       = ([string]$bios.SerialNumber).Trim()
        biosVendor   = [string]$bios.Manufacturer
        biosVersion  = [string]$bios.SMBIOSBIOSVersion
        biosDate     = Iso $bios.ReleaseDate
        chassis      = $chassis
    }
}

Section 'cpu' {
    $out.cpu = @(Get-CimInstance Win32_Processor | ForEach-Object {
            [ordered]@{ name = (([string]$_.Name) -replace '\s+', ' ').Trim(); cores = [int]$_.NumberOfCores
                threads = [int]$_.NumberOfLogicalProcessors; mhz = [int]$_.MaxClockSpeed }
        })
}

Section 'volumes' {
    $out.volumes = @(Get-CimInstance Win32_LogicalDisk -Filter 'DriveType=3' | ForEach-Object {
            [ordered]@{ drive = [string]$_.DeviceID; label = [string]$_.VolumeName; fs = [string]$_.FileSystem
                size = [int64]$_.Size; free = [int64]$_.FreeSpace }
        })
}

Section 'disks' {
    $out.disks = @(Get-CimInstance Win32_DiskDrive | ForEach-Object {
            [ordered]@{ model = ([string]$_.Model).Trim(); size = [int64]$_.Size; interface = [string]$_.InterfaceType
                media = [string]$_.MediaType; serial = ([string]$_.SerialNumber).Trim() }
        })
}

Section 'interfaces' {
    if (-not (Has 'Get-NetAdapter')) { $out.unavailable.Add('interfaces'); return }
    $ips = @(Get-NetIPAddress -ErrorAction SilentlyContinue)
    $out.interfaces = @(Get-NetAdapter | ForEach-Object {
            $a = $_
            [ordered]@{
                index       = [int]$a.ifIndex
                name        = [string]$a.Name
                description = [string]$a.InterfaceDescription
                mac         = [string]$a.MacAddress
                status      = [string]$a.Status
                speedBps    = [int64]$a.ReceiveLinkSpeed
                virtual     = [bool]$a.Virtual
                hardware    = [bool]$a.HardwareInterface
                addresses   = @($ips | Where-Object { $_.InterfaceIndex -eq $a.ifIndex } | ForEach-Object {
                        [ordered]@{ address = [string]$_.IPAddress; prefixLength = [int]$_.PrefixLength
                            family = [string]$_.AddressFamily; prefixOrigin = [string]$_.PrefixOrigin
                            suffixOrigin = [string]$_.SuffixOrigin; state = [string]$_.AddressState }
                    })
            }
        })
}

if ($collectPackages) {
    Section 'software' {
        $hives = @(
            @{ path = 'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\*'; arch = $arch },
            @{ path = 'HKLM:\SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall\*'; arch = 'x86' })
        $list = New-Object System.Collections.Generic.List[object]
        foreach ($h in $hives) {
            Get-ItemProperty $h.path -ErrorAction SilentlyContinue |
                Where-Object { $_.DisplayName -and $_.SystemComponent -ne 1 -and -not $_.ParentKeyName -and $_.ReleaseType -notmatch 'Update|Hotfix' } |
                ForEach-Object {
                    $list.Add([ordered]@{ name = ([string]$_.DisplayName).Trim(); version = ([string]$_.DisplayVersion).Trim()
                            publisher = ([string]$_.Publisher).Trim(); installDate = [string]$_.InstallDate; arch = $h.arch })
                }
        }
        $out.software = $list
    }
}

Section 'hotfixes' {
    $inv = [Globalization.CultureInfo]::InvariantCulture
    $out.hotfixes = @(Get-CimInstance Win32_QuickFixEngineering | ForEach-Object {
            $raw = ([string]$_.InstalledOn).Trim()
            $d = [datetime]::MinValue
            $when = $null
            if ($raw -and [datetime]::TryParse($raw, $inv, [Globalization.DateTimeStyles]::AssumeLocal, [ref]$d)) { $when = Iso $d }
            elseif ($raw -match '^[0-9a-fA-F]{15,16}$') { $when = Iso ([datetime]::FromFileTimeUtc([Convert]::ToInt64($raw, 16))) }
            [ordered]@{ id = [string]$_.HotFixID; description = [string]$_.Description; installedOn = $when }
        })
}

Section 'updates' {
    $u = [ordered]@{ rebootRequired = [bool](Test-Path 'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\WindowsUpdate\Auto Update\RebootRequired') }
    try {
        $au = New-Object -ComObject Microsoft.Update.AutoUpdate
        $u.lastSearch = Iso $au.Results.LastSearchSuccessDate
        $u.lastInstall = Iso $au.Results.LastInstallationSuccessDate
    } catch { $out.errors['updates.history'] = $_.Exception.Message }
    # updates Windows Update already knows about (offline search: no download, no network);
    # in its own runspace so a busy update service cannot stall the inventory
    $ps = [powershell]::Create().AddScript({
            $searcher = (New-Object -ComObject Microsoft.Update.Session).CreateUpdateSearcher()
            $searcher.Online = $false
            $result = $searcher.Search("IsInstalled=0 and IsHidden=0 and Type='Software'")
            foreach ($x in $result.Updates) {
                [ordered]@{ title = [string]$x.Title; kb = @($x.KBArticleIDs | ForEach-Object { [string]$_ })
                    severity = [string]$x.MsrcSeverity; categories = @($x.Categories | ForEach-Object { [string]$_.Name })
                    downloaded = [bool]$x.IsDownloaded }
            }
        })
    try {
        $h = $ps.BeginInvoke()
        if ($h.AsyncWaitHandle.WaitOne($timeoutSec * 1000)) {
            $u.pending = @($ps.EndInvoke($h) | ForEach-Object { $_.BaseObject })
            if ($ps.Streams.Error.Count -gt 0) { $out.errors['updates.pending'] = [string]$ps.Streams.Error[0] }
        } else {
            $out.errors['updates.pending'] = 'timeout'
        }
    } catch { $out.errors['updates.pending'] = $_.Exception.Message }
    $out.updates = $u
}

Section 'services' {
    $out.services = @(Get-CimInstance Win32_Service | ForEach-Object {
            [ordered]@{ name = [string]$_.Name; displayName = [string]$_.DisplayName; state = [string]$_.State
                startMode = [string]$_.StartMode; account = [string]$_.StartName }
        })
}

Section 'listening' {
    if (-not (Has 'Get-NetTCPConnection')) { $out.unavailable.Add('listening'); return }
    $procs = @{}
    Get-Process | ForEach-Object { $procs[[int]$_.Id] = [string]$_.ProcessName }
    $list = New-Object System.Collections.Generic.List[object]
    Get-NetTCPConnection -State Listen -ErrorAction SilentlyContinue | ForEach-Object {
        $list.Add([ordered]@{ proto = 'tcp'; address = [string]$_.LocalAddress; port = [int]$_.LocalPort
                pid = [int]$_.OwningProcess; process = $procs[[int]$_.OwningProcess] })
    }
    Get-NetUDPEndpoint -ErrorAction SilentlyContinue | ForEach-Object {
        $list.Add([ordered]@{ proto = 'udp'; address = [string]$_.LocalAddress; port = [int]$_.LocalPort
                pid = [int]$_.OwningProcess; process = $procs[[int]$_.OwningProcess] })
    }
    $out.listening = $list
}

Section 'firewall' {
    if (-not (Has 'Get-NetFirewallProfile')) { $out.unavailable.Add('firewall'); return }
    $out.firewall = @(Get-NetFirewallProfile | ForEach-Object { [ordered]@{ profile = [string]$_.Name; enabled = [string]$_.Enabled -eq 'True' } })
}

Section 'defender' {
    if (-not (Has 'Get-MpComputerStatus')) { $out.unavailable.Add('defender'); return }
    $m = Get-MpComputerStatus
    $out.defender = [ordered]@{
        antivirus          = [bool]$m.AntivirusEnabled
        realtime           = [bool]$m.RealTimeProtectionEnabled
        signatureUpdated   = Iso $m.AntivirusSignatureLastUpdated
        signatureVersion   = [string]$m.AntivirusSignatureVersion
        productVersion     = [string]$m.AMProductVersion
    }
}

Section 'antivirus' {
    # Security Center knows third-party products, but only on client editions
    $list = @(Get-CimInstance -Namespace root/SecurityCenter2 -ClassName AntivirusProduct -ErrorAction SilentlyContinue)
    if ($list.Count -eq 0) { $out.unavailable.Add('antivirus'); return }
    $out.antivirus = @($list | ForEach-Object { [ordered]@{ name = [string]$_.displayName; state = [int64]$_.productState } })
}

if (Get-Service -Name DHCPServer -ErrorAction SilentlyContinue) {
    Section 'dhcp' {
        if (-not (Has 'Get-DhcpServerv4Scope')) { throw 'DhcpServer PowerShell module missing (RSAT DHCP tools)' }
        $scopes = New-Object System.Collections.Generic.List[object]
        $leases = New-Object System.Collections.Generic.List[object]
        $reservations = New-Object System.Collections.Generic.List[object]
        foreach ($s in @(Get-DhcpServerv4Scope)) {
            $id = [string]$s.ScopeId
            $scopes.Add([ordered]@{ id = $id; mask = [string]$s.SubnetMask; name = [string]$s.Name; state = [string]$s.State
                    start = [string]$s.StartRange; end = [string]$s.EndRange })
            foreach ($l in @(Get-DhcpServerv4Lease -ScopeId $s.ScopeId)) {
                $leases.Add([ordered]@{ ip = [string]$l.IPAddress; mac = [string]$l.ClientId; hostname = [string]$l.HostName
                        state = [string]$l.AddressState; expires = Iso $l.LeaseExpiryTime; scope = $id; description = [string]$l.Description })
            }
            foreach ($r in @(Get-DhcpServerv4Reservation -ScopeId $s.ScopeId)) {
                $reservations.Add([ordered]@{ ip = [string]$r.IPAddress; mac = [string]$r.ClientId; name = [string]$r.Name
                        description = [string]$r.Description; scope = $id })
            }
        }
        $out.dhcp = [ordered]@{ scopes = $scopes; leases = $leases; reservations = $reservations }
    }
}

$out.collectedAt = (Get-Date).ToUniversalTime().ToString('o')
$out | ConvertTo-Json -Depth 6 -Compress
