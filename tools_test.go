package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

// mockStreamProvider 是用于测试 runReactTurn 的流式对话桩实现。
type mockStreamProvider struct {
	// streamResponses 按调用顺序返回的流式聚合结果。
	streamResponses []StreamResult
	// streamCallCount 记录 ChatStream 被调用次数。
	streamCallCount int
	// chatCallCount 记录 Chat 被调用次数，用于确保 react 不再走非流式分支。
	chatCallCount int
}

// Chat 是非流式接口桩；该测试不期望它被调用。
func (m *mockStreamProvider) Chat(_ ChatRequest) (*chatAPIResponse, error) {
	m.chatCallCount++
	return nil, errors.New("unexpected Chat call")
}

// ChatStream 返回预置结果，并将内容通过回调模拟一次流式输出。
func (m *mockStreamProvider) ChatStream(_ ChatRequest, onChunk func(StreamChunk)) (StreamResult, error) {
	if m.streamCallCount >= len(m.streamResponses) {
		return StreamResult{}, errors.New("no more stream responses")
	}

	resp := m.streamResponses[m.streamCallCount]
	m.streamCallCount++
	if onChunk != nil && resp.Content != "" {
		onChunk(StreamChunk{Content: resp.Content})
	}
	return resp, nil
}

// captureStdout 捕获函数执行期间写入标准输出的内容。
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe create failed: %v", err)
	}
	os.Stdout = w

	fn()

	_ = w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	_ = r.Close()
	return buf.String()
}

// TestRunReactTurn_StreamOnlyWithoutTools 验证无工具场景只调用一次 ChatStream 并直接返回最终文本。
func TestRunReactTurn_StreamOnlyWithoutTools(t *testing.T) {
	provider := &mockStreamProvider{
		streamResponses: []StreamResult{{Content: "你好，我是助手。"}},
	}
	history := []Message{{Role: "system", Content: "你是助手"}}
	cfg := Config{Prompts: DefaultPromptConfig(), ContextTrimTokenThreshold: 16000, ContextKeepRecentRounds: 6}
	stats := TurnRuntimeStats{}

	output := captureStdout(t, func() {
		answer, err := runReactTurn(provider, cfg, &history, &stats, "你好")
		if err != nil {
			t.Fatalf("runReactTurn failed: %v", err)
		}
		if answer != "你好，我是助手。" {
			t.Fatalf("unexpected answer: %s", answer)
		}
	})

	if provider.chatCallCount != 0 {
		t.Fatalf("Chat should not be called, got %d", provider.chatCallCount)
	}
	if provider.streamCallCount != 1 {
		t.Fatalf("ChatStream call count mismatch, got %d", provider.streamCallCount)
	}
	if !strings.Contains(output, "助手(流式): 你好，我是助手。") {
		t.Fatalf("unexpected stream output: %s", output)
	}
	if len(history) != 3 {
		t.Fatalf("history size mismatch, got %d", len(history))
	}
	if history[2].Role != "assistant" || history[2].Content != "你好，我是助手。" {
		t.Fatalf("assistant history mismatch: %+v", history[2])
	}
}

