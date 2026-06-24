# 更新日志

## [Unreleased] - 2026/06/22

- Runtime check: add a local WeChat 4.x UI automation smoke script for window detection, explicit text send, and last visible text reads without public protocol endpoints.
- Runtime docs: document the WCFerry/classic-WeChat blocker and the new local-only WeChat 4.x UI automation route with clipboard/window restoration safety notes.
- Protocol bridge: add a loopback-only WeChat UI compatibility bridge with health, profile/contact stubs, explicit text sending, and visible last-text reads.
- Runtime check: require visible UI delivery verification before reporting a WeChat UI send as successful; best-effort keyboard submission is now explicit and not accepted as delivery evidence.
- Runtime check: switch WeChat UI send/read smoke evidence to local chat-area OCR; verified `Codex visible OCR smoke 2026-06-23` in File Transfer Assistant and read it back as the last visible text.
- Protocol bridge: keep current-chat read mode aligned with current-chat send mode so bridge smoke checks do not enter WeChat 4.x contact search by default.
- Runtime safety: document WeChat 4.x MMUI input-focus risk; bridge send attempts fail closed when visible OCR delivery cannot prove the message was sent.
- Runtime safety: add operator takeover pause windows and a local dynamic pause file so the WeChat UI bridge can avoid send/read automation during human handoff periods.
- Runtime tooling: add a PowerShell helper for setting, checking, and clearing WeChat UI operator takeover pauses through the bridge API or local pause file.
- Runtime safety: verify the message editor draft before pressing Enter, so WeChat UI send attempts fail closed before submission when the MMUI editor cannot be focused.
- Runtime safety: add an expected chat-title guard so current-chat send/read automation refuses to run on the wrong WeChat surface.
- Protocol bridge: expose a read-only WeChat UI status endpoint with OCR title and title-match state for operator/runtime readiness checks.
- Runtime safety: add local operator pause set/clear endpoints so a human takeover window can be applied without restarting the WeChat UI bridge.
- Protocol bridge: add a manual current-last-text injection endpoint that forwards visible WeChat text into the existing main-service `sync-message` assistant flow through a loopback-only callback.
- Runtime safety: add an in-memory duplicate guard for manual WeChat UI text injection so the same visible message is not forwarded repeatedly by accident.
- Protocol bridge: add a single-step current-last-text poll endpoint that injects only when the visible text changed, preparing for safer low-frequency polling.
- Protocol bridge: add explicit start/stop/status controls for a low-frequency WeChat UI poll loop with pause-aware skipping and a minimum poll interval.
- Runtime safety: suppress recently sent WeChat UI text during polling so assistant replies are not re-injected as incoming messages.
- Runtime safety: make the WeChat UI poll loop prime the current visible text on startup by default, avoiding immediate replies to stale on-screen messages.
- Protocol bridge: support group-shaped WeChat UI callback payloads with `sender_wxid` and `at_bot` metadata so existing group trigger logic can be exercised through the UI bridge.
- Runtime safety: validate injecting poll-loop callback configuration before startup, while still allowing observe-only polling without a callback.
- Runtime safety: stop the WeChat UI poll loop after consecutive read/inject errors by default, reducing repeated focus/OCR attempts during window-size or operator handoff conflicts.
- Runtime safety: detect known blocking system dialogs over the WeChat window and fail closed before OCR/send; document resizable WeChat window guidance and operator takeover windows.
- Runtime safety: add a configurable minimum WeChat window size preflight so resized windows that are too small are reported as `window_too_small` before send/read automation.
- Protocol bridge: normalize `/api/Operator/UiStatus` with `blocked`, `usable`, and `unusable_reason` fields so callers can avoid UI automation while WeChat is covered or on the wrong chat.
- Runtime safety: lock the operator-pause contract in tests so send/read/status/inject/poll endpoints return HTTP 423 without invoking UI automation during manual takeover.
- Runtime validation: cover `/api/Operator/UiStatus` at the handler level with a fake local smoke script, proving blocked-window script output becomes `blocked=true` and `usable=false` in the bridge API.
- Runtime safety: add optional `require_ui_usable` preflight to `PollStart`, rejecting background polling before it starts when `UiStatus` reports an unusable WeChat surface.
- Protocol bridge: add `/api/Operator/Readiness` as a combined pause/UI/poll preflight endpoint; it avoids touching WeChat while an operator pause is active.
- Runtime safety: return structured `blocking_window` errors from WeChat UI send/read script failures and map them to HTTP 409 in the bridge.
- Runtime validation: make the assistant-flow E2E harness verify `assistant_session_logs.status=success` and expected `reply_text` before local DB cleanup.
- Runtime validation: extend the assistant-flow E2E harness to prepare reversible chatroom whitelist/member data and verify group `@bot` plus `助手：` prefix triggers through mock OpenClaw and mock WeChat send.
- Runtime validation: add an assistant-flow negative E2E mode for non-whitelisted groups, proving no OpenClaw request and no WeChat send are produced when chatroom AI is disabled.
- Runtime safety: log AI reply send failures from the assistant plugin so WeChat UI bridge errors such as `blocking_window` are visible in the main service logs.
- Runtime fix: make the robot text-send client return non-2xx WeChat bridge response bodies as errors, preserving `blocking_window` details for main-service logs.
- Runtime validation: let the assistant-flow E2E harness simulate WeChat UI bridge send failures and assert that the main-service log contains the expected bridge error text.
- Runtime validation: add a local assistant-flow E2E harness with mock OpenClaw and mock WeChat bridge endpoints to verify the 9001 `sync-message` to `/Msg/SendTxt` path before real WeChat UI acceptance.
- Runtime validation: let the assistant-flow E2E harness start an isolated temporary main service and report callback/preflight diagnostics when active robot wxid or AI enablement prerequisites are not satisfied.
- Runtime validation: add a PowerShell WeChat UI acceptance runner that aggregates bridge tests, harness self-test, read-only real UI status, and optional full mock main-service E2E coverage.
- Runtime tooling: add a read-only WeChat UI diagnostic helper that summarizes window size, title match, foreground state, blocking windows, and the next manual action before real send/read smoke.
- Runtime tooling: add a reusable PowerShell WeChat UI bridge launcher that sets encoding-safe defaults, window thresholds, assistant callback URL, and operator takeover windows.
- Runtime validation: add a gated real WeChat smoke helper that refuses to send unless read-only readiness passes, then verifies visible send and last-text readback.
- Runtime tooling: add a PowerShell main-service launcher that points the assistant runtime at the local WeChat UI bridge and loopback OpenClaw bridge with encoding-safe bot trigger defaults.
- Runtime tooling: add a read-only local stack status helper for checking UI bridge, main service, and OpenClaw bridge endpoints before real WeChat smoke.
- Runtime validation: expand the WeChat UI acceptance runner to parse helper scripts and verify bridge/main/status helper safe defaults.
- Runtime check: expand WeChat UI title OCR for fullscreen windows and document fullscreen as an acceptable daily operation mode.
- Runtime safety: switch WeChat UI paste/submit keystrokes to Win32 keyboard events and report WeChat 4.x login prompts as `requires_login` instead of attempting chat automation.
- Protocol bridge: normalize WeChat UI login prompts as `requires_login` in operator readiness so polling and real send preflights fail closed until the assistant account is manually logged in.
- Runtime safety: change real WeChat visible-verification message defaults and docs to normal ASCII memo-style text instead of obvious automation test wording.
- Runtime fix: widen WeChat UI OCR chat/editor regions for fullscreen windows so left-aligned drafts and incoming bubbles are not cropped during verification.
- Runtime validation: lengthen gated real WeChat visible-delivery OCR waits while keeping last-visible-message matching as the final send proof.
- Runtime safety: retry WeChat UI click/paste once when editor draft OCR fails, while still refusing to press Enter until the expected draft is visible.
- Runtime validation: increase full assistant-flow E2E main-service startup waits to tolerate slow local MySQL auto-migration during acceptance.
- Runtime fix: include the `messages` table in startup auto-migration so fresh OpenClaw assistant databases can process WeChat callbacks and reach assistant plugins.
- Runtime validation: pass the local temporary-main-service assistant-flow E2E check from `sync-message` through mock OpenClaw to mock `/Msg/SendTxt` with reversible local DB preparation.
- Runtime safety: tighten WeChat UI send verification so a successful send must also match the current conversation's last readable message.
- Runtime debug: add local OpenClaw HTTP bridge, Docker Compose override, and local end-to-end validation notes.
- Runtime fix: make the OpenClaw bridge tolerate plain-text CLI replies and migrate local admin/MCP tables required by startup.
- Runtime fix: expose the local `wechat-ipad` protocol service on `127.0.0.1:3010` for real personal WeChat login, keeping `127.0.0.1:8090` for the WeChat auth/admin service.
- Runtime fix: return QR code URL/base64 fields from `/api/v1/robot/login` for real scan-login validation.
- Runtime validation: real QR generation and scan callbacks reached the protocol service, but current `wechat-ipad:latest` login was blocked by WeChat version checks across Mac/iPad/QRx/WinUnified/WinUwp and by a Padx interaction-key error.

