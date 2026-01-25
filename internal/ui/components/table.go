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

// RenderNodeTableWide creates a full-width table of nodes with detailed info
func RenderNodeTableWide(nodes []proxmox.Node, selectedIdx int, width int) string {
	var sb strings.Builder

	// Ensure minimum width
	minTableWidth := 120
	if width < minTableWidth {
		width = minTableWidth
	}

	// Determine max name length from actual data
	maxNameLen := 10
	for _, node := range nodes {
		if len(node.Name) > maxNameLen {
			maxNameLen = len(node.Name)
		}
	}
	if maxNameLen > 25 {
		maxNameLen = 25
	}

	// Column widths - fit to terminal width
	colName := maxNameLen + 2
	colStatus := 8
	colVMs := 5
	colCPUs := 6
	colCPUPct := 7
	colRAMUsed := 12
	colRAMMax := 10
	colDiskUsed := 12
	colDiskMax := 10
	colCPUModel := width - colName - colStatus - colVMs - colCPUs - colCPUPct - colRAMUsed - colRAMMax - colDiskUsed - colDiskMax - 15
	if colCPUModel < 15 {
		colCPUModel = 15
	}
	if colCPUModel > 35 {
		colCPUModel = 35
	}

	// Header - two lines for better readability
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	// Main header
	header1 := fmt.Sprintf("  %-*s %-*s %*s %*s %*s  %-*s  %-*s  %s",
		colName, "Host",
		colStatus, "Status",
		colVMs, "VMs",
		colCPUs, "CPUs",
		colCPUPct, "CPU%",
		colRAMUsed+colRAMMax+1, "RAM (Used/Total)",
		colDiskUsed+colDiskMax+1, "Disk (Used/Total)",
		"CPU Model")
	sb.WriteString(headerStyle.Render(header1) + "\n")
	sb.WriteString("  " + strings.Repeat("=", width-4) + "\n")

	// Rows
	for i, node := range nodes {
		style := normalStyle
		if i == selectedIdx {
			style = selectedStyle
		}
		if node.Status != "online" {
			style = offlineStyle
		}

		// Format values
		cpuPctStr := fmt.Sprintf("%5.1f%%", node.GetCPUPercent())
		ramUsedStr := FormatBytesShort(node.UsedMem)
		ramMaxStr := FormatBytesShort(node.MaxMem)
		diskUsedStr := FormatBytesShort(node.UsedDisk)
		diskMaxStr := FormatBytesShort(node.MaxDisk)

		// CPU model - truncate if needed
		cpuModel := node.CPUModel
		if cpuModel == "" {
			cpuModel = "-"
		} else {
			cpuModel = shortenCPUModel(cpuModel)
		}

		// Format RAM and Disk with color coding
		ramPct := node.GetMemPercent()
		diskPct := node.GetDiskPercent()

		ramColor := getUsageColor(ramPct)
		diskColor := getUsageColor(diskPct)

		ramStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ramColor))
		diskStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(diskColor))

		// Build the row
		var row strings.Builder

		// Name (full or truncated to fit)
		nodeName := node.Name
		if len(nodeName) > colName-1 {
			nodeName = nodeName[:colName-4] + "..."
		}
		row.WriteString(fmt.Sprintf("%-*s ", colName, nodeName))

		// Status
		statusStyle := normalStyle
		if node.Status == "online" {
			statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
		} else {
			statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
		}
		row.WriteString(statusStyle.Render(fmt.Sprintf("%-*s", colStatus, node.Status)) + " ")

		// VMs count
		row.WriteString(fmt.Sprintf("%*d ", colVMs, len(node.VMs)))

		// CPUs (sockets x cores format if available)
		cpuStr := fmt.Sprintf("%d", node.CPUCores)
		if node.CPUSockets > 0 {
			cpuStr = fmt.Sprintf("%d", node.CPUCores)
		}
		row.WriteString(fmt.Sprintf("%*s ", colCPUs, cpuStr))

		// CPU usage %
		cpuPctColor := getUsageColor(node.GetCPUPercent())
		cpuPctStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(cpuPctColor))
		row.WriteString(cpuPctStyle.Render(fmt.Sprintf("%*s", colCPUPct, cpuPctStr)) + "  ")

		// RAM used/total
		row.WriteString(ramStyle.Render(fmt.Sprintf("%*s", colRAMUsed, ramUsedStr)))
		row.WriteString(dimStyle.Render("/"))
		row.WriteString(fmt.Sprintf("%-*s  ", colRAMMax, ramMaxStr))

		// Disk used/total
		row.WriteString(diskStyle.Render(fmt.Sprintf("%*s", colDiskUsed, diskUsedStr)))
		row.WriteString(dimStyle.Render("/"))
		row.WriteString(fmt.Sprintf("%-*s  ", colDiskMax, diskMaxStr))

		// CPU model
		row.WriteString(dimStyle.Render(truncate(cpuModel, colCPUModel)))

		// Selector
		if i == selectedIdx {
			sb.WriteString("> ")
		} else {
			sb.WriteString("  ")
		}

		sb.WriteString(style.Render(row.String()) + "\n")
	}

	return sb.String()
}

// FormatBytesShort formats bytes to a short human-readable format (e.g., "128G")
func FormatBytesShort(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%dB", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	units := []string{"K", "M", "G", "T", "P"}
	val := float64(bytes) / float64(div)
	if val >= 100 {
		return fmt.Sprintf("%.0f%s", val, units[exp])
	}
	return fmt.Sprintf("%.1f%s", val, units[exp])
}

// shortenCPUModel shortens the CPU model name
func shortenCPUModel(model string) string {
	// Remove common prefixes/suffixes to make it shorter
	replacements := []struct {
		old string
		new string
	}{
		{"Intel(R) Xeon(R) ", "Xeon "},
		{"Intel(R) Core(TM) ", "Core "},
		{"AMD EPYC ", "EPYC "},
		{"AMD Ryzen ", "Ryzen "},
		{" Processor", ""},
		{" CPU", ""},
		{" @ ", "@"},
		{"  ", " "},
	}

	result := model
	for _, r := range replacements {
		result = strings.ReplaceAll(result, r.old, r.new)
	}
	return result
}

// getUsageColor returns color based on usage percentage
func getUsageColor(percent float64) string {
	if percent > 80 {
		return "1" // red
	} else if percent > 60 {
		return "3" // yellow
	}
	return "2" // green
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

// RenderSuggestionTable creates a table of migration suggestions (shows all)
func RenderSuggestionTable(suggestions []analyzer.MigrationSuggestion) string {
	return RenderSuggestionTableWithScroll(suggestions, 0, len(suggestions))
}

// RenderSuggestionTableWithScroll creates a scrollable table of migration suggestions
func RenderSuggestionTableWithScroll(suggestions []analyzer.MigrationSuggestion, scrollPos, maxVisible int) string {
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

	// Show scroll up indicator if not at top
	if scrollPos > 0 {
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("  ↑ more above...") + "\n")
	}

	// Calculate visible range
	endPos := scrollPos + maxVisible
	if endPos > len(suggestions) {
		endPos = len(suggestions)
	}

	// Rows (only visible portion)
	for i := scrollPos; i < endPos; i++ {
		sug := suggestions[i]
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

	// Show scroll down indicator if not at bottom
	if endPos < len(suggestions) {
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("  ↓ more below...") + "\n")
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
