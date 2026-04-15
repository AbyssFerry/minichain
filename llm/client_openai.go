package llm
import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// openAICompatibleClient 是 OpenAI 兼容协议客户端实现。
type openAICompatibleClient struct {
	// model 是默认请求模型。
	model string
	// apiKey 是鉴权密钥。
	apiKey string
	// endpoint 是完整请求地址。
	endpoint string
	// httpClient 执行 HTTP 请求。
	httpClient *http.Client
}

// newOpenAICompatibleClient 创建 OpenAI 兼容协议客户端。
func newOpenAICompatibleClient(model, apiKey, baseURL string, timeout time.Duration) (*openAICompatibleClient, error) {
	if timeout <= 0 {
		timeout = defaultTurnTimeout
	}

	endpoint := strings.TrimRight(baseURL, "/")
	if !strings.HasSuffix(endpoint, "/chat/completions") {
		endpoint += "/chat/completions"
	}

	return &openAICompatibleClient{
		model:    strings.TrimSpace(model),
		apiKey:   strings.TrimSpace(apiKey),
		endpoint: endpoint,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}, nil
}

// Chat 发送非流式请求并解析 JSON 响应。
func (c *openAICompatibleClient) Chat(ctx context.Context, req ChatRequest) (*chatAPIResponse, error) {
	body := c.buildRequest(req, false)
	respBody, err := c.sendJSON(ctx, body)
	if err != nil {
		return nil, err
	}

	var parsed chatAPIResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse response JSON: %w", err)
	}
	if parsed.Error != nil {
		return nil, fmt.Errorf("request failed: %s", formatChatAPIError(parsed.Error))
	}

	return &parsed, nil
}

// ChatStream 发送流式请求并返回可遍历的流式结果。
func (c *openAICompatibleClient) ChatStream(ctx context.Context, req ChatRequest) (*StreamResult, error) {
	result := newStreamResult()

	go func() {
		body := c.buildRequest(req, true)
		payload, err := json.Marshal(body)
		if err != nil {
			result.finish(StreamSummary{}, fmt.Errorf("failed to marshal request body: %w", err))
			return
		}

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
		if err != nil {
			result.finish(StreamSummary{}, fmt.Errorf("failed to create request: %w", err))
			return
		}
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
		httpReq.Header.Set("Content-Type", "application/json")

		resp, err := c.httpClient.Do(httpReq)
		if err != nil {
			result.finish(StreamSummary{}, fmt.Errorf("request failed: %w", err))
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			errBody, _ := io.ReadAll(resp.Body)
			result.finish(StreamSummary{}, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(errBody))))
			return
		}

		scanner := bufio.NewScanner(resp.Body)
		buf := make([]byte, 0, 64*1024)
		scanner.Buffer(buf, 1024*1024)

		var fullContent strings.Builder
		toolCallsByIndex := make(map[int]ToolCall)
		toolCallOrder := make([]int, 0)
		var usage Usage
		finishReason := ""
		responseID := ""
		responseModelName := ""
		systemFingerprint := ""
		refusal := ""

		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || !strings.HasPrefix(line, "data:") {
				continue
			}

			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "[DONE]" {
				break
			}

			var chunk chatAPIResponse
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue
			}
			if chunk.Error != nil {
				result.finish(StreamSummary{}, fmt.Errorf("request failed: %s", formatChatAPIError(chunk.Error)))
				return
			}

			if chunk.ID != "" {
				responseID = chunk.ID
			}
			if chunk.Model != "" {
				responseModelName = chunk.Model
			}
			if chunk.SystemFingerprint != "" {
				systemFingerprint = chunk.SystemFingerprint
			}

			if len(chunk.Choices) > 0 {
				choice := chunk.Choices[0]
				if choice.FinishReason != "" {
					finishReason = choice.FinishReason
				}
				if choice.Refusal != nil {
					refusal = fmt.Sprint(choice.Refusal)
				}
				delta := choice.Delta
				if strings.TrimSpace(delta.Refusal) != "" {
					refusal = delta.Refusal
				}
				if len(delta.ToolCalls) > 0 {
					mergeStreamToolCalls(toolCallsByIndex, &toolCallOrder, delta.ToolCalls)
				}
				if delta.Content != "" {
					fullContent.WriteString(delta.Content)
					result.events <- StreamEvent{Type: "content", Content: delta.Content, FinishReason: finishReason}
				}
			} else if chunk.Usage.TotalTokens > 0 {
				usage = chunk.Usage
			}
		}

		if err := scanner.Err(); err != nil {
			result.finish(StreamSummary{}, fmt.Errorf("failed to read stream: %w", err))
			return
		}

		toolCalls := make([]ToolCall, 0, len(toolCallOrder))
		for _, idx := range toolCallOrder {
			toolCalls = append(toolCalls, toolCallsByIndex[idx])
		}

		additionalKwargs := map[string]any{"refusal": nil}
		if strings.TrimSpace(refusal) != "" {
			additionalKwargs["refusal"] = refusal
		}

		result.finish(StreamSummary{
			Content:          fullContent.String(),
			ToolCalls:        toolCalls,
			Usage:            usage,
			ID:               responseID,
			ModelName:        responseModelName,
			AdditionalKwargs: additionalKwargs,
			ResponseMetadata: buildResponseMetadata(responseID, responseModelName, finishReason, systemFingerprint, nil, usage),
			UsageMetadata:    buildUsageMetadata(usage),
			FinishReason:     finishReason,
		}, nil)
	}()

	return result, nil
}

