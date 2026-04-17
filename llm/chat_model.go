package llm

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/abyssferry/minichain/utils"
)

const defaultTurnTimeout = 90 * time.Second

// ChatModel 表示普通聊天模型，支持 invoke 与 stream。
type ChatModel struct {
	// client 是底层模型客户端。
	client ChatProvider
	// systemPrompt 是会话初始化时注入的系统提示词。
	systemPrompt string
	// requestDefaults 是请求默认参数。
	requestDefaults ChatRequest
	// requestTimeout 是单轮请求超时时间。
	requestTimeout time.Duration
	// contextTrimTokenThreshold 是自动裁剪阈值。
	contextTrimTokenThreshold int
	// contextKeepRecentRounds 是裁剪后保留轮数。
	contextKeepRecentRounds int
	// state 保存当前会话状态。
	state *State
	// debugMessages 控制是否输出消息调试信息。
	debugMessages bool
	// mu 用于串行化会话操作。
	mu sync.Mutex
}

// InitChatModel 创建普通聊天模型。
func InitChatModel(opts ChatModelOptions) (*ChatModel, error) {
	if strings.TrimSpace(opts.Model) == "" {
		return nil, fmt.Errorf("model cannot be empty")
	}
	if strings.TrimSpace(opts.APIKey) == "" {
		return nil, fmt.Errorf("api key cannot be empty")
	}
	if strings.TrimSpace(opts.BaseURL) == "" {
		return nil, fmt.Errorf("base url cannot be empty")
	}
	requestTimeout, err := resolveRequestTimeout(opts.RequestTimeout)
	if err != nil {
		return nil, err
	}

	if opts.ContextKeepRecentRounds == 0 {
		opts.ContextKeepRecentRounds = 6
	}

	systemPrompt := strings.TrimSpace(opts.SystemPrompt)

	client, err := newOpenAICompatibleClient(opts.Model, opts.APIKey, opts.BaseURL, requestTimeout, opts.DebugRequestParams)
	if err != nil {
		return nil, err
	}

	state := NewState()
	if systemPrompt != "" {
		state.AppendMessages(Message{Role: "system", Content: systemPrompt})
	}

	return &ChatModel{
		client:       client,
		systemPrompt: systemPrompt,
		requestDefaults: ChatRequest{
			Model:            opts.Model,
			Temperature:      opts.Temperature,
			TopP:             opts.TopP,
			MaxTokens:        opts.MaxTokens,
			Stop:             append([]string(nil), opts.Stop...),
			PresencePenalty:  opts.PresencePenalty,
			FrequencyPenalty: opts.FrequencyPenalty,
			Seed:             opts.Seed,
			Thinking:         cloneThinkingConfig(opts.Thinking),
		},
		requestTimeout:            requestTimeout,
		contextTrimTokenThreshold: opts.ContextTrimTokenThreshold,
		contextKeepRecentRounds:   opts.ContextKeepRecentRounds,
		state:                     state,
		debugMessages:             opts.DebugMessages,
	}, nil
}

// Invoke 发送非流式请求并返回最终回复。
func (m *ChatModel) Invoke(input InvokeInput) (InvokeOutput, error) {
	turnCtx, turnCancel := m.withRequestTimeout()
	defer turnCancel()

	m.mu.Lock()
	defer m.mu.Unlock()

	if shouldTrimByUsage(m.state.Stats, m.contextTrimTokenThreshold) {
		_ = trimAndSummarizeHistoryContext(turnCtx, m.client, m.contextKeepRecentRounds, &m.state.Messages, &m.state.Stats, "usage", m.requestDefaults.Thinking)
	}

	turnMessages := cloneMessages(input.Messages)
	requestMessages := append(m.state.CloneMessages(), turnMessages...)
	request := m.requestDefaults
	request.Messages = cloneMessages(requestMessages)
	request.Stream = false
	request.StreamOptions = nil

	resp, err := m.client.Chat(turnCtx, request)
	if err != nil {
		return InvokeOutput{}, err
	}

	finishReason := extractFinishReason(resp)
	if finishReason == "length" {
		if trimErr := trimAndSummarizeHistoryContext(turnCtx, m.client, m.contextKeepRecentRounds, &m.state.Messages, &m.state.Stats, "finish_reason_length", m.requestDefaults.Thinking); trimErr == nil {
			requestMessages = append(m.state.CloneMessages(), turnMessages...)
			request.Messages = cloneMessages(requestMessages)
			resp, err = m.client.Chat(turnCtx, request)
			if err != nil {
				return InvokeOutput{}, err
			}
			finishReason = extractFinishReason(resp)
		}
	}

	assistant := extractAssistantText(resp)
	toolCalls := extractAssistantToolCalls(resp)
	m.state.Stats.LastTotalTokens = resp.Usage.TotalTokens
	m.state.AppendMessages(turnMessages...)
	m.state.AppendMessages(Message{Role: "assistant", Content: assistant, ToolCalls: toolCalls})
	m.debugPrintMessages("chat_invoke_state_messages", m.state.Messages)

	additionalKwargs := map[string]any{"refusal": extractAssistantRefusal(resp)}
	return InvokeOutput{
		Content:          assistant,
		ToolCalls:        cloneToolCalls(toolCalls),
		FinishReason:     finishReason,
		Usage:            resp.Usage,
		ID:               resp.ID,
		ModelName:        resp.Model,
		AdditionalKwargs: additionalKwargs,
		ResponseMetadata: buildResponseMetadata(resp.ID, resp.Model, finishReason, resp.SystemFingerprint, extractAssistantLogprobs(resp), resp.Usage),
		UsageMetadata:    buildUsageMetadata(resp.Usage),
	}, nil
}

