package main

// PromptConfig 定义系统对话中使用的所有提示词。
type PromptConfig struct {
	// SystemPrompt 是通用聊天模式的系统提示词。
	SystemPrompt string
	// ReactSystemPrompt 是 ReAct 模式的系统提示词，包含工具使用说明。
	ReactSystemPrompt string
	// SummaryInstruction 是用于指导模型总结历史对话的指令模板。
	SummaryInstruction string
	// SummaryMarker 是插入摘要消息时的前缀标记。
	SummaryMarker string
}

// DefaultPromptConfig 返回预设的默认提示词配置。
func DefaultPromptConfig() PromptConfig {
	return PromptConfig{
		SystemPrompt:       "你是一个有帮助的助手。",
		ReactSystemPrompt:  "你是一个有帮助的助手。用户问天气时调用 get_current_weather，用户问时间时调用 get_current_time。",
		SummaryInstruction: "请将以下对话历史总结为简明扼要的摘要，保留关键信息和上下文：\n\n{{MESSAGES}}\n\n请提供摘要：",
		SummaryMarker:      "【历史上下文已摘要】",
	}
}

// Config 定义 OpenAI 兼容客户端初始化所需的基础配置。
type Config struct {
	// Model 是调用的模型名称，例如 qwen-plus。
	Model string
	// APIKey 是模型服务平台的访问密钥。
	APIKey string
	// BaseURL 是 OpenAI 兼容接口基础地址。
	BaseURL string
	// MaxReactRounds 是 ReAct 模式每次用户输入允许的最大推理轮数；0 表示不限制。
	MaxReactRounds int
	// Prompts 是对话中使用的提示词配置。
	Prompts PromptConfig
	// ContextTrimTokenThreshold 是触发上下文裁剪的 token 数阈值；0 表示禁用基于 usage 的自动裁剪。
	ContextTrimTokenThreshold int
	// ContextKeepRecentRounds 是裁剪时保留的最近对话轮数。
	ContextKeepRecentRounds int
}

// ChatProvider 定义统一的对话服务接口，便于后续切换不同 OpenAI 兼容提供方。
type ChatProvider interface {
	// Chat 发送非流式对话请求并返回完整响应。
	Chat(req ChatRequest) (*chatAPIResponse, error)
	// ChatStream 发送流式请求，并通过 onChunk 回调返回增量文本。
	ChatStream(req ChatRequest, onChunk func(StreamChunk)) (StreamResult, error)
}

