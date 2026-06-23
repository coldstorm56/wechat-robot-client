package main

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"wechat-robot-client/model"
	"wechat-robot-client/pkg/robot"
)

type bridgeConfig struct {
	Addr                 string
	Python               string
	Script               string
	BotWxID              string
	BotName              string
	ContactAliases       map[string]string
	Timeout              time.Duration
	DryRun               bool
	SendCurrent          bool
	RequireVerify        bool
	ExpectChatTitle      string
	OperatorPauseWindows string
	OperatorPauseFile    string
	AssistantSyncURL     string
	InjectDedupeTTL      time.Duration
	OutgoingEchoTTL      time.Duration
	MaxPollErrors        int
}

type clientResponse struct {
	Success bool   `json:"Success"`
	Code    int    `json:"Code"`
	Message string `json:"Message"`
	Data    any    `json:"Data"`
}

type sendTextRequest struct {
	WxID    string `json:"Wxid"`
	Type    int    `json:"Type"`
	ToWxID  string `json:"ToWxid"`
	Content string `json:"Content"`
	At      string `json:"At"`
}

type contactDetailRequest struct {
	WxID     string `json:"Wxid"`
	ToWxIDs  string `json:"Towxids"`
	ChatRoom string `json:"ChatRoom"`
}

type readLastRequest struct {
	Contact string `json:"contact"`
	ToWxID  string `json:"to_wxid"`
}

type injectCurrentLastTextRequest struct {
	CallbackURL string `json:"callback_url"`
	WechatID    string `json:"wechat_id"`
	FromWxID    string `json:"from_wxid"`
	ToWxID      string `json:"to_wxid"`
	SenderWxID  string `json:"sender_wxid"`
	Contact     string `json:"contact"`
	Content     string `json:"content"`
	PushContent string `json:"push_content"`
	AtBot       bool   `json:"at_bot"`
	AtWxID      string `json:"at_wxid"`
	DedupeKey   string `json:"dedupe_key"`
	SkipDedupe  bool   `json:"skip_dedupe"`
}

type pollCurrentLastTextRequest struct {
	CallbackURL string `json:"callback_url"`
	WechatID    string `json:"wechat_id"`
	FromWxID    string `json:"from_wxid"`
	ToWxID      string `json:"to_wxid"`
	SenderWxID  string `json:"sender_wxid"`
	Contact     string `json:"contact"`
	AtBot       bool   `json:"at_bot"`
	AtWxID      string `json:"at_wxid"`
	Inject      *bool  `json:"inject"`
}

type pollLoopStartRequest struct {
	CallbackURL     string `json:"callback_url"`
	WechatID        string `json:"wechat_id"`
	FromWxID        string `json:"from_wxid"`
	ToWxID          string `json:"to_wxid"`
	SenderWxID      string `json:"sender_wxid"`
	Contact         string `json:"contact"`
	AtBot           bool   `json:"at_bot"`
	AtWxID          string `json:"at_wxid"`
	Inject          *bool  `json:"inject"`
	PrimeOnStart    *bool  `json:"prime_on_start"`
	IntervalSeconds int    `json:"interval_seconds"`
	MaxErrors       *int   `json:"max_errors"`
}

type syncMessageBuildOptions struct {
	SenderWxID string
	AtWxID     string
}

type scriptOutput struct {
	OK             bool   `json:"ok"`
	Error          string `json:"error"`
	LastText       string `json:"last_text"`
	DeliveryStatus string `json:"delivery_status"`
	Status         any    `json:"status"`
}

type pauseState struct {
	Paused bool   `json:"paused"`
	Source string `json:"source,omitempty"`
	Reason string `json:"reason,omitempty"`
	Until  string `json:"until,omitempty"`
}

type pauseFileConfig struct {
	Paused     bool   `json:"paused"`
	PauseUntil string `json:"pause_until"`
	Until      string `json:"until"`
	Reason     string `json:"reason"`
	Windows    string `json:"windows"`
}

type operatorPauseRequest struct {
	Paused     *bool  `json:"paused"`
	PauseUntil string `json:"pause_until"`
	Until      string `json:"until"`
	Reason     string `json:"reason"`
	Minutes    int    `json:"minutes"`
	Windows    string `json:"windows"`
}

var (
	injectedMessages = newDedupeStore()
	polledMessages   = newLastTextStore()
	outgoingMessages = newRecentTextStore()
	readVisibleText  = readCurrentLastText
	backgroundPoller = newPollRunner()
)

