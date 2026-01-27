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
		// Table width = 2 (prefix) + 110 (totalWidth) + 2 (scrollbar space) = 114 characters
		tableWidth := 114
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
		// Table width = 2 (prefix) + 110 (totalWidth) + 2 (scrollbar space) = 114 characters
		tableWidth := 114
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
				components.FormatRAMShort(vm.RAM),
				components.FormatStorageG(vm.Storage)))
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
				components.FormatRAMShort(vm.RAM),
				components.FormatStorageG(vm.Storage)))
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

	// Migration details (only set for migrating VMs)
	Details *analyzer.MigrationDetails
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
	arrowOutStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("1")) // Red for out
	arrowInStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("2"))  // Green for in

	// Title - ensure it's always rendered at the top
	sb.WriteString(titleStyle.Render("Host Detail: "+hostName) + "\n")
	sb.WriteString(borderStyle.Render(strings.Repeat("━", width)) + "\n")

	// Check if this is the source node
	isSource := (hostName == sourceNodeName || hostName == result.SourceBefore.Name)

	// Get the node from cluster to show CPU info
	node := proxmox.GetNodeByName(cluster, hostName)

	// Show CPU model and threads
	if node != nil {
		cpuInfoStr := ""
		if node.CPUModel != "" {
			cpuInfoStr = fmt.Sprintf("%s, %d threads", node.CPUModel, node.CPUCores)
		} else {
			cpuInfoStr = fmt.Sprintf("%d threads", node.CPUCores)
		}
		sb.WriteString(labelStyle.Render("CPU:    ") + valueStyle.Render(cpuInfoStr) + "\n")
	}

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
			components.FormatRAMShort(beforeState.RAMUsed),
			components.FormatBytesShort(beforeState.StorageUsed))) + "\n")
	sb.WriteString(labelStyle.Render("After:  ") +
		valueStyle.Render(fmt.Sprintf("VMs: %d, vCPUs: %d, CPU: %.1f%%, RAM: %s, Storage: %s",
			afterState.VMCount, afterState.VCPUs, afterState.CPUPercent,
			components.FormatRAMShort(afterState.RAMUsed),
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
					item.Details = sug.Details
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
					Details:   sug.Details,
				}
				vmList = append(vmList, item)
			}
		}
	}

	// Calculate max visible rows with fixed reservation for reasoning panel
	// Fixed overhead:
	// - Title + border: 2 lines
	// - CPU info: 1 line
	// - Before/After summary: 2 lines + 1 blank
	// - Table header + separator: 2 lines
	// - Table closing line: 1 line
	// - Scroll info: 1 line
	// - Blank before reasoning: 1 line
	// - Reasoning panel (fixed): 26 lines (max height including all sections)
	// - Help text: 1 line
	// Total: 38 lines
	fixedOverhead := 38
	maxVisible := height - fixedOverhead
	if maxVisible < 5 {
		maxVisible = 5
	}

	// Column widths - increased for full hostnames
	const (
		colDir     = 2
		colVMID    = 6
		colName    = 28
		colState   = 5
		colHCPU    = 6
		colVMCPU   = 7
		colCPU     = 6
		colVCPU    = 5
		colRAM     = 8
		colStorage = 8
		colTarget  = 24
	)
	totalWidth := colDir + colVMID + colName + colState + colHCPU + colVMCPU + colCPU + colVCPU + colRAM + colStorage + colTarget + 10

	// Scrollbar styles
	scrollTrackStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	scrollThumbStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("6"))

	totalItems := len(vmList)
	needsScrollbar := totalItems > maxVisible

	// Ensure scrollPos is within valid range (don't modify - let the key handler manage this)
	if scrollPos < 0 {
		scrollPos = 0
	}
	maxScroll := totalItems - maxVisible
	if maxScroll < 0 {
		maxScroll = 0
	}
	if scrollPos > maxScroll {
		scrollPos = maxScroll
	}

	// Calculate scrollbar thumb position and size
	thumbPos := 0
	thumbSize := maxVisible
	if needsScrollbar && totalItems > 0 {
		thumbSize = max(1, maxVisible*maxVisible/totalItems)
		if thumbSize > maxVisible {
			thumbSize = maxVisible
		}
		scrollRange := maxVisible - thumbSize
		if scrollRange > 0 && totalItems > maxVisible {
			thumbPos = scrollPos * scrollRange / (totalItems - maxVisible)
		}
	}

	// Header - aligned with data columns (no extra space between dir and VMID)
	header := fmt.Sprintf("  %*s%*s %-*s %-*s %*s %*s %*s %*s %*s %*s %-*s",
		colDir, "",
		colVMID, "VMID",
		colName, "Name",
		colState, "State",
		colHCPU, "HCPU%",
		colVMCPU, "VMCPU%",
		colCPU, "CPU%",
		colVCPU, "vCPU",
		colRAM, "RAM",
		colStorage, "Storage",
		colTarget, "Migration")
	// Pad header to totalWidth for alignment
	headerPadded := header
	if len(header) < totalWidth+4 {
		headerPadded = header + strings.Repeat(" ", totalWidth+4-len(header))
	}
	sb.WriteString(headerStyle.Render(headerPadded) + "\n")
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

		// VMCPU%: percentage of allocated vCPUs (already in 0-100)
		vmCpuStr := fmt.Sprintf("%.1f", vm.CPUUsage)

		// CPU%: total thread consumption = VMCPU% * vCPUs
		// e.g., if VMCPU% is 10% and VM has 16 vCPUs, CPU% = 160 (1.6 threads worth)
		cpuPercent := vm.CPUUsage * float64(vm.VCPUs)
		cpuStr := fmt.Sprintf("%.0f", cpuPercent)

		// HCPU%: host CPU percentage contribution
		// = VMCPU% * vCPUs / hostCores (actual % of this host's capacity the VM uses)
		hCpuPercent := 0.0
		if node != nil && node.CPUCores > 0 {
			hCpuPercent = vm.CPUUsage * float64(vm.VCPUs) / float64(node.CPUCores)
		}
		hCpuStr := fmt.Sprintf("%.1f", hCpuPercent)

		// Truncate name to fit column width
		displayName := vm.Name
		if len(displayName) > colName {
			displayName = displayName[:colName-1] + "…"
		}

		// Truncate migration string to fit column width
		displayMigration := migrationStr
		if len(displayMigration) > colTarget {
			displayMigration = displayMigration[:colTarget-1] + "…"
		}

		// Build row content with truncated names
		rowContent := fmt.Sprintf("%*d %-*s %-*s %*s %*s %*s %*d %*s %*s %-*s",
			colVMID, vm.VMID,
			colName, displayName,
			colState, stateStr,
			colHCPU, hCpuStr,
			colVMCPU, vmCpuStr,
			colCPU, cpuStr,
			colVCPU, vm.VCPUs,
			colRAM, components.FormatRAMShort(vm.RAM),
			colStorage, components.FormatStorageG(vm.Storage),
			colTarget, displayMigration)

		// Truncate if too long, pad if too short for consistent width
		rowRunes := []rune(rowContent)
		if len(rowRunes) > totalWidth {
			rowContent = string(rowRunes[:totalWidth])
		} else if len(rowRunes) < totalWidth {
			rowContent += strings.Repeat(" ", totalWidth-len(rowRunes))
		}

		// Selector prefix
		selector := "  "
		if isSelected {
			selector = "▶ "
		}

		// Build the full line first, then add scrollbar
		var line string
		if isSelected {
			line = selector + dirStr + selectedStyle.Render(rowContent)
		} else {
			line = selector + dirStr + rowContent
		}

		// Add scrollbar character at fixed position from right edge
		if needsScrollbar {
			rowIdx := i - scrollPos
			if rowIdx >= thumbPos && rowIdx < thumbPos+thumbSize {
				sb.WriteString(line + " " + scrollThumbStyle.Render("█") + "\n")
			} else {
				sb.WriteString(line + " " + scrollTrackStyle.Render("│") + "\n")
			}
		} else {
			sb.WriteString(line + "\n")
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

	// Show migration reasoning panel if selected VM is migrating
	if cursorPos >= 0 && cursorPos < len(vmList) {
		selectedVM := vmList[cursorPos]
		if selectedVM.Details != nil && selectedVM.Direction != "" {
			sb.WriteString("\n")
			sb.WriteString(renderMigrationReasoning(selectedVM, hostName))
		}
	}

	// Help text
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	sb.WriteString("\n" + helpStyle.Render("↑/↓/PgUp/PgDn/Home/End: Navigate  Esc: Back  q: Quit"))

	return sb.String()
}

