# OpenClaw runtime joint debug

This note is for local end-to-end validation of the OpenClaw assistant MVP.

## 1. Start the OpenClaw HTTP bridge

The bridge exposes the HTTP contract expected by `OPENCLAW_BASE_URL` and calls the local OpenClaw CLI through WSL on Windows.

```powershell
$env:PATH='C:\Users\28029\.cache\codex-go\go1.26.4-tar\go\bin;'+$env:PATH
go run ./cmd/openclaw-bridge
```

Default endpoint:

```text
http://127.0.0.1:18790/api/assistant/chat
```

Optional settings:

```powershell
$env:OPENCLAW_BRIDGE_ADDR='127.0.0.1:18790'
$env:OPENCLAW_BRIDGE_PATH='/api/assistant/chat'
$env:OPENCLAW_BRIDGE_AGENT='main'
$env:OPENCLAW_BRIDGE_SESSION_PREFIX='wechat'
$env:OPENCLAW_BRIDGE_EXEC_MODE='wsl'
$env:OPENCLAW_BRIDGE_TIMEOUT='120'
```

Smoke test:

```powershell
Invoke-RestMethod -Method Post `
  -Uri 'http://127.0.0.1:18790/api/assistant/chat' `
  -ContentType 'application/json' `
  -Body '{"channel":"wechat_personal","session_id":"friend:test","message":"Reply with pong only."}'
```

## 2. Prepare local service dependencies

The current local compose file does not expose MySQL and Redis to a Windows-hosted `go run`.
Use the override file when running the client on the host:

```powershell
cd .deploy/local
docker compose -f docker-compose.yml -f docker-compose.openclaw-runtime.yml up -d wechat-admin-mysql wechat-admin-redis wechat-admin-qdrant wechat-server wechat-ipad
```

Expected host ports:

```text
MySQL: 127.0.0.1:3306
Redis: 127.0.0.1:6379
Qdrant gRPC: 127.0.0.1:6334
WeChat auth/admin service: 127.0.0.1:8090
WeChat iPad protocol service: 127.0.0.1:3010
OpenClaw bridge: 127.0.0.1:18790
wechat-robot-client: 127.0.0.1:9001
```

For real personal WeChat login, start the iPad protocol service too:

```powershell
docker compose -f docker-compose.yml -f docker-compose.openclaw-runtime.yml up -d wechat-ipad
```

In the host `.env`, `WECHAT_SERVER_HOST` must point to the iPad protocol service:

```text
WECHAT_SERVER_HOST=127.0.0.1:3010
```

If Docker is not installed or not in PATH, install or start Docker Desktop first, then reopen PowerShell and confirm:

```powershell
docker version
docker compose version
```

## 3. Prepare the robot database

`startup/config.go` uses `ROBOT_CODE` as the robot instance database name. Before `go run .`, confirm that database exists.

For the draft `.env` value `ROBOT_CODE=openclaw_assistant_dev`, create it once:

```powershell
docker exec wechat-admin-mysql mysql -uroot -pmroot12345678 -e "CREATE DATABASE IF NOT EXISTS openclaw_assistant_dev CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;"
```

## 4. Start wechat-robot-client

```powershell
$env:PATH='C:\Users\28029\.cache\codex-go\go1.26.4-tar\go\bin;'+$env:PATH
$env:GO_ENV='dev'
go run .
```

## 5. Acceptance checklist

- Private chat AI is enabled, then a direct message receives an OpenClaw reply.
- Whitelisted group has `ChatAIEnabled=true`.
- Group `@bot` receives an OpenClaw reply.
- Group `助手：ping` receives an OpenClaw reply.
- A non-whitelisted group does not trigger a reply.
- `assistant_session_logs` contains `success` rows for successful calls and useful `failed` rows for failures.

## 6. Current personal WeChat protocol blocker

On 2026-06-23, the local Docker and service chain was validated up to QR generation:

- `wechat-ipad:latest` was up to date and reachable on `127.0.0.1:3010`.
- `wechat-robot-client` was reachable on `127.0.0.1:9001`.
- `/api/v1/robot/login` returned QR code URL/base64 successfully.
- The protocol endpoints `LoginGetQRMac`, `LoginGetQR`, `LoginGetQRx`, `LoginGetQRPadx`, `LoginGetQRWinUnified`, and `LoginGetQRWinUwp` all produced scanable QR codes.

Real scan-login did not complete because WeChat rejected the protocol login during `LoginCheckQR`:

```text
Mac/iPad/QRx/WinUnified/WinUwp: version too low
Padx: scan status interaction key missing
```

The main service remained healthy (`is-running=true`) but not logged in (`is-loggedin=false`). Continue true private/group chat validation only after replacing `WECHAT_SERVER_HOST` with a currently working and trusted personal WeChat protocol endpoint, or after the `wechat-ipad` image is updated to a login-compatible version.

## 7. New WeChat 4.x UI automation bridge option

On 2026-06-23, WCFerry/go_wcf_http was evaluated as a local bridge option, but that path is not the current main line: the official v39.5.2 runtime depends on classic WeChat 3.9.12.51, and the local classic WeChat login was blocked by a WeChat "version too low" prompt.

The next local-only route is:

```text
Windows WeChat 4.x at D:\software\Weixin
  -> local UI Automation smoke/bridge
  -> wechat-robot-client existing assistant flow
  -> OpenClaw bridge
