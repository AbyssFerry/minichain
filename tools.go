package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"
)

// ToolFunc 定义工具函数的签名，接收参数映射表，返回字符串结果或错误，ToolFunc是一个这个类型别名。
type ToolFunc func(arguments map[string]any) (string, error)

// preparedToolCall 表示执行前已完成校验与参数解析的工具调用。
type preparedToolCall struct {
	// ToolCallID 是工具调用唯一标识，用于回填 tool 角色消息。
	ToolCallID string
	// ToolName 是工具函数名。
	ToolName string
	// RawArguments 是原始参数字符串，仅用于日志输出。
	RawArguments string
	// Arguments 是反序列化后的参数对象。
	Arguments map[string]any
	// ToolFunc 是要执行的本地工具函数。
	ToolFunc ToolFunc
}

// toolExecutionResult 表示单个工具调用执行后的聚合结果。
type toolExecutionResult struct {
	// ToolCallID 是工具调用唯一标识，用于回填 tool 角色消息。
	ToolCallID string
	// ToolName 是工具函数名。
	ToolName string
	// RawArguments 是原始参数字符串，仅用于日志输出。
	RawArguments string
	// Output 是工具执行结果；执行失败时为容错文本。
	Output string
}

// ToolExecutionCallbacks 定义工具执行生命周期回调，用于向外暴露实时事件。
type ToolExecutionCallbacks struct {
	// OnBefore 在单个工具执行前触发。
	OnBefore func(call preparedToolCall)
	// OnAfter 在单个工具执行后触发；output 已是最终展示文本（含失败容错文本）。
	OnAfter func(call preparedToolCall, output string)
}

// defaultTools 返回默认注册给模型的函数工具定义。
func defaultTools() []ToolDefinition {
	return []ToolDefinition{
		{
			Type: "function",
			Function: ToolFunction{
				Name:        "get_current_time",
				Description: "当你想知道现在时间时非常有用。",
				Parameters:  map[string]any{},
			},
		},
		{
			Type: "function",
			Function: ToolFunction{
				Name:        "get_current_weather",
				Description: "当你想查询指定城市天气时非常有用。",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"location": map[string]any{
							"type":        "string",
							"description": "城市或县区，比如北京市、杭州市、余杭区等。",
						},
					},
					"required": []string{"location"},
				},
			},
		},
	}
}

// defaultToolMapper 返回工具名到本地处理函数的映射表。
func defaultToolMapper() map[string]ToolFunc {
	// 返回值是一个映射表，键是工具函数的名称(string类型)，值是对应的本地实现函数(ToolFunc类型)。
	return map[string]ToolFunc{
		"get_current_time":    getCurrentTime,
		"get_current_weather": getCurrentWeather,
	}
}

