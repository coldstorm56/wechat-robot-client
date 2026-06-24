package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"wechat-robot-client/vars"
)

func TestOpenClawAdapterCallUsesReplyAndAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("expected authorization header, got %q", got)
		}
		var req OpenClawRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Message != "你好" {
			t.Fatalf("expected message, got %q", req.Message)
		}
		_, _ = w.Write([]byte(`{"reply":"你好，我是助手"}`))
	}))
	defer server.Close()

	adapter := NewOpenClawAdapter(vars.OpenClawSettingS{
		BaseURL: server.URL,
		APIKey:  "test-key",
		Timeout: time.Second,
	})
	result, err := adapter.Call(context.Background(), OpenClawRequest{Message: "你好"})
	if err != nil {
		t.Fatalf("call OpenClaw: %v", err)
	}
	if result.Reply != "你好，我是助手" {
		t.Fatalf("expected reply, got %q", result.Reply)
	}
	if result.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", result.StatusCode)
	}
}

func TestOpenClawAdapterCallFallsBackToContentAndTruncates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"content":"一二三四五"}`))
	}))
	defer server.Close()

	adapter := NewOpenClawAdapter(vars.OpenClawSettingS{
		BaseURL:        server.URL,
		Timeout:        time.Second,
		MaxReplyLength: 3,
	})
	result, err := adapter.Call(context.Background(), OpenClawRequest{Message: "测试"})
	if err != nil {
		t.Fatalf("call OpenClaw: %v", err)
	}
	if result.Reply != "一二三" {
		t.Fatalf("expected truncated content, got %q", result.Reply)
	}
}
