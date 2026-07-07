package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"golang.org/x/term"
)

type toolLogMode string

const (
	toolLogOff     toolLogMode = "off"
	toolLogCompact toolLogMode = "compact"
	toolLogNormal  toolLogMode = "normal"
	toolLogVerbose toolLogMode = "verbose"
	toolLogJSONL   toolLogMode = "jsonl"
)

func validToolLogMode(m toolLogMode) bool {
	switch m {
	case toolLogOff, toolLogCompact, toolLogNormal, toolLogVerbose, toolLogJSONL:
		return true
	default:
		return false
	}
}

type toolLogger struct {
	mode  toolLogMode
	next  int
	color bool
}

type toolLogResult struct {
	Result   string
	Err      error
	Duration time.Duration
}

func newToolLogger(mode toolLogMode) *toolLogger {
	if mode == "" {
		mode = toolLogNormal
	}
	color := mode != toolLogJSONL && term.IsTerminal(int(os.Stdout.Fd())) && os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"
	return &toolLogger{mode: mode, color: color}
}

func (l *toolLogger) nextID() int {
	l.next++
	return l.next
}

func (l *toolLogger) logCall(id int, tc toolCall) {
	if l == nil || l.mode == toolLogOff {
		return
	}
	e := toolCallEvent(id, tc)
	if l.mode == toolLogJSONL {
		printJSONL(e)
		return
	}
	if l.mode == toolLogCompact {
		fmt.Printf("%s %s %s\n", colorize(l.color, ansiDim, fmt.Sprintf("tool #%d", id)), colorize(l.color, ansiTool, tc.name), compactToolTarget(tc))
		return
	}
	fmt.Printf("\n%s\n", colorize(l.color, ansiTool, fmt.Sprintf("╭─ tool #%d: %s", id, tc.name)))
	fmt.Printf("%s\n", colorize(l.color, ansiDim, "│  started "+time.Now().Format("15:04:05")))
	for _, line := range callDetailLines(tc, l.mode == toolLogVerbose, l.color) {
		fmt.Println(line)
	}
}

func (l *toolLogger) logResult(id int, tc toolCall, r toolLogResult) {
	if l == nil || l.mode == toolLogOff {
		return
	}
	e := toolResultEvent(id, tc, r)
	if l.mode == toolLogJSONL {
		printJSONL(e)
		return
	}
	status := "✓"
	statusColor := ansiOK
	if r.Err != nil {
		status = "✗"
		statusColor = ansiErr
	}
	if l.mode == toolLogCompact {
		fmt.Printf("%s %s %s %s\n", colorize(l.color, ansiDim, fmt.Sprintf("tool #%d", id)), colorize(l.color, ansiTool, tc.name), colorize(l.color, statusColor, status), r.Duration.Round(time.Millisecond))
		return
	}
	statusText := humanStatus(r.Err)
	if tc.name == "bash" {
		statusText = fmt.Sprintf("%s (exit %d)", statusText, exitCode(r.Err))
	}
	fmt.Printf("%s %s  %s\n", colorize(l.color, ansiDim, "│"), keyLine(l.color, "status", colorize(l.color, statusColor, statusText)), keyLine(l.color, "duration", r.Duration.Round(time.Millisecond).String()))
	for _, line := range resultDetailLines(tc, r, l.mode == toolLogVerbose, l.color) {
		fmt.Println(line)
	}
	fmt.Printf("%s\n", colorize(l.color, ansiTool, "╰─"))
}

func toolCallEvent(id int, tc toolCall) map[string]any {
	e := map[string]any{
		"type":       "tool_call",
		"id":         fmt.Sprintf("tool-%03d", id),
		"tool":       tc.name,
		"started_at": time.Now().Format(time.RFC3339Nano),
	}
	if cwd, err := os.Getwd(); err == nil {
		e["cwd"] = cwd
	}
	for k, v := range summarizedArgs(tc) {
		e[k] = v
	}
	return e
}

