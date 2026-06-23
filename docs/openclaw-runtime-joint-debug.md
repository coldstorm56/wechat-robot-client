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

Send one explicit smoke message to the currently open conversation. For the File Transfer Assistant smoke, open File Transfer Assistant in WeChat 4.x first. The command first requires local screenshot OCR to see the draft in the message editor before pressing Enter, then returns `ok=true` only after local screenshot OCR reads the submitted text from the visible WeChat chat area and confirms the current conversation's last readable message matches the submitted text. If the editor draft, visible delivery, or last-message check cannot be verified, treat the send as unproven.

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

OCR notes:

- The OCR verification is local-only and uses the visible WeChat window; it does not upload screenshots.
- Use distinct ASCII smoke text for verification because OCR can normalize or drop spaces.
- `read-last` returns the last OCR-readable visible text in the chat area, not a protocol message object.
- On 2026-06-23, with File Transfer Assistant already open, `Codex visible OCR smoke 2026-06-23` returned `delivery_status=visible_verified`, and `read-last` returned the same text.
- If a send attempt fails before Enter with `message draft did not reach the WeChat editor`, treat it as an input-focus failure. The current WeChat 4.x MMUI message editor does not expose a normal UIA edit control, so automated focus remains a known risk; the bridge must continue to fail closed instead of reporting success.
- If a send attempt reports `ok=true`, verify `last_text_status=last_visible_verified` and inspect `last_text`; this is stricter than merely seeing the submitted text somewhere in the chat area.
- Window size can be adjusted. A larger WeChat 4.x window is usually more stable for OCR and click targeting; avoid making it so narrow that the chat list, message area, or editor are compressed or hidden. Resizing is safe as long as the current chat title, message area, and editor remain visible. Before enabling polling, choose a comfortable stable size; the bridge does not require a fixed pixel size, but it will fail closed when the resized window no longer exposes enough readable UI.
- For current-chat mode, set an expected visible chat title such as `文件传输助手`; the bridge will fail before touching the editor when WeChat is on a different surface such as Service Accounts, Contacts, or search results.
- If `status` returns `blocking_windows`, a known system dialog or other blocking window is covering WeChat. Close or handle that dialog manually first; the bridge should not click through security prompts or report OCR/send success while the WeChat surface is blocked.

Safety notes:

- No background polling is enabled by the smoke script.
- It only focuses WeChat during explicit `send` or `read-last` commands.
- It restores the previous clipboard text and foreground window by default.
- Failures are logged to `%TEMP%\wechat-ui-automation-bridge.log` unless `WECHAT_UI_LOG_PATH` overrides it.
- The bridge supports operator takeover pauses. During a pause, send/read endpoints return `operator pause active` and do not touch the WeChat window. Use this for daily manual handoff windows or any temporary period where the operator wants full control of the desktop.
- For daily-use safety, send actions should remain explicit until the bridge has a tested trigger, whitelist, and pause control.

Start the local compatibility bridge:

```powershell
$env:WECHAT_UI_BRIDGE_ADDR='127.0.0.1:3021'
$env:WECHAT_UI_BOT_WXID='wechat_ui_bot'
$env:WECHAT_UI_BOT_NAME='此刻正佳'
$env:WECHAT_UI_CONTACT_ALIASES='filehelper=文件传输助手'
$env:WECHAT_UI_SEND_CURRENT_CHAT='true'
$env:WECHAT_UI_SEND_REQUIRE_VERIFY='true'
$env:WECHAT_UI_EXPECT_CHAT_TITLE='文件传输助手'
$env:WECHAT_UI_OPERATOR_PAUSE_WINDOWS='12:00-13:30,19:00-22:00'
$env:WECHAT_UI_OPERATOR_PAUSE_FILE="$env:TEMP\wechat-ui-operator-pause.json"
$env:WECHAT_UI_ASSISTANT_SYNC_URL='http://127.0.0.1:9001/api/v1/wechat-client/wechat_ui_bot/sync-message'
$env:WECHAT_UI_INJECT_DEDUPE_TTL_SECONDS='120'
$env:WECHAT_UI_OUTGOING_ECHO_TTL_SECONDS='300'
$env:WECHAT_UI_POLL_MAX_ERRORS='3'
go run ./cmd/wechat-ui-bridge
```

