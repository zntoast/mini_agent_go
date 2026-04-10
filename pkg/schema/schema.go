package schema

import "encoding/json"

type LLMProvider string

const (
	ProviderAnthropic LLMProvider = "anthropic"
	ProviderOpenAI    LLMProvider = "openai"
)

type FunctionCall struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

type Message struct {
	Role       string      `json:"role"`
	Content    interface{} `json:"content"`
	Thinking   *string     `json:"thinking,omitempty"`
	ToolCalls  []*ToolCall `json:"tool_calls,omitempty"`
	ToolCallID *string     `json:"tool_call_id,omitempty"`
	Name       *string     `json:"name,omitempty"`
}

func (m Message) MarshalJSON() ([]byte, error) {
	type Alias Message
	aux := &struct {
		Content interface{} `json:"content"`
		*Alias
	}{
		Content: m.Content,
		Alias:   (*Alias)(&m),
	}
	return json.Marshal(aux)
}

type TokenUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type LLMResponse struct {
	Content      string      `json:"content"`
	Thinking     *string     `json:"thinking,omitempty"`
	ToolCalls    []*ToolCall `json:"tool_calls,omitempty"`
	FinishReason string      `json:"finish_reason"`
	Usage        *TokenUsage `json:"usage,omitempty"`
}
