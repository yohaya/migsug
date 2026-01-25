package views

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/yourusername/migsug/internal/analyzer"
	"github.com/yourusername/migsug/internal/proxmox"
	"github.com/yourusername/migsug/internal/ui/components"
)

// titleStyle for results view
var resultsTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("5"))

// RenderResults renders the migration results view (non-scrollable version for backwards compatibility)
func RenderResults(result *analyzer.AnalysisResult, width int) string {
	return RenderResultsWithScroll(result, width, 24, 0)
}

// RenderResultsWithScroll renders the migration results view with scrolling support
func RenderResultsWithScroll(result *analyzer.AnalysisResult, width, height, scrollPos int) string {
	var sb strings.Builder

	// Title
	sb.WriteString(resultsTitleStyle.Render("Migration Suggestions") + "\n\n")

	// Summary
	if len(result.Suggestions) == 0 {
		errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true)
		sb.WriteString(errorStyle.Render("No migration suggestions generated.") + "\n")
		sb.WriteString("This might mean:\n")
		sb.WriteString("  • No VMs match the criteria\n")
		sb.WriteString("  • No target nodes have sufficient capacity\n")
		sb.WriteString("  • All target nodes are excluded\n\n")
		return sb.String()
	}

	sb.WriteString(components.RenderMigrationSummary(
		result.TotalVMs,
		result.TotalVCPUs,
		result.TotalRAM,
		result.TotalStorage,
		result.ImprovementInfo,
	))
	sb.WriteString("\n\n")

	// Suggestions table with scrolling
	sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6")).
		Render("Suggested Migrations:") + " ")

	// Show scroll indicator if there are more suggestions than visible
	maxVisible := calculateVisibleRows(height)
	if len(result.Suggestions) > maxVisible {
		scrollInfo := fmt.Sprintf("(showing %d-%d of %d, use ↑/↓ to scroll)",
			scrollPos+1,
			min(scrollPos+maxVisible, len(result.Suggestions)),
			len(result.Suggestions))
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(scrollInfo))
	}
	sb.WriteString("\n\n")

	sb.WriteString(components.RenderSuggestionTableWithScroll(result.Suggestions, scrollPos, maxVisible))
	sb.WriteString("\n")

	// Before/After comparison
	sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6")).
		Render("Source Node Impact:") + "\n\n")
	sb.WriteString(components.RenderNodeStateComparison(
		result.SourceBefore.Name,
		result.SourceBefore,
		result.SourceAfter,
	))
	sb.WriteString("\n")

	// Target nodes
	if len(result.TargetsAfter) > 0 {
		sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6")).
			Render("Target Nodes Impact:") + "\n\n")

		for targetName, afterState := range result.TargetsAfter {
			beforeState := result.TargetsBefore[targetName]
			// Only show targets that actually receive VMs
			if afterState.VMCount != beforeState.VMCount {
				sb.WriteString(components.RenderNodeStateComparison(
					targetName,
					beforeState,
					afterState,
				))
				sb.WriteString("\n")
			}
		}
	}

	// Help text
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	sb.WriteString("\n" + helpStyle.Render("↑/↓: Scroll  s: Save  r: New Analysis  Esc: Back  q: Quit"))

	return sb.String()
}

// calculateVisibleRows calculates how many suggestion rows can fit on screen
func calculateVisibleRows(height int) int {
	// Reserve space for: title (2), summary (4), section headers (4), node comparison (8), help (2)
	// Each suggestion takes 2 rows (data + reason)
	reserved := 20
	available := height - reserved
	if available < 3 {
		return 3
	}
	return available / 2 // Each suggestion takes 2 lines
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// RenderVMSelection renders the VM selection view
func RenderVMSelection(vms []proxmox.VM, selectedVMs map[int]bool, cursorIdx int, width int) string {
	var sb strings.Builder

	// Title
	sb.WriteString(resultsTitleStyle.Render("Select VMs to Migrate") + "\n\n")

	// Instructions
	instructionStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	sb.WriteString(instructionStyle.Render(
		fmt.Sprintf("Selected: %d VMs - Use Space to toggle, Enter to confirm", len(selectedVMs))) + "\n\n")

	// VM table
	sb.WriteString(components.RenderVMTable(vms, selectedVMs, cursorIdx))
	sb.WriteString("\n")

	// Help text
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	sb.WriteString(helpStyle.Render("↑/↓: Navigate  Space: Toggle  Enter: Confirm  Esc: Back"))

	return sb.String()
}
