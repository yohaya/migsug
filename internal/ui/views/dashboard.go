package views

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/yourusername/migsug/internal/proxmox"
	"github.com/yourusername/migsug/internal/ui/components"
)

// RenderDashboard renders the main dashboard view
func RenderDashboard(cluster *proxmox.Cluster, selectedIdx int, width int) string {
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

	// Help text
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	sb.WriteString(helpStyle.Render("Up/Down: Navigate | Enter: Select node | q: Quit | ?: Help"))

	return sb.String()
}
