package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/zntoast/mini_agent/pkg/agent"
	"github.com/zntoast/mini_agent/pkg/config"
	"github.com/zntoast/mini_agent/pkg/llm"
	"github.com/zntoast/mini_agent/pkg/mcp"
	"github.com/zntoast/mini_agent/pkg/schema"
	"github.com/zntoast/mini_agent/pkg/tools"
)

type Colors struct {
	Reset        string
	Bold         string
	Dim          string
	Red          string
	Green        string
	Yellow       string
	Blue         string
	Magenta      string
	Cyan         string
	BrightRed    string
	BrightGreen  string
	BrightYellow string
	BrightBlue   string
	BrightCyan   string
}

func NewColors() Colors {
	return Colors{
		Reset:        "\033[0m",
		Bold:         "\033[1m",
		Dim:          "\033[2m",
		Red:          "\033[31m",
		Green:        "\033[32m",
		Yellow:       "\033[33m",
		Blue:         "\033[34m",
		Magenta:      "\033[35m",
		Cyan:         "\033[36m",
		BrightRed:    "\033[91m",
		BrightGreen:  "\033[92m",
		BrightYellow: "\033[93m",
		BrightBlue:   "\033[94m",
		BrightCyan:   "\033[96m",
	}
}

func printBanner() {
	colors := NewColors()
	fmt.Println()
	fmt.Printf("%s%s╔══════════════════════════════════════════════════════════════════════╗%s\n", colors.Bold, colors.BrightCyan, colors.Reset)
	fmt.Printf("%s%s║                    🤖 Mini Agent - Interactive Session                    ║%s\n", colors.Bold, colors.BrightCyan, colors.Reset)
	fmt.Printf("%s%s╚══════════════════════════════════════════════════════════════════════╝%s\n", colors.Bold, colors.BrightCyan, colors.Reset)
	fmt.Println()
}

func printHelp() {
	colors := NewColors()
	helpText := fmt.Sprintf(`
%[1$s%[2$sAvailable Commands:%[3$s
  %[4$s/help%[3$s      - Show this help message
  %[4$s/clear%[3$s     - Clear session history (keep system prompt)
  %[4$s/history%[3$s   - Show current session message count
  %[4$s/stats%[3$s     - Show session statistics
  %[4$s/exit%[3$s      - Exit program (also: exit, quit, q)

%[1$s%[2$sKeyboard Shortcuts:%[3$s
  %[5$sCtrl+C%[3$s     - Exit program
  %[5$sCtrl+U%[3$s     - Clear current input line
  %[5$sCtrl+L%[3$s     - Clear screen

%[1$s%[2$sUsage:%[3$s
  - Enter your task directly, Agent will help you complete it
  - Agent remembers all conversation content in this session
  - Use %[4$s/clear%[3$s to start a new session
`, colors.Bold, colors.BrightYellow, colors.Reset, colors.BrightGreen, colors.BrightCyan)
	fmt.Println(helpText)
}

func printSessionInfo(agentInstance *agent.Agent, workspaceDir string, model string) {
	colors := NewColors()
	boxWidth := 58

	fmt.Printf("%s┌%s┐%s\n", colors.Dim, strings.Repeat("─", boxWidth), colors.Reset)

	headerText := fmt.Sprintf("%sSession Info%s", colors.BrightCyan, colors.Reset)
	headerWidth := len([]rune(headerText))
	headerPadding := (boxWidth - headerWidth) / 2
	fmt.Printf("%s│%s %s%s%s%s %s│%s\n", colors.Dim, colors.Reset, strings.Repeat(" ", headerPadding), headerText, strings.Repeat(" ", boxWidth-headerWidth-headerPadding), colors.Dim, colors.Reset)

	fmt.Printf("%s├%s┤%s\n", colors.Dim, strings.Repeat("─", boxWidth), colors.Reset)

	printInfoLine := func(text string) {
		padding := boxWidth - 1 - len([]rune(text))
		fmt.Printf("%s│%s %s%s%s│%s\n", colors.Dim, colors.Reset, text, strings.Repeat(" ", padding), colors.Dim, colors.Reset)
	}

	printInfoLine(fmt.Sprintf("Model: %s", model))
	printInfoLine(fmt.Sprintf("Workspace: %s", workspaceDir))
	printInfoLine(fmt.Sprintf("Message History: %d messages", len(agentInstance.GetHistory())))
	printInfoLine(fmt.Sprintf("Available Tools: %d tools", len(agentInstance.Tools)))

	fmt.Printf("%s└%s┘%s\n", colors.Dim, strings.Repeat("─", boxWidth), colors.Reset)
	fmt.Println()
	fmt.Printf("%sType %s/help%s for help, %s/exit%s to quit%s\n", colors.Dim, colors.BrightGreen, colors.Dim, colors.BrightGreen, colors.Dim, colors.Reset)
	fmt.Println()
}

