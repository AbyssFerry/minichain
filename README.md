# minichain

一个轻量、可扩展、面向 OpenAI 兼容接口的 Go 对话与 Agent 包。

## 1. 特性

- 支持非流式调用 `Invoke` 与流式调用 `Stream`
- 内置 ReAct Agent，支持工具调用与多轮推理
- 工具注册支持三种方式：函数自动建模、显式 Schema、结构化工具接口
- 会话支持上下文记忆、重置与按 token 裁剪
- 支持超时控制、采样参数和调试消息

## 2. 安装

```bash
go get github.com/abyssferry/minichain@latest
```

导入核心库：

```go
import "github.com/abyssferry/minichain/llm"
```

## 3. 环境变量

推荐先从模板生成 `.env`：

```bash
cp .env.example .env
```

Windows PowerShell：

```powershell
Copy-Item .env.example .env
```

`.env` 示例：

```env
MODEL=gpt-5-nano
API_KEY=your_api_key
BASE_URL=https://api.openai.com/v1
DEBUG_MESSAGES=false
```

说明：

- 使用 `utils.LoadEnv` 读取 `.env`
- 示例 CLI 运行命令：`go run ./cmd/minichain-cli`

## 4. 快速开始

### 4.1 初始化 ChatModel 并调用 Invoke

```go
package main

import (
	"fmt"
	"log"
	"time"

	"github.com/abyssferry/minichain/llm"
	"github.com/abyssferry/minichain/utils"
)

func main() {
	envMap, err := utils.LoadEnv(".env")
	if err != nil {
		log.Fatal(err)
	}

	model := utils.GetEnv(envMap, "MODEL", "")
	apiKey := utils.GetEnv(envMap, "API_KEY", "")
	baseURL := utils.GetEnv(envMap, "BASE_URL", "")
	temperature := 0.3
	requestTimeout := 90 * time.Second

	chatModel, err := llm.InitChatModel(llm.ChatModelOptions{
		Model:          model,
		SystemPrompt:   "你是一个简洁、可靠的助手。",
		APIKey:         apiKey,
		BaseURL:        baseURL,
		Temperature:    &temperature,
		RequestTimeout: &requestTimeout,
	})
	if err != nil {
		log.Fatal(err)
	}

	out, err := chatModel.Invoke(llm.InvokeInput{
		Messages: []llm.Message{{Role: "user", Content: "你好，请做个自我介绍。"}},
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(out.Content)
}
```

### 4.2 初始化 Agent 并调用 Stream

```go
registry := llm.NewToolRegistry()

_ = registry.RegisterFromHandler(
	"get_current_time",
	"当用户询问当前时间时调用",
	func(_ struct{}) (string, error) {
		return time.Now().Format("2006-01-02 15:04:05"), nil
	},
)

agent, err := llm.CreateAgent(llm.AgentOptions{
	Model:                     model,
	SystemPrompt:              "你是一个会用工具解决问题的助手。",
	APIKey:                    apiKey,
	BaseURL:                   baseURL,
	Tools:                     registry,
	MaxReactRounds:            20,
	ContextTrimTokenThreshold: 500,
	ContextKeepRecentRounds:   2,
	Temperature:               &temperature,
	RequestTimeout:            &requestTimeout,
})
if err != nil {
	log.Fatal(err)
}

stream, err := agent.Stream(llm.InvokeInput{
	Messages: []llm.Message{{Role: "user", Content: "现在几点？"}},
})
if err != nil {
	log.Fatal(err)
}

for event := range stream.Events {
	switch event.Type {
	case "content":
		fmt.Print(event.Content)
	case "tool_start":
		fmt.Printf("\n[tool_start] %s args=%s\n", event.ToolName, event.RawArguments)
	case "tool_end":
		fmt.Printf("[tool_end] %s output=%s\n", event.ToolName, event.Content)
	case "error":
		fmt.Printf("\n[error] %s\n", event.Content)
	}
}

summary, err := stream.Wait()
if err != nil {
	log.Fatal(err)
}
fmt.Println("\nfinish:", summary.FinishReason)
```

