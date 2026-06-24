package model

type AssistantSessionLogStatus string

const (
	AssistantSessionLogStatusReceived AssistantSessionLogStatus = "received"
	AssistantSessionLogStatusSuccess  AssistantSessionLogStatus = "success"
	AssistantSessionLogStatusFailed   AssistantSessionLogStatus = "failed"
)

type AssistantSessionLog struct {
	ID              int64                     `gorm:"column:id;primaryKey;autoIncrement;comment:主键ID" json:"id"`
	MessageID       int64                     `gorm:"column:message_id;index;comment:消息表ID" json:"message_id"`
	MsgID           int64                     `gorm:"column:msg_id;index;comment:微信消息ID" json:"msg_id"`
	Channel         string                    `gorm:"column:channel;type:varchar(32);index;comment:渠道" json:"channel"`
	SessionID       string                    `gorm:"column:session_id;type:varchar(255);index;comment:会话ID" json:"session_id"`
	FromWxID        string                    `gorm:"column:from_wxid;type:varchar(128);index;comment:消息来源微信ID" json:"from_wxid"`
	SenderWxID      string                    `gorm:"column:sender_wxid;type:varchar(128);index;comment:发送人微信ID" json:"sender_wxid"`
	IsChatRoom      bool                      `gorm:"column:is_chat_room;default:false;comment:是否群聊" json:"is_chat_room"`
	TriggerType     string                    `gorm:"column:trigger_type;type:varchar(32);comment:触发类型" json:"trigger_type"`
	RequestText     string                    `gorm:"column:request_text;type:text;comment:请求文本" json:"request_text"`
	ContextCount    int                       `gorm:"column:context_count;comment:上下文条数" json:"context_count"`
	OpenClawURL     string                    `gorm:"column:openclaw_url;type:varchar(512);comment:OpenClaw接口地址" json:"openclaw_url"`
	RequestPayload  string                    `gorm:"column:request_payload;type:longtext;comment:请求负载" json:"request_payload"`
	ResponsePayload string                    `gorm:"column:response_payload;type:longtext;comment:响应负载" json:"response_payload"`
	ReplyText       string                    `gorm:"column:reply_text;type:text;comment:回复文本" json:"reply_text"`
	Status          AssistantSessionLogStatus `gorm:"column:status;type:varchar(32);index;comment:状态" json:"status"`
	ErrorMessage    string                    `gorm:"column:error_message;type:text;comment:错误信息" json:"error_message"`
	DurationMS      int64                     `gorm:"column:duration_ms;comment:耗时毫秒" json:"duration_ms"`
	CreatedAt       int64                     `gorm:"column:created_at;not null;index;comment:创建时间" json:"created_at"`
	UpdatedAt       int64                     `gorm:"column:updated_at;not null;comment:更新时间" json:"updated_at"`
}

func (AssistantSessionLog) TableName() string {
	return "assistant_session_logs"
}
