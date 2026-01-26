package views

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/yourusername/migsug/internal/analyzer"
	"github.com/yourusername/migsug/internal/proxmox"
)

// Box drawing characters
const (
	criteriaBoxHoriz = "━"
	criteriaBoxThin  = "─"
)

// CriteriaState holds the state for criteria input
type CriteriaState struct {
	SelectedMode   analyzer.MigrationMode
	VMCount        string
	VCPUCount      string
	CPUUsage       string
	RAMAmount      string
	StorageAmount  string
	SelectedVMs    map[int]bool
	ExcludeNodes   []string
	CursorPosition int
	InputFocused   bool
	ErrorMessage   string // Validation error message to display
}

// RenderCriteria renders the criteria selection view (without node data)
func RenderCriteria(state CriteriaState, sourceNode string, width int) string {
	return RenderCriteriaWithNode(state, sourceNode, nil, width)
}

// RenderCriteriaWithNode renders the criteria selection view with node data
func RenderCriteriaWithNode(state CriteriaState, sourceNode string, node *proxmox.Node, width int) string {
	return RenderCriteriaFull(state, sourceNode, node, nil, "", width)
}

// RenderCriteriaFull renders the criteria selection view with full header like dashboard
func RenderCriteriaFull(state CriteriaState, sourceNode string, node *proxmox.Node, cluster *proxmox.Cluster, version string, width int) string {
	var sb strings.Builder

	// Ensure minimum width
	if width < 80 {
		width = 100
	}

	// Title with version (same as dashboard)
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("5"))
	versionStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	borderStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("6"))

	title := "KVM Migration Suggester"
	if version != "" && version != "dev" {
		title += " " + versionStyle.Render("v"+version)
	}
	sb.WriteString(titleStyle.Render(title) + "\n")

	// Graphical top border
	sb.WriteString(borderStyle.Render(strings.Repeat(criteriaBoxHoriz, width)) + "\n\n")

	// Cluster summary (same as dashboard) if cluster is available
	if cluster != nil {
		sb.WriteString(renderClusterSummary(cluster, width))
		sb.WriteString("\n")
	}

	// Selected source node instruction with CPU info
	valueStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	nodeInfoStr := sourceNode
	if node != nil && node.CPUModel != "" {
		nodeInfoStr = fmt.Sprintf("%s %s(%s, %d threads)", sourceNode,
			dimStyle.Render(""),
			dimStyle.Render(node.CPUModel),
			node.CPUCores)
	} else if node != nil {
		nodeInfoStr = fmt.Sprintf("%s %s(%d threads)", sourceNode,
			dimStyle.Render(""),
			node.CPUCores)
	}
	sb.WriteString("Selected source node: " + valueStyle.Render(nodeInfoStr) + "\n")

	// Show selected host data if available
	if node != nil {
		sb.WriteString(renderNodeSummary(node, width))
	}
	sb.WriteString("\n")

	// Instructions
	sb.WriteString("Select migration mode:\n\n")

	// Mode options table header
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	header := fmt.Sprintf("  %-20s%s", "Mode", "Description")
	sb.WriteString(headerStyle.Render(header) + "\n")
	sb.WriteString("  " + borderStyle.Render(strings.Repeat(criteriaBoxHoriz, width-4)) + "\n")

	// Mode options with descriptions
	modes := []struct {
		mode analyzer.MigrationMode
		name string
		desc string
	}{
		{analyzer.ModeVMCount, "VM", "Migrate a specific number of VMs"},
		{analyzer.ModeVCPU, "vCPU", "Migrate VMs based on the count of vCPUs to migrate"},
		{analyzer.ModeCPUUsage, "CPU Usage (%)", "Migrate VMs based on the CPU Usage Percentage to migrate from the Host"},
		{analyzer.ModeRAM, "RAM (GiB)", "Migrate VMs based on the amount of GiB RAM to migrate from the Host"},
		{analyzer.ModeStorage, "Storage (GiB)", "Migrate VMs based on the amount of GiB of VM Storage to migrate from Host"},
		{analyzer.ModeSpecific, "Specific VMs", "Manually select which VMs to migrate"},
	}

	// Styles for selection
	selectedBgStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("236")).
		Foreground(lipgloss.Color("15")).
		Bold(true)

	for i, m := range modes {
		isCurrentMode := state.CursorPosition == i
		isNavigating := !state.InputFocused

		// Build row content
		name := fmt.Sprintf("%-20s", m.name)
		desc := fmt.Sprintf("%-50s", m.desc)

		// Selector indicator - show arrow when navigating, checkmark when mode is selected
		selector := "  "
		if isCurrentMode && isNavigating {
			selector = "▶ "
		} else if isCurrentMode && state.InputFocused {
			selector = "✓ "
		}

		// Apply styling - highlight current mode even when input is focused
		if isCurrentMode {
			rowContent := fmt.Sprintf("%s%s", name, desc)
			// Pad to full width
			if len(rowContent) < width-4 {
				rowContent += strings.Repeat(" ", width-4-len(rowContent))
			}
			sb.WriteString(selector + selectedBgStyle.Render(rowContent) + "\n")
		} else {
			sb.WriteString(selector + name + dimStyle.Render(desc) + "\n")
		}
	}

	sb.WriteString("\n")
	sb.WriteString(borderStyle.Render(strings.Repeat(criteriaBoxThin, width)) + "\n\n")

	// Input field based on selected mode
	inputLabel := ""
	inputValue := ""
	inputSuffix := ""
	showInput := true

	switch state.SelectedMode {
	case analyzer.ModeVMCount:
		inputLabel = "Number of VMs to migrate"
		inputValue = state.VMCount
	case analyzer.ModeVCPU:
		inputLabel = "Number of vCPUs to migrate"
		inputValue = state.VCPUCount
	case analyzer.ModeCPUUsage:
		inputLabel = "CPU usage percentage to free"
		inputValue = state.CPUUsage
		inputSuffix = "%"
	case analyzer.ModeRAM:
		inputLabel = "RAM to free (GiB)"
		inputValue = state.RAMAmount
	case analyzer.ModeStorage:
		inputLabel = "Storage to free (GiB)"
		inputValue = state.StorageAmount
	case analyzer.ModeSpecific:
		showInput = false
		noteStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Italic(true)
		sb.WriteString(noteStyle.Render("  ℹ Press Enter to select specific VMs on the next screen") + "\n")
	}

	if showInput {
		labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("7"))
		inputStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))
		suffixStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
		errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)

		// Display value with cursor when focused
		displayValue := inputValue
		if state.InputFocused {
			displayValue = inputValue + "█"
		}
		if displayValue == "" && !state.InputFocused {
			displayValue = "_"
		}

		// Inline input after the label
		sb.WriteString("  " + labelStyle.Render(inputLabel+": ") + inputStyle.Render(displayValue))
		if inputSuffix != "" {
			sb.WriteString(" " + suffixStyle.Render(inputSuffix))
		}
		sb.WriteString("\n")

		// Show error message if present
		if state.ErrorMessage != "" {
			sb.WriteString("  " + errorStyle.Render("⚠ "+state.ErrorMessage) + "\n")
		}
	}

	sb.WriteString("\n")

	// Help text
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	if state.InputFocused {
		sb.WriteString(helpStyle.Render("Type value │ Enter: Confirm │ Esc: Cancel input"))
	} else {
		sb.WriteString(helpStyle.Render("↑/↓: Navigate │ Enter: Select mode │ Esc: Back to host selection │ q: Quit"))
	}

	return sb.String()
}