func toolResultEvent(id int, tc toolCall, r toolLogResult) map[string]any {
	e := map[string]any{
		"type":        "tool_result",
		"id":          fmt.Sprintf("tool-%03d", id),
		"tool":        tc.name,
		"duration_ms": r.Duration.Milliseconds(),
		"status":      humanStatus(r.Err),
	}
	if tc.name == "bash" {
		e["exit_code"] = exitCode(r.Err)
	}
	if r.Err != nil {
		e["error"] = redact(r.Err.Error())
	} else {
		e["result_bytes"] = len(r.Result)
		if r.Result != "" {
			e["result_preview"] = preview(redact(r.Result), 600)
		}
	}
	for k, v := range resultMetadata(tc, r) {
		e[k] = v
	}
	return e
}

func callDetailLines(tc toolCall, verbose, color bool) []string {
	var lines []string
	if cwd, err := os.Getwd(); err == nil {
		lines = append(lines, logLine(color, keyLine(color, "cwd", cwd)))
	}
	switch tc.name {
	case "bash":
		cmd, _ := tc.args["command"].(string)
		cmd = redact(cmd)
		if desc, _ := tc.args["description"].(string); desc != "" {
			lines = append(lines, logLine(color, keyLine(color, "description", desc)))
		}
		lines = append(lines, logLine(color, keyLine(color, "command", fmt.Sprintf("%d bytes, %d lines", len(cmd), lineCount(cmd)))))
		snippet, note := bashCommandPreview(cmd, verbose)
		lines = append(lines, sectionLine(color, "bash"))
		lines = append(lines, codeBlock(snippet, color)...)
		if note != "" {
			lines = append(lines, logLine(color, colorize(color, ansiDim, note)))
		}
	case "read":
		lines = append(lines, logLine(color, keyLine(color, "path", strArg(tc, "path"))))
		if v, ok := tc.args["offset"]; ok {
			lines = append(lines, logLine(color, keyLine(color, "offset", fmt.Sprintf("%v", v))))
		}
		if v, ok := tc.args["limit"]; ok {
			lines = append(lines, logLine(color, keyLine(color, "limit", fmt.Sprintf("%v", v))))
		}
	case "write":
		content, _ := tc.args["content"].(string)
		redacted := redact(content)
		lines = append(lines, logLine(color, keyLine(color, "path", strArg(tc, "path"))))
		lines = append(lines, logLine(color, keyLine(color, "content", fmt.Sprintf("%d bytes, %d lines, sha256 %s", len(content), lineCount(content), shortHash(sha256Hex(content))))))
		limit := 1200
		if verbose {
			limit = 6000
		}
		lines = append(lines, sectionLine(color, "content preview"))
		lines = append(lines, codeBlock(preview(redacted, limit), color)...)
	case "edit":
		oldStr, _ := tc.args["old_string"].(string)
		newStr, _ := tc.args["new_string"].(string)
		path := strArg(tc, "path")
		lines = append(lines, logLine(color, keyLine(color, "path", path)))
		lines = append(lines, logLine(color, keyLine(color, "replace", fmt.Sprintf("%d bytes → %d bytes", len(oldStr), len(newStr)))))
		lines = append(lines, sectionLine(color, "diff"))
		lines = append(lines, unifiedDiff(path, redact(oldStr), redact(newStr), verbose, color)...)
	}
	return lines
}

