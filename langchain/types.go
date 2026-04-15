package langchain

import (
	"context"
	"time"
)

// State 表示一次模型运行期间需要持续保持的状态。
type State struct {
	// Messages 是当前会话消息历史。
	Messages []Message
	// Stats 是最近一次请求运行统计。
	Stats TurnRuntimeStats
}

// NewState 创建空状态。
func NewState() *State {
	state := &State{}
	state.Reset()
	return state
}

// Reset 将状态恢复为空历史。
func (s *State) Reset() {
	if s == nil {
		return
	}
	s.Stats = TurnRuntimeStats{}
	s.Messages = nil
}

// CloneMessages 返回当前消息历史的副本。
func (s *State) CloneMessages() []Message {
	if s == nil {
		return nil
	}
	return cloneMessages(s.Messages)
}

// AppendMessages 将消息追加到当前历史。
func (s *State) AppendMessages(messages ...Message) {
	if s == nil || len(messages) == 0 {
		return
	}
	s.Messages = append(s.Messages, cloneMessages(messages)...)
}

// ChatModelOptions 定义 init_chat_model 所需配置。
type ChatModelOptions struct {
	// Model 是模型名称，例如 gpt-5-nano。
	Model string
	// SystemPrompt 是初始化会话时注入的系统提示词；为空表示不注入。
	SystemPrompt string
	// APIKey 是访问模型服务的鉴权密钥。
	APIKey string
	// BaseURL 是 OpenAI 兼容接口基础地址。
	BaseURL string
	// ContextTrimTokenThreshold 是触发裁剪的 token 阈值。
	ContextTrimTokenThreshold int
	// ContextKeepRecentRounds 是裁剪时保留的最近轮次。
	ContextKeepRecentRounds int
	// Temperature 是采样温度。
	Temperature *float64
	// TopP 是 nucleus sampling 的阈值。
	TopP *float64
	// MaxTokens 是最大输出 token 数。
	MaxTokens *int
	// Stop 是停止词列表。
	Stop []string
	// PresencePenalty 是 presence penalty。
	PresencePenalty *float64
	// FrequencyPenalty 是 frequency penalty。
	FrequencyPenalty *float64
	// Seed 是随机种子。
	Seed *int
	// RequestTimeout 是单轮请求超时时间；为空时使用默认值。
	RequestTimeout *time.Duration
	// DebugMessages 控制是否打印请求与状态消息列表调试信息。
	DebugMessages bool
}

// AgentOptions 定义 create_agent 所需配置。
type AgentOptions struct {
	// Model 是模型名称，例如 gpt-5-nano。
	Model string
	// SystemPrompt 是初始化会话时注入的系统提示词；为空表示不注入。
	SystemPrompt string
	// APIKey 是访问模型服务的鉴权密钥。
	APIKey string
	// BaseURL 是 OpenAI 兼容接口基础地址。
	BaseURL string
	// MaxReactRounds 是单次调用允许的最大 ReAct 轮数，0 表示不限制。
	MaxReactRounds int
	// Tools 是工具注册器；为空时会自动创建空注册器。
	Tools *ToolRegistry
	// ContextTrimTokenThreshold 是触发裁剪的 token 阈值。
	ContextTrimTokenThreshold int
	// ContextKeepRecentRounds 是裁剪时保留的最近轮次。
	ContextKeepRecentRounds int
	// Temperature 是采样温度。
	Temperature *float64
	// TopP 是 nucleus sampling 的阈值。
	TopP *float64
	// MaxTokens 是最大输出 token 数。
	MaxTokens *int
	// Stop 是停止词列表。
	Stop []string
	// PresencePenalty 是 presence penalty。
	PresencePenalty *float64
	// FrequencyPenalty 是 frequency penalty。
	FrequencyPenalty *float64
	// Seed 是随机种子。
	Seed *int
	// RequestTimeout 是单轮请求超时时间；为空时使用默认值。
	RequestTimeout *time.Duration
	// DebugMessages 控制是否打印请求与状态消息列表调试信息。
	DebugMessages bool
}

// InvokeInput 定义 invoke 的输入。
type InvokeInput struct {
	// Messages 是本轮输入消息列表。
	Messages []Message
}

// InvokeOutput 定义 invoke 或 stream 的最终输出。
type InvokeOutput struct {
	// Content 是助手最终回复文本。
	Content string
	// ToolCalls 是模型返回的工具调用集合。
	ToolCalls []ToolCall
	// FinishReason 是模型结束原因。
	FinishReason string
	// Usage 是本次调用的 token 用量。
	Usage Usage
	// ID 是响应 ID。
	ID string
	// ModelName 是响应模型名。
	ModelName string
	// AdditionalKwargs 是附加字段，例如 refusal。
	AdditionalKwargs map[string]any
	// ResponseMetadata 是响应元数据。
	ResponseMetadata map[string]any
	// UsageMetadata 是标准化后的 usage 元数据。
	UsageMetadata map[string]any
}