// TestRunReactTurn_StreamWithToolLoop 验证带工具场景会先处理工具再继续下一轮流式输出。
func TestRunReactTurn_StreamWithToolLoop(t *testing.T) {
	provider := &mockStreamProvider{
		streamResponses: []StreamResult{
			{
				ToolCalls: []ToolCall{{
					ID:    "call_1",
					Type:  "function",
					Index: 0,
					Function: ToolCallFunction{
						Name:      "get_current_time",
						Arguments: "{}",
					},
				}},
			},
			{Content: "现在时间已告诉你。"},
		},
	}
	history := []Message{{Role: "system", Content: "你是助手"}}
	cfg := Config{Prompts: DefaultPromptConfig(), ContextTrimTokenThreshold: 16000, ContextKeepRecentRounds: 6}
	stats := TurnRuntimeStats{}

	output := captureStdout(t, func() {
		answer, err := runReactTurn(provider, cfg, &history, &stats, "现在几点")
		if err != nil {
			t.Fatalf("runReactTurn failed: %v", err)
		}
		if answer != "现在时间已告诉你。" {
			t.Fatalf("unexpected answer: %s", answer)
		}
	})

	if provider.chatCallCount != 0 {
		t.Fatalf("Chat should not be called, got %d", provider.chatCallCount)
	}
	if provider.streamCallCount != 2 {
		t.Fatalf("ChatStream call count mismatch, got %d", provider.streamCallCount)
	}
	if strings.Count(output, "助手(流式):") != 2 {
		t.Fatalf("stream prompt count mismatch: %s", output)
	}
	if !strings.Contains(output, "[工具] 开始 get_current_time 参数={}") {
		t.Fatalf("missing tool start log: %s", output)
	}
	if !strings.Contains(output, "[工具] 完成 get_current_time 参数={} 输出=当前时间：") {
		t.Fatalf("missing tool finish log: %s", output)
	}
	if !strings.Contains(output, "现在时间已告诉你。") {
		t.Fatalf("missing final stream content: %s", output)
	}

	if len(history) != 5 {
		t.Fatalf("history size mismatch, got %d", len(history))
	}
	if history[1].Role != "user" {
		t.Fatalf("unexpected role at history[1]: %s", history[1].Role)
	}
	if history[2].Role != "assistant" || len(history[2].ToolCalls) != 1 {
		t.Fatalf("assistant tool message mismatch: %+v", history[2])
	}
	if history[3].Role != "tool" || history[3].ToolCallID != "call_1" {
		t.Fatalf("tool message mismatch: %+v", history[3])
	}
	if history[4].Role != "assistant" || history[4].Content != "现在时间已告诉你。" {
		t.Fatalf("final assistant mismatch: %+v", history[4])
	}
}

// TestRunReactTurn_MaxRoundsExceeded 验证配置上限后超过轮数会返回错误。
func TestRunReactTurn_MaxRoundsExceeded(t *testing.T) {
	provider := &mockStreamProvider{
		streamResponses: []StreamResult{
			{
				ToolCalls: []ToolCall{{
					ID:    "call_1",
					Type:  "function",
					Index: 0,
					Function: ToolCallFunction{
						Name:      "get_current_time",
						Arguments: "{}",
					},
				}},
			},
		},
	}
	history := []Message{{Role: "system", Content: "你是助手"}}
	cfg := Config{MaxReactRounds: 1, Prompts: DefaultPromptConfig(), ContextTrimTokenThreshold: 16000, ContextKeepRecentRounds: 6}
	stats := TurnRuntimeStats{}

	errText := ""
	_ = captureStdout(t, func() {
		_, err := runReactTurn(provider, cfg, &history, &stats, "现在几点")
		if err == nil {
			t.Fatal("expected exceeded max rounds error")
		}
		errText = err.Error()
	})

	if !strings.Contains(errText, "react loop exceeded max rounds") {
		t.Fatalf("unexpected error: %s", errText)
	}
	if provider.streamCallCount != 1 {
		t.Fatalf("ChatStream call count mismatch, got %d", provider.streamCallCount)
	}
}

