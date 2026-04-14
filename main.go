package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/abyssferry/zhitong-ai-agent/utils"
)

// main 是程序入口，负责初始化配置、创建客户端并启动交互式命令行会话。
func main() {
	envMap, err := utils.LoadEnv(".env")
	if err != nil {
		log.Fatal(err)
	}

	cfg := Config{
		Model:                     getRequiredValue(envMap, "MODEL"),
		APIKey:                    getRequiredValue(envMap, "API_KEY"),
		BaseURL:                   getRequiredValue(envMap, "BASE_URL"),
		MaxReactRounds:            getOptionalPositiveIntValue(envMap, "MAX_REACT_ROUNDS"),
		Prompts:                   DefaultPromptConfig(),
		ContextTrimTokenThreshold: getOptionalPositiveIntValue(envMap, "CONTEXT_TRIM_TOKEN_THRESHOLD"),
		ContextKeepRecentRounds:   getOptionalPositiveIntValue(envMap, "CONTEXT_KEEP_RECENT_ROUNDS"),
	}

	// 设置默认值：若环境变量未设置则使用硬编码默认值
	if cfg.ContextTrimTokenThreshold == 0 {
		cfg.ContextTrimTokenThreshold = 16000
	}
	if cfg.ContextKeepRecentRounds == 0 {
		cfg.ContextKeepRecentRounds = 6
	}

	client, err := NewOpenAICompatibleClient(cfg)
	if err != nil {
		log.Fatal(err)
	}

	chatHistory := []Message{{Role: "system", Content: cfg.Prompts.SystemPrompt}}
	streamHistory := []Message{{Role: "system", Content: cfg.Prompts.SystemPrompt}}
	reactHistory := []Message{{
		Role:    "system",
		Content: cfg.Prompts.ReactSystemPrompt,
	}}

	// 为每个模式初始化对话轮次运行时统计
	chatStats := TurnRuntimeStats{}
	streamStats := TurnRuntimeStats{}
	reactStats := TurnRuntimeStats{}

	mode := "chat"
	scanner := bufio.NewScanner(os.Stdin)

	fmt.Println("OpenAI-Compatible CLI 已启动")
	fmt.Println("命令: /mode chat | /mode stream | /mode react | /clear | /help | /exit")

	for {
		fmt.Printf("[%s] 你: ", mode)
		if !scanner.Scan() {
			fmt.Println("\n输入结束，程序退出")
			return
		}

		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}

		if strings.HasPrefix(input, "/") {
			// 斜杠开头视为控制命令，不进入模型对话流程。
			nextMode, shouldExit, cleared := handleCommand(input, mode)
			if shouldExit {
				fmt.Println("已退出")
				return
			}
			mode = nextMode
			if cleared {
				chatHistory = []Message{{Role: "system", Content: cfg.Prompts.SystemPrompt}}
				streamHistory = []Message{{Role: "system", Content: cfg.Prompts.SystemPrompt}}
				reactHistory = []Message{{
					Role:    "system",
					Content: cfg.Prompts.ReactSystemPrompt,
				}}
				chatStats = TurnRuntimeStats{}
				streamStats = TurnRuntimeStats{}
				reactStats = TurnRuntimeStats{}
				fmt.Println("上下文已清空")
			}
			continue
		}

		switch mode {
		case "chat":
			assistant, err := runChatTurn(client, cfg, &chatHistory, &chatStats, input)
			if err != nil {
				fmt.Printf("错误: %v\n", err)
				continue
			}
			fmt.Printf("助手: %s\n", assistant)
		case "stream":
			assistant, err := runStreamTurn(client, cfg, &streamHistory, &streamStats, input)
			if err != nil {
				fmt.Printf("错误: %v\n", err)
				continue
			}
			fmt.Printf("\n助手(完整): %s\n", assistant)
		case "react":
			_, err := runReactTurn(client, cfg, &reactHistory, &reactStats, input)
			if err != nil {
				fmt.Printf("错误: %v\n", err)
				continue
			}
		default:
			fmt.Printf("未知模式: %s\n", mode)
		}
	}
}

// getRequiredValue 返回必填配置项；当键不存在或为空时终止程序。
func getRequiredValue(envMap map[string]string, key string) string {
	value := strings.TrimSpace(envMap[key])
	if value == "" {
		log.Fatalf("missing required key in .env: %s", key)
	}
	return value
}

