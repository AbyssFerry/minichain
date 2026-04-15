package llm
// Message 表示一次对话消息。
type Message struct {
	// Role 表示消息角色，例如 system、user、assistant、tool。
	Role string `json:"role"`
	// Content 表示消息文本内容。
	Content string `json:"content,omitempty"`
	// ToolCallID 是 tool 角色消息对应的工具调用 ID。
	ToolCallID string `json:"tool_call_id,omitempty"`
	// ToolCalls 是 assistant 返回的工具调用集合。
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

// ToolCall 表示模型返回的单次工具调用。
type ToolCall struct {
	// ID 是工具调用唯一标识。
	ID string `json:"id,omitempty"`
	// Type 固定为 function。
	Type string `json:"type,omitempty"`
	// Function 是调用函数信息。
	Function ToolCallFunction `json:"function"`
	// Index 是并行工具调用时的序号。
	Index int `json:"index,omitempty"`
}

// ToolCallFunction 描述被调用函数的名称和参数。
type ToolCallFunction struct {
	// Name 是工具函数名称。
	Name string `json:"name"`
	// Arguments 是 JSON 字符串格式参数。
	Arguments string `json:"arguments,omitempty"`
}

// ToolDefinition 是发送给模型的工具定义。
type ToolDefinition struct {
	// Type 固定为 function。
	Type string `json:"type"`
	// Function 是函数定义元数据。
	Function ToolFunction `json:"function"`
}

// ToolFunction 定义工具函数元数据。
type ToolFunction struct {
	// Name 是工具名称。
	Name string `json:"name"`
	// Description 是工具用途描述。
	Description string `json:"description"`
	// Parameters 是 JSON Schema 风格参数定义。
	Parameters map[string]any `json:"parameters,omitempty"`
}

// Usage 表示 token 用量统计。
type Usage struct {
	// PromptTokens 是输入 token 数。
	PromptTokens int `json:"prompt_tokens"`
	// CompletionTokens 是输出 token 数。
	CompletionTokens int `json:"completion_tokens"`
	// TotalTokens 是总 token 数。
	TotalTokens int `json:"total_tokens"`
	// PromptTokensDetails 是输入 token 细节。
	PromptTokensDetails map[string]any `json:"prompt_tokens_details,omitempty"`
	// CompletionTokensDetails 是输出 token 细节。
	CompletionTokensDetails map[string]any `json:"completion_tokens_details,omitempty"`
}

// StreamOptions 定义流式输出附加选项。
type StreamOptions struct {
	// IncludeUsage 控制是否在尾包返回 usage。
	IncludeUsage bool `json:"include_usage"`
}

// chatAPIRequest 表示 OpenAI 兼容接口请求体。
type chatAPIRequest struct {
	// Model 是本次请求模型名。
	Model string `json:"model,omitempty"`
	// Messages 是对话上下文。
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
	// ParallelToolCalls 表示是否允许并行工具调用。
	ParallelToolCalls bool `json:"parallel_tool_calls,omitempty"`
	// ToolChoice 是工具调用策略。
	ToolChoice any `json:"tool_choice,omitempty"`
	// StreamOptions 是流式附加参数。
	StreamOptions *StreamOptions `json:"stream_options,omitempty"`
}

// chatAPIResponse 表示 OpenAI 兼容接口响应体。
type chatAPIResponse struct {
	// ID 是响应唯一标识。
	ID string `json:"id,omitempty"`
	// Object 是响应对象名。
	Object string `json:"object,omitempty"`
	// Created 是创建时间戳。
	Created int64 `json:"created,omitempty"`
	// Model 是返回的模型名。
	Model string `json:"model,omitempty"`
	// SystemFingerprint 是服务端指纹。
	SystemFingerprint string `json:"system_fingerprint,omitempty"`
	// Choices 是候选输出列表。
	Choices []chatAPIChoice `json:"choices,omitempty"`
	// Usage 是 token 用量统计。
	Usage Usage `json:"usage,omitempty"`
	// Error 是错误对象，部分兼容服务会在 200/stream chunk 中返回。
	Error *chatAPIError `json:"error,omitempty"`
}

// chatAPIError 表示兼容接口错误对象。
type chatAPIError struct {
	// Message 是错误描述。
	Message string `json:"message,omitempty"`
	// Type 是错误类型。
	Type string `json:"type,omitempty"`
	// Param 是相关参数名。
	Param any `json:"param,omitempty"`
	// Code 是错误码。
	Code string `json:"code,omitempty"`
}

// chatAPIChoice 表示单个候选输出。
type chatAPIChoice struct {
	// Index 是候选序号。
	Index int `json:"index,omitempty"`
	// Message 是非流式完整消息。
	Message Message `json:"message,omitempty"`
	// Delta 是流式增量消息。
	Delta chatAPIDelta `json:"delta,omitempty"`
	// FinishReason 是结束原因。
	FinishReason string `json:"finish_reason,omitempty"`
	// LogProbs 是概率信息，兼容字段。
	LogProbs any `json:"logprobs,omitempty"`
	// Refusal 是拒绝信息。
	Refusal any `json:"refusal,omitempty"`
}

// chatAPIDelta 表示流式增量结构。
type chatAPIDelta struct {
	// Role 是增量角色。
	Role string `json:"role,omitempty"`
	// Content 是增量文本。
	Content string `json:"content,omitempty"`
	// ToolCalls 是工具调用增量信息。
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	// Refusal 是拒绝信息。
	Refusal string `json:"refusal,omitempty"`
}