func main() {
	cfg := loadConfig()
	if err := ensureLoopbackAddr(cfg.Addr); err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", healthHandler(cfg))
	mux.HandleFunc("/api"+robot.LoginGetCacheInfo, getCacheInfoHandler(cfg))
	mux.HandleFunc("/api"+robot.UserGetContactProfile, getProfileHandler(cfg))
	mux.HandleFunc("/api"+robot.FriendGetContactDetail, getContactDetailHandler(cfg))
	mux.HandleFunc("/api"+robot.MsgSendTxt, sendTextHandler(cfg))
	mux.HandleFunc("/api/Msg/CurrentLastText", readLastTextHandler(cfg))
	mux.HandleFunc("/api/Operator/PauseStatus", pauseStatusHandler(cfg))
	mux.HandleFunc("/api/Operator/PauseSet", pauseSetHandler(cfg))
	mux.HandleFunc("/api/Operator/PauseClear", pauseClearHandler(cfg))
	mux.HandleFunc("/api/Operator/UiStatus", uiStatusHandler(cfg))
	mux.HandleFunc("/api/Operator/InjectCurrentLastText", injectCurrentLastTextHandler(cfg))
	mux.HandleFunc("/api/Operator/PollCurrentLastText", pollCurrentLastTextHandler(cfg))
	mux.HandleFunc("/api/Operator/PollStart", pollStartHandler(cfg, backgroundPoller))
	mux.HandleFunc("/api/Operator/PollStop", pollStopHandler(backgroundPoller))
	mux.HandleFunc("/api/Operator/PollStatus", pollStatusHandler(backgroundPoller))

	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Printf("WeChat UI bridge listening on http://%s", cfg.Addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func loadConfig() bridgeConfig {
	addr := flag.String("addr", envString("WECHAT_UI_BRIDGE_ADDR", "127.0.0.1:3021"), "HTTP listen address")
	python := flag.String("python", envString("WECHAT_UI_PYTHON", "python"), "Python executable")
	script := flag.String("script", envString("WECHAT_UI_SCRIPT", filepath.Join("scripts", "wechat_ui_smoke.py")), "WeChat UI smoke script path")
	botWxID := flag.String("bot-wxid", envString("WECHAT_UI_BOT_WXID", "wechat_ui_bot"), "pseudo wxid used by the compatibility bridge")
	botName := flag.String("bot-name", envString("WECHAT_UI_BOT_NAME", "此刻正佳"), "bot display name")
	aliases := flag.String("contact-aliases", envString("WECHAT_UI_CONTACT_ALIASES", "filehelper=文件传输助手"), "comma-separated wxid=display-name contact aliases")
	timeoutSeconds := flag.Int("timeout", envInt("WECHAT_UI_TIMEOUT", 12), "script timeout seconds")
	dryRun := flag.Bool("dry-run", envBool("WECHAT_UI_DRY_RUN", false), "return protocol-shaped success without touching WeChat UI")
	sendCurrent := flag.Bool("send-current", envBool("WECHAT_UI_SEND_CURRENT_CHAT", true), "send to the currently open WeChat conversation instead of navigating by contact")
	requireVerify := flag.Bool("require-verify", envBool("WECHAT_UI_SEND_REQUIRE_VERIFY", true), "require visible message verification before reporting send success")
	expectChatTitle := flag.String("expect-chat-title", envString("WECHAT_UI_EXPECT_CHAT_TITLE", ""), "optional current chat title that must be visible before send/read automation runs")
	pauseWindows := flag.String("operator-pause-windows", envString("WECHAT_UI_OPERATOR_PAUSE_WINDOWS", ""), "daily operator takeover windows, e.g. 09:00-12:00,18:30-20:00")
	pauseFile := flag.String("operator-pause-file", envString("WECHAT_UI_OPERATOR_PAUSE_FILE", filepath.Join(os.TempDir(), "wechat-ui-operator-pause.json")), "optional local file for dynamic operator takeover pauses")
	assistantSyncURL := flag.String("assistant-sync-url", envString("WECHAT_UI_ASSISTANT_SYNC_URL", ""), "optional loopback main-service sync-message callback URL")
	injectDedupeSeconds := flag.Int("inject-dedupe-seconds", envInt("WECHAT_UI_INJECT_DEDUPE_TTL_SECONDS", 120), "suppress duplicate injected visible messages for this many seconds")
	outgoingEchoSeconds := flag.Int("outgoing-echo-seconds", envInt("WECHAT_UI_OUTGOING_ECHO_TTL_SECONDS", 300), "suppress poll injection for recently sent visible text for this many seconds")
	maxPollErrors := flag.Int("poll-max-errors", envInt("WECHAT_UI_POLL_MAX_ERRORS", 3), "stop the poll loop after this many consecutive errors; 0 disables the fuse")
	flag.Parse()
	maxPollErrorsValue := *maxPollErrors
	if maxPollErrorsValue < 0 {
		maxPollErrorsValue = 0
	}

	return bridgeConfig{
		Addr:                 strings.TrimSpace(*addr),
		Python:               strings.TrimSpace(*python),
		Script:               strings.TrimSpace(*script),
		BotWxID:              strings.TrimSpace(*botWxID),
		BotName:              strings.TrimSpace(*botName),
		ContactAliases:       parseAliases(*aliases),
		Timeout:              time.Duration(*timeoutSeconds) * time.Second,
		DryRun:               *dryRun,
		SendCurrent:          *sendCurrent,
		RequireVerify:        *requireVerify,
		ExpectChatTitle:      strings.TrimSpace(*expectChatTitle),
		OperatorPauseWindows: strings.TrimSpace(*pauseWindows),
		OperatorPauseFile:    strings.TrimSpace(*pauseFile),
		AssistantSyncURL:     strings.TrimSpace(*assistantSyncURL),
		InjectDedupeTTL:      time.Duration(*injectDedupeSeconds) * time.Second,
		OutgoingEchoTTL:      time.Duration(*outgoingEchoSeconds) * time.Second,
		MaxPollErrors:        maxPollErrorsValue,
	}
}

func healthHandler(cfg bridgeConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		pause := currentPauseState(cfg, time.Now())
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":                     true,
			"status":                 "live",
			"bridge":                 "wechat-ui",
			"bot_wxid":               cfg.BotWxID,
			"dry_run":                cfg.DryRun,
			"send_current_chat":      cfg.SendCurrent,
			"require_verify":         cfg.RequireVerify,
			"expect_chat_title":      cfg.ExpectChatTitle,
			"operator_pause":         pause.Paused,
			"operator_pause_state":   pause,
			"operator_pause_file":    cfg.OperatorPauseFile,
			"operator_pause_windows": cfg.OperatorPauseWindows,
			"assistant_sync_url":     cfg.AssistantSyncURL,
			"inject_dedupe_seconds":  int(cfg.InjectDedupeTTL.Seconds()),
			"outgoing_echo_seconds":  int(cfg.OutgoingEchoTTL.Seconds()),
			"poll_max_errors":        cfg.MaxPollErrors,
			"poll_loop":              backgroundPoller.Status(),
		})
	}
}

func pauseStatusHandler(cfg bridgeConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePost(w, r) {
			return
		}
		writeClientOK(w, currentPauseState(cfg, time.Now()))
	}
}

func pauseSetHandler(cfg bridgeConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePost(w, r) {
			return
		}
		if strings.TrimSpace(cfg.OperatorPauseFile) == "" {
			writeClientError(w, http.StatusBadRequest, "operator pause file is not configured")
			return
		}
		var req operatorPauseRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			writeClientError(w, http.StatusBadRequest, fmt.Sprintf("decode request: %v", err))
			return
		}
		content, err := buildPauseFileConfig(req, time.Now())
		if err != nil {
			writeClientError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := writePauseFile(cfg.OperatorPauseFile, content); err != nil {
			writeClientError(w, http.StatusInternalServerError, fmt.Sprintf("write pause file: %v", err))
			return
		}
		writeClientOK(w, currentPauseState(cfg, time.Now()))
	}
}

func pauseClearHandler(cfg bridgeConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePost(w, r) {
			return
		}
		if strings.TrimSpace(cfg.OperatorPauseFile) == "" {
			writeClientError(w, http.StatusBadRequest, "operator pause file is not configured")
			return
		}
		if err := os.Remove(cfg.OperatorPauseFile); err != nil && !errors.Is(err, os.ErrNotExist) {
			writeClientError(w, http.StatusInternalServerError, fmt.Sprintf("clear pause file: %v", err))
			return
		}
		writeClientOK(w, currentPauseState(cfg, time.Now()))
	}
}

