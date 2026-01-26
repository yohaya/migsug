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

	// Migration summary (displayed above the table)
	sb.WriteString(components.RenderMigrationSummary(
		result.TotalVMs,
		result.TotalVCPUs,
		result.TotalRAM,
		result.TotalStorage,
		result.ImprovementInfo,
	))
	sb.WriteString("\n\n")

	// Calculate visible rows based on terminal height and number of target nodes
	maxVisible := calculateVisibleRowsWithTargets(height, activeTargets)

	// Suggestions table with scrolling (includes closing dashes)
	sb.WriteString(components.RenderSuggestionTableWithCursor(result.Suggestions, scrollPos, maxVisible, cursorPos))

	// Show scroll info after the closing dashes if there are more items than visible
	if len(result.Suggestions) > maxVisible {
		scrollInfo := fmt.Sprintf("Showing %d-%d of %d",
			scrollPos+1,
			min(scrollPos+maxVisible, len(result.Suggestions)),
			len(result.Suggestions))
		// Right-align so last digit is below last "-" of the table
		// Table width = 2 (prefix) + 104 (totalWidth) = 106 characters for the dashes
		tableWidth := 106
		padding := tableWidth - len(scrollInfo)
		if padding > 0 {
			scrollInfo = strings.Repeat(" ", padding) + scrollInfo
		}
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(scrollInfo) + "\n")
	}
	sb.WriteString("\n")

	// Combined impact table (source + targets) - title in regular white
	sb.WriteString("Migration Impact:\n\n")
	sb.WriteString(components.RenderImpactTable(
		result.SourceBefore,
		result.SourceAfter,
		result.TargetsBefore,
		result.TargetsAfter,
	))

	// Help text
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	sb.WriteString("\n" + helpStyle.Render("↑/↓/PgUp/PgDn/Home/End: Navigate  r: New Analysis  Esc: Back  q: Quit"))

	return sb.String()
}

// RenderResultsInteractive renders the results view with section focus and impact cursor support
func RenderResultsInteractive(result *analyzer.AnalysisResult, cluster *proxmox.Cluster, sourceNode *proxmox.Node, version string, width, height, scrollPos, cursorPos, focusedSection, impactCursor int) string {
	var sb strings.Builder

	if width < 80 {
		width = 100
	}

	// Count active targets
	activeTargets := 0
	for targetName, afterState := range result.TargetsAfter {
		beforeState := result.TargetsBefore[targetName]
		if afterState.VMCount != beforeState.VMCount {
			activeTargets++
		}
	}

	// Title with version
	versionStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	borderStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("6"))

	title := "KVM Migration Suggester"
	if version != "" && version != "dev" {
		title += " " + versionStyle.Render("v"+version)
	}
	sb.WriteString(resultsTitleStyle.Render(title) + "\n")
	sb.WriteString(borderStyle.Render(strings.Repeat("━", width)) + "\n\n")

	// Cluster summary
	if cluster != nil {
		sb.WriteString(renderResultsClusterSummary(cluster, width))
		sb.WriteString("\n")
	}

	// Source node info
	if sourceNode != nil {
		sb.WriteString(renderSourceNodeSummary(sourceNode, width))
		sb.WriteString("\n")
	}

	// No suggestions case
	if len(result.Suggestions) == 0 {
		errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true)
		sb.WriteString(errorStyle.Render("No migration suggestions generated.") + "\n")
		sb.WriteString("This might mean:\n")
		sb.WriteString("  • No VMs match the criteria\n")
		sb.WriteString("  • No target nodes have sufficient capacity\n")
		sb.WriteString("  • All target nodes are excluded\n\n")
		return sb.String()
	}

	// Migration summary with section indicator (always show ▶)
	summaryTitle := "▶ Migration Summary:"
	sb.WriteString(summaryTitle + "\n")
	sb.WriteString(components.RenderMigrationSummaryContent(
		result.TotalVMs,
		result.TotalVCPUs,
		result.TotalRAM,
		result.TotalStorage,
	))
	sb.WriteString("\n\n")

	// Calculate visible rows
	maxVisible := calculateVisibleRowsWithTargets(height, activeTargets)

	// Suggestions table
	if focusedSection == 0 {
		sb.WriteString(components.RenderSuggestionTableWithCursor(result.Suggestions, scrollPos, maxVisible, cursorPos))
	} else {
		sb.WriteString(components.RenderSuggestionTableWithCursor(result.Suggestions, scrollPos, maxVisible, -1))
	}

	// Scroll info
	if len(result.Suggestions) > maxVisible {
		scrollInfo := fmt.Sprintf("Showing %d-%d of %d",
			scrollPos+1,
			min(scrollPos+maxVisible, len(result.Suggestions)),
			len(result.Suggestions))
		tableWidth := 106
		padding := tableWidth - len(scrollInfo)
		if padding > 0 {
			scrollInfo = strings.Repeat(" ", padding) + scrollInfo
		}
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(scrollInfo) + "\n")
	}
	sb.WriteString("\n")

	// Impact table with section indicator (always show ▶)
	impactTitle := "▶ Migration Impact:"
	sb.WriteString(impactTitle + "\n\n")

	if focusedSection == 1 {
		sb.WriteString(components.RenderImpactTableWithCursor(
			result.SourceBefore,
			result.SourceAfter,
			result.TargetsBefore,
			result.TargetsAfter,
			impactCursor,
		))
	} else {
		sb.WriteString(components.RenderImpactTable(
			result.SourceBefore,
			result.SourceAfter,
			result.TargetsBefore,
			result.TargetsAfter,
		))
	}

	// Help text with TAB instruction
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	sb.WriteString("\n" + helpStyle.Render("Tab: Switch section  ↑/↓: Navigate  Enter: View details  r: New Analysis  Esc: Back  q: Quit"))

	return sb.String()
}

