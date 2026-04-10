package llm

import "github.com/zntoast/mini_agent/pkg/schema"

type LLMClientBase interface {
	Generate(messages []*schema.Message, tools []interface{}) (*schema.LLMResponse, error)
}

type Tool interface {
	GetName() string
	GetDescription() string
	GetParameters() map[string]interface{}
}