### 新功能

- 阶段 1：新增 OpenClaw 助理 MVP 的运行时配置、环境变量示例和 HTTP Adapter，默认关闭以保持现有 AI 路径不变。
- 阶段 2：OpenClaw 开启时将私聊/群聊 AI 回复路由到 OpenClaw，并接入群白名单、群聊 `TRIGGER_MODE`/`TRIGGER_PREFIX` 和按条数控制的上下文窗口。
- 阶段 3：新增 `assistant_session_logs` 会话日志表、仓储和迁移，并记录 OpenClaw 请求、响应、回复、耗时与异常。
- 阶段 4：新增 OpenClaw AI 助理 MVP 配置文档，并在 README 提供入口。

## [5.1.0] - 2026/05/16

### 体验性优化

- iPad 协议新增支持 pprof 监控(查看方法: 机器人详情界面，更新镜像下拉框 -> 性能采样)

## [5.0.0] - 2026/05/16

### 体验性优化

- 重构了长期记忆模块

- 优化了 GitHub Actions 的构建速度

- 群聊总结新增图片模式

- 修复引用抖音链接也会触发抖音解析的问题，抖音视频下载增加了大小限制，超过 25M 的视频不会下载

- 优化 Qdrant 向量数据库 Docker-compose 配置

- 移除了一些废弃的数据表

