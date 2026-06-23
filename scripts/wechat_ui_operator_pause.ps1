param(
    [ValidateSet('status', 'set', 'clear')]
    [string]$Action = 'status',

    [string]$BridgeUrl = 'http://127.0.0.1:3021',

    [int]$Minutes = 0,

    [string]$Until = '',

    [string]$Windows = '',

    [string]$Reason = 'operator takeover',

    [string]$PauseFile = '',

    [switch]$LocalFile
)

$ErrorActionPreference = 'Stop'

function Get-PauseFilePath {
    param([string]$InputPath)

    if (-not [string]::IsNullOrWhiteSpace($InputPath)) {
        return $InputPath
    }
    if (-not [string]::IsNullOrWhiteSpace($env:WECHAT_UI_OPERATOR_PAUSE_FILE)) {
        return $env:WECHAT_UI_OPERATOR_PAUSE_FILE
    }
    return Join-Path $env:TEMP 'wechat-ui-operator-pause.json'
}

function New-PausePayload {
    $payload = [ordered]@{
        reason = $Reason
    }
    if ($Minutes -gt 0) {
        $payload['minutes'] = $Minutes
    }
    if (-not [string]::IsNullOrWhiteSpace($Until)) {
        $payload['pause_until'] = $Until
    }
    if (-not [string]::IsNullOrWhiteSpace($Windows)) {
        $payload['windows'] = $Windows
    }
    if ($Minutes -le 0 -and [string]::IsNullOrWhiteSpace($Until) -and [string]::IsNullOrWhiteSpace($Windows)) {
        $payload['paused'] = $true
    }
    return $payload
}

function Write-JsonFile {
    param(
        [string]$Path,
        [object]$Value
    )

    $parent = Split-Path -Parent $Path
    if (-not [string]::IsNullOrWhiteSpace($parent)) {
        New-Item -ItemType Directory -Force -Path $parent | Out-Null
    }
    $json = $Value | ConvertTo-Json -Depth 8
    Set-Content -LiteralPath $Path -Value $json -Encoding UTF8
    return $Value
}

function Read-JsonFile {
    param([string]$Path)

    $content = Get-Content -LiteralPath $Path -Raw
    try {
        return $content | ConvertFrom-Json
    } catch {
        return [ordered]@{
            parse_error = $_.Exception.Message
        }
    }
}

function Invoke-BridgeJson {
    param(
        [string]$Path,
        [object]$Body = $null
    )

    $uri = ($BridgeUrl.TrimEnd('/')) + $Path
    if ($null -eq $Body) {
        return Invoke-RestMethod -Method Post -Uri $uri
    }
    $json = $Body | ConvertTo-Json -Depth 8
    return Invoke-RestMethod -Method Post -Uri $uri -ContentType 'application/json' -Body $json
}

if ($LocalFile) {
    $path = Get-PauseFilePath $PauseFile
    switch ($Action) {
        'status' {
            if (Test-Path -LiteralPath $path) {
                [ordered]@{
                    mode = 'local_file'
                    pause_file = $path
                    state = Read-JsonFile -Path $path
                } | ConvertTo-Json -Depth 8
            } else {
                [ordered]@{
                    mode = 'local_file'
                    pause_file = $path
                    paused = $false
                    reason = 'pause file does not exist'
                } | ConvertTo-Json -Depth 8
            }
        }
        'set' {
            $payload = New-PausePayload
            $state = Write-JsonFile -Path $path -Value $payload
            [ordered]@{
                mode = 'local_file'
                pause_file = $path
                state = $state
            } | ConvertTo-Json -Depth 8
        }
        'clear' {
            Remove-Item -LiteralPath $path -ErrorAction SilentlyContinue
            [ordered]@{
                mode = 'local_file'
                pause_file = $path
                paused = $false
                cleared = $true
            } | ConvertTo-Json -Depth 8
        }
    }
    return
}

switch ($Action) {
    'status' {
        Invoke-BridgeJson -Path '/api/Operator/PauseStatus' | ConvertTo-Json -Depth 8
    }
    'set' {
        Invoke-BridgeJson -Path '/api/Operator/PauseSet' -Body (New-PausePayload) | ConvertTo-Json -Depth 8
    }
    'clear' {
        Invoke-BridgeJson -Path '/api/Operator/PauseClear' | ConvertTo-Json -Depth 8
    }
}
