package service

type OpenClawMessage struct {
	Role       string `json:"role"`
	Content    string `json:"content"`
	SenderID   string `json:"sender_id,omitempty"`
	SenderName string `json:"sender_name,omitempty"`
	CreatedAt  int64  `json:"created_at,omitempty"`
}

type OpenClawRequest struct {
	Channel        string            `json:"channel"`
	SessionID      string            `json:"session_id"`
	ConversationID string            `json:"conversation_id"`
	BotName        string            `json:"bot_name"`
	Message        string            `json:"message"`
	Messages       []OpenClawMessage `json:"messages,omitempty"`
	Metadata       map[string]any    `json:"metadata,omitempty"`
}

type OpenClawResponse struct {
	Reply   string `json:"reply,omitempty"`
	Content string `json:"content,omitempty"`
}

type OpenClawCallResult struct {
	RequestPayload  string
	ResponsePayload string
	Reply           string
	StatusCode      int
	DurationMS      int64
}
