package main

import (
	"strings"
	"testing"
)

func TestBuildPromptIncludesContextAndCurrentMessage(t *testing.T) {
	prompt := buildPrompt(assistantRequest{
		Channel:   "wechat_group",
		SessionID: "group:room@chatroom:wxid_user",
		BotName:   "assistant",
		Messages: []assistantMsg{
			{Role: "assistant", Content: "previous answer"},
			{Role: "user", Content: "current question", SenderID: "wxid_user"},
		},
	}, "current question")

	for _, want := range []string{
		"Channel: wechat_group",
		"Session: group:room@chatroom:wxid_user",
		"assistant: previous answer",
		"user(wxid_user): current question",
		"Current user message:\ncurrent question",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("expected prompt to contain %q, got:\n%s", want, prompt)
		}
	}
}

func TestSafeSessionPart(t *testing.T) {
	got := safeSessionPart("group:room@chatroom:wxid/user")
	if got != "group:room@chatroom:wxid_user" {
		t.Fatalf("unexpected safe session part %q", got)
	}
}

func TestShellJoinQuotesArgs(t *testing.T) {
	got := shellJoin([]string{"openclaw", "agent", "--message", "it's ok"})
	want := "'openclaw' 'agent' '--message' 'it'\\''s ok'"
	if got != want {
		t.Fatalf("unexpected shell command %q", got)
	}
}

func TestExtractPlainTextReplySkipsOpenClawWarnings(t *testing.T) {
	output := []byte(`│
◇  Config warnings ─────────────────────────────╮
│  - plugin warning                             │
├───────────────────────────────────────────────╯
您好，请问有什么可以帮您的？`)

	got := extractPlainTextReply(output)
	if got != "您好，请问有什么可以帮您的？" {
		t.Fatalf("unexpected plain text reply %q", got)
	}
}
