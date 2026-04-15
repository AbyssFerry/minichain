package llm
import (
	"testing"
	"time"
)

// TestResolveRequestTimeout_Default 验证未配置超时时返回默认值。
func TestResolveRequestTimeout_Default(t *testing.T) {
	resolved, err := resolveRequestTimeout(nil)
	if err != nil {
		t.Fatalf("resolveRequestTimeout(nil) returned error: %v", err)
	}
	if resolved != defaultTurnTimeout {
		t.Fatalf("unexpected default timeout: got %v, want %v", resolved, defaultTurnTimeout)
	}
}

// TestResolveRequestTimeout_Custom 验证传入正超时时保持原值。
func TestResolveRequestTimeout_Custom(t *testing.T) {
	custom := 12 * time.Second
	resolved, err := resolveRequestTimeout(&custom)
	if err != nil {
		t.Fatalf("resolveRequestTimeout(custom) returned error: %v", err)
	}
	if resolved != custom {
		t.Fatalf("unexpected custom timeout: got %v, want %v", resolved, custom)
	}
}

// TestResolveRequestTimeout_Invalid 验证传入非正超时时返回错误。
func TestResolveRequestTimeout_Invalid(t *testing.T) {
	invalid := 0 * time.Second
	_, err := resolveRequestTimeout(&invalid)
	if err == nil {
		t.Fatal("expected error for non-positive timeout, got nil")
	}
}

// TestChatModelWithRequestTimeout_UsesConfiguredDuration 验证 ChatModel 内部上下文超时使用配置值。
func TestChatModelWithRequestTimeout_UsesConfiguredDuration(t *testing.T) {
	model := &ChatModel{requestTimeout: 3 * time.Second}
	ctx, cancel := model.withRequestTimeout()
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("expected context deadline to be set")
	}

	remaining := time.Until(deadline)
	if remaining > 3*time.Second || remaining <= 0 {
		t.Fatalf("unexpected remaining timeout: %v", remaining)
	}
}

// TestAgentWithRequestTimeout_UsesConfiguredDuration 验证 Agent 内部上下文超时使用配置值。
func TestAgentWithRequestTimeout_UsesConfiguredDuration(t *testing.T) {
	agent := &Agent{requestTimeout: 2 * time.Second}
	ctx, cancel := agent.withRequestTimeout()
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("expected context deadline to be set")
	}

	remaining := time.Until(deadline)
	if remaining > 2*time.Second || remaining <= 0 {
		t.Fatalf("unexpected remaining timeout: %v", remaining)
	}
}

