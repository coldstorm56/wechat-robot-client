package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

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

type scriptOutput struct {
	OK             bool   `json:"ok"`
	Error          string `json:"error"`
	LastText       string `json:"last_text"`
	DeliveryStatus string `json:"delivery_status"`
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
	flag.Parse()

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
		output, err := runScript(r.Context(), cfg, args...)
		if err != nil {
			writeClientError(w, http.StatusBadGateway, err.Error())
			return
		}
		writeClientOK(w, map[string]any{
			"contact":   contact,
			"last_text": output.LastText,
		})
	}
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
