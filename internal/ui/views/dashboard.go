package views

import (
	"fmt"
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
	// - Help text: 1 line
	// Total: 14 lines
	fixedOverhead := 14
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

	// Help text
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	sb.WriteString(helpStyle.Render("↑/↓/PgUp/PgDn/Home/End: Navigate │ 1-8: Sort columns │ Enter: Select │ r: Refresh │ q: Quit"))

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

	// Help text
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	sb.WriteString(helpStyle.Render("↑/↓/PgUp/PgDn/Home/End: Navigate │ 1-8: Sort columns │ Enter: Select │ r: Refresh │ q: Quit"))

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
