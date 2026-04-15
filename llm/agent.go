package llm
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/abyssferry/minichain/utils"
)

// preparedToolCall 表示已校验并可执行的工具调用。
type preparedToolCall struct {
	// ToolCallID 是工具调用唯一标识。
	ToolCallID string
	// ToolName 是工具名称。
	ToolName string
	// RawArguments 是原始参数字符串。
	RawArguments string
	// Arguments 是反序列化后的参数。
	Arguments map[string]any
	// ToolFunc 是可执行工具函数。
	ToolFunc ToolFunc
}

// toolExecutionResult 表示工具执行完成后的结果。
type toolExecutionResult struct {
	// ToolCallID 是工具调用唯一标识。
	ToolCallID string
	// ToolName 是工具名称。
	ToolName string
	// RawArguments 是原始参数字符串。
	RawArguments string
	// Output 是工具输出文本。
	Output string
}

// Agent 是内置 ReAct 框架的智能体实现。
type Agent struct {
	// client 是底层模型客户端。
	client ChatProvider
	// systemPrompt 是会话初始化时注入的系统提示词。
	systemPrompt string
	// requestDefaults 是请求默认参数。
	requestDefaults ChatRequest
	// requestTimeout 是单轮请求超时时间。
	requestTimeout time.Duration
	// maxReactRounds 是单次调用最大轮数。
	maxReactRounds int
	// contextTrimTokenThreshold 是自动裁剪阈值。
	contextTrimTokenThreshold int
	// contextKeepRecentRounds 是裁剪后保留轮数。
	contextKeepRecentRounds int
	// tools 是工具定义列表。
	tools []ToolDefinition
	// mapper 是工具执行映射。
	mapper map[string]ToolFunc
	// state 保存会话状态。
	state *State
	// debugMessages 控制是否输出消息调试信息。
	debugMessages bool
	// mu 用于串行化会话操作。
	mu sync.Mutex
}

// CreateAgent 创建带 ReAct 的 Agent，行为类似 create_agent。
func CreateAgent(opts AgentOptions) (*Agent, error) {
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

	registry := opts.Tools
	if registry == nil {
		registry = NewToolRegistry()
	}

	client, err := newOpenAICompatibleClient(opts.Model, opts.APIKey, opts.BaseURL, requestTimeout)
	if err != nil {
		return nil, err
	}

	state := NewState()
	if systemPrompt != "" {
		state.AppendMessages(Message{Role: "system", Content: systemPrompt})
	}

	return &Agent{
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
		},
		requestTimeout:            requestTimeout,
		maxReactRounds:            opts.MaxReactRounds,
		contextTrimTokenThreshold: opts.ContextTrimTokenThreshold,
		contextKeepRecentRounds:   opts.ContextKeepRecentRounds,
		tools:                     registry.Definitions(),
		mapper:                    registry.Mapper(),
		state:                     state,
		debugMessages:             opts.DebugMessages,
	}, nil
}

// Invoke 以非流式方式运行 ReAct 并返回最终答案。
func (a *Agent) Invoke(input InvokeInput) (InvokeOutput, error) {
	return a.runReActTurn(input.Messages, nil)
}

// Stream 以流式方式运行 ReAct 并持续输出事件。
func (a *Agent) Stream(input InvokeInput) (*StreamResult, error) {
	result := newStreamResult()
	go func() {
		output, err := a.runStreamedReActTurn(input.Messages, func(event StreamEvent) {
			result.events <- event
		})
		if err == nil {
			result.summary = StreamSummary{
				Content:          output.Content,
				ToolCalls:        cloneToolCalls(output.ToolCalls),
				Usage:            output.Usage,
				ID:               output.ID,
				ModelName:        output.ModelName,
				AdditionalKwargs: output.AdditionalKwargs,
				ResponseMetadata: output.ResponseMetadata,
				UsageMetadata:    output.UsageMetadata,
				FinishReason:     output.FinishReason,
			}
		}
		result.finish(result.summary, err)
	}()
	return result, nil
}

