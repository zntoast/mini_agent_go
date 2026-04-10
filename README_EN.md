# Mini-Agent

**中文** | [English](./README.md)

An interactive AI agent framework written in Go that uses Large Language Models (LLMs) to accomplish tasks through a combination of text generation and tool execution.

## Features

- **Multi-turn Conversations** - Engage in iterative dialogues with the agent
- **Tool Execution** - Execute bash commands, read/write files, and manage notes
- **Multiple LLM Providers** - Support for OpenAI and Anthropic-compatible APIs
- **Token Management** - Automatic message summarization when context exceeds limits
- **Retry Mechanism** - Exponential backoff retry on API failures
- **ACP Protocol** - Model Context Protocol server for external integrations

## Project Structure

```
mini_agent/
├── cmd/
│   └── mini-agent/
│       └── main.go              # Entry point, CLI handling, REPL loop
├── pkg/
│   ├── agent/
│   │   └── agent.go             # Core Agent: decision loop, message management
│   ├── config/
│   │   └── config.go            # YAML configuration loading
│   ├── schema/
│   │   └── schema.go            # Message, ToolCall, LLMResponse types
│   ├── llm/
│   │   ├── base.go              # LLMClientBase interface
│   │   ├── llm_wrapper.go       # Provider router (Anthropic/OpenAI)
│   │   ├── openai_client.go     # OpenAI-compatible API client
│   │   ├── anthropic_client.go  # Anthropic API client
│   │   └── retry.go             # Exponential backoff retry logic
│   ├── tools/
│   │   ├── base.go              # Tool interface definition
│   │   ├── bash_tool.go         # Bash/PowerShell execution
│   │   ├── file_tools.go        # File read/write/edit operations
│   │   └── note_tool.go         # Session notes recording
│   ├── logger/
│   │   └── logger.go            # Request/response logging
│   ├── utils/
│   │   └── terminal.go          # Display width calculation
│   └── acp/
│       └── server.go            # Model Context Protocol server
├── config.yaml                  # Configuration file
├── go.mod                       # Go module definition
└── go.sum                       # Dependencies checksums
```

## Installation

### Prerequisites

- Go 1.21+
- Access to OpenAI or Anthropic API (or compatible service like MiniMax)

### Build

```bash
git clone <repository>
cd mini_agent
go build -o mini-agent ./cmd/mini-agent
```

## Configuration

Create `config.yaml` in one of these locations (searched in order):

1. `./mini_agent/config/config.yaml`
2. `~/.mini-agent/config/config.yaml`
3. `<executable_dir>/config/config.yaml`

### Configuration Options

```yaml
# LLM Configuration
llm:
  api_key: "YOUR_API_KEY" # Required: API authentication
  api_base: "https://api.minimaxi.com" # API endpoint
  model: "MiniMax-Text-01" # Model name
  provider: "anthropic" # "anthropic" or "openai"

  retry:
    enabled: true
    max_retries: 3
    initial_delay: 1.0
    max_delay: 60.0
    exponential_base: 2.0

# Agent Configuration
agent:
  max_steps: 100 # Max tool-use iterations
  workspace_dir: "./workspace" # Working directory
  system_prompt_path: "system_prompt.md"

# Tools Configuration
tools:
  enable_file_tools: true # File read/write/edit
  enable_bash: true # Execute commands
  enable_note: true # Session notes
  enable_mcp: false # Model Context Protocol
```

## Usage

### Interactive Mode

```bash
./mini-agent --workspace /path/to/project
```

Commands:

- `/help` - Show help
- `/clear` - Clear session history
- `/history` - Show message count
- `/stats` - Show session statistics
- `/exit`, `/quit`, `/q` - Exit program

### Task Mode

```bash
./mini-agent --workspace /path/to/project --task "Your task here"
```

## Execution Flow