// renderClusterSummary creates the cluster summary (same as dashboard)
func renderClusterSummary(cluster *proxmox.Cluster, width int) string {
	var sb strings.Builder

	labelStyle := lipgloss.NewStyle() // Regular text color
	valueStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	runningStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	stoppedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	// Count online nodes
	onlineNodes := 0
	for _, node := range cluster.Nodes {
		if node.Status == "online" {
			onlineNodes++
		}
	}

	// Calculate cluster-wide storage in TiB
	totalStorageTiB := float64(cluster.TotalStorage) / (1024 * 1024 * 1024 * 1024)
	usedStorageTiB := float64(cluster.UsedStorage) / (1024 * 1024 * 1024 * 1024)
	storagePercent := 0.0
	if cluster.TotalStorage > 0 {
		storagePercent = float64(cluster.UsedStorage) / float64(cluster.TotalStorage) * 100
	}

	// Calculate RAM in GiB
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

	// Color codes for usage
	cpuColor := getUsageColor(avgCPU)
	ramColor := getUsageColor(ramPercent)
	storageColor := getUsageColor(storagePercent)

	// Fixed column widths for vertical alignment
	col1Width := 34 // "VMs:   4639 (On: 4046, Off: 593)" + 2 char spacing
	col2Width := 30 // "RAM: 49306/75927 GiB (64.9%)" needs ~30 chars

	// Row 1: Nodes, CPU, vCPUs
	nodesStr := fmt.Sprintf("%d/%d online", onlineNodes, len(cluster.Nodes))
	col1Content := fmt.Sprintf("Nodes: %s", nodesStr)
	sb.WriteString(labelStyle.Render("Nodes: ") + valueStyle.Render(nodesStr))
	sb.WriteString(strings.Repeat(" ", col1Width-len(col1Content)))

	cpuStr := fmt.Sprintf("%.1f%%", avgCPU)
	col2Content := fmt.Sprintf("CPU: %s", cpuStr)
	sb.WriteString(labelStyle.Render("CPU: ") + lipgloss.NewStyle().Foreground(lipgloss.Color(cpuColor)).Render(cpuStr))
	sb.WriteString(strings.Repeat(" ", col2Width-len(col2Content)))

	sb.WriteString(labelStyle.Render("vCPUs: ") + valueStyle.Render(fmt.Sprintf("%d", cluster.TotalVCPUs)))
	sb.WriteString("\n")

	// Row 2: VMs, RAM, Storage
	col1Row2 := fmt.Sprintf("VMs:   %d ", cluster.TotalVMs) + fmt.Sprintf("(On: %d, Off: %d)", cluster.RunningVMs, cluster.StoppedVMs)
	sb.WriteString(labelStyle.Render("VMs:   ") + valueStyle.Render(fmt.Sprintf("%d ", cluster.TotalVMs)))
	sb.WriteString(dimStyle.Render("(") + runningStyle.Render(fmt.Sprintf("On: %d", cluster.RunningVMs)) + dimStyle.Render(", "))
	sb.WriteString(stoppedStyle.Render(fmt.Sprintf("Off: %d", cluster.StoppedVMs)) + dimStyle.Render(")"))
	if len(col1Row2) < col1Width {
		sb.WriteString(strings.Repeat(" ", col1Width-len(col1Row2)))
	}

	ramValStr := fmt.Sprintf("%.0f/%.0f GiB", usedRAMGiB, totalRAMGiB)
	ramPctStr := fmt.Sprintf("(%.1f%%)", ramPercent)
	ramFull := fmt.Sprintf("RAM: %s %s", ramValStr, ramPctStr)
	sb.WriteString(labelStyle.Render("RAM: ") + valueStyle.Render(ramValStr) + " " + lipgloss.NewStyle().Foreground(lipgloss.Color(ramColor)).Render(ramPctStr))
	if len(ramFull) < col2Width {
		sb.WriteString(strings.Repeat(" ", col2Width-len(ramFull)))
	}

	sb.WriteString(labelStyle.Render("Storage: ") + valueStyle.Render(fmt.Sprintf("%.0f/%.0f TiB", usedStorageTiB, totalStorageTiB)))
	sb.WriteString(" " + lipgloss.NewStyle().Foreground(lipgloss.Color(storageColor)).Render(fmt.Sprintf("(%.1f%%)", storagePercent)))
	sb.WriteString("\n")

	return sb.String()
}

