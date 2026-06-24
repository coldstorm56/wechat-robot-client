param(
    [string]$GoExe = '',
    [string]$Python = 'python',
    [string]$BridgeAddr = '127.0.0.1:3021',
    [string]$WechatId = 'wechat_ui_bot',
    [string]$BotName = '',
    [string]$ExpectedChatTitle = '',
    [string]$FileHelperAlias = '',
    [string]$PauseWindows = '',
    [string]$PauseFile = '',
    [int]$MainServicePort = 9001,
    [int]$MinWindowWidth = 640,
    [int]$MinWindowHeight = 480,
    [int]$InjectDedupeTtlSeconds = 120,
    [int]$OutgoingEchoTtlSeconds = 300,
    [int]$PollMaxErrors = 3,
    [switch]$DryRun,
    [switch]$AllowUnverifiedSend,
    [switch]$NavigateToContact,
    [switch]$NoStart
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

function New-DefaultBotName {
    return -join @(
        [char]0x6b64,
        [char]0x523b,
        [char]0x6b63,
        [char]0x4f73
    )
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

function Set-EnvValue {
    param(
        [string]$Name,
        [string]$Value
    )

    Set-Item -Path "Env:$Name" -Value $Value
}

function ConvertTo-EnvBool {
    param([bool]$Value)

    if ($Value) {
        return 'true'
    }
    return 'false'
}

if ([string]::IsNullOrWhiteSpace($FileHelperAlias)) {
    $FileHelperAlias = New-FileTransferAssistantText
}
if ([string]::IsNullOrWhiteSpace($ExpectedChatTitle)) {
    $ExpectedChatTitle = $FileHelperAlias
}
if ([string]::IsNullOrWhiteSpace($BotName)) {
    $BotName = New-DefaultBotName
}
if ([string]::IsNullOrWhiteSpace($PauseFile)) {
    $PauseFile = Join-Path $env:TEMP 'wechat-ui-operator-pause.json'
}

$assistantSyncUrl = "http://127.0.0.1:$MainServicePort/api/v1/wechat-client/$WechatId/sync-message"
$go = Resolve-GoExe $GoExe

Set-EnvValue 'WECHAT_UI_BRIDGE_ADDR' $BridgeAddr
Set-EnvValue 'WECHAT_UI_PYTHON' $Python
Set-EnvValue 'WECHAT_UI_BOT_WXID' $WechatId
Set-EnvValue 'WECHAT_UI_BOT_NAME' $BotName
Set-EnvValue 'WECHAT_UI_CONTACT_ALIASES' "filehelper=$FileHelperAlias"
Set-EnvValue 'WECHAT_UI_SEND_CURRENT_CHAT' (ConvertTo-EnvBool (-not $NavigateToContact))
Set-EnvValue 'WECHAT_UI_SEND_REQUIRE_VERIFY' (ConvertTo-EnvBool (-not $AllowUnverifiedSend))
Set-EnvValue 'WECHAT_UI_EXPECT_CHAT_TITLE' $ExpectedChatTitle
Set-EnvValue 'WECHAT_UI_MIN_WINDOW_WIDTH' "$MinWindowWidth"
Set-EnvValue 'WECHAT_UI_MIN_WINDOW_HEIGHT' "$MinWindowHeight"
Set-EnvValue 'WECHAT_UI_OPERATOR_PAUSE_WINDOWS' $PauseWindows
Set-EnvValue 'WECHAT_UI_OPERATOR_PAUSE_FILE' $PauseFile
Set-EnvValue 'WECHAT_UI_ASSISTANT_SYNC_URL' $assistantSyncUrl
Set-EnvValue 'WECHAT_UI_INJECT_DEDUPE_TTL_SECONDS' "$InjectDedupeTtlSeconds"
Set-EnvValue 'WECHAT_UI_OUTGOING_ECHO_TTL_SECONDS' "$OutgoingEchoTtlSeconds"
Set-EnvValue 'WECHAT_UI_POLL_MAX_ERRORS' "$PollMaxErrors"
Set-EnvValue 'WECHAT_UI_DRY_RUN' (ConvertTo-EnvBool $DryRun)

$summary = [ordered]@{
    bridge_addr = $BridgeAddr
    main_service_env = "WECHAT_SERVER_HOST=$BridgeAddr"
    assistant_sync_url = $assistantSyncUrl
    wechat_id = $WechatId
    bot_name = $BotName
    expected_chat_title = $ExpectedChatTitle
    filehelper_alias = $FileHelperAlias
    send_current_chat = (-not $NavigateToContact)
    require_verify = (-not $AllowUnverifiedSend)
    min_window_width = $MinWindowWidth
    min_window_height = $MinWindowHeight
    operator_pause_windows = $PauseWindows
    operator_pause_file = $PauseFile
    dry_run = [bool]$DryRun
    command = "$go run ./cmd/wechat-ui-bridge"
}

$summary | ConvertTo-Json -Depth 8

if ($NoStart) {
    return
}

& $go run ./cmd/wechat-ui-bridge
exit $LASTEXITCODE
