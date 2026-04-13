package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/zntoast/mini_agent/pkg/config"
	"github.com/zntoast/mini_agent/pkg/tools"
)

type Client struct {
	servers   []MCPServerConfig
	tools     map[string]MCPTool
	mu        sync.RWMutex
	timeout   time.Duration
	processes map[string]*exec.Cmd
	stdins    map[string]chan string
}

type MCPServerConfig struct {
	Name    string   `json:"name"`
	Command string   `json:"command"`
	Args    []string `json:"args"`
	Env     []string `json:"env"`
}

type MCPTool struct {
	Name        string
	Description string
	InputSchema map[string]interface{}
	ServerName  string
}

type jsonRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
	ID      int         `json:"id"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
	ID      int             `json:"id"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type callToolResult struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text,omitempty"`
	} `json:"content"`
	IsError bool `json:"isError,omitempty"`
}

func NewClient(cfg *config.Config) (*Client, error) {
	mcpConfigPath := cfg.Tools.MCPConfigPath
	if mcpConfigPath == "" {
		mcpConfigPath = "mcp.json"
	}

	servers, err := loadServerConfig(mcpConfigPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load MCP config: %w", err)
	}

	timeout := time.Duration(cfg.Tools.MCP.ExecuteTimeout) * time.Second
	if timeout == 0 {
		timeout = 60 * time.Second
	}

	return &Client{
		servers:   servers,
		tools:     make(map[string]MCPTool),
		timeout:   timeout,
		processes: make(map[string]*exec.Cmd),
		stdins:    make(map[string]chan string),
	}, nil
}

func loadServerConfig(path string) ([]MCPServerConfig, error) {
	var allServers []MCPServerConfig

	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	if info.IsDir() {
		entries, err := os.ReadDir(path)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			filePath := filepath.Join(path, entry.Name())
			servers, err := loadServerConfigFile(filePath)
			if err != nil {
				continue
			}
			allServers = append(allServers, servers...)
		}
	} else {
		return loadServerConfigFile(path)
	}

	return allServers, nil
}

