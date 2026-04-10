package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
)

type BackgroundShell struct {
	BashID      string
	Command     string
	Process     *exec.Cmd
	StartTime   time.Time
	OutputLines []string
	LastReadIdx int
	Status      string
	ExitCode    int
	mu          sync.Mutex
}

type BackgroundShellManager struct {
	shells map[string]*BackgroundShell
	mu     sync.RWMutex
}

var shellManager = &BackgroundShellManager{
	shells: make(map[string]*BackgroundShell),
}

func (m *BackgroundShellManager) Add(shell *BackgroundShell) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.shells[shell.BashID] = shell
}

func (m *BackgroundShellManager) Get(bashID string) *BackgroundShell {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.shells[bashID]
}

func (m *BackgroundShellManager) GetAvailableIDs() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ids := make([]string, 0, len(m.shells))
	for id := range m.shells {
		ids = append(ids, id)
	}
	return ids
}

func (m *BackgroundShellManager) Remove(bashID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.shells, bashID)
}

type BashTool struct {
	*BaseTool
	WorkspaceDir string
	IsWindows    bool
}

func NewBashTool(workspaceDir string) *BashTool {
	isWindows := runtime.GOOS == "windows"
	shellName := "PowerShell"
	if !isWindows {
		shellName = "bash"
	}

	return &BashTool{
		BaseTool: &BaseTool{
			Name:        "bash",
			Description: fmt.Sprintf("Execute %s commands in foreground or background.", shellName),
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"command": map[string]interface{}{
						"type":        "string",
						"description": fmt.Sprintf("The %s command to execute", shellName),
					},
					"timeout": map[string]interface{}{
						"type":        "integer",
						"description": "Timeout in seconds (default: 120, max: 600)",
					},
					"run_in_background": map[string]interface{}{
						"type":        "boolean",
						"description": "Set true to run in background",
					},
				},
				"required": []string{"command"},
			},
		},
		WorkspaceDir: workspaceDir,
		IsWindows:    isWindows,
	}
}

func (t *BashTool) Execute(params map[string]interface{}) (ToolResult, error) {
	command, ok := params["command"].(string)
	if !ok {
		return ToolResult{Success: false, Error: "command is required"}, nil
	}

	timeout := 120
	if timeoutVal, ok := params["timeout"].(float64); ok {
		timeout = int(timeoutVal)
		if timeout > 600 {
			timeout = 600
		}
		if timeout < 1 {
			timeout = 120
		}
	}

	runInBackground := false
	if bg, ok := params["run_in_background"].(bool); ok {
		runInBackground = bg
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	if t.IsWindows {
		return t.executeWindows(ctx, command, runInBackground)
	}
	return t.executeUnix(ctx, command, runInBackground)
}

func (t *BashTool) executeWindows(ctx context.Context, command string, runInBackground bool) (ToolResult, error) {
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-Command", command)
	cmd.Dir = t.WorkspaceDir

	if runInBackground && runtime.GOOS == "windows" {
		cmd.SysProcAttr = &syscall.SysProcAttr{
			CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
		}
	}

	cmd.Stdout = &bytes.Buffer{}
	cmd.Stderr = &bytes.Buffer{}

	err := cmd.Start()
	if err != nil {
		return ToolResult{Success: false, Error: fmt.Sprintf("Failed to start command: %v", err)}, nil
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-ctx.Done():
		cmd.Process.Kill()
		return ToolResult{Success: false, Error: fmt.Sprintf("Command timed out after %d seconds", cmd.ProcessState.ExitCode())}, nil
	case err := <-done:
		if err != nil {
			return ToolResult{
				Success: false,
				Content: cmd.Stdout.(*bytes.Buffer).String(),
				Error:   fmt.Sprintf("Command failed: %v\nStderr: %s", err, cmd.Stderr.(*bytes.Buffer).String()),
			}, nil
		}
		return ToolResult{
			Success: true,
			Content: cmd.Stdout.(*bytes.Buffer).String(),
		}, nil
	}
}

func (t *BashTool) executeUnix(ctx context.Context, command string, runInBackground bool) (ToolResult, error) {
	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	cmd.Dir = t.WorkspaceDir

	cmd.Stdout = &bytes.Buffer{}
	cmd.Stderr = &bytes.Buffer{}

	err := cmd.Start()
	if err != nil {
		return ToolResult{Success: false, Error: fmt.Sprintf("Failed to start command: %v", err)}, nil
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-ctx.Done():
		cmd.Process.Kill()
		return ToolResult{Success: false, Error: "Command timed out"}, nil
	case err := <-done:
		if err != nil {
			return ToolResult{
				Success: false,
				Content: cmd.Stdout.(*bytes.Buffer).String(),
				Error:   fmt.Sprintf("Command failed: %v\nStderr: %s", err, cmd.Stderr.(*bytes.Buffer).String()),
			}, nil
		}
		return ToolResult{
			Success: true,
			Content: cmd.Stdout.(*bytes.Buffer).String(),
		}, nil
	}
}

type BashOutputTool struct {
	*BaseTool
}

func NewBashOutputTool() *BashOutputTool {
	return &BashOutputTool{
		BaseTool: &BaseTool{
			Name:        "bash_output",
			Description: "Retrieves output from a running or completed background bash shell.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"bash_id": map[string]interface{}{
						"type":        "string",
						"description": "The ID of the background shell to retrieve output from",
					},
					"filter_str": map[string]interface{}{
						"type":        "string",
						"description": "Optional regex pattern to filter output lines",
					},
				},
				"required": []string{"bash_id"},
			},
		},
	}
}