// RenderHostDetail renders the detail view for a selected host showing VMs added/removed (legacy)
func RenderHostDetail(result *analyzer.AnalysisResult, hostName, sourceNodeName string, width, height int) string {
	return RenderHostDetailInteractive(result, hostName, sourceNodeName, width, height, 0, 0)
}

// RenderHostDetailInteractive renders the detail view with two scrollable tables and Tab switching
func RenderHostDetailInteractive(result *analyzer.AnalysisResult, hostName, sourceNodeName string, width, height, focusedSection, scrollPos int) string {
	var sb strings.Builder

	if width < 80 {
		width = 100
	}

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("5"))
	borderStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	labelStyle := lipgloss.NewStyle()
	valueStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))

	// Title
	sb.WriteString(titleStyle.Render("Host Detail: "+hostName) + "\n")
	sb.WriteString(borderStyle.Render(strings.Repeat("━", width)) + "\n\n")

	// Check if this is the source node
	isSource := (hostName == sourceNodeName || hostName == result.SourceBefore.Name)

	// Get before/after state
	var beforeState, afterState analyzer.NodeState
	if isSource {
		beforeState = result.SourceBefore
		afterState = result.SourceAfter
	} else {
		beforeState = result.TargetsBefore[hostName]
		afterState = result.TargetsAfter[hostName]
	}

	// Show before/after summary
	sb.WriteString(labelStyle.Render("Before: ") +
		valueStyle.Render(fmt.Sprintf("VMs: %d, vCPUs: %d, CPU: %.1f%%, RAM: %.1f%%",
			beforeState.VMCount, beforeState.VCPUs, beforeState.CPUPercent, beforeState.RAMPercent)) + "\n")
	sb.WriteString(labelStyle.Render("After:  ") +
		valueStyle.Render(fmt.Sprintf("VMs: %d, vCPUs: %d, CPU: %.1f%%, RAM: %.1f%%",
			afterState.VMCount, afterState.VCPUs, afterState.CPUPercent, afterState.RAMPercent)) + "\n\n")

	// Collect VMs for before/after
	var beforeVMs, afterVMs []analyzer.MigrationSuggestion

	for _, sug := range result.Suggestions {
		if isSource && sug.SourceNode == sourceNodeName {
			// Source node: before has VMs, after is empty
			beforeVMs = append(beforeVMs, sug)
		} else if !isSource && sug.TargetNode == hostName {
			// Target node: before is empty, after has VMs
			afterVMs = append(afterVMs, sug)
		}
	}

	// Calculate max visible rows for each table
	maxVisible := (height - 20) / 2
	if maxVisible < 3 {
		maxVisible = 3
	}

	// Render Before VMs table
	beforeTitle := "Before VMs"
	if focusedSection == 0 {
		beforeTitle = "▶ Before VMs"
	}

	if isSource {
		sb.WriteString(fmt.Sprintf("%s (%d):\n", beforeTitle, len(beforeVMs)))
		sb.WriteString(headerStyle.Render(fmt.Sprintf("  %6s  %-24s  %-20s  %6s  %8s  %8s",
			"VMID", "Name", "Target", "vCPUs", "RAM", "Storage")) + "\n")
		sb.WriteString("  " + strings.Repeat("─", 90) + "\n")

		// Apply scrolling for before section
		startIdx := 0
		if focusedSection == 0 {
			startIdx = scrollPos
		}
		endIdx := startIdx + maxVisible
		if endIdx > len(beforeVMs) {
			endIdx = len(beforeVMs)
		}

		for i := startIdx; i < endIdx; i++ {
			vm := beforeVMs[i]
			sb.WriteString(fmt.Sprintf("  %6d  %-24s  %-20s  %6d  %8s  %8s\n",
				vm.VMID,
				truncateString(vm.VMName, 24),
				truncateString(vm.TargetNode, 20),
				vm.VCPUs,
				components.FormatBytesShort(vm.RAM),
				components.FormatBytesShort(vm.Storage)))
		}

		// Show scroll info if needed
		if len(beforeVMs) > maxVisible && focusedSection == 0 {
			scrollInfo := fmt.Sprintf("Showing %d-%d of %d", startIdx+1, endIdx, len(beforeVMs))
			sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("  "+scrollInfo) + "\n")
		}
	} else {
		sb.WriteString(fmt.Sprintf("%s (0):\n", beforeTitle))
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("  (no VMs on this host before migration)") + "\n")
	}
	sb.WriteString("\n")

	// Render After VMs table
	afterTitle := "After VMs"
	if focusedSection == 1 {
		afterTitle = "▶ After VMs"
	}

	if isSource {
		sb.WriteString(fmt.Sprintf("%s (0):\n", afterTitle))
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("  (all VMs migrated away)") + "\n")
	} else {
		sb.WriteString(fmt.Sprintf("%s (%d):\n", afterTitle, len(afterVMs)))
		sb.WriteString(headerStyle.Render(fmt.Sprintf("  %6s  %-24s  %-20s  %6s  %8s  %8s",
			"VMID", "Name", "From", "vCPUs", "RAM", "Storage")) + "\n")
		sb.WriteString("  " + strings.Repeat("─", 90) + "\n")

		// Apply scrolling for after section
		startIdx := 0
		if focusedSection == 1 {
			startIdx = scrollPos
		}
		endIdx := startIdx + maxVisible
		if endIdx > len(afterVMs) {
			endIdx = len(afterVMs)
		}

		for i := startIdx; i < endIdx; i++ {
			vm := afterVMs[i]
			sb.WriteString(fmt.Sprintf("  %6d  %-24s  %-20s  %6d  %8s  %8s\n",
				vm.VMID,
				truncateString(vm.VMName, 24),
				truncateString(vm.SourceNode, 20),
				vm.VCPUs,
				components.FormatBytesShort(vm.RAM),
				components.FormatBytesShort(vm.Storage)))
		}

		// Show scroll info if needed
		if len(afterVMs) > maxVisible && focusedSection == 1 {
			scrollInfo := fmt.Sprintf("Showing %d-%d of %d", startIdx+1, endIdx, len(afterVMs))
			sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("  "+scrollInfo) + "\n")
		}
	}
	sb.WriteString("\n")

	// Help text
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	sb.WriteString(helpStyle.Render("Tab: Switch section  ↑/↓/PgUp/PgDn: Scroll  Esc: Back"))

	return sb.String()
}