// StreamEvent 定义流式事件。
type StreamEvent struct {
	// Type 是事件类型，可能值包括 content、tool_start、tool_end、error。
	Type string
	// Content 是增量文本或工具输出文本。
	Content string
	// ToolName 是工具事件对应的工具名。
	ToolName string
	// RawArguments 是工具原始参数字符串。
	RawArguments string
	// FinishReason 是流式结束原因，仅在结束事件中使用。
	FinishReason string
}

// StreamSummary 表示流式请求结束后的聚合结果。
type StreamSummary struct {
	// Content 是聚合后的完整文本。
	Content string
	// ToolCalls 是聚合后的完整工具调用。
	ToolCalls []ToolCall
	// Usage 是流式结束时返回的 token 用量。
	Usage Usage
	// ID 是响应 ID。
	ID string
	// ModelName 是响应模型名。
	ModelName string
	// AdditionalKwargs 是附加字段，例如 refusal。
	AdditionalKwargs map[string]any
	// ResponseMetadata 是响应元数据。
	ResponseMetadata map[string]any
	// UsageMetadata 是标准化后的 usage 元数据。
	UsageMetadata map[string]any
	// FinishReason 是模型结束原因。
	FinishReason string
}

// StreamResult 表示可遍历的流式结果包装。
type StreamResult struct {
	// Events 是可 range 的流式事件通道。
	Events  <-chan StreamEvent
	events  chan StreamEvent
	done    chan streamOutcome
	summary StreamSummary
	err     error
}

// streamOutcome 是流式结果的内部完成态。
type streamOutcome struct {
	summary StreamSummary
	err     error
}

// newStreamResult 创建一个流式结果包装。
func newStreamResult() *StreamResult {
	events := make(chan StreamEvent)
	return &StreamResult{
		Events: events,
		events: events,
		done:   make(chan streamOutcome, 1),
	}
}

// finish 写入流式完成结果并关闭事件通道。
func (r *StreamResult) finish(summary StreamSummary, err error) {
	if r == nil {
		return
	}
	r.summary = summary
	r.err = err
	r.done <- streamOutcome{summary: summary, err: err}
	close(r.done)
	close(r.events)
}

// Wait 等待流式结果完成并返回聚合结果。
func (r *StreamResult) Wait() (StreamSummary, error) {
	if r == nil {
		return StreamSummary{}, nil
	}
	outcome := <-r.done
	return outcome.summary, outcome.err
}

// ChatRequest 表示发送给模型客户端的统一请求。
type ChatRequest struct {
	// Model 是请求使用的模型名称。
	Model string `json:"model,omitempty"`
	// Messages 是本轮请求携带的上下文消息。
	Messages []Message `json:"messages"`
	// Temperature 是采样温度。
	Temperature *float64 `json:"temperature,omitempty"`
	// TopP 是 nucleus sampling 阈值。
	TopP *float64 `json:"top_p,omitempty"`
	// MaxTokens 是最大输出 token 数。
	MaxTokens *int `json:"max_tokens,omitempty"`
	// Stop 是停止词列表。
	Stop []string `json:"stop,omitempty"`
	// PresencePenalty 是 presence penalty。
	PresencePenalty *float64 `json:"presence_penalty,omitempty"`
	// FrequencyPenalty 是 frequency penalty。
	FrequencyPenalty *float64 `json:"frequency_penalty,omitempty"`
	// Seed 是随机种子。
	Seed *int `json:"seed,omitempty"`
	// Stream 表示是否启用流式。
	Stream bool `json:"stream,omitempty"`
	// Tools 是可调用工具列表。
	Tools []ToolDefinition `json:"tools,omitempty"`
	// ParallelToolCalls 控制是否允许并行工具调用。
	ParallelToolCalls bool `json:"parallel_tool_calls,omitempty"`
	// ToolChoice 用于指定工具调用策略。
	ToolChoice any `json:"tool_choice,omitempty"`
	// StreamOptions 是流式附加参数。
	StreamOptions *StreamOptions `json:"stream_options,omitempty"`
}

// TurnRuntimeStats 记录最近一次请求运行状态。
type TurnRuntimeStats struct {
	// LastTotalTokens 是上一轮总 token。
	LastTotalTokens int
	// LastTrimReason 是最近一次裁剪原因。
	LastTrimReason string
}

// ChatProvider 定义底层模型请求接口。
type ChatProvider interface {
	// Chat 发送非流式请求并返回完整响应。
	Chat(ctx context.Context, req ChatRequest) (*chatAPIResponse, error)
	// ChatStream 发送流式请求并返回可遍历的流式结果。
	ChatStream(ctx context.Context, req ChatRequest) (*StreamResult, error)
}

// cloneMessages 返回消息切片副本。
func cloneMessages(messages []Message) []Message {
	if len(messages) == 0 {
		return nil
	}
	result := make([]Message, len(messages))
	copy(result, messages)
	return result
}

// splitMessagesByRole 将消息按角色拆分。
