package views

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/yourusername/migsug/internal/proxmox"
	"github.com/yourusername/migsug/internal/ui/components"
)

// Box drawing characters for a more graphical look
const (
	boxHorizontal    = "━"
	boxVertical      = "│"
	boxTopLeft       = "┏"
	boxTopRight      = "┓"
	boxBottomLeft    = "┗"
	boxBottomRight   = "┛"
	boxHorizontalTee = "┳"
	boxThinHoriz     = "─"
	boxDoubleLine    = "═"
)

// RenderDashboard renders the main dashboard view (without refresh info)
func RenderDashboard(cluster *proxmox.Cluster, selectedIdx int, width int) string {
	return RenderDashboardFull(cluster, selectedIdx, width, 0, false, "")
}

// RenderDashboardWithRefresh renders the main dashboard view with refresh countdown
func RenderDashboardWithRefresh(cluster *proxmox.Cluster, selectedIdx int, width int, countdown int, refreshing bool) string {
	return RenderDashboardFull(cluster, selectedIdx, width, countdown, refreshing, "")
}

// RenderDashboardWithHeight renders the main dashboard view with height limit
func RenderDashboardWithHeight(cluster *proxmox.Cluster, selectedIdx int, width, height int, countdown int, refreshing bool, version string, progress RefreshProgress, sortInfo SortInfo) string {
	var sb strings.Builder

	// Ensure minimum width
	if width < 80 {
		width = 100
	}

	// Title with version
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("5"))
	versionStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	title := "KVM Migration Suggester"
	if version != "" && version != "dev" {
		title += " " + versionStyle.Render("v"+version)
	}
	sb.WriteString(titleStyle.Render(title) + "\n")

	// Graphical top border
	borderStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	sb.WriteString(borderStyle.Render(strings.Repeat(boxHorizontal, width)) + "\n\n")

	// Cluster summary with enhanced info
	sb.WriteString(renderEnhancedClusterSummary(cluster, width))
	sb.WriteString("\n")

	// Instructions
	sb.WriteString("Select source node to migrate from:\n\n")

	// Calculate visible rows for nodes based on terminal height
	// Fixed overhead:
	// - Title + border + blank: 3 lines
	// - Cluster summary (2 rows): 2 lines
	// - Blank: 1 line
	// - Instructions + blank: 2 lines
	// - Table header + separator: 2 lines
	// - Scroll info (if scrolling): 1 line
	// - Separator: 1 line
	// - Refresh status: 1 line
	// - Status flags legend: 1 line
	// - Help text: 1 line
	// Total: 15 lines
	fixedOverhead := 15
	maxVisibleNodes := height - fixedOverhead
	if maxVisibleNodes < 3 {
		maxVisibleNodes = 3
	}

	// Node table with width and scroll support
	compSortInfo := components.SortInfo{Column: sortInfo.Column, Ascending: sortInfo.Ascending}
	sb.WriteString(components.RenderNodeTableWideWithScroll(cluster.Nodes, selectedIdx, width, compSortInfo, maxVisibleNodes))

	// Graphical separator
	sb.WriteString(borderStyle.Render(strings.Repeat(boxThinHoriz, width)) + "\n")

	// Show scroll info if there are more nodes than visible (right-aligned below separator)
	if len(cluster.Nodes) > maxVisibleNodes {
		scrollPos := 0
		if selectedIdx >= maxVisibleNodes {
			scrollPos = selectedIdx - maxVisibleNodes + 1
		}
		endPos := scrollPos + maxVisibleNodes
		if endPos > len(cluster.Nodes) {
			endPos = len(cluster.Nodes)
		}
		scrollInfo := fmt.Sprintf("Showing %d-%d of %d nodes",
			scrollPos+1, endPos, len(cluster.Nodes))
		// Right-align: pad with spaces so last char aligns with end of separator
		padding := width - len(scrollInfo)
		if padding < 0 {
			padding = 0
		}
		sb.WriteString(strings.Repeat(" ", padding) + lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(scrollInfo) + "\n")
	}

	// Refresh status line
	refreshStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	if refreshing {
		if progress.Total > 0 {
			// Show progress bar
			percent := float64(progress.Current) / float64(progress.Total) * 100
			barWidth := 20
			filled := int(float64(barWidth) * float64(progress.Current) / float64(progress.Total))
			bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
			sb.WriteString(refreshStyle.Render(fmt.Sprintf("⟳ %s: [%s] %d/%d (%.0f%%)", progress.Stage, bar, progress.Current, progress.Total, percent)) + "\n")
		} else if progress.Stage != "" {
			sb.WriteString(refreshStyle.Render(fmt.Sprintf("⟳ %s...", progress.Stage)) + "\n")
		} else {
			sb.WriteString(refreshStyle.Render("⟳ Refreshing cluster data...") + "\n")
		}
	} else if countdown > 0 {
		sb.WriteString(refreshStyle.Render(fmt.Sprintf("⟳ Auto-refresh in %ds", countdown)) + "  ")
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("(Press 'r' to refresh now)") + "\n")
	}

	// Status flags legend
	flagStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	sb.WriteString(flagStyle.Render("Status flags: O=OSD, P=Provisioning Enabled, C=Create Date 90+ days") + "\n")

	// Help text
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	sb.WriteString(helpStyle.Render("↑/↓/PgUp/PgDn/Home/End: Navigate │ 1-8: Sort columns │ Enter: Select │ B: Balance Cluster │ r: Refresh │ q: Quit"))

	return sb.String()
}

