package ui

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fatih/color"
)

var (
	cyan   = color.New(color.FgCyan).SprintFunc()
	green  = color.New(color.FgGreen).SprintFunc()
	yellow = color.New(color.FgYellow).SprintFunc()
	red    = color.New(color.FgRed).SprintFunc()
	dim    = color.New(color.Faint).SprintFunc()
	bold   = color.New(color.Bold).SprintFunc()

	reader = bufio.NewReader(os.Stdin)
)

// Intro prints a styled header.
func Intro(msg string) {
	fmt.Printf("\n  %s\n\n", bold(msg))
}

// Outro prints a styled footer.
func Outro(msg string) {
	if msg != "" {
		fmt.Printf("\n  %s\n\n", msg)
	} else {
		fmt.Println()
	}
}

// Note prints a boxed message.
func Note(content, title string) {
	lines := strings.Split(content, "\n")
	maxLen := len(title)
	for _, l := range lines {
		stripped := stripAnsi(l)
		if len(stripped) > maxLen {
			maxLen = len(stripped)
		}
	}
	width := maxLen + 4
	if width < 40 {
		width = 40
	}

	border := dim(strings.Repeat("─", width))
	fmt.Printf("  %s %s %s\n", dim("┌"), dim(title), border[:len(border)-len(title)*3])
	for _, l := range lines {
		fmt.Printf("  %s  %s\n", dim("│"), l)
	}
	fmt.Printf("  %s%s\n", dim("└"), border)
}

// Cancel prints a cancellation message and exits.
func Cancel(msg string) {
	fmt.Printf("  %s %s\n", red("✗"), msg)
}

// Log provides structured log output.
var Log = struct {
	Step    func(string)
	Info    func(string)
	Success func(string)
	Warn    func(string)
	Error   func(string)
	Message func(string)
}{
	Step:    func(msg string) { fmt.Printf("  %s %s\n", dim("◆"), msg) },
	Info:    func(msg string) { fmt.Printf("  %s %s\n", cyan("ℹ"), msg) },
	Success: func(msg string) { fmt.Printf("  %s %s\n", green("✓"), msg) },
	Warn:    func(msg string) { fmt.Printf("  %s %s\n", yellow("⚠"), msg) },
	Error:   func(msg string) { fmt.Printf("  %s %s\n", red("✗"), msg) },
	Message: func(msg string) { fmt.Printf("  %s\n", msg) },
}

// Confirm asks a yes/no question. Returns the answer and whether the user cancelled (Ctrl+C/empty).
func Confirm(msg string, defaultVal bool) (bool, bool) {
	hint := "Y/n"
	if !defaultVal {
		hint = "y/N"
	}
	fmt.Printf("  %s %s %s ", dim("◆"), msg, dim("("+hint+")"))

	line, err := reader.ReadString('\n')
	if err != nil {
		return false, true
	}
	line = strings.TrimSpace(strings.ToLower(line))
	if line == "" {
		return defaultVal, false
	}
	return line == "y" || line == "yes", false
}

// SelectOption is a choice for the Select prompt.
type SelectOption struct {
	Value string
	Label string
}

// Select asks the user to pick from a list. Returns the chosen value and whether cancelled.
func Select(msg string, options []SelectOption) (string, bool) {
	fmt.Printf("  %s %s\n", dim("◆"), msg)
	for i, opt := range options {
		fmt.Printf("    %s %s\n", cyan(fmt.Sprintf("%d.", i+1)), opt.Label)
	}
	fmt.Printf("  %s ", dim("›"))

	line, err := reader.ReadString('\n')
	if err != nil {
		return "", true
	}
	line = strings.TrimSpace(line)
	idx, err := strconv.Atoi(line)
	if err != nil || idx < 1 || idx > len(options) {
		// Default to first option
		return options[0].Value, false
	}
	return options[idx-1].Value, false
}

// Text asks for free-form text input. Returns the text and whether cancelled.
func Text(msg, placeholder string) (string, bool) {
	if placeholder != "" {
		fmt.Printf("  %s %s %s\n  %s ", dim("◆"), msg, dim("("+placeholder+")"), dim("›"))
	} else {
		fmt.Printf("  %s %s\n  %s ", dim("◆"), msg, dim("›"))
	}

	line, err := reader.ReadString('\n')
	if err != nil {
		return "", true
	}
	return strings.TrimSpace(line), false
}

// Spinner provides an animated spinner for long operations.
type Spinner struct {
	msg    string
	done   chan struct{}
	mu     sync.Mutex
	active bool
}

// NewSpinner creates a spinner.
func NewSpinner() *Spinner {
	return &Spinner{}
}

// Start begins the spinner animation.
func (s *Spinner) Start(msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active {
		return
	}
	s.msg = msg
	s.done = make(chan struct{})
	s.active = true
	go s.run()
}

// SetMessage updates the spinner message.
func (s *Spinner) SetMessage(msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.msg = msg
}

// Stop halts the spinner and prints a final message.
func (s *Spinner) Stop(msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.active {
		return
	}
	s.active = false
	close(s.done)
	fmt.Printf("\r  %s %s\n", green("✓"), msg)
}

func (s *Spinner) run() {
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	i := 0
	ticker := time.NewTicker(80 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			s.mu.Lock()
			msg := s.msg
			s.mu.Unlock()
			fmt.Printf("\r  %s %s", cyan(frames[i%len(frames)]), msg)
			i++
		}
	}
}

// Color helpers for use in commands.
func Cyan(s string) string   { return cyan(s) }
func Green(s string) string  { return green(s) }
func Yellow(s string) string { return yellow(s) }
func Red(s string) string    { return red(s) }
func Dim(s string) string    { return dim(s) }
func Bold(s string) string   { return bold(s) }

func stripAnsi(s string) string {
	var result []byte
	inEsc := false
	for i := 0; i < len(s); i++ {
		if s[i] == '\x1b' {
			inEsc = true
			continue
		}
		if inEsc {
			if s[i] == 'm' {
				inEsc = false
			}
			continue
		}
		result = append(result, s[i])
	}
	return string(result)
}