// TestRunReactTurn_MultipleToolCallsOrdered 验证同一轮多个工具会执行并按原顺序回填历史。
func TestRunReactTurn_MultipleToolCallsOrdered(t *testing.T) {
	provider := &mockStreamProvider{
		streamResponses: []StreamResult{
			{
				ToolCalls: []ToolCall{
					{
						ID:    "call_1",
						Type:  "function",
						Index: 0,
						Function: ToolCallFunction{
							Name:      "get_current_weather",
							Arguments: `{"location":"杭州市"}`,
						},
					},
					{
						ID:    "call_2",
						Type:  "function",
						Index: 1,
						Function: ToolCallFunction{
							Name:      "get_current_time",
							Arguments: "{}",
						},
					},
				},
			},
			{Content: "工具结果已整理。"},
		},
	}
	history := []Message{{Role: "system", Content: "你是助手"}}
	cfg := Config{Prompts: DefaultPromptConfig(), ContextTrimTokenThreshold: 16000, ContextKeepRecentRounds: 6}
	stats := TurnRuntimeStats{}

	output := captureStdout(t, func() {
		answer, err := runReactTurn(provider, cfg, &history, &stats, "请告诉我杭州天气和现在时间")
		if err != nil {
			t.Fatalf("runReactTurn failed: %v", err)
		}
		if answer != "工具结果已整理。" {
			t.Fatalf("unexpected answer: %s", answer)
		}
	})

	if provider.streamCallCount != 2 {
		t.Fatalf("ChatStream call count mismatch, got %d", provider.streamCallCount)
	}
	if len(history) != 6 {
		t.Fatalf("history size mismatch, got %d", len(history))
	}
	if history[3].Role != "tool" || history[3].ToolCallID != "call_1" {
		t.Fatalf("tool message[0] mismatch: %+v", history[3])
	}
	if history[4].Role != "tool" || history[4].ToolCallID != "call_2" {
		t.Fatalf("tool message[1] mismatch: %+v", history[4])
	}
	if !strings.Contains(history[3].Content, "杭州市今天是") {
		t.Fatalf("unexpected weather output: %s", history[3].Content)
	}
	if !strings.Contains(history[4].Content, "当前时间：") {
		t.Fatalf("unexpected time output: %s", history[4].Content)
	}

	if !strings.Contains(output, "[工具] 开始 get_current_weather 参数={\"location\":\"杭州市\"}") {
		t.Fatalf("missing weather start log: %s", output)
	}
	if !strings.Contains(output, "[工具] 开始 get_current_time 参数={}") {
		t.Fatalf("missing time start log: %s", output)
	}
	if !strings.Contains(output, "[工具] 完成 get_current_weather 参数={\"location\":\"杭州市\"} 输出=") {
		t.Fatalf("missing weather finish log: %s", output)
	}
	if !strings.Contains(output, "[工具] 完成 get_current_time 参数={} 输出=当前时间：") {
		t.Fatalf("missing time finish log: %s", output)
	}
}

// TestRunReactTurn_ToolFailureContinues 验证单个工具失败时仍会回填失败文本并继续下一轮。
func TestRunReactTurn_ToolFailureContinues(t *testing.T) {
	provider := &mockStreamProvider{
		streamResponses: []StreamResult{
			{
				ToolCalls: []ToolCall{
					{
						ID:    "call_1",
						Type:  "function",
						Index: 0,
						Function: ToolCallFunction{
							Name:      "get_current_weather",
							Arguments: "{}",
						},
					},
					{
						ID:    "call_2",
						Type:  "function",
						Index: 1,
						Function: ToolCallFunction{
							Name:      "get_current_time",
							Arguments: "{}",
						},
					},
				},
			},
			{Content: "已处理完毕。"},
		},
	}
	history := []Message{{Role: "system", Content: "你是助手"}}
	cfg := Config{Prompts: DefaultPromptConfig(), ContextTrimTokenThreshold: 16000, ContextKeepRecentRounds: 6}
	stats := TurnRuntimeStats{}

	output := captureStdout(t, func() {
		answer, err := runReactTurn(provider, cfg, &history, &stats, "帮我查天气和时间")
		if err != nil {
			t.Fatalf("runReactTurn failed: %v", err)
		}
		if answer != "已处理完毕。" {
			t.Fatalf("unexpected answer: %s", answer)
		}
	})

	if provider.streamCallCount != 2 {
		t.Fatalf("ChatStream call count mismatch, got %d", provider.streamCallCount)
	}
	if len(history) != 6 {
		t.Fatalf("history size mismatch, got %d", len(history))
	}
	if history[3].Role != "tool" || history[3].ToolCallID != "call_1" {
		t.Fatalf("tool message[0] mismatch: %+v", history[3])
	}
	if history[4].Role != "tool" || history[4].ToolCallID != "call_2" {
		t.Fatalf("tool message[1] mismatch: %+v", history[4])
	}
	if !strings.Contains(history[3].Content, "工具执行失败: missing argument: location") {
		t.Fatalf("unexpected failed tool output: %s", history[3].Content)
	}
	if !strings.Contains(history[4].Content, "当前时间：") {
		t.Fatalf("unexpected successful tool output: %s", history[4].Content)
	}
	if !strings.Contains(output, "[工具] 完成 get_current_weather 参数={} 输出=工具执行失败: missing argument: location") {
		t.Fatalf("missing failed tool log: %s", output)
	}
	if !strings.Contains(output, "[工具] 开始 get_current_time 参数={}") {
		t.Fatalf("missing time start log: %s", output)
	}
}
