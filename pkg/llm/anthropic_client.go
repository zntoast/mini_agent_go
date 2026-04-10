package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/zntoast/mini_agent/pkg/schema"
)

type AnthropicClient struct {
	APIKey        string
	APIBase       string
	Model         string
	RetryConfig   *RetryConfig
	RetryCallback func(error, int)
}

func NewAnthropicClient(apiKey, apiBase, model string, retryConfig *RetryConfig) *AnthropicClient {
	return &AnthropicClient{
		APIKey:      apiKey,
		APIBase:     apiBase,
		Model:       model,
		RetryConfig: retryConfig,
	}
}

type AnthropicMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"`
}

type AnthropicContentBlock struct {
	Type     string                 `json:"type"`
	Text     string                 `json:"text,omitempty"`
	Thinking string                 `json:"thinking,omitempty"`
	ID       string                 `json:"id,omitempty"`
	Name     string                 `json:"name,omitempty"`
	Input    map[string]interface{} `json:"input,omitempty"`
}

type AnthropicToolUse struct {
	Type  string                 `json:"type"`
	ID    string                 `json:"id"`
	Name  string                 `json:"name"`
	Input map[string]interface{} `json:"input"`
}

type AnthropicToolResult struct {
	Type      string `json:"type"`
	ToolUseID string `json:"tool_use_id"`
	Content   string `json:"content"`
}

type AnthropicRequest struct {
	Model     string                   `json:"model"`
	MaxTokens int                      `json:"max_tokens"`
	Messages  []AnthropicMessage       `json:"messages"`
	System    string                   `json:"system,omitempty"`
	Tools     []map[string]interface{} `json:"tools,omitempty"`
}

type AnthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type AnthropicResponse struct {
	ID         string         `json:"id"`
	Type       string         `json:"type"`
	Role       string         `json:"role"`
	Content    []interface{}  `json:"content"`
	StopReason string         `json:"stop_reason"`
	Usage      AnthropicUsage `json:"usage"`
}

