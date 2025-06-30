package components

import (
	"time"
)

// SpinnerStyle represents different spinner animations
type SpinnerStyle int

const (
	SpinnerDots SpinnerStyle = iota
	SpinnerLine
	SpinnerCircle
)

// Spinner provides animated loading indicators
type Spinner struct {
	frames []string
	fps    time.Duration
}

// NewSpinner creates a new spinner with the given style
func NewSpinner(style SpinnerStyle) *Spinner {
	switch style {
	case SpinnerLine:
		return &Spinner{
			frames: []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"},
			fps:    80 * time.Millisecond,
		}
	case SpinnerCircle:
		return &Spinner{
			frames: []string{"◐", "◓", "◑", "◒"},
			fps:    120 * time.Millisecond,
		}
	default: // SpinnerDots
		return &Spinner{
			frames: []string{"⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷"},
			fps:    100 * time.Millisecond,
		}
	}
}

// Frame returns the current frame based on the elapsed time
func (s *Spinner) Frame(elapsed time.Duration) string {
	frameIndex := int(elapsed/s.fps) % len(s.frames)
	return s.frames[frameIndex]
}

// FPS returns the frame duration for smooth animation
func (s *Spinner) FPS() time.Duration {
	return s.fps
}