- 处理历史消息的时候，超过 10 分钟的历史消息不会处理，避免扫码登录且勾选了同步历史消息的时候引起不必要的刷屏

- 同步群成员新增群成员的性别信息，群成员的性别信息会注入 AI 上下文

## [4.7.0] - 2026/05/01

### 体验性优化

- OpenAI SDK 迁移到官方版

- 同步朋友圈时间间隔，以前硬编码 10 分钟，现在支持界面设置

### 新功能

- 重构了记忆模块

## [4.6.0] - 2026/04/19

### BUG 修复

- 优化艾特处理逻辑

- 优化语言发送接口失真问题

### 体验性优化

- 去除返回的思考内容`<think />` 和 `<thinking />`标签包裹的内容

- 支持修改文本嵌入维度，以前代码写死 1536，现在支持配置，以适配更多模型

### 新功能

- 放开上传视频能力，最大上传视频限制 25M，配合`豆包视频理解`Skill，实现解析视频消息，更多 Skills 访问[https://git.houhoukang.com/houhou/wechat-robot-skills](https://git.houhoukang.com/houhou/wechat-robot-skills)

## [4.5.0] - 2026/04/10

### BUG 修复

- 修复解析引用消息可能会类型解析错误的问题

- 暂时移除长期记忆功能，待重构后上线

### 体验性优化

- 优化 skill 执行流程

### 新功能

- 内置检索知识库文档工具

## [4.4.0] - 2026/04/05

### 体验性优化

- 重写了记忆模块

## [4.3.1] - 2026/04/04

### 体验性优化

- Skills 安装源支持 Gitea

- 去除 AI 返回的 <think></think> 思维链内容

## [4.3.0] - 2026/04/04

### 新功能

- OSS 支持火山云

### 体验性优化

- 优化抖音解析背景音乐发送效果

## [4.2.0] - 2026/03/29

### 新功能

- 优化聊天记录搜索功能

## [4.1.0] - 2026/03/21

### 新功能

- AI 长久记忆新增开关

## [4.0.0] - 2026/03/13

### 新功能

- AI 对话支持长久记忆

## [3.1.0] - 2026/03/12

### 破坏性更新

- 表结构更新，请按照 [SQL 升级脚本](https://github.com/hp0912/wechat-robot-admin-backend/blob/main/template/3_1_0.sql)进行升级

### 新功能

- `Agent Skills 引擎`注入机器人上下文环境变量

```
ROBOT_WECHAT_CLIENT_PORT: 机器人客户端服务端口，可用于在 SKILL 脚本直接调用客户端接口 `http://127.0.0.1:{ROBOT_WECHAT_CLIENT_PORT}/api/v1/xxxxx`
ROBOT_ID: 机器人实例 ID
ROBOT_CODE: 机器人实例编码
ROBOT_REDIS_DB: 机器人的 Redis DB
ROBOT_WX_ID: 机器人的微信 ID
ROBOT_FROM_WX_ID: 微信消息来源(群聊 ID 或者好友微信 ID)
ROBOT_SENDER_WX_ID: 微信消息发送人的微信 ID
ROBOT_MESSAGE_ID: 微信消息 ID
ROBOT_REF_MESSAGE_ID: 如果是引用消息，则是引用的消息的 ID
```

- 支持注入自定义环境变量，可用于配置 SKILL 脚本的私密数据

- 新暴露了支持发送本地文件的发送图片/视频/文件/语音的接口(用于 Skills)

```
Body 公共参数
{
  "to_wxid": "",
  "file_path": ""
}
[POST] http://127.0.0.1:{ROBOT_WECHAT_CLIENT_PORT}/api/v1/robot/message/send/image/local
[POST] http://127.0.0.1:{ROBOT_WECHAT_CLIENT_PORT}/api/v1/robot/message/send/video/local
[POST] http://127.0.0.1:{ROBOT_WECHAT_CLIENT_PORT}/api/v1/robot/message/send/voice/local
[POST] http://127.0.0.1:{ROBOT_WECHAT_CLIENT_PORT}/api/v1/robot/message/send/file/local
```

## [3.0.0] - 2026/03/07

### 破坏性更新

- 表结构更新，请按照 [SQL 升级脚本](https://github.com/hp0912/wechat-robot-admin-backend/blob/main/template/3_0_0.sql)进行升级

- `wechat-robot-admin-backend` 服务需要新增一个环境变量：HOST_DATA_DIR: ${PWD} # 自动取 docker-compose.yml 所在目录的绝对路径，详情见 docker-compose.yml 文件

### 新功能

- 内置 Agent Skills 引擎

- 新增群红包通知功能，支持通知指定的多个人

## [2.6.0] - 2026/02/22

### 破坏性更新

- docker-compose 文件新增本地部署网易云

> 网易云部署完后，记得访问 `http://localhost:3000/qrlogin.html` 登录

- MCP 点歌工具采用本地访问本地部署服务，需要更新 MCP 工具版本

### 体验性优化

- 抖音解析支持发送图片 BGM

## [2.5.0] - 2026/02/18

### 破坏性更新

- 表结构更新，请按照 [SQL 升级脚本](https://github.com/hp0912/wechat-robot-admin-backend/blob/main/template/2_5_0.sql)进行升级

### 新特性

- 新增群红包提醒功能

- 新增 AI 播客功能(完善中...)

### 体验性优化

- 普通聊天去除 ai 参数 MaxCompletionTokens，支持思考的模型思维链会占用 token 数，导致 AI 无内容输出

- AI 后端改为流式输出，以支持某些思考模型，流式输出完成后再将消息统一发送到微信。

- 提升命令行 MCP 服务器的兼容性

## [2.4.0] - 2026/02/01

### 破坏性更新

- 表结构更新，请按照 [SQL 升级脚本](https://github.com/hp0912/wechat-robot-admin-backend/blob/main/template/2_4_0.sql)进行升级

- AI 绘图配置数据结构有变化，请重新配置

### 体验性优化

- 支持解析抖音图片

- AI 绘图新增造相(Z-Image)

## [2.3.0] - 2026/01/02

### 破坏性更新

- 表结构新增和更新，请按照 [SQL 升级脚本](https://github.com/hp0912/wechat-robot-admin-backend/blob/main/template/2_3_0.sql)进行升级

### 体验性优化

- 群聊短视频解析支持开/关了

## [2.2.0] - 2025/12/31

### 破坏性更新

- 表结构新增和更新，请按照 [SQL 升级脚本](https://github.com/hp0912/wechat-robot-admin-backend/blob/main/template/2_2_0.sql)进行升级

### 新特性

- 新增表情包提取 MCP 工具

- 新增群成员管理功能，将群成员设置为管理员 / 将群成员加入黑名单(黑名单的成员不会触发 AI 交互)

### 体验性优化

- 优化聊天记录查询性能

- 优化 MCP 协议，新增 MCP 私有协议，用于发送聊天记录、图片、视频、语音等等

- 优化群聊总结提示词，新增提取群聊分享的链接资源

- Mac 自动过滑块改为手动过滑块，提升准确性

### BUG 修复

- 修复引用消息丢失了部分上下文的问题

- 修复群成员最后活跃时间不正确的问题

## [2.1.0] - 2025/11/23

### 体验性优化

- 图片 / 视频改成分片发送，解决发送大视频内存爆炸的问题

## [2.0.0] - 2025/11/15

### 破坏性更新

- `wechat-robot-admin-backend`服务新增了一个必填环境变量`UUID_URL`，可填写为`http://wechat-slider:9000`

- 表结构新增和更新，请按照 [SQL 升级脚本](https://github.com/hp0912/wechat-robot-admin-backend/blob/main/template/2_0_0.sql)进行升级

- 需要更新`wechat-slider`服务的镜像，执行 `docker pull registry.cn-shenzhen.aliyuncs.com/houhou/wechat/wechat-slider-base:latest`

- 移除 `jimeng-free-api` 服务，执行 `docker compose rm -s -f jimeng-free-api` 或者 `docker-compose rm -s -f jimeng-free-api`，哪个能用用哪个

- 拉取最新代码获取最新`docker-compose.yml`文件，根据文件内容，手动拉一遍镜像，成功率更高。

### 新特性

- 完整的 MCP 协议支持

- 开放微信消息 Webhook 回调

### 体验性优化

- iPad 协议新增支持 pprof 监控(启用方法: 机器人详情界面，更新镜像下拉框 -> 删除服务端容器 -> 创建服务端容器 (启用pprof))

- 即梦逆向 api 由 [jimeng-free-api](https://github.com/LLM-Red-Team/jimeng-free-api) 迁移到 [jimeng-api](https://github.com/iptag/jimeng-api)，以支持更多功能

- 优化机器人管理后台前端项目本地Docker构建(本地构建使用 dev.Dockerfile)

### BUG 修复

- 修复 iPad 协议提取 ticket 异常的问题

## [1.6.0] - 2025/10/12

### 体验性优化

- 提高抖音视频解析稳定性。(破坏性更新： 抖音解析的 API 由免费 API 改为收费 API，原先的环境变量`THIRD_PARTY_API_KEY`记得保持余额充足，一分钱解析10次。附：[充值链接](https://api.pearktrue.cn/dashboard/profile))

## [1.5.2] - 2025/10/01

### 新特性

- 消息图片自动上传 OSS (需要执行数据库升级脚本[https://github.com/hp0912/wechat-robot-admin-backend/blob/1.3.2/template/1_3_2.sql](https://github.com/hp0912/wechat-robot-admin-backend/blob/1.3.2/template/1_3_2.sql))

## [1.5.1] - 2025/09/27

### 体验性优化

- 支持登录设备迁移 (导出登录信息、导入登录信息)

## [1.5.0] - 2025/09/27

### 体验性优化

- 显示当前登录设备类型和微信版本。

## [1.4.3] - 2025/09/22

### 新特性

- iPad 伪装登录。

## [1.4.2] - 2025/09/21

### BUG 修复

- 修复点歌接口挂了的问题

- 修复发送AI消息获取音色接口挂了的问题

- 修复扫码登录UUID检测异常的问题

## [1.4.1] - 2025/09/21

### 新特性

- Mac 扫码登录支持自动过滑块。

> 本次更新包含破坏性更新
>
> `wechat-robot-admin-backend`服务需要新增两个环境变量，否则服务会启动失败
>
> - SLIDER_SERVER_BASE_URL=http://wechat-slider:9000
> - SLIDER_TOKEN=xxxxxxx # 滑块验证码服务密钥，请加入官方交流群获取

## [1.4.0] - 2025/09/20

### 新特性

- ~~Mac 扫码登录支持手动过滑块。~~

> 本次更新包含破坏性更新
>
> `wechat-robot-admin-backend`服务需要新增两个环境变量，否则服务会启动失败
>
> - ~~SLIDER_VERIFY_URL=http://wechat-slider:9000/api/v1/slider-verify-html~~
> - ~~SLIDER_VERIFY_SUBMIT_URL=http://wechat-slider:9000/api/v1/security-verify~~

## [1.3.0] - 2025/09/19

### 体验性优化

- 抖音视频会同时发送链接和视频。

### BUG 修复

- 修复 Mac 登录异常的问题。

## [1.2.0] - 2025/09/10

### 体验性优化

- 抖音视频由手动出发改为自动触发。

## [1.1.15] - 2025/09/09

### 体验性优化

- 抖音视频解析由直接发送视频改为发送卡片链接。

## [1.1.14] - 2025/09/06

### 体验性优化

- 公众号如果没有手动开启AI聊天，默认不开启 (原来会继承全局 AI 设置)。

- 群聊总结改为以聊天记录的形式发送，避免内容过多被折叠。

## [1.1.13] - 2025/09/04

### BUG 修复

- 修复因为每日早报上游接口数据结构变化导致获取每日早报失败的问题

- 修复AI机器人在私聊场景会响应自己发送(从其他设备发送)的消息的问题。

## [1.1.12] - 2025/08/29

### 新特性

- 支持通过登录密钥登录机器人管理后台

## [1.1.11] - 2025/08/19

### 新特性

- 支持多种登录方式(iPad、Windows微信、车载微信、Mac微信)

- 支持Data62登录

- 支持A16登录

### 修改

- 优化创建机器人docker容器流程，如果docker镜像还没拉取过，会自动拉取 (wechat-robot-admin-backend)

## [1.1.10] - 2025/08/18

### 新特性

- 新增文本消息群发接口

## [1.1.9] - 2025/08/16

### 新特性

- 支持发送文件消息，流式发送，避免发送超大文件时，内存溢出
