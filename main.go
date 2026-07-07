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
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/glamour"
	"github.com/chzyer/readline"
	"golang.org/x/term"
)

const (
	defaultModel     = "gpt-5.4-mini"
	defaultEndpoint  = "https://api.openai.com/v1/chat/completions"
	sessionFile      = "/tmp/llm-cli-last-session.json"
	defaultMaxTokens = 8192
)

type message struct {
	Role       string          `json:"role"`
	Content    string          `json:"content"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
	Name       string          `json:"name,omitempty"`
	ToolCalls  []assistantTool `json:"tool_calls,omitempty"`
}

type assistantTool struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function toolFunctionCall `json:"function"`
}

type completionRequest struct {
	Model               string    `json:"model"`
	Messages            []message `json:"messages"`
	MaxCompletionTokens int       `json:"max_completion_tokens,omitempty"`
	Temperature         float32   `json:"temperature,omitempty"`
	Stream              bool      `json:"stream"`
	Tools               []toolDef `json:"tools,omitempty"`
}

type citation struct {
	RefID int    `json:"ref_id"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

type deltaContent struct {
	Content   string          `json:"content"`
	ToolCalls []toolCallDelta `json:"tool_calls"`
}

type streamChoice struct {
	Delta        deltaContent `json:"delta"`
	FinishReason string       `json:"finish_reason"`
}

type streamChunk struct {
	Choices   []streamChoice `json:"choices"`
	Citations []citation     `json:"citations"`
}

type params struct {
	maxTokens       int
	model           string
	systemMsg       string
	includeFile     string
	temperature     float64
	continueSession bool
	interactive     bool
	pretty          bool
	agent           bool
	toolMode        toolMode
	msg             string
}

func main() {
	p := parseArgs()

	model := p.model
	if model == "" {
		model = os.Getenv("OPENAI_MODEL")
	}
	if model == "" {
		model = defaultModel
	}

	if p.interactive {
		runInteractive(model, p)
		return
	}

	if p.agent {
		req := getCompletionRequest(p, model)
		req.Messages = append(req.Messages, message{Role: "user", Content: p.msg})
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if err := runAgentLoop(ctx, req, p); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
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

	fullResponse, citations, _, _, err := streamCompletion(ctx, req, func(chunk string) error {
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

	if r.prev != "" {
		// Count lines printed previously; SplitAfter adds a trailing empty
		// element for a trailing newline, so subtract 1 to get actual line count.
		lines := strings.SplitAfter(r.prev, "\n")
		moveUp := len(lines) - 1
		if moveUp > 0 {
			fmt.Printf("\033[%dA\033[J", moveUp)
		}
	}
	fmt.Print(rendered)
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

	if p.includeFile != "" {
		contents, err := os.ReadFile(p.includeFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error reading file: %v\n", err)
		} else {
			req.Messages = append(req.Messages, message{Role: "user", Content: string(contents)})
		}
	}

	if p.continueSession {
		printSession(req, p.pretty)
	}

	var bash *bashRunner
	if p.agent {
		workdir, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return
		}
		bash, err = newBashRunner(p.toolMode, workdir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return
		}
		if bash != nil {
			defer bash.close()
		}
	}

	rl, err := readline.New("\033[36m❯\033[0m ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return
	}
	defer rl.Close()

	for {
		line, err := rl.Readline()
		if err != nil {
			if err != readline.ErrInterrupt && err != io.EOF {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
			}
			fmt.Println()
			break
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		line = expandAtFiles(line)
		req.Messages = append(req.Messages, message{Role: "user", Content: line})

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)

		if p.agent {
			if err := runAgentTurn(ctx, &req, bash, p.pretty); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
			}
		} else {
			isTTY := p.pretty
			var renderer *ttyRenderer
			var buf strings.Builder
			if isTTY {
				renderer = &ttyRenderer{}
			}
			fullResponse, citations, _, _, err := streamCompletion(ctx, req, func(chunk string) error {
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
				cancel()
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				continue
			}
			printCitations(citations)
			req.Messages = append(req.Messages, message{Role: "assistant", Content: fullResponse})
		}

		cancel()
		if err := saveCompletion(req); err != nil {
			fmt.Fprintf(os.Stderr, "warn: failed to save session: %v\n", err)
		}
	}
}

var atFileRe = regexp.MustCompile(`@(\S+)`)

// expandAtFiles replaces @filename tokens with the contents of the named file,
// wrapped in a fenced code block labeled with the filename.
// Tokens that don't resolve to readable files are left unchanged.
func expandAtFiles(line string) string {
	return atFileRe.ReplaceAllStringFunc(line, func(match string) string {
		path := match[1:] // strip leading @
		contents, err := os.ReadFile(path)
		if err != nil {
			return match
		}
		fmt.Fprintf(os.Stderr, "[including %s]\n", path)
		return fmt.Sprintf("Contents of %s:\n```\n%s\n```", path, strings.TrimRight(string(contents), "\n"))
	})
}

func parseArgs() params {
	var p params
	flag.IntVar(&p.maxTokens, "maxTokens", defaultMaxTokens, "Maximum number of tokens to generate")
	modelUsage := "Model to use (overrides OPENAI_MODEL env var)"
	if envModel := os.Getenv("OPENAI_MODEL"); envModel != "" {
		modelUsage += " [env: " + envModel + "]"
	}
	flag.StringVar(&p.model, "model", "", modelUsage)
	flag.StringVar(&p.systemMsg, "systemMsg", "", "System message to include with the prompt")
	flag.StringVar(&p.includeFile, "includeFile", "", "File to include with the prompt")
	flag.Float64Var(&p.temperature, "temperature", 0, "Temperature")
	flag.BoolVar(&p.continueSession, "c", false, "Continue last session (ignores other flags)")
	flag.BoolVar(&p.agent, "agent", false, "Enable agentic mode with read/write/edit tools")
	toolModeVal := flag.String("tool-mode", "off", "Bash tool access: off (default), safe (fence-sandboxed, respects fence.jsonc), unsafe (unrestricted)")
	isTTY := term.IsTerminal(int(os.Stdout.Fd()))
	flag.BoolVar(&p.pretty, "pretty", isTTY, "Render markdown (default: true when stdout is a TTY)")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options] message\n\n", os.Args[0])
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, `
Agentic mode (-agent) gives the model read/write/edit tools and loops until done.
Add -tool-mode=safe to also enable the bash tool, sandboxed to the current directory
via fence (no network, no writes outside cwd). Use -tool-mode=unsafe for unrestricted
bash access. Drop a fence.jsonc in your project directory to customise the sandbox
(e.g. allow specific domains or extra readable paths).
`)
	}
	flag.Parse()
	p.toolMode = toolMode(*toolModeVal)
	msg := strings.TrimSpace(strings.Join(flag.Args(), " "))
	if msg == "" {
		p.interactive = true
		return p
	} else if msg == "-" {
		msg = ""
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
	ep = strings.TrimRight(ep, "/")
	if strings.HasSuffix(ep, "/chat/completions") {
		return ep
	}
	return ep + "/chat/completions"
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
	req := completionRequest{
		Model:               model,
		MaxCompletionTokens: p.maxTokens,
		Temperature:         float32(p.temperature),
		Stream:              true,
		Messages:            msgs,
	}
	if p.agent {
		tools := make([]toolDef, 0, len(agentTools))
		for _, t := range agentTools {
			if t.Function.Name == "bash" && p.toolMode == toolModeOff {
				continue
			}
			tools = append(tools, t)
		}
		req.Tools = tools
	}
	return req
}

func streamCompletion(ctx context.Context, req completionRequest, callback func(chunk string) error) (fullResponse string, citations []citation, toolCalls []toolCall, finishReason string, err error) {
	body, err := json.Marshal(req)
	if err != nil {
		return "", nil, nil, "", err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint(), bytes.NewReader(body))
	if err != nil {
		return "", nil, nil, "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey())
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return "", nil, nil, "", fmt.Errorf("request error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return "", nil, nil, "", fmt.Errorf("API error %d: %s", resp.StatusCode, errBody)
	}

	var chunks []string
	pending := map[int]*pendingToolCall{}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
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
		if fr := sc.Choices[0].FinishReason; fr != "" {
			finishReason = fr
		}
		delta := sc.Choices[0].Delta

		// accumulate tool call argument fragments
		for _, tcd := range delta.ToolCalls {
			p, ok := pending[tcd.Index]
			if !ok {
				p = &pendingToolCall{id: tcd.ID, name: tcd.Function.Name}
				pending[tcd.Index] = p
			}
			if tcd.ID != "" {
				p.id = tcd.ID
			}
			if tcd.Function.Name != "" {
				p.name = tcd.Function.Name
			}
			p.argsBuf.WriteString(tcd.Function.Arguments)
		}

		chunk := delta.Content
		if chunk == "" {
			continue
		}
		if err := callback(chunk); err != nil {
			return "", nil, nil, "", fmt.Errorf("callback error: %w", err)
		}
		chunks = append(chunks, chunk)
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		return "", nil, nil, "", fmt.Errorf("stream error: %w", err)
	}

	if len(pending) > 0 {
		toolCalls, err = toolCallsFromPending(pending)
		if err != nil {
			return "", nil, nil, "", err
		}
	}

	return strings.Join(chunks, ""), citations, toolCalls, finishReason, nil
}

func runAgentLoop(ctx context.Context, req completionRequest, p params) error {
	workdir, err := os.Getwd()
	if err != nil {
		return err
	}
	bash, err := newBashRunner(p.toolMode, workdir)
	if err != nil {
		return err
	}
	if bash != nil {
		defer bash.close()
	}

	if err := runAgentTurn(ctx, &req, bash, p.pretty); err != nil {
		return err
	}
	if err := saveCompletion(req); err != nil {
		fmt.Fprintf(os.Stderr, "warn: failed to save session: %v\n", err)
	}
	return nil
}

// runAgentTurn runs one user turn: streams a response, executes any tool calls,
// feeds results back, and repeats until the model stops calling tools.
// req is updated in place with all new messages appended.
func runAgentTurn(ctx context.Context, req *completionRequest, bash *bashRunner, pretty bool) error {
	for {
		var renderer *ttyRenderer
		var buf strings.Builder
		if pretty {
			renderer = &ttyRenderer{}
		}

		fullResponse, citations, toolCalls, finishReason, err := streamCompletion(ctx, *req, func(chunk string) error {
			if pretty {
				buf.WriteString(chunk)
				renderer.render(buf.String())
				return nil
			}
			_, err := fmt.Print(chunk)
			return err
		})
		if !pretty {
			fmt.Println()
		}
		if err != nil {
			return err
		}
		if finishReason == "length" {
			return fmt.Errorf("response truncated (hit maxTokens limit); raise -maxTokens and retry")
		}
		printCitations(citations)

		if fullResponse != "" || len(toolCalls) > 0 {
			assistantMsg := message{Role: "assistant", Content: fullResponse}
			for _, tc := range toolCalls {
				assistantMsg.ToolCalls = append(assistantMsg.ToolCalls, assistantTool{
					ID:   tc.id,
					Type: "function",
					Function: toolFunctionCall{
						Name:      tc.name,
						Arguments: mustMarshal(tc.args),
					},
				})
			}
			req.Messages = append(req.Messages, assistantMsg)
		}

		if len(toolCalls) == 0 {
			break
		}

		for _, tc := range toolCalls {
			fmt.Printf("tool call %s(%s)\n", tc.name, toolCallSummary(tc))
			result, toolErr := executeTool(tc, bash)
			var content string
			if toolErr != nil {
				content = "error: " + toolErr.Error()
				fmt.Fprintf(os.Stderr, "[tool] %s error: %v\n", tc.name, toolErr)
			} else {
				content = result
			}
			req.Messages = append(req.Messages, message{
				Role:       "tool",
				ToolCallID: tc.id,
				Name:       tc.name,
				Content:    content,
			})
		}
	}
	return nil
}
