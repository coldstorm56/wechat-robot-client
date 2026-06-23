package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/openai/openai-go/v3"

	"wechat-robot-client/pkg/robotctx"
	"wechat-robot-client/vars"
)

func TestAIChatServiceChatWithOpenClawMock(t *testing.T) {
	oldSettings := *vars.OpenClawSettings
	oldDB := vars.DB
	defer func() {
		*vars.OpenClawSettings = oldSettings
		vars.DB = oldDB
	}()
	vars.DB = nil

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer mock-key" {
			t.Fatalf("expected bearer auth, got %q", got)
		}

		var req OpenClawRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Channel != "wechat_group" {
			t.Fatalf("expected wechat_group channel, got %q", req.Channel)
		}
		if req.SessionID != "group:room@chatroom:wxid_user" {
			t.Fatalf("unexpected session id %q", req.SessionID)
		}
		if req.Message != "助手：查一下养时记" {
			t.Fatalf("unexpected message %q", req.Message)
		}
		if len(req.Messages) != 2 {
			t.Fatalf("expected 2 context messages, got %d", len(req.Messages))
		}
		if req.Metadata["trigger_mode"] != "at_or_prefix" {
			t.Fatalf("unexpected trigger mode metadata: %#v", req.Metadata["trigger_mode"])
		}

		_, _ = w.Write([]byte(`{"reply":"mock pong"}`))
	}))
	defer server.Close()

	*vars.OpenClawSettings = vars.OpenClawSettingS{
		Enabled:        true,
		BaseURL:        server.URL,
		APIKey:         "mock-key",
		Timeout:        time.Second,
		BotName:        "助手",
		TriggerMode:    "at_or_prefix",
		TriggerPrefix:  "助手：",
		EnableContext:  true,
		ContextWindow:  10,
		MaxReplyLength: 1200,
	}

	svc := NewAIChatService(t.Context(), nil)
	reply, err := svc.Chat(robotctx.RobotContext{
		RobotID:      27,
		RobotCode:    "test-bot",
		RobotWxID:    "wxid_bot",
		FromWxID:     "room@chatroom",
		SenderWxID:   "wxid_user",
		MessageID:    123,
		MsgID:        456,
		RefMessageID: 0,
	}, []openai.ChatCompletionMessageParamUnion{
		openai.AssistantMessage("历史回答"),
		openai.UserMessage("助手：查一下养时记"),
	})
	if err != nil {
		t.Fatalf("chat with OpenClaw: %v", err)
	}
	if reply.Content != "mock pong" {
		t.Fatalf("expected mock reply, got %q", reply.Content)
	}
}
