package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/fencesandbox/fence/pkg/fence"
)

type toolDef struct {
	Type     string       `json:"type"`
	Function toolFunction `json:"function"`
}

type toolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  toolParameters `json:"parameters"`
}

type toolParameters struct {
	Type       string              `json:"type"`
	Properties map[string]toolProp `json:"properties"`
	Required   []string            `json:"required"`
}

type toolProp struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

type toolCallDelta struct {
	Index    int              `json:"index"`
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function toolFunctionCall `json:"function"`
}

type toolFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// in-progress assembly of a streaming tool call
type pendingToolCall struct {
	id      string
	name    string
	argsBuf strings.Builder
}

type toolCall struct {
	id   string
	name string
	args map[string]any
}

var agentTools = []toolDef{
	{
		Type: "function",
		Function: toolFunction{
			Name:        "read",
			Description: "Read the contents of a local file.",
			Parameters: toolParameters{
				Type: "object",
				Properties: map[string]toolProp{
					"path":   {Type: "string", Description: "Path to the file to read"},
					"offset": {Type: "integer", Description: "Line number to start reading from (1-based, optional)"},
					"limit":  {Type: "integer", Description: "Maximum number of lines to read (optional)"},
				},
				Required: []string{"path"},
			},
		},
	},
	{
		Type: "function",
		Function: toolFunction{
			Name:        "write",
			Description: "Write content to a new file. Fails if the file already exists.",
			Parameters: toolParameters{
				Type: "object",
				Properties: map[string]toolProp{
					"path":    {Type: "string", Description: "Path of the new file to create"},
					"content": {Type: "string", Description: "Content to write into the file"},
				},
				Required: []string{"path", "content"},
			},
		},
	},
	{
		Type: "function",
		Function: toolFunction{
			Name:        "edit",
			Description: "Replace an exact string inside an existing file. Fails if old_string is not found or matches more than once.",
			Parameters: toolParameters{
				Type: "object",
				Properties: map[string]toolProp{
					"path":       {Type: "string", Description: "Path to the file to edit"},
					"old_string": {Type: "string", Description: "Exact string to find and replace"},
					"new_string": {Type: "string", Description: "Replacement string"},
				},
				Required: []string{"path", "old_string", "new_string"},
			},
		},
	},
	{
		Type: "function",
		Function: toolFunction{
			Name:        "bash",
			Description: "Run a shell command and return its combined stdout+stderr output.",
			Parameters: toolParameters{
				Type: "object",
				Properties: map[string]toolProp{
					"command":     {Type: "string", Description: "Shell command to execute"},
					"description": {Type: "string", Description: "One-line human-readable summary of what this command does and why, shown in logs"},
				},
				Required: []string{"command", "description"},
			},
		},
	},
}

type toolMode string

const (
	toolModeOff      toolMode = "off"
	toolModeSafe     toolMode = "safe"
	toolModeUnsafe   toolMode = "unsafe"
	toolModeRoSystem toolMode = "ro-system" // read whole system, write only cwd
)

// bashRunner wraps command execution, optionally via a fence sandbox manager.
type bashRunner struct {
	manager *fence.Manager
	workdir string
}

func newBashRunner(mode toolMode, workdir string) (*bashRunner, error) {
	if mode == toolModeOff {
		return nil, nil
	}
	r := &bashRunner{workdir: workdir}
	if mode == toolModeSafe || mode == toolModeRoSystem {
		if !fence.IsSupported() {
			return nil, fmt.Errorf("fence sandboxing is not supported on this platform; use -tool-mode=unsafe to run without sandboxing")
		}
		var cfg *fence.Config
		if mode == toolModeRoSystem {
			cfg = roSystemFenceConfig(workdir)
		} else {
			cfg = safeFenceConfig(workdir)
		}
		// merge project-level fence.jsonc if present
		if cfgPath, err := fence.ResolveConfigPath(workdir); err == nil && cfgPath != "" {
			if override, err := fence.LoadConfigResolved(cfgPath); err == nil {
				cfg = fence.MergeConfigs(cfg, override)
				fmt.Fprintf(os.Stderr, "[fence] loaded project config: %s\n", cfgPath)
			}
		}
		m := fence.NewManager(cfg, false, false)
		if err := m.Initialize(); err != nil {
			return nil, fmt.Errorf("fence init: %w", err)
		}
		r.manager = m
	}
	return r, nil
}

func safeFenceConfig(workdir string) *fence.Config {
	cfg := fence.DefaultConfig() // network: all blocked by default
	cfg.Filesystem.DefaultDenyRead = true
	cfg.Filesystem.AllowRead = []string{workdir}
	cfg.Filesystem.AllowWrite = []string{workdir}
	// Allow execution of binaries from standard system and package manager paths.
	// AllowExecute grants exec+read on the named path without exposing the parent
	// directory for listing (unlike AllowRead).
	cfg.Filesystem.AllowExecute = []string{
		"/usr/bin",
		"/usr/local/bin",
		"/bin",
		"/sbin",
		"/usr/sbin",
		"/nix/store",          // Nix-managed binaries
		"/run/current-system", // NixOS system profile
		homeDir() + "/.nix-profile",
	}
	// Silence the per-call warning about coreutils shared-binary aliasing.
	// On Nix systems coreutils is a multi-call binary; fence cannot runtime-deny
	// individual aliases (e.g. chroot) without blocking all of them. We've
	// reviewed this and accept the limitation.
	cfg.Command.AcceptSharedBinaryCannotRuntimeDeny = []string{"chroot"}
	return cfg
}

