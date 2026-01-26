package views

import (
	"fmt"
	"sort"
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
	return RenderResultsWithSource(result, cluster, nil, version, width, height, scrollPos, cursorPos)
}

// RenderResultsWithSource renders the migration results view with source node info
func RenderResultsWithSource(result *analyzer.AnalysisResult, cluster *proxmox.Cluster, sourceNode *proxmox.Node, version string, width, height, scrollPos, cursorPos int) string {
	var sb strings.Builder

	// Ensure minimum width
	if width < 80 {
		width = 100
	}

	// Count active targets (those that receive VMs)
	activeTargets := 0
	for targetName, afterState := range result.TargetsAfter {
		beforeState := result.TargetsBefore[targetName]
		if afterState.VMCount != beforeState.VMCount {
			activeTargets++
		}
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

	// Source node info (same as criteria view)
	if sourceNode != nil {
		sb.WriteString(renderSourceNodeSummary(sourceNode, width))
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
		Render("Suggested Migrations:") + "\n\n")

	// Calculate visible rows based on terminal height and number of target nodes
	maxVisible := calculateVisibleRowsWithTargets(height, activeTargets)

	sb.WriteString(components.RenderSuggestionTableWithCursor(result.Suggestions, scrollPos, maxVisible, cursorPos))

	// Show scroll info below the table if there are more items than visible
	if len(result.Suggestions) > maxVisible {
		scrollInfo := fmt.Sprintf("(showing %d-%d of %d, use ↑/↓ to scroll)",
			scrollPos+1,
			min(scrollPos+maxVisible, len(result.Suggestions)),
			len(result.Suggestions))
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(scrollInfo) + "\n")
	}
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

	// Target nodes (sorted for consistent display)
	if len(result.TargetsAfter) > 0 {
		sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6")).
			Render("Target Nodes Impact:") + "\n\n")

		// Sort target names for consistent ordering
		var targetNames []string
		for targetName := range result.TargetsAfter {
			targetNames = append(targetNames, targetName)
		}
		sort.Strings(targetNames)

		for _, targetName := range targetNames {
			afterState := result.TargetsAfter[targetName]
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
	sb.WriteString("\n" + helpStyle.Render("↑/↓/PgUp/PgDn/Home/End: Navigate  r: New Analysis  Esc: Back  q: Quit"))

	return sb.String()
}

// calculateVisibleRows calculates how many suggestion rows can fit on screen (legacy)
func calculateVisibleRows(height int) int {
	return calculateVisibleRowsWithTargets(height, 4) // Assume 4 targets for backwards compat
}

// calculateVisibleRowsWithTargets calculates visible rows accounting for target nodes
func calculateVisibleRowsWithTargets(height, numTargets int) int {
	// Fixed overhead calculation:
	// - Title + border + blank: 3 lines
	// - Cluster summary + blank: 3 lines
	// - Source node summary + blank: 4 lines
	// - Migration summary + blanks: 3 lines
	// - Suggestions header + blank: 2 lines
	// - Table header + separator: 2 lines
	// - Scroll info (below table): 1 line
	// - Source Node Impact header + blank: 2 lines
	// - Source node state + blank: 2 lines
	// - Target Nodes Impact header + blank: 2 lines
	// - Each target node state + blank: 2 lines each
	// - Help text + buffer: 3 lines (extra 1 for safety)

	fixedOverhead := 3 + 3 + 4 + 3 + 2 + 2 + 1 + 2 + 2 + 2 + 3 // = 27 lines
	targetLines := numTargets * 2                               // Each target takes 2 lines (state + blank)

	reserved := fixedOverhead + targetLines
	available := height - reserved

	if available < 3 {
		return 3
	}
	return available
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

// renderSourceNodeSummary displays the source node summary (same as criteria view)
func renderSourceNodeSummary(node *proxmox.Node, width int) string {
	var sb strings.Builder

	labelStyle := lipgloss.NewStyle() // Regular text color
	valueStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	// Node name with CPU info
	nodeInfoStr := node.Name
	if node.CPUModel != "" {
		nodeInfoStr = fmt.Sprintf("%s %s(%s, %d threads)", node.Name,
			dimStyle.Render(""),
			dimStyle.Render(node.CPUModel),
			node.CPUCores)
	} else {
		nodeInfoStr = fmt.Sprintf("%s %s(%d threads)", node.Name,
			dimStyle.Render(""),
			node.CPUCores)
	}
	sb.WriteString("Selected source node: " + valueStyle.Render(nodeInfoStr) + "\n")

	// Count running VMs
	runningVMs := 0
	stoppedVMs := 0
	runningVCPUs := 0
	for _, vm := range node.VMs {
		if vm.Status == "running" {
			runningVMs++
			runningVCPUs += vm.CPUCores
		} else {
			stoppedVMs++
		}
	}

	// Fixed column widths for vertical alignment
	col1Width := 30
	col2Width := 24

	// Format values
	vmStr := fmt.Sprintf("%d (On: %d, Off: %d)", len(node.VMs), runningVMs, stoppedVMs)
	cpuStr := fmt.Sprintf("%.1f%%", node.GetCPUPercent())

	// vCPU with overcommit percentage
	vcpuOvercommit := 0.0
	if node.CPUCores > 0 {
		vcpuOvercommit = float64(runningVCPUs) / float64(node.CPUCores) * 100
	}
	vcpuStr := fmt.Sprintf("%d (%.0f%%)", runningVCPUs, vcpuOvercommit)

	// Load average with percentage
	laStr := "-"
	if len(node.LoadAverage) > 0 {
		la := node.LoadAverage[0]
		if node.CPUCores > 0 {
			laPercent := la / float64(node.CPUCores) * 100
			laStr = fmt.Sprintf("%.2f (%.1f%%)", la, laPercent)
		} else {
			laStr = fmt.Sprintf("%.2f", la)
		}
	}

	// RAM
	ramUsedGiB := float64(node.UsedMem) / (1024 * 1024 * 1024)
	ramTotalGiB := float64(node.MaxMem) / (1024 * 1024 * 1024)
	ramStr := fmt.Sprintf("%.0f/%.0fG (%.0f%%)", ramUsedGiB, ramTotalGiB, node.GetMemPercent())

	// Disk
	diskUsedTiB := float64(node.UsedDisk) / (1024 * 1024 * 1024 * 1024)
	diskTotalTiB := float64(node.MaxDisk) / (1024 * 1024 * 1024 * 1024)
	diskStr := fmt.Sprintf("%.0f/%.0fT (%.0f%%)", diskUsedTiB, diskTotalTiB, node.GetDiskPercent())

	// Row 1: VMs, CPU, vCPUs
	sb.WriteString("  ")
	col1Row1 := fmt.Sprintf("VMs: %s", vmStr)
	sb.WriteString(labelStyle.Render("VMs: ") + valueStyle.Render(vmStr))
	if len(col1Row1) < col1Width {
		sb.WriteString(strings.Repeat(" ", col1Width-len(col1Row1)))
	}

	col2Row1 := fmt.Sprintf("CPU: %s", cpuStr)
	sb.WriteString(labelStyle.Render("CPU: ") + valueStyle.Render(cpuStr))
	if len(col2Row1) < col2Width {
		sb.WriteString(strings.Repeat(" ", col2Width-len(col2Row1)))
	}

	sb.WriteString(labelStyle.Render("vCPUs: ") + valueStyle.Render(vcpuStr))
	sb.WriteString("\n")

	// Row 2: LA, RAM, Disk
	sb.WriteString("  ")
	col1Row2 := fmt.Sprintf("LA: %s", laStr)
	sb.WriteString(labelStyle.Render("LA: ") + valueStyle.Render(laStr))
	if len(col1Row2) < col1Width {
		sb.WriteString(strings.Repeat(" ", col1Width-len(col1Row2)))
	}

	col2Row2 := fmt.Sprintf("RAM: %s", ramStr)
	sb.WriteString(labelStyle.Render("RAM: ") + valueStyle.Render(ramStr))
	if len(col2Row2) < col2Width {
		sb.WriteString(strings.Repeat(" ", col2Width-len(col2Row2)))
	}

	sb.WriteString(labelStyle.Render("Disk: ") + valueStyle.Render(diskStr))
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
