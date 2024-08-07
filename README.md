# ChatGPT CLI

[![main](https://github.com/edofic/chatgpt-cli/actions/workflows/main.yml/badge.svg)](https://github.com/edofic/chatgpt-cli/actions/workflows/main.yml)

This is a command line client for using ChatGPT. You can easily ask questions (responses are streamed in real time), tweak system prompt (or other params), include local files, pipe the response and ask follow up questions. 


https://user-images.githubusercontent.com/597476/230737546-d9465089-d323-45a5-87d7-84901d8054c7.mov


## Instalation

```
go install github.com/edofic/chatgpt-cli@latest
```

## Usage

It uses the API so you need to provide your own token via `OPENAI_API_KEY` environment variable.

```sh
export OPENAI_API_KEY=your_api_key
```

Without any params the arguments (plural, you can omit the quotes) are taken to be your message

```sh
$ chatgpt-cli 'what is the capital of france?'                                                         /tmp
The capital of France is Paris.
```

*Note* responses are streamed as they are generated (just as on the web UI) which gives you something almost immediately even for longer responses.

### Follow up questions

You can pass `-c` to _continue_ last session and ask follow up questions.

```sh
$ chatgpt-cli -c 'what about germany?' 
The capital of Germany is Berlin.
```

Session is stored in `/tmp/chatgpt-cli-last-session.json` - there is only one "last session".

### System message

You can configure the system prompt and tweak behavior this way 

```sh
$ chatgpt-cli -systemMsg 'You are an assistant that speaks like Shakespeare.' 'what is the capital of france?'
The fair capital of France is known as Paris, my would-be lord.
```

### Files 

You can pass in local files as additional messages

```sh
$ chatgpt-cli -includeFile main.go 'what is this in one sentence?'               
This is a Go programming language script that uses the OpenAI API to provide a command-line interface for interacting with the ChatGPT AI model to generate chat messages.
```

Or you can store the output

```sh
chatgpt-cli 'write a dockerfile for a Go program, omit any explanation' | tee Dockerfile
FROM golang:latest
WORKDIR /app
COPY . .
RUN go build -o myApp .
CMD ["./myApp"]
```

### Model versions

Currently this tool defaults to
[`gpt-4o-mini`](https://platform.openai.com/docs/models/gpt-4o-mini).
You can alternatively use any of the 3.5 & 4 models by specifying the model
name in an environment variable

```sh
export OPENAI_MODEL=gpt-3.5-turbo-0125
```


### Azure OpenAI Service

To use Azure hosted models specify two more environment variables on top of your API key

```sh
OPENAI_AZURE_ENDPOINT='https://<your subdomain>.openai.azure.com/'
OPENAI_AZURE_MODEL='<your model deployment name>'
```
