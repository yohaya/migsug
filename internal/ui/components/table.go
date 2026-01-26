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

// Box drawing characters for table
const (
	boxHeavyHoriz  = "━"
	boxLightHoriz  = "─"
	boxDoubleHoriz = "═"
	boxVertical    = "│"
	boxTopLeft     = "┌"
	boxTopRight    = "┐"
	boxBottomLeft  = "└"
	boxBottomRight = "┘"
	boxTeeDown     = "┬"
	boxTeeUp       = "┴"
	boxTeeRight    = "├"
	boxTeeLeft     = "┤"
	boxCross       = "┼"
)

// SortInfo contains sorting information for the table
type SortInfo struct {
	Column    int  // 0-6 for columns 1-7
	Ascending bool
}

// RenderNodeTableWide creates a full-width table of nodes with detailed info
func RenderNodeTableWide(nodes []proxmox.Node, selectedIdx int, width int) string {
	return RenderNodeTableWideWithSort(nodes, selectedIdx, width, SortInfo{Column: 0, Ascending: true})
}

// RenderNodeTableWideWithSort creates a full-width table with sort indicator
func RenderNodeTableWideWithSort(nodes []proxmox.Node, selectedIdx int, width int, sortInfo SortInfo) string {
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
	colVMs := 6
	colVCPUs := 8
	colCPUPct := 8
	colRAM := 22      // e.g., "1632/2048G (80%)"
	colDisk := 20     // e.g., "165/205T (80%)"
	// CPU Model gets remaining width - no artificial limit
	colCPUModel := width - colName - colStatus - colVMs - colVCPUs - colCPUPct - colRAM - colDisk - 20
	if colCPUModel < 20 {
		colCPUModel = 20
	}

	// Styles
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	borderStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	sepStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	// Sort arrows
	upArrow := "▲"
	downArrow := "▼"
	getSortArrow := func(col int) string {
		if sortInfo.Column == col {
			if sortInfo.Ascending {
				return " " + upArrow
			}
			return " " + downArrow
		}
		return ""
	}

	// Main header with Unicode separators and sort arrows
	sep := sepStyle.Render(boxVertical)
	header1 := fmt.Sprintf("  %-*s %s %-*s %s %*s %s %*s %s %*s %s %-*s %s %-*s %s %s",
		colName, "Host"+getSortArrow(0), sep,
		colStatus, "Status"+getSortArrow(1), sep,
		colVMs, "VMs"+getSortArrow(2), sep,
		colVCPUs, "vCPUs"+getSortArrow(3), sep,
		colCPUPct, "CPU%"+getSortArrow(4), sep,
		colRAM, "RAM"+getSortArrow(5), sep,
		colDisk, "Disk"+getSortArrow(6), sep,
		"CPU Model")
	sb.WriteString(headerStyle.Render(header1) + "\n")
	// Use graphical separator
	sb.WriteString("  " + borderStyle.Render(strings.Repeat(boxHeavyHoriz, width-4)) + "\n")

	// Rows
	for i, node := range nodes {
		isSelected := i == selectedIdx
		isOffline := node.Status != "online"

		// Format values
		cpuPctStr := fmt.Sprintf("%5.1f%%", node.GetCPUPercent())
		// vCPUs: show only running vCPUs
		runningVCPUs := node.GetRunningVCPUs()
		vcpuStr := fmt.Sprintf("%d", runningVCPUs)
		// RAM: show used/total in G with percentage at end
		ramStr := FormatRAMWithPercent(node.UsedMem, node.MaxMem, node.GetMemPercent())
		// Disk: show used/total with percentage
		diskStr := FormatDiskWithPercent(node.UsedDisk, node.MaxDisk, node.GetDiskPercent())

		// CPU info - format as "2x Xeon (2.5GHz, 176 threads)"
		cpuInfo := formatCPUInfo(node.CPUSockets, node.CPUModel, node.CPUMHz, node.CPUCores)

		// Build the row content (plain text for width calculation)
		rowContent := fmt.Sprintf("%-*s %-*s %*d %*s %*s %-*s %-*s %s",
			colName, truncate(node.Name, colName),
			colStatus, node.Status,
			colVMs, len(node.VMs),
			colVCPUs, vcpuStr,
			colCPUPct, cpuPctStr,
			colRAM, ramStr,
			colDisk, diskStr,
			cpuInfo) // Don't truncate CPU info

		// Pad row to full width for consistent highlighting
		if len(rowContent) < width-4 {
			rowContent += strings.Repeat(" ", width-4-len(rowContent))
		}

		// Selector prefix
		prefix := "  "
		if isSelected {
			prefix = "▶ "
		}

		// Apply styling based on selection and status
		var styledRow string
		if isSelected {
			// Selected: highlight entire row with background
			selectBgStyle := lipgloss.NewStyle().
				Background(lipgloss.Color("236")).
				Foreground(lipgloss.Color("15")).
				Bold(true)
			styledRow = selectBgStyle.Render(prefix + rowContent)
		} else if isOffline {
			// Offline node: dim the entire row
			styledRow = prefix + offlineStyle.Render(rowContent)
		} else {
			// Normal row: apply color coding to specific columns
			var coloredRow strings.Builder

			// Name
			nodeName := truncate(node.Name, colName)
			coloredRow.WriteString(fmt.Sprintf("%-*s ", colName, nodeName))

			// Status with color
			statusColor := "2" // green for online
			if node.Status != "online" {
				statusColor = "9" // bright red for offline
			}
			statusStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(statusColor))
			coloredRow.WriteString(statusStyle.Render(fmt.Sprintf("%-*s", colStatus, node.Status)) + " ")

			// VMs
			coloredRow.WriteString(fmt.Sprintf("%*d ", colVMs, len(node.VMs)))

			// vCPUs (running only)
			coloredRow.WriteString(fmt.Sprintf("%*s ", colVCPUs, vcpuStr))

			// CPU % with color
			cpuPctColor := getUsageColor(node.GetCPUPercent())
			cpuPctStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(cpuPctColor))
			coloredRow.WriteString(cpuPctStyle.Render(fmt.Sprintf("%*s", colCPUPct, cpuPctStr)) + " ")

			// RAM with color
			ramColor := getUsageColor(node.GetMemPercent())
			ramStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ramColor))
			coloredRow.WriteString(ramStyle.Render(fmt.Sprintf("%-*s", colRAM, ramStr)) + " ")

			// Disk with color
			diskColor := getUsageColor(node.GetDiskPercent())
			diskStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(diskColor))
			coloredRow.WriteString(diskStyle.Render(fmt.Sprintf("%-*s", colDisk, diskStr)) + " ")

			// CPU model (dimmed) - don't truncate unless terminal is too narrow
			coloredRow.WriteString(dimStyle.Render(cpuInfo))

			styledRow = prefix + coloredRow.String()
		}

		sb.WriteString(styledRow + "\n")
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

