package tui

import (
	"os"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/muesli/termenv"
)

// dynamoCopyJSONCmd copies query result JSON to the system clipboard when the
// OS API is available; otherwise it uses OSC 52 for terminal-side clipboard.
func dynamoCopyJSONCmd(text string) tea.Cmd {
	return func() tea.Msg {
		if err := clipboard.WriteAll(text); err != nil {
			termenv.NewOutput(os.Stdout).Copy(text)
		}
		return nil
	}
}