func uiStatusHandler(cfg bridgeConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePost(w, r) {
			return
		}
		if !allowAutomationNow(w, cfg) {
			return
		}
		args := []string{"status"}
		if cfg.ExpectChatTitle != "" {
			args = append(args, "--expect-title", cfg.ExpectChatTitle)
		}
		output, err := runScript(r.Context(), cfg, args...)
		if err != nil {
			writeClientError(w, http.StatusBadGateway, err.Error())
			return
		}
		writeClientOK(w, normalizeUIStatus(output.Status))
	}
}

func getCacheInfoHandler(cfg bridgeConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePost(w, r) {
			return
		}
		writeClientOK(w, map[string]any{
			"wxid":       cfg.BotWxID,
			"Wxid":       cfg.BotWxID,
			"nickName":   cfg.BotName,
			"deviceName": "Windows WeChat UI",
		})
	}
}

func getProfileHandler(cfg bridgeConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePost(w, r) {
			return
		}
		writeClientOK(w, map[string]any{
			"baseResponse": map[string]any{"ret": 0},
			"userInfo": map[string]any{
				"UserName": skString(cfg.BotWxID),
				"NickName": skString(cfg.BotName),
				"Alias":    cfg.BotWxID,
			},
			"userInfoExt": map[string]any{},
		})
	}
}

func getContactDetailHandler(cfg bridgeConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePost(w, r) {
			return
		}
		var req contactDetailRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			writeClientError(w, http.StatusBadRequest, fmt.Sprintf("decode request: %v", err))
			return
		}
		wxids := splitComma(req.ToWxIDs)
		contacts := make([]map[string]any, 0, len(wxids))
		for _, wxid := range wxids {
			name := contactName(cfg, wxid)
			contacts = append(contacts, map[string]any{
				"UserName": skString(wxid),
				"NickName": skString(name),
				"Remark":   skString(name),
			})
		}
		writeClientOK(w, map[string]any{
			"BaseResponse": map[string]any{"ret": 0},
			"ContactCount": len(contacts),
			"ContactList":  contacts,
			"Ret":          make([]int, len(contacts)),
		})
	}
}

func sendTextHandler(cfg bridgeConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePost(w, r) {
			return
		}
		if !allowAutomationNow(w, cfg) {
			return
		}
		var req sendTextRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeClientError(w, http.StatusBadRequest, fmt.Sprintf("decode request: %v", err))
			return
		}
		if strings.TrimSpace(req.ToWxID) == "" {
			writeClientError(w, http.StatusBadRequest, "ToWxid is required")
			return
		}
		if strings.TrimSpace(req.Content) == "" {
			writeClientError(w, http.StatusBadRequest, "Content is required")
			return
		}

		contact := contactName(cfg, req.ToWxID)
		deliveryStatus := "dry_run"
		if !cfg.DryRun {
			args := []string{"send", "--message", req.Content}
			if cfg.ExpectChatTitle != "" {
				args = append(args, "--expect-title", cfg.ExpectChatTitle)
			}
			if !cfg.SendCurrent {
				args = append(args, "--contact", contact)
			}
			if !cfg.RequireVerify {
				args = append(args, "--no-verify")
			}
			output, err := runScript(r.Context(), cfg, args...)
			if err != nil {
				writeClientError(w, http.StatusBadGateway, err.Error())
				return
			}
			deliveryStatus = output.DeliveryStatus
			if cfg.RequireVerify && deliveryStatus != "visible_verified" {
				writeClientError(w, http.StatusBadGateway, "visible delivery was not verified")
				return
			}
		}

		now := time.Now().Unix()
		rememberOutgoingMessage(cfg, req.ToWxID, req.Content, time.Now())
		writeClientOK(w, map[string]any{
			"BaseResponse": map[string]any{"ret": 0},
			"List": []map[string]any{
				{
					"Ret":         0,
					"ToUsetName":  skString(req.ToWxID),
					"MsgId":       now,
					"ClientMsgid": now,
					"Createtime":  now,
					"servertime":  now,
					"Type":        1,
					"NewMsgId":    now,
				},
			},
			"Count":          1,
			"NoKnow":         0,
			"DeliveryStatus": deliveryStatus,
		})
	}
}

func readLastTextHandler(cfg bridgeConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePost(w, r) {
			return
		}
		if !allowAutomationNow(w, cfg) {
			return
		}
		var req readLastRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			writeClientError(w, http.StatusBadRequest, fmt.Sprintf("decode request: %v", err))
			return
		}
		last, contact, err := readVisibleText(r.Context(), cfg, req)
		if err != nil {
			writeClientError(w, http.StatusBadGateway, err.Error())
			return
		}
		writeClientOK(w, map[string]any{
			"contact":   contact,
			"last_text": last,
		})
	}
}

