package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

type bridgeConfig struct {
	Addr          string
	Path          string
	Agent         string
	SessionPrefix string
	Command       string
	ExecMode      string
	APIKey        string
	Timeout       time.Duration
}

type assistantRequest struct {
	Channel        string         `json:"channel"`
	SessionID      string         `json:"session_id"`
	ConversationID string         `json:"conversation_id"`
	BotName        string         `json:"bot_name"`
	Message        string         `json:"message"`
	Messages       []assistantMsg `json:"messages"`
	Metadata       map[string]any `json:"metadata"`
}

type assistantMsg struct {
	Role       string `json:"role"`
	Content    string `json:"content"`
	SenderID   string `json:"sender_id"`
	SenderName string `json:"sender_name"`
	CreatedAt  int64  `json:"created_at"`
}

type assistantResponse struct {
	Reply   string `json:"reply,omitempty"`
	Content string `json:"content,omitempty"`
	Error   string `json:"error,omitempty"`
}

type agentResponse struct {
	Status string `json:"status"`
	Result struct {
		Payloads []struct {
			Text string `json:"text"`
		} `json:"payloads"`
		FinalAssistantVisibleText string `json:"finalAssistantVisibleText"`
		FinalAssistantRawText     string `json:"finalAssistantRawText"`
	} `json:"result"`
}

func main() {
	cfg := loadConfig()
	mux := http.NewServeMux()
	mux.HandleFunc("/health", healthHandler)
	mux.HandleFunc(cfg.Path, chatHandler(cfg))

	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Printf("OpenClaw bridge listening on http://%s%s", cfg.Addr, cfg.Path)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func loadConfig() bridgeConfig {
	defaultMode := "native"
	defaultCommand := "openclaw"
	if runtime.GOOS == "windows" {
		defaultMode = "wsl"
	}

	addr := flag.String("addr", envString("OPENCLAW_BRIDGE_ADDR", "127.0.0.1:18790"), "HTTP listen address")
	path := flag.String("path", envString("OPENCLAW_BRIDGE_PATH", "/api/assistant/chat"), "chat endpoint path")
	agent := flag.String("agent", envString("OPENCLAW_BRIDGE_AGENT", "main"), "OpenClaw agent id")
	sessionPrefix := flag.String("session-prefix", envString("OPENCLAW_BRIDGE_SESSION_PREFIX", "wechat"), "OpenClaw session key prefix")
	command := flag.String("command", envString("OPENCLAW_BRIDGE_COMMAND", defaultCommand), "OpenClaw command")
	execMode := flag.String("exec-mode", envString("OPENCLAW_BRIDGE_EXEC_MODE", defaultMode), "execution mode: native or wsl")
	apiKey := flag.String("api-key", envString("OPENCLAW_BRIDGE_API_KEY", ""), "optional bearer token expected from client")
	timeoutSeconds := flag.Int("timeout", envInt("OPENCLAW_BRIDGE_TIMEOUT", 120), "OpenClaw agent timeout in seconds")
	flag.Parse()

	cleanPath := strings.TrimSpace(*path)
	if !strings.HasPrefix(cleanPath, "/") {
		cleanPath = "/" + cleanPath
	}

	return bridgeConfig{
		Addr:          strings.TrimSpace(*addr),
		Path:          cleanPath,
		Agent:         strings.TrimSpace(*agent),
		SessionPrefix: strings.TrimSpace(*sessionPrefix),
		Command:       strings.TrimSpace(*command),
		ExecMode:      strings.ToLower(strings.TrimSpace(*execMode)),
		APIKey:        strings.TrimSpace(*apiKey),
		Timeout:       time.Duration(*timeoutSeconds) * time.Second,
	}
}

func healthHandler(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"status": "live",
	})
}

func chatHandler(cfg bridgeConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, assistantResponse{Error: "method not allowed"})
			return
		}
		if cfg.APIKey != "" && r.Header.Get("Authorization") != "Bearer "+cfg.APIKey {
			writeJSON(w, http.StatusUnauthorized, assistantResponse{Error: "unauthorized"})
			return
		}

		var payload assistantRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeJSON(w, http.StatusBadRequest, assistantResponse{Error: fmt.Sprintf("decode request: %v", err)})
			return
		}

		reply, err := callOpenClawAgent(r.Context(), cfg, payload)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, assistantResponse{Error: err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, assistantResponse{Reply: reply, Content: reply})
	}
}

