package views

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/yourusername/migsug/internal/proxmox"
	"github.com/yourusername/migsug/internal/ui/components"
)

// RenderDashboard renders the main dashboard view (without refresh info)
func RenderDashboard(cluster *proxmox.Cluster, selectedIdx int, width int) string {
	return RenderDashboardWithRefresh(cluster, selectedIdx, width, 0, false)
}

// RenderDashboardWithRefresh renders the main dashboard view with refresh countdown
func RenderDashboardWithRefresh(cluster *proxmox.Cluster, selectedIdx int, width int, countdown int, refreshing bool) string {
	var sb strings.Builder

	// Ensure minimum width
	if width < 60 {
		width = 80
	}

	// Title with full width
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("5"))
	sb.WriteString(titleStyle.Render("Proxmox VM Migration Suggester") + "\n")
	sb.WriteString(strings.Repeat("=", width) + "\n\n")

	// Cluster summary with width
	sb.WriteString(components.RenderClusterSummaryWide(cluster, width))
	sb.WriteString("\n")

	// Instructions
	instructionStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	sb.WriteString(instructionStyle.Render("Select source node to migrate from:") + "\n\n")

	// Node table with width
	sb.WriteString(components.RenderNodeTableWide(cluster.Nodes, selectedIdx, width))
	sb.WriteString("\n")

	// Full-width separator
	sb.WriteString(strings.Repeat("-", width) + "\n")

	// Refresh status line
	refreshStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	if refreshing {
		sb.WriteString(refreshStyle.Render("Refreshing cluster data...") + "\n")
	} else if countdown > 0 {
		sb.WriteString(refreshStyle.Render(fmt.Sprintf("Auto-refresh in %ds", countdown)) + "  ")
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("(Press 'r' to refresh now)") + "\n")
	}

	// Help text
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	sb.WriteString(helpStyle.Render("Up/Down: Navigate | Enter: Select node | r: Refresh | q: Quit | ?: Help"))

	return sb.String()
}
