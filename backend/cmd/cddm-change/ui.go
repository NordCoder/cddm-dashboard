package main

import (
	"fmt"
	"io"
	"os"
	"strings"
)

type ColorMode string

const (
	ColorAuto   ColorMode = "auto"
	ColorAlways ColorMode = "always"
	ColorNever  ColorMode = "never"
)

type UI struct {
	mode ColorMode
}

const (
	ansiReset   = "\x1b[0m"
	ansiBold    = "\x1b[1m"
	ansiDim     = "\x1b[2m"
	ansiRed     = "\x1b[31m"
	ansiGreen   = "\x1b[32m"
	ansiYellow  = "\x1b[33m"
	ansiBlue    = "\x1b[34m"
	ansiMagenta = "\x1b[35m"
	ansiCyan    = "\x1b[36m"
	ansiGray    = "\x1b[90m"
)

func parseColor(v string) (ColorMode, error) {
	switch ColorMode(strings.ToLower(strings.TrimSpace(v))) {
	case ColorAuto:
		return ColorAuto, nil
	case ColorAlways:
		return ColorAlways, nil
	case ColorNever:
		return ColorNever, nil
	default:
		return ColorAuto, fmt.Errorf("invalid color mode %q; expected auto, always, or never", v)
	}
}

func newUI(mode ColorMode) *UI { return &UI{mode: mode} }

func (u *UI) enabled(w io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" || u.mode == ColorNever {
		return false
	}
	if u.mode == ColorAlways {
		return true
	}
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func (u *UI) style(w io.Writer, code, text string) string {
	if !u.enabled(w) || code == "" {
		return text
	}
	return code + text + ansiReset
}

func (u *UI) header(w io.Writer, issue int, subtitle string) {
	brand := u.style(w, ansiBold+ansiCyan, "CDDM")
	title := u.style(w, ansiBold, fmt.Sprintf("Change #%d", issue))
	fmt.Fprintf(w, "%s  %s", brand, title)
	if subtitle != "" {
		fmt.Fprintf(w, "  %s", u.style(w, ansiDim, subtitle))
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, u.style(w, ansiGray, strings.Repeat("─", 72)))
}

func (u *UI) section(w io.Writer, title string) {
	fmt.Fprintf(w, "\n%s\n", u.style(w, ansiBold+ansiBlue, title))
}

func (u *UI) statusColor(status string) string {
	s := strings.ToUpper(status)
	switch {
	case strings.Contains(s, "FAILED"), strings.Contains(s, "ERROR"), strings.Contains(s, "BLOCK"), strings.Contains(s, "MISMATCH"), strings.Contains(s, "INCONCLUSIVE"):
		return ansiRed
	case strings.Contains(s, "CANDIDATE"), s == "NO_OP", s == "COMPLETED":
		return ansiGreen
	case strings.Contains(s, "RUN"), strings.Contains(s, "CONTINUE"), strings.Contains(s, "PENDING"), strings.Contains(s, "RECONCIL"):
		return ansiYellow
	default:
		return ansiCyan
	}
}

func (u *UI) badge(w io.Writer, status string) string {
	if status == "" {
		status = "UNKNOWN"
	}
	return u.style(w, ansiBold+u.statusColor(status), "["+status+"]")
}

func (u *UI) label(w io.Writer, label string) string {
	return u.style(w, ansiDim, fmt.Sprintf("%-12s", label))
}

func (u *UI) errorf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(os.Stderr, "%s %s\n", u.style(os.Stderr, ansiBold+ansiRed, "✗ ERROR"), msg)
}

func (u *UI) warnf(w io.Writer, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(w, "%s %s\n", u.style(w, ansiBold+ansiYellow, "! WARN"), msg)
}

func (u *UI) successf(w io.Writer, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(w, "%s %s\n", u.style(w, ansiBold+ansiGreen, "✓"), msg)
}