// RefreshProgress contains progress info for display
type RefreshProgress struct {
	Stage   string
	Current int
	Total   int
}

// SortInfo contains sorting information for display
type SortInfo struct {
	Column    int // 0-7 for columns 1-8
	Ascending bool
}

// RenderDashboardFull renders the main dashboard view with all options
func RenderDashboardFull(cluster *proxmox.Cluster, selectedIdx int, width int, countdown int, refreshing bool, version string) string {
	return RenderDashboardWithProgress(cluster, selectedIdx, width, countdown, refreshing, version, RefreshProgress{})
}

// RenderDashboardWithProgress renders the main dashboard view with progress info
func RenderDashboardWithProgress(cluster *proxmox.Cluster, selectedIdx int, width int, countdown int, refreshing bool, version string, progress RefreshProgress) string {
	return RenderDashboardWithSort(cluster, selectedIdx, width, countdown, refreshing, version, progress, SortInfo{Column: 0, Ascending: true})
}

// RenderDashboardWithSort renders the main dashboard view with sort info
func RenderDashboardWithSort(cluster *proxmox.Cluster, selectedIdx int, width int, countdown int, refreshing bool, version string, progress RefreshProgress, sortInfo SortInfo) string {
	var sb strings.Builder

	// Ensure minimum width
	if width < 80 {
		width = 100
	}

	// Title with version
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("5"))
	versionStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	title := "KVM Migration Suggester"
	if version != "" && version != "dev" {
		title += " " + versionStyle.Render("v"+version)
	}
	sb.WriteString(titleStyle.Render(title) + "\n")

	// Graphical top border
	borderStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	sb.WriteString(borderStyle.Render(strings.Repeat(boxHorizontal, width)) + "\n\n")

	// Cluster summary with enhanced info
	sb.WriteString(renderEnhancedClusterSummary(cluster, width))
	sb.WriteString("\n")

	// Instructions
	sb.WriteString("Select source node to migrate from:\n\n")

	// Node table with width - using the updated component that colors whole lines
	compSortInfo := components.SortInfo{Column: sortInfo.Column, Ascending: sortInfo.Ascending}
	sb.WriteString(components.RenderNodeTableWideWithSort(cluster.Nodes, selectedIdx, width, compSortInfo))
	sb.WriteString("\n")

	// Graphical separator
	sb.WriteString(borderStyle.Render(strings.Repeat(boxThinHoriz, width)) + "\n")

	// Refresh status line
	refreshStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	if refreshing {
		if progress.Total > 0 {
			// Show progress bar
			percent := float64(progress.Current) / float64(progress.Total) * 100
			barWidth := 20
			filled := int(float64(barWidth) * float64(progress.Current) / float64(progress.Total))
			bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
			sb.WriteString(refreshStyle.Render(fmt.Sprintf("⟳ %s: [%s] %d/%d (%.0f%%)", progress.Stage, bar, progress.Current, progress.Total, percent)) + "\n")
		} else if progress.Stage != "" {
			sb.WriteString(refreshStyle.Render(fmt.Sprintf("⟳ %s...", progress.Stage)) + "\n")
		} else {
			sb.WriteString(refreshStyle.Render("⟳ Refreshing cluster data...") + "\n")
		}
	} else if countdown > 0 {
		sb.WriteString(refreshStyle.Render(fmt.Sprintf("⟳ Auto-refresh in %ds", countdown)) + "  ")
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("(Press 'r' to refresh now)") + "\n")
	}

	// Status flags legend
	flagStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	sb.WriteString(flagStyle.Render("Status flags: O=OSD, P=Provisioning Enabled, C=Create Date 90+ days") + "\n")

	// Help text
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	sb.WriteString(helpStyle.Render("↑/↓/PgUp/PgDn/Home/End: Navigate │ 1-8: Sort columns │ Enter: Select │ B: Balance Cluster │ r: Refresh │ q: Quit"))

	return sb.String()
}

