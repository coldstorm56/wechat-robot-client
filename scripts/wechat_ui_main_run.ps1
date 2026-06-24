param(
    [string]$GoExe = '',
    [int]$MainPort = 9001,
    [string]$BridgeAddr = '127.0.0.1:3021',
    [string]$OpenClawBaseUrl = 'http://127.0.0.1:18790/api/assistant/chat',
    [int]$OpenClawTimeout = 120,
    [string]$BotName = '',
    [string]$TriggerPrefix = '',
    [string]$TriggerMode = 'at_or_prefix',
    [int]$ContextWindow = 10,
    [int]$MaxReplyLength = 1200,
    [switch]$DisableOpenClaw,
    [switch]$AllowNonLoopbackOpenClaw,
    [switch]$NoStart
)

$ErrorActionPreference = 'Stop'

try {
    [Console]::OutputEncoding = [System.Text.Encoding]::UTF8
    $OutputEncoding = [System.Text.Encoding]::UTF8
} catch {
}

function New-AssistantText {
    return -join @([char]0x52a9, [char]0x624b)
}

function New-AssistantPrefix {
    return (New-AssistantText) + [char]0xff1a
}

function Resolve-GoExe {
    param([string]$Configured)

    if (-not [string]::IsNullOrWhiteSpace($Configured)) {
        return $Configured
    }
    $localGo = 'C:\Users\28029\.cache\codex-go\go1.26.4-tar\go\bin\go.exe'
    if (Test-Path -LiteralPath $localGo) {
        return $localGo
    }
    return 'go'
}

function Assert-LoopbackHostPort {
    param(
        [string]$Value,
        [string]$Name
    )

    $parts = $Value.Split(':')
    if ($parts.Count -ne 2 -or [string]::IsNullOrWhiteSpace($parts[0])) {
        throw "$Name must be host:port, got '$Value'"
    }
    $hostName = $parts[0].Trim()
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

function Assert-LoopbackUrl {
    param(
        [string]$Value,
        [string]$Name
    )

    $uri = [System.Uri]$Value
    if ($uri.Scheme -ne 'http' -and $uri.Scheme -ne 'https') {
        throw "$Name must use http or https, got '$Value'"
    }
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

function Set-EnvValue {
    param(
        [string]$Name,
        [string]$Value
    )

    Set-Item -Path "Env:$Name" -Value $Value
}

if ([string]::IsNullOrWhiteSpace($BotName)) {
    $BotName = New-AssistantText
}
if ([string]::IsNullOrWhiteSpace($TriggerPrefix)) {
    $TriggerPrefix = New-AssistantPrefix
}

Assert-LoopbackHostPort -Value $BridgeAddr -Name 'BridgeAddr'
if (-not $DisableOpenClaw -and -not $AllowNonLoopbackOpenClaw) {
    Assert-LoopbackUrl -Value $OpenClawBaseUrl -Name 'OpenClawBaseUrl'
}

$go = Resolve-GoExe $GoExe
$openClawEnabled = -not $DisableOpenClaw

Set-EnvValue 'GO_ENV' 'dev'
Set-EnvValue 'WECHAT_CLIENT_PORT' "$MainPort"
Set-EnvValue 'WECHAT_SERVER_HOST' $BridgeAddr
Set-EnvValue 'OPENCLAW_ENABLED' ($(if ($openClawEnabled) { 'true' } else { 'false' }))
Set-EnvValue 'OPENCLAW_BASE_URL' ($(if ($openClawEnabled) { $OpenClawBaseUrl } else { '' }))
Set-EnvValue 'OPENCLAW_TIMEOUT' "$OpenClawTimeout"
Set-EnvValue 'BOT_NAME' $BotName
Set-EnvValue 'TRIGGER_MODE' $TriggerMode
Set-EnvValue 'TRIGGER_PREFIX' $TriggerPrefix
Set-EnvValue 'ENABLE_CONTEXT' 'true'
Set-EnvValue 'CONTEXT_WINDOW' "$ContextWindow"
Set-EnvValue 'MAX_REPLY_LENGTH' "$MaxReplyLength"

$summary = [ordered]@{
    command = "$go run ."
    main_port = $MainPort
    go_env = 'dev'
    wechat_server_host = $BridgeAddr
    openclaw_enabled = $openClawEnabled
    openclaw_base_url = $(if ($openClawEnabled) { $OpenClawBaseUrl } else { '' })
    bot_name = $BotName
    trigger_mode = $TriggerMode
    trigger_prefix = $TriggerPrefix
    context_window = $ContextWindow
    max_reply_length = $MaxReplyLength
}

$summary | ConvertTo-Json -Depth 8

if ($NoStart) {
    return
}

& $go run .
exit $LASTEXITCODE
