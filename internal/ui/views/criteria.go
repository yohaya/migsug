package views

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/yourusername/migsug/internal/analyzer"
)

// Box drawing characters
const (
	criteriaBoxHoriz = "━"
	criteriaBoxThin  = "─"
)

// CriteriaState holds the state for criteria input
type CriteriaState struct {
	SelectedMode   analyzer.MigrationMode
	VMCount        string
	VCPUCount      string
	CPUUsage       string
	RAMAmount      string
	StorageAmount  string
	SelectedVMs    map[int]bool
	ExcludeNodes   []string
	CursorPosition int
	InputFocused   bool
}

// RenderCriteria renders the criteria selection view
func RenderCriteria(state CriteriaState, sourceNode string, width int) string {
	var sb strings.Builder

	// Ensure minimum width
	if width < 80 {
		width = 100
	}

	// Title with graphical border
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("5"))
	borderStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("6"))

	sb.WriteString(titleStyle.Render(fmt.Sprintf("Migration Criteria │ Source: %s", sourceNode)) + "\n")
	sb.WriteString(borderStyle.Render(strings.Repeat(criteriaBoxHoriz, width)) + "\n\n")

	// Instructions
	instructionStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	sb.WriteString(instructionStyle.Render("Select migration mode:") + "\n\n")

	// Mode options table header
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	header := fmt.Sprintf("  %-20s %-50s", "Mode", "Description")
	sb.WriteString(headerStyle.Render(header) + "\n")
	sb.WriteString("  " + borderStyle.Render(strings.Repeat(criteriaBoxHoriz, width-4)) + "\n")

	// Mode options with descriptions
	modes := []struct {
		mode analyzer.MigrationMode
		name string
		desc string
	}{
		{analyzer.ModeVMCount, "VM Count", "Migrate a specific number of virtual machines"},
		{analyzer.ModeVCPU, "vCPU Count", "Migrate VMs until reaching target vCPU count"},
		{analyzer.ModeCPUUsage, "CPU Usage %", "Migrate VMs to free up target CPU percentage"},
		{analyzer.ModeRAM, "RAM Amount", "Migrate VMs to free up target RAM (in GB)"},
		{analyzer.ModeStorage, "Storage Amount", "Migrate VMs to free up target storage (in GB)"},
		{analyzer.ModeSpecific, "Specific VMs", "Manually select which VMs to migrate"},
	}

	// Styles for selection
	selectedBgStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("236")).
		Foreground(lipgloss.Color("15")).
		Bold(true)
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	for i, m := range modes {
		isSelected := state.CursorPosition == i && !state.InputFocused

		// Build row content
		name := fmt.Sprintf("%-20s", m.name)
		desc := fmt.Sprintf("%-50s", m.desc)

		// Selector indicator
		selector := "  "
		if isSelected {
			selector = "▶ "
		}

		// Apply styling
		if isSelected {
			rowContent := fmt.Sprintf("%s%s", name, desc)
			// Pad to full width
			if len(rowContent) < width-4 {
				rowContent += strings.Repeat(" ", width-4-len(rowContent))
			}
			sb.WriteString(selector + selectedBgStyle.Render(rowContent) + "\n")
		} else {
			sb.WriteString(selector + name + dimStyle.Render(desc) + "\n")
		}
	}

	sb.WriteString("\n")
	sb.WriteString(borderStyle.Render(strings.Repeat(criteriaBoxThin, width)) + "\n\n")

	// Input field based on selected mode
	inputLabel := ""
	inputValue := ""
	inputSuffix := ""
	showInput := true

	switch state.SelectedMode {
	case analyzer.ModeVMCount:
		inputLabel = "Number of VMs to migrate"
		inputValue = state.VMCount
	case analyzer.ModeVCPU:
		inputLabel = "Total vCPUs to migrate"
		inputValue = state.VCPUCount
	case analyzer.ModeCPUUsage:
		inputLabel = "CPU usage percentage to free"
		inputValue = state.CPUUsage
		inputSuffix = "%"
	case analyzer.ModeRAM:
		inputLabel = "RAM amount to free"
		inputValue = state.RAMAmount
		inputSuffix = "GB"
	case analyzer.ModeStorage:
		inputLabel = "Storage amount to free"
		inputValue = state.StorageAmount
		inputSuffix = "GB"
	case analyzer.ModeSpecific:
		showInput = false
		noteStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Italic(true)
		sb.WriteString(noteStyle.Render("  ℹ Press Enter to select specific VMs on the next screen") + "\n")
	}

	if showInput {
		labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("7"))
		sb.WriteString(labelStyle.Render("  "+inputLabel+":") + "\n\n")
		sb.WriteString(renderUnicodeInputBox(inputValue, inputSuffix, state.InputFocused, width))
		sb.WriteString("\n")
	}

	sb.WriteString("\n")

	// Help text
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	if state.InputFocused {
		sb.WriteString(helpStyle.Render("Type value │ Enter: Confirm │ Esc: Cancel input"))
	} else {
		sb.WriteString(helpStyle.Render("↑/↓: Navigate │ Enter: Select mode │ Esc: Back to host selection │ q: Quit"))
	}

	return sb.String()
}

// renderUnicodeInputBox creates a Unicode input box
func renderUnicodeInputBox(value string, suffix string, focused bool, width int) string {
	var sb strings.Builder

	// Input box width
	boxWidth := 25

	// Display value with cursor
	displayValue := value
	if focused {
		displayValue = value + "█"
	}
	if displayValue == "" && !focused {
		displayValue = "     " // placeholder
	}

	// Pad to fill box
	if len(displayValue) < boxWidth-4 {
		displayValue = displayValue + strings.Repeat(" ", boxWidth-4-len(displayValue))
	} else if len(displayValue) > boxWidth-4 {
		displayValue = displayValue[:boxWidth-4]
	}

	// Choose colors based on focus
	borderColor := "240"
	if focused {
		borderColor = "3" // yellow when focused
	}

	borderStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(borderColor))
	textStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Bold(true)
	suffixStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	// Unicode box
	topBorder := "┌" + strings.Repeat("─", boxWidth-2) + "┐"
	bottomBorder := "└" + strings.Repeat("─", boxWidth-2) + "┘"

	sb.WriteString("  " + borderStyle.Render(topBorder) + "\n")
	sb.WriteString("  " + borderStyle.Render("│") + " " + textStyle.Render(displayValue) + " " + borderStyle.Render("│"))
	if suffix != "" {
		sb.WriteString(" " + suffixStyle.Render(suffix))
	}
	sb.WriteString("\n")
	sb.WriteString("  " + borderStyle.Render(bottomBorder) + "\n")

	return sb.String()
}