Daily pause windows use local `HH:MM-HH:MM` time and can cross midnight, for example `22:00-01:00`. These windows are the default "operator takes over" schedule: during that time the bridge may keep running, but polling skips work and send/read operations fail closed without focusing WeChat. UI-touching endpoints return HTTP `423` with `operator pause active`.

For an immediate temporary human takeover without restarting the bridge, prefer the local loopback API:

```powershell
Invoke-RestMethod -Method Post `
  -Uri http://127.0.0.1:3021/api/Operator/PauseSet `
  -ContentType 'application/json' `
  -Body '{"minutes":30,"reason":"operator takeover"}'
```

To pause until an exact local/RFC3339 time:

```powershell
Invoke-RestMethod -Method Post `
  -Uri http://127.0.0.1:3021/api/Operator/PauseSet `
  -ContentType 'application/json' `
  -Body '{"pause_until":"2026-06-24T18:30:00+08:00","reason":"operator takeover"}'
```

To resume immediately:

```powershell
Invoke-RestMethod -Method Post http://127.0.0.1:3021/api/Operator/PauseClear
```

The same dynamic pause can also be controlled by the local pause file:

```powershell
$pauseFile = "$env:TEMP\wechat-ui-operator-pause.json"
@{
  pause_until = '2026-06-24T18:30:00+08:00'
  reason = 'operator takeover'
} | ConvertTo-Json | Set-Content -Encoding UTF8 $pauseFile
```

To resume immediately:

```powershell
Remove-Item "$env:TEMP\wechat-ui-operator-pause.json" -ErrorAction SilentlyContinue
```

The bridge exposes a minimal old-protocol-compatible subset:

```text
GET  /health
POST /api/Login/GetCacheInfo
POST /api/User/GetContractProfile
POST /api/Friend/GetContractDetail
POST /api/Msg/SendTxt
POST /api/Msg/CurrentLastText
POST /api/Operator/PauseStatus
POST /api/Operator/PauseSet
POST /api/Operator/PauseClear
POST /api/Operator/UiStatus
POST /api/Operator/InjectCurrentLastText
POST /api/Operator/PollCurrentLastText
POST /api/Operator/PollStart
POST /api/Operator/PollStop
POST /api/Operator/PollStatus
```

`UiStatus` keeps the raw visible-window fields from the Python smoke script and adds bridge-level readiness fields:

- `blocked=true` means a known blocking window is covering WeChat, such as a Windows security/firewall prompt.
- `usable=false` means the bridge should not send/read yet. `unusable_reason` can be `blocking_window`, `unexpected_chat_title`, or `not_foreground`.
- `blocking_windows` is preserved so the operator can see which local window needs manual handling.

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
Invoke-RestMethod -Method Post http://127.0.0.1:3021/api/Operator/PauseStatus
Invoke-RestMethod -Method Post `
  -Uri http://127.0.0.1:3021/api/Operator/PauseSet `
  -ContentType 'application/json' `
  -Body '{"minutes":5,"reason":"smoke handoff"}'
Invoke-RestMethod -Method Post http://127.0.0.1:3021/api/Operator/PauseClear
Invoke-RestMethod -Method Post http://127.0.0.1:3021/api/Operator/UiStatus
Invoke-RestMethod -Method Post `
  -Uri http://127.0.0.1:3021/api/Msg/CurrentLastText `
  -ContentType 'application/json' `
  -Body '{}'
Invoke-RestMethod -Method Post `
  -Uri http://127.0.0.1:3021/api/Msg/SendTxt `
  -ContentType 'application/json' `
  -Body '{"Wxid":"wechat_ui_bot","ToWxid":"filehelper","Content":"wechat-ui bridge smoke"}'
```

Manual assistant-flow injection smoke:

```powershell
Invoke-RestMethod -Method Post `
  -Uri http://127.0.0.1:3021/api/Operator/InjectCurrentLastText `
  -ContentType 'application/json' `
  -Body '{"from_wxid":"filehelper","content":"manual visible text smoke"}'
```

When `content` is omitted, `InjectCurrentLastText` first runs the local `read-last` OCR check against the current visible WeChat conversation and then posts the resulting text to `WECHAT_UI_ASSISTANT_SYNC_URL` or the request `callback_url`. The callback URL is restricted to loopback hosts (`127.0.0.1`, `localhost`, or `::1`). This is a manual bridge into the existing `/api/v1/wechat-client/:wechatID/sync-message` assistant flow; it is not continuous polling.