// Reset 清空会话上下文并重置到系统提示词。
func (a *Agent) Reset() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.state.Reset()
	if strings.TrimSpace(a.systemPrompt) != "" {
		a.state.AppendMessages(Message{Role: "system", Content: a.systemPrompt})
	}
}

// runReActTurn 执行统一的 ReAct 轮询流程。
func (a *Agent) runReActTurn(inputMessages []Message, emit func(StreamEvent)) (InvokeOutput, error) {
	turnCtx, turnCancel := a.withRequestTimeout()
	defer turnCancel()

	a.mu.Lock()
	if shouldTrimByUsage(a.state.Stats, a.contextTrimTokenThreshold) {
		_ = trimAndSummarizeHistoryContext(turnCtx, a.client, a.contextKeepRecentRounds, &a.state.Messages, &a.state.Stats, "usage")
	}
	turnMessages := cloneMessages(inputMessages)
	requestMessages := append(a.state.CloneMessages(), turnMessages...)
	a.mu.Unlock()

	currentMessages := requestMessages
	pendingTurnMessages := turnMessages
	reactRoundCount := 0
	var finalOutput InvokeOutput

	for {
		if a.maxReactRounds > 0 && reactRoundCount >= a.maxReactRounds {
			return InvokeOutput{}, errors.New("react loop exceeded max rounds")
		}
		reactRoundCount++

		summary, err, _ := a.streamAssistantRound(turnCtx, currentMessages, emit)
		if err != nil {
			return InvokeOutput{}, err
		}

		a.mu.Lock()
		a.state.Stats.LastTotalTokens = summary.Usage.TotalTokens
		a.state.AppendMessages(pendingTurnMessages...)
		a.state.AppendMessages(Message{Role: "assistant", Content: summary.Content, ToolCalls: summary.ToolCalls})
		a.mu.Unlock()

		if len(summary.ToolCalls) == 0 {
			a.mu.Lock()
			a.debugPrintMessages("agent_react_turn_final_state_messages", a.state.Messages)
			a.mu.Unlock()
			finalOutput = InvokeOutput{
				Content:          summary.Content,
				ToolCalls:        cloneToolCalls(summary.ToolCalls),
				FinishReason:     summary.FinishReason,
				Usage:            summary.Usage,
				ID:               summary.ID,
				ModelName:        summary.ModelName,
				AdditionalKwargs: summary.AdditionalKwargs,
				ResponseMetadata: summary.ResponseMetadata,
				UsageMetadata:    summary.UsageMetadata,
			}
			if summary.FinishReason == "length" {
				a.mu.Lock()
				_ = trimAndSummarizeHistoryContext(turnCtx, a.client, a.contextKeepRecentRounds, &a.state.Messages, &a.state.Stats, "finish_reason_length")
				a.mu.Unlock()
			}
			return finalOutput, nil
		}

		preparedCalls, err := prepareToolCalls(summary.ToolCalls, a.mapper)
		if err != nil {
			return InvokeOutput{}, err
		}
		results := executeToolCallsConcurrently(preparedCalls, emit)

		a.mu.Lock()
		for _, toolResult := range results {
			a.state.AppendMessages(Message{Role: "tool", ToolCallID: toolResult.ToolCallID, Content: toolResult.Output})
		}
		currentMessages = a.state.CloneMessages()
		pendingTurnMessages = nil
		a.mu.Unlock()
	}
}

// runStreamedReActTurn 执行流式 ReAct 并返回最终输出。
func (a *Agent) runStreamedReActTurn(inputMessages []Message, emit func(StreamEvent)) (InvokeOutput, error) {
	return a.runReActTurn(inputMessages, emit)
}

// withRequestTimeout 为请求附加内部超时。
func (a *Agent) withRequestTimeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), a.requestTimeout)
}

