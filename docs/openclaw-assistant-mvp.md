# OpenClaw AI 助理 MVP 配置说明

本文档说明第一阶段 OpenClaw AI 助理底座的启用方式和当前边界。

## 启用条件

OpenClaw 默认关闭。只有设置以下环境变量后，现有私聊/群聊 AI 回复才会改走 OpenClaw：

```bash
OPENCLAW_ENABLED=true
OPENCLAW_BASE_URL=http://127.0.0.1:8080/api/assistant/chat
OPENCLAW_API_KEY=your-api-key
```

`OPENCLAW_BASE_URL` 当前按完整文本对话接口地址处理，客户端会直接向该地址发送 `POST` JSON 请求。

## 环境变量

```bash
OPENCLAW_ENABLED=false
OPENCLAW_BASE_URL=
OPENCLAW_API_KEY=
OPENCLAW_TIMEOUT=30
BOT_NAME=助手
TRIGGER_MODE=at_or_prefix
TRIGGER_PREFIX=助手：
ENABLE_CONTEXT=true
CONTEXT_WINDOW=10
MAX_REPLY_LENGTH=1200
```

- `OPENCLAW_ENABLED`：总开关，默认关闭。
- `OPENCLAW_BASE_URL`：OpenClaw 文本对话接口地址。
- `OPENCLAW_API_KEY`：可选鉴权密钥，会作为 `Authorization: Bearer ...` 请求头发送。
- `OPENCLAW_TIMEOUT`：请求超时秒数。
- `BOT_NAME`：发送给 OpenClaw 的机器人名称。
- `TRIGGER_MODE`：群聊触发模式，支持 `always`、`at`、`prefix`、`at_or_prefix`。
- `TRIGGER_PREFIX`：群聊前缀触发词。
- `ENABLE_CONTEXT`：是否发送上下文窗口。
- `CONTEXT_WINDOW`：发送给 OpenClaw 的上下文消息条数，包含当前消息。
- `MAX_REPLY_LENGTH`：OpenClaw 回复最大字符数，按 rune 截断。

## 当前行为

- 私聊：仍尊重好友级或全局 AI 开关；开启后自动调用 OpenClaw。
- 群聊：OpenClaw 模式下只响应群设置里显式开启 AI 的群，作为第一阶段群白名单。
- 群聊触发：按 `TRIGGER_MODE` 判断，默认支持 @机器人 或 `助手：` 前缀。
- 上下文：OpenClaw 模式使用 `CONTEXT_WINDOW` 按条数取最近文本/引用上下文；关闭上下文时只发送当前消息。
- 回复：OpenClaw 响应兼容 `{ "reply": "..." }` 和 `{ "content": "..." }`。
- 日志：`assistant_session_logs` 记录请求、响应、回复、耗时和异常；API key 不会写入请求 payload。

## 第一阶段暂不包含

- 企业微信官方方案或外部客户群协议接入。
- OpenClaw 工作流编排 UI。
- 多模态消息完整转发。
- tool/action 结构化返回处理。
- 频率限制和灰度策略。

## 最小验证建议

1. 配置一个返回 `{ "reply": "pong" }` 的 OpenClaw mock 接口。
2. 设置 `OPENCLAW_ENABLED=true` 和 `OPENCLAW_BASE_URL`。
3. 私聊开启 AI 后发送文本，确认机器人回复 `pong`。
4. 群聊只给白名单群开启 AI，分别验证 @机器人 和 `助手：ping`。
5. 查询 `assistant_session_logs`，确认有 `success` 或可定位的 `failed` 记录。
