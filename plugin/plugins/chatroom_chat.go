package plugins

import (
	"log"
	"regexp"
	"strings"
	"wechat-robot-client/interface/plugin"
	"wechat-robot-client/model"
	"wechat-robot-client/pkg/robot"
	"wechat-robot-client/service"
	"wechat-robot-client/vars"
)

type ChatRoomAIChatPlugin struct{}

func NewChatRoomAIChatPlugin() plugin.MessageHandler {
	return &ChatRoomAIChatPlugin{}
}

func (p *ChatRoomAIChatPlugin) GetName() string {
	return "ChatRoomAIChat"
}

func (p *ChatRoomAIChatPlugin) GetLabels() []string {
	return []string{"text", "chat"}
}

func (p *ChatRoomAIChatPlugin) Match(ctx *plugin.MessageContext) bool {
	return NewChatRoomCommonPlugin().Match(ctx)
}

func (p *ChatRoomAIChatPlugin) PreAction(ctx *plugin.MessageContext) bool {
	return NewChatRoomCommonPlugin().PreAction(ctx)
}

func (p *ChatRoomAIChatPlugin) PostAction(ctx *plugin.MessageContext) {

}

func (p *ChatRoomAIChatPlugin) Run(ctx *plugin.MessageContext) {
	if !p.PreAction(ctx) {
		return
	}
	isAIEnabled := p.isAIEnabled(ctx)
	isAITrigger := p.isAITrigger(ctx)
	if isAIEnabled {
		if isAITrigger {
			defer func() {
				err := ctx.MessageService.SetMessageIsInContext(ctx.Message)
				if err != nil {
					log.Printf("更新消息上下文失败: %v", err)
				}
			}()
			aiChat := NewAIChatPlugin()
			if !aiChat.Match(ctx) {
				return
			}
			aiChat.Run(ctx)
			return
		}
	}
}

func (p *ChatRoomAIChatPlugin) isAIEnabled(ctx *plugin.MessageContext) bool {
	if !vars.OpenClawSettings.Enabled {
		return ctx.Settings.IsAIChatEnabled()
	}
	chatRoomSettings, err := service.NewChatRoomSettingsService(ctx.Context).GetChatRoomSettings(ctx.Message.FromWxID)
	if err != nil {
		log.Printf("[OpenClaw] 获取群聊白名单设置失败: %v", err)
		return false
	}
	return chatRoomSettings != nil && chatRoomSettings.ChatAIEnabled != nil && *chatRoomSettings.ChatAIEnabled
}

func (p *ChatRoomAIChatPlugin) isAITrigger(ctx *plugin.MessageContext) bool {
	if !vars.OpenClawSettings.Enabled {
		return ctx.Settings.IsAITrigger()
	}

	messageContent := ctx.Message.Content
	if ctx.Message.AppMsgType == model.AppMsgTypequote {
		var xmlMessage robot.XmlMessage
		if err := vars.RobotRuntime.XmlDecoder(messageContent, &xmlMessage); err == nil {
			messageContent = xmlMessage.AppMsg.Title
		}
	}

	if ctx.Message.IsAtMe {
		atAllRegex := regexp.MustCompile(vars.AtAllRegexp)
		if atAllRegex.MatchString(messageContent) {
			return false
		}
	}

	triggerPrefix := strings.TrimSpace(vars.OpenClawSettings.TriggerPrefix)
	mode := strings.ToLower(strings.TrimSpace(vars.OpenClawSettings.TriggerMode))
	hasPrefix := triggerPrefix != "" && strings.HasPrefix(strings.TrimSpace(messageContent), triggerPrefix)

	switch mode {
	case "always":
		return true
	case "at":
		return ctx.Message.IsAtMe
	case "prefix":
		return hasPrefix
	case "at_or_prefix", "":
		return ctx.Message.IsAtMe || hasPrefix
	default:
		log.Printf("[OpenClaw] 未知 TRIGGER_MODE=%q，按 at_or_prefix 处理", vars.OpenClawSettings.TriggerMode)
		return ctx.Message.IsAtMe || hasPrefix
	}
}