func loadServerConfigFile(path string) ([]MCPServerConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var config struct {
		Servers []MCPServerConfig `json:"servers"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return config.Servers, nil
}

func (c *Client) Connect() error {
	for _, server := range c.servers {
		if err := c.connectServer(server); err != nil {
			return fmt.Errorf("failed to connect to server %s: %w", server.Name, err)
		}
	}
	return nil
}

func (c *Client) connectServer(server MCPServerConfig) error {
	cmd := exec.Command(server.Command, server.Args...)

	env := os.Environ()
	for _, e := range server.Env {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) == 2 {
			key := parts[0]
			value := parts[1]
			if strings.HasPrefix(value, "${") && strings.HasSuffix(value, "}") {
				envVar := os.Getenv(key)
				if envVar != "" {
					value = envVar
				}
			}
			env = append(env, fmt.Sprintf("%s=%s", key, value))
		}
	}
	cmd.Env = env

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	c.processes[server.Name] = cmd
	stdinChan := make(chan string)
	c.stdins[server.Name] = stdinChan

	scanner := bufio.NewScanner(stdout)
	respChan := make(chan map[string]interface{})
	errChan := make(chan error, 1)
	nextID := 1
	var mu sync.Mutex

	go func() {
		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}

			var resp jsonRPCResponse
			if err := json.Unmarshal(line, &resp); err != nil {
				continue
			}

			result := make(map[string]interface{})
			if resp.Result != nil {
				json.Unmarshal(resp.Result, &result)
			}

			respChan <- map[string]interface{}{
				"id":     resp.ID,
				"result": result,
				"error":  resp.Error,
			}
		}
		if err := scanner.Err(); err != nil {
			errChan <- err
		}
		close(respChan)
	}()

	sendRequest := func(method string, params interface{}) (map[string]interface{}, error) {
		mu.Lock()
		id := nextID
		nextID++
		mu.Unlock()

		req := jsonRPCRequest{
			JSONRPC: "2.0",
			Method:  method,
			Params:  params,
			ID:      id,
		}

		reqBytes, _ := json.Marshal(req)
		fmt.Fprintln(stdin, string(reqBytes))

		ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
		defer cancel()

		for {
			select {
			case resp := <-respChan:
				if resp["id"] == id {
					if resp["error"] != nil {
						return nil, fmt.Errorf("MCP error")
					}
					return resp["result"].(map[string]interface{}), nil
				}
			case <-ctx.Done():
				return nil, ctx.Err()
			case err := <-errChan:
				return nil, err
			}
		}
	}

	_, err = sendRequest("initialize", map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{},
		"clientInfo": map[string]interface{}{
			"name":    "mini-agent",
			"version": "0.1.0",
		},
	})
	if err != nil {
		cmd.Process.Kill()
		return err
	}

	sendRequest("notifications/initialized", nil)

	toolsResult, err := sendRequest("tools/list", nil)
	if err != nil {
		cmd.Process.Kill()
		return err
	}

	if toolsResult == nil {
		return fmt.Errorf("failed to list tools")
	}

	toolsList, ok := toolsResult["tools"].([]interface{})
	if !ok {
		return fmt.Errorf("invalid tools list format")
	}

	for _, t := range toolsList {
		tMap := t.(map[string]interface{})
		toolName := tMap["name"].(string)

		inputSchema := make(map[string]interface{})
		if schemaRaw, ok := tMap["inputSchema"].(json.RawMessage); ok {
			json.Unmarshal(schemaRaw, &inputSchema)
		}

		c.mu.Lock()
		c.tools[toolName] = MCPTool{
			Name:        toolName,
			Description: tMap["description"].(string),
			InputSchema: inputSchema,
			ServerName:  server.Name,
		}
		c.mu.Unlock()
	}

	go func() {
		for {
			select {
			case msg, ok := <-stdinChan:
				if !ok {
					return
				}
				fmt.Fprintln(stdin, msg)
			}
		}
	}()

	return nil
}

func (c *Client) GetTools() []tools.Tool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make([]tools.Tool, 0, len(c.tools))
	for _, t := range c.tools {
		mcpTool := t
		result = append(result, &mcpToolAdapter{
			tool:   mcpTool,
			client: c,
		})
	}
	return result
}

type mcpToolAdapter struct {
	tool   MCPTool
	client *Client
}

func (a *mcpToolAdapter) GetName() string {
	return a.tool.Name
}

func (a *mcpToolAdapter) GetDescription() string {
	return a.tool.Description
}

func (a *mcpToolAdapter) GetParameters() map[string]interface{} {
	return a.tool.InputSchema
}

func (a *mcpToolAdapter) ToSchema() map[string]interface{} {
	return map[string]interface{}{
		"name":        a.tool.Name,
		"description": a.tool.Description,
		"inputSchema": a.tool.InputSchema,
	}
}

func (a *mcpToolAdapter) Execute(params map[string]interface{}) (tools.ToolResult, error) {
	c := a.client

	c.mu.RLock()
	tool, exists := c.tools[a.tool.Name]
	stdinChan, hasStdin := c.stdins[tool.ServerName]
	c.mu.RUnlock()

	if !exists {
		return tools.ToolResult{Success: false, Error: "tool not found"}, nil
	}

	if !hasStdin {
		return tools.ToolResult{Success: false, Error: "MCP server stdin not available"}, nil
	}

	id := time.Now().UnixNano()
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  "tools/call",
		Params: map[string]interface{}{
			"name":      a.tool.Name,
			"arguments": params,
		},
		ID: int(id),
	}

	reqBytes, _ := json.Marshal(req)

	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	resultChan := make(chan tools.ToolResult, 1)
	errChan := make(chan error, 1)

	go func() {
		stdinChan <- string(reqBytes)
	}()

	go func() {
		conn, ok := c.processes[tool.ServerName]
		if !ok || conn == nil {
			errChan <- fmt.Errorf("server process not found")
			return
		}

		stdout, err := conn.StdoutPipe()
		if err != nil {
			errChan <- err
			return
		}

		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}

			var resp jsonRPCResponse
			if err := json.Unmarshal(line, &resp); err != nil {
				continue
			}

			if resp.ID == int(id) {
				if resp.Error != nil {
					resultChan <- tools.ToolResult{
						Success: false,
						Error:   resp.Error.Message,
					}
					return
				}

				var callResult callToolResult
				if resp.Result != nil {
					json.Unmarshal(resp.Result, &callResult)
				}

				if len(callResult.Content) > 0 {
					resultChan <- tools.ToolResult{
						Success: true,
						Content: callResult.Content[0].Text,
					}
				} else {
					resultChan <- tools.ToolResult{
						Success: true,
						Content: "Tool executed successfully",
					}
				}
				return
			}
		}
		errChan <- fmt.Errorf("no response received")
	}()

	select {
	case result := <-resultChan:
		return result, nil
	case err := <-errChan:
		return tools.ToolResult{Success: false, Error: err.Error()}, nil
	case <-ctx.Done():
		return tools.ToolResult{Success: false, Error: "timeout"}, nil
	}
}