func printStats(agentInstance *agent.Agent, sessionStart time.Time) {
	colors := NewColors()
	duration := time.Since(sessionStart)
	hours := int(duration.Hours())
	minutes := int(duration.Minutes()) % 60
	seconds := int(duration.Seconds()) % 60

	messages := agentInstance.GetHistory()
	userMsgs := 0
	assistantMsgs := 0
	toolMsgs := 0

	for _, m := range messages {
		switch m.Role {
		case "user":
			userMsgs++
		case "assistant":
			assistantMsgs++
		case "tool":
			toolMsgs++
		}
	}

	fmt.Printf("\n%s%sSession Statistics:%s\n", colors.Bold, colors.BrightCyan, colors.Reset)
	fmt.Printf("%s----------------------------------------%s\n", colors.Dim, colors.Reset)
	fmt.Printf("  Session Duration: %02d:%02d:%02d\n", hours, minutes, seconds)
	fmt.Printf("  Total Messages: %d\n", len(messages))
	fmt.Printf("    - User Messages: %s%d%s\n", colors.BrightGreen, userMsgs, colors.Reset)
	fmt.Printf("    - Assistant Replies: %s%d%s\n", colors.BrightBlue, assistantMsgs, colors.Reset)
	fmt.Printf("    - Tool Calls: %s%d%s\n", colors.BrightYellow, toolMsgs, colors.Reset)
	fmt.Printf("  Available Tools: %d\n", len(agentInstance.Tools))
	fmt.Printf("%s----------------------------------------%s\n", colors.Dim, colors.Reset)
	fmt.Println()
}

func initializeTools(cfg *config.Config, workspaceDir string) ([]tools.Tool, error) {
	var toolList []tools.Tool
	colors := NewColors()

	if cfg.Tools.EnableBash {
		toolList = append(toolList, tools.NewBashTool(workspaceDir))
		toolList = append(toolList, tools.NewBashOutputTool())
		toolList = append(toolList, tools.NewBashKillTool())
	}

	if cfg.Tools.EnableFileTools {
		toolList = append(toolList, tools.NewReadTool(workspaceDir))
		toolList = append(toolList, tools.NewWriteTool(workspaceDir))
		toolList = append(toolList, tools.NewEditTool(workspaceDir))
	}

	if cfg.Tools.EnableNote {
		memoryFile := filepath.Join(workspaceDir, ".agent_memory.json")
		toolList = append(toolList, tools.NewSessionNoteTool(memoryFile))
		toolList = append(toolList, tools.NewRecallNoteTool(memoryFile))
	}

	if cfg.Tools.EnablePersistentMemory {
		persistentMemoryFile := filepath.Join(workspaceDir, ".agent_persistent_memory.json")
		toolList = append(toolList, tools.NewSessionMemoryTool(persistentMemoryFile))
		toolList = append(toolList, tools.NewRecallMemoryTool(persistentMemoryFile))
		toolList = append(toolList, tools.NewSessionSummaryTool(persistentMemoryFile))
	}

	if cfg.Tools.EnableMCP {
		mcpClient, err := mcp.NewClient(cfg)
		if err != nil {
			fmt.Printf("%s⚠️ Failed to create MCP client: %v%s\n", colors.Yellow, err, colors.Reset)
		} else if err := mcpClient.Connect(); err != nil {
			fmt.Printf("%s⚠️ Failed to connect to MCP server: %v%s\n", colors.Yellow, err, colors.Reset)
		} else {
			mcpTools := mcpClient.GetTools()
			toolList = append(toolList, mcpTools...)
			fmt.Printf("%s✅ Loaded %d MCP tools:%s\n", colors.BrightGreen, len(mcpTools), colors.Reset)
			for _, t := range mcpTools {
				fmt.Printf("%s   - %s%s: %s%s\n", colors.Dim, colors.Cyan, t.GetName(), colors.Dim, t.GetDescription())
			}
			fmt.Println()
		}
	}

	return toolList, nil
}

