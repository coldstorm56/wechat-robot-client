param(
    [string]$Python = 'python',
    [string]$ExpectTitle = '',
    [string]$Message = '',
    [int]$MinWindowWidth = 640,
    [int]$MinWindowHeight = 480,
    [int]$VerifyTimeoutSeconds = 12,
    [int]$EditorVerifyTimeoutSeconds = 5,
    [switch]$KeepFocus,
    [switch]$SkipReadiness
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

function New-NaturalSmokeMessage {
    return 'memo sync ' + (Get-Date -Format 'MMdd HHmm')
}

function Invoke-JsonCommand {
    param([string[]]$Command)

    $rawLines = & $Command[0] @($Command[1..($Command.Length - 1)])
    $exitCode = $LASTEXITCODE
    $raw = ($rawLines | Out-String).Trim()
    if ([string]::IsNullOrWhiteSpace($raw)) {
        return [ordered]@{
            exit_code = $exitCode
            json = $null
            raw = $raw
        }
    }
    return [ordered]@{
        exit_code = $exitCode
        json = ($raw | ConvertFrom-Json)
        raw = $raw
    }
}

if ([string]::IsNullOrWhiteSpace($ExpectTitle)) {
    $ExpectTitle = New-FileTransferAssistantText
}
if ([string]::IsNullOrWhiteSpace($Message)) {
    $Message = New-NaturalSmokeMessage
}

$diagnoseCommand = @(
    'powershell',
    '-NoProfile',
    '-ExecutionPolicy',
    'Bypass',
    '-File',
    '.\scripts\wechat_ui_diagnose.ps1',
    '-Python',
    $Python,
    '-ExpectTitle',
    $ExpectTitle,
    '-MinWindowWidth',
    "$MinWindowWidth",
    '-MinWindowHeight',
    "$MinWindowHeight"
)
if ($KeepFocus) {
    $diagnoseCommand += '-KeepFocus'
}

$diagnosis = Invoke-JsonCommand $diagnoseCommand
if ($diagnosis.exit_code -ne 0) {
    [ordered]@{
        ok = $false
        phase = 'diagnose'
        sent = $false
        reason = 'diagnose_failed'
        diagnosis = $diagnosis.json
        raw_output = $diagnosis.raw
    } | ConvertTo-Json -Depth 12
    exit $diagnosis.exit_code
}

if (-not $SkipReadiness -and -not $diagnosis.json.ready_for_real_send) {
    [ordered]@{
        ok = $false
        phase = 'readiness'
        sent = $false
        reason = $diagnosis.json.reason
        next_action = $diagnosis.json.next_action
        diagnosis = $diagnosis.json
    } | ConvertTo-Json -Depth 12
    exit 3
}

$sendCommand = @(
    $Python,
    'scripts\wechat_ui_smoke.py',
    '--min-window-width',
    "$MinWindowWidth",
    '--min-window-height',
    "$MinWindowHeight"
)
if ($KeepFocus) {
    $sendCommand += '--keep-focus'
}
$sendCommand += @(
    'send',
    '--message',
    $Message,
    '--expect-title',
    $ExpectTitle,
    '--verify',
    '--verify-last',
    '--verify-editor',
    '--verify-timeout',
    "$VerifyTimeoutSeconds",
    '--editor-verify-timeout',
    "$EditorVerifyTimeoutSeconds"
)

$send = Invoke-JsonCommand $sendCommand
if ($send.exit_code -ne 0 -or -not $send.json.ok) {
    [ordered]@{
        ok = $false
        phase = 'send'
        delivery_proven = $false
        message = $Message
        diagnosis = $diagnosis.json
        send = $send.json
        raw_output = $send.raw
    } | ConvertTo-Json -Depth 12
    if ($send.exit_code -ne 0) {
        exit $send.exit_code
    }
    exit 1
}

$readCommand = @(
    $Python,
    'scripts\wechat_ui_smoke.py',
    '--min-window-width',
    "$MinWindowWidth",
    '--min-window-height',
    "$MinWindowHeight"
)
if ($KeepFocus) {
    $readCommand += '--keep-focus'
}
$readCommand += @(
    'read-last',
    '--expect-title',
    $ExpectTitle
)

$read = Invoke-JsonCommand $readCommand
$lastText = ''
if ($read.json -and $read.json.last_text) {
    $lastText = $read.json.last_text
}
$lastTextMatches = ($lastText -eq $Message)

[ordered]@{
    ok = ($send.json.ok -and $read.exit_code -eq 0 -and $read.json.ok -and $lastTextMatches)
    phase = 'complete'
    sent = $true
    message = $Message
    last_text_matches = $lastTextMatches
    diagnosis = $diagnosis.json
    send = $send.json
    read = $read.json
} | ConvertTo-Json -Depth 12

if (-not $lastTextMatches) {
    exit 4
}
exit 0