func (c *AnthropicClient) Generate(messages []*schema.Message, tools []interface{}) (*schema.LLMResponse, error) {
	systemMsg, apiMessages := c.convertMessages(messages)

	requestBody := AnthropicRequest{
		Model:     c.Model,
		MaxTokens: 16384,
		Messages:  apiMessages,
	}

	if systemMsg != "" {
		requestBody.System = systemMsg
	}

	if len(tools) > 0 {
		requestBody.Tools = c.convertTools(tools)
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := c.APIBase + "/v1/messages"

	var lastErr error
	maxAttempts := 1
	if c.RetryConfig != nil && c.RetryConfig.Enabled {
		maxAttempts = c.RetryConfig.MaxRetries + 1
	}

	for attempt := 0; attempt < maxAttempts; attempt++ {
		req, err := http.NewRequestWithContext(context.Background(), "POST", url, bytes.NewBuffer(jsonBody))
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-api-key", c.APIKey)
		req.Header.Set("anthropic-version", "2023-06-01")
		req.Header.Set("Authorization", "Bearer "+c.APIKey)

		client := &http.Client{Timeout: 120 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			if attempt < maxAttempts-1 && c.RetryConfig != nil && c.RetryConfig.Enabled {
				delay := c.RetryConfig.CalculateDelay(attempt)
				if c.RetryCallback != nil {
					c.RetryCallback(err, attempt+1)
				}
				time.Sleep(time.Duration(delay * float64(time.Second)))
				continue
			}
			return nil, fmt.Errorf("request failed: %w", err)
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read response: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
			if attempt < maxAttempts-1 && c.RetryConfig != nil && c.RetryConfig.Enabled {
				delay := c.RetryConfig.CalculateDelay(attempt)
				if c.RetryCallback != nil {
					c.RetryCallback(lastErr, attempt+1)
				}
				time.Sleep(time.Duration(delay * float64(time.Second)))
				continue
			}
			return nil, lastErr
		}

		var anthropicResp AnthropicResponse
		if err := json.Unmarshal(body, &anthropicResp); err != nil {
			return nil, fmt.Errorf("failed to parse response: %w", err)
		}

		return c.parseResponse(&anthropicResp), nil
	}

	return nil, &RetryExhaustedError{LastException: lastErr, Attempts: maxAttempts}
}

func (c *AnthropicClient) convertMessages(messages []*schema.Message) (string, []AnthropicMessage) {
	var systemMessage string
	var result []AnthropicMessage

	for _, msg := range messages {
		if msg.Role == "system" {
			if s, ok := msg.Content.(string); ok {
				systemMessage = s
			}
			continue
		}

		if msg.Role == "user" {
			content := msg.Content
			if s, ok := msg.Content.(string); ok {
				content = s
			} else {
				content = fmt.Sprintf("%v", msg.Content)
			}
			result = append(result, AnthropicMessage{
				Role:    "user",
				Content: content,
			})
		} else if msg.Role == "assistant" {
			if msg.Thinking != nil || len(msg.ToolCalls) > 0 {
				var contentBlocks []interface{}

				if msg.Thinking != nil {
					contentBlocks = append(contentBlocks, map[string]interface{}{
						"type":     "thinking",
						"thinking": *msg.Thinking,
					})
				}

				if msg.Content != nil {
					contentBlocks = append(contentBlocks, map[string]interface{}{
						"type": "text",
						"text": msg.Content,
					})
				}

				for _, tc := range msg.ToolCalls {
					contentBlocks = append(contentBlocks, map[string]interface{}{
						"type":  "tool_use",
						"id":    tc.ID,
						"name":  tc.Function.Name,
						"input": tc.Function.Arguments,
					})
				}

				result = append(result, AnthropicMessage{
					Role:    "assistant",
					Content: contentBlocks,
				})
			} else {
				content := msg.Content
				if s, ok := msg.Content.(string); ok {
					content = s
				} else {
					content = fmt.Sprintf("%v", msg.Content)
				}
				result = append(result, AnthropicMessage{
					Role:    "assistant",
					Content: content,
				})
			}
		} else if msg.Role == "tool" {
			result = append(result, AnthropicMessage{
				Role: "user",
				Content: []map[string]interface{}{
					{
						"type":        "tool_result",
						"tool_use_id": *msg.ToolCallID,
						"content":     msg.Content,
					},
				},
			})
		}
	}

	return systemMessage, result
}

func (c *AnthropicClient) convertTools(tools []interface{}) []map[string]interface{} {
	var result []map[string]interface{}

	for _, t := range tools {
		switch tool := t.(type) {
		case Tool:
			result = append(result, map[string]interface{}{
				"name":         tool.GetName(),
				"description":  tool.GetDescription(),
				"input_schema": tool.GetParameters(),
			})
		case map[string]interface{}:
			result = append(result, tool)
		}
	}

	return result
}

func (c *AnthropicClient) parseResponse(resp *AnthropicResponse) *schema.LLMResponse {
	var textContent string
	var thinkingContent string
	var toolCalls []*schema.ToolCall

	for _, block := range resp.Content {
		switch b := block.(type) {
		case map[string]interface{}:
			if t, ok := b["type"].(string); ok {
				switch t {
				case "text":
					if text, ok := b["text"].(string); ok {
						textContent += text
					}
				case "thinking":
					if thinking, ok := b["thinking"].(string); ok {
						thinkingContent += thinking
					}
				case "tool_use":
					id, _ := b["id"].(string)
					name, _ := b["name"].(string)
					input, _ := b["input"].(map[string]interface{})

					toolCalls = append(toolCalls, &schema.ToolCall{
						ID:   id,
						Type: "function",
						Function: schema.FunctionCall{
							Name:      name,
							Arguments: input,
						},
					})
				}
			}
		}
	}

	stopReason := resp.StopReason
	if stopReason == "" {
		stopReason = "stop"
	}

	var thinking *string
	if thinkingContent != "" {
		thinking = &thinkingContent
	}

	return &schema.LLMResponse{
		Content:      textContent,
		Thinking:     thinking,
		ToolCalls:    toolCalls,
		FinishReason: stopReason,
		Usage: &schema.TokenUsage{
			PromptTokens:     resp.Usage.InputTokens,
			CompletionTokens: resp.Usage.OutputTokens,
			TotalTokens:      resp.Usage.InputTokens + resp.Usage.OutputTokens,
		},
	}
}
