package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	barStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	emptyBarStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	criticalStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	warningStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
)

// RenderResourceBar creates a visual progress bar for resource usage
func RenderResourceBar(label string, percent float64, width int) string {
	barWidth := width - len(label) - 10 // Reserve space for label and percentage

	if barWidth < 10 {
		barWidth = 10
	}

	filled := int((percent / 100.0) * float64(barWidth))
	if filled > barWidth {
		filled = barWidth
	}
	empty := barWidth - filled

	// Choose color based on utilization
	style := barStyle
	if percent >= 90 {
		style = criticalStyle
	} else if percent >= 75 {
		style = warningStyle
	}

	bar := style.Render(strings.Repeat("█", filled)) +
		emptyBarStyle.Render(strings.Repeat("░", empty))

	return fmt.Sprintf("%s [%s] %5.1f%%", label, bar, percent)
}

// RenderResourceBarWithValues creates a bar with actual values
func RenderResourceBarWithValues(label string, used, total int64, width int) string {
	percent := 0.0
	if total > 0 {
		percent = float64(used) / float64(total) * 100
	}

	barWidth := width - len(label) - 30 // Reserve more space for values

	if barWidth < 10 {
		barWidth = 10
	}

	filled := int((percent / 100.0) * float64(barWidth))
	if filled > barWidth {
		filled = barWidth
	}
	empty := barWidth - filled

	// Choose color based on utilization
	style := barStyle
	if percent >= 90 {
		style = criticalStyle
	} else if percent >= 75 {
		style = warningStyle
	}

	bar := style.Render(strings.Repeat("█", filled)) +
		emptyBarStyle.Render(strings.Repeat("░", empty))

	return fmt.Sprintf("%s [%s] %5.1f%% (%s / %s)",
		label, bar, percent,
		FormatBytes(used), FormatBytes(total))
}

// FormatBytes converts bytes to human-readable format
func FormatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	units := []string{"KB", "MB", "GB", "TB", "PB"}
	return fmt.Sprintf("%.1f %s", float64(bytes)/float64(div), units[exp])
}
