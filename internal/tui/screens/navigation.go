package screens

import (
	"strconv"

	tea "github.com/charmbracelet/bubbletea"
)

// NavigationHandler handles all navigation-related key events for MainScreen
type NavigationHandler struct {
	screen *MainScreen
}

// NewNavigationHandler creates a new navigation handler
func NewNavigationHandler(screen *MainScreen) *NavigationHandler {
	return &NavigationHandler{screen: screen}
}

// HandleKey processes navigation keys and returns whether the key was handled
func (nh *NavigationHandler) HandleKey(msg tea.KeyMsg) (handled bool, model tea.Model, cmd tea.Cmd) {
	// List navigation keys
	switch msg.String() {
	case "up", "k":
		return true, nh.moveSelection(-1), nil

	case "down", "j":
		return true, nh.moveSelection(1), nil

	case "pgup":
		return true, nh.moveSelection(-10), nil

	case "pgdown":
		return true, nh.moveSelection(10), nil

	case "home":
		return true, nh.jumpToFirst(), nil

	case "end":
		return true, nh.jumpToLast(), nil

	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		if nh.screen.activeTab == 0 { // Currently only for tools, but could be extended
			return true, nh.quickSelect(msg.String()), nil
		}
		return false, nh.screen, nil
	}

	return false, nh.screen, nil
}

// moveSelection moves the selection by the given offset
func (nh *NavigationHandler) moveSelection(offset int) tea.Model {
	actualCount := nh.screen.getActualItemCount()
	if actualCount == 0 {
		return nh.screen
	}

	currentList := nh.screen.getCurrentList()
	if len(currentList) == 0 {
		return nh.screen
	}

	// Get current position
	current, exists := nh.screen.selectedIndex[nh.screen.activeTab]
	if !exists {
		current = 0
	}

	// Calculate new position
	newPos := current + offset

	// Bound the position
	if newPos < 0 {
		newPos = 0
	} else if newPos >= len(currentList) {
		newPos = len(currentList) - 1
	}

	nh.screen.selectedIndex[nh.screen.activeTab] = newPos
	return nh.screen
}

// jumpToFirst jumps to the first item in the current list
func (nh *NavigationHandler) jumpToFirst() tea.Model {
	if nh.screen.getActualItemCount() > 0 {
		nh.screen.selectedIndex[nh.screen.activeTab] = 0
	}
	return nh.screen
}

// jumpToLast jumps to the last item in the current list
func (nh *NavigationHandler) jumpToLast() tea.Model {
	if nh.screen.getActualItemCount() > 0 {
		currentList := nh.screen.getCurrentList()
		if len(currentList) > 0 {
			nh.screen.selectedIndex[nh.screen.activeTab] = len(currentList) - 1
		}
	}
	return nh.screen
}

// quickSelect selects an item by number key
func (nh *NavigationHandler) quickSelect(key string) tea.Model {
	if nh.screen.getActualItemCount() == 0 {
		return nh.screen
	}

	num, err := strconv.Atoi(key)
	if err != nil {
		return nh.screen
	}

	idx := num - 1 // Convert to 0-based index
	currentList := nh.screen.getCurrentList()

	if idx < len(currentList) {
		nh.screen.selectedIndex[nh.screen.activeTab] = idx
		// For now, just update selection. The user can press Enter to activate.
		// This keeps navigation separate from actions.
		return nh.screen
	}

	return nh.screen
}