func injectCurrentLastTextHandler(cfg bridgeConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePost(w, r) {
			return
		}
		if !allowAutomationNow(w, cfg) {
			return
		}
		var req injectCurrentLastTextRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			writeClientError(w, http.StatusBadRequest, fmt.Sprintf("decode request: %v", err))
			return
		}
		callbackURL := firstNonEmpty(req.CallbackURL, cfg.AssistantSyncURL)
		if callbackURL == "" {
			writeClientError(w, http.StatusBadRequest, "callback_url or WECHAT_UI_ASSISTANT_SYNC_URL is required")
			return
		}
		if err := ensureLoopbackURL(callbackURL); err != nil {
			writeClientError(w, http.StatusBadRequest, err.Error())
			return
		}
		wechatID := firstNonEmpty(req.WechatID, cfg.BotWxID)
		toWxID := firstNonEmpty(req.ToWxID, cfg.BotWxID)
		fromWxID := firstNonEmpty(req.FromWxID, req.Contact, "filehelper")
		atWxID := resolveAtWxID(req.AtWxID, wechatID, req.AtBot)
		content := strings.TrimSpace(req.Content)
		contact := strings.TrimSpace(req.Contact)
		if content == "" {
			var err error
			content, contact, err = readVisibleText(r.Context(), cfg, readLastRequest{
				Contact: req.Contact,
				ToWxID:  fromWxID,
			})
			if err != nil {
				writeClientError(w, http.StatusBadGateway, err.Error())
				return
			}
			if contact != "" && strings.TrimSpace(req.FromWxID) == "" {
				fromWxID = contact
			}
		}
		content = strings.TrimSpace(content)
		if content == "" {
			writeClientError(w, http.StatusBadGateway, "current visible last text is empty")
			return
		}
		now := time.Now()
		dedupeKey := firstNonEmpty(req.DedupeKey, buildInjectionDedupeKey(wechatID, fromWxID, toWxID, content))
		if !req.SkipDedupe && cfg.InjectDedupeTTL > 0 && !injectedMessages.TryReserve(dedupeKey, now, cfg.InjectDedupeTTL) {
			writeDuplicateInjection(w, dedupeKey, cfg.InjectDedupeTTL)
			return
		}
		payload, msgID := buildSyncMessageCallbackPayloadWithOptions(wechatID, fromWxID, toWxID, content, req.PushContent, now, syncMessageBuildOptions{
			SenderWxID: req.SenderWxID,
			AtWxID:     atWxID,
		})
		result, err := postSyncMessage(r.Context(), cfg, callbackURL, payload)
		if err != nil {
			if !req.SkipDedupe && cfg.InjectDedupeTTL > 0 {
				injectedMessages.Forget(dedupeKey)
			}
			writeClientError(w, http.StatusBadGateway, err.Error())
			return
		}
		writeClientOK(w, map[string]any{
			"callback_url": callbackURL,
			"wechat_id":    wechatID,
			"from_wxid":    fromWxID,
			"to_wxid":      toWxID,
			"content":      content,
			"msg_id":       msgID,
			"dedupe_key":   dedupeKey,
			"contact":      contact,
			"status_code":  result.statusCode,
			"response":     result.body,
		})
	}
}

func pollCurrentLastTextHandler(cfg bridgeConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePost(w, r) {
			return
		}
		if !allowAutomationNow(w, cfg) {
			return
		}
		var req pollCurrentLastTextRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			writeClientError(w, http.StatusBadRequest, fmt.Sprintf("decode request: %v", err))
			return
		}
		result, status, err := pollCurrentLastText(r.Context(), cfg, req)
		if err != nil {
			writeClientError(w, status, err.Error())
			return
		}
		if status == http.StatusConflict {
			writeJSON(w, status, clientResponse{
				Success: false,
				Code:    -3,
				Message: "duplicate visible text injection suppressed",
				Data:    result,
			})
			return
		}
		writeClientOK(w, result)
	}
}

func pollCurrentLastText(ctx context.Context, cfg bridgeConfig, req pollCurrentLastTextRequest) (map[string]any, int, error) {
	result := map[string]any{}
	status := http.StatusOK

	wechatID := firstNonEmpty(req.WechatID, cfg.BotWxID)
	toWxID := firstNonEmpty(req.ToWxID, cfg.BotWxID)
	fromWxID := firstNonEmpty(req.FromWxID, req.Contact, "filehelper")
	atWxID := resolveAtWxID(req.AtWxID, wechatID, req.AtBot)
	content, contact, err := readVisibleText(ctx, cfg, readLastRequest{
		Contact: req.Contact,
		ToWxID:  fromWxID,
	})
	if err != nil {
		return result, http.StatusBadGateway, err
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return result, http.StatusBadGateway, errors.New("current visible last text is empty")
	}
	if contact != "" && strings.TrimSpace(req.FromWxID) == "" {
		fromWxID = contact
	}
	streamKey := buildPollStreamKey(wechatID, fromWxID, toWxID)
	base := map[string]any{
		"wechat_id":  wechatID,
		"from_wxid":  fromWxID,
		"to_wxid":    toWxID,
		"content":    content,
		"stream_key": streamKey,
		"contact":    contact,
	}
	if !polledMessages.Changed(streamKey, content) {
		base["changed"] = false
		base["injected"] = false
		return base, status, nil
	}
	if outgoingMessages.Seen(buildOutgoingEchoKey(wechatID, fromWxID, toWxID, content), time.Now()) {
		polledMessages.Remember(streamKey, content)
		base["changed"] = true
		base["injected"] = false
		base["skipped"] = true
		base["reason"] = "outgoing echo"
		return base, status, nil
	}
	shouldInject := true
	if req.Inject != nil {
		shouldInject = *req.Inject
	}
	if !shouldInject {
		polledMessages.Remember(streamKey, content)
		base["changed"] = true
		base["injected"] = false
		return base, status, nil
	}
	callbackURL := firstNonEmpty(req.CallbackURL, cfg.AssistantSyncURL)
	if callbackURL == "" {
		return result, http.StatusBadRequest, errors.New("callback_url or WECHAT_UI_ASSISTANT_SYNC_URL is required")
	}
	if err := ensureLoopbackURL(callbackURL); err != nil {
		return result, http.StatusBadRequest, err
	}
	now := time.Now()
	dedupeKey := buildInjectionDedupeKey(wechatID, fromWxID, toWxID, content)
	if cfg.InjectDedupeTTL > 0 && !injectedMessages.TryReserve(dedupeKey, now, cfg.InjectDedupeTTL) {
		base["changed"] = true
		base["injected"] = false
		base["duplicate"] = true
		base["dedupe_key"] = dedupeKey
		base["dedupe_seconds"] = int(cfg.InjectDedupeTTL.Seconds())
		return base, http.StatusConflict, nil
	}
	payload, msgID := buildSyncMessageCallbackPayloadWithOptions(wechatID, fromWxID, toWxID, content, "", now, syncMessageBuildOptions{
		SenderWxID: req.SenderWxID,
		AtWxID:     atWxID,
	})
	postResult, err := postSyncMessage(ctx, cfg, callbackURL, payload)
	if err != nil {
		if cfg.InjectDedupeTTL > 0 {
			injectedMessages.Forget(dedupeKey)
		}
		return result, http.StatusBadGateway, err
	}
	polledMessages.Remember(streamKey, content)
	base["changed"] = true
	base["injected"] = true
	base["callback_url"] = callbackURL
	base["msg_id"] = msgID
	base["dedupe_key"] = dedupeKey
	base["status_code"] = postResult.statusCode
	base["response"] = postResult.body
	return base, status, nil
}

