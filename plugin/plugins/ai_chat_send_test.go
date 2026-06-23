package plugins

import (
	"context"
	"errors"
	"io"
	"mime/multipart"
	"testing"

	"github.com/openai/openai-go/v3"

	"wechat-robot-client/dto"
	"wechat-robot-client/interface/plugin"
	"wechat-robot-client/model"
	"wechat-robot-client/pkg/robot"
)

type fakeAIChatMessageService struct {
	sendErr error
	toWxID  string
	content string
	at      []string
}

func (s *fakeAIChatMessageService) SendTextMessage(toWxID, content string, at ...string) error {
	s.toWxID = toWxID
	s.content = content
	s.at = append([]string(nil), at...)
	return s.sendErr
}

func (s *fakeAIChatMessageService) SendLongTextMessage(string, string) error { return nil }
func (s *fakeAIChatMessageService) SendAppMessage(string, int, string) error { return nil }
func (s *fakeAIChatMessageService) MsgUploadImg(string, io.Reader) (*model.Message, error) {
	return nil, nil
}
func (s *fakeAIChatMessageService) SendImageMessageByLocalPath(string, string) error { return nil }
func (s *fakeAIChatMessageService) SendImageMessageByRemoteURL(string, string) error { return nil }
func (s *fakeAIChatMessageService) SendVideoMessageByLocalPath(string, string) error { return nil }
func (s *fakeAIChatMessageService) SendVideoMessageByRemoteURL(string, string) error { return nil }
func (s *fakeAIChatMessageService) SendVoiceMessageByLocalPath(string, string) error { return nil }
func (s *fakeAIChatMessageService) SendFileMessageByLocalPath(string, string) error  { return nil }
func (s *fakeAIChatMessageService) SendImageMessageStream(context.Context, dto.SendImageMessageRequest, io.Reader, *multipart.FileHeader) (*model.Message, error) {
	return nil, nil
}
func (s *fakeAIChatMessageService) SendFileMessage(context.Context, dto.SendFileMessageRequest, io.Reader, *multipart.FileHeader) error {
	return nil
}
func (s *fakeAIChatMessageService) MsgSendVoice(string, io.Reader, string) error { return nil }
func (s *fakeAIChatMessageService) MsgSendVideo(string, io.Reader, string) error { return nil }
func (s *fakeAIChatMessageService) SendMusicMessage(string, string) error        { return nil }
func (s *fakeAIChatMessageService) ShareLink(string, robot.ShareLinkMessage) error {
	return nil
}
func (s *fakeAIChatMessageService) ResetChatRoomAIMessageContext(*model.Message) error {
	return nil
}
func (s *fakeAIChatMessageService) GetAIMessageContext(*model.Message) ([]openai.ChatCompletionMessageParamUnion, error) {
	return nil, nil
}
func (s *fakeAIChatMessageService) SetMessageIsInContext(*model.Message) error { return nil }
func (s *fakeAIChatMessageService) XmlDecoder(string) (robot.XmlMessage, error) {
	return robot.XmlMessage{}, nil
}
func (s *fakeAIChatMessageService) UpdateMessage(*model.Message) error { return nil }
func (s *fakeAIChatMessageService) ChatRoomAIDisabled(string) error    { return nil }
func (s *fakeAIChatMessageService) GetChatRoomMember(string, string) (*model.ChatRoomMember, error) {
	return nil, nil
}
func (s *fakeAIChatMessageService) ToolsCompleted(string, string) error { return nil }

func TestAIChatPluginSendMessageReturnsSendError(t *testing.T) {
	sendErr := errors.New("bridge blocking_window")
	messageService := &fakeAIChatMessageService{sendErr: sendErr}
	ctx := &plugin.MessageContext{
		Message:        &model.Message{FromWxID: "wxid_friend"},
		MessageService: messageService,
	}

	err := (&AIChatPlugin{}).SendMessage(ctx, "reply text")

	if !errors.Is(err, sendErr) {
		t.Fatalf("err=%v", err)
	}
	if messageService.toWxID != "wxid_friend" || messageService.content != "reply text" || len(messageService.at) != 0 {
		t.Fatalf("send call mismatch: %#v", messageService)
	}
}

func TestAIChatPluginSendMessageReturnsChatRoomSendError(t *testing.T) {
	sendErr := errors.New("bridge blocking_window")
	messageService := &fakeAIChatMessageService{sendErr: sendErr}
	ctx := &plugin.MessageContext{
		Message: &model.Message{
			FromWxID:   "room@chatroom",
			SenderWxID: "wxid_sender",
			IsChatRoom: true,
		},
		MessageService: messageService,
	}

	err := (&AIChatPlugin{}).SendMessage(ctx, "reply text")

	if !errors.Is(err, sendErr) {
		t.Fatalf("err=%v", err)
	}
	if messageService.toWxID != "room@chatroom" || len(messageService.at) != 1 || messageService.at[0] != "wxid_sender" {
		t.Fatalf("send call mismatch: %#v", messageService)
	}
}