Manual injection has a local in-memory duplicate guard. By default, the same `wechat_id`/`from_wxid`/`to_wxid`/`content` is suppressed for 120 seconds and returns HTTP 409 instead of forwarding the same visible text twice. Use `WECHAT_UI_INJECT_DEDUPE_TTL_SECONDS=0` to disable this guard for debugging, or pass an explicit `dedupe_key`/`skip_dedupe` in the request when a controlled test requires it.

The bridge also keeps a short in-memory record of text it has just sent through `SendTxt`. By default, if polling sees the same text in the same conversation within 300 seconds, it reports `skipped=true` with `reason=outgoing echo` and does not inject it into the assistant flow. Set `WECHAT_UI_OUTGOING_ECHO_TTL_SECONDS=0` only for controlled debugging.

For group-style injection, pass a chatroom `from_wxid`, a `sender_wxid`, and optionally `at_bot=true` or `at_wxid`:

```powershell
Invoke-RestMethod -Method Post `
  -Uri http://127.0.0.1:3021/api/Operator/InjectCurrentLastText `
  -ContentType 'application/json' `
  -Body '{"from_wxid":"room@chatroom","sender_wxid":"wxid_user","content":"助手：群聊 smoke","at_bot":true}'
```

The bridge formats this as the existing callback expects: `Content` becomes `sender_wxid:\ntext`, and `at_bot=true` writes the bot wxid into `MsgSource`/`atuserlist` so the main service can set `IsAtMe`. This is only the payload bridge foundation; reliable sender extraction from the WeChat 4.x UI still needs a later UI parsing stage.

Single-step poll smoke:

```powershell
Invoke-RestMethod -Method Post `
  -Uri http://127.0.0.1:3021/api/Operator/PollCurrentLastText `
  -ContentType 'application/json' `
  -Body '{}'
```

`PollCurrentLastText` reads the current visible last text once, compares it with the bridge's in-memory checkpoint for the `wechat_id`/`from_wxid`/`to_wxid` stream, and only injects when the text changed. Pass `{"inject":false}` to observe and update the checkpoint without calling the main service. This endpoint is intended as the safe stepping stone toward low-frequency polling; it does not run a background loop by itself.

Explicit low-frequency polling loop:

```powershell
Invoke-RestMethod -Method Post `
  -Uri http://127.0.0.1:3021/api/Operator/PollStart `
  -ContentType 'application/json' `
  -Body '{"interval_seconds":60}'

Invoke-RestMethod -Method Post http://127.0.0.1:3021/api/Operator/PollStatus
Invoke-RestMethod -Method Post http://127.0.0.1:3021/api/Operator/PollStop
```

The poll loop is never started by default. `PollStart` runs one check immediately and then repeats at a low frequency; intervals below 30 seconds are clamped to 30 seconds. During operator takeover pause windows or a dynamic pause file, the loop records a skipped state and does not read or focus the WeChat window.

By default, the loop stops itself after 3 consecutive poll errors (`WECHAT_UI_POLL_MAX_ERRORS=3`). This prevents repeated focus/OCR attempts when the WeChat window is too small, hidden, on the wrong page, or otherwise unreadable. Pass `{"max_errors":0}` only for controlled debugging when the operator is watching the desktop.

By default, `PollStart` uses `prime_on_start=true`: the first immediate check records the current visible last text as the baseline with `inject=false`, so the bridge does not reply to an old message that was already on screen before polling started. Pass `{"prime_on_start":false}` only for a controlled smoke test where the current visible text should be injected immediately.

If the poll loop is allowed to inject (`inject` omitted or `true`), `PollStart` validates `WECHAT_UI_ASSISTANT_SYNC_URL` or request `callback_url` before starting and rejects non-loopback or empty callbacks. For observe-only loops, pass `{"inject":false}`; no callback is required.

Assistant-flow local E2E harness:

```powershell
python scripts/assistant_flow_e2e.py --self-test
```

`--self-test` only verifies the harness and its local mock servers. To check the real main service path without touching WeChat, run the harness with local mock OpenClaw and mock WeChat bridge ports, then start the main service inside the delay window:

