package main

import (
	"testing"
	
	tea "github.com/charmbracelet/bubbletea"
)

// Test that demonstrates the Ctrl+C bug on inspection screen
func TestCtrlCBug(t *testing.T) {
	m := model{
		screen: screenInspection,
		ready:  true,
	}
	
	// Create Ctrl+C key message
	ctrlC := tea.KeyMsg{Type: tea.KeyCtrlC}
	
	// Call handleKeyMsg
	_, cmd := m.handleKeyMsg(ctrlC)
	
	// Check if quit command was returned
	if cmd == nil {
		t.Error("Ctrl+C on inspection screen returned nil command - BUG CONFIRMED")
	} else if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("Ctrl+C on inspection screen returned wrong command type: %T", cmd())
	}
}

// Test that demonstrates the working 'q' key on inspection screen
func TestQKeyWorks(t *testing.T) {
	m := model{
		screen: screenInspection,
		ready:  true,
	}
	
	// Create 'q' key message
	qKey := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}}
	
	// Call handleKeyMsg
	_, cmd := m.handleKeyMsg(qKey)
	
	// Check if quit command was returned
	if cmd == nil {
		t.Error("'q' key on inspection screen returned nil command")
	} else if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("'q' key on inspection screen returned wrong command type: %T", cmd())
	}
}

// Test Ctrl+L to switch to debug log
func TestCtrlLDebugLog(t *testing.T) {
	// This test is just documentation
	// Ctrl+L is detected via msg.String() == "ctrl+l" in the actual code
	// which is hard to simulate in unit tests without the full bubbletea framework
	t.Log("Note: Ctrl+L test requires full integration testing")
}