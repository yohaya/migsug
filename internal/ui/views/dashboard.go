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
	instructionStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	sb.WriteString(instructionStyle.Render("Select source node to migrate from:") + "\n\n")

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

	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	valueStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	runningStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	stoppedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

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

	// Fixed column widths for alignment
	col1Width := 22 // "Nodes: XX/XX online" or "CPU:   XX.X%"
	col2Width := 35 // "VMs: XXXX (On: XXXX, Off: XXX)" or "RAM: XXXXX/XXXXX GiB (XX.X%)"

	// Row 1: Nodes, VMs, vCPUs - aligned columns
	nodesStr := fmt.Sprintf("%d/%d online", onlineNodes, len(cluster.Nodes))
	sb.WriteString(labelStyle.Render("Nodes: ") + valueStyle.Render(fmt.Sprintf("%-*s", col1Width-7, nodesStr)))
	vmStr := fmt.Sprintf("%d ", cluster.TotalVMs)
	sb.WriteString(labelStyle.Render("VMs: ") + valueStyle.Render(vmStr))
	sb.WriteString(dimStyle.Render("(") + runningStyle.Render(fmt.Sprintf("On: %d", cluster.RunningVMs)) + dimStyle.Render(", "))
	sb.WriteString(stoppedStyle.Render(fmt.Sprintf("Off: %d", cluster.StoppedVMs)) + dimStyle.Render(")"))
	// Calculate padding to align vCPUs with Storage
	vmFullLen := 5 + len(vmStr) + 1 + 4 + len(fmt.Sprintf("%d", cluster.RunningVMs)) + 2 + 5 + len(fmt.Sprintf("%d", cluster.StoppedVMs)) + 1
	if vmFullLen < col2Width {
		sb.WriteString(strings.Repeat(" ", col2Width-vmFullLen))
	}
	sb.WriteString(labelStyle.Render("vCPUs: ") + valueStyle.Render(fmt.Sprintf("%d", cluster.TotalVCPUs)))
	sb.WriteString("\n")

	// Row 2: CPU, RAM, Storage - aligned columns with usage colors
	cpuStr := fmt.Sprintf("%.1f%%", avgCPU)
	sb.WriteString(labelStyle.Render("CPU:   ") + lipgloss.NewStyle().Foreground(lipgloss.Color(cpuColor)).Render(fmt.Sprintf("%-*s", col1Width-7, cpuStr)))
	ramValStr := fmt.Sprintf("%.0f/%.0f GiB", usedRAMGiB, totalRAMGiB)
	ramPctStr := fmt.Sprintf("(%.1f%%)", ramPercent)
	sb.WriteString(labelStyle.Render("RAM: ") + valueStyle.Render(ramValStr) + " " + lipgloss.NewStyle().Foreground(lipgloss.Color(ramColor)).Render(ramPctStr))
	// Pad to align with Storage
	ramFullLen := 5 + len(ramValStr) + 1 + len(ramPctStr)
	if ramFullLen < col2Width {
		sb.WriteString(strings.Repeat(" ", col2Width-ramFullLen))
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