func pollStartHandler(cfg bridgeConfig, runner *pollRunner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePost(w, r) {
			return
		}
		var req pollLoopStartRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			writeClientError(w, http.StatusBadRequest, fmt.Sprintf("decode request: %v", err))
			return
		}
		interval := normalizePollInterval(req.IntervalSeconds)
		pollReq := pollCurrentLastTextRequest{
			CallbackURL: req.CallbackURL,
			WechatID:    req.WechatID,
			FromWxID:    req.FromWxID,
			ToWxID:      req.ToWxID,
			SenderWxID:  req.SenderWxID,
			Contact:     req.Contact,
			AtBot:       req.AtBot,
			AtWxID:      req.AtWxID,
			Inject:      req.Inject,
		}
		prime := true
		if req.PrimeOnStart != nil {
			prime = *req.PrimeOnStart
		}
		if err := validatePollStartCallback(cfg, pollReq); err != nil {
			writeClientError(w, http.StatusBadRequest, err.Error())
			return
		}
		maxErrors := cfg.MaxPollErrors
		if req.MaxErrors != nil {
			maxErrors = *req.MaxErrors
		}
		if err := runner.Start(cfg, pollReq, interval, prime, maxErrors); err != nil {
			writeClientError(w, http.StatusConflict, err.Error())
			return
		}
		writeClientOK(w, runner.Status())
	}
}

func pollStopHandler(runner *pollRunner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePost(w, r) {
			return
		}
		runner.Stop()
		writeClientOK(w, runner.Status())
	}
}

func pollStatusHandler(runner *pollRunner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requirePost(w, r) {
			return
		}
		writeClientOK(w, runner.Status())
	}
}

func readCurrentLastText(ctx context.Context, cfg bridgeConfig, req readLastRequest) (string, string, error) {
	contact := ""
	if !cfg.SendCurrent {
		contact = strings.TrimSpace(req.Contact)
		if contact == "" && strings.TrimSpace(req.ToWxID) != "" {
			contact = contactName(cfg, req.ToWxID)
		}
	}
	args := []string{"read-last"}
	if cfg.ExpectChatTitle != "" {
		args = append(args, "--expect-title", cfg.ExpectChatTitle)
	}
	if contact != "" {
		args = append(args, "--contact", contact)
	}
	output, err := runScript(ctx, cfg, args...)
	if err != nil {
		return "", contact, err
	}
	return output.LastText, contact, nil
}

type syncPostResult struct {
	statusCode int
	body       string
}

type dedupeStore struct {
	mu   sync.Mutex
	seen map[string]time.Time
}

type lastTextStore struct {
	mu     sync.Mutex
	values map[string]string
}

type recentTextStore struct {
	mu      sync.Mutex
	expires map[string]time.Time
}

type pollRunner struct {
	mu         sync.Mutex
	cancel     context.CancelFunc
	done       chan struct{}
	running    bool
	interval   time.Duration
	request    pollCurrentLastTextRequest
	prime      bool
	lastRun    string
	lastError  string
	lastState  map[string]any
	runCount   int
	maxErrors  int
	errorCount int
	stopReason string
}

func newDedupeStore() *dedupeStore {
	return &dedupeStore{seen: map[string]time.Time{}}
}

func newLastTextStore() *lastTextStore {
	return &lastTextStore{values: map[string]string{}}
}

func newRecentTextStore() *recentTextStore {
	return &recentTextStore{expires: map[string]time.Time{}}
}

func newPollRunner() *pollRunner {
	return &pollRunner{}
}

func normalizePollInterval(seconds int) time.Duration {
	if seconds <= 0 {
		return 60 * time.Second
	}
	interval := time.Duration(seconds) * time.Second
	if interval < 30*time.Second {
		return 30 * time.Second
	}
	return interval
}

func validatePollStartCallback(cfg bridgeConfig, req pollCurrentLastTextRequest) error {
	shouldInject := true
	if req.Inject != nil {
		shouldInject = *req.Inject
	}
	if !shouldInject {
		return nil
	}
	callbackURL := firstNonEmpty(req.CallbackURL, cfg.AssistantSyncURL)
	if callbackURL == "" {
		return errors.New("callback_url or WECHAT_UI_ASSISTANT_SYNC_URL is required before starting an injecting poll loop")
	}
	if err := ensureLoopbackURL(callbackURL); err != nil {
		return err
	}
	return nil
}

func pollRequestWithInject(req pollCurrentLastTextRequest, inject bool) pollCurrentLastTextRequest {
	req.Inject = &inject
	return req
}

func (p *pollRunner) Start(cfg bridgeConfig, req pollCurrentLastTextRequest, interval time.Duration, prime bool, maxErrors int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.running {
		return errors.New("poll loop already running")
	}
	if maxErrors < 0 {
		maxErrors = 0
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	p.cancel = cancel
	p.done = done
	p.running = true
	p.interval = interval
	p.request = req
	p.prime = prime
	p.lastError = ""
	p.lastState = nil
	p.runCount = 0
	p.maxErrors = maxErrors
	p.errorCount = 0
	p.stopReason = ""
	go func() {
		defer close(done)
		p.loop(ctx, cfg, req, interval, prime)
	}()
	return nil
}

func (p *pollRunner) Stop() {
	done := p.stopWithReason("")
	if done != nil {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
	}
}

func (p *pollRunner) stopWithReason(reason string) chan struct{} {
	p.mu.Lock()
	cancel := p.cancel
	done := p.done
	p.cancel = nil
	p.done = nil
	p.running = false
	if strings.TrimSpace(reason) != "" {
		p.stopReason = strings.TrimSpace(reason)
	}
	p.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return done
}

func (p *pollRunner) Status() map[string]any {
	p.mu.Lock()
	defer p.mu.Unlock()
	return map[string]any{
		"running":          p.running,
		"interval_seconds": int(p.interval.Seconds()),
		"request":          p.request,
		"prime_on_start":   p.prime,
		"last_run":         p.lastRun,
		"last_error":       p.lastError,
		"last_state":       p.lastState,
		"run_count":        p.runCount,
		"max_errors":       p.maxErrors,
		"error_count":      p.errorCount,
		"stop_reason":      p.stopReason,
	}
}

func (p *pollRunner) loop(ctx context.Context, cfg bridgeConfig, req pollCurrentLastTextRequest, interval time.Duration, prime bool) {
	if prime {
		p.runOnce(ctx, cfg, pollRequestWithInject(req, false), true)
	} else {
		p.runOnce(ctx, cfg, req, false)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.runOnce(ctx, cfg, req, false)
		}
	}
}