// VMListItem represents a VM in the host detail view
type VMListItem struct {
	VMID      int
	Name      string
	Status    string  // "running" or "stopped"
	CPUUsage  float64 // CPU usage percentage
	VCPUs     int
	RAM       int64
	Storage   int64
	Direction string // "←" for out, "→" for in, "" for staying
	Target    string // Target/Source node for migration
}

// RenderHostDetailBrowseable renders a browseable list of all VMs on a host
func RenderHostDetailBrowseable(result *analyzer.AnalysisResult, cluster *proxmox.Cluster, hostName, sourceNodeName string, width, height, scrollPos, cursorPos int) string {
	var sb strings.Builder

	if width < 80 {
		width = 100
	}

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("5"))
	borderStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	labelStyle := lipgloss.NewStyle()
	valueStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))
	selectedStyle := lipgloss.NewStyle().Background(lipgloss.Color("236")).Foreground(lipgloss.Color("15")).Bold(true)
	arrowOutStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("1"))  // Red for out
	arrowInStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("2"))   // Green for in

	// Title
	sb.WriteString(titleStyle.Render("Host Detail: "+hostName) + "\n")
	sb.WriteString(borderStyle.Render(strings.Repeat("━", width)) + "\n\n")

	// Check if this is the source node
	isSource := (hostName == sourceNodeName || hostName == result.SourceBefore.Name)

	// Get before/after state
	var beforeState, afterState analyzer.NodeState
	if isSource {
		beforeState = result.SourceBefore
		afterState = result.SourceAfter
	} else {
		beforeState = result.TargetsBefore[hostName]
		afterState = result.TargetsAfter[hostName]
	}

	// Show before/after summary with storage
	sb.WriteString(labelStyle.Render("Before: ") +
		valueStyle.Render(fmt.Sprintf("VMs: %d, vCPUs: %d, CPU: %.1f%%, RAM: %s, Storage: %s",
			beforeState.VMCount, beforeState.VCPUs, beforeState.CPUPercent,
			components.FormatBytesShort(beforeState.RAMUsed),
			components.FormatBytesShort(beforeState.StorageUsed))) + "\n")
	sb.WriteString(labelStyle.Render("After:  ") +
		valueStyle.Render(fmt.Sprintf("VMs: %d, vCPUs: %d, CPU: %.1f%%, RAM: %s, Storage: %s",
			afterState.VMCount, afterState.VCPUs, afterState.CPUPercent,
			components.FormatBytesShort(afterState.RAMUsed),
			components.FormatBytesShort(afterState.StorageUsed))) + "\n\n")

	// Build VM list
	var vmList []VMListItem

	// Create a map of VMs being migrated for quick lookup
	migratingVMs := make(map[int]analyzer.MigrationSuggestion)
	for _, sug := range result.Suggestions {
		migratingVMs[sug.VMID] = sug
	}

	if isSource {
		// Source node: show all VMs from the node
		sourceNode := proxmox.GetNodeByName(cluster, sourceNodeName)
		if sourceNode != nil {
			for _, vm := range sourceNode.VMs {
				item := VMListItem{
					VMID:     vm.VMID,
					Name:     vm.Name,
					Status:   vm.Status,
					CPUUsage: vm.CPUUsage,
					VCPUs:    vm.CPUCores,
					RAM:      vm.MaxMem,
					Storage:  vm.MaxDisk,
				}
				if vm.MaxDisk == 0 {
					item.Storage = vm.UsedDisk
				}

				// Check if this VM is being migrated
				if sug, ok := migratingVMs[vm.VMID]; ok && sug.SourceNode == sourceNodeName {
					item.Direction = "←"
					item.Target = sug.TargetNode
				}

				vmList = append(vmList, item)
			}
		}
	} else {
		// Target node: show existing VMs + VMs being migrated in
		targetNode := proxmox.GetNodeByName(cluster, hostName)
		if targetNode != nil {
			for _, vm := range targetNode.VMs {
				item := VMListItem{
					VMID:     vm.VMID,
					Name:     vm.Name,
					Status:   vm.Status,
					CPUUsage: vm.CPUUsage,
					VCPUs:    vm.CPUCores,
					RAM:      vm.MaxMem,
					Storage:  vm.MaxDisk,
				}
				if vm.MaxDisk == 0 {
					item.Storage = vm.UsedDisk
				}
				vmList = append(vmList, item)
			}
		}

		// Add VMs being migrated in
		for _, sug := range result.Suggestions {
			if sug.TargetNode == hostName {
				item := VMListItem{
					VMID:      sug.VMID,
					Name:      sug.VMName,
					Status:    sug.Status,
					CPUUsage:  sug.CPUUsage,
					VCPUs:     sug.VCPUs,
					RAM:       sug.RAM,
					Storage:   sug.Storage,
					Direction: "→",
					Target:    sug.SourceNode,
				}
				vmList = append(vmList, item)
			}
		}
	}

	// Calculate max visible rows
	maxVisible := height - 15
	if maxVisible < 3 {
		maxVisible = 3
	}

	// Column widths
	const (
		colDir     = 2  // Arrow direction
		colVMID    = 6
		colName    = 24
		colState   = 5
		colCPU     = 6  // CPU%
		colVCPU    = 5
		colRAM     = 8
		colStorage = 8
		colTarget  = 20
	)
	totalWidth := colDir + colVMID + colName + colState + colCPU + colVCPU + colRAM + colStorage + colTarget + 8

	// Header
	header := fmt.Sprintf("  %*s %*s %-*s %-*s %*s %*s %*s %*s %-*s",
		colDir, "",
		colVMID, "VMID",
		colName, "Name",
		colState, "State",
		colCPU, "CPU%",
		colVCPU, "vCPU",
		colRAM, "RAM",
		colStorage, "Storage",
		colTarget, "Migration")
	sb.WriteString(headerStyle.Render(header) + "\n")
	sb.WriteString("  " + strings.Repeat("─", totalWidth) + "\n")

	// Calculate visible range
	endPos := scrollPos + maxVisible
	if endPos > len(vmList) {
		endPos = len(vmList)
	}

	// Render VM rows
	for i := scrollPos; i < endPos; i++ {
		vm := vmList[i]
		isSelected := (i == cursorPos)

		// State string
		stateStr := "Off"
		if vm.Status == "running" {
			stateStr = "On"
		}

		// Direction arrow with color
		dirStr := "  "
		if vm.Direction == "←" {
			dirStr = arrowOutStyle.Render("← ")
		} else if vm.Direction == "→" {
			dirStr = arrowInStyle.Render("→ ")
		}

		// Migration target/source info
		migrationStr := ""
		if vm.Direction == "←" {
			migrationStr = "→ " + vm.Target
		} else if vm.Direction == "→" {
			migrationStr = "← " + vm.Target
		}

		// CPU% string
		cpuStr := fmt.Sprintf("%.1f", vm.CPUUsage)

		// Build row content
		rowContent := fmt.Sprintf("%*d %-*s %-*s %*s %*d %*s %*s %-*s",
			colVMID, vm.VMID,
			colName, truncateString(vm.Name, colName),
			colState, stateStr,
			colCPU, cpuStr,
			colVCPU, vm.VCPUs,
			colRAM, components.FormatBytesShort(vm.RAM),
			colStorage, components.FormatBytesShort(vm.Storage),
			colTarget, truncateString(migrationStr, colTarget))

		// Selector prefix
		selector := "  "
		if isSelected {
			selector = "▶ "
		}

		if isSelected {
			// Pad row for consistent highlighting
			if len(rowContent) < totalWidth {
				rowContent += strings.Repeat(" ", totalWidth-len(rowContent))
			}
			sb.WriteString(selector + dirStr + selectedStyle.Render(rowContent) + "\n")
		} else {
			sb.WriteString(selector + dirStr + rowContent + "\n")
		}
	}

	// Closing line
	sb.WriteString("  " + strings.Repeat("─", totalWidth) + "\n")

	// Scroll info
	if len(vmList) > maxVisible {
		scrollInfo := fmt.Sprintf("Showing %d-%d of %d", scrollPos+1, endPos, len(vmList))
		padding := totalWidth + 2 - len(scrollInfo)
		if padding > 0 {
			scrollInfo = strings.Repeat(" ", padding) + scrollInfo
		}
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(scrollInfo) + "\n")
	}

	// Legend
	sb.WriteString("\n")
	legendStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	sb.WriteString(legendStyle.Render("  ← Migrating out   → Migrating in") + "\n")

	// Help text
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	sb.WriteString("\n" + helpStyle.Render("↑/↓/PgUp/PgDn/Home/End: Navigate  Esc: Back  q: Quit"))

	return sb.String()
}

