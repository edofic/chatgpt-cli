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

func main() {
	maxTokens := flag.Int("maxTokens", 500, "Maximum number of tokens to generate")
	systemMsg := flag.String("systemMsg", "", "System message to include with the prompt")
	includeFile := flag.String("includeFile", "", "File to include with the prompt")
	temperature := flag.Float64("temperature", 0, "ChatGPT temperature")
	continueSession := flag.Bool("c", false, "Continue last session (ignores other flags)")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options] message\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()
	msg := strings.TrimSpace(strings.Join(flag.Args(), " "))
	if msg == "" {
		flag.Usage()
		return
	} else if msg == "-" {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			msg += scanner.Text() + "\n"
		}
	}

	apiKey := os.Getenv("OPENAI_API_KEY")
	client := openai.NewClient(apiKey)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	var req openai.ChatCompletionRequest
	if *continueSession {
		session, err := os.ReadFile("/tmp/chatgpt-cli-last-session.json")
		if err != nil {
			panic(err)
		}
		err = json.Unmarshal(session, &req)
		if err != nil {
			panic(err)
		}
	} else {
		msgs := []openai.ChatCompletionMessage{}
		if *systemMsg != "" {
			msgs = append(msgs, openai.ChatCompletionMessage{Role: openai.ChatMessageRoleSystem, Content: *systemMsg})
		}
		req = openai.ChatCompletionRequest{
			Model:       openai.GPT3Dot5Turbo,
			MaxTokens:   *maxTokens,
			Temperature: float32(*temperature),
			Stream:      true,
			Messages:    msgs,
		}
	}
	req.Messages = append(req.Messages, openai.ChatCompletionMessage{Role: openai.ChatMessageRoleUser, Content: msg})
	if *includeFile != "" {
		contents, err := os.ReadFile(*includeFile)
		if err != nil {
			panic(err)
		}
		req.Messages = append(
			req.Messages,
			openai.ChatCompletionMessage{Role: openai.ChatMessageRoleUser, Content: string(contents)},
		)
	}
	stream, err := client.CreateChatCompletionStream(ctx, req)
	if err != nil {
		fmt.Printf("ChatCompletionStream error: %v\n", err)
		return
	}
	defer stream.Close()

	responseChunks := []string{}
	for {
		response, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			fmt.Println()
			break
		}

		if err != nil {
			fmt.Printf("\nStream error: %v\n", err)
			return
		}

		chunk := response.Choices[0].Delta.Content
		fmt.Print(chunk)
		responseChunks = append(responseChunks, chunk)
	}

	fullResponse := strings.Join(responseChunks, "")
	req.Messages = append(req.Messages, openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleAssistant,
		Content: fullResponse,
	})

	resJson, err := json.Marshal(req)
	if err != nil {
		panic(err)
	}
	os.WriteFile("/tmp/chatgpt-cli-last-session.json", resJson, 0644)
}