// renderEnhancedClusterSummary creates a rich cluster summary with all requested info
func renderEnhancedClusterSummary(cluster *proxmox.Cluster, width int) string {
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

	// Color codes for usage
	cpuColor := getUsageColorCode(avgCPU)
	ramColor := getUsageColorCode(ramPercent)
	storageColor := getUsageColorCode(storagePercent)

	// Fixed column widths for vertical alignment
	col1Width := 34 // "VMs:   4639 (On: 4046, Off: 593)" + 2 char spacing
	col2Width := 30 // "RAM: 49306/75927 GiB (64.9%)" needs ~30 chars

	// Row 1: Nodes, CPU, vCPUs
	nodesStr := fmt.Sprintf("%d/%d online", onlineNodes, len(cluster.Nodes))
	col1Content := fmt.Sprintf("Nodes: %s", nodesStr)
	sb.WriteString(labelStyle.Render("Nodes: ") + valueStyle.Render(nodesStr))
	sb.WriteString(strings.Repeat(" ", col1Width-len(col1Content)))

	cpuStr := fmt.Sprintf("%.1f%%", avgCPU)
	col2Content := fmt.Sprintf("CPU: %s", cpuStr)
	sb.WriteString(labelStyle.Render("CPU: ") + lipgloss.NewStyle().Foreground(lipgloss.Color(cpuColor)).Render(cpuStr))
	sb.WriteString(strings.Repeat(" ", col2Width-len(col2Content)))

	// vCPUs with cluster-wide percentage (vCPUs / total threads)
	vcpuPct := 0.0
	if cluster.TotalCPUs > 0 {
		vcpuPct = float64(cluster.TotalVCPUs) / float64(cluster.TotalCPUs) * 100
	}
	sb.WriteString(labelStyle.Render("vCPUs: ") + valueStyle.Render(fmt.Sprintf("%d/%d", cluster.TotalVCPUs, cluster.TotalCPUs)) + " " + valueStyle.Render(fmt.Sprintf("(%.0f%%)", vcpuPct)))
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
	sb.WriteString(labelStyle.Render("RAM: ") + valueStyle.Render(ramValStr) + " " + lipgloss.NewStyle().Foreground(lipgloss.Color(ramColor)).Render(ramPctStr))
	if len(ramFull) < col2Width {
		sb.WriteString(strings.Repeat(" ", col2Width-len(ramFull)))
	}

	sb.WriteString(labelStyle.Render("Storage: ") + valueStyle.Render(fmt.Sprintf("%.0f/%.0f TiB", usedStorageTiB, totalStorageTiB)))
	sb.WriteString(" " + lipgloss.NewStyle().Foreground(lipgloss.Color(storageColor)).Render(fmt.Sprintf("(%.1f%%)", storagePercent)))
	sb.WriteString("\n")

	return sb.String()
}

