package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/zntoast/mini_agent/pkg/llm"
	"github.com/zntoast/mini_agent/pkg/logger"
	"github.com/zntoast/mini_agent/pkg/schema"
	"github.com/zntoast/mini_agent/pkg/tools"
	"github.com/zntoast/mini_agent/pkg/utils"
)

type Agent struct {
	LLM                *llm.LLMClient
	SystemPrompt       string
	Tools              map[string]tools.Tool
	MaxSteps           int
	TokenLimit         int
	WorkspaceDir       string
	CancelEvent        *Event
	Messages           []*schema.Message
	Logger             *logger.AgentLogger
	APITotalTokens     int
	SkipNextTokenCheck bool
	mu                 sync.RWMutex
}

type Colors struct {
	Reset         string
	Bold          string
	Dim           string
	Red           string
	Green         string
	Yellow        string
	Blue          string
	Magenta       string
	Cyan          string
	BrightRed     string
	BrightGreen   string
	BrightYellow  string
	BrightBlue    string
	BrightMagenta string
	BrightCyan    string
}

func NewColors() Colors {
	return Colors{
		Reset:         "\033[0m",
		Bold:          "\033[1m",
		Dim:           "\033[2m",
		Red:           "\033[31m",
		Green:         "\033[32m",
		Yellow:        "\033[33m",
		Blue:          "\033[34m",
		Magenta:       "\033[35m",
		Cyan:          "\033[36m",
		BrightRed:     "\033[91m",
		BrightGreen:   "\033[92m",
		BrightYellow:  "\033[93m",
		BrightBlue:    "\033[94m",
		BrightMagenta: "\033[95m",
		BrightCyan:    "\033[96m",
	}
}

func NewAgent(llmClient *llm.LLMClient, systemPrompt string, toolList []tools.Tool, maxSteps int, workspaceDir string, tokenLimit int) *Agent {
	toolsMap := make(map[string]tools.Tool)
	for _, tool := range toolList {
		toolsMap[tool.GetName()] = tool
	}

	absWorkspaceDir, _ := filepath.Abs(workspaceDir)
	os.MkdirAll(absWorkspaceDir, 0755)

	if !strings.Contains(systemPrompt, "Current Workspace") {
		systemPrompt = systemPrompt + fmt.Sprintf("\n\n## Current Workspace\nYou are currently working in: `%s`\nAll relative paths will be resolved relative to this directory.", absWorkspaceDir)
	}

	agent := &Agent{
		LLM:          llmClient,
		SystemPrompt: systemPrompt,
		Tools:        toolsMap,
		MaxSteps:     maxSteps,
		TokenLimit:   tokenLimit,
		WorkspaceDir: absWorkspaceDir,
		Logger:       logger.NewAgentLogger(),
	}

	agent.Messages = []*schema.Message{
		{Role: "system", Content: systemPrompt},
	}

	return agent
}

func (a *Agent) AddUserMessage(content string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.Messages = append(a.Messages, &schema.Message{
		Role:    "user",
		Content: content,
	})
}

func (a *Agent) CheckCancelled() bool {
	if a.CancelEvent == nil {
		return false
	}
	return a.CancelEvent.IsSet()
}

func (a *Agent) CleanupIncompleteMessages() {
	a.mu.Lock()
	defer a.mu.Unlock()

	lastAssistantIdx := -1
	for i := len(a.Messages) - 1; i >= 0; i-- {
		if a.Messages[i].Role == "assistant" {
			lastAssistantIdx = i
			break
		}
	}

	if lastAssistantIdx == -1 {
		return
	}

	removedCount := len(a.Messages) - lastAssistantIdx
	if removedCount > 0 {
		a.Messages = a.Messages[:lastAssistantIdx]
		fmt.Printf("%s   Cleaned up %d incomplete message(s)%s\n", NewColors().Dim, removedCount, NewColors().Reset)
	}
}

func (a *Agent) EstimateTokens() int {
	totalChars := 0

	for _, msg := range a.Messages {
		if content, ok := msg.Content.(string); ok {
			totalChars += len(content)
		} else if contentBytes, err := json.Marshal(msg.Content); err == nil {
			totalChars += len(contentBytes)
		}

		if msg.Thinking != nil {
			totalChars += len(*msg.Thinking)
		}

		if msg.ToolCalls != nil {
			for _, tc := range msg.ToolCalls {
				if argsBytes, err := json.Marshal(tc.Function.Arguments); err == nil {
					totalChars += len(argsBytes)
				}
			}
		}
	}

	return int(float64(totalChars) / 2.5)
}

