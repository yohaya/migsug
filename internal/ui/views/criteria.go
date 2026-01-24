package views

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/yourusername/migsug/internal/analyzer"
)

var (
	selectedOptionStyle = lipgloss.NewStyle().
		Background(lipgloss.Color("6")).
		Foreground(lipgloss.Color("0")).
		Bold(true).
		Padding(0, 1)

	optionStyle = lipgloss.NewStyle().
		Padding(0, 1)

	inputStyle = lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("6")).
		Padding(0, 1)
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
func RenderCriteria(state CriteriaState, sourcNode string, width int) string {
	var sb strings.Builder

	// Title
	sb.WriteString(titleStyle.Render(fmt.Sprintf("Migration Criteria - Source: %s", sourcNode)) + "\n\n")

	// Instructions
	instructionStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	sb.WriteString(instructionStyle.Render("Select migration mode:") + "\n\n")

	// Mode options
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

	for i, m := range modes {
		style := optionStyle
		if state.CursorPosition == i && !state.InputFocused {
			style = selectedOptionStyle
		}

		radio := "○"
		if m.mode == state.SelectedMode {
			radio = "●"
		}

		sb.WriteString(fmt.Sprintf("  %s %s\n", radio, style.Render(m.name)))
		descStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Italic(true).MarginLeft(4)
		sb.WriteString(descStyle.Render(m.desc) + "\n")
	}

	sb.WriteString("\n")

	// Input field based on selected mode
	switch state.SelectedMode {
	case analyzer.ModeVMCount:
		sb.WriteString("Number of VMs to migrate: ")
		sb.WriteString(renderInput(state.VMCount, state.InputFocused) + "\n")

	case analyzer.ModeVCPU:
		sb.WriteString("Total vCPUs to migrate: ")
		sb.WriteString(renderInput(state.VCPUCount, state.InputFocused) + "\n")

	case analyzer.ModeCPUUsage:
		sb.WriteString("CPU usage percentage to free: ")
		sb.WriteString(renderInput(state.CPUUsage, state.InputFocused) + "%\n")

	case analyzer.ModeRAM:
		sb.WriteString("RAM amount to free (GB): ")
		sb.WriteString(renderInput(state.RAMAmount, state.InputFocused) + "\n")

	case analyzer.ModeStorage:
		sb.WriteString("Storage amount to free (GB): ")
		sb.WriteString(renderInput(state.StorageAmount, state.InputFocused) + "\n")

	case analyzer.ModeSpecific:
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).
			Render("(VM selection will be shown on next screen)") + "\n")
	}

	sb.WriteString("\n")

	// Help text
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	if state.InputFocused {
		sb.WriteString(helpStyle.Render("Type value, Enter to confirm, Esc to cancel"))
	} else {
		sb.WriteString(helpStyle.Render("↑/↓: Navigate  Enter: Select/Input  Esc: Back  q: Quit"))
	}

	return sb.String()
}

func renderInput(value string, focused bool) string {
	if focused {
		return inputStyle.BorderForeground(lipgloss.Color("3")).Render(value + "█")
	}
	if value == "" {
		return inputStyle.BorderForeground(lipgloss.Color("240")).Render("_____")
	}
	return inputStyle.Render(value)
}
