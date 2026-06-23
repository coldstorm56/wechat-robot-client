param(
    [string]$BridgeUrl = 'http://127.0.0.1:3021',
    [string]$MainUrl = 'http://127.0.0.1:9001',
    [string]$OpenClawUrl = 'http://127.0.0.1:18790',
    [string]$Python = 'python',
    [string]$ExpectTitle = '',
    [int]$TimeoutSeconds = 3,
    [switch]$IncludeReadiness,
    [switch]$IncludeRealUi
)

$ErrorActionPreference = 'Stop'

try {
    [Console]::OutputEncoding = [System.Text.Encoding]::UTF8
    $OutputEncoding = [System.Text.Encoding]::UTF8
} catch {
}
$env:PYTHONIOENCODING = 'utf-8'

function New-FileTransferAssistantText {
    return -join @(
        [char]0x6587,
        [char]0x4ef6,
        [char]0x4f20,
        [char]0x8f93,
        [char]0x52a9,
        [char]0x624b
    )
}

function Assert-LoopbackUrl {
    param(
        [string]$Value,
        [string]$Name
    )

    $uri = [System.Uri]$Value
    $hostName = $uri.Host
    $ip = $null
    if ([System.Net.IPAddress]::TryParse($hostName, [ref]$ip)) {
        if (-not [System.Net.IPAddress]::IsLoopback($ip)) {
            throw "$Name must point to loopback, got '$Value'"
        }
        return
    }
    if ($hostName -ne 'localhost') {
        throw "$Name must point to loopback, got '$Value'"
    }
}

function Invoke-JsonHttp {
    param(
        [string]$Name,
        [ValidateSet('GET', 'POST')]
        [string]$Method,
        [string]$Url
    )

    try {
        $response = Invoke-WebRequest -Method $Method -Uri $Url -TimeoutSec $TimeoutSeconds -UseBasicParsing
        $parsed = $null
        try {
            $parsed = $response.Content | ConvertFrom-Json
        } catch {
            $parsed = $response.Content
        }
        return [ordered]@{
            ok = ($response.StatusCode -ge 200 -and $response.StatusCode -lt 300)
            status = [int]$response.StatusCode
            data = $parsed
        }
    } catch {
        $status = 0
        if ($_.Exception.Response -and $_.Exception.Response.StatusCode) {
            $status = [int]$_.Exception.Response.StatusCode
        }
        return [ordered]@{
            ok = $false
            status = $status
            error = $_.Exception.Message
        }
    }
}

function Invoke-JsonProcess {
    param([string[]]$Command)

    try {
        $rawLines = & $Command[0] @($Command[1..($Command.Length - 1)])
        $exitCode = $LASTEXITCODE
        $raw = ($rawLines | Out-String).Trim()
        $parsed = $null
        if (-not [string]::IsNullOrWhiteSpace($raw)) {
            $parsed = $raw | ConvertFrom-Json
        }
        return [ordered]@{
            ok = ($exitCode -eq 0)
            exit_code = $exitCode
            data = $parsed
            raw = $raw
        }
    } catch {
        return [ordered]@{
            ok = $false
            exit_code = -1
            error = $_.Exception.Message
        }
    }
}

Assert-LoopbackUrl -Value $BridgeUrl -Name 'BridgeUrl'
Assert-LoopbackUrl -Value $MainUrl -Name 'MainUrl'
Assert-LoopbackUrl -Value $OpenClawUrl -Name 'OpenClawUrl'

if ([string]::IsNullOrWhiteSpace($ExpectTitle)) {
    $ExpectTitle = New-FileTransferAssistantText
}

$bridge = [ordered]@{
    health = Invoke-JsonHttp -Name 'bridge health' -Method GET -Url (($BridgeUrl.TrimEnd('/')) + '/health')
    pause_status = Invoke-JsonHttp -Name 'bridge pause' -Method POST -Url (($BridgeUrl.TrimEnd('/')) + '/api/Operator/PauseStatus')
    poll_status = Invoke-JsonHttp -Name 'bridge poll' -Method POST -Url (($BridgeUrl.TrimEnd('/')) + '/api/Operator/PollStatus')
}
if ($IncludeReadiness) {
    $bridge['readiness'] = Invoke-JsonHttp -Name 'bridge readiness' -Method POST -Url (($BridgeUrl.TrimEnd('/')) + '/api/Operator/Readiness')
}

$main = [ordered]@{
    is_running = Invoke-JsonHttp -Name 'main is-running' -Method GET -Url (($MainUrl.TrimEnd('/')) + '/api/v1/robot/is-running')
    is_loggedin = Invoke-JsonHttp -Name 'main is-loggedin' -Method GET -Url (($MainUrl.TrimEnd('/')) + '/api/v1/robot/is-loggedin')
}

$openClaw = [ordered]@{
    health = Invoke-JsonHttp -Name 'openclaw health' -Method GET -Url (($OpenClawUrl.TrimEnd('/')) + '/health')
}

$realUi = $null
if ($IncludeRealUi) {
    $realUi = Invoke-JsonProcess @(
        'powershell',
        '-NoProfile',
        '-ExecutionPolicy',
        'Bypass',
        '-File',
        '.\scripts\wechat_ui_diagnose.ps1',
        '-Python',
        $Python,
        '-ExpectTitle',
        $ExpectTitle
    )
}

$readyForBridgeRuntime = (
    $bridge.health.ok -and
    $bridge.pause_status.ok -and
    $bridge.poll_status.ok -and
    $main.is_running.ok -and
    $openClaw.health.ok
)

$readyForRealSend = $null
if ($IncludeRealUi -and $realUi.data) {
    $readyForRealSend = [bool]$realUi.data.ready_for_real_send
}

[ordered]@{
    ok = $true
    ready_for_bridge_runtime = [bool]$readyForBridgeRuntime
    ready_for_real_send = $readyForRealSend
    bridge_url = $BridgeUrl
    main_url = $MainUrl
    openclaw_url = $OpenClawUrl
    bridge = $bridge
    main = $main
    openclaw = $openClaw
    real_ui = $realUi
} | ConvertTo-Json -Depth 14
