package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/yourusername/migsug/internal/analyzer"
	"github.com/yourusername/migsug/internal/proxmox"
)

var (
	headerStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	selectedStyle = lipgloss.NewStyle().Background(lipgloss.Color("240")).Foreground(lipgloss.Color("15"))
	normalStyle   = lipgloss.NewStyle()
	offlineStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

// RenderNodeTable creates a table of nodes with resource usage
func RenderNodeTable(nodes []proxmox.Node, selectedIdx int) string {
	var sb strings.Builder

	// Column widths (must match between header and rows)
	const (
		colName    = 15
		colStatus  = 8
		colVMs     = 5
		colCPU     = 8
		colRAM     = 8
		colStorage = 8
	)

	// Header (with 2-char prefix to align with row selector "→ ")
	header := fmt.Sprintf("  %-*s %-*s %*s %*s %*s %*s",
		colName, "Name",
		colStatus, "Status",
		colVMs, "VMs",
		colCPU, "CPU",
		colRAM, "RAM",
		colStorage, "Storage")
	sb.WriteString(headerStyle.Render(header) + "\n")
	sb.WriteString("  " + strings.Repeat("─", colName+colStatus+colVMs+colCPU+colRAM+colStorage+5) + "\n")

	// Rows
	for i, node := range nodes {
		style := normalStyle
		if i == selectedIdx {
			style = selectedStyle
		}
		if node.Status != "online" {
			style = offlineStyle
		}

		// Format percentages with consistent width (right-aligned)
		cpuStr := fmt.Sprintf("%6.1f%%", node.GetCPUPercent())
		ramStr := fmt.Sprintf("%6.1f%%", node.GetMemPercent())
		storageStr := fmt.Sprintf("%6.1f%%", node.GetDiskPercent())

		row := fmt.Sprintf("%-*s %-*s %*d %*s %*s %*s",
			colName, truncate(node.Name, colName),
			colStatus, node.Status,
			colVMs, len(node.VMs),
			colCPU, cpuStr,
			colRAM, ramStr,
			colStorage, storageStr,
		)

		if i == selectedIdx {
			sb.WriteString("→ ")
		} else {
			sb.WriteString("  ")
		}

		sb.WriteString(style.Render(row) + "\n")
	}

	return sb.String()
}

// RenderNodeTableWide creates a full-width table of nodes with resource bars
func RenderNodeTableWide(nodes []proxmox.Node, selectedIdx int, width int) string {
	var sb strings.Builder

	// Calculate column widths based on terminal width
	// Reserve space for: selector(2) + name + status + VMs + CPU + RAM + Storage + spacing
	minTableWidth := 80
	if width < minTableWidth {
		width = minTableWidth
	}

	// Column widths
	colName := 15
	colStatus := 8
	colVMs := 5
	colCPU := 8
	colRAM := 8
	colStorage := 8
	colCPUBar := (width - colName - colStatus - colVMs - colCPU - colRAM - colStorage - 20) / 3
	if colCPUBar < 10 {
		colCPUBar = 10
	}
	colRAMBar := colCPUBar
	colStorageBar := colCPUBar

	// Header
	header := fmt.Sprintf("  %-*s %-*s %*s %*s %-*s %*s %-*s %*s %-*s",
		colName, "Name",
		colStatus, "Status",
		colVMs, "VMs",
		colCPU, "CPU",
		colCPUBar, "",
		colRAM, "RAM",
		colRAMBar, "",
		colStorage, "Disk",
		colStorageBar, "")
	sb.WriteString(headerStyle.Render(header) + "\n")
	sb.WriteString("  " + strings.Repeat("-", width-4) + "\n")

	// Rows
	for i, node := range nodes {
		style := normalStyle
		if i == selectedIdx {
			style = selectedStyle
		}
		if node.Status != "online" {
			style = offlineStyle
		}

		// Format percentages
		cpuPct := node.GetCPUPercent()
		ramPct := node.GetMemPercent()
		diskPct := node.GetDiskPercent()

		cpuStr := fmt.Sprintf("%5.1f%%", cpuPct)
		ramStr := fmt.Sprintf("%5.1f%%", ramPct)
		diskStr := fmt.Sprintf("%5.1f%%", diskPct)

		// Create progress bars
		cpuBar := renderProgressBar(cpuPct, colCPUBar)
		ramBar := renderProgressBar(ramPct, colRAMBar)
		diskBar := renderProgressBar(diskPct, colStorageBar)

		// Format row
		row := fmt.Sprintf("%-*s %-*s %*d %*s %s %*s %s %*s %s",
			colName, truncate(node.Name, colName),
			colStatus, node.Status,
			colVMs, len(node.VMs),
			colCPU, cpuStr,
			cpuBar,
			colRAM, ramStr,
			ramBar,
			colStorage, diskStr,
			diskBar)

		// Selector
		if i == selectedIdx {
			sb.WriteString("> ")
		} else {
			sb.WriteString("  ")
		}

		sb.WriteString(style.Render(row) + "\n")
	}

	return sb.String()
}

// renderProgressBar creates a text-based progress bar
func renderProgressBar(percent float64, width int) string {
	if width < 5 {
		width = 5
	}

	// Calculate filled portion
	barWidth := width - 2 // Account for [ and ]
	filled := int(percent / 100.0 * float64(barWidth))
	if filled > barWidth {
		filled = barWidth
	}
	if filled < 0 {
		filled = 0
	}

	// Choose color based on percentage
	barColor := "2" // green
	if percent > 80 {
		barColor = "1" // red
	} else if percent > 60 {
		barColor = "3" // yellow
	}

	// Build the bar
	filledPart := strings.Repeat("=", filled)
	emptyPart := strings.Repeat("-", barWidth-filled)

	barStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(barColor))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	return "[" + barStyle.Render(filledPart) + dimStyle.Render(emptyPart) + "]"
}