func resultDetailLines(tc toolCall, r toolLogResult, verbose, color bool) []string {
	var lines []string
	if r.Err != nil {
		lines = append(lines, sectionLine(color, "error"))
		lines = append(lines, codeBlock(preview(redact(r.Err.Error()), 2000), color)...)
		return lines
	}

	switch tc.name {
	case "bash":
		if r.Result == "" {
			lines = append(lines, logLine(color, colorize(color, ansiDim, "output: <empty>")))
			break
		}
		limit := 2000
		if verbose {
			limit = 8000
		}
		lines = append(lines, logLine(color, keyLine(color, "output", fmt.Sprintf("%d bytes", len(r.Result)))))
		lines = append(lines, codeBlock(preview(redact(r.Result), limit), color)...)
	case "read":
		if r.Result == "" {
			lines = append(lines, logLine(color, colorize(color, ansiDim, "content: <empty>")))
			break
		}
		limit := 1600
		if verbose {
			limit = 8000
		}
		lines = append(lines, logLine(color, keyLine(color, "content", fmt.Sprintf("%d bytes", len(r.Result)))))
		lines = append(lines, codeBlock(preview(redact(r.Result), limit), color)...)
	case "write", "edit":
		if r.Result != "" {
			lines = append(lines, logLine(color, keyLine(color, "result", r.Result)))
		}
		path := strArg(tc, "path")
		if data, err := os.ReadFile(path); err == nil {
			content := string(data)
			lines = append(lines, logLine(color, keyLine(color, "file", fmt.Sprintf("%d bytes, %d lines, sha256 %s", len(data), lineCount(content), shortHash(sha256Bytes(data))))))
			if verbose {
				lines = append(lines, sectionLine(color, "file preview"))
				lines = append(lines, codeBlock(preview(redact(content), 8000), color)...)
			}
		}
	default:
		if r.Result != "" {
			lines = append(lines, logLine(color, keyLine(color, "result", fmt.Sprintf("%d bytes", len(r.Result)))))
			lines = append(lines, codeBlock(preview(redact(r.Result), 2000), color)...)
		}
	}
	return lines
}

func summarizedArgs(tc toolCall) map[string]any {
	m := map[string]any{}
	switch tc.name {
	case "bash":
		cmd, _ := tc.args["command"].(string)
		m["command"] = redact(cmd)
		m["command_bytes"] = len(cmd)
	case "read", "write", "edit":
		m["path"] = strArg(tc, "path")
	}
	return m
}

func resultMetadata(tc toolCall, r toolLogResult) map[string]any {
	m := map[string]any{}
	if r.Err != nil {
		return m
	}
	if tc.name == "write" || tc.name == "edit" {
		path := strArg(tc, "path")
		if data, err := os.ReadFile(path); err == nil {
			m["file_bytes"] = len(data)
			m["file_lines"] = lineCount(string(data))
			m["file_sha256"] = sha256Bytes(data)
			m["file_preview"] = preview(redact(string(data)), 600)
		}
	}
	return m
}

func compactToolTarget(tc toolCall) string {
	switch tc.name {
	case "bash":
		if desc, _ := tc.args["description"].(string); desc != "" {
			return desc
		}
		cmd, _ := tc.args["command"].(string)
		return fmt.Sprintf("%q", previewOneLine(redact(firstExecutableLine(cmd)), 80))
	case "read", "write", "edit":
		return fmt.Sprintf("%q", strArg(tc, "path"))
	default:
		return ""
	}
}

// firstExecutableLine returns the first non-blank, non-comment line of a shell
// script, falling back to the whole command if none is found.
func firstExecutableLine(cmd string) string {
	for _, line := range strings.Split(cmd, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			return trimmed
		}
	}
	return cmd
}

func humanStatus(err error) string {
	if err != nil {
		return "error"
	}
	return "success"
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

func strArg(tc toolCall, name string) string {
	s, _ := tc.args[name].(string)
	return s
}

func lineCount(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

func sha256Hex(s string) string { return sha256Bytes([]byte(s)) }

func sha256Bytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func shortHash(h string) string {
	if len(h) <= 12 {
		return h
	}
	return h[:12]
}

func preview(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max] + fmt.Sprintf("\n... <truncated, %d bytes total>", len(s))
}

func previewOneLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max] + fmt.Sprintf("... <%d bytes>", len(s))
}

