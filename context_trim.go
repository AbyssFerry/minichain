package main

import (
	"fmt"
	"strings"
)

// shouldTrimByUsage 根据上一轮的 token 用量判断是否需要裁剪。
func shouldTrimByUsage(stats TurnRuntimeStats, cfg Config) bool {
	if cfg.ContextTrimTokenThreshold <= 0 {
		return false
	}
	return stats.LastTotalTokens >= cfg.ContextTrimTokenThreshold
}

// shouldTrimByError 判断错误是否由超上下文长度引起。
func shouldTrimByError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	patterns := []string{
		"context length",
		"context_length_exceeded",
		"token limit",
		"tokens are too many",
		"exceed",
		"maximum",
	}
	for _, pattern := range patterns {
		if strings.Contains(errStr, pattern) {
			return true
		}
	}
	return false
}

// countRecentRounds 计算保留最近 N 轮对话需要保留的消息起始索引。
// 返回值表示从 index 开始的消息需要被保留。
// 一轮 = 1 个 user 消息 + 1 个（或多个）响应消息（assistant/tool）。
func countRecentRounds(history []Message, keepRounds int) int {
	if len(history) == 0 || keepRounds <= 0 {
		return len(history)
	}

	// 从末尾向前扫描，计数 user 消息（每个 user 消息代表一轮新的交互）
	roundCount := 0
	for i := len(history) - 1; i > 0; i-- { // 保留 history[0] (系统提示词)，所以 i > 0
		if history[i].Role == "user" {
			roundCount++
			if roundCount >= keepRounds {
				return i
			}
		}
	}
	// 如果找不到足够的轮数，从 index 1 开始保留
	return 1
}

// selectTrimRange 选择可被裁剪的消息范围。
// 返回 (startIndex, endIndex)，表示 history[startIndex:endIndex] 可被摘要替换。
// history[0] 永远不被裁剪（系统消息）；[startIndex:endIndex] 之外的消息保持不动。
func selectTrimRange(history []Message, keepRecentRounds int) (int, int) {
	if len(history) <= 1 {
		return -1, -1 // 无法裁剪
	}

	// 计算保留的最近消息起始位置
	keepStart := countRecentRounds(history, keepRecentRounds)
	if keepStart < 1 {
		keepStart = 1
	}

	// 可裁剪的范围是 [1, keepStart)，返回 (1, keepStart)
	if keepStart > 1 {
		return 1, keepStart
	}

	return -1, -1 // 无法裁剪
}

// buildSummaryPrompt 构造用于摘要的请求提示，包含待摘要的消息内容。
func buildSummaryPrompt(cfg PromptConfig, messages []Message) string {
	var msgStrs []string
	for _, msg := range messages {
		role := msg.Role
		if role == "tool" {
			// tool 消息不需要详细展示，只需要标记
			msgStrs = append(msgStrs, fmt.Sprintf("[%s]: %s", role, msg.Content))
		} else {
			msgStrs = append(msgStrs, fmt.Sprintf("[%s]: %s", role, msg.Content))
		}
	}

	messagesText := strings.Join(msgStrs, "\n\n")
	return strings.ReplaceAll(cfg.SummaryInstruction, "{{MESSAGES}}", messagesText)
}

// summarizeMessages 调用模型对指定的消息列表进行摘要。
// 注意：摘要请求本身不会再次被裁剪，以避免递归。
func summarizeMessages(client ChatProvider, cfg Config, messages []Message) (string, error) {
	// 构造摘要请求：系统提示词 + 摘要指令
	systemMsg := Message{Role: "system", Content: cfg.Prompts.SystemPrompt}
	summaryPrompt := buildSummaryPrompt(cfg.Prompts, messages)
	userMsg := Message{Role: "user", Content: summaryPrompt}

	req := ChatRequest{
		Messages: []Message{systemMsg, userMsg},
	}

	resp, err := client.Chat(req)
	if err != nil {
		return "", fmt.Errorf("failed to summarize messages: %w", err)
	}

	summary := extractAssistantText(resp)
	return summary, nil
}

