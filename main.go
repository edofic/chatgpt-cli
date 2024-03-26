package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	openai "github.com/sashabaranov/go-openai"
)

const (
	defaultModel = openai.GPT3Dot5Turbo
	sessionFile  = "/tmp/chatgpt-cli-last-session.json"
)

type params struct {
	maxTokens       int
	systemMsg       string
	includeFile     string
	temperature     float64
	continueSession bool
	msg             string
}

func main() {
	p := parseArgs()

	client := getClient()
	model := os.Getenv("OPENAI_MODEL")
	if model == "" {
		model = defaultModel
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	req := getCompletionRequest(p, model)
	req = appendMessages(req, p)

	fullResponse, err := streamCompletion(ctx, client, req, func(chunk string) error {
		_, err := fmt.Print(chunk)
		return err
	})
	fmt.Println()
	if err != nil {
		panic(err)
	}

	req.Messages = append(req.Messages, openai.ChatCompletionMessage{Role: openai.ChatMessageRoleAssistant, Content: fullResponse})

	err = saveCompletion(req)
	if err != nil {
		panic(err)
	}
}

func parseArgs() params {
	// var versions of flags from main, returning a params struct
	var p params
	flag.IntVar(&p.maxTokens, "maxTokens", 500, "Maximum number of tokens to generate")
	flag.StringVar(&p.systemMsg, "systemMsg", "", "System message to include with the prompt")
	flag.StringVar(&p.includeFile, "includeFile", "", "File to include with the prompt")
	flag.Float64Var(&p.temperature, "temperature", 0, "ChatGPT temperature")
	flag.BoolVar(&p.continueSession, "c", false, "Continue last session (ignores other flags)")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options] message\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()
	msg := strings.TrimSpace(strings.Join(flag.Args(), " "))
	if msg == "" {
		flag.Usage()
		os.Exit(1)
	} else if msg == "-" {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			msg += scanner.Text() + "\n"
		}
	}
	p.msg = msg
	return p
}

func getClient() *openai.Client {
	apiKey := os.Getenv("OPENAI_API_KEY")
	url := os.Getenv("OPENAI_AZURE_ENDPOINT")
	if url != "" {
		deployment := os.Getenv("OPENAI_AZURE_MODEL")
		config := openai.DefaultAzureConfig(apiKey, url)
		config.AzureModelMapperFunc = func(model string) string {
			if deployment != "" {
				return deployment
			}
			return model
		}
		return openai.NewClientWithConfig(config)
	}
	return openai.NewClient(apiKey)
}

func getCompletionRequest(p params, model string) openai.ChatCompletionRequest {
	if p.continueSession {
		req := loadLastCompletion()
		if req != nil {
			return *req
		}
		fmt.Println("WARN: failed to load previous session, starting a new one")
	}
	return newCompletionRequest(p, model)
}

func loadLastCompletion() *openai.ChatCompletionRequest {
	var req openai.ChatCompletionRequest
	session, err := os.ReadFile(sessionFile)
	if err != nil {
		return nil
	}
	err = json.Unmarshal(session, &req)
	if err != nil {
		return nil
	}
	return &req
}

func saveCompletion(req openai.ChatCompletionRequest) error {
	resJson, err := json.Marshal(req)
	if err != nil {
		return err
	}
	return os.WriteFile(sessionFile, resJson, 0644)
}

func newCompletionRequest(p params, model string) openai.ChatCompletionRequest {
	msgs := []openai.ChatCompletionMessage{}
	if p.systemMsg != "" {
		msgs = append(msgs, openai.ChatCompletionMessage{Role: openai.ChatMessageRoleSystem, Content: p.systemMsg})
	}
	return openai.ChatCompletionRequest{
		Model:       model,
		MaxTokens:   p.maxTokens,
		Temperature: float32(p.temperature),
		Stream:      true,
		Messages:    msgs,
	}
}

func appendMessages(req openai.ChatCompletionRequest, p params) openai.ChatCompletionRequest {
	req.Messages = append(req.Messages, openai.ChatCompletionMessage{Role: openai.ChatMessageRoleUser, Content: p.msg})
	if p.includeFile != "" {
		contents, err := os.ReadFile(p.includeFile)
		if err != nil {
			panic(err)
		}
		req.Messages = append(
			req.Messages,
			openai.ChatCompletionMessage{Role: openai.ChatMessageRoleUser, Content: string(contents)},
		)
	}
	return req
}

func streamCompletion(ctx context.Context, client *openai.Client, req openai.ChatCompletionRequest, callback func(chunk string) error) (fullResponse string, err error) {
	stream, err := client.CreateChatCompletionStream(ctx, req)
	if err != nil {
		return "", fmt.Errorf("ChatCompletionStream error: %v\n", err)
	}
	defer stream.Close()

	responseChunks := []string{}
	for {
		response, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return "", fmt.Errorf("stream error: %v\n", err)
		}

		chunk := response.Choices[0].Delta.Content
		err = callback(chunk)
		if err != nil {
			return "", fmt.Errorf("callback error: %v\n", err)
		}
		responseChunks = append(responseChunks, chunk)
	}
	return strings.Join(responseChunks, ""), nil
}