func bashCommandPreview(cmd string, verbose bool) (string, string) {
	maxBytes, maxLines := 1400, 80
	if verbose {
		maxBytes, maxLines = 8000, 400
	}
	parts := strings.Split(strings.TrimRight(cmd, "\n"), "\n")
	if len(parts) == 1 && len(cmd) <= maxBytes {
		return cmd, ""
	}
	start := 0
	if len(cmd) > maxBytes || len(parts) > maxLines {
		for i, line := range parts {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}
			// If a long comment/header would hide the first real command, start there.
			if i > 0 && bytesBeforeLine(parts, i) > maxBytes/3 {
				start = i
			}
			break
		}
	}

	var b strings.Builder
	bytesWritten, linesWritten := 0, 0
	for i := start; i < len(parts); i++ {
		line := parts[i]
		lineBytes := len(line)
		if linesWritten >= maxLines || (bytesWritten > 0 && bytesWritten+lineBytes+1 > maxBytes) {
			break
		}
		if linesWritten > 0 {
			b.WriteByte('\n')
			bytesWritten++
		}
		b.WriteString(line)
		bytesWritten += lineBytes
		linesWritten++
	}
	shown := b.String()
	if shown == "" {
		shown = preview(cmd, maxBytes)
	}

	var notes []string
	if start > 0 {
		notes = append(notes, fmt.Sprintf("omitted %d leading comment/blank line(s) so the first executable line is visible", start))
	}
	if start+linesWritten < len(parts) || len(shown) < len(strings.TrimRight(cmd, "\n")) {
		notes = append(notes, fmt.Sprintf("preview truncated; full command is %d bytes / %d lines (use -tool-log=verbose for more)", len(cmd), lineCount(cmd)))
	}
	return shown, strings.Join(notes, "; ")
}

func bytesBeforeLine(lines []string, idx int) int {
	n := 0
	for i := 0; i < idx && i < len(lines); i++ {
		n += len(lines[i]) + 1
	}
	return n
}

func splitDisplayLines(s string) []string {
	if s == "" {
		return []string{""}
	}
	return strings.Split(strings.TrimRight(s, "\n"), "\n")
}

type diffOp struct {
	kind byte
	line string
}

func unifiedDiff(path, oldStr, newStr string, verbose, color bool) []string {
	oldLines := splitDisplayLines(oldStr)
	newLines := splitDisplayLines(newStr)
	ops := diffOps(oldLines, newLines)
	max := 140
	if verbose {
		max = 500
	}
	out := []string{
		logLine(color, colorize(color, ansiDel, "--- ")+path+" (old_string)"),
		logLine(color, colorize(color, ansiAdd, "+++ ")+path+" (new_string)"),
	}

	const ctx = 3
	hunks := buildHunks(ops, ctx)
	total := 0
	for _, hunk := range hunks {
		out = append(out, logLine(color, colorize(color, ansiHunk, hunk.header)))
		for _, op := range hunk.ops {
			if total >= max {
				out = append(out, logLine(color, colorize(color, ansiDim, fmt.Sprintf("... <diff truncated, %d lines total>", len(ops)))))
				return out
			}
			total++
			prefix := string(op.kind)
			line := prefix + op.line
			switch op.kind {
			case '+':
				line = colorize(color, ansiAdd, line)
			case '-':
				line = colorize(color, ansiDel, line)
			default:
				line = colorize(color, ansiDim, line)
			}
			out = append(out, logLine(color, line))
		}
	}
	return out
}

type diffHunk struct {
	header string
	ops    []diffOp
}