// RenderVMTable creates a table of VMs with resource usage
func RenderVMTable(vms []proxmox.VM, selectedIndices map[int]bool, cursorIdx int) string {
	var sb strings.Builder

	// Column widths
	const (
		colCheck   = 3 // [x]
		colVMID    = 6
		colName    = 20
		colStatus  = 8
		colVCPU    = 5
		colCPU     = 7
		colRAM     = 10
		colStorage = 10
	)

	// Header (with prefix to align with "→ [x] ")
	header := fmt.Sprintf("      %*s %-*s %-*s %*s %*s %*s %*s",
		colVMID, "VMID",
		colName, "Name",
		colStatus, "Status",
		colVCPU, "vCPU",
		colCPU, "CPU%",
		colRAM, "RAM",
		colStorage, "Storage")
	sb.WriteString(headerStyle.Render(header) + "\n")
	sb.WriteString("      " + strings.Repeat("─", colVMID+colName+colStatus+colVCPU+colCPU+colRAM+colStorage+6) + "\n")

	// Rows
	for i, vm := range vms {
		style := normalStyle
		if i == cursorIdx {
			style = selectedStyle
		}
		if vm.Status != "running" {
			style = offlineStyle
		}

		checkbox := " "
		if selectedIndices[vm.VMID] {
			checkbox = "✓"
		}

		// Format CPU usage with consistent width
		cpuStr := fmt.Sprintf("%5.1f%%", vm.CPUUsage)

		row := fmt.Sprintf("[%s] %*d %-*s %-*s %*d %*s %*s %*s",
			checkbox,
			colVMID, vm.VMID,
			colName, truncate(vm.Name, colName),
			colStatus, vm.Status,
			colVCPU, vm.CPUCores,
			colCPU, cpuStr,
			colRAM, FormatBytes(vm.UsedMem),
			colStorage, FormatBytes(vm.UsedDisk),
		)

		if i == cursorIdx {
			sb.WriteString("→ ")
		} else {
			sb.WriteString("  ")
		}

		sb.WriteString(style.Render(row) + "\n")
	}

	return sb.String()
}