// Stream 发送流式请求并返回可遍历的流式结果。
func (m *ChatModel) Stream(input InvokeInput) (*StreamResult, error) {
	result := newStreamResult()
	go func() {
		summary, err := m.runStreamTurn(input.Messages, func(event StreamEvent) {
			result.events <- event
		})
		result.finish(summary, err)
	}()
	return result, nil
}

// Reset 清空会话上下文并重置到系统提示词。
func (m *ChatModel) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state.Reset()
	if strings.TrimSpace(m.systemPrompt) != "" {
		m.state.AppendMessages(Message{Role: "system", Content: m.systemPrompt})
	}
}

// History 返回当前历史副本，便于调试与测试。
func (m *ChatModel) History() []Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state.CloneMessages()
}

// runStreamTurn 执行一次流式请求，必要时在首次失败后裁剪并重试一次。
func (m *ChatModel) runStreamTurn(inputMessages []Message, emit func(StreamEvent)) (StreamSummary, error) {
	turnCtx, turnCancel := m.withRequestTimeout()
	defer turnCancel()

	m.mu.Lock()
	if shouldTrimByUsage(m.state.Stats, m.contextTrimTokenThreshold) {
		_ = trimAndSummarizeHistoryContext(turnCtx, m.client, m.contextKeepRecentRounds, &m.state.Messages, &m.state.Stats, "usage", m.requestDefaults.Thinking)
	}
	turnMessages := cloneMessages(inputMessages)
	requestMessages := append(m.state.CloneMessages(), turnMessages...)
	request := m.requestDefaults
	request.Messages = cloneMessages(requestMessages)
	request.Stream = true
	request.StreamOptions = &StreamOptions{IncludeUsage: true}
	m.mu.Unlock()

	summary, err, _ := m.streamOnce(turnCtx, request, emit)
	if err == nil {
		m.mu.Lock()
		m.state.Stats.LastTotalTokens = summary.Usage.TotalTokens
		m.state.AppendMessages(turnMessages...)
		m.state.AppendMessages(Message{Role: "assistant", Content: summary.Content, ToolCalls: summary.ToolCalls})
		m.debugPrintMessages("chat_stream_state_messages", m.state.Messages)
		if summary.FinishReason == "length" {
			_ = trimAndSummarizeHistoryContext(turnCtx, m.client, m.contextKeepRecentRounds, &m.state.Messages, &m.state.Stats, "finish_reason_length", m.requestDefaults.Thinking)
		}
		m.mu.Unlock()
	}
	return summary, err
}

// streamOnce 执行一次流式请求并将内容事件转发给调用方。
func (m *ChatModel) streamOnce(ctx context.Context, request ChatRequest, emit func(StreamEvent)) (StreamSummary, error, bool) {
	stream, err := m.client.ChatStream(ctx, request)
	if err != nil {
		return StreamSummary{}, err, false
	}

	sawContent := false
	for event := range stream.Events {
		if event.Type == "content" && event.Content != "" {
			sawContent = true
		}
		if emit != nil {
			emit(event)
		}
	}

	summary, waitErr := stream.Wait()
	if waitErr != nil {
		return summary, waitErr, sawContent
	}
	return summary, nil, sawContent
}

// extractFinishReason 提取首个候选的结束原因。
func extractFinishReason(resp *chatAPIResponse) string {
	if resp == nil || len(resp.Choices) == 0 {
		return ""
	}
	return resp.Choices[0].FinishReason
}

// extractAssistantToolCalls 提取首个候选工具调用。
func extractAssistantToolCalls(resp *chatAPIResponse) []ToolCall {
	if resp == nil || len(resp.Choices) == 0 {
		return nil
	}
	return cloneToolCalls(resp.Choices[0].Message.ToolCalls)
}

// extractAssistantRefusal 提取首个候选拒绝信息。
func extractAssistantRefusal(resp *chatAPIResponse) any {
	if resp == nil || len(resp.Choices) == 0 {
		return nil
	}
	if resp.Choices[0].Refusal != nil {
		return resp.Choices[0].Refusal
	}
	if strings.TrimSpace(resp.Choices[0].Message.Content) == "" {
		return ""
	}
	return nil
}

// extractAssistantLogprobs 提取首个候选概率信息。
func extractAssistantLogprobs(resp *chatAPIResponse) any {
	if resp == nil || len(resp.Choices) == 0 {
		return nil
	}
	return resp.Choices[0].LogProbs
}

// cloneToolCalls 返回工具调用切片副本。
func cloneToolCalls(calls []ToolCall) []ToolCall {
	if len(calls) == 0 {
		return nil
	}
	result := make([]ToolCall, len(calls))
	copy(result, calls)
	return result
}

// withRequestTimeout 为请求附加内部超时。
func (m *ChatModel) withRequestTimeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), m.requestTimeout)
}

// debugPrintMessages 在开启调试时打印消息列表。
func (m *ChatModel) debugPrintMessages(stage string, messages []Message) {
	if m == nil || !m.debugMessages {
		return
	}
	fmt.Fprintf(os.Stderr, "\n[debug][%s]\n", stage)
	if err := utils.PrintMessageListToWriter(os.Stderr, messages); err != nil {
		fmt.Fprintf(os.Stderr, "[debug][%s] print failed: %v\n", stage, err)
	}
}

// resolveRequestTimeout 解析请求超时配置。
func resolveRequestTimeout(configured *time.Duration) (time.Duration, error) {
	if configured == nil {
		return defaultTurnTimeout, nil
	}
	if *configured <= 0 {
		return 0, fmt.Errorf("request timeout must be positive")
	}
	return *configured, nil
}