```powershell
python scripts/assistant_flow_e2e.py `
  --wechat-port 3022 `
  --openclaw-port 18791 `
  --inject-delay-seconds 45 `
  --wechat-id wechat_ui_bot `
  --from-wxid wxid_e2e_friend `
  --content "assistant flow e2e smoke" `
  --reply "OpenClaw mock reply"
```

In another PowerShell window during the delay:

```powershell
$env:WECHAT_SERVER_HOST='127.0.0.1:3022'
$env:OPENCLAW_ENABLED='true'
$env:OPENCLAW_BASE_URL='http://127.0.0.1:18791/api/assistant/chat'
$env:BOT_NAME='助手'
$env:TRIGGER_MODE='at_or_prefix'
$env:TRIGGER_PREFIX='助手：'
go run .
```

The harness posts one `sync-message` callback to `http://127.0.0.1:9001/api/v1/wechat-client/{wechat_id}/sync-message` and waits for the main service to call the mock `/api/Msg/SendTxt`. `--wechat-id` must match the running `vars.RobotRuntime.WxID`; private chat AI or the relevant group whitelist must already be enabled in the local database. If no `/api/Msg/SendTxt` call is observed, check `WECHAT_SERVER_HOST`, `OPENCLAW_BASE_URL`, the active bot wxid, and the AI enablement settings before moving to real WeChat UI acceptance.

The harness can also start an isolated temporary main service on a different port, leaving an existing `9001` process untouched:

```powershell
python scripts/assistant_flow_e2e.py `
  --start-main-command "C:\Users\28029\.cache\codex-go\go1.26.4-tar\go\bin\go.exe run ." `
  --prepare-local-db `
  --main-port 9002 `
  --wechat-port 3022 `
  --openclaw-port 18791 `
  --wechat-id wechat_ui_bot `
  --from-wxid wxid_e2e_friend `
  --content "assistant flow real-main smoke" `
  --reply "OpenClaw mock reply real-main"
```

Earlier local diagnostic note (2026-06-24): without DB preparation, the temporary `9002` main service started and reported `/api/v1/robot/is-running=true` and `/api/v1/robot/is-loggedin=false`, and the callback returned HTTP 200. No mock OpenClaw request and no mock `/api/Msg/SendTxt` were observed because `robot_admin.robot.id=27` currently has an empty `wechat_id`; `SyncMessageCallback` ignores callbacks whose `{wechat_id}` does not match `vars.RobotRuntime.WxID`.

When using a fresh robot database, confirm that `messages` exists before callback acceptance. The table is required before message plugins can run; it is now included in startup auto-migration for the OpenClaw assistant route.

Use a normal-looking test sender such as `wxid_e2e_friend` for private-chat E2E. `filehelper` is useful for real WeChat UI smoke tests, but the main-service contact classification can treat special built-in accounts as non-friend contacts and skip private AI chat.

Validated local main-service E2E (2026-06-24): with `--prepare-local-db`, the harness temporarily set `robot_admin.robot.id=27` to `wechat_ui_bot`, enabled global private-chat AI, started a temporary `9002` main service, injected `wxid_e2e_friend`, observed one mock OpenClaw request, and captured mock `/api/Msg/SendTxt` with `Content="OpenClaw mock reply cleanup smoke"`. The DB patch restored the original robot/global-settings values and removed test messages/contacts after the run.

Point the main service at this bridge only after the explicit send/read smoke checks pass:

```powershell
$env:WECHAT_SERVER_HOST='127.0.0.1:3021'
```

Current bridge boundary: this first bridge wraps explicit text submission to the currently open conversation, OCR-based visible last-text reads, manual current-last-text injection into the existing assistant sync callback, group-shaped callback payload construction, single-step changed-text polling, an explicit low-frequency poll loop, and recent outgoing echo suppression. With `WECHAT_UI_SEND_CURRENT_CHAT=true`, both bridge send and bridge read avoid automatic contact search even if a `filehelper`/`to_wxid` field is provided. A keyboard submission is not accepted as real delivery unless the same text becomes visible in the WeChat UI. On the current Windows WeChat 4.x client, contact-search navigation can fall into WeChat "搜一搜" and is not enabled as the default bridge path, and message-editor focus can fail from a background bridge process. Reliable group sender extraction from UI text, group `@` detection from UI text, prefix detection from UI text, and whitelist-driven automatic replies still need a later stage after real UI smoke validation.
