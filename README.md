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

```go
import (
	"github.com/abyssferry/minichain/llm"
	"github.com/abyssferry/minichain/utils"
)
```

## 3. 环境准备（.env）

请先把项目中的 `.env.example` 复制一份，并将副本重命名为 `.env`。

`.env` 示例：

```env
MODEL=gpt-5-nano
API_KEY=your_api_key
BASE_URL=https://api.openai.com/v1
DEBUG_MESSAGES=false
```

### 3.1 使用 LoadEnv 与 GetEnv

```go
package main

import (
	"fmt"
	"log"

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
	debug := utils.GetEnv(envMap, "DEBUG_MESSAGES", "false")

	fmt.Println("MODEL:", model)
	fmt.Println("API_KEY 是否存在:", apiKey != "")
	fmt.Println("BASE_URL:", baseURL)
	fmt.Println("DEBUG_MESSAGES:", debug)
}
```

## 4. 快速开始（3组完整示例）

### 4.1 InitChatModel 创建 + Invoke 调用

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

	temperature := 0.3
	requestTimeout := 90 * time.Second

	chatModel, err := llm.InitChatModel(llm.ChatModelOptions{
		Model:          utils.GetEnv(envMap, "MODEL", ""),
		APIKey:         utils.GetEnv(envMap, "API_KEY", ""),
		BaseURL:        utils.GetEnv(envMap, "BASE_URL", ""),
		SystemPrompt:   "你是一个简洁、可靠的助手。",
		Temperature:    &temperature,
		RequestTimeout: &requestTimeout,
	})
	if err != nil {
		log.Fatal(err)
	}

	out, err := chatModel.Invoke(llm.InvokeInput{
		Messages: []llm.Message{
			{Role: "user", Content: "请用两句话介绍你自己。"},
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("content:", out.Content)
	fmt.Println("finish_reason:", out.FinishReason)
	fmt.Println("total_tokens:", out.Usage.TotalTokens)
}
```

### 4.2 InitChatModel 创建 + Stream 调用

```go
package main

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/abyssferry/minichain/llm"
	"github.com/abyssferry/minichain/utils"
)

func main() {
	envMap, err := utils.LoadEnv(".env")
	if err != nil {
		log.Fatal(err)
	}

	temperature := 0.3
	requestTimeout := 90 * time.Second

	chatModel, err := llm.InitChatModel(llm.ChatModelOptions{
		Model:          utils.GetEnv(envMap, "MODEL", ""),
		APIKey:         utils.GetEnv(envMap, "API_KEY", ""),
		BaseURL:        utils.GetEnv(envMap, "BASE_URL", ""),
		SystemPrompt:   "你是一个简洁、可靠的助手。",
		Temperature:    &temperature,
		RequestTimeout: &requestTimeout,
	})
	if err != nil {
		log.Fatal(err)
	}

	result, err := chatModel.Stream(llm.InvokeInput{
		Messages: []llm.Message{
			{Role: "user", Content: "请讲一个 50 字以内的小故事。"},
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	var contentBuilder strings.Builder
	for event := range result.Events {
		switch event.Type {
		case "content":
			fmt.Print(event.Content)
			contentBuilder.WriteString(event.Content)
		case "error":
			fmt.Printf("\n[stream_error] %s\n", event.Content)
		}
	}

	summary, err := result.Wait()
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("\n-----")
	fmt.Println("stream_content:", contentBuilder.String())
	fmt.Println("summary_content:", summary.Content)
	fmt.Println("finish_reason:", summary.FinishReason)
	fmt.Println("total_tokens:", summary.Usage.TotalTokens)
}
```

### 4.3 CreateAgent 创建 + Agent.Stream 调用

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

	registry := llm.NewToolRegistry()
	err = registry.RegisterFromHandler(
		"get_current_time",
		"当用户询问当前时间时调用",
		func(_ struct{}) (string, error) {
			return time.Now().Format("2006-01-02 15:04:05"), nil
		},
	)
	if err != nil {
		log.Fatal(err)
	}

	temperature := 0.2
	requestTimeout := 90 * time.Second

	agent, err := llm.CreateAgent(llm.AgentOptions{
		Model:          utils.GetEnv(envMap, "MODEL", ""),
		APIKey:         utils.GetEnv(envMap, "API_KEY", ""),
		BaseURL:        utils.GetEnv(envMap, "BASE_URL", ""),
		SystemPrompt:   "你是一个会优先调用工具来回答问题的助手。",
		Tools:          registry,
		MaxReactRounds: 20,
		Temperature:    &temperature,
		RequestTimeout: &requestTimeout,
	})
	if err != nil {
		log.Fatal(err)
	}

	result, err := agent.Stream(llm.InvokeInput{
		Messages: []llm.Message{
			{Role: "user", Content: "现在几点？请先调用工具再回答。"},
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	for event := range result.Events {
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

	summary, err := result.Wait()
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("\n-----")
	fmt.Println("final_content:", summary.Content)
	fmt.Println("finish_reason:", summary.FinishReason)
	fmt.Println("total_tokens:", summary.Usage.TotalTokens)
}
```

## 5. 工具注册（三种方式）

### 5.1 RegisterFromHandler（推荐）

处理函数签名要求：

- `func(StructType) (string, error)`
- 只能有 1 个入参
- 入参必须是结构体（可为结构体指针）

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
	return fmt.Sprintf("%s 未来 %d 天天气晴朗", location, max(args.Days, 1)), nil
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
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

### 5.2 RegisterSpec（显式 Schema）

适合已有 JSON Schema 或希望手工控制参数结构的场景。

```go
package main

import (
	"fmt"
	"strings"

	"github.com/abyssferry/minichain/llm"
)

func registerBySpec() (*llm.ToolRegistry, error) {
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
		return nil, err
	}
	return registry, nil
}
```

### 5.3 RegisterStructuredTool（结构化工具）

适合把工具封装为带 `Name`、`Description`、`Handler` 的对象。

```go
package main

import "github.com/abyssferry/minichain/llm"

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

func registerByStructuredTool() (*llm.ToolRegistry, error) {
	registry := llm.NewToolRegistry()
	if err := registry.RegisterStructuredTool(TimeTool{}); err != nil {
		return nil, err
	}
	return registry, nil
}
```

## 6. 参数参考

### 6.1 InitChatModel 参数

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

### 6.2 CreateAgent 参数

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

## 7. 返回格式

### 7.1 Invoke 返回格式

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

### 7.2 Stream 返回格式

`Stream` 和 `Agent.Stream` 立即返回类型：`*llm.StreamResult`

`StreamResult` 关键成员：

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

推荐调用顺序：

1. `for event := range result.Events { ... }` 先消费事件
2. `summary, err := result.Wait()` 再拿最终汇总

## 8. 项目结构

```text
minichain/
├─ go.mod
├─ .env.example
├─ README.md
├─ LICENSE
├─ cmd/
│  └─ minichain-cli/
│     └─ main.go
├─ llm/
│  ├─ types.go
│  ├─ chat_model.go
│  ├─ agent.go
│  ├─ tools_registry.go
│  ├─ client_openai.go
│  ├─ openai_protocol.go
│  ├─ context_trim.go
│  └─ *_test.go
└─ utils/
   ├─ godotenv.go
   ├─ message_debug.go
   └─ *_test.go
```

## 9. 许可证

本项目采用 MIT 协议，详见 `LICENSE`。


