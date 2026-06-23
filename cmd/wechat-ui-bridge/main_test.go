package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"wechat-robot-client/pkg/robot"
)

func TestParseAliasesAndContactName(t *testing.T) {
	cfg := bridgeConfig{
		ContactAliases: parseAliases("filehelper=文件传输助手,wxid_a=张三,bad"),
	}

	for wxid, want := range map[string]string{
		"filehelper": "文件传输助手",
		"wxid_a":     "张三",
		"wxid_b":     "wxid_b",
	} {
		if got := contactName(cfg, wxid); got != want {
			t.Fatalf("contactName(%q)=%q, want %q", wxid, got, want)
		}
	}
}

func TestEnsureLoopbackAddr(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:3021", "localhost:3021", "[::1]:3021"} {
		if err := ensureLoopbackAddr(addr); err != nil {
			t.Fatalf("expected %s to be accepted: %v", addr, err)
		}
	}
	if err := ensureLoopbackAddr("0.0.0.0:3021"); err == nil {
		t.Fatal("expected non-loopback address to be rejected")
	}
}

func TestEnsureLoopbackURL(t *testing.T) {
	for _, raw := range []string{"http://127.0.0.1:9001/api/v1/wechat-client/wechat_ui_bot/sync-message", "http://localhost:9001/callback", "https://[::1]:9001/callback"} {
		if err := ensureLoopbackURL(raw); err != nil {
			t.Fatalf("expected %s to be accepted: %v", raw, err)
		}
	}
	for _, raw := range []string{"http://example.com/callback", "ftp://127.0.0.1/callback", "/callback"} {
		if err := ensureLoopbackURL(raw); err == nil {
			t.Fatalf("expected %s to be rejected", raw)
		}
	}
}

func TestSplitComma(t *testing.T) {
	got := splitComma("a, b,,c ")
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("len=%d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d]=%q, want %q", i, got[i], want[i])
		}
	}
}

func TestPauseWindows(t *testing.T) {
	loc := time.FixedZone("CST", 8*60*60)
	now := time.Date(2026, 6, 24, 10, 30, 0, 0, loc)
	state := pauseStateFromWindows("test", "09:00-11:00,14:00-15:00", now)
	if !state.Paused {
		t.Fatal("expected current time to be paused")
	}
	if state.Until != "2026-06-24T11:00:00+08:00" {
		t.Fatalf("until=%q", state.Until)
	}

	now = time.Date(2026, 6, 24, 12, 0, 0, 0, loc)
	if state := pauseStateFromWindows("test", "09:00-11:00", now); state.Paused {
		t.Fatalf("did not expect pause: %#v", state)
	}
}

func TestPauseWindowAcrossMidnight(t *testing.T) {
	loc := time.FixedZone("CST", 8*60*60)
	now := time.Date(2026, 6, 24, 23, 30, 0, 0, loc)
	state := pauseStateFromWindows("test", "22:00-01:00", now)
	if !state.Paused {
		t.Fatal("expected overnight pause")
	}
	if state.Until != "2026-06-25T01:00:00+08:00" {
		t.Fatalf("until=%q", state.Until)
	}

	now = time.Date(2026, 6, 24, 0, 30, 0, 0, loc)
	if state := pauseStateFromWindows("test", "22:00-01:00", now); !state.Paused {
		t.Fatal("expected after-midnight pause")
	}
}

