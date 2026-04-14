package utils

import (
	"bytes"
	"strings"
	"testing"
)

// debugToolCall 用于测试嵌套字段的打印表现。
type debugToolCall struct {
	ID   string `json:"id,omitempty"`
	Type string `json:"type,omitempty"`
}

// debugMessage 用于模拟主流程中的 Message 结构。
type debugMessage struct {
	Role       string          `json:"role"`
	Content    string          `json:"content,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
	ToolCalls  []debugToolCall `json:"tool_calls,omitempty"`
}

func TestPrintMessageListToWriter(t *testing.T) {
	messages := []debugMessage{
		{
			Role:       "assistant",
			Content:    "hello",
			ToolCallID: "call_1",
			ToolCalls: []debugToolCall{
				{ID: "tc_1", Type: "function"},
			},
		},
	}

	var out bytes.Buffer
	err := PrintMessageListToWriter(&out, messages)
	if err != nil {
		t.Fatalf("PrintMessageListToWriter returned error: %v", err)
	}

	text := out.String()
	if !strings.Contains(text, "message 总数: 1") {
		t.Fatalf("unexpected output, missing count: %s", text)
	}
	if !strings.Contains(text, "Role =") {
		t.Fatalf("unexpected output, missing Role field: %s", text)
	}
	if !strings.Contains(text, "ToolCalls =") {
		t.Fatalf("unexpected output, missing ToolCalls field: %s", text)
	}
	if !strings.Contains(text, "\"assistant\"") {
		t.Fatalf("unexpected output, missing field value: %s", text)
	}
}

func TestPrintMessageListToWriterInvalidInput(t *testing.T) {
	var out bytes.Buffer
	err := PrintMessageListToWriter(&out, "not-a-list")
	if err == nil {
		t.Fatalf("expected error for non-list input")
	}
}
