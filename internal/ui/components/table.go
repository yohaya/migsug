package components

import (
	"fmt"
	"sort"
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
	return RenderNodeTableWideWithScroll(nodes, selectedIdx, width, sortInfo, len(nodes))
}

// RenderNodeTableWideWithScroll creates a full-width table with sort indicator and scroll support
func RenderNodeTableWideWithScroll(nodes []proxmox.Node, selectedIdx int, width int, sortInfo SortInfo, maxVisible int) string {
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
	// Note: Headers include sort numbers like "[1]" and sort arrow "▼", so widths must accommodate
	colName := maxNameLen + 2
	colStatus := 14 // "Status [2] ▼" = 13 chars, data "online (abcd)" = 14 chars
	colVMs := 10    // "VMs [3] ▼" = 9 chars, need space for header + data up to 4 digits
	colVCPUs := 12  // "vCPUs [4] ▼" = 11 chars, data "9999 (999%)" = 12 chars
	colCPUPct := 11 // "CPU% [5] ▼" = 10 chars
	colLA := 14     // "LA [6] ▼" = 8 chars, but data can be "62.56 (35.5%)"
	colRAM := 22    // "RAM [7] ▼" = 9 chars, but data is "1632/2048G (80%)"
	colDisk := 20   // "Disk [8] ▼" = 10 chars, but data is "165/205T (80%)"
	colSwap := 4    // "Swap" (no sort number)
	colKVM := 7     // "KVM" (no sort number), values like "8.1.2"
	// CPU Model gets remaining width - no artificial limit
	colCPUModel := width - colName - colStatus - colVMs - colVCPUs - colCPUPct - colLA - colRAM - colDisk - colSwap - colKVM - 13
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

	totalItems := len(nodes)
	if maxVisible <= 0 || maxVisible > totalItems {
		maxVisible = totalItems
	}

	// Scrollbar styles
	scrollTrackStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	scrollThumbStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	needsScrollbar := totalItems > maxVisible

	// Calculate scroll position to keep selected item visible
	scrollPos := 0
	if selectedIdx >= maxVisible {
		scrollPos = selectedIdx - maxVisible + 1
	}
	if scrollPos+maxVisible > totalItems {
		scrollPos = totalItems - maxVisible
	}
	if scrollPos < 0 {
		scrollPos = 0
	}

	// Calculate scrollbar thumb position and size
	thumbPos := 0
	thumbSize := maxVisible
	if needsScrollbar && totalItems > 0 {
		thumbSize = max(1, maxVisible*maxVisible/totalItems)
		if thumbSize > maxVisible {
			thumbSize = maxVisible
		}
		scrollRange := maxVisible - thumbSize
		if scrollRange > 0 && totalItems > maxVisible {
			thumbPos = scrollPos * scrollRange / (totalItems - maxVisible)
		}
	}

	// Main header with sort numbers [1-8] and sort arrows (aligned to match row format)
	header1 := fmt.Sprintf("  %-*s %-*s %*s %*s %*s %*s %*s %*s %-*s %-*s %s",
		colName, "Host [1]"+getSortArrow(0),
		colStatus, "Status [2]"+getSortArrow(1),
		colVMs, "VMs [3]"+getSortArrow(2),
		colVCPUs, "vCPUs [4]"+getSortArrow(3),
		colCPUPct, "CPU% [5]"+getSortArrow(4),
		colLA, "LA [6]"+getSortArrow(5),
		colRAM, "RAM [7]"+getSortArrow(6),
		colDisk, "Disk [8]"+getSortArrow(7),
		colSwap, "Swap",
		colKVM, "KVM",
		"CPU Model")
	if needsScrollbar {
		sb.WriteString(headerStyle.Render(header1) + "  \n")
		sb.WriteString("  " + borderStyle.Render(strings.Repeat(boxHeavyHoriz, width-4)) + "  \n")
	} else {
		sb.WriteString(headerStyle.Render(header1) + "\n")
		sb.WriteString("  " + borderStyle.Render(strings.Repeat(boxHeavyHoriz, width-4)) + "\n")
	}

	// Calculate visible range
	endPos := scrollPos + maxVisible
	if endPos > totalItems {
		endPos = totalItems
	}

	// Rows (only visible portion)
	for i := scrollPos; i < endPos; i++ {
		node := nodes[i]
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

		// Swap status
		swapStr := "No"
		if node.HasActiveSwap() {
			swapStr = "Yes"
		}

		// KVM/PVE version (extract short version from "pve-manager/8.1.2/...")
		kvmStr := extractPVEVersion(node.PVEVersion)

		// Get status with indicators (e.g., "online (OP)")
		statusWithIndicators := node.GetStatusWithIndicators()

		// Build the row content (plain text for width calculation)
		rowContent := fmt.Sprintf("%-*s %-*s %*d %*s %*s %*s %*s %*s %-*s %-*s %s",
			colName, truncate(node.Name, colName),
			colStatus, statusWithIndicators,
			colVMs, len(node.VMs),
			colVCPUs, vcpuStr,
			colCPUPct, cpuPctStr,
			colLA, laStr,
			colRAM, ramStr,
			colDisk, diskStr,
			colSwap, swapStr,
			colKVM, kvmStr,
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

			// Status with indicators and color
			statusColor := "2" // green for online
			if node.Status != "online" {
				statusColor = "9" // bright red for offline
			}
			statusStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(statusColor))
			coloredRow.WriteString(statusStyle.Render(fmt.Sprintf("%-*s", colStatus, statusWithIndicators)) + " ")

			// VMs
			coloredRow.WriteString(fmt.Sprintf("%*d ", colVMs, len(node.VMs)))

			// vCPUs with color based on overcommit percentage
			// Below 300%: green, 300-499%: yellow, 500%+: red
			vcpuColor := "2" // green
			if vcpuOvercommit >= 500 {
				vcpuColor = "9" // bright red
			} else if vcpuOvercommit >= 300 {
				vcpuColor = "3" // yellow
			}
			vcpuStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(vcpuColor))
			coloredRow.WriteString(vcpuStyle.Render(fmt.Sprintf("%*s", colVCPUs, vcpuStr)) + " ")

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

			// RAM with color (right-aligned)
			ramColor := getUsageColor(node.GetMemPercent())
			ramStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ramColor))
			coloredRow.WriteString(ramStyle.Render(fmt.Sprintf("%*s", colRAM, ramStr)) + " ")

			// Disk with color based on free space (right-aligned)
			diskColor := getDiskFreeColor(node.MaxDisk - node.UsedDisk)
			diskStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(diskColor))
			coloredRow.WriteString(diskStyle.Render(fmt.Sprintf("%*s", colDisk, diskStr)) + " ")

			// Swap with color (red for Yes, green for No)
			swapColor := "2" // green for No
			if node.HasActiveSwap() {
				swapColor = "9" // bright red for Yes
			}
			swapStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(swapColor))
			coloredRow.WriteString(swapStyle.Render(fmt.Sprintf("%-*s", colSwap, swapStr)) + " ")

			// KVM version (dimmed)
			coloredRow.WriteString(dimStyle.Render(fmt.Sprintf("%-*s", colKVM, kvmStr)) + " ")

			// CPU model (dimmed) - don't truncate unless terminal is too narrow
			coloredRow.WriteString(dimStyle.Render(cpuInfo))

			styledRow = prefix + coloredRow.String()
		}

		// Add scrollbar character if needed
		scrollChar := ""
		if needsScrollbar {
			rowIdx := i - scrollPos
			if rowIdx >= thumbPos && rowIdx < thumbPos+thumbSize {
				scrollChar = " " + scrollThumbStyle.Render("█")
			} else {
				scrollChar = " " + scrollTrackStyle.Render("│")
			}
		}

		sb.WriteString(styledRow + scrollChar + "\n")
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