// getUsageColor returns color code based on usage percentage
func getUsageColor(percent float64) string {
	if percent >= 87 {
		return "9" // bright red
	} else if percent >= 80 {
		return "3" // yellow
	}
	return "2" // green
}

// renderNodeSummary displays the selected node's summary info
func renderNodeSummary(node *proxmox.Node, width int) string {
	var sb strings.Builder

	labelStyle := lipgloss.NewStyle() // Regular text color
	valueStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))

	// Count running VMs
	runningVMs := 0
	stoppedVMs := 0
	runningVCPUs := 0
	for _, vm := range node.VMs {
		if vm.Status == "running" {
			runningVMs++
			runningVCPUs += vm.CPUCores
		} else {
			stoppedVMs++
		}
	}

	// Fixed column widths for vertical alignment
	col1Width := 30 // "VMs: 158 (On: 143, Off: 15)" + 2 char spacing
	col2Width := 24 // "RAM: 1641/2015G (81%)" + 2 char spacing

	// Format values
	vmStr := fmt.Sprintf("%d (On: %d, Off: %d)", len(node.VMs), runningVMs, stoppedVMs)
	cpuStr := fmt.Sprintf("%.1f%%", node.GetCPUPercent())

	// vCPU with overcommit percentage
	vcpuOvercommit := 0.0
	if node.CPUCores > 0 {
		vcpuOvercommit = float64(runningVCPUs) / float64(node.CPUCores) * 100
	}
	vcpuStr := fmt.Sprintf("%d (%.0f%%)", runningVCPUs, vcpuOvercommit)

	// Load average with percentage
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

	// RAM
	ramUsedGiB := float64(node.UsedMem) / (1024 * 1024 * 1024)
	ramTotalGiB := float64(node.MaxMem) / (1024 * 1024 * 1024)
	ramStr := fmt.Sprintf("%.0f/%.0fG (%.0f%%)", ramUsedGiB, ramTotalGiB, node.GetMemPercent())

	// Disk
	diskUsedTiB := float64(node.UsedDisk) / (1024 * 1024 * 1024 * 1024)
	diskTotalTiB := float64(node.MaxDisk) / (1024 * 1024 * 1024 * 1024)
	diskStr := fmt.Sprintf("%.0f/%.0fT (%.0f%%)", diskUsedTiB, diskTotalTiB, node.GetDiskPercent())

	// Row 1: VMs, CPU, vCPUs
	sb.WriteString("  ")
	col1Row1 := fmt.Sprintf("VMs: %s", vmStr)
	sb.WriteString(labelStyle.Render("VMs: ") + valueStyle.Render(vmStr))
	if len(col1Row1) < col1Width {
		sb.WriteString(strings.Repeat(" ", col1Width-len(col1Row1)))
	}

	col2Row1 := fmt.Sprintf("CPU: %s", cpuStr)
	sb.WriteString(labelStyle.Render("CPU: ") + valueStyle.Render(cpuStr))
	if len(col2Row1) < col2Width {
		sb.WriteString(strings.Repeat(" ", col2Width-len(col2Row1)))
	}

	sb.WriteString(labelStyle.Render("vCPUs: ") + valueStyle.Render(vcpuStr))
	sb.WriteString("\n")

	// Row 2: LA, RAM, Disk
	sb.WriteString("  ")
	col1Row2 := fmt.Sprintf("LA: %s", laStr)
	sb.WriteString(labelStyle.Render("LA: ") + valueStyle.Render(laStr))
	if len(col1Row2) < col1Width {
		sb.WriteString(strings.Repeat(" ", col1Width-len(col1Row2)))
	}

	col2Row2 := fmt.Sprintf("RAM: %s", ramStr)
	sb.WriteString(labelStyle.Render("RAM: ") + valueStyle.Render(ramStr))
	if len(col2Row2) < col2Width {
		sb.WriteString(strings.Repeat(" ", col2Width-len(col2Row2)))
	}

	sb.WriteString(labelStyle.Render("Disk: ") + valueStyle.Render(diskStr))
	sb.WriteString("\n")

	return sb.String()
}

