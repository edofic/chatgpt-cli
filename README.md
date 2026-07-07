# LLM CLI

[![main](https://github.com/edofic/llm-cli/actions/workflows/main.yml/badge.svg)](https://github.com/edofic/llm-cli/actions/workflows/main.yml)

This is a command line client for using LLMs over OpenAI-compatible APIs. You can easily ask questions (responses are streamed in real time), tweak system prompt (or other params), include local files, pipe the response and ask follow up questions. 

https://github.com/user-attachments/assets/89777daa-6774-4016-a595-f654c4ae2a56

## Installation

```
go install github.com/edofic/llm-cli@latest
```

## Usage

It uses the API so you need to provide your own token via `OPENAI_API_KEY` environment variable.

```sh
export OPENAI_API_KEY=your_api_key
```

Without any params the arguments (plural, you can omit the quotes) are taken to be your message

```sh
$ llm-cli 'what is the capital of france?'
The capital of France is Paris.
```

*Note* responses are streamed as they are generated (just as on the web UI) which gives you something almost immediately even for longer responses.

### Reading from stdin

Pass `-` as the message to read the prompt from stdin. Useful for piping in content.

```sh
$ cat notes.txt | llm-cli -
```

### Tuning the response

Two flags let you tune the model output:

- `-maxTokens` sets the maximum number of tokens to generate (default `500`)
- `-temperature` sets the sampling temperature (default `0`)

```sh
$ llm-cli -maxTokens 2000 -temperature 0.7 'write a short poem about Go'
```

### Interactive mode

If you invoke the CLI without a message, it drops into an interactive REPL. Type a message and press Enter to send; the response streams back and the session stays open for follow-ups. Press Ctrl-D (or Ctrl-C) to exit.

```sh
$ llm-cli
> what is the capital of france?
The capital of France is Paris.
> and germany?
The capital of Germany is Berlin.
>
```

Flags like `-c`, `-systemMsg`, and `-includeFile` work the same way in interactive mode. `-includeFile` loads the file as initial context before the first prompt. Each exchange is saved to the session file, so you can resume later with `-c`.

### Follow up questions

You can pass `-c` to _continue_ last session and ask follow up questions.

```sh
$ llm-cli -c 'what about germany?' 
The capital of Germany is Berlin.
```

Session is stored in `/tmp/llm-cli-last-session.json` - there is only one "last session".

### System message

You can configure the system prompt and tweak behavior this way 

```sh
$ llm-cli -systemMsg 'You are an assistant that speaks like Shakespeare.' 'what is the capital of france?'
The fair capital of France is known as Paris, my would-be lord.
```

### Files 

You can pass in local files as additional messages

```sh
$ llm-cli -includeFile main.go 'what is this in one sentence?'               
This is a Go programming language script that uses an OpenAI-compatible API to provide a command-line interface for interacting with AI language models.
```

Or you can store the output

```sh
llm-cli 'write a dockerfile for a Go program, omit any explanation' | tee Dockerfile
FROM golang:latest
WORKDIR /app
COPY . .
RUN go build -o myApp .
CMD ["./myApp"]
```

### Model versions

Currently this tool defaults to `gpt-5.4-mini`.
You can override the model with the `-model` flag or the `OPENAI_MODEL` environment variable (the flag takes precedence):

```sh
llm-cli -model gpt-4o 'explain this'
```

```sh
export OPENAI_MODEL=gpt-4o
```


### Custom API endpoint

To point at any OpenAI-compatible API (e.g. a local LLM server or a proxy), set `OPENAI_ENDPOINT`:

```sh
export OPENAI_ENDPOINT='http://localhost:8080/v1'
```

## Agentic mode

Pass `-agent` to give the model tools it can call to accomplish multi-step tasks. The model loops autonomously, calling tools and feeding results back, until it decides it is done.

```sh
$ llm-cli -agent 'add a TODO comment at the top of main.go'
tool call read("main.go")
tool call edit("main.go")
Done. Added a TODO comment at the top of main.go.
```

Each tool call is printed to stdout as it happens so you can follow along.

### Available tools

| Tool | What it does |
|------|-------------|
| `read` | Read a local file, with optional line offset and limit |
| `write` | Create a new file (fails if it already exists) |
| `edit` | Replace an exact string inside an existing file (fails if not found or not unique) |
| `bash` | Run a shell command and return combined stdout+stderr (disabled by default; see below) |

### Bash tool and sandboxing

The `bash` tool is disabled by default. Enable it with `-tool-mode`:

```sh
# Sandboxed: cwd read/write only, no network, uses fence
llm-cli -agent -tool-mode=safe 'run the tests and fix any failures'

# Unrestricted: full filesystem and network access
llm-cli -agent -tool-mode=unsafe 'set up the project dependencies'
```

`safe` mode uses [fence](https://github.com/fencesandbox/fence) to enforce:
- filesystem writes restricted to the current directory
- filesystem reads restricted to the current directory (system paths blocked)
- all outbound network blocked

`unsafe` gives the model a plain `sh -c` with no restrictions. Use it only when you trust the task and the model output.

### Customising the sandbox with fence.jsonc

When `-tool-mode=safe` is active, llm-cli looks for a `fence.jsonc` in the current directory (and walks up to the user config at `~/.config/fence/fence.jsonc`). Settings there are merged on top of the defaults, so you can poke holes without switching to `unsafe`:

```jsonc
// fence.jsonc
{
  "network": {
    // allow the model to fetch packages
    "allowedDomains": ["*.npmjs.org", "registry.npmjs.org"]
  },
  "filesystem": {
    // also allow reading the shared fixtures directory
    "allowRead": ["../fixtures"]
  }
}
```

See the [fence documentation](https://github.com/fencesandbox/fence) for the full config reference.

## Configuration reference

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `-maxTokens` | `8192` | Maximum number of tokens to generate |
| `-temperature` | `0` | Sampling temperature |
| `-model` | `""` | Model to use (overrides `OPENAI_MODEL`) |
| `-systemMsg` | `""` | System message to include with the prompt |
| `-includeFile` | `""` | File to include as an additional user message |
| `-pretty` | auto | Render markdown; defaults to true when stdout is a TTY |
| `-c` | `false` | Continue last session |
| `-agent` | `false` | Enable agentic mode (read/write/edit tools) |
| `-tool-mode` | `off` | Bash tool: `off`, `safe` (fence-sandboxed), `unsafe` |

### Environment variables

| Variable | Description |
| --- | --- |
| `OPENAI_API_KEY` | API key (required) |
| `OPENAI_MODEL` | Override the default model (`gpt-5.4-mini`) |
| `OPENAI_ENDPOINT` | Base URL for an OpenAI-compatible API (e.g. `http://localhost:8080/v1`) |