// getUsageColorCode returns color code based on usage percentage
// Up to 79%: green, 80-86%: yellow, 87%+: red
func getUsageColorCode(percent float64) string {
	if percent >= 87 {
		return "9" // bright red (readable on black background)
	} else if percent >= 80 {
		return "3" // yellow
	}
	return "2" // green
}

// RenderDashboardHostDetail renders the host detail view showing VMs and migration modes in split view
// focusSection: 0 = VM list, 1 = migration modes
func RenderDashboardHostDetail(node *proxmox.Node, cluster *proxmox.Cluster, version string, width, height, scrollPos, cursorPos int) string {
	return RenderDashboardHostDetailFull(node, cluster, version, width, height, scrollPos, cursorPos, 0, 0)
}

// RenderDashboardHostDetailFull renders the host detail view with focus section and mode selection
func RenderDashboardHostDetailFull(node *proxmox.Node, cluster *proxmox.Cluster, version string, width, height, scrollPos, cursorPos, focusSection, modeIdx int) string {
	var sb strings.Builder

	// Ensure minimum width
	if width < 80 {
		width = 100
	}

	// Styles
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("5"))
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	borderStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	selectedStyle := lipgloss.NewStyle().Background(lipgloss.Color("236")).Foreground(lipgloss.Color("15")).Bold(true)

	// Title with version
	versionStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	title := "KVM Migration Suggester"
	if version != "" {
		title += " " + versionStyle.Render("v"+version)
	}
	sb.WriteString(titleStyle.Render(title) + "\n")
	sb.WriteString(borderStyle.Render(strings.Repeat(boxDoubleLine, width)) + "\n")

	// Host detail header - regular grey color
	sb.WriteString(dimStyle.Render(fmt.Sprintf("Host: %s", node.Name)) + " ")
	cpuPct := node.GetCPUPercent()
	ramPct := node.GetMemPercent()
	sb.WriteString(dimStyle.Render(fmt.Sprintf("│ VMs: %d │ vCPUs: %d │ CPU: %.1f%% │ RAM: %.1f%% │ Storage: %s",
		len(node.VMs),
		node.GetRunningVCPUs(),
		cpuPct,
		ramPct,
		components.FormatStorageG(node.MaxDisk))) + "\n")

	// Mode options - "Balance Cluster" is last
	modes := []struct {
		name string
		desc string
	}{
		{"Migrate All", "Migrate all VMs from host, spread across cluster"},
		{"vCPU", "Migrate VMs by total vCPU count"},
		{"CPU Usage (%)", "Migrate VMs by host CPU usage percentage"},
		{"RAM (GiB)", "Migrate VMs by RAM amount in GiB"},
		{"Storage (GiB)", "Migrate VMs by storage amount in GiB"},
		{"Create Date", "Migrate VMs created more than N days ago"},
		{"Specific VMs", "Manually select specific VMs to migrate"},
		{"Balance Cluster", "Balance all hosts in cluster to same % usage"},
	}

	// Calculate split heights - VM list gets at least 50% of available space
	// Reserve: title(2) + host header(1) + empty line(1) + VM header+sep(2) + VM closing(1) + modes header+sep(2) + modes(len) + modes closing(1) + empty line(1) + help(1) + buffer(2)
	fixedOverhead := 2 + 1 + 1 + 2 + 1 + 2 + len(modes) + 1 + 1 + 1 + 2
	availableHeight := height - fixedOverhead

	// VM list gets at least 50% of the available space
	minVMListHeight := (height - 10) / 2 // At least 50% of usable height
	if minVMListHeight < 10 {
		minVMListHeight = 10
	}
	vmListHeight := availableHeight
	if vmListHeight < minVMListHeight {
		vmListHeight = minVMListHeight
	}

	// === VM LIST SECTION ===
	// Empty line between host header and VM list
	sb.WriteString("\n")

	// Build VM list (sorted by name)
	type vmItem struct {
		VMID       int
		Name       string
		Status     string
		CPUUsage   float64
		VCPUs      int
		RAM        int64
		UsedDisk   int64
		MaxDisk    int64
	}
	var vmList []vmItem
	for _, vm := range node.VMs {
		vmList = append(vmList, vmItem{
			VMID:     vm.VMID,
			Name:     vm.Name,
			Status:   vm.Status,
			CPUUsage: vm.CPUUsage,
			VCPUs:    vm.CPUCores,
			RAM:      vm.MaxMem,
			UsedDisk: vm.UsedDisk,
			MaxDisk:  vm.MaxDisk,
		})
	}

	sort.Slice(vmList, func(i, j int) bool {
		return vmList[i].Name < vmList[j].Name
	})

	// VM table column widths (matching migration summary style)
	const (
		colVMID       = 6
		colName       = 24
		colState      = 5
		colHCPU       = 6
		colVCPU       = 5
		colRAM        = 8
		colUsedDisk   = 9
		colMaxDisk    = 9
	)
	vmTableWidth := colVMID + colName + colState + colHCPU + colVCPU + colRAM + colUsedDisk + colMaxDisk + 7

	// Calculate visible VM rows
	maxVisibleVMs := vmListHeight
	if maxVisibleVMs < 5 {
		maxVisibleVMs = 5
	}

	totalVMs := len(vmList)
	needsVMScrollbar := totalVMs > maxVisibleVMs

	// Scrollbar calculations for VM list
	vmThumbPos := 0
	vmThumbSize := maxVisibleVMs
	if needsVMScrollbar && totalVMs > 0 {
		vmThumbSize = maxVisibleVMs * maxVisibleVMs / totalVMs
		if vmThumbSize < 1 {
			vmThumbSize = 1
		}
		if vmThumbSize > maxVisibleVMs {
			vmThumbSize = maxVisibleVMs
		}
		scrollRange := maxVisibleVMs - vmThumbSize
		if scrollRange > 0 && totalVMs > maxVisibleVMs {
			vmThumbPos = scrollPos * scrollRange / (totalVMs - maxVisibleVMs)
		}
	}

	// Scrollbar styles
	scrollTrackStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	scrollThumbStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("6"))

	// VM table header
	vmHeader := fmt.Sprintf("  %*s %-*s %-*s %*s %*s %*s %*s %*s",
		colVMID, "VMID",
		colName, "Name",
		colState, "State",
		colHCPU, "HCPU%",
		colVCPU, "vCPU",
		colRAM, "RAM",
		colUsedDisk, "Used",
		colMaxDisk, "Max")
	if needsVMScrollbar {
		sb.WriteString(headerStyle.Render(vmHeader) + "  \n")
		sb.WriteString("  " + strings.Repeat("─", vmTableWidth) + "  \n")
	} else {
		sb.WriteString(headerStyle.Render(vmHeader) + "\n")
		sb.WriteString("  " + strings.Repeat("─", vmTableWidth) + "\n")
	}

	startIdx := scrollPos
	endIdx := startIdx + maxVisibleVMs
	if endIdx > totalVMs {
		endIdx = totalVMs
	}

	// Render visible VMs with scrollbar
	for i := startIdx; i < endIdx; i++ {
		vm := vmList[i]

		stateStr := "Off"
		if vm.Status == "running" {
			stateStr = "On"
		}

		// Calculate HCPU% = CPUUsage * vCPUs / hostCores
		hcpuPct := 0.0
		if node.CPUCores > 0 {
			hcpuPct = vm.CPUUsage * float64(vm.VCPUs) / float64(node.CPUCores)
		}
		cpuStr := fmt.Sprintf("%.1f", hcpuPct)
		vcpuStr := fmt.Sprintf("%d", vm.VCPUs)
		ramStr := components.FormatRAMShort(vm.RAM)
		usedDiskStr := components.FormatStorageG(vm.UsedDisk)
		maxDiskStr := components.FormatStorageG(vm.MaxDisk)

		name := vm.Name
		if len(name) > colName {
			name = name[:colName-3] + "..."
		}

		row := fmt.Sprintf("%*d %-*s %-*s %*s %*s %*s %*s %*s",
			colVMID, vm.VMID,
			colName, name,
			colState, stateStr,
			colHCPU, cpuStr,
			colVCPU, vcpuStr,
			colRAM, ramStr,
			colUsedDisk, usedDiskStr,
			colMaxDisk, maxDiskStr)

		// Scrollbar character
		scrollChar := ""
		if needsVMScrollbar {
			rowIdx := i - scrollPos
			if rowIdx >= vmThumbPos && rowIdx < vmThumbPos+vmThumbSize {
				scrollChar = scrollThumbStyle.Render("█")
			} else {
				scrollChar = scrollTrackStyle.Render("│")
			}
		}

		// Only show cursor if VM list is focused
		if i == cursorPos && focusSection == 0 {
			// Pad row for consistent highlighting
			if len(row) < vmTableWidth {
				row += strings.Repeat(" ", vmTableWidth-len(row))
			}
			sb.WriteString("▶ " + selectedStyle.Render(row))
		} else {
			// All VMs in regular grey
			sb.WriteString("  " + dimStyle.Render(row))
		}

		if needsVMScrollbar {
			sb.WriteString(" " + scrollChar)
		}
		sb.WriteString("\n")
	}

	// VM table closing line
	if needsVMScrollbar {
		sb.WriteString("  " + strings.Repeat("─", vmTableWidth) + "  \n")
	} else {
		sb.WriteString("  " + strings.Repeat("─", vmTableWidth) + "\n")
	}

	// VM scroll info (right-aligned)
	if totalVMs > maxVisibleVMs {
		scrollInfo := fmt.Sprintf("Showing %d-%d of %d", startIdx+1, endIdx, totalVMs)
		padding := vmTableWidth + 2 - len(scrollInfo)
		if padding > 0 {
			scrollInfo = strings.Repeat(" ", padding) + scrollInfo
		}
		sb.WriteString(dimStyle.Render(scrollInfo) + "\n")
	}

	// === MIGRATION MODES SECTION ===
	sb.WriteString(dimStyle.Render("─── Select Migration Mode: ───") + "\n")

	// Mode table column widths
	const (
		colModeName = 20
		colModeDesc = 50
	)
	modeTableWidth := colModeName + colModeDesc + 4

	// Mode table header separator
	sb.WriteString("  " + strings.Repeat("─", modeTableWidth) + "\n")

	// Render modes as a table - one row per mode
	for i, mode := range modes {
		name := mode.name
		desc := mode.desc

		// Pad/truncate to column widths
		if len(name) > colModeName {
			name = name[:colModeName-1] + "…"
		}
		if len(desc) > colModeDesc {
			desc = desc[:colModeDesc-1] + "…"
		}

		row := fmt.Sprintf("%-*s  %s", colModeName, name, desc)

		if i == modeIdx && focusSection == 1 {
			// Pad row for consistent highlighting
			if len(row) < modeTableWidth {
				row += strings.Repeat(" ", modeTableWidth-len(row))
			}
			sb.WriteString("▶ " + selectedStyle.Render(row) + "\n")
		} else {
			sb.WriteString("  " + dimStyle.Render(row) + "\n")
		}
	}

	// Mode table closing line
	sb.WriteString("  " + strings.Repeat("─", modeTableWidth) + "\n")

	// Empty line before help
	sb.WriteString("\n")

	// Help text
	sb.WriteString(helpStyle.Render("Tab: Switch section │ ↑/↓: Navigate │ Enter: Select │ Esc: Back"))

	return sb.String()
}