### Sequence Diagram

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│   User      │     │   Agent     │     │    LLM      │     │   Tools     │
└──────┬──────┘     └──────┬──────┘     └──────┬──────┘     └──────┬──────┘
       │                   │                   │                   │
       │  1. Input Task    │                   │                   │
       │──────────────────>│                   │                   │
       │                   │  2. Load Config    │                   │
       │                   │──────┐             │                   │
       │                   │      │             │                   │
       │                   │<─────┘             │                   │
       │                   │                   │                   │
       │                   │  3. Generate (messages + tools)       │
       │                   │───────────────────────────────────────>
       │                   │                   │                   │
       │                   │  4. Response (no tool_calls)            │
       │                   │<───────────────────────────────────────│
       │                   │                   │                   │
       │  5. Display       │                   │                   │
       │<──────────────────│                   │                   │
       │                   │                   │                   │
       │                   │ OR:                │                   │
       │                   │                   │                   │
       │                   │  4. Response (with tool_calls)         │
       │                   │<───────────────────────────────────────│
       │                   │                   │                   │
       │                   │  5. Execute Tool   │                   │
       │                   │───────────────────────────────────────>
       │                   │                   │                   │
       │                   │  6. Tool Result   │                   │
       │                   │<───────────────────────────────────────│
       │                   │                   │                   │
       │                   │  7. Add to history, loop back to 3      │
       │                   │───────────────────┘                   │
       │                   │                   │                   │
       │  8. Final Result  │                   │                   │
       │<──────────────────│                   │                   │
```

### Agent Loop

```
┌─────────────────────────────────────────────────────────────────┐
│                     Agent Execution Loop                         │
├─────────────────────────────────────────────────────────────────┤
│  for step < MaxSteps:                                           │
│                                                                 │
│  ┌─────────────┐                                                │
│  │ 1. Check    │  Check cancellation / max steps                │
│  │    Precond  │                                                │
│  └──────┬──────┘                                                │
│         │                                                       │
│         v                                                       │
│  ┌─────────────┐                                                │
│  │ 2. Summarize│  If token count > limit, compress history     │
│  │    Messages │                                                │
│  └──────┬──────┘                                                │
│         │                                                       │
│         v                                                       │
│  ┌─────────────┐                                                │
│  │ 3. Prepare  │  Convert tools to schema format                │
│  │    Tools    │                                                │
│  └──────┬──────┘                                                │
│         │                                                       │
│         v                                                       │
│  ┌─────────────┐                                                │
│  │ 4. LLM      │  Send messages + tools to LLM                  │
│  │    Generate │                                                │
│  └──────┬──────┘                                                │
│         │                                                       │
│         v                                                       │
│  ┌─────────────┐                                                │
│  │ 5. Display  │  Show thinking & content                       │
│  │    Output   │                                                │
│  └──────┬──────┘                                                │
│         │                                                       │
│         v                                                       │
│  ┌─────────────┐     ┌─────────────┐                            │
│  │ 6. Tool     │────>│  Execute    │  For each tool_call:       │
│  │    Calls?   │ Yes │  Tool       │  - Parse arguments          │
│  └──────┬──────┘     │  Results    │  - Execute tool            │
│         │ No         └──────┬──────┘  - Add to history         │
│         │                   │                                   │
│         v                   v                                   │
│  ┌─────────────┐     ┌─────────────┐                            │
│  │ 7. Return   │<────│ 8. Loop    │                            │
│  │    Result   │     │    Back    │                            │
│  └─────────────┘     └─────────────┘                            │
└─────────────────────────────────────────────────────────────────┘
```

## Available Tools

| Tool           | Description                    | Parameters                                |
| -------------- | ------------------------------ | ----------------------------------------- |
| `bash`         | Execute shell commands         | `command`, `timeout`, `run_in_background` |
| `bash_output`  | Get background command output  | `id`, `filter_str`                        |
| `bash_kill`    | Terminate background command   | `id`                                      |
| `read_file`    | Read file contents             | `path`, `offset`, `limit`                 |
| `write_file`   | Write content to file          | `path`, `content`                         |
| `edit_file`    | Edit file (single replacement) | `path`, `old_str`, `new_str`              |
| `record_note`  | Record a session note          | `content`, `category`                     |
| `recall_notes` | Retrieve session notes         | `category`                                |

## Token Management

- **Local estimation**: `char_count / 2.5`
- **API reported**: From LLM response `usage.total_tokens`
- **Summarization trigger**: When either exceeds `TokenLimit` (80000)
- **Summarization strategy**: Keeps system prompt + user messages, compresses assistant/tool rounds

## Logging

Logs are written to `~/.mini-agent/log/agent_run_YYYYMMDD_HHMMSS.log`:

```json
{"level":"REQUEST","timestamp":"...","messages":[...]}
{"level":"RESPONSE","timestamp":"...","content":"...","tool_calls":[...]}
{"level":"TOOL_RESULT","timestamp":"...","tool":"read_file","result":"..."}
```

## License

MIT
