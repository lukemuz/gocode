package main

import (
	"fmt"
	"os"
	"strings"
)

// ANSI escape sequences. We deliberately keep this tiny and self-contained
// rather than pull in a colour library — gocode's go.mod is dep-free and
// staying that way is a feature.
const (
	ansiReset    = "\x1b[0m"
	ansiBold     = "\x1b[1m"
	ansiDim      = "\x1b[2m"
	ansiItalic   = "\x1b[3m"
	ansiRed      = "\x1b[31m"
	ansiGreen    = "\x1b[32m"
	ansiYellow   = "\x1b[33m"
	ansiBlue     = "\x1b[34m"
	ansiMagenta  = "\x1b[35m"
	ansiCyan     = "\x1b[36m"
	ansiGrey     = "\x1b[90m"
	ansiBoldCyan = "\x1b[1;36m"
)

// useColor is set during init based on TTY detection and the NO_COLOR /
// TERM=dumb conventions. When false, all paint helpers are no-ops.
var useColor = true

func init() {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		useColor = false
		return
	}
	if !isTTY(os.Stderr) {
		useColor = false
	}
}

// isTTY returns true if f is attached to a character device (a terminal).
// Pure stdlib: relies on os.ModeCharDevice, which the runtime fills in via
// fstat on Unix and GetFileType on Windows.
func isTTY(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func paint(s, code string) string {
	if !useColor || s == "" {
		return s
	}
	return code + s + ansiReset
}

func bold(s string) string     { return paint(s, ansiBold) }
func dim(s string) string      { return paint(s, ansiDim) }
func italic(s string) string   { return paint(s, ansiItalic) }
func red(s string) string      { return paint(s, ansiRed) }
func green(s string) string    { return paint(s, ansiGreen) }
func yellow(s string) string   { return paint(s, ansiYellow) }
func cyan(s string) string     { return paint(s, ansiCyan) }
func grey(s string) string     { return paint(s, ansiGrey) }
func boldCyan(s string) string { return paint(s, ansiBoldCyan) }

// humanBytes formats n as a short human-readable size string.
func humanBytes(n int) string {
	const (
		kib = 1024
		mib = 1024 * kib
	)
	switch {
	case n >= mib:
		return fmt.Sprintf("%.1f MiB", float64(n)/mib)
	case n >= kib:
		return fmt.Sprintf("%.1f KiB", float64(n)/kib)
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// truncate shortens s to at most n runes, appending an ellipsis when cut.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// oneLine collapses any internal newlines/tabs to spaces. Useful when
// echoing user-supplied tool arguments (e.g. multi-line bash commands)
// onto a single status line.
func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	return strings.TrimSpace(s)
}