// renderUnicodeInputBox creates a Unicode input box
func renderUnicodeInputBox(value string, suffix string, focused bool, width int) string {
	var sb strings.Builder

	// Input box width
	boxWidth := 25

	// Display value with cursor
	displayValue := value
	if focused {
		displayValue = value + "█"
	}
	if displayValue == "" && !focused {
		displayValue = "     " // placeholder
	}

	// Pad to fill box
	if len(displayValue) < boxWidth-4 {
		displayValue = displayValue + strings.Repeat(" ", boxWidth-4-len(displayValue))
	} else if len(displayValue) > boxWidth-4 {
		displayValue = displayValue[:boxWidth-4]
	}

	// Choose colors based on focus
	borderColor := "240"
	if focused {
		borderColor = "3" // yellow when focused
	}

	borderStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(borderColor))
	textStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Bold(true)
	suffixStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	// Unicode box
	topBorder := "┌" + strings.Repeat("─", boxWidth-2) + "┐"
	bottomBorder := "└" + strings.Repeat("─", boxWidth-2) + "┘"

	sb.WriteString("  " + borderStyle.Render(topBorder) + "\n")
	sb.WriteString("  " + borderStyle.Render("│") + " " + textStyle.Render(displayValue) + " " + borderStyle.Render("│"))
	if suffix != "" {
		sb.WriteString(" " + suffixStyle.Render(suffix))
	}
	sb.WriteString("\n")
	sb.WriteString("  " + borderStyle.Render(bottomBorder) + "\n")

	return sb.String()
}
