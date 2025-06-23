package main

import (
	"fmt"
	"os"
	
	tea "github.com/charmbracelet/bubbletea"
)

type model struct {
	lastKey string
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Show key details
		m.lastKey = fmt.Sprintf("Type: %v, String: %q, Runes: %v", msg.Type, msg.String(), msg.Runes)
		
		// Quit on 'q' or Ctrl+C
		if msg.Type == tea.KeyCtrlC || msg.String() == "q" {
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m model) View() string {
	return fmt.Sprintf(`Key Test Program
================
Press any key to see how it's detected
Press 'q' or Ctrl+C to quit

Last key pressed:
%s

Try pressing:
- Ctrl+L
- Ctrl+C
- Regular keys
`, m.lastKey)
}

func main() {
	p := tea.NewProgram(model{})
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v", err)
		os.Exit(1)
	}
}