// FormatStorageG formats storage size always in GB with whole numbers
func FormatStorageG(bytes int64) string {
	const gib = 1024 * 1024 * 1024
	gb := float64(bytes) / float64(gib)
	return fmt.Sprintf("%.0fG", gb)
}

// FormatRAMShort formats RAM size with whole numbers (no decimals)
func FormatRAMShort(bytes int64) string {
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
	return fmt.Sprintf("%.0f%s", val, units[exp])
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

// FormatUsedTotalPercent formats RAM as used/totalG (percent%)
// e.g., "1639/2015G (81%)"
func FormatUsedTotalPercent(usedBytes, totalBytes int64, percent float64) string {
	const gib = 1024 * 1024 * 1024
	usedG := float64(usedBytes) / float64(gib)
	totalG := float64(totalBytes) / float64(gib)
	return fmt.Sprintf("%.0f/%.0fG (%.0f%%)", usedG, totalG, percent)
}

// FormatUsedTotalPercentStorage formats storage as used/total (percent%)
// e.g., "31.4T/34.6T (91%)"
func FormatUsedTotalPercentStorage(usedBytes, totalBytes int64, percent float64) string {
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

// extractPVEVersion extracts the short version from PVE version string
// Input format: "pve-manager/8.1.2/1234567890abcdef" -> Output: "8.1.2"
func extractPVEVersion(pveVersion string) string {
	if pveVersion == "" {
		return "-"
	}
	// Split by "/" and get the second part (version number)
	parts := strings.Split(pveVersion, "/")
	if len(parts) >= 2 {
		return parts[1]
	}
	return pveVersion
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
	return RenderVMTableWithScroll(vms, selectedIndices, cursorIdx, len(vms))
}

// RenderVMTableWithScroll creates a table of VMs with scroll support
func RenderVMTableWithScroll(vms []proxmox.VM, selectedIndices map[int]bool, cursorIdx int, maxVisible int) string {
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

	totalWidth := colCheck + colVMID + colName + colStatus + colVCPU + colCPU + colRAM + colStorage + 6

	// Scrollbar styles
	scrollTrackStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	scrollThumbStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("6"))

	totalItems := len(vms)
	if maxVisible <= 0 || maxVisible > totalItems {
		maxVisible = totalItems
	}
	needsScrollbar := totalItems > maxVisible

	// Calculate scroll position to keep selected item visible
	scrollPos := 0
	if cursorIdx >= maxVisible {
		scrollPos = cursorIdx - maxVisible + 1
	}
	if scrollPos+maxVisible > totalItems {
		scrollPos = totalItems - maxVisible
	}
	if scrollPos < 0 {
		scrollPos = 0
	}

	// Calculate scrollbar thumb position
	thumbPos := 0
	thumbSize := maxVisible
	if needsScrollbar && totalItems > 0 {
		thumbSize = max(1, maxVisible*maxVisible/totalItems)
		if thumbSize > maxVisible {
			thumbSize = maxVisible
		}
		scrollRange := maxVisible - thumbSize
		if scrollRange > 0 && totalItems > maxVisible {
			thumbPos = scrollPos * scrollRange / (totalItems - maxVisible)
		}
	}

	// Header (with prefix to align with "→ [x] ")
	header := fmt.Sprintf("      %*s %-*s %-*s %*s %*s %*s %*s",
		colVMID, "VMID",
		colName, "Name",
		colStatus, "Status",
		colVCPU, "vCPU",
		colCPU, "CPU%",
		colRAM, "RAM",
		colStorage, "Storage")
	if needsScrollbar {
		sb.WriteString(headerStyle.Render(header) + "  \n")
		sb.WriteString("      " + strings.Repeat("─", totalWidth) + "  \n")
	} else {
		sb.WriteString(headerStyle.Render(header) + "\n")
		sb.WriteString("      " + strings.Repeat("─", totalWidth) + "\n")
	}

	// Calculate visible range
	endPos := scrollPos + maxVisible
	if endPos > totalItems {
		endPos = totalItems
	}

	// Rows (only visible portion)
	for i := scrollPos; i < endPos; i++ {
		vm := vms[i]
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
			colStorage, FormatStorageG(vm.UsedDisk),
		)

		if i == cursorIdx {
			sb.WriteString("→ ")
		} else {
			sb.WriteString("  ")
		}

		sb.WriteString(style.Render(row))

		// Add scrollbar character if needed
		if needsScrollbar {
			rowIdx := i - scrollPos
			if rowIdx >= thumbPos && rowIdx < thumbPos+thumbSize {
				sb.WriteString(" " + scrollThumbStyle.Render("█"))
			} else {
				sb.WriteString(" " + scrollTrackStyle.Render("│"))
			}
		}
		sb.WriteString("\n")
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

	// Sort suggestions by VM name (make a copy to avoid modifying original)
	sortedSuggestions := make([]analyzer.MigrationSuggestion, len(suggestions))
	copy(sortedSuggestions, suggestions)
	sort.Slice(sortedSuggestions, func(i, j int) bool {
		return sortedSuggestions[i].VMName < sortedSuggestions[j].VMName
	})
	suggestions = sortedSuggestions

	// Column widths
	const (
		colVMID     = 6
		colName     = 24 // Server name
		colTo       = 18
		colState    = 5 // "On" or "Off"
		colCPU      = 6 // CPU% = VMCPU% * vCPU (total thread consumption)
		colHCPU     = 6 // HCPU% (host CPU% - normalized)
		colVMCPU    = 7 // VMCPU% (% of allocated vCPUs)
		colVCPU     = 5
		colRAM      = 8
		colUsedDisk = 9 // Used/actual disk
		colMaxDisk  = 9 // Max/allocated disk
	)

	totalWidth := colVMID + colName + colTo + colState + colCPU + colHCPU + colVMCPU + colVCPU + colRAM + colUsedDisk + colMaxDisk + 10

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
	header := fmt.Sprintf("  %*s %-*s %-*s %-*s %*s %*s %*s %*s %*s %*s %*s",
		colVMID, "VMID",
		colName, "Name",
		colTo, "To",
		colState, "State",
		colHCPU, "HCPU%",
		colVMCPU, "VMCPU%",
		colCPU, "CPU%",
		colVCPU, "vCPU",
		colRAM, "RAM",
		colUsedDisk, "Used",
		colMaxDisk, "Max")
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

		// State: "On" for running, "Off" for stopped
		stateStr := "Off"
		if sug.Status == "running" {
			stateStr = "On"
		}

		// VMCPU%: percentage of allocated vCPUs (already in 0-100 from resources.go)
		vmCpuStr := fmt.Sprintf("%.1f", sug.CPUUsage)

		// CPU%: total thread consumption = VMCPU% * vCPUs
		// e.g., if VMCPU% is 10% and VM has 16 vCPUs, CPU% = 160 (1.6 threads worth)
		cpuPercent := sug.CPUUsage * float64(sug.VCPUs)
		cpuStr := fmt.Sprintf("%.0f", cpuPercent)

		// HCPU%: host CPU percentage contribution
		// = VMCPU% * vCPUs / SourceCores (actual % of source host's capacity this VM uses)
		hCpuPercent := 0.0
		if sug.SourceCores > 0 {
			hCpuPercent = sug.CPUUsage * float64(sug.VCPUs) / float64(sug.SourceCores)
		}
		hCpuStr := fmt.Sprintf("%.1f", hCpuPercent)

		row := fmt.Sprintf("%*d %-*s %-*s %-*s %*s %*s %*s %*d %*s %*s %*s",
			colVMID, sug.VMID,
			colName, truncate(sug.VMName, colName),
			colTo, truncate(sug.TargetNode, colTo),
			colState, stateStr,
			colHCPU, hCpuStr,
			colVMCPU, vmCpuStr,
			colCPU, cpuStr,
			colVCPU, sug.VCPUs,
			colRAM, FormatRAMShort(sug.RAM),
			colUsedDisk, FormatStorageG(sug.UsedDisk),
			colMaxDisk, FormatStorageG(sug.MaxDisk),
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

	// Closing dashes (same as header separator)
	if needsScrollbar {
		sb.WriteString("  " + strings.Repeat("─", totalWidth) + "  \n")
	} else {
		sb.WriteString("  " + strings.Repeat("─", totalWidth) + "\n")
	}

	return sb.String()
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// RenderImpactTable renders a combined table showing before/after for all nodes
func RenderImpactTable(sourceBefore, sourceAfter analyzer.NodeState, targetsBefore, targetsAfter map[string]analyzer.NodeState, cluster *proxmox.Cluster) string {
	var sb strings.Builder

	// Column widths - increased for used/total (percent) display
	const (
		colHost    = 28 // Extended to support "kv0002-123-250-123-123 (src)"
		colVMs     = 5
		colVCPUs   = 12 // e.g., "1234 (123%)"
		colCPU     = 6
		colRAM     = 20 // e.g., "1639/2015G (81%)"
		colStorage = 20 // e.g., "31.4T/34.6T (91%)"
		colSep     = 3  // " | "
	)

	// Styles
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	labelStyle := lipgloss.NewStyle()
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("15"))
	greenStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	yellowStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("3"))

	// Calculate total width
	sectionWidth := colVMs + colVCPUs + colCPU + colRAM + colStorage + 4 // 4 spaces between columns
	totalWidth := colHost + colSep + sectionWidth + colSep + sectionWidth

	// Header row 1: Hostname | Before | After
	header1 := fmt.Sprintf("  %-*s │ %-*s │ %-*s",
		colHost, "Hostname",
		sectionWidth, "Before",
		sectionWidth, "After")
	sb.WriteString(headerStyle.Render(header1) + "\n")

	// Header row 2: column names (same color as main header)
	colHeaders := fmt.Sprintf("%*s %*s %*s %*s %*s",
		colVMs, "VMs",
		colVCPUs, "vCPUs",
		colCPU, "CPU%",
		colRAM, "RAM",
		colStorage, "Storage")
	header2 := fmt.Sprintf("  %-*s │ %s │ %s",
		colHost, "",
		colHeaders,
		colHeaders)
	sb.WriteString(headerStyle.Render(header2) + "\n")

	// Separator
	sb.WriteString("  " + strings.Repeat("─", totalWidth) + "\n")

	// Helper function to get status indicators for a node
	getStatusIndicators := func(nodeName string) string {
		if cluster == nil {
			return ""
		}
		for _, node := range cluster.Nodes {
			if node.Name == nodeName {
				return node.GetStatusIndicators()
			}
		}
		return ""
	}

	// Helper function to render a row
	renderRow := func(name string, before, after analyzer.NodeState, isSource bool) {
		// Before values
		beforeVMs := fmt.Sprintf("%*d", colVMs, before.VMCount)
		// vCPUs with percentage of host threads
		beforeVCPUPct := 0.0
		if before.CPUCores > 0 {
			beforeVCPUPct = float64(before.VCPUs) / float64(before.CPUCores) * 100
		}
		beforeVCPUs := fmt.Sprintf("%*s", colVCPUs, fmt.Sprintf("%d (%.0f%%)", before.VCPUs, beforeVCPUPct))
		beforeCPU := fmt.Sprintf("%*.1f", colCPU-1, before.CPUPercent) + "%"
		beforeRAMStr := FormatUsedTotalPercent(before.RAMUsed, before.RAMTotal, before.RAMPercent)
		beforeRAM := fmt.Sprintf("%*s", colRAM, beforeRAMStr)
		beforeStorageStr := FormatUsedTotalPercentStorage(before.StorageUsed, before.StorageTotal, before.StoragePercent)
		beforeStorage := fmt.Sprintf("%*s", colStorage, beforeStorageStr)

		// After values
		afterVMs := fmt.Sprintf("%*d", colVMs, after.VMCount)
		// vCPUs with percentage of host threads
		afterVCPUPct := 0.0
		if after.CPUCores > 0 {
			afterVCPUPct = float64(after.VCPUs) / float64(after.CPUCores) * 100
		}
		afterVCPUs := fmt.Sprintf("%*s", colVCPUs, fmt.Sprintf("%d (%.0f%%)", after.VCPUs, afterVCPUPct))
		afterCPU := fmt.Sprintf("%*.1f", colCPU-1, after.CPUPercent) + "%"
		afterRAMStr := FormatUsedTotalPercent(after.RAMUsed, after.RAMTotal, after.RAMPercent)
		afterRAM := fmt.Sprintf("%*s", colRAM, afterRAMStr)
		afterStorageStr := FormatUsedTotalPercentStorage(after.StorageUsed, after.StorageTotal, after.StoragePercent)
		afterStorage := fmt.Sprintf("%*s", colStorage, afterStorageStr)

		// Determine color based on improvement (green = better, yellow = worse)
		vmsDiff := after.VMCount - before.VMCount
		cpuDiff := after.CPUPercent - before.CPUPercent

		// For source node: decrease is good (green), for target: increase is expected (yellow)
		var afterVMsStyle, afterCPUStyle lipgloss.Style
		if isSource {
			// Source: less VMs/CPU is good
			if vmsDiff < 0 {
				afterVMsStyle = greenStyle
			} else {
				afterVMsStyle = valueStyle
			}
			if cpuDiff < -0.1 {
				afterCPUStyle = greenStyle
			} else {
				afterCPUStyle = valueStyle
			}
		} else {
			// Target: more VMs is expected (show in yellow)
			if vmsDiff > 0 {
				afterVMsStyle = yellowStyle
			} else {
				afterVMsStyle = valueStyle
			}
			if cpuDiff > 0.1 {
				afterCPUStyle = yellowStyle
			} else {
				afterCPUStyle = valueStyle
			}
		}

		beforeSection := fmt.Sprintf("%s %s %s %s %s",
			beforeVMs, beforeVCPUs, beforeCPU, beforeRAM, beforeStorage)
		afterSection := afterVMsStyle.Render(afterVMs) + " " +
			valueStyle.Render(afterVCPUs) + " " +
			afterCPUStyle.Render(afterCPU) + " " +
			valueStyle.Render(afterRAM) + " " +
			valueStyle.Render(afterStorage)

		row := fmt.Sprintf("  %-*s │ %s │ %s",
			colHost, truncate(name, colHost),
			labelStyle.Render(beforeSection),
			afterSection)
		sb.WriteString(row + "\n")
	}

	// Render source node first
	sourceFlags := getStatusIndicators(sourceBefore.Name)
	sourceLabel := sourceBefore.Name + " (src)"
	if sourceFlags != "" {
		sourceLabel = sourceBefore.Name + " (" + sourceFlags + ",src)"
	}
	renderRow(sourceLabel, sourceBefore, sourceAfter, true)

	// Render target nodes (sorted)
	var targetNames []string
	for name := range targetsAfter {
		targetNames = append(targetNames, name)
	}
	sort.Strings(targetNames)

	for _, name := range targetNames {
		afterState := targetsAfter[name]
		beforeState := targetsBefore[name]
		// Only show targets that receive VMs
		if afterState.VMCount != beforeState.VMCount {
			targetFlags := getStatusIndicators(name)
			targetLabel := name
			if targetFlags != "" {
				targetLabel = name + " (" + targetFlags + ")"
			}
			renderRow(targetLabel, beforeState, afterState, false)
		}
	}

	// Closing line
	sb.WriteString("  " + strings.Repeat("─", totalWidth) + "\n")

	return sb.String()
}

// RenderImpactTableWithCursor renders the impact table with cursor highlighting
func RenderImpactTableWithCursor(sourceBefore, sourceAfter analyzer.NodeState, targetsBefore, targetsAfter map[string]analyzer.NodeState, cursorPos int, cluster *proxmox.Cluster) string {
	var sb strings.Builder

	// Column widths - increased for used/total (percent) display
	const (
		colHost    = 28
		colVMs     = 5
		colVCPUs   = 12 // e.g., "1234 (123%)"
		colCPU     = 6
		colRAM     = 20 // e.g., "1639/2015G (81%)"
		colStorage = 20 // e.g., "31.4T/34.6T (91%)"
		colSep     = 3
	)

	// Styles
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	labelStyle := lipgloss.NewStyle()
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("15"))
	greenStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	yellowStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	selectedStyle := lipgloss.NewStyle().Background(lipgloss.Color("236")).Foreground(lipgloss.Color("15")).Bold(true)

	// Calculate total width
	sectionWidth := colVMs + colVCPUs + colCPU + colRAM + colStorage + 4
	totalWidth := colHost + colSep + sectionWidth + colSep + sectionWidth

	// Header row 1
	header1 := fmt.Sprintf("  %-*s │ %-*s │ %-*s",
		colHost, "Hostname",
		sectionWidth, "Before",
		sectionWidth, "After")
	sb.WriteString(headerStyle.Render(header1) + "\n")

	// Header row 2
	colHeaders := fmt.Sprintf("%*s %*s %*s %*s %*s",
		colVMs, "VMs",
		colVCPUs, "vCPUs",
		colCPU, "CPU%",
		colRAM, "RAM",
		colStorage, "Storage")
	header2 := fmt.Sprintf("  %-*s │ %s │ %s",
		colHost, "",
		colHeaders,
		colHeaders)
	sb.WriteString(headerStyle.Render(header2) + "\n")

	// Separator
	sb.WriteString("  " + strings.Repeat("─", totalWidth) + "\n")

	rowIndex := 0

	// Helper function to get status indicators for a node
	getStatusIndicators := func(nodeName string) string {
		if cluster == nil {
			return ""
		}
		for _, node := range cluster.Nodes {
			if node.Name == nodeName {
				return node.GetStatusIndicators()
			}
		}
		return ""
	}

	// Helper function to render a row with optional cursor highlight
	renderRowWithCursor := func(name string, before, after analyzer.NodeState, isSource bool) {
		isSelected := (rowIndex == cursorPos)

		// Build row content
		beforeVMs := fmt.Sprintf("%*d", colVMs, before.VMCount)
		// vCPUs with percentage of host threads
		beforeVCPUPct := 0.0
		if before.CPUCores > 0 {
			beforeVCPUPct = float64(before.VCPUs) / float64(before.CPUCores) * 100
		}
		beforeVCPUs := fmt.Sprintf("%*s", colVCPUs, fmt.Sprintf("%d (%.0f%%)", before.VCPUs, beforeVCPUPct))
		beforeCPU := fmt.Sprintf("%*.1f", colCPU-1, before.CPUPercent) + "%"
		beforeRAMStr := FormatUsedTotalPercent(before.RAMUsed, before.RAMTotal, before.RAMPercent)
		beforeRAM := fmt.Sprintf("%*s", colRAM, beforeRAMStr)
		beforeStorageStr := FormatUsedTotalPercentStorage(before.StorageUsed, before.StorageTotal, before.StoragePercent)
		beforeStorage := fmt.Sprintf("%*s", colStorage, beforeStorageStr)

		afterVMs := fmt.Sprintf("%*d", colVMs, after.VMCount)
		// vCPUs with percentage of host threads
		afterVCPUPct := 0.0
		if after.CPUCores > 0 {
			afterVCPUPct = float64(after.VCPUs) / float64(after.CPUCores) * 100
		}
		afterVCPUs := fmt.Sprintf("%*s", colVCPUs, fmt.Sprintf("%d (%.0f%%)", after.VCPUs, afterVCPUPct))
		afterCPU := fmt.Sprintf("%*.1f", colCPU-1, after.CPUPercent) + "%"
		afterRAMStr := FormatUsedTotalPercent(after.RAMUsed, after.RAMTotal, after.RAMPercent)
		afterRAM := fmt.Sprintf("%*s", colRAM, afterRAMStr)
		afterStorageStr := FormatUsedTotalPercentStorage(after.StorageUsed, after.StorageTotal, after.StoragePercent)
		afterStorage := fmt.Sprintf("%*s", colStorage, afterStorageStr)

		beforeSection := fmt.Sprintf("%s %s %s %s %s", beforeVMs, beforeVCPUs, beforeCPU, beforeRAM, beforeStorage)
		afterSection := fmt.Sprintf("%s %s %s %s %s", afterVMs, afterVCPUs, afterCPU, afterRAM, afterStorage)

		// Selector indicator
		selector := "  "
		if isSelected {
			selector = "▶ "
		}

		if isSelected {
			// Render entire row with highlight
			rowContent := fmt.Sprintf("%-*s │ %s │ %s",
				colHost, truncate(name, colHost),
				beforeSection,
				afterSection)
			// Pad to full width
			if len(rowContent) < totalWidth {
				rowContent += strings.Repeat(" ", totalWidth-len(rowContent))
			}
			sb.WriteString(selector + selectedStyle.Render(rowContent) + "\n")
		} else {
			// Normal rendering with colors
			vmsDiff := after.VMCount - before.VMCount
			cpuDiff := after.CPUPercent - before.CPUPercent

			var afterVMsStyle, afterCPUStyle lipgloss.Style
			if isSource {
				if vmsDiff < 0 {
					afterVMsStyle = greenStyle
				} else {
					afterVMsStyle = valueStyle
				}
				if cpuDiff < -0.1 {
					afterCPUStyle = greenStyle
				} else {
					afterCPUStyle = valueStyle
				}
			} else {
				if vmsDiff > 0 {
					afterVMsStyle = yellowStyle
				} else {
					afterVMsStyle = valueStyle
				}
				if cpuDiff > 0.1 {
					afterCPUStyle = yellowStyle
				} else {
					afterCPUStyle = valueStyle
				}
			}

			afterSectionStyled := afterVMsStyle.Render(afterVMs) + " " +
				valueStyle.Render(afterVCPUs) + " " +
				afterCPUStyle.Render(afterCPU) + " " +
				valueStyle.Render(afterRAM) + " " +
				valueStyle.Render(afterStorage)

			row := fmt.Sprintf("%s%-*s │ %s │ %s",
				selector,
				colHost, truncate(name, colHost),
				labelStyle.Render(beforeSection),
				afterSectionStyled)
			sb.WriteString(row + "\n")
		}

		rowIndex++
	}

	// Render source node first
	sourceFlags := getStatusIndicators(sourceBefore.Name)
	sourceLabel := sourceBefore.Name + " (src)"
	if sourceFlags != "" {
		sourceLabel = sourceBefore.Name + " (" + sourceFlags + ",src)"
	}
	renderRowWithCursor(sourceLabel, sourceBefore, sourceAfter, true)

	// Render target nodes (sorted)
	var targetNames []string
	for name := range targetsAfter {
		targetNames = append(targetNames, name)
	}
	sort.Strings(targetNames)

	for _, name := range targetNames {
		afterState := targetsAfter[name]
		beforeState := targetsBefore[name]
		if afterState.VMCount != beforeState.VMCount {
			targetFlags := getStatusIndicators(name)
			targetLabel := name
			if targetFlags != "" {
				targetLabel = name + " (" + targetFlags + ")"
			}
			renderRowWithCursor(targetLabel, beforeState, afterState, false)
		}
	}

	// Closing line
	sb.WriteString("  " + strings.Repeat("─", totalWidth) + "\n")

	return sb.String()
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