func roSystemFenceConfig(workdir string) *fence.Config {
	cfg := fence.DefaultConfig()
	cfg.Filesystem.DefaultDenyRead = false // read anywhere
	cfg.Filesystem.AllowRead = []string{"/"}
	cfg.Filesystem.AllowWrite = []string{workdir}
	cfg.Filesystem.AllowExecute = []string{
		"/usr/bin",
		"/usr/local/bin",
		"/bin",
		"/sbin",
		"/usr/sbin",
		"/nix/store",
		"/run/current-system",
		homeDir() + "/.nix-profile",
	}
	cfg.Command.AcceptSharedBinaryCannotRuntimeDeny = []string{"chroot"}
	return cfg
}

func homeDir() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	return "/tmp"
}

func (r *bashRunner) run(command string) (string, error) {
	var cmd *exec.Cmd
	if r.manager != nil {
		wrapped, err := r.manager.WrapCommandInDir(command, r.workdir)
		if err != nil {
			return "", fmt.Errorf("fence wrap: %w", err)
		}
		cmd = exec.Command("sh", "-c", wrapped)
	} else {
		cmd = exec.Command("sh", "-c", command)
		cmd.Dir = r.workdir
	}
	out, err := cmd.CombinedOutput()
	result := strings.TrimRight(string(out), "\n")
	if err != nil {
		if result != "" {
			return "", fmt.Errorf("%s\n%w", result, err)
		}
		return "", err
	}
	return result, nil
}

func (r *bashRunner) close() {
	if r.manager != nil {
		r.manager.Cleanup()
	}
}

func executeTool(tc toolCall, bash *bashRunner) (string, error) {
	switch tc.name {
	case "read":
		return toolRead(tc.args)
	case "write":
		return toolWrite(tc.args)
	case "edit":
		return toolEdit(tc.args)
	case "bash":
		if bash == nil {
			return "", fmt.Errorf("bash tool is disabled; use -tool-mode=safe or -tool-mode=unsafe to enable it")
		}
		return toolBash(tc.args, bash)
	default:
		return "", fmt.Errorf("unknown tool: %s", tc.name)
	}
}

func toolRead(args map[string]any) (string, error) {
	path, _ := args["path"].(string)
	if path == "" {
		return "", fmt.Errorf("path required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(data), "\n")

	offset := 0
	if v, ok := args["offset"]; ok {
		switch n := v.(type) {
		case float64:
			offset = int(n) - 1
		case int:
			offset = n - 1
		}
	}
	limit := len(lines)
	if v, ok := args["limit"]; ok {
		switch n := v.(type) {
		case float64:
			limit = int(n)
		case int:
			limit = n
		}
	}

	if offset < 0 {
		offset = 0
	}
	if offset >= len(lines) {
		return "", nil
	}
	end := offset + limit
	if end > len(lines) {
		end = len(lines)
	}
	return strings.Join(lines[offset:end], "\n"), nil
}

func toolWrite(args map[string]any) (string, error) {
	path, _ := args["path"].(string)
	content, _ := args["content"].(string)
	if path == "" {
		return "", fmt.Errorf("path required")
	}
	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("file already exists: %s", path)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", err
	}
	return fmt.Sprintf("wrote %d bytes to %s", len(content), path), nil
}

func toolEdit(args map[string]any) (string, error) {
	path, _ := args["path"].(string)
	oldStr, _ := args["old_string"].(string)
	newStr, _ := args["new_string"].(string)
	if path == "" || oldStr == "" {
		return "", fmt.Errorf("path and old_string required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	content := string(data)
	count := strings.Count(content, oldStr)
	if count == 0 {
		return "", fmt.Errorf("old_string not found in %s", path)
	}
	if count > 1 {
		return "", fmt.Errorf("old_string matches %d times in %s, must be unique", count, path)
	}
	result := strings.Replace(content, oldStr, newStr, 1)
	if err := os.WriteFile(path, []byte(result), 0o644); err != nil {
		return "", err
	}
	return fmt.Sprintf("edited %s", path), nil
}

func toolBash(args map[string]any, r *bashRunner) (string, error) {
	command, _ := args["command"].(string)
	if command == "" {
		return "", fmt.Errorf("command required")
	}
	return r.run(command)
}

func toolCallsFromPending(pending map[int]*pendingToolCall) ([]toolCall, error) {
	calls := make([]toolCall, 0, len(pending))
	for i := 0; i < len(pending); i++ {
		p, ok := pending[i]
		if !ok {
			continue
		}
		var args map[string]any
		if err := json.Unmarshal([]byte(p.argsBuf.String()), &args); err != nil {
			return nil, fmt.Errorf("failed to parse args for %s: %w", p.name, err)
		}
		calls = append(calls, toolCall{id: p.id, name: p.name, args: args})
	}
	return calls, nil
}

func mustMarshal(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}