func (p *pollRunner) runOnce(parent context.Context, cfg bridgeConfig, req pollCurrentLastTextRequest, prime bool) {
	now := time.Now()
	if state := currentPauseState(cfg, now); state.Paused {
		p.recordPollResult(now, map[string]any{
			"skipped": true,
			"reason":  "operator pause active",
			"pause":   state,
			"prime":   prime,
		}, "")
		return
	}
	ctx, cancel := context.WithTimeout(parent, cfg.Timeout+2*time.Second)
	defer cancel()
	result, status, err := pollCurrentLastText(ctx, cfg, req)
	if err != nil {
		if p.recordPollResult(now, map[string]any{"status_code": status, "prime": prime}, err.Error()) {
			p.stopWithReason("max consecutive poll errors reached")
		}
		return
	}
	result["status_code"] = status
	result["prime"] = prime
	p.recordPollResult(now, result, "")
}

func (p *pollRunner) recordPollResult(now time.Time, state map[string]any, err string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lastRun = now.Format(time.RFC3339)
	p.lastState = state
	p.lastError = err
	p.runCount++
	if err == "" {
		p.errorCount = 0
		return false
	}
	p.errorCount++
	return p.maxErrors > 0 && p.errorCount >= p.maxErrors
}

func (s *dedupeStore) TryReserve(key string, now time.Time, ttl time.Duration) bool {
	if key == "" || ttl <= 0 {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for seenKey, expiresAt := range s.seen {
		if !expiresAt.After(now) {
			delete(s.seen, seenKey)
		}
	}
	if expiresAt, ok := s.seen[key]; ok && expiresAt.After(now) {
		return false
	}
	s.seen[key] = now.Add(ttl)
	return true
}

func (s *dedupeStore) Forget(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.seen, key)
}

func (s *lastTextStore) Changed(key, value string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.values[key] != value
}

func (s *lastTextStore) Remember(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[key] = value
}

func (s *recentTextStore) Remember(key string, now time.Time, ttl time.Duration) {
	if key == "" || ttl <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expires[key] = now.Add(ttl)
}

func (s *recentTextStore) Seen(key string, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for seenKey, expiresAt := range s.expires {
		if !expiresAt.After(now) {
			delete(s.expires, seenKey)
		}
	}
	expiresAt, ok := s.expires[key]
	return ok && expiresAt.After(now)
}

func rememberOutgoingMessage(cfg bridgeConfig, toWxID, content string, now time.Time) {
	content = strings.TrimSpace(content)
	if cfg.OutgoingEchoTTL <= 0 || content == "" {
		return
	}
	for _, fromWxID := range uniqueNonEmpty(toWxID, contactName(cfg, toWxID)) {
		outgoingMessages.Remember(
			buildOutgoingEchoKey(cfg.BotWxID, fromWxID, cfg.BotWxID, content),
			now,
			cfg.OutgoingEchoTTL,
		)
	}
}

func buildPollStreamKey(wechatID, fromWxID, toWxID string) string {
	return strings.Join([]string{
		strings.ToLower(strings.TrimSpace(wechatID)),
		strings.ToLower(strings.TrimSpace(fromWxID)),
		strings.ToLower(strings.TrimSpace(toWxID)),
	}, "\x00")
}

func buildOutgoingEchoKey(wechatID, fromWxID, toWxID, content string) string {
	return buildInjectionDedupeKey(wechatID, fromWxID, toWxID, content)
}

func buildInjectionDedupeKey(wechatID, fromWxID, toWxID, content string) string {
	return strings.Join([]string{
		strings.ToLower(strings.TrimSpace(wechatID)),
		strings.ToLower(strings.TrimSpace(fromWxID)),
		strings.ToLower(strings.TrimSpace(toWxID)),
		strings.TrimSpace(content),
	}, "\x00")
}

func writeDuplicateInjection(w http.ResponseWriter, dedupeKey string, ttl time.Duration) {
	writeJSON(w, http.StatusConflict, clientResponse{
		Success: false,
		Code:    -3,
		Message: "duplicate visible text injection suppressed",
		Data: map[string]any{
			"dedupe_key":     dedupeKey,
			"dedupe_seconds": int(ttl.Seconds()),
		},
	})
}

func postSyncMessage(parent context.Context, cfg bridgeConfig, callbackURL string, payload clientResponse) (syncPostResult, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return syncPostResult{}, fmt.Errorf("marshal sync-message callback: %w", err)
	}
	ctx, cancel := context.WithTimeout(parent, cfg.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, callbackURL, bytes.NewReader(body))
	if err != nil {
		return syncPostResult{}, fmt.Errorf("create sync-message callback request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return syncPostResult{}, fmt.Errorf("post sync-message callback: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	result := syncPostResult{statusCode: resp.StatusCode, body: strings.TrimSpace(string(raw))}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return result, fmt.Errorf("sync-message callback returned HTTP %d: %s", resp.StatusCode, result.body)
	}
	return result, nil
}

func buildSyncMessageCallbackPayload(wechatID, fromWxID, toWxID, content, pushContent string, now time.Time) (clientResponse, int64) {
	return buildSyncMessageCallbackPayloadWithOptions(wechatID, fromWxID, toWxID, content, pushContent, now, syncMessageBuildOptions{})
}

func buildSyncMessageCallbackPayloadWithOptions(wechatID, fromWxID, toWxID, content, pushContent string, now time.Time, opts syncMessageBuildOptions) (clientResponse, int64) {
	msgID := now.UnixNano() / int64(time.Millisecond)
	if strings.TrimSpace(pushContent) == "" {
		pushContent = content
	}
	syncContent := buildSyncMessageContent(fromWxID, opts.SenderWxID, content)
	msgSource := buildMsgSource(opts.AtWxID)
	return clientResponse{
		Success: true,
		Code:    0,
		Message: "ok",
		Data: robot.SyncMessage{
			AddMsgs: []robot.Message{
				{
					MsgId:        msgID,
					NewMsgId:     msgID,
					FromUserName: robotString(fromWxID),
					ToUserName:   robotString(toWxID),
					Content:      robotString(syncContent),
					CreateTime:   now.Unix(),
					MsgType:      model.MsgTypeText,
					Status:       3,
					PushContent:  pushContent,
					MsgSource:    msgSource,
				},
			},
			Status:  1,
			Time:    int(now.Unix()),
			Remarks: fmt.Sprintf("wechat-ui injected for %s", wechatID),
		},
	}, msgID
}

