param(
    [string]$Python = 'python',
    [string]$GoExe = '',
    [string]$MainCommand = '',
    [int]$MainPort = 9002,
    [int]$WechatPort = 3022,
    [int]$OpenClawPort = 18791,
    [string]$WechatId = 'wechat_ui_bot',
    [string]$ExpectTitle = '',
    [switch]$RunMainE2E,
    [switch]$SkipRealUiStatus,
    [switch]$RequireRealUiStatusUsable
)

$ErrorActionPreference = 'Stop'

try {
    [Console]::OutputEncoding = [System.Text.Encoding]::UTF8
    $OutputEncoding = [System.Text.Encoding]::UTF8
} catch {
}
$env:PYTHONIOENCODING = 'utf-8'

if ([string]::IsNullOrWhiteSpace($ExpectTitle)) {
    $ExpectTitle = [string]::Concat(
        [char]0x6587,
        [char]0x4ef6,
        [char]0x4f20,
        [char]0x8f93,
        [char]0x52a9,
        [char]0x624b
    )
}
$triggerPrefix = [string]::Concat([char]0x52a9, [char]0x624b, [char]0xff1a)

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

function Resolve-MainCommand {
    param(
        [string]$Configured,
        [string]$ResolvedGo
    )

    if (-not [string]::IsNullOrWhiteSpace($Configured)) {
        return $Configured
    }
    return '"' + $ResolvedGo + '" run .'
}

function Invoke-AcceptanceStep {
    param(
        [string]$Name,
        [scriptblock]$Script
    )

    Write-Host "==> $Name"
    $start = Get-Date
    & $Script
    $elapsed = [int]((Get-Date) - $start).TotalSeconds
    Write-Host "<== $Name (${elapsed}s)"
}

function Invoke-CommandStep {
    param(
        [string]$Name,
        [string[]]$Command
    )

    Invoke-AcceptanceStep $Name {
        & $Command[0] @($Command[1..($Command.Length - 1)])
        if ($LASTEXITCODE -ne 0) {
            throw "$Name failed with exit code $LASTEXITCODE"
        }
    }
}

$go = Resolve-GoExe $GoExe
$main = Resolve-MainCommand $MainCommand $go

Write-Host "WeChat UI acceptance"
Write-Host "Python: $Python"
Write-Host "Go: $go"
Write-Host "Main command: $main"
Write-Host "Run main E2E: $RunMainE2E"

Invoke-CommandStep 'python compile smoke and harness' @(
    $Python,
    '-m',
    'py_compile',
    'scripts\wechat_ui_smoke.py',
    'scripts\assistant_flow_e2e.py'
)

Invoke-CommandStep 'wechat-ui-bridge tests' @(
    $go,
    'test',
    './cmd/wechat-ui-bridge',
    '-count=1'
)

Invoke-CommandStep 'assistant-flow harness self-test' @(
    $Python,
    'scripts\assistant_flow_e2e.py',
    '--self-test'
)

if (-not $SkipRealUiStatus) {
    Invoke-AcceptanceStep 'real WeChat UI status (read-only)' {
        $output = & $Python scripts\wechat_ui_smoke.py --min-window-width 640 --min-window-height 480 status --expect-title $ExpectTitle
        if ($LASTEXITCODE -ne 0) {
            throw "real WeChat UI status failed with exit code $LASTEXITCODE"
        }
        $output | Write-Host
        if ($RequireRealUiStatusUsable) {
            $parsed = $output | ConvertFrom-Json
            if (-not $parsed.status.window_size_ok) {
                throw 'real WeChat UI status is not usable: window_size_ok=false'
            }
            if ($parsed.status.blocking_windows -and $parsed.status.blocking_windows.Count -gt 0) {
                throw 'real WeChat UI status is not usable: blocking_windows present'
            }
            if ($false -eq $parsed.status.title_match) {
                throw 'real WeChat UI status is not usable: title_match=false'
            }
        }
    }
}

