package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type SessionMemory struct {
	Sessions    []Session    `json:"sessions"`
	UserProfile UserProfile  `json:"user_profile"`
	LastUpdate  string       `json:"last_update"`
	Memories    []MemoryItem `json:"memories"`
	mu          sync.RWMutex
}

type Session struct {
	ID        string    `json:"id"`
	StartTime string    `json:"start_time"`
	EndTime   string    `json:"end_time"`
	Messages  []Message `json:"messages"`
	Summary   string    `json:"summary"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type UserProfile struct {
	Name          string            `json:"name"`
	Preferences   map[string]string `json:"preferences"`
	LastSession   string            `json:"last_session"`
	TotalSessions int               `json:"total_sessions"`
}

type MemoryItem struct {
	ID       string `json:"id"`
	Category string `json:"category"`
	Key      string `json:"key"`
	Content  string `json:"content"`
	Created  string `json:"created"`
}

var globalStore = &MemoryStore{
	memory: make(map[string]*SessionMemory),
}

type MemoryStore struct {
	memory map[string]*SessionMemory
	mu     sync.RWMutex
}

func GetOrCreateMemory(memoryFile string) *SessionMemory {
	globalStore.mu.Lock()
	defer globalStore.mu.Unlock()

	if mem, exists := globalStore.memory[memoryFile]; exists {
		return mem
	}

	mem := &SessionMemory{
		Sessions:    []Session{},
		UserProfile: UserProfile{Preferences: make(map[string]string)},
		Memories:    []MemoryItem{},
	}

	if data, err := os.ReadFile(memoryFile); err == nil {
		json.Unmarshal(data, mem)
	}

	globalStore.memory[memoryFile] = mem
	return mem
}

func (m *SessionMemory) Save(memoryFile string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.LastUpdate = time.Now().Format(time.RFC3339)
	dir := filepath.Dir(memoryFile)
	os.MkdirAll(dir, 0755)

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(memoryFile, data, 0644)
}

type SessionMemoryTool struct {
	*BaseTool
	MemoryFile string
	Memory     *SessionMemory
}

func NewSessionMemoryTool(memoryFile string) *SessionMemoryTool {
	return &SessionMemoryTool{
		BaseTool: &BaseTool{
			Name:        "save_memory",
			Description: "Save important information to persistent memory for future sessions.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"content": map[string]interface{}{
						"type":        "string",
						"description": "The information to remember",
					},
					"category": map[string]interface{}{
						"type":        "string",
						"description": "Category: preference, fact, project, task, or general",
					},
					"key": map[string]interface{}{
						"type":        "string",
						"description": "Optional unique key to identify this memory",
					},
				},
				"required": []string{"content", "category"},
			},
		},
		MemoryFile: memoryFile,
		Memory:     GetOrCreateMemory(memoryFile),
	}
}

func (t *SessionMemoryTool) Execute(params map[string]interface{}) (ToolResult, error) {
	content, ok := params["content"].(string)
	if !ok {
		return ToolResult{Success: false, Error: "content is required"}, nil
	}

	category := "general"
	if cat, ok := params["category"].(string); ok {
		category = cat
	}

	key := ""
	if k, ok := params["key"].(string); ok {
		key = k
	}

	t.Memory.mu.Lock()
	defer t.Memory.mu.Unlock()

	if key != "" {
		t.Memory.UserProfile.Preferences[key] = content
	} else {
		memID := uuid.New().String()
		memItem := MemoryItem{
			ID:       memID,
			Category: category,
			Key:      key,
			Content:  content,
			Created:  time.Now().Format(time.RFC3339),
		}
		if key == "" {
			memItem.Key = fmt.Sprintf("%s:%s", category, memID[:8])
		}
		t.Memory.Memories = append(t.Memory.Memories, memItem)
		t.Memory.UserProfile.Preferences[memItem.Key] = content
	}

	if err := t.Memory.Save(t.MemoryFile); err != nil {
		return ToolResult{Success: false, Error: fmt.Sprintf("Failed to save memory: %v", err)}, nil
	}

	displayContent := content
	if len(content) > 100 {
		displayContent = content[:100] + "..."
	}

	return ToolResult{
		Success: true,
		Content: fmt.Sprintf("Saved to memory: [%s] %s", category, displayContent),
	}, nil
}

type RecallMemoryTool struct {
	*BaseTool
	MemoryFile string
	Memory     *SessionMemory
}

func NewRecallMemoryTool(memoryFile string) *RecallMemoryTool {
	return &RecallMemoryTool{
		BaseTool: &BaseTool{
			Name:        "recall_memory",
			Description: "Recall previously saved memories from persistent storage.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Search query to find relevant memories",
					},
					"category": map[string]interface{}{
						"type":        "string",
						"description": "Filter by category: preference, fact, project, task, general",
					},
				},
			},
		},
		MemoryFile: memoryFile,
		Memory:     GetOrCreateMemory(memoryFile),
	}
}

func (t *RecallMemoryTool) Execute(params map[string]interface{}) (ToolResult, error) {
	query := ""
	if q, ok := params["query"].(string); ok {
		query = q
	}

	category := ""
	if cat, ok := params["category"].(string); ok {
		category = cat
	}

	t.Memory.mu.RLock()
	defer t.Memory.mu.RUnlock()

	var results []string
	lowerQuery := strings.ToLower(query)

	for _, mem := range t.Memory.Memories {
		if category != "" && mem.Category != category {
			continue
		}

		if query != "" {
			lowerContent := strings.ToLower(mem.Content)
			lowerKey := strings.ToLower(mem.Key)
			if !strings.Contains(lowerKey, lowerQuery) && !strings.Contains(lowerContent, lowerQuery) {
				continue
			}
		}

		results = append(results, fmt.Sprintf("[%s] %s", mem.Key, mem.Content))
	}

	if len(results) == 0 {
		return ToolResult{
			Success: true,
			Content: "No memories found matching your query.",
		}, nil
	}

	return ToolResult{
		Success: true,
		Content: "Recalled Memories:\n" + strings.Join(results, "\n"),
	}, nil
}

type SessionSummaryTool struct {
	*BaseTool
	MemoryFile string
	Memory     *SessionMemory
}

func NewSessionSummaryTool(memoryFile string) *SessionSummaryTool {
	return &SessionSummaryTool{
		BaseTool: &BaseTool{
			Name:        "summarize_session",
			Description: "Create and save a summary of the current conversation session.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"summary": map[string]interface{}{
						"type":        "string",
						"description": "Summary of the conversation",
					},
				},
				"required": []string{"summary"},
			},
		},
		MemoryFile: memoryFile,
		Memory:     GetOrCreateMemory(memoryFile),
	}
}

func (t *SessionSummaryTool) Execute(params map[string]interface{}) (ToolResult, error) {
	summary, ok := params["summary"].(string)
	if !ok {
		return ToolResult{Success: false, Error: "summary is required"}, nil
	}

	t.Memory.mu.Lock()
	defer t.Memory.mu.Unlock()

	session := Session{
		ID:        fmt.Sprintf("session_%d", time.Now().Unix()),
		StartTime: time.Now().Format(time.RFC3339),
		EndTime:   time.Now().Format(time.RFC3339),
		Summary:   summary,
	}

	t.Memory.Sessions = append(t.Memory.Sessions, session)
	t.Memory.UserProfile.TotalSessions++
	t.Memory.UserProfile.LastSession = session.ID

	if err := t.Memory.Save(t.MemoryFile); err != nil {
		return ToolResult{Success: false, Error: fmt.Sprintf("Failed to save session: %v", err)}, nil
	}

	return ToolResult{
		Success: true,
		Content: fmt.Sprintf("Session summary saved. Total sessions: %d", t.Memory.UserProfile.TotalSessions),
	}, nil
}
