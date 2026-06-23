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
