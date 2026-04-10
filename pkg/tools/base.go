package tools

import "encoding/json"

type ToolResult struct {
	Success bool   `json:"success"`
	Content string `json:"content"`
	Error   string `json:"error,omitempty"`
}

type Tool interface {
	GetName() string
	GetDescription() string
	GetParameters() map[string]interface{}
	Execute(params map[string]interface{}) (ToolResult, error)
	ToSchema() map[string]interface{}
}

type BaseTool struct {
	Name        string
	Description string
	Parameters  map[string]interface{}
}

func (t *BaseTool) GetName() string {
	return t.Name
}

func (t *BaseTool) GetDescription() string {
	return t.Description
}

func (t *BaseTool) GetParameters() map[string]interface{} {
	return t.Parameters
}

func (t *BaseTool) ToSchema() map[string]interface{} {
	return map[string]interface{}{
		"name":         t.Name,
		"description":  t.Description,
		"input_schema": t.Parameters,
	}
}

func ParseJSONArguments(argsJSON string) (map[string]interface{}, error) {
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return nil, err
	}
	return args, nil
}
