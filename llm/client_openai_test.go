package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestFormatChatAPIError 验证错误对象格式化输出。
func TestFormatChatAPIError(t *testing.T) {
	got := formatChatAPIError(&chatAPIError{Message: "bad", Code: "invalid_parameter"})
	if !strings.Contains(got, "bad") || !strings.Contains(got, "invalid_parameter") {
		t.Fatalf("unexpected formatted error: %s", got)
	}
}

// TestBuildRequestThinkingConfig 验证 Kimi 与非 Kimi 模型的 thinking 参数构建逻辑。
func TestBuildRequestThinkingConfig(t *testing.T) {
	testCases := []struct {
		name            string
		clientModel     string
		requestModel    string
		requestThinking *ThinkingConfig
		wantThinking    *ThinkingConfig
	}{
		{
			name:         "kimi omits thinking when unset",
			clientModel:  "kimi-k2.5",
			wantThinking: nil,
		},
		{
			name:            "kimi keeps explicit disabled",
			clientModel:     "kimi-k2.5",
			requestThinking: &ThinkingConfig{Type: "disabled"},
			wantThinking:    &ThinkingConfig{Type: "disabled"},
		},
		{
			name:            "kimi keeps explicit enabled",
			clientModel:     "kimi-k2.5",
			requestThinking: &ThinkingConfig{Type: "enabled"},
			wantThinking:    &ThinkingConfig{Type: "enabled"},
		},
		{
			name:            "non kimi ignores thinking",
			clientModel:     "gpt-4.1",
			requestThinking: &ThinkingConfig{Type: "enabled"},
			wantThinking:    nil,
		},
		{
			name:         "request model overrides client model",
			clientModel:  "gpt-4.1",
			requestModel: "kimi-k2.5",
			wantThinking: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			client := &openAICompatibleClient{model: tc.clientModel}
			body := client.buildRequest(ChatRequest{
				Model:    tc.requestModel,
				Thinking: cloneThinkingConfig(tc.requestThinking),
			}, false)

			if tc.wantThinking == nil {
				if body.Thinking != nil {
					t.Fatalf("expected thinking to be omitted, got %+v", body.Thinking)
				}
				return
			}

			if body.Thinking == nil {
				t.Fatalf("expected thinking to be present, got nil")
			}
			if body.Thinking.Type != tc.wantThinking.Type {
				t.Fatalf("unexpected thinking type: got %s want %s", body.Thinking.Type, tc.wantThinking.Type)
			}
		})
	}
}

type recordingChatProvider struct {
	lastRequest ChatRequest
}

// Chat 记录请求并返回一个最小可解析的摘要响应。
func (p *recordingChatProvider) Chat(_ context.Context, req ChatRequest) (*chatAPIResponse, error) {
	p.lastRequest = req
	return &chatAPIResponse{
		Choices: []chatAPIChoice{{Message: Message{Content: "summary"}}},
	}, nil
}

// ChatStream 不在此测试中使用。
func (p *recordingChatProvider) ChatStream(_ context.Context, _ ChatRequest) (*StreamResult, error) {
	return nil, fmt.Errorf("unexpected ChatStream call")
}

// TestSummarizeMessagesPropagatesThinking 验证摘要请求会继承 thinking 配置。
func TestSummarizeMessagesPropagatesThinking(t *testing.T) {
	provider := &recordingChatProvider{}
	thinking := &ThinkingConfig{Type: "disabled"}

	if _, err := summarizeMessages(context.Background(), provider, []Message{{Role: "user", Content: "hello"}}, thinking); err != nil {
		t.Fatalf("summarizeMessages failed: %v", err)
	}

	if provider.lastRequest.Thinking == nil {
		t.Fatalf("expected thinking to be propagated")
	}
	if provider.lastRequest.Thinking.Type != thinking.Type {
		t.Fatalf("unexpected thinking type: got %s want %s", provider.lastRequest.Thinking.Type, thinking.Type)
	}
}

// TestChatStreamReturnsChunkError 验证流式 200 响应中的 error chunk 会返回错误。
func TestChatStreamReturnsChunkError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		writer.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(writer, "data: {\"error\":{\"message\":\"bad stream\",\"code\":\"invalid_parameter_error\"}}\n\n")
		_, _ = fmt.Fprint(writer, "data: [DONE]\n\n")
	}))
	defer server.Close()

	client, err := newOpenAICompatibleClient("test-model", "test-key", server.URL, 5*time.Second, false)
	if err != nil {
		t.Fatalf("new client failed: %v", err)
	}

	stream, err := client.ChatStream(context.Background(), ChatRequest{Messages: []Message{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatalf("ChatStream failed before reading: %v", err)
	}
	for range stream.Events {
		// no-op
	}
	summary, waitErr := stream.Wait()
	if waitErr == nil {
		t.Fatalf("expected error, got summary: %+v", summary)
	}
	if !strings.Contains(waitErr.Error(), "bad stream") {
		t.Fatalf("unexpected stream error: %v", waitErr)
	}
}

// TestSanitizeRequestForDebug 验证请求参数调试输出会对敏感文本做脱敏。
func TestSanitizeRequestForDebug(t *testing.T) {
	body := chatAPIRequest{
		Model: "gpt-5-nano",
		Messages: []Message{
			{Role: "user", Content: "token=sk-123456, Authorization: Bearer abcdef"},
			{
				Role: "assistant",
				ToolCalls: []ToolCall{{
					ID:   "call_1",
					Type: "function",
					Function: ToolCallFunction{
						Name:      "demo",
						Arguments: `{"api_key":"super-secret"}`,
					},
				}},
			},
		},
	}

	sanitized := sanitizeRequestForDebug(body)
	raw, err := json.Marshal(sanitized)
	if err != nil {
		t.Fatalf("marshal sanitized payload failed: %v", err)
	}
	text := string(raw)

	if strings.Contains(text, "sk-123456") {
		t.Fatalf("expected OpenAI key to be redacted: %s", text)
	}
	if strings.Contains(strings.ToLower(text), "bearer abcdef") {
		t.Fatalf("expected bearer token to be redacted: %s", text)
	}
	if strings.Contains(text, "super-secret") {
		t.Fatalf("expected api key value to be redacted: %s", text)
	}
}
