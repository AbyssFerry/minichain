package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type OpenAICompatibleClient struct {
	// model 是默认请求使用的模型名称。
	model string
	// apiKey 是请求鉴权使用的 API Key。
	apiKey string
	// endpoint 是最终请求地址，形如 /chat/completions。
	endpoint string
	// httpClient 负责实际的 HTTP 请求发送与超时控制。
	httpClient *http.Client
}

// NewOpenAICompatibleClient 根据配置创建 OpenAI 兼容客户端并规范化请求端点。
func NewOpenAICompatibleClient(cfg Config) (*OpenAICompatibleClient, error) {
	endpoint := strings.TrimRight(cfg.BaseURL, "/")
	if !strings.HasSuffix(endpoint, "/chat/completions") {
		endpoint += "/chat/completions"
	}

	return &OpenAICompatibleClient{
		model:    strings.TrimSpace(cfg.Model),
		apiKey:   strings.TrimSpace(cfg.APIKey),
		endpoint: endpoint,
		httpClient: &http.Client{
			Timeout: 90 * time.Second,
		},
	}, nil
}

// Chat 发送非流式对话请求并解析完整 JSON 响应。
func (c *OpenAICompatibleClient) Chat(req ChatRequest) (*chatAPIResponse, error) {
	body := chatAPIRequest{
		Model:             c.model,
		Messages:          req.Messages,
		Tools:             req.Tools,
		ParallelToolCalls: req.ParallelToolCalls,
		ToolChoice:        req.ToolChoice,
	}

	respBody, err := c.sendJSON(body)
	if err != nil {
		return nil, err
	}

	var parsed chatAPIResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse response JSON: %w", err)
	}

	return &parsed, nil
}

// ChatStream 发送流式对话请求，按 SSE 数据块回调并聚合结果。
func (c *OpenAICompatibleClient) ChatStream(req ChatRequest, onChunk func(StreamChunk)) (StreamResult, error) {
	body := chatAPIRequest{
		Model:             c.model,
		Messages:          req.Messages,
		Stream:            true,
		Tools:             req.Tools,
		ParallelToolCalls: req.ParallelToolCalls,
		ToolChoice:        req.ToolChoice,
		StreamOptions: &StreamOptions{
			IncludeUsage: true,
		},
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return StreamResult{}, fmt.Errorf("failed to marshal request body: %w", err)
	}

	httpReq, err := http.NewRequest("POST", c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return StreamResult{}, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return StreamResult{}, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(resp.Body)
		return StreamResult{}, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(errBody)))
	}

	scanner := bufio.NewScanner(resp.Body)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	var fullContent strings.Builder
	var fullReasoning strings.Builder
	toolCallsByIndex := make(map[int]ToolCall)
	toolCallOrder := make([]int, 0)
	var usage Usage

	for scanner.Scan() {
		// 逐行处理 SSE 帧，仅消费 data: 前缀的数据行。
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}

		// 去掉行首的 "data:"，再去掉多余空格，得到真正的 JSON 数据
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}

		// 每个 data 字段都是一段独立 JSON 增量，容忍坏块并继续读取。
		var chunk chatAPIResponse
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		if len(chunk.Choices) > 0 {
			delta := chunk.Choices[0].Delta
			if len(delta.ToolCalls) > 0 {
				mergeStreamToolCalls(toolCallsByIndex, &toolCallOrder, delta.ToolCalls)
			}

			if delta.ReasoningContent != "" {
				fullReasoning.WriteString(delta.ReasoningContent)
				if onChunk != nil {
					onChunk(StreamChunk{ReasoningContent: delta.ReasoningContent})
				}
			}

			if delta.Content != "" {
				// 拼到 fullContent 里，作为完整文本写入历史记录。
				fullContent.WriteString(delta.Content)
				if onChunk != nil {
					onChunk(StreamChunk{Content: delta.Content})
				}
			}
		} else if chunk.Usage.TotalTokens > 0 {
			// 只有在流式结束时才会返回 usage 数据，提前的增量块里通常没有。
			usage = chunk.Usage
		}
	}

	if err := scanner.Err(); err != nil {
		return StreamResult{}, fmt.Errorf("failed to read stream: %w", err)
	}

	toolCalls := make([]ToolCall, 0, len(toolCallOrder))
	for _, idx := range toolCallOrder {
		toolCalls = append(toolCalls, toolCallsByIndex[idx])
	}

	return StreamResult{
		Content:          fullContent.String(),
		ReasoningContent: fullReasoning.String(),
		ToolCalls:        toolCalls,
		Usage:            usage,
	}, nil
}

// mergeStreamToolCalls 合并流式增量中的 tool_calls 片段，按 index 聚合完整参数。
func mergeStreamToolCalls(toolCallsByIndex map[int]ToolCall, toolCallOrder *[]int, deltas []ToolCall) {
	for _, deltaCall := range deltas {
		index := deltaCall.Index
		existing, found := toolCallsByIndex[index]
		// 如果不存在当前 index 的调用记录，先创建一个空的占位对象，并记录顺序以便最后输出时排序。
		if !found {
			existing = ToolCall{Index: index}
			*toolCallOrder = append(*toolCallOrder, index)
		}

		if deltaCall.ID != "" {
			existing.ID = deltaCall.ID
		}
		if deltaCall.Type != "" {
			existing.Type = deltaCall.Type
		}
		if deltaCall.Function.Name != "" {
			existing.Function.Name = deltaCall.Function.Name
		}
		if deltaCall.Function.Arguments != "" {
			// 流式增量的参数通常是分片传输的，需要累加到现有参数后面。
			existing.Function.Arguments += deltaCall.Function.Arguments
		}

		toolCallsByIndex[index] = existing
	}
}

// sendJSON 发送标准 JSON POST 请求并返回响应字节。
func (c *OpenAICompatibleClient) sendJSON(body chatAPIRequest) ([]byte, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	httpReq, err := http.NewRequest("POST", c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	bodyText, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(bodyText)))
	}

	return bodyText, nil
}

// extractAssistantText 从 API 响应中提取首个助手文本内容。
func extractAssistantText(resp *chatAPIResponse) string {
	if resp == nil || len(resp.Choices) == 0 {
		return ""
	}
	return resp.Choices[0].Message.Content
}
