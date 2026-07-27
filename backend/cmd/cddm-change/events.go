package main

import (
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
)

const maxEventText = 360

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(authorization\s*:\s*)([^\s]+)`),
	regexp.MustCompile(`(?i)((?:password|passwd|secret|token|cookie)\s*[=:]\s*)([^\s,;]+)`),
	regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9_]{20,}\b`),
	regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{16,}\b`),
}

func redact(s string) string {
	s = strings.ReplaceAll(s, "\x00", "")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	for _, re := range secretPatterns {
		if re.NumSubexp() >= 2 {
			s = re.ReplaceAllString(s, `${1}[REDACTED]`)
		} else {
			s = re.ReplaceAllString(s, `[REDACTED]`)
		}
	}
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > maxEventText {
		s = s[:maxEventText-1] + "…"
	}
	return s
}

type renderedEvent struct {
	Kind    string
	Text    string
	Success *bool
}

func parseRenderedEvent(line string) renderedEvent {
	var event map[string]any
	if json.Unmarshal([]byte(line), &event) != nil {
		return renderedEvent{Kind: "EVENT", Text: "malformed JSON (raw line preserved)"}
	}
	return renderEvent(event)
}

func renderEvent(event map[string]any) renderedEvent {
	eventType := stringValue(event["type"])
	if eventType == "" {
		eventType = stringValue(event["event"])
	}
	if eventType == "" {
		eventType = "unknown"
	}
	if eventType == "thread.started" {
		thread := stringValue(event["thread_id"])
		if thread == "" {
			if obj, ok := event["thread"].(map[string]any); ok {
				thread = stringValue(obj["id"])
			}
		}
		if thread == "" {
			thread = "started"
		}
		return renderedEvent{Kind: "THREAD", Text: redact(thread)}
	}
	if strings.HasPrefix(eventType, "turn.") {
		state := strings.ToUpper(strings.TrimPrefix(eventType, "turn."))
		text := state
		if detail := firstString(event, "message", "error", "detail"); detail != "" {
			text += " · " + redact(detail)
		}
		r := renderedEvent{Kind: "TURN", Text: text}
		if state == "COMPLETED" {
			v := true
			r.Success = &v
		} else if state == "FAILED" || state == "CANCELLED" {
			v := false
			r.Success = &v
		}
		return r
	}
	if eventType == "error" || strings.HasSuffix(eventType, ".error") {
		v := false
		return renderedEvent{Kind: "ERROR", Text: redact(defaultString(firstString(event, "message", "error", "detail"), "error")), Success: &v}
	}

	item := event
	if obj, ok := event["item"].(map[string]any); ok {
		item = obj
	}
	itemType := stringValue(item["type"])
	if itemType == "" {
		itemType = stringValue(event["item_type"])
	}
	phase := eventType
	if idx := strings.LastIndex(eventType, "."); idx >= 0 {
		phase = eventType[idx+1:]
	}

	switch itemType {
	case "agent_message", "message":
		return renderedEvent{Kind: "MESSAGE", Text: defaultString(itemText(item), phase)}
	case "reasoning", "analysis":
		return renderedEvent{Kind: "THINK", Text: defaultString(itemText(item), phase)}
	case "command_execution", "command", "shell_command":
		command := commandValue(item)
		if phase == "started" || phase == "created" {
			return renderedEvent{Kind: "RUN", Text: defaultString(command, "command")}
		}
		result := phase
		if n, ok := numberInt(item["exit_code"]); ok {
			result = fmt.Sprintf("exit=%d", n)
		} else if status := stringValue(item["status"]); status != "" {
			result = status
		}
		text := result
		if command != "" {
			text = command + " · " + result
		}
		r := renderedEvent{Kind: "DONE", Text: redact(text)}
		if n, ok := numberInt(item["exit_code"]); ok {
			ok := n == 0
			r.Success = &ok
		}
		return r
	case "file_change", "file_changes", "patch", "edit":
		return renderedEvent{Kind: "EDIT", Text: fileChangeSummary(item)}
	case "mcp_tool_call", "tool_call":
		name := firstString(item, "tool", "name", "server")
		return renderedEvent{Kind: "TOOL", Text: redact(defaultString(name, "tool") + " · " + phase)}
	case "web_search", "search":
		return renderedEvent{Kind: "SEARCH", Text: redact(defaultString(stringValue(item["query"]), "search") + " · " + phase)}
	case "todo_list", "plan":
		return renderedEvent{Kind: "PLAN", Text: phase}
	}
	if strings.HasPrefix(eventType, "item.") {
		return renderedEvent{Kind: "ITEM", Text: redact(defaultString(itemType, "unknown") + " · " + phase)}
	}
	return renderedEvent{Kind: "EVENT", Text: redact(eventType)}
}

func (u *UI) printEvent(w io.Writer, elapsed time.Duration, e renderedEvent) {
	stamp := u.style(w, ansiDim, fmt.Sprintf("[%s]", humanDuration(elapsed)))
	kind := fmt.Sprintf("%-7s", e.Kind)
	color := ansiCyan
	symbol := "•"
	switch e.Kind {
	case "THINK":
		color, symbol = ansiMagenta, "◌"
	case "MESSAGE":
		color, symbol = ansiCyan, "◆"
	case "RUN":
		color, symbol = ansiYellow, "›"
	case "DONE":
		color, symbol = ansiGreen, "✓"
		if e.Success != nil && !*e.Success {
			color, symbol = ansiRed, "✗"
		}
	case "EDIT":
		color, symbol = ansiBlue, "Δ"
	case "TOOL", "SEARCH":
		color, symbol = ansiCyan, "◇"
	case "WARN":
		color, symbol = ansiYellow, "!"
	case "ERROR":
		color, symbol = ansiRed, "✗"
	case "TURN":
		color, symbol = ansiBlue, "●"
		if e.Success != nil {
			if *e.Success {
				color, symbol = ansiGreen, "✓"
			} else {
				color, symbol = ansiRed, "✗"
			}
		}
	case "THREAD":
		color, symbol = ansiCyan, "◆"
	case "PLAN":
		color, symbol = ansiBlue, "≡"
	}
	label := u.style(w, ansiBold+color, symbol+" "+kind)
	fmt.Fprintf(w, "%s %s %s\n", stamp, label, e.Text)
}

func parseUsage(event map[string]any, total *Usage) {
	candidates := []map[string]any{event}
	for _, key := range []string{"usage", "token_usage", "tokens"} {
		if obj, ok := event[key].(map[string]any); ok {
			candidates = append(candidates, obj)
		}
	}
	for _, c := range candidates {
		setUsageMax(&total.Input, c, "input_tokens", "input", "prompt_tokens")
		setUsageMax(&total.Cached, c, "cached_input_tokens", "cached_tokens", "cache_read_tokens")
		setUsageMax(&total.Output, c, "output_tokens", "output", "completion_tokens")
	}
}

func setUsageMax(dst **int, obj map[string]any, names ...string) {
	for _, name := range names {
		if n, ok := numberInt(obj[name]); ok && n >= 0 {
			if *dst == nil || n > **dst {
				*dst = ptr(n)
			}
			return
		}
	}
}

func itemText(item map[string]any) string {
	for _, key := range []string{"text", "message", "summary", "reasoning", "content"} {
		if s := stringValue(item[key]); strings.TrimSpace(s) != "" {
			return redact(s)
		}
	}
	return ""
}

func commandValue(item map[string]any) string {
	for _, key := range []string{"command", "cmd"} {
		if s := stringValue(item[key]); s != "" {
			return redact(s)
		}
		if list, ok := item[key].([]any); ok {
			parts := make([]string, 0, len(list))
			for _, value := range list {
				parts = append(parts, fmt.Sprint(value))
			}
			return redact(strings.Join(parts, " "))
		}
	}
	return ""
}

func fileChangeSummary(item map[string]any) string {
	var paths []string
	if list, ok := item["changes"].([]any); ok {
		for _, value := range list {
			obj, ok := value.(map[string]any)
			if !ok {
				continue
			}
			path := firstString(obj, "path", "file")
			kind := firstString(obj, "kind", "type")
			if path != "" {
				paths = append(paths, defaultString(kind, "edit")+" "+path)
			}
			if len(paths) == 8 {
				break
			}
		}
	}
	if len(paths) == 0 {
		if path := firstString(item, "path", "file"); path != "" {
			paths = append(paths, path)
		}
	}
	if len(paths) == 0 {
		return "file changes"
	}
	return redact(strings.Join(paths, ", "))
}

func stringValue(v any) string {
	s, _ := v.(string)
	return s
}

func firstString(obj map[string]any, keys ...string) string {
	for _, key := range keys {
		if s := stringValue(obj[key]); s != "" {
			return s
		}
	}
	return ""
}

func defaultString(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}
