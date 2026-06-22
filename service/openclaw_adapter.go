package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"wechat-robot-client/vars"
)

type OpenClawAdapter struct {
	settings vars.OpenClawSettingS
	client   *http.Client
	timeout  time.Duration
}

func NewOpenClawAdapter(settings vars.OpenClawSettingS) *OpenClawAdapter {
	timeout := settings.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &OpenClawAdapter{
		settings: settings,
		client: &http.Client{
			Timeout: timeout,
		},
		timeout: timeout,
	}
}

func (a *OpenClawAdapter) Call(ctx context.Context, payload OpenClawRequest) (*OpenClawCallResult, error) {
	if strings.TrimSpace(a.settings.BaseURL) == "" {
		return nil, errors.New("OPENCLAW_BASE_URL 未设置")
	}

	requestBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("序列化 OpenClaw 请求失败: %w", err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, a.settings.BaseURL, bytes.NewReader(requestBytes))
	if err != nil {
		return nil, fmt.Errorf("创建 OpenClaw 请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if a.settings.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+a.settings.APIKey)
	}

	start := time.Now()
	resp, err := a.client.Do(req)
	durationMS := time.Since(start).Milliseconds()
	result := &OpenClawCallResult{
		RequestPayload: string(requestBytes),
		DurationMS:     durationMS,
	}
	if err != nil {
		return result, fmt.Errorf("调用 OpenClaw 失败: %w", err)
	}
	defer resp.Body.Close()

	result.StatusCode = resp.StatusCode
	responseBytes, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return result, fmt.Errorf("读取 OpenClaw 响应失败: %w", readErr)
	}
	result.ResponsePayload = string(responseBytes)

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return result, fmt.Errorf("OpenClaw 返回异常状态码: %d", resp.StatusCode)
	}

	var parsed OpenClawResponse
	if err := json.Unmarshal(responseBytes, &parsed); err != nil {
		return result, fmt.Errorf("解析 OpenClaw 响应失败: %w", err)
	}

	reply := strings.TrimSpace(parsed.Reply)
	if reply == "" {
		reply = strings.TrimSpace(parsed.Content)
	}
	result.Reply = truncateRunes(reply, a.settings.MaxReplyLength)
	return result, nil
}

func truncateRunes(text string, maxLength int) string {
	if maxLength <= 0 {
		return text
	}
	runes := []rune(text)
	if len(runes) <= maxLength {
		return text
	}
	return string(runes[:maxLength])
}