// truncateString truncates a string to maxLen characters
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-1] + "…"
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
	// - Migration summary + 2 blanks: 3 lines
	// - Suggestions table header + separator: 2 lines
	// - Suggestions table closing dashes: 1 line
	// - Scroll info (below table): 1 line
	// - Migration Impact header + blank: 2 lines
	// - Impact table (header1 + header2 + sep + source + closing): 5 lines
	// - Each target node: 1 line each
	// - Help text + buffer: 3 lines (extra 1 for safety)

	fixedOverhead := 3 + 3 + 4 + 3 + 2 + 1 + 1 + 2 + 5 + 3 // = 27 lines
	targetLines := numTargets * 1                          // Each target takes 1 line in combined table

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
	return RenderVMSelectionWithHeight(vms, selectedVMs, cursorIdx, width, 24)
}

// RenderVMSelectionWithHeight renders the VM selection view with height limit
func RenderVMSelectionWithHeight(vms []proxmox.VM, selectedVMs map[int]bool, cursorIdx int, width, height int) string {
	var sb strings.Builder

	// Title
	sb.WriteString(resultsTitleStyle.Render("Select VMs to Migrate") + "\n\n")

	// Instructions
	instructionStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	sb.WriteString(instructionStyle.Render(
		fmt.Sprintf("Selected: %d VMs - Use Space to toggle, Enter to confirm", len(selectedVMs))) + "\n\n")

	// Calculate visible rows based on height
	// Fixed overhead: Title+blank(2) + Instructions+blank(2) + Table header+sep(2) + Help(1) + scroll info(1)
	fixedOverhead := 8
	maxVisible := height - fixedOverhead
	if maxVisible < 3 {
		maxVisible = 3
	}

	// VM table with scroll support
	sb.WriteString(components.RenderVMTableWithScroll(vms, selectedVMs, cursorIdx, maxVisible))

	// Show scroll info if there are more VMs than visible
	if len(vms) > maxVisible {
		scrollPos := 0
		if cursorIdx >= maxVisible {
			scrollPos = cursorIdx - maxVisible + 1
		}
		endPos := scrollPos + maxVisible
		if endPos > len(vms) {
			endPos = len(vms)
		}
		scrollInfo := fmt.Sprintf("(showing %d-%d of %d VMs)",
			scrollPos+1, endPos, len(vms))
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(scrollInfo) + "\n")
	}
	sb.WriteString("\n")

	// Help text
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	sb.WriteString(helpStyle.Render("↑/↓: Navigate  Space: Toggle  Enter: Confirm  Esc: Back"))

	return sb.String()
}
