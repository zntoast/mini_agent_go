package acp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/zntoast/mini_agent/pkg/agent"
	"github.com/zntoast/mini_agent/pkg/config"
	"github.com/zntoast/mini_agent/pkg/llm"
	"github.com/zntoast/mini_agent/pkg/schema"
	"github.com/zntoast/mini_agent/pkg/tools"
)

type SessionState struct {
	Agent     *agent.Agent
	Cancelled bool
}

type MCPServer struct {
	Config       *config.Config
	LLM          *llm.LLMClient
	BaseTools    []tools.Tool
	SystemPrompt string
	Sessions     map[string]*SessionState
}

func NewMCPServer(cfg *config.Config, llmClient *llm.LLMClient, baseTools []tools.Tool, systemPrompt string) *MCPServer {
	return &MCPServer{
		Config:       cfg,
		LLM:          llmClient,
		BaseTools:    baseTools,
		SystemPrompt: systemPrompt,
		Sessions:     make(map[string]*SessionState),
	}
}

func (s *MCPServer) HandleRequest(request map[string]interface{}) (map[string]interface{}, error) {
	method, ok := request["method"].(string)
	if !ok {
		return nil, fmt.Errorf("missing method")
	}

	switch method {
	case "initialize":
		return s.HandleInitialize(request)
	case "newSession":
		return s.HandleNewSession(request)
	case "prompt":
		return s.HandlePrompt(request)
	case "cancel":
		return s.HandleCancel(request)
	default:
		return nil, fmt.Errorf("unknown method: %s", method)
	}
}

func (s *MCPServer) HandleInitialize(request map[string]interface{}) (map[string]interface{}, error) {
	return map[string]interface{}{
		"protocolVersion": "1.0",
		"agentCapabilities": map[string]interface{}{
			"loadSession": false,
		},
		"agentInfo": map[string]interface{}{
			"name":    "mini-agent",
			"title":   "Mini-Agent",
			"version": "0.1.0",
		},
	}, nil
}

func (s *MCPServer) HandleNewSession(request map[string]interface{}) (map[string]interface{}, error) {
	sessionID := fmt.Sprintf("sess-%d", len(s.Sessions))

	workspace := s.Config.Agent.WorkspaceDir
	if cwd, ok := request["cwd"].(string); ok && cwd != "" {
		workspace = cwd
	}

	toolList := make([]tools.Tool, len(s.BaseTools))
	copy(toolList, s.BaseTools)

	agentInstance := agent.NewAgent(
		s.LLM,
		s.SystemPrompt,
		toolList,
		s.Config.Agent.MaxSteps,
		workspace,
		80000,
	)

	s.Sessions[sessionID] = &SessionState{
		Agent: agentInstance,
	}

	return map[string]interface{}{
		"sessionId": sessionID,
	}, nil
}

func (s *MCPServer) HandlePrompt(request map[string]interface{}) (map[string]interface{}, error) {
	sessionID, ok := request["sessionId"].(string)
	if !ok {
		sessionID = ""
	}

	state, exists := s.Sessions[sessionID]
	if !exists {
		newSession, err := s.HandleNewSession(map[string]interface{}{"cwd": ""})
		if err != nil {
			return map[string]interface{}{"stopReason": "refusal"}, nil
		}
		sessionID = newSession["sessionId"].(string)
		state = s.Sessions[sessionID]
	}

	state.Cancelled = false

	promptContent := ""
	if prompt, ok := request["prompt"].([]interface{}); ok {
		for _, block := range prompt {
			if blockMap, ok := block.(map[string]interface{}); ok {
				if text, ok := blockMap["text"].(string); ok {
					promptContent += text + "\n"
				}
			}
		}
	}

	state.Agent.AddUserMessage(strings.TrimSpace(promptContent))

	stopReason, err := s.runTurn(state)
	if err != nil {
		return map[string]interface{}{
			"stopReason": "refusal",
			"error":      err.Error(),
		}, nil
	}

	return map[string]interface{}{
		"stopReason": stopReason,
	}, nil
}

func (s *MCPServer) HandleCancel(request map[string]interface{}) (map[string]interface{}, error) {
	sessionID, ok := request["sessionId"].(string)
	if !ok {
		return nil, nil
	}

	if state, exists := s.Sessions[sessionID]; exists {
		state.Cancelled = true
	}

	return nil, nil
}

