package llm
import (
	"errors"
	"testing"
)

// TestStateReset 验证状态重置行为。
func TestStateReset(t *testing.T) {
	state := NewState()
	state.AppendMessages(Message{Role: "system", Content: "sys"}, Message{Role: "user", Content: "u1"})
	state.Reset()
	if len(state.Messages) != 0 {
		t.Fatalf("unexpected message size after reset: %d", len(state.Messages))
	}
}

// TestCloneMessages 验证消息副本隔离。
func TestCloneMessages(t *testing.T) {
	original := []Message{{Role: "user", Content: "hello"}}
	cloned := cloneMessages(original)
	cloned[0].Content = "changed"
	if original[0].Content != "hello" {
		t.Fatalf("clone should not mutate original: %+v", original)
	}
}

// TestStreamResultWait 验证流式结果等待和错误回传。
func TestStreamResultWait(t *testing.T) {
	result := newStreamResult()
	expectedErr := errors.New("stream failed")
	summary := StreamSummary{Content: "hello", FinishReason: "stop"}
	result.finish(summary, expectedErr)

	gotSummary, gotErr := result.Wait()
	if gotErr == nil || gotErr.Error() != expectedErr.Error() {
		t.Fatalf("unexpected error: %v", gotErr)
	}
	if gotSummary.Content != "hello" || gotSummary.FinishReason != "stop" {
		t.Fatalf("unexpected summary: %+v", gotSummary)
	}
}

