package components

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// ProgressBar renders a visual progress indicator
type ProgressBar struct {
	width       int
	showPercent bool
	fillChar    string
	emptyChar   string
	style       lipgloss.Style
}

// NewProgressBar creates a new progress bar
func NewProgressBar(width int) *ProgressBar {
	return &ProgressBar{
		width:       width,
		showPercent: true,
		fillChar:    "█",
		emptyChar:   "░",
		style:       lipgloss.NewStyle().Foreground(lipgloss.Color("212")),
	}
}

// Render returns the progress bar as a string
func (p *ProgressBar) Render(percent float64) string {
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}

	filled := int(float64(p.width) * percent / 100)
	empty := p.width - filled

	bar := strings.Repeat(p.fillChar, filled) + strings.Repeat(p.emptyChar, empty)
	
	if p.showPercent {
		return fmt.Sprintf("%s %3.0f%%", p.style.Render(bar), percent)
	}
	return p.style.Render(bar)
}

// IndeterminateProgress shows an indeterminate progress indicator
type IndeterminateProgress struct {
	width     int
	style     lipgloss.Style
	fillStyle lipgloss.Style
}

// NewIndeterminateProgress creates a new indeterminate progress indicator
func NewIndeterminateProgress(width int) *IndeterminateProgress {
	return &IndeterminateProgress{
		width:     width,
		style:     lipgloss.NewStyle().Foreground(lipgloss.Color("240")),
		fillStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("212")),
	}
}

// Render returns the indeterminate progress bar based on elapsed time
func (ip *IndeterminateProgress) Render(elapsed time.Duration) string {
	// Create a moving block effect
	position := int(elapsed.Seconds()*10) % (ip.width + 10)
	
	bar := make([]string, ip.width)
	for i := 0; i < ip.width; i++ {
		bar[i] = "░"
	}
	
	// Create a 5-character wide moving block
	blockSize := 5
	for i := 0; i < blockSize; i++ {
		pos := position - i
		if pos >= 0 && pos < ip.width {
			bar[pos] = "█"
		}
	}
	
	// Apply styles
	result := ""
	for _, char := range bar {
		if char == "█" {
			result += ip.fillStyle.Render(string(char))
		} else {
			result += ip.style.Render(string(char))
		}
	}
	
	return result
}

// ProgressMessage shows a progress message with elapsed time
func ProgressMessage(message string, elapsed time.Duration, showSpinner bool) string {
	timeStr := formatDuration(elapsed)
	
	if showSpinner {
		spinner := NewSpinner(SpinnerLine)
		spinnerFrame := spinner.Frame(elapsed)
		return fmt.Sprintf("%s %s (%s)", spinnerFrame, message, timeStr)
	}
	
	return fmt.Sprintf("%s (%s)", message, timeStr)
}

// formatDuration formats a duration in a human-readable way
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	
	minutes := int(d.Minutes())
	seconds := int(d.Seconds()) % 60
	
	if minutes < 60 {
		return fmt.Sprintf("%dm %ds", minutes, seconds)
	}
	
	hours := minutes / 60
	minutes = minutes % 60
	return fmt.Sprintf("%dh %dm %ds", hours, minutes, seconds)
}