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
	return RenderResultsFull(result, nil, "", width, height, scrollPos)
}

// RenderResultsFull renders the migration results view with full header
func RenderResultsFull(result *analyzer.AnalysisResult, cluster *proxmox.Cluster, version string, width, height, scrollPos int) string {
	return RenderResultsWithCursor(result, cluster, version, width, height, scrollPos, -1)
}

// RenderResultsWithCursor renders the migration results view with cursor navigation
func RenderResultsWithCursor(result *analyzer.AnalysisResult, cluster *proxmox.Cluster, version string, width, height, scrollPos, cursorPos int) string {
	var sb strings.Builder

	// Ensure minimum width
	if width < 80 {
		width = 100
	}

	// Title with version (same as dashboard)
	versionStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	borderStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("6"))

	title := "KVM Migration Suggester"
	if version != "" && version != "dev" {
		title += " " + versionStyle.Render("v"+version)
	}
	sb.WriteString(resultsTitleStyle.Render(title) + "\n")

	// Graphical top border
	sb.WriteString(borderStyle.Render(strings.Repeat("━", width)) + "\n\n")

	// Cluster summary if available
	if cluster != nil {
		sb.WriteString(renderResultsClusterSummary(cluster, width))
		sb.WriteString("\n")
	}

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

	sb.WriteString(components.RenderSuggestionTableWithCursor(result.Suggestions, scrollPos, maxVisible, cursorPos))
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
	sb.WriteString("\n" + helpStyle.Render("↑/↓/PgUp/PgDn/Home/End: Navigate  s: Save  r: New Analysis  Esc: Back  q: Quit"))

	return sb.String()
}

// calculateVisibleRows calculates how many suggestion rows can fit on screen
func calculateVisibleRows(height int) int {
	// Reserve space for: title (2), summary (4), section headers (4), node comparison (8), help (2)
	// Each suggestion takes 1 row
	reserved := 20
	available := height - reserved
	if available < 5 {
		return 5
	}
	return available // Each suggestion takes 1 line
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// renderResultsClusterSummary creates a compact cluster summary for results view
func renderResultsClusterSummary(cluster *proxmox.Cluster, width int) string {
	var sb strings.Builder

	labelStyle := lipgloss.NewStyle() // Regular text color
	valueStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	runningStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	stoppedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	// Count online nodes
	onlineNodes := 0
	for _, node := range cluster.Nodes {
		if node.Status == "online" {
			onlineNodes++
		}
	}

	// Calculate cluster-wide storage in TiB
	totalStorageTiB := float64(cluster.TotalStorage) / (1024 * 1024 * 1024 * 1024)
	usedStorageTiB := float64(cluster.UsedStorage) / (1024 * 1024 * 1024 * 1024)
	storagePercent := 0.0
	if cluster.TotalStorage > 0 {
		storagePercent = float64(cluster.UsedStorage) / float64(cluster.TotalStorage) * 100
	}

	// Calculate RAM in GiB
	totalRAMGiB := float64(cluster.TotalRAM) / (1024 * 1024 * 1024)
	usedRAM := int64(0)
	for _, node := range cluster.Nodes {
		usedRAM += node.UsedMem
	}
	usedRAMGiB := float64(usedRAM) / (1024 * 1024 * 1024)
	ramPercent := 0.0
	if cluster.TotalRAM > 0 {
		ramPercent = float64(usedRAM) / float64(cluster.TotalRAM) * 100
	}

	// Calculate average CPU
	avgCPU := 0.0
	if len(cluster.Nodes) > 0 {
		totalCPU := 0.0
		for _, node := range cluster.Nodes {
			totalCPU += node.CPUUsage
		}
		avgCPU = (totalCPU / float64(len(cluster.Nodes))) * 100
	}

	// Fixed column widths
	col1Width := 34
	col2Width := 30

	// Row 1: Nodes, CPU, vCPUs
	nodesStr := fmt.Sprintf("%d/%d online", onlineNodes, len(cluster.Nodes))
	col1Content := fmt.Sprintf("Nodes: %s", nodesStr)
	sb.WriteString(labelStyle.Render("Nodes: ") + valueStyle.Render(nodesStr))
	sb.WriteString(strings.Repeat(" ", col1Width-len(col1Content)))

	cpuStr := fmt.Sprintf("%.1f%%", avgCPU)
	col2Content := fmt.Sprintf("CPU: %s", cpuStr)
	sb.WriteString(labelStyle.Render("CPU: ") + valueStyle.Render(cpuStr))
	sb.WriteString(strings.Repeat(" ", col2Width-len(col2Content)))

	sb.WriteString(labelStyle.Render("vCPUs: ") + valueStyle.Render(fmt.Sprintf("%d", cluster.TotalVCPUs)))
	sb.WriteString("\n")

	// Row 2: VMs, RAM, Storage
	col1Row2 := fmt.Sprintf("VMs:   %d ", cluster.TotalVMs) + fmt.Sprintf("(On: %d, Off: %d)", cluster.RunningVMs, cluster.StoppedVMs)
	sb.WriteString(labelStyle.Render("VMs:   ") + valueStyle.Render(fmt.Sprintf("%d ", cluster.TotalVMs)))
	sb.WriteString(dimStyle.Render("(") + runningStyle.Render(fmt.Sprintf("On: %d", cluster.RunningVMs)) + dimStyle.Render(", "))
	sb.WriteString(stoppedStyle.Render(fmt.Sprintf("Off: %d", cluster.StoppedVMs)) + dimStyle.Render(")"))
	if len(col1Row2) < col1Width {
		sb.WriteString(strings.Repeat(" ", col1Width-len(col1Row2)))
	}

	ramValStr := fmt.Sprintf("%.0f/%.0f GiB", usedRAMGiB, totalRAMGiB)
	ramPctStr := fmt.Sprintf("(%.1f%%)", ramPercent)
	ramFull := fmt.Sprintf("RAM: %s %s", ramValStr, ramPctStr)
	sb.WriteString(labelStyle.Render("RAM: ") + valueStyle.Render(ramValStr) + " " + valueStyle.Render(ramPctStr))
	if len(ramFull) < col2Width {
		sb.WriteString(strings.Repeat(" ", col2Width-len(ramFull)))
	}

	sb.WriteString(labelStyle.Render("Storage: ") + valueStyle.Render(fmt.Sprintf("%.0f/%.0f TiB", usedStorageTiB, totalStorageTiB)))
	sb.WriteString(" " + valueStyle.Render(fmt.Sprintf("(%.1f%%)", storagePercent)))
	sb.WriteString("\n")

	return sb.String()
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
