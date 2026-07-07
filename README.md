# LLM CLI

[![main](https://github.com/edofic/llm-cli/actions/workflows/main.yml/badge.svg)](https://github.com/edofic/llm-cli/actions/workflows/main.yml)

This is a command line client for using LLMs over OpenAI-compatible APIs. You can easily ask questions (responses are streamed in real time), tweak system prompt (or other params), include local files, pipe the response and ask follow up questions. 


https://user-images.githubusercontent.com/597476/230737546-d9465089-d323-45a5-87d7-84901d8054c7.mov


## Instalation

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
$ llm-cli 'what is the capital of france?'                                                         /tmp
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

Flags like `-c`, `-systemMsg`, and `-includeFile` work the same way in interactive mode. Each exchange is saved to the session file, so you can resume later with `-c`.

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
You can alternatively use any other available model by specifying the model
name in an environment variable

```sh
export OPENAI_MODEL=gpt-4o
```


### Custom API endpoint

To point at any OpenAI-compatible API (e.g. a local LLM server or a proxy), set `OPENAI_ENDPOINT`:

```sh
export OPENAI_ENDPOINT='http://localhost:8080/v1'
```

### Azure OpenAI Service

To use Azure hosted models specify two more environment variables on top of your API key

```sh
OPENAI_AZURE_ENDPOINT='https://<your subdomain>.openai.azure.com/'
OPENAI_AZURE_MODEL='<your model deployment name>'
```

## Configuration reference

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `-maxTokens` | `500` | Maximum number of tokens to generate |
| `-temperature` | `0` | Sampling temperature |
| `-systemMsg` | `""` | System message to include with the prompt |
| `-includeFile` | `""` | File to include as an additional user message |
| `-c` | `false` | Continue last session |

### Environment variables

| Variable | Description |
| --- | --- |
| `OPENAI_API_KEY` | API key (required) |
| `OPENAI_MODEL` | Override the default model (`gpt-5.4-mini`) |
| `OPENAI_ENDPOINT` | Base URL for an OpenAI-compatible API |
| `OPENAI_AZURE_ENDPOINT` | Azure OpenAI endpoint URL |
| `OPENAI_AZURE_MODEL` | Azure deployment name |