if ($RunMainE2E) {
    Invoke-CommandStep 'private assistant-flow E2E' @(
        $Python,
        'scripts\assistant_flow_e2e.py',
        '--prepare-local-db',
        '--start-main-command', $main,
        '--main-port', "$MainPort",
        '--wechat-port', "$WechatPort",
        '--openclaw-port', "$OpenClawPort",
        '--wechat-id', $WechatId,
        '--from-wxid', 'wxid_e2e_friend',
        '--content', 'assistant flow acceptance private smoke',
        '--reply', 'OpenClaw mock reply acceptance private smoke',
        '--wait-reply-seconds', '35',
        '--main-start-timeout', '120'
    )

    Invoke-CommandStep 'send-failure observability E2E' @(
        $Python,
        'scripts\assistant_flow_e2e.py',
        '--prepare-local-db',
        '--start-main-command', $main,
        '--main-port', "$MainPort",
        '--wechat-port', "$WechatPort",
        '--openclaw-port', "$OpenClawPort",
        '--wechat-id', $WechatId,
        '--from-wxid', 'wxid_e2e_friend',
        '--content', 'assistant flow acceptance send error smoke',
        '--reply', 'OpenClaw mock reply acceptance send error smoke',
        '--mock-wechat-send-error', 'blocking_window',
        '--expect-main-log-text', 'blocking_window',
        '--wait-reply-seconds', '35',
        '--main-start-timeout', '120'
    )

    Invoke-CommandStep 'group @bot assistant-flow E2E' @(
        $Python,
        'scripts\assistant_flow_e2e.py',
        '--prepare-local-db',
        '--start-main-command', $main,
        '--main-port', "$MainPort",
        '--wechat-port', "$WechatPort",
        '--openclaw-port', "$OpenClawPort",
        '--wechat-id', $WechatId,
        '--from-wxid', 'room_e2e@chatroom',
        '--to-wxid', $WechatId,
        '--sender-wxid', 'wxid_group_user',
        '--at-wxid', $WechatId,
        '--content', 'group at acceptance assistant flow smoke',
        '--reply', 'OpenClaw mock reply group at acceptance smoke',
        '--wait-reply-seconds', '35',
        '--main-start-timeout', '120'
    )

    Invoke-CommandStep 'group prefix assistant-flow E2E' @(
        $Python,
        'scripts\assistant_flow_e2e.py',
        '--prepare-local-db',
        '--start-main-command', $main,
        '--main-port', "$MainPort",
        '--wechat-port', "$WechatPort",
        '--openclaw-port', "$OpenClawPort",
        '--wechat-id', $WechatId,
        '--from-wxid', 'room_e2e@chatroom',
        '--to-wxid', $WechatId,
        '--sender-wxid', 'wxid_group_user',
        '--content', ($triggerPrefix + 'group prefix acceptance assistant flow smoke'),
        '--reply', 'OpenClaw mock reply group prefix acceptance smoke',
        '--wait-reply-seconds', '35',
        '--main-start-timeout', '120'
    )

    Invoke-CommandStep 'non-whitelisted group suppression E2E' @(
        $Python,
        'scripts\assistant_flow_e2e.py',
        '--prepare-local-db',
        '--no-enable-chat-room-ai',
        '--expect-no-send',
        '--start-main-command', $main,
        '--main-port', "$MainPort",
        '--wechat-port', "$WechatPort",
        '--openclaw-port', "$OpenClawPort",
        '--wechat-id', $WechatId,
        '--from-wxid', 'room_e2e@chatroom',
        '--to-wxid', $WechatId,
        '--sender-wxid', 'wxid_group_user',
        '--at-wxid', $WechatId,
        '--content', 'group non whitelist acceptance assistant flow smoke',
        '--reply', 'OpenClaw mock reply should not send',
        '--wait-reply-seconds', '6',
        '--main-start-timeout', '120'
    )
}

Write-Host 'Acceptance completed.'
