package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/glamour"
	"golang.org/x/term"
)

const (
	defaultModel    = "gpt-5.4-mini"
	defaultEndpoint = "https://api.openai.com/v1/chat/completions"
	sessionFile     = "/tmp/llm-cli-last-session.json"
)

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type completionRequest struct {
	Model               string    `json:"model"`
	Messages            []message `json:"messages"`
	MaxCompletionTokens int       `json:"max_completion_tokens,omitempty"`
	Temperature         float32   `json:"temperature,omitempty"`
	Stream              bool      `json:"stream"`
}

type citation struct {
	RefID int    `json:"ref_id"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

type deltaContent struct {
	Content string `json:"content"`
}

type streamChoice struct {
	Delta deltaContent `json:"delta"`
}

type streamChunk struct {
	Choices   []streamChoice `json:"choices"`
	Citations []citation     `json:"citations"`
}

type params struct {
	maxTokens       int
	systemMsg       string
	includeFile     string
	temperature     float64
	continueSession bool
	interactive     bool
	pretty          bool
	msg             string
}

func main() {
	p := parseArgs()

	model := os.Getenv("OPENAI_MODEL")
	if model == "" {
		model = defaultModel
	}

	if p.interactive {
		runInteractive(model, p)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	req := getCompletionRequest(p, model)
	req.Messages = append(req.Messages, message{Role: "user", Content: p.msg})
	if p.includeFile != "" {
		contents, err := os.ReadFile(p.includeFile)
		if err != nil {
			panic(err)
		}
		req.Messages = append(req.Messages, message{Role: "user", Content: string(contents)})
	}

	isTTY := p.pretty
	var renderer *ttyRenderer
	var buf strings.Builder
	if isTTY {
		renderer = &ttyRenderer{}
	}

	fullResponse, citations, err := streamCompletion(ctx, req, func(chunk string) error {
		if isTTY {
			buf.WriteString(chunk)
			renderer.render(buf.String())
			return nil
		}
		_, err := fmt.Print(chunk)
		return err
	})
	if !isTTY {
		fmt.Println()
	}
	if err != nil {
		panic(err)
	}
	printCitations(citations)

	req.Messages = append(req.Messages, message{Role: "assistant", Content: fullResponse})
	if err := saveCompletion(req); err != nil {
		panic(err)
	}
}

type ttyRenderer struct {
	prev string
}

func (r *ttyRenderer) render(text string) {
	rendered, err := glamour.Render(text, "dark")
	if err != nil {
		rendered = text
	}

	// Find which line the two outputs first diverge on
	prevLines := strings.SplitAfter(r.prev, "\n")
	newLines := strings.SplitAfter(rendered, "\n")
	commonLines := 0
	for commonLines < len(prevLines) && commonLines < len(newLines) && prevLines[commonLines] == newLines[commonLines] {
		commonLines++
	}

	// Move cursor up past the lines that changed and clear from there
	clearLines := len(prevLines) - commonLines
	if clearLines > 0 {
		fmt.Printf("\033[%dA\033[J", clearLines)
	}
	fmt.Print(strings.Join(newLines[commonLines:], ""))
	r.prev = rendered
}

func printCitations(citations []citation) {
	if len(citations) == 0 {
		return
	}
	fmt.Println("\nSources:")
	for _, c := range citations {
		fmt.Printf("  [%d] %s\n      %s\n", c.RefID, c.Title, c.URL)
	}
}

func printSession(req completionRequest, pretty bool) {
	for _, m := range req.Messages {
		switch m.Role {
		case "system":
			fmt.Printf("[system] %s\n", m.Content)
		case "user":
			fmt.Printf("> %s\n", m.Content)
		case "assistant":
			if pretty {
				r := &ttyRenderer{}
				r.render(m.Content)
			} else {
				fmt.Printf("%s\n", m.Content)
			}
		}
	}
}

func runInteractive(model string, p params) {
	req := getCompletionRequest(p, model)

	if p.continueSession {
		printSession(req, p.pretty)
	}

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			fmt.Println()
			break
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		req.Messages = append(req.Messages, message{Role: "user", Content: line})

		isTTY := p.pretty
		var renderer *ttyRenderer
		var buf strings.Builder
		if isTTY {
			renderer = &ttyRenderer{}
		}

		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		fullResponse, citations, err := streamCompletion(ctx, req, func(chunk string) error {
			if isTTY {
				buf.WriteString(chunk)
				renderer.render(buf.String())
				return nil
			}
			_, err := fmt.Print(chunk)
			return err
		})
		cancel()
		if !isTTY {
			fmt.Println()
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			continue
		}
		printCitations(citations)

		req.Messages = append(req.Messages, message{Role: "assistant", Content: fullResponse})
		if err := saveCompletion(req); err != nil {
			fmt.Fprintf(os.Stderr, "warn: failed to save session: %v\n", err)
		}
	}
}

func parseArgs() params {
	var p params
	flag.IntVar(&p.maxTokens, "maxTokens", 500, "Maximum number of tokens to generate")
	flag.StringVar(&p.systemMsg, "systemMsg", "", "System message to include with the prompt")
	flag.StringVar(&p.includeFile, "includeFile", "", "File to include with the prompt")
	flag.Float64Var(&p.temperature, "temperature", 0, "Temperature")
	flag.BoolVar(&p.continueSession, "c", false, "Continue last session (ignores other flags)")
	isTTY := term.IsTerminal(int(os.Stdout.Fd()))
	flag.BoolVar(&p.pretty, "pretty", isTTY, "Render markdown (default: true when stdout is a TTY)")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options] message\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()
	msg := strings.TrimSpace(strings.Join(flag.Args(), " "))
	if msg == "" {
		p.interactive = true
		return p
	} else if msg == "-" {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			msg += scanner.Text() + "\n"
		}
	}
	p.msg = msg
	return p
}

func endpoint() string {
	ep := os.Getenv("OPENAI_ENDPOINT")
	if ep == "" {
		return defaultEndpoint
	}
	return strings.TrimRight(ep, "/") + "/chat/completions"
}

func apiKey() string {
	return os.Getenv("OPENAI_API_KEY")
}

func getCompletionRequest(p params, model string) completionRequest {
	if p.continueSession {
		req := loadLastCompletion()
		if req != nil {
			return *req
		}
		fmt.Println("WARN: failed to load previous session, starting a new one")
	}
	return newCompletionRequest(p, model)
}

func loadLastCompletion() *completionRequest {
	var req completionRequest
	session, err := os.ReadFile(sessionFile)
	if err != nil {
		return nil
	}
	if err := json.Unmarshal(session, &req); err != nil {
		return nil
	}
	return &req
}

func saveCompletion(req completionRequest) error {
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	return os.WriteFile(sessionFile, data, 0o644)
}

func newCompletionRequest(p params, model string) completionRequest {
	var msgs []message
	if p.systemMsg != "" {
		msgs = append(msgs, message{Role: "system", Content: p.systemMsg})
	}
	return completionRequest{
		Model:               model,
		MaxCompletionTokens: p.maxTokens,
		Temperature:         float32(p.temperature),
		Stream:              true,
		Messages:            msgs,
	}
}

func streamCompletion(ctx context.Context, req completionRequest, callback func(chunk string) error) (fullResponse string, citations []citation, err error) {
	body, err := json.Marshal(req)
	if err != nil {
		return "", nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint(), bytes.NewReader(body))
	if err != nil {
		return "", nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey())
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return "", nil, fmt.Errorf("request error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return "", nil, fmt.Errorf("API error %d: %s", resp.StatusCode, errBody)
	}

	var chunks []string
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var sc streamChunk
		if err := json.Unmarshal([]byte(data), &sc); err != nil {
			continue
		}
		if len(sc.Citations) > 0 {
			citations = sc.Citations
		}
		if len(sc.Choices) == 0 {
			continue
		}
		chunk := sc.Choices[0].Delta.Content
		if chunk == "" {
			continue
		}
		if err := callback(chunk); err != nil {
			return "", nil, fmt.Errorf("callback error: %w", err)
		}
		chunks = append(chunks, chunk)
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		return "", nil, fmt.Errorf("stream error: %w", err)
	}

	return strings.Join(chunks, ""), citations, nil
}