// getOptionalPositiveIntValue 返回可选正整数配置；空值返回 0，非法值会终止程序。
func getOptionalPositiveIntValue(envMap map[string]string, key string) int {
	raw := strings.TrimSpace(envMap[key])
	if raw == "" {
		return 0
	}

	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		log.Fatalf("invalid optional key in .env: %s must be a positive integer, got %q", key, raw)
	}
	return value
}

// handleCommand 解析并执行 CLI 命令，返回下一模式、是否退出和是否清空上下文。
func handleCommand(input string, mode string) (nextMode string, shouldExit bool, cleared bool) {
	parts := strings.Fields(strings.TrimSpace(input))
	if len(parts) == 0 {
		return mode, false, false
	}

	switch parts[0] {
	case "/exit", "/quit":
		return mode, true, false
	case "/help":
		fmt.Println("命令: /mode chat|stream|react, /clear, /help, /exit")
		return mode, false, false
	case "/clear":
		return mode, false, true
	case "/mode":
		if len(parts) < 2 {
			fmt.Println("用法: /mode chat|stream|react")
			return mode, false, false
		}
		target := strings.ToLower(parts[1])
		if target != "chat" && target != "stream" && target != "react" {
			fmt.Println("仅支持: chat, stream, react")
			return mode, false, false
		}
		fmt.Printf("已切换模式: %s\n", target)
		return target, false, false
	default:
		fmt.Println("未知命令，可输入 /help 查看帮助")
		return mode, false, false
	}
}

// runChatTurn 执行一次普通对话轮次，支持上下文自动裁剪和错误重试。
func runChatTurn(client ChatProvider, cfg Config, history *[]Message, stats *TurnRuntimeStats, userInput string) (string, error) {
	// 检查是否需要基于 usage 进行裁剪
	if shouldTrimByUsage(*stats, cfg) {
		if err := TrimAndSummarizeHistoryContext(client, cfg, history, stats, "usage"); err != nil {
			fmt.Printf("警告: 上下文裁剪失败: %v\n", err)
		}
	}

	*history = append(*history, Message{Role: "user", Content: userInput})

	// 使用 RetryWithTrim 处理请求和错误触发的重试
	respInterface, _, err := RetryWithTrim(client, cfg, history, stats, func() (interface{}, Usage, error) {
		resp, respErr := client.Chat(ChatRequest{Messages: *history})
		if respErr != nil {
			return nil, Usage{}, respErr
		}
		return resp, resp.Usage, nil
	})

	if err != nil {
		return "", err
	}

	resp := respInterface.(*chatAPIResponse)
	assistant := extractAssistantText(resp)
	*history = append(*history, Message{Role: "assistant", Content: assistant})
	return assistant, nil
}

// runStreamTurn 执行一次流式对话轮次，支持上下文自动裁剪和错误重试。
func runStreamTurn(client ChatProvider, cfg Config, history *[]Message, stats *TurnRuntimeStats, userInput string) (string, error) {
	// 检查是否需要基于 usage 进行裁剪
	if shouldTrimByUsage(*stats, cfg) {
		if err := TrimAndSummarizeHistoryContext(client, cfg, history, stats, "usage"); err != nil {
			fmt.Printf("警告: 上下文裁剪失败: %v\n", err)
		}
	}

	*history = append(*history, Message{Role: "user", Content: userInput})
	fmt.Print("助手(流式): ")

	// 使用 RetryWithTrim 处理请求和错误触发的重试
	respInterface, usage, err := RetryWithTrim(client, cfg, history, stats, func() (interface{}, Usage, error) {
		result, respErr := client.ChatStream(ChatRequest{Messages: *history}, func(chunk StreamChunk) {
			if chunk.Content != "" {
				fmt.Print(chunk.Content)
			}
		})
		if respErr != nil {
			return nil, Usage{}, respErr
		}
		return result, result.Usage, nil
	})

	if err != nil {
		return "", err
	}

	result := respInterface.(StreamResult)
	if usage.TotalTokens > 0 {
		fmt.Printf("\n用量: prompt=%d completion=%d total=%d", usage.PromptTokens, usage.CompletionTokens, usage.TotalTokens)
	}
	*history = append(*history, Message{Role: "assistant", Content: result.Content})
	return result.Content, nil
}