func (a *Agent) SummarizeMessages() error {
	colors := NewColors()

	if a.SkipNextTokenCheck {
		a.SkipNextTokenCheck = false
		return nil
	}

	estimatedTokens := a.EstimateTokens()
	shouldSummarize := estimatedTokens > a.TokenLimit || a.APITotalTokens > a.TokenLimit

	if !shouldSummarize {
		return nil
	}

	fmt.Printf("\n%s📊 Token usage - Local estimate: %d, API reported: %d, Limit: %d%s\n", colors.BrightYellow, estimatedTokens, a.APITotalTokens, a.TokenLimit, colors.Reset)
	fmt.Printf("%s🔄 Triggering message history summarization...%s\n", colors.BrightYellow, colors.Reset)

	userIndices := []int{}
	for i, msg := range a.Messages {
		if msg.Role == "user" && i > 0 {
			userIndices = append(userIndices, i)
		}
	}

	if len(userIndices) < 1 {
		fmt.Printf("%s⚠️  Insufficient messages, cannot summarize%s\n", colors.BrightYellow, colors.Reset)
		return nil
	}

	newMessages := []*schema.Message{a.Messages[0]}
	summaryCount := 0

	for i, userIdx := range userIndices {
		newMessages = append(newMessages, a.Messages[userIdx])

		var nextUserIdx int
		if i < len(userIndices)-1 {
			nextUserIdx = userIndices[i+1]
		} else {
			nextUserIdx = len(a.Messages)
		}

		executionMessages := a.Messages[userIdx+1 : nextUserIdx]

		if len(executionMessages) > 0 {
			summaryText, err := a.CreateSummary(executionMessages, i+1)
			if err == nil && summaryText != "" {
				newMessages = append(newMessages, &schema.Message{
					Role:    "user",
					Content: "[Assistant Execution Summary]\n\n" + summaryText,
				})
				summaryCount++
			}
		}
	}

	a.mu.Lock()
	a.Messages = newMessages
	a.mu.Unlock()

	a.SkipNextTokenCheck = true

	newTokens := a.EstimateTokens()
	fmt.Printf("%s✓ Summary completed, local tokens: %d → %d%s\n", colors.BrightGreen, estimatedTokens, newTokens, colors.Reset)
	fmt.Printf("%s  Structure: system + %d user messages + %d summaries%s\n", colors.Dim, len(userIndices), summaryCount, colors.Reset)
	fmt.Printf("%s  Note: API token count will update on next LLM call%s\n", colors.Dim, colors.Reset)

	return nil
}

func (a *Agent) CreateSummary(messages []*schema.Message, roundNum int) (string, error) {
	colors := NewColors()

	if len(messages) == 0 {
		return "", nil
	}

	var summaryContent strings.Builder
	summaryContent.WriteString(fmt.Sprintf("Round %d execution process:\n\n", roundNum))

	for _, msg := range messages {
		if msg.Role == "assistant" {
			contentText := ""
			if s, ok := msg.Content.(string); ok {
				contentText = s
			}
			summaryContent.WriteString(fmt.Sprintf("Assistant: %s\n", contentText))

			if len(msg.ToolCalls) > 0 {
				var toolNames []string
				for _, tc := range msg.ToolCalls {
					toolNames = append(toolNames, tc.Function.Name)
				}
				summaryContent.WriteString(fmt.Sprintf("  → Called tools: %s\n", strings.Join(toolNames, ", ")))
			}
		} else if msg.Role == "tool" {
			resultPreview := ""
			if s, ok := msg.Content.(string); ok {
				resultPreview = s
			}
			summaryContent.WriteString(fmt.Sprintf("  ← Tool returned: %s...\n", resultPreview))
		}
	}

	summaryPrompt := fmt.Sprintf(`Please provide a concise summary of the following Agent execution process:

%s

Requirements:
1. Focus on what tasks were completed and which tools were called
2. Keep key execution results and important findings
3. Be concise and clear, within 1000 words
4. Use English
5. Do not include "user" related content, only summarize the Agent's execution process`, summaryContent.String())

	summaryMsg := &schema.Message{
		Role:    "user",
		Content: summaryPrompt,
	}

	response, err := a.LLM.Generate([]*schema.Message{
		{Role: "system", Content: "You are an assistant skilled at summarizing Agent execution processes."},
		summaryMsg,
	}, nil)

	if err != nil {
		fmt.Printf("%s✗ Summary generation failed for round %d: %v%s\n", colors.BrightRed, roundNum, err, colors.Reset)
		return summaryContent.String(), nil
	}

	fmt.Printf("%s✓ Summary for round %d generated successfully%s\n", colors.BrightGreen, roundNum, colors.Reset)
	return response.Content, nil
}