func callOpenClawAgent(parent context.Context, cfg bridgeConfig, payload assistantRequest) (string, error) {
	message := strings.TrimSpace(payload.Message)
	if message == "" && len(payload.Messages) > 0 {
		message = strings.TrimSpace(payload.Messages[len(payload.Messages)-1].Content)
	}
	if message == "" {
		return "", errors.New("empty message")
	}

	sessionID := firstNonEmpty(payload.SessionID, payload.ConversationID, "default")
	sessionKey := fmt.Sprintf("agent:%s:%s:%s", cfg.Agent, safeSessionPart(cfg.SessionPrefix), safeSessionPart(sessionID))
	prompt := buildPrompt(payload, message)

	ctx, cancel := context.WithTimeout(parent, cfg.Timeout+5*time.Second)
	defer cancel()

	args := []string{
		"agent",
		"--agent", cfg.Agent,
		"--session-key", sessionKey,
		"--message", prompt,
		"--json",
		"--timeout", fmt.Sprintf("%d", int(cfg.Timeout.Seconds())),
	}

	var cmd *exec.Cmd
	switch cfg.ExecMode {
	case "wsl":
		cmd = exec.CommandContext(ctx, "wsl.exe", "-e", "sh", "-lc", shellJoin(append([]string{cfg.Command}, args...)))
	case "native", "":
		cmd = exec.CommandContext(ctx, cfg.Command, args...)
	default:
		return "", fmt.Errorf("unsupported OPENCLAW_BRIDGE_EXEC_MODE %q", cfg.ExecMode)
	}

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(output))
		if detail == "" {
			detail = strings.TrimSpace(stderr.String())
		}
		if detail == "" {
			detail = err.Error()
		}
		return "", fmt.Errorf("openclaw agent failed: %s", detail)
	}

	var parsed agentResponse
	if err := json.Unmarshal(output, &parsed); err != nil {
		return "", fmt.Errorf("parse openclaw agent response: %w", err)
	}
	if parsed.Status != "" && parsed.Status != "ok" {
		return "", fmt.Errorf("openclaw agent status %q", parsed.Status)
	}
	for _, payload := range parsed.Result.Payloads {
		if text := strings.TrimSpace(payload.Text); text != "" {
			return text, nil
		}
	}
	if text := strings.TrimSpace(parsed.Result.FinalAssistantVisibleText); text != "" {
		return text, nil
	}
	if text := strings.TrimSpace(parsed.Result.FinalAssistantRawText); text != "" {
		return text, nil
	}
	return "", errors.New("openclaw agent returned empty reply")
}

func buildPrompt(payload assistantRequest, currentMessage string) string {
	var b strings.Builder
	b.WriteString("You are the WeChat AI assistant behind wechat-robot-client. Reply directly to the user in the user's language.\n")
	b.WriteString("Do not mention this bridge unless the user asks about infrastructure.\n\n")
	fmt.Fprintf(&b, "Channel: %s\n", payload.Channel)
	if payload.BotName != "" {
		fmt.Fprintf(&b, "Bot name: %s\n", payload.BotName)
	}
	if payload.SessionID != "" {
		fmt.Fprintf(&b, "Session: %s\n", payload.SessionID)
	}
	if len(payload.Messages) > 0 {
		b.WriteString("\nRecent messages:\n")
		for _, msg := range payload.Messages {
			content := strings.TrimSpace(msg.Content)
			if content == "" {
				continue
			}
			role := firstNonEmpty(msg.Role, "user")
			sender := firstNonEmpty(msg.SenderName, msg.SenderID)
			if sender != "" {
				fmt.Fprintf(&b, "- %s(%s): %s\n", role, sender, content)
			} else {
				fmt.Fprintf(&b, "- %s: %s\n", role, content)
			}
		}
	}
	fmt.Fprintf(&b, "\nCurrent user message:\n%s", currentMessage)
	return b.String()
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("write json response: %v", err)
	}
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
	var parsed int
	if _, err := fmt.Sscanf(value, "%d", &parsed); err != nil || parsed <= 0 {
		return defaultValue
	}
	return parsed
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func safeSessionPart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "default"
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-', r == '_', r == ':', r == '.', r == '@':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	if b.Len() == 0 {
		return "default"
	}
	return b.String()
}

func shellJoin(args []string) string {
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, shellQuote(arg))
	}
	return strings.Join(quoted, " ")
}

func shellQuote(arg string) string {
	if arg == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(arg, "'", `'\''`) + "'"
}