func buildSyncMessageContent(fromWxID, senderWxID, content string) string {
	content = strings.TrimSpace(content)
	senderWxID = strings.TrimSpace(senderWxID)
	if strings.HasSuffix(strings.TrimSpace(fromWxID), "@chatroom") && senderWxID != "" {
		return senderWxID + ":\n" + content
	}
	return content
}

func buildMsgSource(atWxID string) string {
	atWxID = strings.TrimSpace(atWxID)
	if atWxID == "" {
		return ""
	}
	return "<msgsource><atuserlist>" + xmlEscapeString(atWxID) + "</atuserlist></msgsource>"
}

func resolveAtWxID(raw, botWxID string, atBot bool) string {
	if value := strings.TrimSpace(raw); value != "" {
		return value
	}
	if atBot {
		return strings.TrimSpace(botWxID)
	}
	return ""
}

func xmlEscapeString(value string) string {
	var buf bytes.Buffer
	if err := xml.EscapeText(&buf, []byte(value)); err != nil {
		return value
	}
	return buf.String()
}

func runScript(parent context.Context, cfg bridgeConfig, args ...string) (scriptOutput, error) {
	ctx, cancel := context.WithTimeout(parent, cfg.Timeout+2*time.Second)
	defer cancel()

	fullArgs := []string{cfg.Script, "--timeout", strconv.Itoa(int(cfg.Timeout.Seconds()))}
	fullArgs = append(fullArgs, args...)
	cmd := exec.CommandContext(ctx, cfg.Python, fullArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(output))
		if detail == "" {
			detail = err.Error()
		}
		return scriptOutput{}, fmt.Errorf("wechat UI script failed: %s", detail)
	}
	var parsed scriptOutput
	if err := json.Unmarshal(output, &parsed); err != nil {
		return scriptOutput{}, fmt.Errorf("parse wechat UI script output: %w: %s", err, strings.TrimSpace(string(output)))
	}
	if !parsed.OK {
		if parsed.Error == "" {
			parsed.Error = "unknown UI automation failure"
		}
		return parsed, errors.New(parsed.Error)
	}
	return parsed, nil
}

func normalizeUIStatus(status any) map[string]any {
	result := map[string]any{
		"blocked": false,
		"usable":  true,
	}
	statusMap, ok := status.(map[string]any)
	if !ok {
		result["status"] = status
		return result
	}
	for key, value := range statusMap {
		result[key] = value
	}

	if blockers, ok := statusMap["blocking_windows"].([]any); ok && len(blockers) > 0 {
		result["blocked"] = true
		result["usable"] = false
		result["unusable_reason"] = "blocking_window"
		return result
	}
	if titleMatch, ok := statusMap["title_match"].(bool); ok && !titleMatch {
		result["usable"] = false
		result["unusable_reason"] = "unexpected_chat_title"
		return result
	}
	if foreground, ok := statusMap["foreground"].(bool); ok && !foreground {
		result["usable"] = false
		result["unusable_reason"] = "not_foreground"
	}
	return result
}

func allowAutomationNow(w http.ResponseWriter, cfg bridgeConfig) bool {
	state := currentPauseState(cfg, time.Now())
	if !state.Paused {
		return true
	}
	writeJSON(w, http.StatusLocked, clientResponse{
		Success: false,
		Code:    -2,
		Message: "operator pause active",
		Data:    state,
	})
	return false
}

func currentPauseState(cfg bridgeConfig, now time.Time) pauseState {
	if state := pauseStateFromFile(cfg.OperatorPauseFile, now); state.Paused {
		return state
	}
	if state := pauseStateFromWindows("env", cfg.OperatorPauseWindows, now); state.Paused {
		return state
	}
	return pauseState{Paused: false}
}

func buildPauseFileConfig(req operatorPauseRequest, now time.Time) (pauseFileConfig, error) {
	cfg := pauseFileConfig{
		Reason:  strings.TrimSpace(req.Reason),
		Windows: strings.TrimSpace(req.Windows),
	}
	if cfg.Reason == "" {
		cfg.Reason = "operator takeover"
	}
	if req.Minutes < 0 {
		return pauseFileConfig{}, fmt.Errorf("minutes must be >= 0")
	}
	if req.Minutes > 0 {
		cfg.PauseUntil = now.Add(time.Duration(req.Minutes) * time.Minute).Format(time.RFC3339)
	}
	if until := strings.TrimSpace(req.PauseUntil); until != "" {
		cfg.PauseUntil = until
	}
	if until := strings.TrimSpace(req.Until); until != "" {
		cfg.PauseUntil = until
	}
	if cfg.PauseUntil != "" {
		if _, err := parsePauseUntil(cfg.PauseUntil, now.Location()); err != nil {
			return pauseFileConfig{}, err
		}
	}
	if cfg.PauseUntil == "" && cfg.Windows == "" {
		paused := true
		if req.Paused != nil {
			paused = *req.Paused
		}
		cfg.Paused = paused
	}
	return cfg, nil
}

func writePauseFile(path string, cfg pauseFileConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

func pauseStateFromFile(path string, now time.Time) pauseState {
	path = strings.TrimSpace(path)
	if path == "" {
		return pauseState{}
	}
	content, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return pauseState{}
		}
		return pauseState{Paused: true, Source: "file", Reason: fmt.Sprintf("read pause file %s: %v", path, err)}
	}
	return pauseStateFromContent("file", strings.TrimSpace(string(content)), now)
}

func pauseStateFromContent(source, content string, now time.Time) pauseState {
	content = strings.TrimSpace(strings.TrimPrefix(content, "\ufeff"))
	if content == "" {
		return pauseState{}
	}
	lower := strings.ToLower(content)
	if lower == "off" || lower == "resume" || lower == "false" || lower == "0" {
		return pauseState{}
	}
	if strings.HasPrefix(content, "{") {
		var cfg pauseFileConfig
		if err := json.Unmarshal([]byte(content), &cfg); err != nil {
			return pauseState{Paused: true, Source: source, Reason: fmt.Sprintf("invalid pause file JSON: %v", err)}
		}
		if cfg.Windows != "" {
			if state := pauseStateFromWindows(source, cfg.Windows, now); state.Paused {
				if cfg.Reason != "" {
					state.Reason = cfg.Reason
				}
				return state
			}
		}
		until := strings.TrimSpace(cfg.PauseUntil)
		if until == "" {
			until = strings.TrimSpace(cfg.Until)
		}
		if until != "" {
			return pauseStateUntil(source, until, cfg.Reason, now)
		}
		if cfg.Paused {
			reason := cfg.Reason
			if reason == "" {
				reason = "manual operator takeover"
			}
			return pauseState{Paused: true, Source: source, Reason: reason}
		}
		return pauseState{}
	}
	if strings.Contains(content, "-") && strings.Contains(content, ":") && !strings.Contains(content, "T") {
		return pauseStateFromWindows(source, content, now)
	}
	if strings.HasPrefix(lower, "until=") {
		return pauseStateUntil(source, strings.TrimSpace(content[len("until="):]), "manual operator takeover", now)
	}
	if lower == "pause" || lower == "paused" || lower == "true" || lower == "1" {
		return pauseState{Paused: true, Source: source, Reason: "manual operator takeover"}
	}
	return pauseStateUntil(source, content, "manual operator takeover", now)
}