## 5. Invoke 与 Stream 返回结构（详细）

### 5.1 Invoke 返回结构

`Invoke` 返回类型：`llm.InvokeOutput`

| 字段 | 类型 | 说明 |
|---|---|---|
| Content | string | 助手最终回复文本 |
| ToolCalls | []ToolCall | 模型返回的工具调用列表 |
| FinishReason | string | 结束原因，常见值：`stop`、`tool_calls`、`length` |
| Usage | Usage | token 用量（输入、输出、总量） |
| ID | string | 响应 ID |
| ModelName | string | 实际返回模型名 |
| AdditionalKwargs | map[string]any | 附加字段（如 refusal） |
| ResponseMetadata | map[string]any | 响应元信息 |
| UsageMetadata | map[string]any | 标准化 usage 元数据 |

### 5.2 Stream 返回结构

`Stream` 立即返回类型：`*llm.StreamResult`

`StreamResult` 的关键成员：

- `Events <-chan StreamEvent`：流式事件通道
- `Wait() (StreamSummary, error)`：等待流式结束并拿到完整汇总

`StreamEvent` 字段：

| 字段 | 类型 | 说明 |
|---|---|---|
| Type | string | 事件类型：`content`、`tool_start`、`tool_end`、`error` |
| Content | string | 增量文本或工具输出 |
| ToolName | string | 工具事件对应工具名 |
| RawArguments | string | 工具原始参数 JSON |
| FinishReason | string | 结束原因（结束事件时可用） |

`Wait()` 返回 `StreamSummary` 字段：

| 字段 | 类型 | 说明 |
|---|---|---|
| Content | string | 聚合后的完整文本 |
| ToolCalls | []ToolCall | 聚合后的完整工具调用 |
| Usage | Usage | 流式完成时的 token 用量 |
| ID | string | 响应 ID |
| ModelName | string | 响应模型名 |
| AdditionalKwargs | map[string]any | 附加字段（如 refusal） |
| ResponseMetadata | map[string]any | 响应元信息 |
| UsageMetadata | map[string]any | 标准化 usage 元数据 |
| FinishReason | string | 结束原因 |

调用顺序建议：

1. `for event := range result.Events { ... }` 先消费事件
2. `summary, err := result.Wait()` 再拿最终汇总

## 6. 工具注册

### 6.1 RegisterFromHandler（推荐）

处理函数签名必须是：

- `func(StructType) (string, error)`
- 只能有 1 个入参
- 入参必须是结构体（可为结构体指针）

`tool` 标签支持：

- `desc=...` 字段描述
- `required` 必填
- `default=...` 默认值
- `enum=a|b|c` 枚举值

完整示例（带参数校验与默认值）：

```go
package main

import (
	"fmt"
	"strings"

	"github.com/abyssferry/minichain/llm"
)

type WeatherArgs struct {
	Location string `json:"location" tool:"desc=城市名，例如北京、上海;required"`
	Unit     string `json:"unit,omitempty" tool:"desc=温度单位;default=c;enum=c|f"`
	Days     int    `json:"days,omitempty" tool:"desc=预报天数;default=1"`
}

func GetWeather(args WeatherArgs) (string, error) {
	location := strings.TrimSpace(args.Location)
	if location == "" {
		return "", fmt.Errorf("location is required")
	}
	unit := strings.ToLower(strings.TrimSpace(args.Unit))
	if unit == "" {
		unit = "c"
	}
	if unit != "c" && unit != "f" {
		return "", fmt.Errorf("invalid unit: %s", unit)
	}
	days := args.Days
	if days <= 0 {
		days = 1
	}
	return fmt.Sprintf("%s 未来 %d 天，温度单位 %s", location, days, strings.ToUpper(unit)), nil
}

func registerByHandler() (*llm.ToolRegistry, error) {
	registry := llm.NewToolRegistry()
	err := registry.RegisterFromHandler("get_weather", "查询天气", GetWeather)
	if err != nil {
		return nil, err
	}
	return registry, nil
}
```