// FormatRAMWithPercent formats used/total RAM in G with percentage at end
// e.g., "1232/2320G (52%)"
func FormatRAMWithPercent(usedBytes, totalBytes int64, percent float64) string {
	const gib = 1024 * 1024 * 1024
	usedG := float64(usedBytes) / float64(gib)
	totalG := float64(totalBytes) / float64(gib)
	return fmt.Sprintf("%.0f/%.0fG (%.0f%%)", usedG, totalG, percent)
}

// FormatDiskWithPercent formats used/total disk with percentage
// e.g., "156/205T (76%)"
func FormatDiskWithPercent(usedBytes, totalBytes int64, percent float64) string {
	usedStr := FormatBytesShort(usedBytes)
	totalStr := FormatBytesShort(totalBytes)
	return fmt.Sprintf("%s/%s (%.0f%%)", usedStr, totalStr, percent)
}

// FormatRAMGiB formats bytes to GiB with percentage (e.g., "1.6T (80%)")
func FormatRAMGiB(bytes int64, percent float64) string {
	const gib = 1024 * 1024 * 1024
	const tib = gib * 1024

	if bytes >= tib {
		val := float64(bytes) / float64(tib)
		return fmt.Sprintf("%.1fT (%d%%)", val, int(percent))
	}
	val := float64(bytes) / float64(gib)
	if val >= 100 {
		return fmt.Sprintf("%.0fG (%d%%)", val, int(percent))
	}
	return fmt.Sprintf("%.1fG (%d%%)", val, int(percent))
}

// FormatRAMGiBSimple formats bytes to GiB without percentage (e.g., "2.0T")
func FormatRAMGiBSimple(bytes int64) string {
	const gib = 1024 * 1024 * 1024
	const tib = gib * 1024

	if bytes >= tib {
		val := float64(bytes) / float64(tib)
		return fmt.Sprintf("%.1fT", val)
	}
	val := float64(bytes) / float64(gib)
	if val >= 100 {
		return fmt.Sprintf("%.0fG", val)
	}
	return fmt.Sprintf("%.1fG", val)
}

// formatCPUInfo formats CPU info as "2x Xeon (2.5GHz, 176 threads)"
func formatCPUInfo(sockets int, model string, mhz float64, threads int) string {
	if model == "" {
		return "-"
	}

	// Shorten the model name
	shortModel := shortenCPUModel(model)

	// Format GHz and threads
	suffix := ""
	if mhz > 0 && threads > 0 {
		ghz := mhz / 1000.0
		suffix = fmt.Sprintf(" (%.1fGHz, %d threads)", ghz, threads)
	} else if mhz > 0 {
		ghz := mhz / 1000.0
		suffix = fmt.Sprintf(" (%.1fGHz)", ghz)
	} else if threads > 0 {
		suffix = fmt.Sprintf(" (%d threads)", threads)
	}

	// Format with socket count if available
	if sockets > 0 {
		return fmt.Sprintf("%dx %s%s", sockets, shortModel, suffix)
	}
	return shortModel + suffix
}

// shortenCPUModel shortens the CPU model name
func shortenCPUModel(model string) string {
	// Remove common prefixes/suffixes to make it shorter
	replacements := []struct {
		old string
		new string
	}{
		{"Intel(R) Xeon(R) CPU ", "Xeon "},
		{"Intel(R) Xeon(R) ", "Xeon "},
		{"Intel(R) Core(TM) ", "Core "},
		{"AMD EPYC ", "EPYC "},
		{"AMD Ryzen ", "Ryzen "},
		{" Processor", ""},
		{" CPU", ""},
		{" @ ", " "},
		{"  ", " "},
	}

	result := model
	for _, r := range replacements {
		result = strings.ReplaceAll(result, r.old, r.new)
	}
	return strings.TrimSpace(result)
}

// getUsageColor returns color based on usage percentage
func getUsageColor(percent float64) string {
	if percent > 80 {
		return "9" // bright red (readable on black background)
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
