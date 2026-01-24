package views

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/yourusername/migsug/internal/proxmox"
	"github.com/yourusername/migsug/internal/ui/components"
)

var titleStyle = lipgloss.NewStyle().
	Bold(true).
	Foreground(lipgloss.Color("5")).
	Padding(1, 0)

// RenderDashboard renders the main dashboard view
func RenderDashboard(cluster *proxmox.Cluster, selectedIdx int, width int) string {
	var sb strings.Builder

	// Title
	sb.WriteString(titleStyle.Render("🖥️  Proxmox VM Migration Suggester") + "\n\n")

	// Cluster summary
	sb.WriteString(components.RenderClusterSummary(cluster))
	sb.WriteString("\n\n")

	// Instructions
	instructionStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	sb.WriteString(instructionStyle.Render("Select source node to migrate from:") + "\n\n")

	// Node table
	sb.WriteString(components.RenderNodeTable(cluster.Nodes, selectedIdx))
	sb.WriteString("\n")

	// Help text
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	sb.WriteString(helpStyle.Render("↑/↓: Navigate  Enter: Select  q: Quit  ?: Help"))

	return sb.String()
}