func (t *BashOutputTool) Execute(params map[string]interface{}) (ToolResult, error) {
	bashID, ok := params["bash_id"].(string)
	if !ok {
		return ToolResult{Success: false, Error: "bash_id is required"}, nil
	}

	shell := shellManager.Get(bashID)
	if shell == nil {
		available := shellManager.GetAvailableIDs()
		return ToolResult{
			Success: false,
			Error:   fmt.Sprintf("Shell not found: %s. Available: %v", bashID, available),
		}, nil
	}

	shell.mu.Lock()
	defer shell.mu.Unlock()

	newLines := shell.OutputLines[shell.LastReadIdx:]
	shell.LastReadIdx = len(shell.OutputLines)

	var filteredLines []string
	if filterStr, ok := params["filter_str"].(string); ok && filterStr != "" {
		re, err := regexp.Compile(filterStr)
		if err == nil {
			for _, line := range newLines {
				if re.MatchString(line) {
					filteredLines = append(filteredLines, line)
				}
			}
		} else {
			filteredLines = newLines
		}
	} else {
		filteredLines = newLines
	}

	output := strings.Join(filteredLines, "\n")
	return ToolResult{
		Success: true,
		Content: output,
	}, nil
}

type BashKillTool struct {
	*BaseTool
}

func NewBashKillTool() *BashKillTool {
	return &BashKillTool{
		BaseTool: &BaseTool{
			Name:        "bash_kill",
			Description: "Kills a running background bash shell by its ID.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"bash_id": map[string]interface{}{
						"type":        "string",
						"description": "The ID of the background shell to terminate",
					},
				},
				"required": []string{"bash_id"},
			},
		},
	}
}

func (t *BashKillTool) Execute(params map[string]interface{}) (ToolResult, error) {
	bashID, ok := params["bash_id"].(string)
	if !ok {
		return ToolResult{Success: false, Error: "bash_id is required"}, nil
	}

	shell := shellManager.Get(bashID)
	if shell == nil {
		available := shellManager.GetAvailableIDs()
		return ToolResult{
			Success: false,
			Error:   fmt.Sprintf("Shell not found: %s. Available: %v", bashID, available),
		}, nil
	}

	if shell.Process != nil && shell.Process.Process != nil {
		shell.Process.Process.Kill()
	}

	shellManager.Remove(bashID)

	return ToolResult{
		Success: true,
		Content: fmt.Sprintf("Shell %s terminated", bashID),
	}, nil
}