func pauseStateUntil(source, rawUntil, reason string, now time.Time) pauseState {
	until, err := parsePauseUntil(rawUntil, now.Location())
	if err != nil {
		return pauseState{Paused: true, Source: source, Reason: fmt.Sprintf("invalid pause-until value %q: %v", rawUntil, err)}
	}
	if !until.After(now) {
		return pauseState{}
	}
	if reason == "" {
		reason = "manual operator takeover"
	}
	return pauseState{
		Paused: true,
		Source: source,
		Reason: reason,
		Until:  until.Format(time.RFC3339),
	}
}

func parsePauseUntil(raw string, loc *time.Location) (time.Time, error) {
	value := strings.TrimSpace(raw)
	for _, layout := range []string{
		time.RFC3339,
		"2006-01-02 15:04",
		"2006-01-02 15:04:05",
		"2006/01/02 15:04",
		"2006/01/02 15:04:05",
	} {
		if t, err := time.ParseInLocation(layout, value, loc); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("expected RFC3339 or local datetime")
}

func pauseStateFromWindows(source, raw string, now time.Time) pauseState {
	for _, window := range splitComma(raw) {
		startRaw, endRaw, ok := strings.Cut(window, "-")
		if !ok {
			return pauseState{Paused: true, Source: source, Reason: fmt.Sprintf("invalid pause window %q", window)}
		}
		start, err := parseClock(startRaw)
		if err != nil {
			return pauseState{Paused: true, Source: source, Reason: fmt.Sprintf("invalid pause window %q: %v", window, err)}
		}
		end, err := parseClock(endRaw)
		if err != nil {
			return pauseState{Paused: true, Source: source, Reason: fmt.Sprintf("invalid pause window %q: %v", window, err)}
		}
		nowMinutes := now.Hour()*60 + now.Minute()
		if clockInWindow(nowMinutes, start, end) {
			return pauseState{
				Paused: true,
				Source: source,
				Reason: fmt.Sprintf("operator takeover window %s", window),
				Until:  nextWindowEnd(now, start, end).Format(time.RFC3339),
			}
		}
	}
	return pauseState{}
}

func parseClock(raw string) (int, error) {
	parts := strings.Split(strings.TrimSpace(raw), ":")
	if len(parts) != 2 {
		return 0, fmt.Errorf("expected HH:MM")
	}
	hour, err := strconv.Atoi(parts[0])
	if err != nil || hour < 0 || hour > 23 {
		return 0, fmt.Errorf("invalid hour")
	}
	minute, err := strconv.Atoi(parts[1])
	if err != nil || minute < 0 || minute > 59 {
		return 0, fmt.Errorf("invalid minute")
	}
	return hour*60 + minute, nil
}

func clockInWindow(now, start, end int) bool {
	if start == end {
		return true
	}
	if start < end {
		return now >= start && now < end
	}
	return now >= start || now < end
}

func nextWindowEnd(now time.Time, start, end int) time.Time {
	endTime := time.Date(now.Year(), now.Month(), now.Day(), end/60, end%60, 0, 0, now.Location())
	nowMinutes := now.Hour()*60 + now.Minute()
	if start >= end && nowMinutes >= start {
		endTime = endTime.Add(24 * time.Hour)
	}
	return endTime
}

func contactName(cfg bridgeConfig, wxid string) string {
	value := strings.TrimSpace(wxid)
	if value == "" {
		return value
	}
	if alias, ok := cfg.ContactAliases[value]; ok {
		return alias
	}
	return value
}

func parseAliases(raw string) map[string]string {
	aliases := map[string]string{}
	for _, item := range strings.Split(raw, ",") {
		key, value, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key != "" && value != "" {
			aliases[key] = value
		}
	}
	return aliases
}

func ensureLoopbackAddr(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid listen address %q: %w", addr, err)
	}
	ip := net.ParseIP(host)
	if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return fmt.Errorf("refuse to listen on non-loopback address %q", addr)
	}
	return nil
}

func ensureLoopbackURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("invalid callback_url %q: %w", raw, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("callback_url must use http or https")
	}
	host := parsed.Hostname()
	if host == "" {
		return fmt.Errorf("callback_url must include a host")
	}
	ip := net.ParseIP(host)
	if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return fmt.Errorf("refuse to post sync-message callback to non-loopback host %q", host)
	}
	return nil
}

func requirePost(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == http.MethodPost {
		return true
	}
	writeClientError(w, http.StatusMethodNotAllowed, "method not allowed")
	return false
}

func writeClientOK(w http.ResponseWriter, data any) {
	writeJSON(w, http.StatusOK, clientResponse{
		Success: true,
		Code:    0,
		Message: "ok",
		Data:    data,
	})
}

func writeClientError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, clientResponse{
		Success: false,
		Code:    -1,
		Message: message,
		Data:    map[string]any{},
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("write json response: %v", err)
	}
}

func skString(value string) map[string]string {
	return map[string]string{"string": value}
}

func robotString(value string) robot.SKBuiltinStringT {
	return robot.SKBuiltinStringT{String: &value}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func uniqueNonEmpty(values ...string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	return result
}

func splitComma(value string) []string {
	parts := strings.Split(value, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			values = append(values, part)
		}
	}
	return values
}

func envString(key, defaultValue string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return defaultValue
}

func envInt(key string, defaultValue int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return defaultValue
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return defaultValue
	}
	return parsed
}

func envBool(key string, defaultValue bool) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	if value == "" {
		return defaultValue
	}
	return value == "1" || value == "true" || value == "yes" || value == "on"
}