func TestPauseFileUntil(t *testing.T) {
	loc := time.FixedZone("CST", 8*60*60)
	now := time.Date(2026, 6, 24, 10, 30, 0, 0, loc)
	path := filepath.Join(t.TempDir(), "pause.json")
	if err := os.WriteFile(path, []byte("\ufeff"+`{"pause_until":"2026-06-24T11:00:00+08:00","reason":"handoff"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	state := pauseStateFromFile(path, now)
	if !state.Paused || state.Reason != "handoff" {
		t.Fatalf("state=%#v", state)
	}

	now = time.Date(2026, 6, 24, 11, 1, 0, 0, loc)
	if state := pauseStateFromFile(path, now); state.Paused {
		t.Fatalf("pause should expire: %#v", state)
	}
}

func TestBuildPauseFileConfigMinutes(t *testing.T) {
	loc := time.FixedZone("CST", 8*60*60)
	now := time.Date(2026, 6, 24, 10, 30, 0, 0, loc)
	cfg, err := buildPauseFileConfig(operatorPauseRequest{
		Minutes: 30,
		Reason:  "manual test",
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PauseUntil != "2026-06-24T11:00:00+08:00" {
		t.Fatalf("pause_until=%q", cfg.PauseUntil)
	}
	if cfg.Reason != "manual test" {
		t.Fatalf("reason=%q", cfg.Reason)
	}
}

func TestWritePauseFileRoundTrip(t *testing.T) {
	loc := time.FixedZone("CST", 8*60*60)
	now := time.Date(2026, 6, 24, 10, 30, 0, 0, loc)
	path := filepath.Join(t.TempDir(), "nested", "pause.json")
	if err := writePauseFile(path, pauseFileConfig{
		PauseUntil: "2026-06-24T11:00:00+08:00",
		Reason:     "manual takeover",
	}); err != nil {
		t.Fatal(err)
	}
	state := pauseStateFromFile(path, now)
	if !state.Paused || state.Reason != "manual takeover" || state.Until != "2026-06-24T11:00:00+08:00" {
		t.Fatalf("state=%#v", state)
	}
}

func TestBuildSyncMessageCallbackPayload(t *testing.T) {
	now := time.Unix(1_770_000_000, 123_000_000)
	payload, msgID := buildSyncMessageCallbackPayload("wechat_ui_bot", "filehelper", "wechat_ui_bot", "hello", "", now)
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var decoded robot.ClientResponse[robot.SyncMessage]
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if !decoded.Success || decoded.Code != 0 {
		t.Fatalf("decoded response=%#v", decoded)
	}
	if len(decoded.Data.AddMsgs) != 1 {
		t.Fatalf("AddMsgs len=%d", len(decoded.Data.AddMsgs))
	}
	msg := decoded.Data.AddMsgs[0]
	if msgID != 1770000000123 || msg.NewMsgId != msgID || msg.MsgId != msgID {
		t.Fatalf("msg ids got msgID=%d msg=%#v", msgID, msg)
	}
	if *msg.FromUserName.String != "filehelper" || *msg.ToUserName.String != "wechat_ui_bot" || *msg.Content.String != "hello" {
		t.Fatalf("unexpected message=%#v", msg)
	}
}

func TestInjectCurrentLastTextHandlerPostsProvidedContent(t *testing.T) {
	called := false
	var callbackErr error
	callback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.URL.Path != "/api/v1/wechat-client/wechat_ui_bot/sync-message" {
			callbackErr = fmt.Errorf("path=%s", r.URL.Path)
			http.Error(w, callbackErr.Error(), http.StatusBadRequest)
			return
		}
		var payload robot.ClientResponse[robot.SyncMessage]
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			callbackErr = err
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if len(payload.Data.AddMsgs) != 1 {
			callbackErr = fmt.Errorf("AddMsgs len=%d", len(payload.Data.AddMsgs))
			http.Error(w, callbackErr.Error(), http.StatusBadRequest)
			return
		}
		msg := payload.Data.AddMsgs[0]
		if *msg.FromUserName.String != "filehelper" || *msg.ToUserName.String != "wechat_ui_bot" || *msg.Content.String != "visible hello" {
			callbackErr = fmt.Errorf("message=%#v", msg)
			http.Error(w, callbackErr.Error(), http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer callback.Close()

	cfg := bridgeConfig{
		BotWxID:          "wechat_ui_bot",
		Timeout:          time.Second,
		AssistantSyncURL: callback.URL + "/api/v1/wechat-client/wechat_ui_bot/sync-message",
	}
	body := `{"from_wxid":"filehelper","content":"visible hello"}`
	req := httptest.NewRequest(http.MethodPost, "/api/Operator/InjectCurrentLastText", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	injectCurrentLastTextHandler(cfg)(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
	if !called {
		t.Fatal("callback was not called")
	}
	if callbackErr != nil {
		t.Fatal(callbackErr)
	}
}

func TestDedupeStoreReserveAndForget(t *testing.T) {
	store := newDedupeStore()
	now := time.Date(2026, 6, 24, 10, 0, 0, 0, time.UTC)
	if !store.TryReserve("k", now, time.Minute) {
		t.Fatal("first reserve should pass")
	}
	if store.TryReserve("k", now.Add(30*time.Second), time.Minute) {
		t.Fatal("duplicate reserve should be suppressed")
	}
	if !store.TryReserve("k", now.Add(61*time.Second), time.Minute) {
		t.Fatal("expired reserve should pass")
	}
	store.Forget("k")
	if !store.TryReserve("k", now.Add(62*time.Second), time.Minute) {
		t.Fatal("forgotten key should pass")
	}
}

func TestInjectCurrentLastTextHandlerSuppressesDuplicate(t *testing.T) {
	oldStore := injectedMessages
	injectedMessages = newDedupeStore()
	defer func() { injectedMessages = oldStore }()

	callbackCount := 0
	callback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callbackCount++
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer callback.Close()

	cfg := bridgeConfig{
		BotWxID:          "wechat_ui_bot",
		Timeout:          time.Second,
		AssistantSyncURL: callback.URL + "/api/v1/wechat-client/wechat_ui_bot/sync-message",
		InjectDedupeTTL:  time.Minute,
	}
	body := `{"from_wxid":"filehelper","content":"visible hello"}`
	for i, wantStatus := range []int{http.StatusOK, http.StatusConflict} {
		req := httptest.NewRequest(http.MethodPost, "/api/Operator/InjectCurrentLastText", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp := httptest.NewRecorder()
		injectCurrentLastTextHandler(cfg)(resp, req)
		if resp.Code != wantStatus {
			t.Fatalf("request %d status=%d body=%s", i+1, resp.Code, resp.Body.String())
		}
	}
	if callbackCount != 1 {
		t.Fatalf("callbackCount=%d", callbackCount)
	}
}

func TestPollCurrentLastTextHandlerInjectsOnlyWhenChanged(t *testing.T) {
	oldInjected := injectedMessages
	oldPolled := polledMessages
	oldReader := readVisibleText
	injectedMessages = newDedupeStore()
	polledMessages = newLastTextStore()
	visibleText := "visible one"
	readVisibleText = func(context.Context, bridgeConfig, readLastRequest) (string, string, error) {
		return visibleText, "", nil
	}
	defer func() {
		injectedMessages = oldInjected
		polledMessages = oldPolled
		readVisibleText = oldReader
	}()

	callbackCount := 0
	callback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callbackCount++
		var payload robot.ClientResponse[robot.SyncMessage]
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if got := *payload.Data.AddMsgs[0].Content.String; got != visibleText {
			http.Error(w, fmt.Sprintf("content=%q", got), http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer callback.Close()

	cfg := bridgeConfig{
		BotWxID:          "wechat_ui_bot",
		Timeout:          time.Second,
		AssistantSyncURL: callback.URL + "/api/v1/wechat-client/wechat_ui_bot/sync-message",
		InjectDedupeTTL:  time.Minute,
	}
	for i, wantChanged := range []bool{true, false} {
		req := httptest.NewRequest(http.MethodPost, "/api/Operator/PollCurrentLastText", strings.NewReader(`{}`))
		resp := httptest.NewRecorder()
		pollCurrentLastTextHandler(cfg)(resp, req)
		if resp.Code != http.StatusOK {
			t.Fatalf("request %d status=%d body=%s", i+1, resp.Code, resp.Body.String())
		}
		var body clientResponse
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		data := body.Data.(map[string]any)
		if data["changed"] != wantChanged {
			t.Fatalf("request %d changed=%v want %v", i+1, data["changed"], wantChanged)
		}
	}
	if callbackCount != 1 {
		t.Fatalf("callbackCount=%d", callbackCount)
	}

	visibleText = "visible two"
	req := httptest.NewRequest(http.MethodPost, "/api/Operator/PollCurrentLastText", strings.NewReader(`{}`))
	resp := httptest.NewRecorder()
	pollCurrentLastTextHandler(cfg)(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("changed text status=%d body=%s", resp.Code, resp.Body.String())
	}
	if callbackCount != 2 {
		t.Fatalf("callbackCount after change=%d", callbackCount)
	}
}

func TestPollCurrentLastTextHandlerCanObserveWithoutInjecting(t *testing.T) {
	oldPolled := polledMessages
	oldReader := readVisibleText
	polledMessages = newLastTextStore()
	readVisibleText = func(context.Context, bridgeConfig, readLastRequest) (string, string, error) {
		return "observe only", "", nil
	}
	defer func() {
		polledMessages = oldPolled
		readVisibleText = oldReader
	}()

	cfg := bridgeConfig{BotWxID: "wechat_ui_bot", Timeout: time.Second}
	req := httptest.NewRequest(http.MethodPost, "/api/Operator/PollCurrentLastText", strings.NewReader(`{"inject":false}`))
	resp := httptest.NewRecorder()
	pollCurrentLastTextHandler(cfg)(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestNormalizePollInterval(t *testing.T) {
	for seconds, want := range map[int]time.Duration{
		0:  60 * time.Second,
		1:  30 * time.Second,
		30: 30 * time.Second,
		90: 90 * time.Second,
	} {
		if got := normalizePollInterval(seconds); got != want {
			t.Fatalf("normalizePollInterval(%d)=%v, want %v", seconds, got, want)
		}
	}
}

func TestPollRunnerStartStopRunsImmediately(t *testing.T) {
	oldInjected := injectedMessages
	oldPolled := polledMessages
	oldReader := readVisibleText
	injectedMessages = newDedupeStore()
	polledMessages = newLastTextStore()
	readVisibleText = func(context.Context, bridgeConfig, readLastRequest) (string, string, error) {
		return "loop visible", "", nil
	}
	defer func() {
		injectedMessages = oldInjected
		polledMessages = oldPolled
		readVisibleText = oldReader
	}()

	var callbackCount int32
	callback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callbackCount, 1)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer callback.Close()

	runner := newPollRunner()
	cfg := bridgeConfig{
		BotWxID:          "wechat_ui_bot",
		Timeout:          time.Second,
		AssistantSyncURL: callback.URL + "/api/v1/wechat-client/wechat_ui_bot/sync-message",
		InjectDedupeTTL:  time.Minute,
	}
	if err := runner.Start(cfg, pollCurrentLastTextRequest{}, normalizePollInterval(1)); err != nil {
		t.Fatal(err)
	}
	defer runner.Stop()
	if err := runner.Start(cfg, pollCurrentLastTextRequest{}, normalizePollInterval(1)); err == nil {
		t.Fatal("expected duplicate start to fail")
	}
	waitFor(t, time.Second, func() bool {
		return atomic.LoadInt32(&callbackCount) == 1 && runner.Status()["run_count"].(int) >= 1
	})
	runner.Stop()
	if runner.Status()["running"].(bool) {
		t.Fatal("runner should be stopped")
	}
}

func TestPollRunnerSkipsDuringPause(t *testing.T) {
	oldReader := readVisibleText
	var readCount int32
	readVisibleText = func(context.Context, bridgeConfig, readLastRequest) (string, string, error) {
		atomic.AddInt32(&readCount, 1)
		return "should not read", "", nil
	}
	defer func() { readVisibleText = oldReader }()

	path := filepath.Join(t.TempDir(), "pause.json")
	if err := os.WriteFile(path, []byte(`{"paused":true,"reason":"test handoff"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := newPollRunner()
	cfg := bridgeConfig{
		BotWxID:           "wechat_ui_bot",
		Timeout:           time.Second,
		OperatorPauseFile: path,
	}
	if err := runner.Start(cfg, pollCurrentLastTextRequest{}, normalizePollInterval(1)); err != nil {
		t.Fatal(err)
	}
	defer runner.Stop()
	waitFor(t, time.Second, func() bool {
		return runner.Status()["run_count"].(int) >= 1
	})
	if got := atomic.LoadInt32(&readCount); got != 0 {
		t.Fatalf("readCount=%d", got)
	}
	state := runner.Status()["last_state"].(map[string]any)
	if state["skipped"] != true {
		t.Fatalf("last_state=%#v", state)
	}
}

func waitFor(t *testing.T, timeout time.Duration, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition was not met before timeout")
}
