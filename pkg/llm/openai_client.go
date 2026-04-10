package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/zntoast/mini_agent/pkg/schema"
)

type OpenAIClient struct {
	APIKey        string
	APIBase       string
	Model         string
	RetryConfig   *RetryConfig
	RetryCallback func(error, int)
}

func NewOpenAIClient(apiKey, apiBase, model string, retryConfig *RetryConfig) *OpenAIClient {
	return &OpenAIClient{
		APIKey:      apiKey,
		APIBase:     apiBase,
		Model:       model,
		RetryConfig: retryConfig,
	}
}

type OpenAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type OpenAIFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type OpenAIToolCall struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"`
	Function OpenAIFunctionCall `json:"function"`
}

type OpenAIMessageResponse struct {
	Content          string           `json:"content"`
	ToolCalls        []OpenAIToolCall `json:"tool_calls,omitempty"`
	ReasoningDetails []struct {
		Text string `json:"text"`
	} `json:"reasoning_details,omitempty"`
}

type OpenAIChoice struct {
	Message      OpenAIMessageResponse `json:"message"`
	FinishReason string                `json:"finish_reason"`
}

type OpenAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type OpenAIResponse struct {
	Choices []OpenAIChoice `json:"choices"`
	Usage   OpenAIUsage    `json:"usage"`
}

func (c *OpenAIClient) Generate(messages []*schema.Message, tools []interface{}) (*schema.LLMResponse, error) {
	apiMessages := c.convertMessages(messages)

	requestBody := map[string]interface{}{
		"model":    c.Model,
		"messages": apiMessages,
		"extra_body": map[string]interface{}{
			"reasoning_split": true,
		},
	}

	if len(tools) > 0 {
		requestBody["tools"] = c.convertTools(tools)
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := c.APIBase + "/chat/completions"

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

		var openAIResp OpenAIResponse
		if err := json.Unmarshal(body, &openAIResp); err != nil {
			return nil, fmt.Errorf("failed to parse response: %w", err)
		}

		return c.parseResponse(&openAIResp), nil
	}

	return nil, &RetryExhaustedError{LastException: lastErr, Attempts: maxAttempts}
}

func (c *OpenAIClient) convertMessages(messages []*schema.Message) []map[string]interface{} {
	var result []map[string]interface{}

	for _, msg := range messages {
		if msg.Role == "system" {
			result = append(result, map[string]interface{}{
				"role":    "system",
				"content": msg.Content,
			})
			continue
		}

		if msg.Role == "user" {
			content := msg.Content
			if s, ok := msg.Content.(string); ok {
				content = s
			} else {
				content = fmt.Sprintf("%v", msg.Content)
			}
			result = append(result, map[string]interface{}{
				"role":    "user",
				"content": content,
			})
		} else if msg.Role == "assistant" {
			assistantMsg := map[string]interface{}{
				"role": "assistant",
			}

			if msg.Content != nil {
				assistantMsg["content"] = msg.Content
			}

			if msg.Thinking != nil {
				assistantMsg["reasoning_details"] = []map[string]string{
					{"text": *msg.Thinking},
				}
			}

			if len(msg.ToolCalls) > 0 {
				var toolCalls []map[string]interface{}
				for _, tc := range msg.ToolCalls {
					argsJSON, _ := json.Marshal(tc.Function.Arguments)
					toolCalls = append(toolCalls, map[string]interface{}{
						"id":   tc.ID,
						"type": "function",
						"function": map[string]interface{}{
							"name":      tc.Function.Name,
							"arguments": string(argsJSON),
						},
					})
				}
				assistantMsg["tool_calls"] = toolCalls
			}

			result = append(result, assistantMsg)
		} else if msg.Role == "tool" {
			result = append(result, map[string]interface{}{
				"role":         "tool",
				"tool_call_id": *msg.ToolCallID,
				"content":      msg.Content,
			})
		}
	}

	return result
}

func (c *OpenAIClient) convertTools(tools []interface{}) []map[string]interface{} {
	var result []map[string]interface{}

	for _, t := range tools {
		switch tool := t.(type) {
		case Tool:
			result = append(result, map[string]interface{}{
				"type": "function",
				"function": map[string]interface{}{
					"name":        tool.GetName(),
					"description": tool.GetDescription(),
					"parameters":  tool.GetParameters(),
				},
			})
		case map[string]interface{}:
			if tool["type"] == "function" {
				result = append(result, tool)
			} else if name, ok := tool["name"].(string); ok {
				result = append(result, map[string]interface{}{
					"type": "function",
					"function": map[string]interface{}{
						"name":        name,
						"description": tool["description"],
						"parameters":  tool["input_schema"],
					},
				})
			}
		}
	}

	return result
}

func (c *OpenAIClient) parseResponse(resp *OpenAIResponse) *schema.LLMResponse {
	if len(resp.Choices) == 0 {
		return &schema.LLMResponse{
			Content:      "",
			FinishReason: "stop",
		}
	}

	choice := resp.Choices[0]

	content := choice.Message.Content
	if content == "" {
		content = ""
	}

	var thinking *string
	if len(choice.Message.ReasoningDetails) > 0 {
		thinkingStr := choice.Message.ReasoningDetails[0].Text
		thinking = &thinkingStr
	}

	var toolCalls []*schema.ToolCall
	if len(choice.Message.ToolCalls) > 0 {
		for _, tc := range choice.Message.ToolCalls {
			args := make(map[string]interface{})
			json.Unmarshal([]byte(tc.Function.Arguments), &args)

			toolCalls = append(toolCalls, &schema.ToolCall{
				ID:   tc.ID,
				Type: "function",
				Function: schema.FunctionCall{
					Name:      tc.Function.Name,
					Arguments: args,
				},
			})
		}
	}

	finishReason := choice.FinishReason
	if finishReason == "" {
		finishReason = "stop"
	}

	return &schema.LLMResponse{
		Content:      content,
		Thinking:     thinking,
		ToolCalls:    toolCalls,
		FinishReason: finishReason,
		Usage: &schema.TokenUsage{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		},
	}
}

func ConvertContentToString(content interface{}) string {
	switch v := content.(type) {
	case string:
		return v
	case []interface{}:
		var parts []string
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				if text, ok := m["text"].(string); ok {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	return fmt.Sprintf("%v", content)
}