// debugPrintMessages 在开启调试时打印消息列表。
func (a *Agent) debugPrintMessages(stage string, messages []Message) {
	if a == nil || !a.debugMessages {
		return
	}
	fmt.Fprintf(os.Stderr, "\n[debug][%s]\n", stage)
	if err := utils.PrintMessageListToWriter(os.Stderr, messages); err != nil {
		fmt.Fprintf(os.Stderr, "[debug][%s] print failed: %v\n", stage, err)
	}
}

// streamAssistantRound 执行一次模型流式响应并返回聚合结果。
func (a *Agent) streamAssistantRound(ctx context.Context, messages []Message, emit func(StreamEvent)) (StreamSummary, error, bool) {
	request := a.requestDefaults
	request.Messages = cloneMessages(messages)
	request.Tools = cloneToolDefinitions(a.tools)
	request.ParallelToolCalls = true
	request.Stream = true
	request.StreamOptions = &StreamOptions{IncludeUsage: true}

	stream, err := a.client.ChatStream(ctx, request)
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

// executeToolCallsConcurrently 并发执行工具并保持结果顺序。
func executeToolCallsConcurrently(preparedCalls []preparedToolCall, onEvent func(StreamEvent)) []toolExecutionResult {
	if len(preparedCalls) == 0 {
		return []toolExecutionResult{}
	}

	type indexedResult struct {
		index  int
		result toolExecutionResult
	}

	resultCh := make(chan indexedResult, len(preparedCalls))
	var wg sync.WaitGroup

	for index, prepared := range preparedCalls {
		index := index
		prepared := prepared
		wg.Add(1)
		go func() {
			defer wg.Done()
			if onEvent != nil {
				onEvent(StreamEvent{Type: "tool_start", ToolName: prepared.ToolName, RawArguments: prepared.RawArguments})
			}
			toolOutput, err := prepared.ToolFunc(prepared.Arguments)
			if err != nil {
				toolOutput = "工具执行失败: " + err.Error()
			}
			if onEvent != nil {
				onEvent(StreamEvent{Type: "tool_end", ToolName: prepared.ToolName, RawArguments: prepared.RawArguments, Content: toolOutput})
			}
			resultCh <- indexedResult{index: index, result: toolExecutionResult{ToolCallID: prepared.ToolCallID, ToolName: prepared.ToolName, RawArguments: prepared.RawArguments, Output: toolOutput}}
		}()
	}

	wg.Wait()
	close(resultCh)

	results := make([]toolExecutionResult, len(preparedCalls))
	for item := range resultCh {
		results[item.index] = item.result
	}
	return results
}

// prepareToolCalls 校验工具调用并解析参数。
func prepareToolCalls(toolCalls []ToolCall, mapper map[string]ToolFunc) ([]preparedToolCall, error) {
	preparedCalls := make([]preparedToolCall, 0, len(toolCalls))
	for _, toolCall := range toolCalls {
		if toolCall.Function.Name == "" {
			return nil, errors.New("tool call missing function name")
		}
		args, err := parseToolArguments(toolCall.Function.Arguments)
		if err != nil {
			return nil, fmt.Errorf("invalid tool arguments for %s: %w", toolCall.Function.Name, err)
		}
		toolFunc, ok := mapper[toolCall.Function.Name]
		if !ok {
			return nil, fmt.Errorf("unsupported tool: %s", toolCall.Function.Name)
		}
		preparedCalls = append(preparedCalls, preparedToolCall{ToolCallID: toolCall.ID, ToolName: toolCall.Function.Name, RawArguments: toolCall.Function.Arguments, Arguments: args, ToolFunc: toolFunc})
	}
	return preparedCalls, nil
}

// parseToolArguments 解析工具参数 JSON。
func parseToolArguments(raw string) (map[string]any, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return map[string]any{}, nil
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(trimmed), &args); err != nil {
		return nil, err
	}
	if args == nil {
		return map[string]any{}, nil
	}
	return args, nil
}

