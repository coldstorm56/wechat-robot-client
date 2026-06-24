package robot

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientSendTextMessageReturnsHTTPErrorBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/Msg/SendTxt" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"Success":false,"Code":-5,"Message":"WeChat UI blocked: blocking_window","Data":{"error_code":"blocking_window"}}`))
	}))
	defer server.Close()

	client := NewClient(WechatDomain(strings.TrimPrefix(server.URL, "http://")), ProxyInfo{})
	_, err := client.SendTextMessage(SendTextMessageRequest{
		Wxid:    "wechat_ui_bot",
		ToWxid:  "filehelper",
		Content: "hello",
	})

	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "blocking_window") {
		t.Fatalf("err=%v", err)
	}
}