// runReactTurn 执行带工具调用的 ReAct 对话循环；cfg.MaxReactRounds 为 0 时不限制轮数。
// 支持基于 usage 和错误的自动上下文裁剪。
func runReactTurn(client ChatProvider, cfg Config, history *[]Message, stats *TurnRuntimeStats, userInput string) (string, error) {
	tools := defaultTools()
	mapper := defaultToolMapper()

	*history = append(*history, Message{Role: "user", Content: userInput})

	reactRoundCount := 0
	for {
		if cfg.MaxReactRounds > 0 && reactRoundCount >= cfg.MaxReactRounds {
			return "", errors.New("react loop exceeded max rounds")
		}

		// 在每轮开始前检查是否需要基于 usage 进行裁剪
		if reactRoundCount > 0 && shouldTrimByUsage(*stats, cfg) {
			if err := TrimAndSummarizeHistoryContext(client, cfg, history, stats, "usage"); err != nil {
				fmt.Printf("警告: 上下文裁剪失败: %v\n", err)
			}
		}

		// 每轮都用流式请求驱动推理，正文按增量输出，工具调用在本轮结束后统一处理。
		if reactRoundCount == 0 {
			fmt.Print("助手(流式): ")
		}
		reactRoundCount++

		result, err := client.ChatStream(ChatRequest{
			Messages:          *history,
			Tools:             tools,
			ParallelToolCalls: true,
		}, func(chunk StreamChunk) {
			if chunk.Content != "" {
				fmt.Print(chunk.Content)
			}
		})

		// 处理错误并尝试重试（带裁剪）
		if err != nil {
			if shouldTrimByError(err) {
				if trimErr := TrimAndSummarizeHistoryContext(client, cfg, history, stats, "error"); trimErr != nil {
					// 裁剪失败，返回原始错误
					return "", err
				}
				// 裁剪成功，重试本轮请求
				result, err = client.ChatStream(ChatRequest{
					Messages:          *history,
					Tools:             tools,
					ParallelToolCalls: true,
				}, func(chunk StreamChunk) {
					if chunk.Content != "" {
						fmt.Print(chunk.Content)
					}
				})
				if err != nil {
					return "", err
				}
			} else {
				return "", err
			}
		}

		// 更新 usage
		if stats != nil {
			stats.LastTotalTokens = result.Usage.TotalTokens
		}

		assistant := Message{Role: "assistant", Content: result.Content, ToolCalls: result.ToolCalls}
		*history = append(*history, assistant)

		if len(assistant.ToolCalls) == 0 {
			fmt.Println()
			return assistant.Content, nil
		}

		fmt.Println()

		preparedCalls, err := prepareToolCalls(assistant.ToolCalls, mapper)
		if err != nil {
			return "", err
		}

		var logMu sync.Mutex
		callbacks := ToolExecutionCallbacks{
			OnBefore: func(call preparedToolCall) {
				logMu.Lock()
				defer logMu.Unlock()
				fmt.Printf("[工具] 开始 %s 参数=%s\n", call.ToolName, call.RawArguments)
			},
			OnAfter: func(call preparedToolCall, output string) {
				logMu.Lock()
				defer logMu.Unlock()
				fmt.Printf("[工具] 完成 %s 参数=%s 输出=%s\n", call.ToolName, call.RawArguments, output)
			},
		}

		results, err := executeToolCallsConcurrently(preparedCalls, callbacks)
		if err != nil {
			return "", err
		}

		// 按 assistant 返回顺序回填工具结果，保证上下文与日志可预测。
		for _, result := range results {
			*history = append(*history, Message{
				Role:       "tool",
				ToolCallID: result.ToolCallID,
				Content:    result.Output,
			})
		}

		fmt.Print("助手(流式): ")
	}
}

// executeToolCallsConcurrently 并发执行同一轮中已准备好的工具调用，并按输入顺序返回结果。
func executeToolCallsConcurrently(preparedCalls []preparedToolCall, callbacks ToolExecutionCallbacks) ([]toolExecutionResult, error) {
	if len(preparedCalls) == 0 {
		return []toolExecutionResult{}, nil
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
			if callbacks.OnBefore != nil {
				callbacks.OnBefore(prepared)
			}

			toolOutput, err := prepared.ToolFunc(prepared.Arguments)
			if err != nil {
				toolOutput = "工具执行失败: " + err.Error()
			}

			if callbacks.OnAfter != nil {
				callbacks.OnAfter(prepared, toolOutput)
			}

			resultCh <- indexedResult{
				index: index,
				result: toolExecutionResult{
					ToolCallID:   prepared.ToolCallID,
					ToolName:     prepared.ToolName,
					RawArguments: prepared.RawArguments,
					Output:       toolOutput,
				},
			}
		}()
	}

	wg.Wait()
	close(resultCh)

	results := make([]toolExecutionResult, len(preparedCalls))
	for item := range resultCh {
		results[item.index] = item.result
	}

	return results, nil
}

// prepareToolCalls 校验工具调用并完成参数解析，返回可直接执行的工具调用列表。
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

		preparedCalls = append(preparedCalls, preparedToolCall{
			ToolCallID:   toolCall.ID,
			ToolName:     toolCall.Function.Name,
			RawArguments: toolCall.Function.Arguments,
			Arguments:    args,
			ToolFunc:     toolFunc,
		})
	}

	return preparedCalls, nil
}

// parseToolArguments 解析工具调用参数 JSON，空字符串时返回空参数对象。
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

// getCurrentTime 返回当前本地时间字符串。
func getCurrentTime(_ map[string]any) (string, error) {
	time.Sleep(1 * time.Second)

	return fmt.Sprintf("当前时间：%s。", time.Now().Format("2006-01-02 15:04:05")), nil
}

// getCurrentWeather 根据 location 参数返回模拟天气信息。
func getCurrentWeather(arguments map[string]any) (string, error) {
	time.Sleep(2 * time.Second)

	location, _ := arguments["location"].(string)
	location = strings.TrimSpace(location)
	if location == "" {
		return "", errors.New("missing argument: location")
	}

	weather := []string{"晴天", "多云", "雨天"}
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	return fmt.Sprintf("%s今天是%s。", location, weather[r.Intn(len(weather))]), nil
}