// Message 表示 Chat Completions 协议中的一条对话消息。
type Message struct {
	// Role 表示消息角色，常见值为 system、user、assistant、tool。
	Role string `json:"role"`
	// Content 表示消息文本内容。
	Content string `json:"content,omitempty"`
	// ToolCallID 用于 tool 角色消息，关联对应的工具调用请求。
	ToolCallID string `json:"tool_call_id,omitempty"`
	// ToolCalls 表示 assistant 返回的工具调用列表。
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

// ToolCall 表示模型返回的一次函数调用指令。
type ToolCall struct {
	// ID 是本次工具调用的唯一标识。
	ID string `json:"id,omitempty"`
	// Type 固定为 function。
	Type string `json:"type,omitempty"`
	// Function 包含被调用函数名与参数。
	Function ToolCallFunction `json:"function"`
	// Index 是并行工具调用时的序号。
	Index int `json:"index,omitempty"`
}

// ToolCallFunction 描述工具调用中的函数信息。
type ToolCallFunction struct {
	// Name 是函数名称，对应本地工具映射键。
	Name string `json:"name"`
	// Arguments 是 JSON 字符串格式的函数入参。
	Arguments string `json:"arguments,omitempty"`
}

// ToolDefinition 描述可供模型选择的工具定义。
type ToolDefinition struct {
	// Type 固定为 function。
	Type string `json:"type"`
	// Function 是工具函数元信息。
	Function ToolFunction `json:"function"`
}

// ToolFunction 定义单个工具的名称、描述与参数 Schema。
type ToolFunction struct {
	// Name 是工具函数名称。
	Name string `json:"name"`
	// Description 是工具用途描述，供模型判断是否调用。
	Description string `json:"description"`
	// Parameters 是 JSON Schema 风格的参数定义。
	Parameters map[string]any `json:"parameters,omitempty"`
}

// StreamOptions 定义流式输出时的附加选项。
type StreamOptions struct {
	// IncludeUsage 控制是否在流式尾包返回 token 用量。
	IncludeUsage bool `json:"include_usage"`
}

// StreamChunk 表示流式返回中的一个增量片段。
type StreamChunk struct {
	// Content 是回复阶段的增量文本。
	Content string
	// ReasoningContent 是思考阶段的增量文本。
	ReasoningContent string
}

// StreamResult 表示一次流式请求聚合后的结果。
type StreamResult struct {
	// Content 是聚合后的完整回复文本。
	Content string
	// ReasoningContent 是聚合后的完整思考文本。
	ReasoningContent string
	// ToolCalls 是聚合后的完整工具调用列表。
	ToolCalls []ToolCall
	// Usage 是流式结束时返回的 token 用量。
	Usage Usage
}

// ChatRequest 是业务侧使用的统一请求结构。
type ChatRequest struct {
	// Messages 是本轮请求携带的完整上下文消息数组。
	Messages []Message `json:"messages"`
	// Tools 是本轮请求可用的工具集合。
	Tools []ToolDefinition `json:"tools,omitempty"`
	// ParallelToolCalls 控制是否允许模型并行返回多个工具调用。
	ParallelToolCalls bool `json:"parallel_tool_calls,omitempty"`
	// ToolChoice 用于指定工具调用策略，例如 auto 或 none。
	ToolChoice any `json:"tool_choice,omitempty"`
}

// chatAPIRequest 是发送到 OpenAI 兼容接口的请求体。
type chatAPIRequest struct {
	// Model 是本次请求使用的模型名。
	Model string `json:"model"`
	// Messages 是参与推理的上下文消息。
	Messages []Message `json:"messages"`
	// Stream 表示是否启用流式输出。
	Stream bool `json:"stream,omitempty"`
	// Tools 是模型可调用的工具列表。
	Tools []ToolDefinition `json:"tools,omitempty"`
	// ParallelToolCalls 表示是否启用并行工具调用。
	ParallelToolCalls bool `json:"parallel_tool_calls,omitempty"`
	// ToolChoice 是工具调用策略参数。
	ToolChoice any `json:"tool_choice,omitempty"`
	// StreamOptions 是流式输出附加参数。
	StreamOptions *StreamOptions `json:"stream_options,omitempty"`
}

// chatAPIResponse 表示 Chat Completions 接口返回体。
type chatAPIResponse struct {
	// Choices 是候选结果数组。
	Choices []choice `json:"choices"`
	// Usage 是本次请求的 token 用量统计。
	Usage Usage `json:"usage"`
}

// choice 表示单个候选结果。
type choice struct {
	// Message 是非流式场景下的完整消息。
	Message Message `json:"message"`
	// Delta 是流式场景下的增量消息。
	Delta delta `json:"delta"`
}

// delta 表示流式返回中的增量内容。
type delta struct {
	// Content 是本数据块追加的文本片段。
	Content string `json:"content"`
	// ReasoningContent 是思考模型在思考阶段返回的增量文本。
	ReasoningContent string `json:"reasoning_content,omitempty"`
	// ToolCalls 是本数据块中的工具调用增量信息。
	ToolCalls []ToolCall `json:"tool_calls"`
}

// Usage 表示输入输出 token 用量统计。
type Usage struct {
	// PromptTokens 是输入 token 数量。
	PromptTokens int `json:"prompt_tokens"`
	// CompletionTokens 是输出 token 数量。
	CompletionTokens int `json:"completion_tokens"`
	// TotalTokens 是总 token 数量。
	TotalTokens int `json:"total_tokens"`
}

// apiErrorResponse 表示接口错误体的通用封装。
type apiErrorResponse struct {
	// Error 是服务端返回的错误详情对象。
	Error any `json:"error"`
}

// TurnRuntimeStats 记录单个对话轮次的运行时状态，用于触发上下文裁剪的判断。
type TurnRuntimeStats struct {
	// LastTotalTokens 是上一轮请求的 usage.TotalTokens 值。
	LastTotalTokens int
	// LastTrimReason 是最近一次触发上下文裁剪的原因（"usage" 或 "error"）；空字符串表示未触发过裁剪。
	LastTrimReason string
}
