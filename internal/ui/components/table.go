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
	Column    int // 0-7 for columns 1-8
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
	colVCPUs := 13 // e.g., "572 (325%)"
	colCPUPct := 8
	colLA := 14   // e.g., "62.56 (35.5%)"
	colRAM := 22  // e.g., "1632/2048G (80%)"
	colDisk := 20 // e.g., "165/205T (80%)"
	// CPU Model gets remaining width - no artificial limit
	colCPUModel := width - colName - colStatus - colVMs - colVCPUs - colCPUPct - colLA - colRAM - colDisk - 24
	if colCPUModel < 20 {
		colCPUModel = 20
	}

	// Styles
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	borderStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("6"))

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

	// Main header with sort arrows (aligned to match row format)
	header1 := fmt.Sprintf("  %-*s %-*s %*s %*s %*s %*s %-*s %-*s %s",
		colName, "Host"+getSortArrow(0),
		colStatus, "Status"+getSortArrow(1),
		colVMs, "VMs"+getSortArrow(2),
		colVCPUs, "vCPUs"+getSortArrow(3),
		colCPUPct, "CPU%"+getSortArrow(4),
		colLA, "LA"+getSortArrow(5),
		colRAM, "RAM"+getSortArrow(6),
		colDisk, "Disk"+getSortArrow(7),
		"CPU Model")
	sb.WriteString(headerStyle.Render(header1) + "\n")
	// Use graphical separator
	sb.WriteString("  " + borderStyle.Render(strings.Repeat(boxHeavyHoriz, width-4)) + "\n")

	// Rows
	for i, node := range nodes {
		isSelected := i == selectedIdx
		isOffline := node.Status != "online"

		// Format values
		// Check if CPU% is 0.0% but has running VMs (error condition)
		runningVMs := 0
		for _, vm := range node.VMs {
			if vm.Status == "running" {
				runningVMs++
			}
		}
		cpuPctStr := fmt.Sprintf("%5.1f%%", node.GetCPUPercent())
		cpuPctError := node.GetCPUPercent() == 0 && runningVMs > 0
		if cpuPctError {
			cpuPctStr = "  err%"
		}
		// vCPUs: show running vCPUs with overcommit percentage (vCPUs/threads)
		runningVCPUs := node.GetRunningVCPUs()
		vcpuOvercommit := 0.0
		if node.CPUCores > 0 {
			vcpuOvercommit = float64(runningVCPUs) / float64(node.CPUCores) * 100
		}
		vcpuStr := fmt.Sprintf("%d (%.0f%%)", runningVCPUs, vcpuOvercommit)
		// Load Average (1 minute) with percentage of total threads
		laStr := "-"
		if len(node.LoadAverage) > 0 {
			la := node.LoadAverage[0]
			if node.CPUCores > 0 {
				laPercent := la / float64(node.CPUCores) * 100
				laStr = fmt.Sprintf("%.2f (%.1f%%)", la, laPercent)
			} else {
				laStr = fmt.Sprintf("%.2f", la)
			}
		}
		// RAM: show used/total in G with percentage at end
		ramStr := FormatRAMWithPercent(node.UsedMem, node.MaxMem, node.GetMemPercent())
		// Disk: show used/total with percentage
		diskStr := FormatDiskWithPercent(node.UsedDisk, node.MaxDisk, node.GetDiskPercent())

		// CPU info - format as "2x Xeon (2.5GHz, 176 threads)"
		cpuInfo := formatCPUInfo(node.CPUSockets, node.CPUModel, node.CPUMHz, node.CPUCores)

		// Build the row content (plain text for width calculation)
		rowContent := fmt.Sprintf("%-*s %-*s %*d %*s %*s %*s %-*s %-*s %s",
			colName, truncate(node.Name, colName),
			colStatus, node.Status,
			colVMs, len(node.VMs),
			colVCPUs, vcpuStr,
			colCPUPct, cpuPctStr,
			colLA, laStr,
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

			// CPU % with color (bright red if error)
			cpuPctColor := getUsageColor(node.GetCPUPercent())
			if cpuPctError {
				cpuPctColor = "9" // bright red for error
			}
			cpuPctStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(cpuPctColor))
			coloredRow.WriteString(cpuPctStyle.Render(fmt.Sprintf("%*s", colCPUPct, cpuPctStr)) + " ")

			// Load Average with color (based on LA relative to CPU cores)
			laColor := "2" // green by default
			if len(node.LoadAverage) > 0 && node.CPUCores > 0 {
				laRatio := node.LoadAverage[0] / float64(node.CPUCores) * 100
				laColor = getUsageColor(laRatio)
			}
			laStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(laColor))
			coloredRow.WriteString(laStyle.Render(fmt.Sprintf("%*s", colLA, laStr)) + " ")

			// RAM with color
			ramColor := getUsageColor(node.GetMemPercent())
			ramStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ramColor))
			coloredRow.WriteString(ramStyle.Render(fmt.Sprintf("%-*s", colRAM, ramStr)) + " ")

			// Disk with color based on free space
			diskColor := getDiskFreeColor(node.MaxDisk - node.UsedDisk)
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
// Up to 79%: green, 80-86%: yellow, 87%+: red
func getUsageColor(percent float64) string {
	if percent >= 87 {
		return "9" // bright red (readable on black background)
	} else if percent >= 80 {
		return "3" // yellow
	}
	return "2" // green
}