func runAgent(workspaceDir, task string) error {
	colors := NewColors()
	sessionStart := time.Now()

	configPath := config.GetDefaultConfigPath()
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		fmt.Printf("%s❌ Configuration file not found%s\n", colors.Red, colors.Reset)
		fmt.Printf("\n%s📦 Configuration Search Path:%s\n", colors.BrightCyan, colors.Reset)
		fmt.Printf("  %s1) mini_agent/config/config.yaml%s (development)\n", colors.Dim, colors.Reset)
		fmt.Printf("  %s2) ~/.mini-agent/config/config.yaml%s (user)\n", colors.Dim, colors.Reset)
		fmt.Printf("  %s3) <package>/config/config.yaml%s (installed)\n", colors.Dim, colors.Reset)
		return nil
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Printf("%s❌ Error loading config: %v%s\n", colors.Red, err, colors.Reset)
		return err
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

	toolList, err := initializeTools(cfg, workspaceDir)
	if err != nil {
		fmt.Printf("%s❌ Error initializing tools: %v%s\n", colors.Red, err, colors.Reset)
		return err
	}

	systemPromptPath := config.FindConfigFile(cfg.Agent.SystemPromptPath)
	systemPrompt := "You are a helpful AI assistant."
	if systemPromptPath != nil {
		if data, err := os.ReadFile(*systemPromptPath); err == nil {
			systemPrompt = string(data)
		}
	}

	agentInstance := agent.NewAgent(
		llmClient,
		systemPrompt,
		toolList,
		cfg.Agent.MaxSteps,
		workspaceDir,
		80000,
	)

	if task != "" {
		fmt.Printf("\n%sAgent%s %s›%s Executing task...\n\n", colors.BrightBlue, colors.Reset, colors.Dim, colors.Reset)
		agentInstance.AddUserMessage(task)
		_, err := agentInstance.Run(nil)
		if err != nil {
			fmt.Printf("\n%s❌ Error: %v%s\n", colors.Red, err, colors.Reset)
		}
		printStats(agentInstance, sessionStart)
		return nil
	}

	printBanner()
	printSessionInfo(agentInstance, workspaceDir, cfg.LLM.Model)

	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Print("You › ")
		input, err := reader.ReadString('\n')
		if err != nil {
			fmt.Println()
			break
		}

		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}

		lowerInput := strings.ToLower(input)

		if strings.HasPrefix(lowerInput, "/") {
			parts := strings.SplitN(input, " ", 2)
			command := strings.ToLower(parts[0])

			switch command {
			case "/exit", "/quit", "/q":
				fmt.Printf("\n%s👋 Goodbye! Thanks for using Mini Agent%s\n\n", colors.BrightYellow, colors.Reset)
				printStats(agentInstance, sessionStart)
				return nil
			case "/help":
				printHelp()
				continue
			case "/clear":
				oldCount := len(agentInstance.GetHistory())
				agentInstance = agent.NewAgent(
					llmClient,
					systemPrompt,
					toolList,
					cfg.Agent.MaxSteps,
					workspaceDir,
					80000,
				)
				fmt.Printf("%s✅ Cleared %d messages, starting new session%s\n\n", colors.BrightGreen, oldCount-1, colors.Reset)
				continue
			case "/history":
				fmt.Printf("\n%sCurrent session message count: %d%s\n\n", colors.BrightCyan, len(agentInstance.GetHistory()), colors.Reset)
				continue
			case "/stats":
				printStats(agentInstance, sessionStart)
				continue
			default:
				fmt.Printf("%s❌ Unknown command: %s%s\n", colors.Red, input, colors.Reset)
				fmt.Printf("%sType /help to see available commands%s\n\n", colors.Dim, colors.Reset)
				continue
			}
		}

		if lowerInput == "exit" || lowerInput == "quit" || lowerInput == "q" {
			fmt.Printf("\n%s👋 Goodbye! Thanks for using Mini Agent%s\n\n", colors.BrightYellow, colors.Reset)
			printStats(agentInstance, sessionStart)
			return nil
		}

		agentInstance.AddUserMessage(input)

		cancelChan := make(chan os.Signal, 1)
		signal.Notify(cancelChan, os.Interrupt, syscall.SIGTERM)

		go func() {
			time.Sleep(100 * time.Millisecond)
			fmt.Printf("\n%sAgent%s %s›%s Thinking...\n\n", colors.BrightBlue, colors.Reset, colors.Dim, colors.Reset)
		}()

		_, err = agentInstance.Run(nil)

		signal.Stop(cancelChan)

		if err != nil {
			fmt.Printf("\n%s❌ Error: %v%s\n", colors.Red, err, colors.Reset)
		}

		fmt.Printf("\n%s%s\n", colors.Dim, strings.Repeat("─", 60))
		fmt.Println()
	}

	return nil
}

func main() {
	workspace := flag.String("workspace", "", "Workspace directory")
	task := flag.String("task", "", "Execute a task non-interactively")
	version := flag.Bool("version", false, "Show version")
	flag.Parse()

	if *version {
		fmt.Println("mini-agent 0.1.0")
		return
	}

	workspaceDir, _ := filepath.Abs(".")
	if *workspace != "" {
		workspaceDir, _ = filepath.Abs(*workspace)
	}

	os.MkdirAll(workspaceDir, 0755)

	if err := runAgent(workspaceDir, *task); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func init() {
	if runtime.GOOS == "windows" {
		signal.Ignore(os.Signal(syscall.SIGINT))
	}
}
