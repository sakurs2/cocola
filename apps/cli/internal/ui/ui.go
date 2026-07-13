package ui

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

const banner = `
 ██████╗ ██████╗  ██████╗ ██████╗ ██╗      █████╗
██╔════╝██╔═══██╗██╔════╝██╔═══██╗██║     ██╔══██╗
██║     ██║   ██║██║     ██║   ██║██║     ███████║
╚██████╗╚██████╔╝╚██████╗╚██████╔╝███████╗██║  ██║
 ╚═════╝ ╚═════╝  ╚═════╝ ╚═════╝ ╚══════╝╚═╝  ╚═╝`

type Printer struct {
	Out   io.Writer
	Err   io.Writer
	Color bool
	JSON  bool
}

func AutoColor(out io.Writer, disabled bool) bool {
	if disabled || os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	file, ok := out.(*os.File)
	return ok && term.IsTerminal(int(file.Fd()))
}

func (p Printer) Banner() {
	if p.JSON {
		return
	}
	lines := strings.Split(strings.TrimPrefix(banner, "\n"), "\n")
	colors := []lipgloss.Color{"#7568FF", "#687BFF", "#5798FF", "#43B8F5", "#35D0D0"}
	for index, line := range lines {
		if p.Color {
			line = lipgloss.NewStyle().Foreground(colors[index%len(colors)]).Bold(true).Render(line)
		}
		fmt.Fprintln(p.Out, line)
	}
	if p.Color {
		fmt.Fprintln(p.Out, lipgloss.NewStyle().Foreground(lipgloss.Color("#7C8799")).Render("Simple, reliable agent infrastructure."))
	} else {
		fmt.Fprintln(p.Out, "Simple, reliable agent infrastructure.")
	}
}

func (p Printer) Section(title string) {
	if p.JSON {
		return
	}
	if p.Color {
		title = lipgloss.NewStyle().Foreground(lipgloss.Color("#7568FF")).Bold(true).Render(title)
	}
	fmt.Fprintf(p.Out, "\n%s\n", title)
}

func (p Printer) Info(message string)    { p.line(p.Out, "→", "#5798FF", message) }
func (p Printer) Success(message string) { p.line(p.Out, "✓", "#30C48D", message) }
func (p Printer) Warn(message string)    { p.line(p.Err, "!", "#F4B942", message) }
func (p Printer) Error(message string)   { p.line(p.Err, "✗", "#FF5D73", message) }

func (p Printer) line(writer io.Writer, symbol, color, message string) {
	if p.JSON {
		return
	}
	if p.Color {
		symbol = lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Bold(true).Render(symbol)
	}
	fmt.Fprintf(writer, "%s %s\n", symbol, message)
}

func (p Printer) KeyValues(rows [][2]string) {
	if p.JSON {
		return
	}
	width := 0
	for _, row := range rows {
		if len(row[0]) > width {
			width = len(row[0])
		}
	}
	for _, row := range rows {
		key := fmt.Sprintf("%-*s", width, row[0])
		if p.Color {
			key = lipgloss.NewStyle().Foreground(lipgloss.Color("#7C8799")).Render(key)
		}
		fmt.Fprintf(p.Out, "  %s  %s\n", key, row[1])
	}
}

func (p Printer) Encode(value any) error {
	encoder := json.NewEncoder(p.Out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
