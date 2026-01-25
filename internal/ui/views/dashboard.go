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

// RenderDashboardFull renders the main dashboard view with all options
func RenderDashboardFull(cluster *proxmox.Cluster, selectedIdx int, width int, countdown int, refreshing bool, version string) string {
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
	sb.WriteString(components.RenderNodeTableWide(cluster.Nodes, selectedIdx, width))
	sb.WriteString("\n")

	// Graphical separator
	sb.WriteString(borderStyle.Render(strings.Repeat(boxThinHoriz, width)) + "\n")

	// Refresh status line
	refreshStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	if refreshing {
		sb.WriteString(refreshStyle.Render("⟳ Refreshing cluster data...") + "\n")
	} else if countdown > 0 {
		sb.WriteString(refreshStyle.Render(fmt.Sprintf("⟳ Auto-refresh in %ds", countdown)) + "  ")
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("(Press 'r' to refresh now)") + "\n")
	}

	// Help text
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	sb.WriteString(helpStyle.Render("↑/↓: Navigate │ Enter: Select node │ r: Refresh │ q: Quit │ ?: Help"))

	return sb.String()
}

// renderEnhancedClusterSummary creates a rich cluster summary with all requested info
func renderEnhancedClusterSummary(cluster *proxmox.Cluster, width int) string {
	var sb strings.Builder

	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	valueStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	// Count online nodes
	onlineNodes := 0
	for _, node := range cluster.Nodes {
		if node.Status == "online" {
			onlineNodes++
		}
	}

	// Calculate cluster-wide storage
	totalStorageGiB := float64(cluster.TotalStorage) / (1024 * 1024 * 1024)
	usedStorageGiB := float64(cluster.UsedStorage) / (1024 * 1024 * 1024)
	storagePercent := 0.0
	if cluster.TotalStorage > 0 {
		storagePercent = float64(cluster.UsedStorage) / float64(cluster.TotalStorage) * 100
	}

	// Calculate RAM
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

	// Line 1: Nodes and VMs with detailed breakdown
	line1 := labelStyle.Render("Nodes: ") +
		valueStyle.Render(fmt.Sprintf("%d/%d online", onlineNodes, len(cluster.Nodes)))

	line1 += "    " + labelStyle.Render("VMs: ") +
		valueStyle.Render(fmt.Sprintf("%d total", cluster.TotalVMs))

	// Show running/stopped breakdown
	runningStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	stoppedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	line1 += " " + dimStyle.Render("(") +
		runningStyle.Render(fmt.Sprintf("%d running", cluster.RunningVMs)) +
		dimStyle.Render(", ") +
		stoppedStyle.Render(fmt.Sprintf("%d stopped", cluster.StoppedVMs)) +
		dimStyle.Render(")")

	// Total vCPUs
	line1 += "    " + labelStyle.Render("vCPUs: ") +
		valueStyle.Render(fmt.Sprintf("%d", cluster.TotalVCPUs))

	sb.WriteString(line1 + "\n")

	// Line 2: Resource usage with graphical elements
	cpuColor := getUsageColorCode(avgCPU)
	ramColor := getUsageColorCode(ramPercent)
	storageColor := getUsageColorCode(storagePercent)

	line2 := labelStyle.Render("CPU: ") +
		lipgloss.NewStyle().Foreground(lipgloss.Color(cpuColor)).Render(fmt.Sprintf("%5.1f%% avg", avgCPU))

	line2 += "    " + labelStyle.Render("RAM: ") +
		lipgloss.NewStyle().Foreground(lipgloss.Color(ramColor)).Render(fmt.Sprintf("%5.1f%%", ramPercent)) +
		dimStyle.Render(fmt.Sprintf(" (%.1f/%.1f GiB)", usedRAMGiB, totalRAMGiB))

	line2 += "    " + labelStyle.Render("Storage: ") +
		lipgloss.NewStyle().Foreground(lipgloss.Color(storageColor)).Render(fmt.Sprintf("%5.1f%%", storagePercent)) +
		dimStyle.Render(fmt.Sprintf(" (%.1f/%.1f GiB)", usedStorageGiB, totalStorageGiB))

	sb.WriteString(line2 + "\n")

	return sb.String()
}

// getUsageColorCode returns color code based on usage percentage
func getUsageColorCode(percent float64) string {
	if percent > 80 {
		return "1" // red
	} else if percent > 60 {
		return "3" // yellow
	}
	return "2" // green
}
