package llm

import (
	"context"
	"fmt"
	"strings"
)

const (
	defaultSummaryInstruction = "请将以下对话历史总结为简明扼要的摘要，保留关键信息和上下文：\n\n{{MESSAGES}}\n\n请提供摘要："
	defaultSummaryMarker      = "【历史上下文已摘要】"
)

// shouldTrimByUsage 根据 token 用量判断是否需要裁剪。
func shouldTrimByUsage(stats TurnRuntimeStats, threshold int) bool {
	if threshold <= 0 {
		return false
	}
	return stats.LastTotalTokens >= threshold
}

// countRecentRounds 计算保留最近 N 轮对话时的起始索引。
func countRecentRounds(history []Message, keepRounds int) int {
	if len(history) == 0 || keepRounds <= 0 {
		return len(history)
	}

	roundCount := 0
	for i := len(history) - 1; i > 0; i-- {
		if history[i].Role == "user" {
			roundCount++
			if roundCount >= keepRounds {
				return i
			}
		}
	}
	return 1
}

// selectTrimRange 选择可被裁剪的历史区间。
func selectTrimRange(history []Message, keepRecentRounds int) (int, int) {
	if len(history) <= 1 {
		return -1, -1
	}

	keepStart := countRecentRounds(history, keepRecentRounds)
	if keepStart < 1 {
		keepStart = 1
	}
	if keepStart > 1 {
		return 1, keepStart
	}
	return -1, -1
}

// buildSummaryPrompt 构造摘要提示词。
func buildSummaryPrompt(messages []Message) string {
	msgStrs := make([]string, 0, len(messages))
	for _, msg := range messages {
		msgStrs = append(msgStrs, fmt.Sprintf("[%s]: %s", msg.Role, msg.Content))
	}
	messagesText := strings.Join(msgStrs, "\n\n")
	return strings.ReplaceAll(defaultSummaryInstruction, "{{MESSAGES}}", messagesText)
}

// summarizeMessages 调用模型生成历史摘要。
func summarizeMessages(ctx context.Context, client ChatProvider, messages []Message, thinking *ThinkingConfig) (string, error) {
	systemMsg := Message{Role: "system", Content: "你是一个有帮助的助手。"}
	summaryPrompt := buildSummaryPrompt(messages)
	userMsg := Message{Role: "user", Content: summaryPrompt}

	resp, err := client.Chat(ctx, ChatRequest{Messages: []Message{systemMsg, userMsg}, Thinking: cloneThinkingConfig(thinking)})
	if err != nil {
		return "", fmt.Errorf("failed to summarize messages: %w", err)
	}

	return extractAssistantText(resp), nil
}

// spliceSummaryIntoHistory 把摘要写回历史。
func spliceSummaryIntoHistory(history *[]Message, summary string, keepRecentRounds int) error {
	if len(*history) <= 1 {
		return fmt.Errorf("history too short to trim")
	}

	keepStart := countRecentRounds(*history, keepRecentRounds)
	if keepStart < 1 {
		keepStart = 1
	}

	summaryContent := defaultSummaryMarker + "\n" + summary
	summaryMsg := Message{Role: "assistant", Content: summaryContent}
	newHistory := []Message{(*history)[0], summaryMsg}
	newHistory = append(newHistory, (*history)[keepStart:]...)
	*history = newHistory
	return nil
}

// trimAndSummarizeHistoryContext 执行历史裁剪和摘要。
func trimAndSummarizeHistoryContext(ctx context.Context, client ChatProvider, keepRecentRounds int, history *[]Message, stats *TurnRuntimeStats, trimReason string, thinking *ThinkingConfig) error {
	if keepRecentRounds <= 0 {
		return nil
	}

	startIdx, endIdx := selectTrimRange(*history, keepRecentRounds)
	if startIdx < 0 || endIdx <= startIdx {
		return nil
	}

	toSummarize := (*history)[startIdx:endIdx]
	summary, err := summarizeMessages(ctx, client, toSummarize, thinking)
	if err != nil {
		return fmt.Errorf("summarization failed: %w", err)
	}

	if err := spliceSummaryIntoHistory(history, summary, keepRecentRounds); err != nil {
		return fmt.Errorf("failed to splice summary: %w", err)
	}

	if stats != nil {
		stats.LastTrimReason = trimReason
	}
	return nil
}