func (s *MCPServer) runTurn(state *SessionState) (string, error) {
	agentInstance := state.Agent

	for step := 0; step < agentInstance.MaxSteps; step++ {
		if state.Cancelled {
			return "cancelled", nil
		}

		toolList := make([]interface{}, 0, len(agentInstance.Tools))
		for _, tool := range agentInstance.Tools {
			toolList = append(toolList, tool)
		}

		response, err := agentInstance.LLM.Generate(agentInstance.Messages, toolList)
		if err != nil {
			return "refusal", err
		}

		if response.Thinking != nil {
			fmt.Printf("🤔 Thinking: %s\n", *response.Thinking)
		}

		if response.Content != "" {
			fmt.Printf("🤖 Assistant: %s\n", response.Content)
		}

		assistantMsg := &schema.Message{
			Role:      "assistant",
			Content:   response.Content,
			Thinking:  response.Thinking,
			ToolCalls: response.ToolCalls,
		}
		agentInstance.Messages = append(agentInstance.Messages, assistantMsg)

		if len(response.ToolCalls) == 0 {
			return "end_turn", nil
		}

		for _, call := range response.ToolCalls {
			name := call.Function.Name
			args := call.Function.Arguments

			fmt.Printf("🔧 Calling tool: %s(%v)\n", name, args)

			tool, exists := agentInstance.Tools[name]
			if !exists {
				fmt.Printf("[ERROR] Unknown tool: %s\n", name)
				continue
			}

			result, err := tool.Execute(args)
			if err != nil {
				fmt.Printf("[ERROR] Tool error: %v\n", err)
				continue
			}

			prefix := "[OK]"
			if !result.Success {
				prefix = "[ERROR]"
			}
			fmt.Printf("%s %s\n", prefix, result.Content)

			toolMsg := &schema.Message{
				Role:       "tool",
				Content:    result.Content,
				ToolCallID: &call.ID,
				Name:       &name,
			}
			if !result.Success {
				toolMsg.Content = fmt.Sprintf("[ERROR] %s", result.Error)
			}
			agentInstance.Messages = append(agentInstance.Messages, toolMsg)
		}
	}

	return "max_turn_requests", nil
}

func RunServer() error {
	configPath := config.GetDefaultConfigPath()

	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	systemPromptPath := config.FindConfigFile(cfg.Agent.SystemPromptPath)
	var systemPrompt string
	if systemPromptPath != nil {
		if data, err := os.ReadFile(*systemPromptPath); err == nil {
			systemPrompt = string(data)
		}
	}
	if systemPrompt == "" {
		systemPrompt = "You are a helpful AI assistant."
	}

	retryConfig := &llm.RetryConfig{
		Enabled:         cfg.LLM.Retry.Enabled,
		MaxRetries:      cfg.LLM.Retry.MaxRetries,
		InitialDelay:    cfg.LLM.Retry.InitialDelay,
		MaxDelay:        cfg.LLM.Retry.MaxDelay,
		ExponentialBase: cfg.LLM.Retry.ExponentialBase,
	}

	var provider schema.LLMProvider
	if cfg.LLM.Provider == "anthropic" {
		provider = schema.ProviderAnthropic
	} else {
		provider = schema.ProviderOpenAI
	}

	llmClient := llm.NewLLMClient(
		cfg.LLM.APIKey,
		provider,
		cfg.LLM.APIBase,
		cfg.LLM.Model,
		retryConfig,
	)

	var baseTools []tools.Tool
	if cfg.Tools.EnableBash {
		baseTools = append(baseTools, tools.NewBashTool(cfg.Agent.WorkspaceDir))
		baseTools = append(baseTools, tools.NewBashOutputTool())
		baseTools = append(baseTools, tools.NewBashKillTool())
	}
	if cfg.Tools.EnableNote {
		baseTools = append(baseTools, tools.NewSessionNoteTool(""))
	}

	server := NewMCPServer(cfg, llmClient, baseTools, systemPrompt)

	reader := bufio.NewReader(os.Stdin)

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("read error: %w", err)
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var request map[string]interface{}
		if err := json.Unmarshal([]byte(line), &request); err != nil {
			continue
		}

		response, err := server.HandleRequest(request)
		if err != nil {
			response = map[string]interface{}{
				"error": err.Error(),
			}
		}

		responseJSON, _ := json.Marshal(response)
		fmt.Println(string(responseJSON))
	}

	return nil
}
