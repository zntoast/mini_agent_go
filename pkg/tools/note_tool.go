package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

type SessionNoteTool struct {
	*BaseTool
	MemoryFile string
}

func NewSessionNoteTool(memoryFile string) *SessionNoteTool {
	return &SessionNoteTool{
		BaseTool: &BaseTool{
			Name:        "record_note",
			Description: "Record important information as session notes for future reference.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"content": map[string]interface{}{
						"type":        "string",
						"description": "The information to record as a note",
					},
					"category": map[string]interface{}{
						"type":        "string",
						"description": "Optional category/tag for this note",
					},
				},
				"required": []string{"content"},
			},
		},
		MemoryFile: memoryFile,
	}
}

func (t *SessionNoteTool) Execute(params map[string]interface{}) (ToolResult, error) {
	content, ok := params["content"].(string)
	if !ok {
		return ToolResult{Success: false, Error: "content is required"}, nil
	}

	category := "general"
	if cat, ok := params["category"].(string); ok {
		category = cat
	}

	notes := t.loadFromFile()

	note := map[string]interface{}{
		"timestamp": time.Now().Format(time.RFC3339),
		"category":  category,
		"content":   content,
	}
	notes = append(notes, note)

	if err := t.saveToFile(notes); err != nil {
		return ToolResult{Success: false, Error: fmt.Sprintf("Failed to save note: %v", err)}, nil
	}

	return ToolResult{
		Success: true,
		Content: fmt.Sprintf("Recorded note: %s (category: %s)", content, category),
	}, nil
}

func (t *SessionNoteTool) loadFromFile() []map[string]interface{} {
	data, err := os.ReadFile(t.MemoryFile)
	if err != nil {
		return []map[string]interface{}{}
	}

	var notes []map[string]interface{}
	if err := json.Unmarshal(data, &notes); err != nil {
		return []map[string]interface{}{}
	}
	return notes
}

func (t *SessionNoteTool) saveToFile(notes []map[string]interface{}) error {
	dir := t.MemoryFile[:len(t.MemoryFile)-len("/.agent_memory.json")]
	os.MkdirAll(dir, 0755)

	data, err := json.MarshalIndent(notes, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(t.MemoryFile, data, 0644)
}

type RecallNoteTool struct {
	*BaseTool
	MemoryFile string
}

func NewRecallNoteTool(memoryFile string) *RecallNoteTool {
	return &RecallNoteTool{
		BaseTool: &BaseTool{
			Name:        "recall_notes",
			Description: "Recall all previously recorded session notes.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"category": map[string]interface{}{
						"type":        "string",
						"description": "Optional: filter notes by category",
					},
				},
			},
		},
		MemoryFile: memoryFile,
	}
}

func (t *RecallNoteTool) Execute(params map[string]interface{}) (ToolResult, error) {
	data, err := os.ReadFile(t.MemoryFile)
	if err != nil {
		return ToolResult{Success: true, Content: "No notes recorded yet."}, nil
	}

	var notes []map[string]interface{}
	if err := json.Unmarshal(data, &notes); err != nil {
		return ToolResult{Success: true, Content: "No notes recorded yet."}, nil
	}

	if len(notes) == 0 {
		return ToolResult{Success: true, Content: "No notes recorded yet."}, nil
	}

	category := ""
	if cat, ok := params["category"].(string); ok {
		category = cat
	}

	var filtered []map[string]interface{}
	for _, note := range notes {
		if category == "" || note["category"] == category {
			filtered = append(filtered, note)
		}
	}

	if len(filtered) == 0 && category != "" {
		return ToolResult{Success: true, Content: fmt.Sprintf("No notes found in category: %s", category)}, nil
	}

	var lines []string
	for i, note := range filtered {
		timestamp := note["timestamp"].(string)
		cat := note["category"].(string)
		content := note["content"].(string)
		lines = append(lines, fmt.Sprintf("%d. [%s] %s\n   (recorded at %s)", i+1, cat, content, timestamp))
	}

	return ToolResult{
		Success: true,
		Content: "Recorded Notes:\n" + strings.Join(lines, "\n"),
	}, nil
}