无参数工具示例：

```go
func GetCurrentTime(_ struct{}) (string, error) {
	return "2026-04-16 21:00:00", nil
}

// 注册
_ = registry.RegisterFromHandler("get_current_time", "获取当前时间", GetCurrentTime)
```

### 6.2 RegisterSpec（显式 Schema）

适合已有 JSON Schema 或希望手工控制参数结构的场景。

完整示例（手工控制 schema 与执行逻辑）：

```go
registry := llm.NewToolRegistry()

err := registry.RegisterSpec(llm.ToolSpec{
	Name:        "echo",
	Description: "回显输入文本",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"text": map[string]any{
				"type": "string",
				"description": "需要回显的文本",
			},
			"upper": map[string]any{
				"type": "boolean",
				"description": "是否转成大写",
				"default": false,
			},
		},
		"required": []string{"text"},
	},
	Executor: func(arguments map[string]any) (string, error) {
		text, ok := arguments["text"].(string)
		if !ok || strings.TrimSpace(text) == "" {
			return "", fmt.Errorf("text is required")
		}
		if upper, ok := arguments["upper"].(bool); ok && upper {
			return strings.ToUpper(text), nil
		}
		return text, nil
	},
})
if err != nil {
	return err
}
```

### 6.3 RegisterStructuredTool（结构化工具）

适合把工具封装为带 `Name/Description/Handler` 的对象。

完整示例（适合多工具项目做模块化封装）：

```go
type TimeTool struct{}

func (TimeTool) Name() string {
	return "get_current_time"
}

func (TimeTool) Description() string {
	return "获取当前时间"
}

func (TimeTool) Handler() any {
	return func(_ struct{}) (string, error) {
		return "2026-04-16 21:00:00", nil
	}
}

registry := llm.NewToolRegistry()
if err := registry.RegisterStructuredTool(TimeTool{}); err != nil {
	return err
}
```

### 6.4 将工具注册器注入 Agent

```go
registry := llm.NewToolRegistry()
_ = registry.RegisterFromHandler("get_current_time", "获取当前时间", GetCurrentTime)
_ = registry.RegisterStructuredTool(TimeTool{})

agent, err := llm.CreateAgent(llm.AgentOptions{
	Model:   model,
	APIKey:  apiKey,
	BaseURL: baseURL,
	Tools:   registry,
})
if err != nil {
	return err
}
```

### 6.5 常见错误

- 工具名重复：`duplicate tool name`
- Handler 不是函数
- Handler 出参不是 `(string, error)`
- Handler 入参不是单一结构体

错误示例（不要这样写）：

```go
// 错误1：入参不是结构体
func BadTool(name string) (string, error) { return name, nil }

// 错误2：返回值不是 (string, error)
func BadReturn(_ struct{}) string { return "oops" }

// 错误3：重复名称
_ = registry.RegisterFromHandler("get_weather", "查询天气", GetWeather)
_ = registry.RegisterFromHandler("get_weather", "再次注册同名工具", GetWeather)
```

### 6.6 调试建议

- 开启 `DebugMessages=true`，便于观察模型请求与工具调用链路。
- 在 `tool_start` 和 `tool_end` 事件里打印 `ToolName` 与 `RawArguments`。
- 工具函数内部返回可读错误信息，方便模型恢复下一步推理。

## 7. 参数详解

### 7.1 InitChatModel 参数

`InitChatModel(opts llm.ChatModelOptions)`

