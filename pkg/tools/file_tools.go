package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type ReadTool struct {
	*BaseTool
	WorkspaceDir string
}

func NewReadTool(workspaceDir string) *ReadTool {
	return &ReadTool{
		BaseTool: &BaseTool{
			Name:        "read_file",
			Description: "Read file contents from the filesystem. Output includes line numbers in format 'LINE_NUMBER|LINE_CONTENT' (1-indexed).",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Absolute or relative path to the file",
					},
					"offset": map[string]interface{}{
						"type":        "integer",
						"description": "Starting line number (1-indexed)",
					},
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "Number of lines to read",
					},
				},
				"required": []string{"path"},
			},
		},
		WorkspaceDir: workspaceDir,
	}
}

func (t *ReadTool) Execute(params map[string]interface{}) (ToolResult, error) {
	path, ok := params["path"].(string)
	if !ok {
		return ToolResult{Success: false, Error: "path is required"}, nil
	}

	filePath := filepath.Join(t.WorkspaceDir, path)
	if !filepath.IsAbs(path) {
		filePath = filepath.Join(t.WorkspaceDir, path)
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return ToolResult{Success: false, Error: fmt.Sprintf("Failed to read file: %v", err)}, nil
	}

	content := string(data)
	lines := strings.Split(content, "\n")

	offset := 0
	limit := len(lines)

	if offsetStr, ok := params["offset"].(string); ok {
		if v, err := strconv.Atoi(offsetStr); err == nil {
			offset = v - 1
		}
	} else if offsetNum, ok := params["offset"].(float64); ok {
		offset = int(offsetNum) - 1
	}

	if limitStr, ok := params["limit"].(string); ok {
		if v, err := strconv.Atoi(limitStr); err == nil {
			limit = v
		}
	} else if limitNum, ok := params["limit"].(float64); ok {
		limit = int(limitNum)
	}

	if offset < 0 {
		offset = 0
	}
	if limit > len(lines) {
		limit = len(lines)
	}

	var sb strings.Builder
	for i := offset; i < limit && i < len(lines); i++ {
		sb.WriteString(fmt.Sprintf("%6d|%s\n", i+1, lines[i]))
	}

	resultContent := sb.String()
	maxTokens := 32000
	if len(resultContent) > maxTokens*4 {
		resultContent = resultContent[:maxTokens*4] + "\n... [truncated]"
	}

	return ToolResult{Success: true, Content: resultContent}, nil
}

type WriteTool struct {
	*BaseTool
	WorkspaceDir string
}

func NewWriteTool(workspaceDir string) *WriteTool {
	return &WriteTool{
		BaseTool: &BaseTool{
			Name:        "write_file",
			Description: "Write content to a file. Will overwrite existing files completely.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Absolute or relative path to the file",
					},
					"content": map[string]interface{}{
						"type":        "string",
						"description": "Complete content to write",
					},
				},
				"required": []string{"path", "content"},
			},
		},
		WorkspaceDir: workspaceDir,
	}
}

func (t *WriteTool) Execute(params map[string]interface{}) (ToolResult, error) {
	path, ok := params["path"].(string)
	if !ok {
		return ToolResult{Success: false, Error: "path is required"}, nil
	}

	content, ok := params["content"].(string)
	if !ok {
		return ToolResult{Success: false, Error: "content is required"}, nil
	}

	filePath := filepath.Join(t.WorkspaceDir, path)
	if !filepath.IsAbs(path) {
		filePath = filepath.Join(t.WorkspaceDir, path)
	}

	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return ToolResult{Success: false, Error: fmt.Sprintf("Failed to create directory: %v", err)}, nil
	}

	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		return ToolResult{Success: false, Error: fmt.Sprintf("Failed to write file: %v", err)}, nil
	}

	return ToolResult{Success: true, Content: fmt.Sprintf("Successfully wrote to %s", filePath)}, nil
}

type EditTool struct {
	*BaseTool
	WorkspaceDir string
}

func NewEditTool(workspaceDir string) *EditTool {
	return &EditTool{
		BaseTool: &BaseTool{
			Name:        "edit_file",
			Description: "Perform exact string replacement in a file. old_str must match exactly and appear uniquely.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Absolute or relative path to the file",
					},
					"old_str": map[string]interface{}{
						"type":        "string",
						"description": "Exact string to find and replace",
					},
					"new_str": map[string]interface{}{
						"type":        "string",
						"description": "Replacement string",
					},
				},
				"required": []string{"path", "old_str", "new_str"},
			},
		},
		WorkspaceDir: workspaceDir,
	}
}

func (t *EditTool) Execute(params map[string]interface{}) (ToolResult, error) {
	path, ok := params["path"].(string)
	if !ok {
		return ToolResult{Success: false, Error: "path is required"}, nil
	}

	oldStr, ok := params["old_str"].(string)
	if !ok {
		return ToolResult{Success: false, Error: "old_str is required"}, nil
	}

	newStr, ok := params["new_str"].(string)
	if !ok {
		return ToolResult{Success: false, Error: "new_str is required"}, nil
	}

	filePath := filepath.Join(t.WorkspaceDir, path)
	if !filepath.IsAbs(path) {
		filePath = filepath.Join(t.WorkspaceDir, path)
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return ToolResult{Success: false, Error: fmt.Sprintf("Failed to read file: %v", err)}, nil
	}

	content := string(data)
	if !strings.Contains(content, oldStr) {
		return ToolResult{Success: false, Error: "Text not found in file"}, nil
	}

	newContent := strings.Replace(content, oldStr, newStr, 1)
	if err := os.WriteFile(filePath, []byte(newContent), 0644); err != nil {
		return ToolResult{Success: false, Error: fmt.Sprintf("Failed to write file: %v", err)}, nil
	}

	return ToolResult{Success: true, Content: fmt.Sprintf("Successfully edited %s", filePath)}, nil
}

func truncateTextByTokens(text string, maxTokens int) string {
	lines := strings.Split(text, "\n")
	if len(lines) <= maxTokens {
		return text
	}

	keepLines := maxTokens / 2
	result := append(append([]string{}, lines[:keepLines]...),
		"\n... [Content truncated] ...\n")
	result = append(result, lines[len(lines)-keepLines:]...)
	return strings.Join(result, "\n")
}

func convertToolsFormat(tools []interface{}) ([]map[string]interface{}, error) {
	result := []map[string]interface{}{}
	for _, t := range tools {
		switch tool := t.(type) {
		case Tool:
			result = append(result, tool.ToSchema())
		case map[string]interface{}:
			result = append(result, tool)
		case string:
			var m map[string]interface{}
			if err := json.Unmarshal([]byte(tool), &m); err == nil {
				result = append(result, m)
			}
		}
	}
	return result, nil
}
