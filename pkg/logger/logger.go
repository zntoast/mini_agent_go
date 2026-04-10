package logger

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/zntoast/mini_agent/pkg/schema"
)

type AgentLogger struct {
	logDir   string
	logFile  *os.File
	logIndex int
}

func NewAgentLogger() *AgentLogger {
	homeDir, _ := os.UserHomeDir()
	logDir := filepath.Join(homeDir, ".mini-agent", "log")
	os.MkdirAll(logDir, 0755)
	return &AgentLogger{
		logDir: logDir,
	}
}

func (l *AgentLogger) StartNewRun() error {
	timestamp := time.Now().Format("20060102_150405")
	filename := fmt.Sprintf("agent_run_%s.log", timestamp)
	logPath := filepath.Join(l.logDir, filename)

	f, err := os.Create(logPath)
	if err != nil {
		return err
	}

	l.logFile = f
	l.logIndex = 0

	fmt.Fprintf(f, "========================================\n")
	fmt.Fprintf(f, "Agent Run Log - %s\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Fprintf(f, "========================================\n\n")

	return nil
}

func (l *AgentLogger) GetLogFilePath() string {
	if l.logFile == nil {
		return ""
	}
	return l.logFile.Name()
}

func (l *AgentLogger) LogRequest(messages []*schema.Message, tools []interface{}) {
	if l.logFile == nil {
		return
	}
	l.logIndex++

	requestData := map[string]interface{}{
		"messages": []interface{}{},
		"tools":    []string{},
	}

	var msgList []interface{}
	for _, msg := range messages {
		msgDict := map[string]interface{}{
			"role":    msg.Role,
			"content": msg.Content,
		}
		if msg.Thinking != nil {
			msgDict["thinking"] = *msg.Thinking
		}
		if msg.ToolCalls != nil {
			tcList := make([]map[string]interface{}, len(msg.ToolCalls))
			for i, tc := range msg.ToolCalls {
				tcList[i] = map[string]interface{}{
					"id":       tc.ID,
					"type":     tc.Type,
					"function": tc.Function,
				}
			}
			msgDict["tool_calls"] = tcList
		}
		if msg.ToolCallID != nil {
			msgDict["tool_call_id"] = *msg.ToolCallID
		}
		if msg.Name != nil {
			msgDict["name"] = *msg.Name
		}
		msgList = append(msgList, msgDict)
	}
	requestData["messages"] = msgList

	if tools != nil {
		toolNames := []string{}
		for _, t := range tools {
			if tool, ok := t.(interface{ GetName() string }); ok {
				toolNames = append(toolNames, tool.GetName())
			}
		}
		requestData["tools"] = toolNames
	}

	content, _ := json.MarshalIndent(requestData, "", "  ")

	l.writeLog("REQUEST", fmt.Sprintf("LLM Request:\n\n%s", content))
}

func (l *AgentLogger) LogResponse(content string, thinking *string, toolCalls []*schema.ToolCall, finishReason *string) {
	if l.logFile == nil {
		return
	}
	l.logIndex++

	responseData := map[string]interface{}{
		"content": content,
	}

	if thinking != nil {
		responseData["thinking"] = *thinking
	}
	if toolCalls != nil {
		tcList := make([]map[string]interface{}, len(toolCalls))
		for i, tc := range toolCalls {
			tcList[i] = map[string]interface{}{
				"id":       tc.ID,
				"type":     tc.Type,
				"function": tc.Function,
			}
		}
		responseData["tool_calls"] = tcList
	}
	if finishReason != nil {
		responseData["finish_reason"] = *finishReason
	}

	logContent, _ := json.MarshalIndent(responseData, "", "  ")

	l.writeLog("RESPONSE", fmt.Sprintf("LLM Response:\n\n%s", logContent))
}

func (l *AgentLogger) LogToolResult(toolName string, arguments map[string]interface{}, resultSuccess bool, resultContent *string, resultError *string) {
	if l.logFile == nil {
		return
	}
	l.logIndex++

	toolResultData := map[string]interface{}{
		"tool_name": toolName,
		"arguments": arguments,
		"success":   resultSuccess,
	}

	if resultSuccess && resultContent != nil {
		toolResultData["result"] = *resultContent
	} else if !resultSuccess && resultError != nil {
		toolResultData["error"] = *resultError
	}

	content, _ := json.MarshalIndent(toolResultData, "", "  ")

	l.writeLog("TOOL_RESULT", fmt.Sprintf("Tool Execution:\n\n%s", content))
}

func (l *AgentLogger) writeLog(logType string, content string) {
	if l.logFile == nil {
		return
	}
	timestamp := time.Now().Format("2006-01-02 15:04:05.000")
	fmt.Fprintf(l.logFile, "\n----------------------------------------\n")
	fmt.Fprintf(l.logFile, "[%d] %s\n", l.logIndex, logType)
	fmt.Fprintf(l.logFile, "Timestamp: %s\n", timestamp)
	fmt.Fprintf(l.logFile, "----------------------------------------\n")
	fmt.Fprintf(l.logFile, "%s\n", content)
}

func (l *AgentLogger) Close() error {
	if l.logFile != nil {
		return l.logFile.Close()
	}
	return nil
}