```

This route must keep the login state on this machine. Do not replace it with an untrusted public endpoint, and do not pull an unknown closed protocol image only because it advertises current login support.

Local UI automation preflight:

```powershell
python .\scripts\wechat_ui_smoke.py inspect
```

If WeChat is not already running, explicitly allow the script to start the local 4.x client:

```powershell
python .\scripts\wechat_ui_smoke.py --launch inspect
```

Send one explicit smoke message to the currently open conversation. For the File Transfer Assistant smoke, open File Transfer Assistant in WeChat 4.x first. The command returns `ok=true` only after the submitted text is visible again in the WeChat UI; if visible delivery cannot be verified, treat the send as unproven even if the keyboard sequence ran.

```powershell
python .\scripts\wechat_ui_smoke.py send `
  --message 'wechat-ui-automation smoke test'
```

Best-effort send without visible verification is available only for local debugging and must not be used as acceptance evidence:

```powershell
python .\scripts\wechat_ui_smoke.py send `
  --message 'wechat-ui-automation smoke test' `
  --no-verify
```

Read the last visible text from the current conversation, or from a named conversation after opening it:

```powershell
python .\scripts\wechat_ui_smoke.py read-last
python .\scripts\wechat_ui_smoke.py read-last --contact '文件传输助手'
```

Safety notes:

- No background polling is enabled by the smoke script.
- It only focuses WeChat during explicit `send` or `read-last` commands.
- It restores the previous clipboard text and foreground window by default.
- Failures are logged to `%TEMP%\wechat-ui-automation-bridge.log` unless `WECHAT_UI_LOG_PATH` overrides it.
- For daily-use safety, send actions should remain explicit until the bridge has a tested trigger, whitelist, and pause control.

Start the local compatibility bridge:

```powershell
$env:WECHAT_UI_BRIDGE_ADDR='127.0.0.1:3021'
$env:WECHAT_UI_BOT_WXID='wechat_ui_bot'
$env:WECHAT_UI_BOT_NAME='此刻正佳'
$env:WECHAT_UI_CONTACT_ALIASES='filehelper=文件传输助手'
$env:WECHAT_UI_SEND_CURRENT_CHAT='true'
$env:WECHAT_UI_SEND_REQUIRE_VERIFY='true'
go run ./cmd/wechat-ui-bridge
```

The bridge exposes a minimal old-protocol-compatible subset:

```text
GET  /health
POST /api/Login/GetCacheInfo
POST /api/User/GetContractProfile
POST /api/Friend/GetContractDetail
POST /api/Msg/SendTxt
POST /api/Msg/CurrentLastText
```

Dry-run the bridge without touching the WeChat UI:

```powershell
$env:WECHAT_UI_DRY_RUN='true'
go run ./cmd/wechat-ui-bridge
```

If an operator intentionally wants best-effort UI submission without visible delivery verification, set `WECHAT_UI_SEND_REQUIRE_VERIFY=false`; do not use that mode for acceptance.

Bridge smoke checks:

```powershell
Invoke-RestMethod http://127.0.0.1:3021/health
Invoke-RestMethod -Method Post http://127.0.0.1:3021/api/Login/GetCacheInfo
Invoke-RestMethod -Method Post `
  -Uri http://127.0.0.1:3021/api/Msg/SendTxt `
  -ContentType 'application/json' `
  -Body '{"Wxid":"wechat_ui_bot","ToWxid":"filehelper","Content":"wechat-ui bridge smoke"}'
```

Point the main service at this bridge only after the explicit send/read smoke checks pass:

```powershell
$env:WECHAT_SERVER_HOST='127.0.0.1:3021'
```

Current bridge boundary: this first bridge wraps explicit text submission to the currently open conversation and best-effort visible last-text reads. A keyboard submission is not accepted as real delivery unless the same text becomes visible in the WeChat UI. On the current Windows WeChat 4.x client, contact-search navigation can fall into WeChat "搜一搜" and is not enabled as the default bridge path. Visible message-body reads may still return time labels or fail when WeChat hides the Chromium message subtree behind the Qt/MMUI shell. Continuous incoming-message polling, callback forwarding into `/api/v1/wechat-client/:wxid/sync-message`, group `@` detection, prefix detection, and whitelist-driven automatic replies still need a later stage after real UI smoke validation.