func (a *Agent) Run(cancelEvent *Event) (string, error) {
	colors := NewColors()

	if cancelEvent != nil {
		a.CancelEvent = cancelEvent
	}

	a.Logger.StartNewRun()
	fmt.Printf("%s📝 Log file: %s%s\n", colors.Dim, a.Logger.GetLogFilePath(), colors.Reset)

	step := 0
	runStartTime := time.Now()

	for step < a.MaxSteps {
		if a.CheckCancelled() {
			a.CleanupIncompleteMessages()
			fmt.Printf("\n%s⚠️  Task cancelled by user.%s\n", colors.BrightYellow, colors.Reset)
			return "Task cancelled by user.", nil
		}

		stepStartTime := time.Now()

		a.SummarizeMessages()

		boxWidth := 58
		stepText := fmt.Sprintf("%s💭 Step %d/%d%s", colors.Bold+colors.BrightCyan, step+1, a.MaxSteps, colors.Reset)
		stepDisplayWidth := utils.CalculateDisplayWidth(stepText)
		padding := max(0, boxWidth-1-stepDisplayWidth)

		fmt.Printf("\n%s╭%s─%s╮%s\n", colors.Dim, strings.Repeat("─", boxWidth), "", colors.Reset)
		fmt.Printf("%s│%s %s%s%s %s│%s\n", colors.Dim, colors.Reset, stepText, strings.Repeat(" ", padding), colors.Dim, colors.Reset)
		fmt.Printf("%s╰%s─%s╯%s\n", colors.Dim, strings.Repeat("─", boxWidth), "", colors.Reset)

		toolList := make([]interface{}, 0, len(a.Tools))
		for _, tool := range a.Tools {
			toolList = append(toolList, tool)
		}

		a.Logger.LogRequest(a.Messages, toolList)

		response, err := a.LLM.Generate(a.Messages, toolList)
		if err != nil {
			if retryErr, ok := err.(*llm.RetryExhaustedError); ok {
				errorMsg := fmt.Sprintf("LLM call failed after %d retries\nLast error: %v", retryErr.Attempts, retryErr.LastException)
				fmt.Printf("\n%s❌ Retry failed:%s %s\n", colors.BrightRed, colors.Reset, errorMsg)
				return errorMsg, retryErr
			}
			errorMsg := fmt.Sprintf("LLM call failed: %v", err)
			fmt.Printf("\n%s❌ Error:%s %s\n", colors.BrightRed, colors.Reset, errorMsg)
			return errorMsg, err
		}

		if response.Usage != nil {
			a.APITotalTokens = response.Usage.TotalTokens
		}

		a.Logger.LogResponse(response.Content, response.Thinking, response.ToolCalls, &response.FinishReason)

		assistantMsg := &schema.Message{
			Role:      "assistant",
			Content:   response.Content,
			Thinking:  response.Thinking,
			ToolCalls: response.ToolCalls,
		}
		a.Messages = append(a.Messages, assistantMsg)

		if response.Thinking != nil {
			fmt.Printf("\n%s🧠 Thinking:%s\n", colors.Bold+colors.Magenta, colors.Reset)
			fmt.Printf("%s%s%s\n", colors.Dim, *response.Thinking, colors.Reset)
		}

		if response.Content != "" {
			fmt.Printf("\n%s🤖 Assistant:%s\n", colors.Bold+colors.BrightBlue, colors.Reset)
			fmt.Printf("%s\n", response.Content)
		}

		if len(response.ToolCalls) == 0 {
			stepElapsed := time.Since(stepStartTime).Seconds()
			totalElapsed := time.Since(runStartTime).Seconds()
			fmt.Printf("\n%s⏱️  Step %d completed in %.2fs (total: %.2fs)%s\n", colors.Dim, step+1, stepElapsed, totalElapsed, colors.Reset)
			return response.Content, nil
		}

		if a.CheckCancelled() {
			a.CleanupIncompleteMessages()
			fmt.Printf("\n%s⚠️  Task cancelled by user.%s\n", colors.BrightYellow, colors.Reset)
			return "Task cancelled by user.", nil
		}

		for _, toolCall := range response.ToolCalls {
			toolCallID := toolCall.ID
			functionName := toolCall.Function.Name
			arguments := toolCall.Function.Arguments

			fmt.Printf("\n%s🔧 Tool Call:%s %s%s%s\n", colors.BrightYellow, colors.Reset, colors.Bold+colors.Cyan, functionName, colors.Reset)
			fmt.Printf("%s   Arguments:%s\n", colors.Dim, colors.Reset)

			truncatedArgs := make(map[string]interface{})
			for key, value := range arguments {
				valueStr := fmt.Sprintf("%v", value)
				if len(valueStr) > 200 {
					valueStr = valueStr[:200] + "..."
				}
				truncatedArgs[key] = valueStr
			}

			argsJSON, _ := json.MarshalIndent(truncatedArgs, "", "  ")
			for _, line := range strings.Split(string(argsJSON), "\n") {
				fmt.Printf("   %s%s%s\n", colors.Dim, line, colors.Reset)
			}

			var result tools.ToolResult
			tool, exists := a.Tools[functionName]
			if !exists {
				result = tools.ToolResult{
					Success: false,
					Content: "",
					Error:   fmt.Sprintf("Unknown tool: %s", functionName),
				}
			} else {
				execResult, err := tool.Execute(arguments)
				if err != nil {
					result = tools.ToolResult{
						Success: false,
						Content: "",
						Error:   fmt.Sprintf("Tool execution failed: %v", err),
					}
				} else {
					result = execResult
				}
			}

			var resultContent *string
			var resultError *string
			if result.Success {
				resultContent = &result.Content
			} else {
				resultError = &result.Error
			}

			a.Logger.LogToolResult(functionName, arguments, result.Success, resultContent, resultError)

			if result.Success {
				resultText := result.Content
				if len(resultText) > 300 {
					resultText = resultText[:300] + colors.Dim + "..." + colors.Reset
				}
				fmt.Printf("%s✓ Result:%s %s\n", colors.BrightGreen, colors.Reset, resultText)
			} else {
				fmt.Printf("%s✗ Error:%s %s%s%s\n", colors.BrightRed, colors.Reset, colors.Red, result.Error, colors.Reset)
			}

			toolMsg := &schema.Message{
				Role:       "tool",
				Content:    result.Content,
				ToolCallID: &toolCallID,
				Name:       &functionName,
			}
			if !result.Success {
				toolMsg.Content = fmt.Sprintf("Error: %s", result.Error)
			}
			a.Messages = append(a.Messages, toolMsg)

			if a.CheckCancelled() {
				a.CleanupIncompleteMessages()
				fmt.Printf("\n%s⚠️  Task cancelled by user.%s\n", colors.BrightYellow, colors.Reset)
				return "Task cancelled by user.", nil
			}
		}

		stepElapsed := time.Since(stepStartTime).Seconds()
		totalElapsed := time.Since(runStartTime).Seconds()
		fmt.Printf("\n%s⏱️  Step %d completed in %.2fs (total: %.2fs)%s\n", colors.Dim, step+1, stepElapsed, totalElapsed, colors.Reset)

		step++
	}

	errorMsg := fmt.Sprintf("Task couldn't be completed after %d steps.", a.MaxSteps)
	fmt.Printf("\n%s⚠️  %s%s\n", colors.BrightYellow, errorMsg, colors.Reset)
	return errorMsg, nil
}

func (a *Agent) GetHistory() []*schema.Message {
	a.mu.RLock()
	defer a.mu.RUnlock()

	result := make([]*schema.Message, len(a.Messages))
	copy(result, a.Messages)
	return result
}

type Event struct {
	set bool
	mu  sync.Mutex
}

func (e *Event) IsSet() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.set
}

func (e *Event) Set() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.set = true
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
