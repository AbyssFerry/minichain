package langchain

import (
	"context"
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

// TestChatStreamReturnsChunkError 验证流式 200 响应中的 error chunk 会返回错误。
func TestChatStreamReturnsChunkError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		writer.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(writer, "data: {\"error\":{\"message\":\"bad stream\",\"code\":\"invalid_parameter_error\"}}\n\n")
		_, _ = fmt.Fprint(writer, "data: [DONE]\n\n")
	}))
	defer server.Close()

	client, err := newOpenAICompatibleClient("test-model", "test-key", server.URL, 5*time.Second)
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
