package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type RetryConfig struct {
	Enabled         bool    `yaml:"enabled"`
	MaxRetries      int     `yaml:"max_retries"`
	InitialDelay    float64 `yaml:"initial_delay"`
	MaxDelay        float64 `yaml:"max_delay"`
	ExponentialBase float64 `yaml:"exponential_base"`
}

type LLMConfig struct {
	APIKey   string      `yaml:"api_key"`
	APIBase  string      `yaml:"api_base"`
	Model    string      `yaml:"model"`
	Provider string      `yaml:"provider"`
	Retry    RetryConfig `yaml:"retry"`
}

type AgentConfig struct {
	MaxSteps         int    `yaml:"max_steps"`
	WorkspaceDir     string `yaml:"workspace_dir"`
	SystemPromptPath string `yaml:"system_prompt_path"`
}

type MCPConfig struct {
	ConnectTimeout float64 `yaml:"connect_timeout"`
	ExecuteTimeout float64 `yaml:"execute_timeout"`
	SSEReadTimeout float64 `yaml:"sse_read_timeout"`
}

type ToolsConfig struct {
	EnableFileTools        bool      `yaml:"enable_file_tools"`
	EnableBash             bool      `yaml:"enable_bash"`
	EnableNote             bool      `yaml:"enable_note"`
	EnablePersistentMemory bool      `yaml:"enable_persistent_memory"`
	EnableSkills           bool      `yaml:"enable_skills"`
	SkillsDir              string    `yaml:"skills_dir"`
	EnableMCP              bool      `yaml:"enable_mcp"`
	MCPConfigPath          string    `yaml:"mcp_config_path"`
	MCP                    MCPConfig `yaml:"mcp"`
}

type Config struct {
	LLM   LLMConfig   `yaml:"llm"`
	Agent AgentConfig `yaml:"agent"`
	Tools ToolsConfig `yaml:"tools"`
}

func getPackageDir() string {
	exe, _ := os.Executable()
	return filepath.Dir(exe)
}

func FindConfigFile(filename string) *string {
	searchPaths := []string{
		filepath.Join("mini_agent", "config", filename),
		filepath.Join(os.Getenv("HOME"), ".mini-agent", "config", filename),
		filepath.Join(getPackageDir(), "config", filename),
	}

	for _, path := range searchPaths {
		if _, err := os.Stat(path); err == nil {
			return &path
		}
	}
	return nil
}

func GetDefaultConfigPath() string {
	if path := FindConfigFile("config.yaml"); path != nil {
		return *path
	}
	return filepath.Join(getPackageDir(), "config", "config.yaml")
}

func Load(configPath string) (*Config, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	if envKey := os.Getenv("MINIMAX_API_KEY"); envKey != "" {
		config.LLM.APIKey = envKey
	}

	if err := config.Validate(); err != nil {
		return nil, err
	}

	return &config, nil
}

func (c *Config) Validate() error {
	if c.LLM.APIKey == "" || c.LLM.APIKey == "YOUR_API_KEY_HERE" {
		return &ConfigError{Message: "Please configure a valid API Key"}
	}
	if c.LLM.APIKey == "" {
		return &ConfigError{Message: "Configuration file missing required field: api_key"}
	}
	return nil
}

type ConfigError struct {
	Message string
}

func (e *ConfigError) Error() string {
	return e.Message
}
