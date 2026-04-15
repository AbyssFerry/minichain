package llm
import (
	"strings"
	"testing"
)

// TestShouldTrimByUsage 验证 token 用量触发裁剪规则。
func TestShouldTrimByUsage(t *testing.T) {
	if shouldTrimByUsage(TurnRuntimeStats{LastTotalTokens: 100}, 0) {
		t.Fatal("threshold=0 should not trim")
	}
	if !shouldTrimByUsage(TurnRuntimeStats{LastTotalTokens: 100}, 100) {
		t.Fatal("expected trim when tokens reach threshold")
	}
}

// TestSelectTrimRange 验证历史裁剪区间选择。
func TestSelectTrimRange(t *testing.T) {
	history := []Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "u1"},
		{Role: "assistant", Content: "a1"},
		{Role: "user", Content: "u2"},
		{Role: "assistant", Content: "a2"},
	}
	start, end := selectTrimRange(history, 1)
	if start != 1 || end != 3 {
		t.Fatalf("unexpected range: (%d,%d)", start, end)
	}
}

// TestBuildSummaryPrompt 验证摘要提示词拼接。
func TestBuildSummaryPrompt(t *testing.T) {
	prompt := buildSummaryPrompt([]Message{{Role: "user", Content: "你好"}, {Role: "assistant", Content: "你好"}})
	if !strings.Contains(prompt, "[user]: 你好") || !strings.Contains(prompt, "[assistant]: 你好") {
		t.Fatalf("unexpected prompt: %s", prompt)
	}
}

