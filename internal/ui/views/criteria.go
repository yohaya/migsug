package views

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/yourusername/migsug/internal/analyzer"
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
	if width < 60 {
		width = 80
	}

	// Title with full width separator
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("5"))
	sb.WriteString(titleStyle.Render(fmt.Sprintf("Migration Criteria - Source: %s", sourceNode)) + "\n")
	sb.WriteString(strings.Repeat("=", width) + "\n\n")

	// Instructions
	instructionStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	sb.WriteString(instructionStyle.Render("Select migration mode:") + "\n\n")

	// Mode options with descriptions in a table format
	modes := []struct {
		mode analyzer.MigrationMode
		name string
		desc string
	}{
		{analyzer.ModeVMCount, "VM Count", "Migrate N virtual machines"},
		{analyzer.ModeVCPU, "vCPU Count", "Migrate VMs totaling N vCPUs"},
		{analyzer.ModeCPUUsage, "CPU Usage", "Migrate VMs using N% CPU"},
		{analyzer.ModeRAM, "RAM Amount", "Migrate VMs using N GB RAM"},
		{analyzer.ModeStorage, "Storage Amount", "Migrate VMs using N GB storage"},
		{analyzer.ModeSpecific, "Specific VMs", "Select specific VMs to migrate"},
	}

	// Calculate column widths
	nameWidth := 18
	descWidth := width - nameWidth - 10 // Account for radio, spaces, padding
	if descWidth < 30 {
		descWidth = 30
	}

	// Styles
	selectedRowStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("240")).
		Foreground(lipgloss.Color("15")).
		Bold(true)

	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	for i, m := range modes {
		// Radio button
		radio := "( )"
		if m.mode == state.SelectedMode {
			radio = "(X)"
		}

		// Format the row
		name := fmt.Sprintf("%-*s", nameWidth, m.name)
		desc := fmt.Sprintf("%-*s", descWidth, m.desc)

		// Build row content
		rowContent := fmt.Sprintf(" %s  %s  %s", radio, name, dimStyle.Render(desc))

		// Apply selection highlighting
		if state.CursorPosition == i && !state.InputFocused {
			// Highlight entire row
			rowContent = fmt.Sprintf(" %s  %s  %s", radio, name, desc)
			sb.WriteString(selectedRowStyle.Render(rowContent))
		} else {
			sb.WriteString(rowContent)
		}
		sb.WriteString("\n")
	}

	sb.WriteString("\n")
	sb.WriteString(strings.Repeat("-", width) + "\n\n")

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
		inputSuffix = " %"
	case analyzer.ModeRAM:
		inputLabel = "RAM amount to free (GB)"
		inputValue = state.RAMAmount
		inputSuffix = " GB"
	case analyzer.ModeStorage:
		inputLabel = "Storage amount to free (GB)"
		inputValue = state.StorageAmount
		inputSuffix = " GB"
	case analyzer.ModeSpecific:
		showInput = false
		noteStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Italic(true)
		sb.WriteString(noteStyle.Render("Press Enter to select specific VMs on the next screen") + "\n")
	}

	if showInput {
		// Render input with ASCII-safe box
		labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("7"))
		sb.WriteString(labelStyle.Render(inputLabel+":") + "\n\n")
		sb.WriteString(renderInputBox(inputValue, inputSuffix, state.InputFocused, width))
		sb.WriteString("\n")
	}

	sb.WriteString("\n")

	// Help text - full width
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	if state.InputFocused {
		sb.WriteString(helpStyle.Render("Type a number and press Enter to confirm, or Esc to cancel"))
	} else {
		sb.WriteString(helpStyle.Render("Up/Down: Navigate modes | Enter: Select mode and input value | Esc: Back | q: Quit"))
	}

	return sb.String()
}

// renderInputBox creates an ASCII-safe input box
func renderInputBox(value string, suffix string, focused bool, width int) string {
	var sb strings.Builder

	// Input box width
	boxWidth := 20
	if boxWidth > width-10 {
		boxWidth = width - 10
	}

	// Display value or placeholder
	displayValue := value
	if displayValue == "" {
		displayValue = "     " // placeholder spaces
	}

	// Pad or truncate to fit box
	if len(displayValue) > boxWidth-4 {
		displayValue = displayValue[:boxWidth-4]
	}

	// Add cursor if focused
	if focused {
		displayValue = displayValue + "_"
	}

	// Pad to fill box
	displayValue = fmt.Sprintf("%-*s", boxWidth-4, displayValue)

	// Choose colors based on focus
	borderColor := "240" // dim gray
	textColor := "7"     // white
	if focused {
		borderColor = "3" // yellow
		textColor = "15"  // bright white
	}

	borderStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(borderColor))
	textStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(textColor))
	suffixStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	// Build ASCII box
	topBorder := "+" + strings.Repeat("-", boxWidth-2) + "+"
	bottomBorder := topBorder
	content := "| " + displayValue + " |"

	sb.WriteString("  " + borderStyle.Render(topBorder) + "\n")
	sb.WriteString("  " + borderStyle.Render("|") + " " + textStyle.Render(displayValue) + " " + borderStyle.Render("|") + suffixStyle.Render(suffix) + "\n")
	sb.WriteString("  " + borderStyle.Render(bottomBorder) + "\n")

	_ = content // unused but kept for reference

	return sb.String()
}