// getDiskFreeColor returns color based on free disk space
// Below 300G: red, 300-500G: yellow, above 500G: green
func getDiskFreeColor(freeBytes int64) string {
	const gib = 1024 * 1024 * 1024
	freeGiB := freeBytes / gib

	if freeGiB < 300 {
		return "9" // bright red
	} else if freeGiB < 500 {
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
	return RenderSuggestionTableWithCursor(suggestions, scrollPos, maxVisible, -1)
}

// RenderSuggestionTableWithCursor creates a scrollable table with cursor highlighting
func RenderSuggestionTableWithCursor(suggestions []analyzer.MigrationSuggestion, scrollPos, maxVisible, cursorPos int) string {
	var sb strings.Builder

	// Column widths - removed From, added CPU%
	const (
		colVMID    = 6
		colName    = 30 // Wider for full server name
		colTo      = 22
		colCPU     = 6 // CPU usage %
		colVCPU    = 5
		colRAM     = 10
		colStorage = 10
	)

	totalWidth := colVMID + colName + colTo + colCPU + colVCPU + colRAM + colStorage + 6

	// Highlight style for selected row
	selectedStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("236")).
		Foreground(lipgloss.Color("15")).
		Bold(true)

	// Scrollbar styles
	scrollTrackStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	scrollThumbStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("6"))

	totalItems := len(suggestions)
	needsScrollbar := totalItems > maxVisible

	// Calculate scrollbar thumb position
	thumbPos := 0
	thumbSize := maxVisible
	if needsScrollbar && totalItems > 0 {
		// Calculate thumb position (0 to maxVisible-thumbSize)
		thumbSize = max(1, maxVisible*maxVisible/totalItems)
		if thumbSize > maxVisible {
			thumbSize = maxVisible
		}
		scrollRange := maxVisible - thumbSize
		if scrollRange > 0 && totalItems > maxVisible {
			thumbPos = scrollPos * scrollRange / (totalItems - maxVisible)
		}
	}

	// Header (with 2-char prefix for alignment, +2 for scrollbar)
	header := fmt.Sprintf("  %*s %-*s %-*s %*s %*s %*s %*s",
		colVMID, "VMID",
		colName, "Name",
		colTo, "To",
		colCPU, "CPU%",
		colVCPU, "vCPU",
		colRAM, "RAM",
		colStorage, "Storage")
	if needsScrollbar {
		sb.WriteString(headerStyle.Render(header) + "  \n")
		sb.WriteString("  " + strings.Repeat("─", totalWidth) + "  \n")
	} else {
		sb.WriteString(headerStyle.Render(header) + "\n")
		sb.WriteString("  " + strings.Repeat("─", totalWidth) + "\n")
	}

	// Calculate visible range
	endPos := scrollPos + maxVisible
	if endPos > totalItems {
		endPos = totalItems
	}

	// Rows (only visible portion) - all on single line with scrollbar
	for i := scrollPos; i < endPos; i++ {
		sug := suggestions[i]
		isSelected := (i == cursorPos)

		// Format CPU usage as percentage
		cpuStr := fmt.Sprintf("%.1f", sug.CPUUsage*100)

		row := fmt.Sprintf("%*d %-*s %-*s %*s %*d %*s %*s",
			colVMID, sug.VMID,
			colName, truncate(sug.VMName, colName),
			colTo, truncate(sug.TargetNode, colTo),
			colCPU, cpuStr,
			colVCPU, sug.VCPUs,
			colRAM, FormatBytes(sug.RAM),
			colStorage, FormatBytes(sug.Storage),
		)

		// Scrollbar character for this row
		scrollChar := ""
		if needsScrollbar {
			rowIdx := i - scrollPos
			if rowIdx >= thumbPos && rowIdx < thumbPos+thumbSize {
				scrollChar = scrollThumbStyle.Render("█")
			} else {
				scrollChar = scrollTrackStyle.Render("│")
			}
		}

		// Selector indicator and styling
		if isSelected {
			// Pad the row to full width for consistent highlighting
			if len(row) < totalWidth {
				row += strings.Repeat(" ", totalWidth-len(row))
			}
			sb.WriteString("▶ " + selectedStyle.Render(row))
		} else {
			style := normalStyle
			if sug.TargetNode == "NONE" {
				style = offlineStyle
			}
			sb.WriteString("  " + style.Render(row))
		}

		if needsScrollbar {
			sb.WriteString(" " + scrollChar)
		}
		sb.WriteString("\n")
	}

	// Pad remaining rows if we have fewer items than maxVisible
	for i := endPos - scrollPos; i < maxVisible && needsScrollbar; i++ {
		rowIdx := i
		scrollChar := ""
		if rowIdx >= thumbPos && rowIdx < thumbPos+thumbSize {
			scrollChar = scrollThumbStyle.Render("█")
		} else {
			scrollChar = scrollTrackStyle.Render("│")
		}
		sb.WriteString(strings.Repeat(" ", totalWidth+2) + " " + scrollChar + "\n")
	}

	return sb.String()
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// RenderNodeStateComparison shows before/after comparison for a node on a single line
func RenderNodeStateComparison(nodeName string, before, after analyzer.NodeState) string {
	var sb strings.Builder

	labelStyle := lipgloss.NewStyle() // Regular text color
	nodeStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("5"))
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("15"))

	// Node name
	sb.WriteString(nodeStyle.Render(fmt.Sprintf("%-20s", nodeName)))

	// VMs: before→after (diff)
	vmDiff := after.VMCount - before.VMCount
	sb.WriteString(labelStyle.Render("VMs: "))
	sb.WriteString(valueStyle.Render(fmt.Sprintf("%d→%d", before.VMCount, after.VMCount)))
	sb.WriteString(renderDiff(vmDiff) + "  ")

	// CPU: before→after (diff)
	cpuDiff := after.CPUPercent - before.CPUPercent
	sb.WriteString(labelStyle.Render("CPU: "))
	sb.WriteString(valueStyle.Render(fmt.Sprintf("%.1f%%→%.1f%%", before.CPUPercent, after.CPUPercent)))
	sb.WriteString(renderPercentDiff(cpuDiff) + "  ")

	// RAM: before→after (diff)
	ramDiff := after.RAMPercent - before.RAMPercent
	sb.WriteString(labelStyle.Render("RAM: "))
	sb.WriteString(valueStyle.Render(fmt.Sprintf("%.1f%%→%.1f%%", before.RAMPercent, after.RAMPercent)))
	sb.WriteString(renderPercentDiff(ramDiff) + "  ")

	// Storage: before→after (diff)
	storageDiff := after.StoragePercent - before.StoragePercent
	sb.WriteString(labelStyle.Render("Stor: "))
	sb.WriteString(valueStyle.Render(fmt.Sprintf("%.1f%%→%.1f%%", before.StoragePercent, after.StoragePercent)))
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