func buildHunks(ops []diffOp, ctx int) []diffHunk {
	// Mark which ops are "interesting" (changed).
	interesting := make([]bool, len(ops))
	for i, op := range ops {
		if op.kind != ' ' {
			for j := max(0, i-ctx); j <= min(len(ops)-1, i+ctx); j++ {
				interesting[j] = true
			}
		}
	}

	var hunks []diffHunk
	i := 0
	// Track line numbers in old and new file.
	oldLine, newLine := 1, 1
	for i < len(ops) {
		if !interesting[i] {
			if ops[i].kind != '+' {
				oldLine++
			}
			if ops[i].kind != '-' {
				newLine++
			}
			i++
			continue
		}
		// Collect this hunk.
		start := i
		startOld, startNew := oldLine, newLine
		for i < len(ops) && interesting[i] {
			if ops[i].kind != '+' {
				oldLine++
			}
			if ops[i].kind != '-' {
				newLine++
			}
			i++
		}
		header := fmt.Sprintf("@@ -%d,%d +%d,%d @@", startOld, oldLine-startOld, startNew, newLine-startNew)
		hunks = append(hunks, diffHunk{header: header, ops: ops[start:i]})
	}
	if len(hunks) == 0 {
		hunks = append(hunks, diffHunk{header: "@@ replacement @@", ops: ops})
	}
	return hunks
}

func diffOps(a, b []string) []diffOp {
	if len(a)*len(b) > 40000 {
		ops := make([]diffOp, 0, len(a)+len(b))
		for _, line := range a {
			ops = append(ops, diffOp{kind: '-', line: line})
		}
		for _, line := range b {
			ops = append(ops, diffOp{kind: '+', line: line})
		}
		return ops
	}
	dp := make([][]int, len(a)+1)
	for i := range dp {
		dp[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	var ops []diffOp
	for i, j := 0, 0; i < len(a) || j < len(b); {
		switch {
		case i < len(a) && j < len(b) && a[i] == b[j]:
			ops = append(ops, diffOp{kind: ' ', line: a[i]})
			i++
			j++
		case j < len(b) && (i == len(a) || dp[i][j+1] >= dp[i+1][j]):
			ops = append(ops, diffOp{kind: '+', line: b[j]})
			j++
		case i < len(a):
			ops = append(ops, diffOp{kind: '-', line: a[i]})
			i++
		}
	}
	return ops
}

const (
	ansiReset = "\033[0m"
	ansiBold  = "\033[1m"
	ansiDim   = "\033[2m"
	ansiTool  = "\033[1;36m"
	ansiKey   = "\033[1;34m"
	ansiOK    = "\033[1;32m"
	ansiErr   = "\033[1;31m"
	ansiAdd   = "\033[32m"
	ansiDel   = "\033[31m"
	ansiHunk  = "\033[36m"
)

func colorize(enabled bool, code, s string) string {
	if !enabled || s == "" {
		return s
	}
	return code + s + ansiReset
}

func logLine(color bool, s string) string {
	return colorize(color, ansiDim, "│  ") + s
}

func keyLine(color bool, key, value string) string {
	return colorize(color, ansiKey, key+":") + " " + value
}

func sectionLine(color bool, title string) string {
	return logLine(color, colorize(color, ansiBold, title+":"))
}

func codeBlock(s string, color bool) []string {
	if s == "" {
		return []string{logLine(color, colorize(color, ansiDim, "  <empty>"))}
	}
	parts := strings.Split(strings.TrimRight(s, "\n"), "\n")
	width := len(fmt.Sprintf("%d", len(parts)))
	out := make([]string, 0, len(parts))
	for i, p := range parts {
		gutter := colorize(color, ansiDim, fmt.Sprintf("%*d │ ", width, i+1))
		out = append(out, logLine(color, gutter+p))
	}
	return out
}

func printJSONL(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		fmt.Printf("{\"type\":\"tool_log_error\",\"error\":%q}\n", err.Error())
		return
	}
	fmt.Println(string(b))
}

var redactionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(authorization:\s*bearer\s+)[^\s]+`),
	regexp.MustCompile(`(?i)((?:api[_-]?key|token|password|secret)\s*[=:]\s*)[^\s'\"]+`),
	regexp.MustCompile(`(?i)(OPENAI_API_KEY\s*=\s*)[^\s]+`),
	regexp.MustCompile(`sk-[A-Za-z0-9_-]{16,}`),
}

func redact(s string) string {
	out := s
	for _, re := range redactionPatterns {
		out = re.ReplaceAllString(out, `${1}REDACTED`)
	}
	return out
}
