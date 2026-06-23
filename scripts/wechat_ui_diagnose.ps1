param(
    [string]$Python = 'python',
    [string]$ExpectTitle = '',
    [int]$MinWindowWidth = 640,
    [int]$MinWindowHeight = 480,
    [switch]$KeepFocus
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

$argsList = @(
    'scripts\wechat_ui_smoke.py',
    '--min-window-width', "$MinWindowWidth",
    '--min-window-height', "$MinWindowHeight"
)
if ($KeepFocus) {
    $argsList += '--keep-focus'
}
$argsList += @('status', '--expect-title', $ExpectTitle)

$rawLines = & $Python @argsList
$exitCode = $LASTEXITCODE
$raw = ($rawLines | Out-String).Trim()
if ($exitCode -ne 0) {
    [ordered]@{
        ok = $false
        ready_for_real_send = $false
        reason = 'status_command_failed'
        exit_code = $exitCode
        raw_output = $raw
        next_action = 'Fix the status command failure before attempting real WeChat UI send/read.'
    } | ConvertTo-Json -Depth 8
    exit $exitCode
}

$parsed = $raw | ConvertFrom-Json
$status = $parsed.status
$blockingCount = 0
if ($status.blocking_windows) {
    $blockingCount = @($status.blocking_windows).Count
}

$reasons = New-Object System.Collections.Generic.List[string]
if (-not $status.window_size_ok) {
    $reasons.Add('window_too_small')
}
if ($blockingCount -gt 0) {
    $reasons.Add('blocking_window')
}
if ($false -eq $status.title_match) {
    $reasons.Add('unexpected_chat_title_or_unreadable_title')
}
if ($false -eq $status.foreground) {
    $reasons.Add('not_foreground')
}
if ($reasons.Count -eq 0) {
    $reasons.Add('ready')
}

$nextAction = 'Ready for a controlled real WeChat send/read smoke.'
if ($blockingCount -gt 0) {
    $nextAction = 'Manually close or allow the blocking local dialog, then rerun this diagnostic. The script will not click security prompts.'
} elseif (-not $status.window_size_ok) {
    $nextAction = 'Resize the WeChat main chat window larger, then rerun this diagnostic.'
} elseif ($false -eq $status.title_match) {
    $nextAction = 'Open the expected WeChat conversation and make sure the chat title is visible, then rerun this diagnostic.'
} elseif ($false -eq $status.foreground) {
    $nextAction = 'Bring WeChat to the foreground or remove covering windows, then rerun this diagnostic.'
}

[ordered]@{
    ok = $true
    ready_for_real_send = ($reasons.Count -eq 1 -and $reasons[0] -eq 'ready')
    reason = ($reasons -join ',')
    expected_title = $ExpectTitle
    window = $status.window
    window_width = $status.window_width
    window_height = $status.window_height
    min_window_width = $status.min_window_width
    min_window_height = $status.min_window_height
    window_size_ok = $status.window_size_ok
    title_match = $status.title_match
    foreground = $status.foreground
    blocking_window_count = $blockingCount
    blocking_windows = $status.blocking_windows
    next_action = $nextAction
} | ConvertTo-Json -Depth 8
