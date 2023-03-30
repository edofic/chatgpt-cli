package main

import (
	"context"
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
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options] message\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()
	msg := strings.Join(flag.Args(), " ")

	apiKey := os.Getenv("OPENAI_API_KEY")
	client := openai.NewClient(apiKey)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	msgs := []openai.ChatCompletionMessage{}
	if *systemMsg != "" {
		msgs = append(msgs, openai.ChatCompletionMessage{Role: openai.ChatMessageRoleSystem, Content: *systemMsg})
	}
	msgs = append(msgs, openai.ChatCompletionMessage{Role: openai.ChatMessageRoleUser, Content: msg})
	if *includeFile != "" {
		contents, err := os.ReadFile(*includeFile)
		if err != nil {
			panic(err)
		}
		msgs = append(msgs, openai.ChatCompletionMessage{Role: openai.ChatMessageRoleUser, Content: string(contents)})
	}
	req := openai.ChatCompletionRequest{
		Model:       openai.GPT3Dot5Turbo,
		MaxTokens:   *maxTokens,
		Temperature: float32(*temperature),
		Stream:      true,
		Messages:    msgs,
	}
	stream, err := client.CreateChatCompletionStream(ctx, req)
	if err != nil {
		fmt.Printf("ChatCompletionStream error: %v\n", err)
		return
	}
	defer stream.Close()

	for {
		response, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			fmt.Println()
			return
		}

		if err != nil {
			fmt.Printf("\nStream error: %v\n", err)
			return
		}

		fmt.Printf(response.Choices[0].Delta.Content)
	}
}
