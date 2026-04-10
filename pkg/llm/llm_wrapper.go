package llm

import (
	"strings"

	"github.com/zntoast/mini_agent/pkg/schema"
)

type LLMClient struct {
	Provider      schema.LLMProvider
	APIKey        string
	Model         string
	APIBase       string
	RetryConfig   *RetryConfig
	RetryCallback func(error, int)
	client        LLMClientBase
}

var minimaxDomains = []string{"api.minimax.io", "api.minimaxi.com"}

func NewLLMClient(apiKey string, provider schema.LLMProvider, apiBase, model string, retryConfig *RetryConfig) *LLMClient {
	apiBase = strings.TrimSuffix(apiBase, "/")

	isMinimax := false
	for _, domain := range minimaxDomains {
		if strings.Contains(apiBase, domain) {
			isMinimax = true
			break
		}
	}

	var fullAPIBase string
	if isMinimax {
		apiBase = strings.Replace(apiBase, "/anthropic", "", -1)
		apiBase = strings.Replace(apiBase, "/v1", "", -1)

		if provider == schema.ProviderAnthropic {
			fullAPIBase = apiBase + "/anthropic"
		} else if provider == schema.ProviderOpenAI {
			fullAPIBase = apiBase + "/v1"
		}
	} else {
		fullAPIBase = apiBase
	}

	client := &LLMClient{
		Provider:    provider,
		APIKey:      apiKey,
		Model:       model,
		APIBase:     fullAPIBase,
		RetryConfig: retryConfig,
	}

	if provider == schema.ProviderAnthropic {
		client.client = NewAnthropicClient(apiKey, fullAPIBase, model, retryConfig)
	} else {
		client.client = NewOpenAIClient(apiKey, fullAPIBase, model, retryConfig)
	}

	return client
}

func (c *LLMClient) SetRetryCallback(callback func(error, int)) {
	c.RetryCallback = callback
	if anthropicClient, ok := c.client.(*AnthropicClient); ok {
		anthropicClient.RetryCallback = callback
	} else if openAIClient, ok := c.client.(*OpenAIClient); ok {
		openAIClient.RetryCallback = callback
	}
}

func (c *LLMClient) Generate(messages []*schema.Message, tools []interface{}) (*schema.LLMResponse, error) {
	return c.client.Generate(messages, tools)
}