// renderMigrationReasoning renders the detailed reasoning panel for a migrating VM
func renderMigrationReasoning(vm VMListItem, currentHost string) string {
	if vm.Details == nil {
		return ""
	}

	var sb strings.Builder
	details := vm.Details

	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("7")) // Light grey, similar to regular text
	goodStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("3"))

	// VM Selection Reason
	sb.WriteString(labelStyle.Render("Why selected: ") + valueStyle.Render(details.SelectionReason) + "\n\n")

	// Target Selection
	targetName := vm.Target
	if vm.Direction == "←" {
		// Migrating out - target is the destination
		targetName = vm.Target
	}

	sb.WriteString(labelStyle.Render("Why this target (") + valueStyle.Render(targetName) + labelStyle.Render("):") + "\n")

	// Score breakdown
	if details.ScoreBreakdown.TotalScore > 0 {
		sb.WriteString("  " + labelStyle.Render("Score: ") + valueStyle.Render(fmt.Sprintf("%.1f", details.ScoreBreakdown.TotalScore)) + "\n")
		if details.ScoreBreakdown.UtilizationScore > 0 {
			sb.WriteString("    " + valueStyle.Render(fmt.Sprintf("- Utilization: %.1f (weight: %.0f%%)",
				details.ScoreBreakdown.UtilizationScore, details.ScoreBreakdown.UtilizationWeight*100)) + "\n")
		}
		if details.ScoreBreakdown.BalanceScore > 0 {
			sb.WriteString("    " + valueStyle.Render(fmt.Sprintf("- Balance: %.1f (weight: %.0f%%)",
				details.ScoreBreakdown.BalanceScore, details.ScoreBreakdown.BalanceWeight*100)) + "\n")
		}
		if details.ScoreBreakdown.HeadroomScore != 0 {
			sb.WriteString("    " + valueStyle.Render(fmt.Sprintf("- Headroom: %.1f (below cluster avg)",
				details.ScoreBreakdown.HeadroomScore)) + "\n")
		}
	}
	sb.WriteString("\n")

	// Target state before/after
	sb.WriteString(labelStyle.Render("Target state change:") + "\n")
	sb.WriteString("  " + labelStyle.Render("Before: ") +
		valueStyle.Render(fmt.Sprintf("CPU: %.1f%%, RAM: %.1f%%, Storage: %.1f%%, VMs: %d",
			details.TargetBefore.CPUPercent,
			details.TargetBefore.RAMPercent,
			details.TargetBefore.StoragePercent,
			details.TargetBefore.VMCount)) + "\n")

	// Color the after values based on impact
	cpuAfterStr := fmt.Sprintf("%.1f%%", details.TargetAfter.CPUPercent)
	ramAfterStr := fmt.Sprintf("%.1f%%", details.TargetAfter.RAMPercent)
	if details.TargetAfter.CPUPercent > 80 {
		cpuAfterStr = warnStyle.Render(cpuAfterStr)
	} else {
		cpuAfterStr = goodStyle.Render(cpuAfterStr)
	}
	if details.TargetAfter.RAMPercent > 80 {
		ramAfterStr = warnStyle.Render(ramAfterStr)
	} else {
		ramAfterStr = goodStyle.Render(ramAfterStr)
	}

	sb.WriteString("  " + labelStyle.Render("After:  ") +
		labelStyle.Render("CPU: ") + cpuAfterStr +
		labelStyle.Render(", RAM: ") + ramAfterStr +
		labelStyle.Render(fmt.Sprintf(", Storage: %.1f%%, VMs: %d",
			details.TargetAfter.StoragePercent,
			details.TargetAfter.VMCount)) + "\n")

	// Cluster context (for MigrateAll mode)
	if details.ClusterAvgCPU > 0 || details.ClusterAvgRAM > 0 {
		sb.WriteString("\n" + labelStyle.Render("Cluster balance target:") + "\n")
		sb.WriteString("  " + valueStyle.Render(fmt.Sprintf("Avg CPU: %.1f%%, Avg RAM: %.1f%%",
			details.ClusterAvgCPU, details.ClusterAvgRAM)) + "\n")
		if details.BelowAverage {
			sb.WriteString("  " + goodStyle.Render("✓ Target stays below cluster average") + "\n")
		} else {
			sb.WriteString("  " + warnStyle.Render("! Target will exceed cluster average") + "\n")
		}
	}

	// Alternatives considered
	if len(details.Alternatives) > 0 {
		sb.WriteString("\n" + labelStyle.Render("Alternative targets:") + "\n")
		for _, alt := range details.Alternatives {
			if alt.Score > 0 {
				sb.WriteString("  " + valueStyle.Render(fmt.Sprintf("• %s (score: %.1f) - %s",
					alt.Name, alt.Score, alt.RejectionReason)) + "\n")
			} else {
				sb.WriteString("  " + valueStyle.Render(fmt.Sprintf("• %s - %s",
					alt.Name, alt.RejectionReason)) + "\n")
			}
		}
	}

	// Constraints applied
	if len(details.ConstraintsApplied) > 0 {
		sb.WriteString("\n" + labelStyle.Render("Constraints checked:") + "\n")
		for _, c := range details.ConstraintsApplied {
			sb.WriteString("  " + valueStyle.Render("• "+c) + "\n")
		}
	}

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