// spliceSummaryIntoHistory 将摘要消息插入到历史记录中间。
// 保留 history[0]（系统消息）+ 中间的摘要消息 + 最近的对话消息。
// summary 将以 assistant 角色插入，content 格式为：PromptConfig.SummaryMarker + "\n" + summary
func spliceSummaryIntoHistory(history *[]Message, summary string, cfg PromptConfig, keepRecentRounds int) error {
	if len(*history) <= 1 {
		return fmt.Errorf("history too short to trim")
	}

	// 计算保留的最近消息起始位置
	keepStart := countRecentRounds(*history, keepRecentRounds)
	if keepStart < 1 {
		keepStart = 1
	}

	// 构造摘要消息
	summaryContent := cfg.SummaryMarker + "\n" + summary
	summaryMsg := Message{Role: "assistant", Content: summaryContent}

	// 新历史 = [系统消息] + [摘要消息] + [最近对话]
	newHistory := []Message{(*history)[0], summaryMsg}
	newHistory = append(newHistory, (*history)[keepStart:]...)

	*history = newHistory
	return nil
}

// TrimAndSummarizeHistoryContext 是上下文裁剪的主接口函数。
// 根据条件判断是否需要裁剪，若需要则调用摘要并回填。
func TrimAndSummarizeHistoryContext(
	client ChatProvider,
	cfg Config,
	history *[]Message,
	stats *TurnRuntimeStats,
	trimReason string, // "usage" 或 "error"
) error {
	if cfg.ContextKeepRecentRounds <= 0 {
		return nil
	}

	startIdx, endIdx := selectTrimRange(*history, cfg.ContextKeepRecentRounds)
	if startIdx < 0 || endIdx <= startIdx {
		return nil // 无法或无需裁剪
	}

	// 抽取待摘要的消息范围
	toSummarize := (*history)[startIdx:endIdx]

	// 调用模型进行摘要（摘要请求本身不再走裁剪）
	summary, err := summarizeMessages(client, cfg, toSummarize)
	if err != nil {
		return fmt.Errorf("summarization failed: %w", err)
	}

	// 将摘要插入历史
	if err := spliceSummaryIntoHistory(history, summary, cfg.Prompts, cfg.ContextKeepRecentRounds); err != nil {
		return fmt.Errorf("failed to splice summary: %w", err)
	}

	// 记录裁剪原因
	if stats != nil {
		stats.LastTrimReason = trimReason
	}

	fmt.Printf("[context] 已触发历史摘要裁剪（原因: %s）\n", trimReason)
	return nil
}

// RetryWithTrim 尝试在裁剪后重新执行请求（仅重试 1 次）。
// 调用者应提供一个返回(response, usage, error)的请求函数。
func RetryWithTrim(
	client ChatProvider,
	cfg Config,
	history *[]Message,
	stats *TurnRuntimeStats,
	requestFn func() (interface{}, Usage, error),
) (interface{}, Usage, error) {
	// 第一次尝试
	resp, usage, err := requestFn()
	if err == nil {
		// 成功，更新 stats
		if stats != nil {
			stats.LastTotalTokens = usage.TotalTokens
		}
		return resp, usage, nil
	}

	// 检查是否由超上下文引起
	if !shouldTrimByError(err) {
		return nil, usage, err
	}

	// 尝试裁剪
	if trimErr := TrimAndSummarizeHistoryContext(client, cfg, history, stats, "error"); trimErr != nil {
		// 裁剪失败，返回原始错误
		return nil, usage, err
	}

	// 重试一次（仅 1 次）
	resp, usage, err = requestFn()
	if err == nil && stats != nil {
		stats.LastTotalTokens = usage.TotalTokens
	}
	return resp, usage, err
}