// RenderSuggestionTable creates a table of migration suggestions
func RenderSuggestionTable(suggestions []analyzer.MigrationSuggestion) string {
	var sb strings.Builder

	// Column widths
	const (
		colVMID    = 6
		colName    = 20
		colFrom    = 12
		colTo      = 12
		colVCPU    = 5
		colRAM     = 10
		colStorage = 10
	)

	// Header (with 2-char prefix for alignment)
	header := fmt.Sprintf("  %*s %-*s %-*s %-*s %*s %*s %*s",
		colVMID, "VMID",
		colName, "Name",
		colFrom, "From",
		colTo, "To",
		colVCPU, "vCPU",
		colRAM, "RAM",
		colStorage, "Storage")
	sb.WriteString(headerStyle.Render(header) + "\n")
	sb.WriteString("  " + strings.Repeat("─", colVMID+colName+colFrom+colTo+colVCPU+colRAM+colStorage+6) + "\n")

	// Rows
	for _, sug := range suggestions {
		style := normalStyle
		if sug.TargetNode == "NONE" {
			style = offlineStyle
		}

		row := fmt.Sprintf("%*d %-*s %-*s %-*s %*d %*s %*s",
			colVMID, sug.VMID,
			colName, truncate(sug.VMName, colName),
			colFrom, truncate(sug.SourceNode, colFrom),
			colTo, truncate(sug.TargetNode, colTo),
			colVCPU, sug.VCPUs,
			colRAM, FormatBytes(sug.RAM),
			colStorage, FormatBytes(sug.Storage),
		)

		sb.WriteString("  " + style.Render(row) + "\n")

		// Add reason as subtitle
		if sug.Reason != "" {
			reasonStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Italic(true)
			sb.WriteString("    " + reasonStyle.Render("└─ "+truncate(sug.Reason, 80)) + "\n")
		}
	}

	return sb.String()
}

// RenderNodeStateComparison shows before/after comparison for a node
func RenderNodeStateComparison(nodeName string, before, after analyzer.NodeState) string {
	var sb strings.Builder

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("5"))
	sb.WriteString(titleStyle.Render(fmt.Sprintf("Node: %s", nodeName)) + "\n\n")

	// Before column
	sb.WriteString(headerStyle.Render("BEFORE") + strings.Repeat(" ", 25))
	sb.WriteString(headerStyle.Render("AFTER") + "\n")
	sb.WriteString(strings.Repeat("─", 60) + "\n")

	// VMs
	sb.WriteString(fmt.Sprintf("VMs:     %-5d", before.VMCount))
	sb.WriteString(strings.Repeat(" ", 20))
	sb.WriteString(fmt.Sprintf("%-5d", after.VMCount))
	diff := after.VMCount - before.VMCount
	sb.WriteString(renderDiff(diff))
	sb.WriteString("\n")

	// CPU
	sb.WriteString(fmt.Sprintf("CPU:     %-5.1f%%", before.CPUPercent))
	sb.WriteString(strings.Repeat(" ", 19))
	sb.WriteString(fmt.Sprintf("%-5.1f%%", after.CPUPercent))
	cpuDiff := after.CPUPercent - before.CPUPercent
	sb.WriteString(renderPercentDiff(cpuDiff))
	sb.WriteString("\n")

	// RAM
	sb.WriteString(fmt.Sprintf("RAM:     %-5.1f%%", before.RAMPercent))
	sb.WriteString(strings.Repeat(" ", 19))
	sb.WriteString(fmt.Sprintf("%-5.1f%%", after.RAMPercent))
	ramDiff := after.RAMPercent - before.RAMPercent
	sb.WriteString(renderPercentDiff(ramDiff))
	sb.WriteString("\n")

	// Storage
	sb.WriteString(fmt.Sprintf("Storage: %-5.1f%%", before.StoragePercent))
	sb.WriteString(strings.Repeat(" ", 19))
	sb.WriteString(fmt.Sprintf("%-5.1f%%", after.StoragePercent))
	storageDiff := after.StoragePercent - before.StoragePercent
	sb.WriteString(renderPercentDiff(storageDiff))
	sb.WriteString("\n")

	return sb.String()
}

func renderDiff(diff int) string {
	if diff > 0 {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Render(fmt.Sprintf(" (+%d)", diff))
	} else if diff < 0 {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Render(fmt.Sprintf(" (%d)", diff))
	}
	return ""
}

func renderPercentDiff(diff float64) string {
	if diff > 0.1 {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Render(fmt.Sprintf(" (+%.1f%%)", diff))
	} else if diff < -0.1 {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Render(fmt.Sprintf(" (%.1f%%)", diff))
	}
	return ""
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}
