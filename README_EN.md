# Mini-Agent

**中文** | [English](./README.md)

An interactive AI agent framework written in Go, based on the **ReAct** (Reasoning + Acting) pattern, that accomplishes complex tasks through a combination of text generation and tool execution.

The Agent automatically performs Thought → Action → Observe → Reason loops until the task is completed.

## Features

- **ReAct Pattern** - Explicit reasoning + tool execution with visible thinking process
- **Multi-turn Conversations** - Engage in iterative dialogues with the agent
- **Tool Execution** - Execute bash commands, read/write files, and manage notes
- **Multiple LLM Providers** - Support for OpenAI and Anthropic-compatible APIs
- **Token Management** - Automatic message summarization when context exceeds limits
- **Retry Mechanism** - Exponential backoff retry on API failures
- **MCP Support** - Connect to Model Context Protocol servers for extended tools
- **Persistent Memory** - Cross-session memory for user preferences and important information

## Screenshot

![Mini-Agent Screenshot](./static/image.png)

## Installation

### Prerequisites

- Go 1.21+
- Access to OpenAI or Anthropic API (or compatible service like MiniMax)
- `uvx` (for MCP servers, if using MCP functionality)

### Build

```bash
git clone <repository>
cd mini_agent
go build -o mini-agent ./cmd/mini-agent
```

## Configuration

### Environment Variables

```bash
# MiniMax API Key (required)
export MINIMAX_API_KEY="your-api-key"

# Or Windows PowerShell
$env:MINIMAX_API_KEY = "your-api-key"
```

### Configuration File

Create `config.yaml` in one of these locations (searched in order):

1. `./mini_agent/config/config.yaml`
2. `~/.mini-agent/config/config.yaml`
3. `<executable_dir>/config/config.yaml`

### MCP Configuration (mcp.json)

```json
{
  "servers": [
    {
      "name": "MiniMax",
      "command": "uvx",
      "args": ["minimax-coding-plan-mcp", "-y"],
      "env": [
        "MINIMAX_API_KEY=your-api-key",
        "MINIMAX_API_HOST=https://api.minimaxi.com"
      ]
    }
  ]
}
```

**Note**: Windows users need to install `uv` first:

```powershell
powershell -ExecutionPolicy ByPass -c "irm https://astral.sh/uv/install.ps1 | iex"
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

This project uses the **ReAct** (Reasoning + Acting) pattern, introduced by **Princeton + Google** in 2022.

### Core Loop

```
┌─────────────────────────────────────────────────────────────┐
│                     ReAct Loop                               │
├─────────────────────────────────────────────────────────────┤
│                                                              │
│  for step < MaxSteps:                                       │
│                                                              │
│  ┌───────────┐  1. Think - LLM decides what tool to use     │
│  │  Thought  │                                               │
│  └─────┬─────┘                                               │
│        ▼                                                     │
│  ┌───────────┐  2. Act - Execute tool call                  │
│  │   Action  │                                               │
│  └─────┬─────┘                                               │
│        ▼                                                     │
│  ┌───────────┐  3. Observe - Get tool return result         │
│  │  Observe  │                                               │
│  └─────┬─────┘                                               │
│        │                                                     │
│        ▼                                                     │
│  ┌───────────┐  4. Reason - Decide next step or finish      │
│  │  Reason   │ ───► Continue loop or return result          │
│  └───────────┘                                               │
│                                                              │
└─────────────────────────────────────────────────────────────┘
```

### Execution Stages

| Stage   | Description                    | Code Indicator            |
|---------|-------------------------------|---------------------------|
| **Thought** | LLM analyzes task, decides tool | `🧠 Thinking:` display   |
| **Action** | Execute tool call             | `🔧 Tool Call:`          |
| **Observe** | Get tool return result        | `✓ Result:`             |
| **Reason** | Decide next step or finish    | Back to step 1 or end   |

### Flow Diagram

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│   User      │     │   Agent     │     │    LLM      │     │   Tools     │
└──────┬──────┘     └──────┬──────┘     └──────┬──────┘     └──────┬──────┘
       │                   │                   │                   │
       │  Input Task       │  Load Config      │                   │
       │──────────────────>│                   │                   │
       │                   │  Generate Response│                   │
       │                   │───────────────────────────────────────>
       │                   │<───────────────────────────────────────│
       │                   │                   │                   │
       │<──────────────────│                   │                   │
       │                   │                   │                   │
       │                   │  Tool Call? ───► Execute Tool         │
       │                   │<───────────────────────────────────────│
       │                   │                   │                   │
       │                   │  Loop or Finish                         │
```

## Available Tools

### Built-in Tools

| Tool           | Description                    | Parameters                                |
| -------------- | ------------------------------ | ----------------------------------------- |
| `bash`         | Execute shell commands         | `command`, `timeout`, `run_in_background` |
| `bash_output`  | Get background command output   | `id`, `filter_str`                        |
| `bash_kill`    | Terminate background command   | `id`                                      |
| `read_file`    | Read file contents             | `path`, `offset`, `limit`                 |
| `write_file`   | Write content to file          | `path`, `content`                         |
| `edit_file`    | Edit file (single replacement)  | `path`, `old_str`, `new_str`             |
| `record_note`  | Record a session note          | `content`, `category`                     |
| `recall_notes` | Retrieve session notes         | `category`                                |

### Persistent Memory Tools

| Tool               | Description                          | Parameters                    |
| ------------------ | ------------------------------------ | ----------------------------- |
| `save_memory`      | Save info to persistent memory       | `content`, `category`, `key` |
| `recall_memory`    | Retrieve from persistent memory       | `query`, `category`           |
| `summarize_session`| Save session summary                 | `summary`                     |

### MCP Tools

MCP tools depend on configured MCP servers. For example, MiniMax MCP provides:

| Tool              | Description        |
| ----------------- | ------------------ |
| `web_search`      | Web search         |
| `understand_image`| Image understanding|

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