// buildRequest 构建兼容接口请求体。
func (c *openAICompatibleClient) buildRequest(req ChatRequest, stream bool) chatAPIRequest {
	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = c.model
	}
	return chatAPIRequest{
		Model:             model,
		Messages:          cloneMessages(req.Messages),
		Temperature:       req.Temperature,
		TopP:              req.TopP,
		MaxTokens:         req.MaxTokens,
		Stop:              append([]string(nil), req.Stop...),
		PresencePenalty:   req.PresencePenalty,
		FrequencyPenalty:  req.FrequencyPenalty,
		Seed:              req.Seed,
		Stream:            stream,
		Tools:             cloneToolDefinitions(req.Tools),
		ParallelToolCalls: req.ParallelToolCalls,
		ToolChoice:        req.ToolChoice,
		StreamOptions:     req.StreamOptions,
	}
}

// sendJSON 发送标准 JSON 请求并返回响应字节。
func (c *openAICompatibleClient) sendJSON(ctx context.Context, body chatAPIRequest) ([]byte, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
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

// mergeStreamToolCalls 合并流式工具调用增量。
func mergeStreamToolCalls(toolCallsByIndex map[int]ToolCall, toolCallOrder *[]int, deltas []ToolCall) {
	for _, deltaCall := range deltas {
		index := deltaCall.Index
		existing, found := toolCallsByIndex[index]
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
			existing.Function.Arguments += deltaCall.Function.Arguments
		}

		toolCallsByIndex[index] = existing
	}
}

// extractAssistantText 提取首个 assistant 文本。
func extractAssistantText(resp *chatAPIResponse) string {
	if resp == nil || len(resp.Choices) == 0 {
		return ""
	}
	return resp.Choices[0].Message.Content
}

// cloneToolDefinitions 返回工具定义副本。
func cloneToolDefinitions(definitions []ToolDefinition) []ToolDefinition {
	if len(definitions) == 0 {
		return nil
	}
	result := make([]ToolDefinition, len(definitions))
	copy(result, definitions)
	return result
}

// formatChatAPIError 把兼容接口错误对象格式化为可读文本。
func formatChatAPIError(apiErr *chatAPIError) string {
	if apiErr == nil {
		return "unknown error"
	}
	message := strings.TrimSpace(apiErr.Message)
	if message == "" {
		message = "unknown error"
	}
	if strings.TrimSpace(apiErr.Code) != "" {
		return fmt.Sprintf("%s (code=%s)", message, apiErr.Code)
	}
	return message
}

// buildUsageMetadata 把 usage 映射为统一元数据结构。
func buildUsageMetadata(usage Usage) map[string]any {
	return map[string]any{
		"input_tokens":         usage.PromptTokens,
		"output_tokens":        usage.CompletionTokens,
		"total_tokens":         usage.TotalTokens,
		"input_token_details":  usage.PromptTokensDetails,
		"output_token_details": usage.CompletionTokensDetails,
	}
}

// buildResponseMetadata 构造响应元数据。
func buildResponseMetadata(id, modelName, finishReason, systemFingerprint string, logprobs any, usage Usage) map[string]any {
	return map[string]any{
		"token_usage": map[string]any{
			"prompt_tokens":             usage.PromptTokens,
			"completion_tokens":         usage.CompletionTokens,
			"total_tokens":              usage.TotalTokens,
			"completion_tokens_details": usage.CompletionTokensDetails,
			"prompt_tokens_details":     usage.PromptTokensDetails,
		},
		"model_provider":     "openai",
		"model_name":         modelName,
		"system_fingerprint": systemFingerprint,
		"id":                 id,
		"finish_reason":      finishReason,
		"logprobs":           logprobs,
	}
}

