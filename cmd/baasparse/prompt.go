package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// stdin is a shared buffered reader so prompts and secret reads share one buffer.
var stdin = bufio.NewReader(os.Stdin)

// readLine reads one line, reporting EOF so required-value loops can fail fast
// (rather than spin) when stdin is closed — e.g. a non-interactive/CI run that
// forgot to supply a flag.
func readLine() (string, bool) {
	line, err := stdin.ReadString('\n')
	return strings.TrimSpace(line), errors.Is(err, io.EOF) && line == ""
}

// prompt asks for a value on stderr, showing an optional default. An empty reply
// (or closed stdin) returns def.
func prompt(label, def string) string {
	if def != "" {
		fmt.Fprintf(os.Stderr, "%s [%s]: ", label, def)
	} else {
		fmt.Fprintf(os.Stderr, "%s: ", label)
	}
	line, _ := readLine()
	if line == "" {
		return def
	}
	return line
}

// promptRequired keeps asking until a non-empty value is given. If stdin is
// closed (non-interactive) it exits with a clear message instead of looping,
// since the value can only come from a flag in that context.
func promptRequired(label string) string {
	for {
		v, eof := readLine2(label)
		if v != "" {
			return v
		}
		if eof {
			fmt.Fprintf(os.Stderr, "error: %s is required (no interactive input available — pass it as a flag)\n", label)
			os.Exit(2)
		}
		fmt.Fprintln(os.Stderr, "  (required)")
	}
}

// readLine2 prints the label prompt and reads one line, returning the trimmed
// value and whether stdin was closed.
func readLine2(label string) (string, bool) {
	fmt.Fprintf(os.Stderr, "%s: ", label)
	return readLine()
}

// promptSecret reads a value without echoing it to the terminal (best-effort via
// stty; falls back to echoed input if not a TTY). eof reports a closed stdin.
func promptSecret(label string) (value string, eof bool) {
	fmt.Fprintf(os.Stderr, "%s: ", label)
	restore := disableEcho()
	value, eof = readLine()
	restore()
	fmt.Fprintln(os.Stderr)
	return value, eof
}

// promptPassword reads a password twice and requires the two to match. A closed
// stdin (non-interactive) exits with a clear message rather than looping.
func promptPassword() string {
	for {
		p1, eof := promptSecret("Password")
		if eof {
			fmt.Fprintln(os.Stderr, "error: password is required (no interactive input available — pass --password)")
			os.Exit(2)
		}
		if p1 == "" {
			fmt.Fprintln(os.Stderr, "  (required)")
			continue
		}
		p2, _ := promptSecret("Confirm password")
		if p1 != p2 {
			fmt.Fprintln(os.Stderr, "  passwords do not match — try again")
			continue
		}
		return p1
	}
}

func disableEcho() func() {
	c := exec.Command("stty", "-echo")
	c.Stdin = os.Stdin
	if err := c.Run(); err != nil {
		return func() {} // not a TTY; input will be echoed
	}
	return func() {
		r := exec.Command("stty", "echo")
		r.Stdin = os.Stdin
		_ = r.Run()
	}
}
