package components

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/yourusername/migsug/internal/proxmox"
)

var (
	boxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("6")).
			Padding(1, 2).
			Width(40)

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("6"))

	labelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("8"))

	valueStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("15"))
)

// RenderClusterSummary creates a summary box for the cluster
func RenderClusterSummary(cluster *proxmox.Cluster) string {
	summary := proxmox.GetClusterSummary(cluster)

	content := titleStyle.Render("Cluster Summary") + "\n\n"

	content += labelStyle.Render("Nodes:   ") +
		valueStyle.Render(fmt.Sprintf("%d online / %d total",
			summary["online_nodes"], summary["total_nodes"])) + "\n"

	content += labelStyle.Render("VMs:     ") +
		valueStyle.Render(fmt.Sprintf("%d", summary["total_vms"])) + "\n"

	content += labelStyle.Render("CPU:     ") +
		valueStyle.Render(fmt.Sprintf("%.1f%% average", summary["avg_cpu_percent"])) + "\n"

	content += labelStyle.Render("RAM:     ") +
		valueStyle.Render(fmt.Sprintf("%.1f%% (%s / %s)",
			summary["mem_percent"],
			FormatBytes(summary["used_memory"].(int64)),
			FormatBytes(summary["total_memory"].(int64)))) + "\n"

	content += labelStyle.Render("Storage: ") +
		valueStyle.Render(fmt.Sprintf("%.1f%%", summary["storage_percent"])) + "\n"

	return boxStyle.Render(content)
}

// RenderClusterSummaryWide creates a full-width cluster summary
func RenderClusterSummaryWide(cluster *proxmox.Cluster, width int) string {
	summary := proxmox.GetClusterSummary(cluster)

	// Calculate column widths
	col1Width := 20
	col2Width := 25
	col3Width := 25
	col4Width := width - col1Width - col2Width - col3Width - 10
	if col4Width < 20 {
		col4Width = 20
	}

	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	valueStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))

	// Build single-line summary
	var line1, line2 string

	// Line 1: Nodes and VMs
	line1 = labelStyle.Render("Nodes: ") +
		valueStyle.Render(fmt.Sprintf("%-*s", col1Width-7, fmt.Sprintf("%d/%d online",
			summary["online_nodes"], summary["total_nodes"])))
	line1 += labelStyle.Render("  VMs: ") +
		valueStyle.Render(fmt.Sprintf("%-*s", col2Width-7, fmt.Sprintf("%d total", summary["total_vms"])))

	// Line 2: Resource usage
	line2 = labelStyle.Render("CPU: ") +
		valueStyle.Render(fmt.Sprintf("%-*s", col1Width-5, fmt.Sprintf("%.1f%% avg", summary["avg_cpu_percent"])))
	line2 += labelStyle.Render("  RAM: ") +
		valueStyle.Render(fmt.Sprintf("%-*s", col2Width-7, fmt.Sprintf("%.1f%% (%s/%s)",
			summary["mem_percent"],
			FormatBytes(summary["used_memory"].(int64)),
			FormatBytes(summary["total_memory"].(int64)))))
	line2 += labelStyle.Render("  Storage: ") +
		valueStyle.Render(fmt.Sprintf("%.1f%%", summary["storage_percent"]))

	return line1 + "\n" + line2 + "\n"
}

// RenderNodeSummary creates a summary box for a specific node
func RenderNodeSummary(node *proxmox.Node) string {
	content := titleStyle.Render(fmt.Sprintf("Node: %s", node.Name)) + "\n\n"

	statusColor := "2"
	if node.Status != "online" {
		statusColor = "1"
	}

	content += labelStyle.Render("Status:  ") +
		lipgloss.NewStyle().Foreground(lipgloss.Color(statusColor)).Bold(true).
			Render(node.Status) + "\n"

	content += labelStyle.Render("VMs:     ") +
		valueStyle.Render(fmt.Sprintf("%d", len(node.VMs))) + "\n"

	content += labelStyle.Render("CPU:     ") +
		valueStyle.Render(fmt.Sprintf("%.1f%% (%d cores)",
			node.GetCPUPercent(), node.CPUCores)) + "\n"

	content += labelStyle.Render("RAM:     ") +
		valueStyle.Render(fmt.Sprintf("%.1f%% (%s / %s)",
			node.GetMemPercent(),
			FormatBytes(node.UsedMem),
			FormatBytes(node.MaxMem))) + "\n"

	content += labelStyle.Render("Storage: ") +
		valueStyle.Render(fmt.Sprintf("%.1f%% (%s / %s)",
			node.GetDiskPercent(),
			FormatBytes(node.UsedDisk),
			FormatBytes(node.MaxDisk))) + "\n"

	return boxStyle.Render(content)
}

// RenderMigrationSummary creates a summary box for migration results
func RenderMigrationSummary(totalVMs int, totalVCPUs int, totalRAM int64, totalStorage int64, improvement string) string {
	content := titleStyle.Render("Migration Summary") + "\n\n"

	content += labelStyle.Render("VMs:        ") +
		valueStyle.Render(fmt.Sprintf("%d", totalVMs)) + "\n"

	content += labelStyle.Render("vCPUs:      ") +
		valueStyle.Render(fmt.Sprintf("%d", totalVCPUs)) + "\n"

	content += labelStyle.Render("RAM:        ") +
		valueStyle.Render(FormatBytes(totalRAM)) + "\n"

	content += labelStyle.Render("Storage:    ") +
		valueStyle.Render(FormatBytes(totalStorage)) + "\n\n"

	content += labelStyle.Render("Improvement: ") +
		lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Bold(true).
			Render(improvement) + "\n"

	return boxStyle.Render(content)
}

// RenderHelp creates a help box with keyboard shortcuts
func RenderHelp() string {
	content := titleStyle.Render("Keyboard Shortcuts") + "\n\n"

	shortcuts := []struct {
		key  string
		desc string
	}{
		{"↑/↓ or j/k", "Navigate"},
		{"Enter", "Select / Confirm"},
		{"Space", "Toggle selection"},
		{"Tab", "Next field"},
		{"Esc", "Go back"},
		{"?", "Toggle help"},
		{"q / Ctrl+C", "Quit"},
	}

	for _, s := range shortcuts {
		content += lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true).
			Render(fmt.Sprintf("%-15s", s.key))
		content += labelStyle.Render(s.desc) + "\n"
	}

	return boxStyle.Width(50).Render(content)
}