| 字段 | 类型 | 必填 | 默认值 | 说明 |
|---|---|---|---|---|
| Model | string | 是 | 无 | 模型名称 |
| SystemPrompt | string | 否 | 空 | 初始化注入 system 消息 |
| APIKey | string | 是 | 无 | 模型服务密钥 |
| BaseURL | string | 是 | 无 | OpenAI 兼容接口地址 |
| ContextTrimTokenThreshold | int | 否 | 0 | 裁剪阈值，`<=0` 表示关闭 |
| ContextKeepRecentRounds | int | 否 | 6 | 裁剪后保留轮数，传 0 自动设 6 |
| Temperature | *float64 | 否 | nil | 采样温度 |
| TopP | *float64 | 否 | nil | nucleus sampling |
| MaxTokens | *int | 否 | nil | 最大输出 token |
| Stop | []string | 否 | nil | 停止词 |
| PresencePenalty | *float64 | 否 | nil | presence penalty |
| FrequencyPenalty | *float64 | 否 | nil | frequency penalty |
| Seed | *int | 否 | nil | 随机种子 |
| RequestTimeout | *time.Duration | 否 | 90s | 单轮请求超时，必须大于 0 |
| DebugMessages | bool | 否 | false | 输出调试消息 |

### 7.2 CreateAgent 参数

`CreateAgent(opts llm.AgentOptions)`

| 字段 | 类型 | 必填 | 默认值 | 说明 |
|---|---|---|---|---|
| Model | string | 是 | 无 | 模型名称 |
| SystemPrompt | string | 否 | 空 | 初始化注入 system 消息 |
| APIKey | string | 是 | 无 | 模型服务密钥 |
| BaseURL | string | 是 | 无 | OpenAI 兼容接口地址 |
| MaxReactRounds | int | 否 | 0 | ReAct 最大轮次，0 表示不限制 |
| Tools | *ToolRegistry | 否 | 自动创建空注册器 | 工具定义与执行器 |
| ContextTrimTokenThreshold | int | 否 | 0 | 裁剪阈值，`<=0` 表示关闭 |
| ContextKeepRecentRounds | int | 否 | 6 | 裁剪后保留轮数 |
| Temperature | *float64 | 否 | nil | 采样温度 |
| TopP | *float64 | 否 | nil | nucleus sampling |
| MaxTokens | *int | 否 | nil | 最大输出 token |
| Stop | []string | 否 | nil | 停止词 |
| PresencePenalty | *float64 | 否 | nil | presence penalty |
| FrequencyPenalty | *float64 | 否 | nil | frequency penalty |
| Seed | *int | 否 | nil | 随机种子 |
| RequestTimeout | *time.Duration | 否 | 90s | 单轮请求超时，必须大于 0 |
| DebugMessages | bool | 否 | false | 输出调试消息 |

## 8. 行为说明

- 上下文裁剪触发条件：上一轮 `usage.total_tokens >= ContextTrimTokenThreshold`
- `ContextTrimTokenThreshold <= 0` 表示禁用自动裁剪
- Agent 工具调用支持并发执行，最终按调用顺序回填
- 当返回 `FinishReason=length` 时，内部会尝试裁剪后继续

## 9. 项目结构（含注释）

```text
minichain/
├─ go.mod                      # Go 模块定义
├─ .env.example                # 环境变量模板
├─ README.md                   # 项目说明文档
├─ TEMP_RELEASE_README.md      # 临时发布指南（仅发布流程）
├─ LICENSE                     # 开源许可证文件
├─ cmd/
│  └─ minichain-cli/
│     └─ main.go               # 示例 CLI 入口，用于手动验证
├─ llm/
│  ├─ types.go                 # 对外类型定义（输入、输出、事件、状态）
│  ├─ chat_model.go            # ChatModel 实现（Invoke/Stream）
│  ├─ agent.go                 # Agent 实现（ReAct + 工具调用）
│  ├─ tools_registry.go        # 工具注册与 schema 构建
│  ├─ client_openai.go         # OpenAI 兼容客户端实现
│  ├─ openai_protocol.go       # OpenAI 协议模型定义
│  ├─ context_trim.go          # 上下文裁剪与摘要逻辑
│  └─ *_test.go                # 核心库测试
└─ utils/
   ├─ godotenv.go              # .env 读取工具
   ├─ message_debug.go         # 消息调试打印工具
   └─ *_test.go                # 工具包测试
```

## 10. 许可证建议

推荐使用 MIT 许可证：

- 商用友好
- 约束最少，适合库项目快速传播
- 社区采用广泛

本仓库已附带 MIT 许可证文件 `LICENSE`。


