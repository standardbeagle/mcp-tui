package screens

import (
	tea "github.com/charmbracelet/bubbletea"
)

// Screen represents a TUI screen interface
type Screen interface {
	tea.Model
	
	// Name returns the screen name for debugging
	Name() string
	
	// CanGoBack returns true if this screen supports going back
	CanGoBack() bool
	
	// Reset resets the screen to its initial state
	Reset()
}

// ScreenTransition represents a transition between screens
type ScreenTransition struct {
	Screen Screen
	Data   interface{} // Optional data to pass to the new screen
}

// TransitionMsg is sent when switching screens
type TransitionMsg struct {
	Transition ScreenTransition
}

// BackMsg is sent when going back to the previous screen
type BackMsg struct{}

// ErrorMsg is sent when an error occurs
type ErrorMsg struct {
	Error error
}

// StatusMsg is sent to update the status bar
type StatusMsg struct {
	Message string
	Level   StatusLevel
}

// StatusLevel represents the type of status message
type StatusLevel int

const (
	StatusInfo StatusLevel = iota
	StatusWarning
	StatusError
	StatusSuccess
)

// BaseScreen provides common functionality for all screens
type BaseScreen struct {
	name        string
	canGoBack   bool
	width       int
	height      int
	lastError   error
	statusMsg   string
	statusLevel StatusLevel
}

// NewBaseScreen creates a new base screen
func NewBaseScreen(name string, canGoBack bool) *BaseScreen {
	return &BaseScreen{
		name:      name,
		canGoBack: canGoBack,
	}
}

// Name returns the screen name
func (bs *BaseScreen) Name() string {
	return bs.name
}

// CanGoBack returns whether this screen supports going back
func (bs *BaseScreen) CanGoBack() bool {
	return bs.canGoBack
}

// Reset resets the screen state
func (bs *BaseScreen) Reset() {
	bs.lastError = nil
	bs.statusMsg = ""
	bs.statusLevel = StatusInfo
}

// SetError sets an error on the screen
func (bs *BaseScreen) SetError(err error) {
	bs.lastError = err
	if err != nil {
		bs.statusMsg = err.Error()
		bs.statusLevel = StatusError
	}
}

// SetStatus sets a status message
func (bs *BaseScreen) SetStatus(message string, level StatusLevel) {
	bs.statusMsg = message
	bs.statusLevel = level
}

// UpdateSize updates the screen dimensions
func (bs *BaseScreen) UpdateSize(width, height int) {
	bs.width = width
	bs.height = height
}

// Width returns the current width
func (bs *BaseScreen) Width() int {
	return bs.width
}

// Height returns the current height
func (bs *BaseScreen) Height() int {
	return bs.height
}

// LastError returns the last error
func (bs *BaseScreen) LastError() error {
	return bs.lastError
}

// StatusMessage returns the current status message and level
func (bs *BaseScreen) StatusMessage() (string, StatusLevel) {
	return bs.statusMsg, bs.statusLevel
}