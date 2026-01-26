package ui

import (
	"fmt"
	"sort"
	"strconv"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yourusername/migsug/internal/analyzer"
	"github.com/yourusername/migsug/internal/proxmox"
	"github.com/yourusername/migsug/internal/ui/components"
	"github.com/yourusername/migsug/internal/ui/views"
)

const refreshInterval = 180 // seconds

// ViewType represents the current view
type ViewType int

const (
	ViewDashboard ViewType = iota
	ViewCriteria
	ViewVMSelection
	ViewAnalyzing
	ViewResults
	ViewError
	ViewHelp
)

// SortColumn represents which column to sort by
type SortColumn int

const (
	SortByName SortColumn = iota
	SortByStatus
	SortByVMs
	SortByVCPUs
	SortByCPUPercent
	SortByLA
	SortByRAM
	SortByDisk
)

// Model is the main application model
type Model struct {
	cluster     *proxmox.Cluster
	client      proxmox.ProxmoxClient
	currentView ViewType
	err         error
	version     string // Application version

	// Dashboard state
	selectedNodeIdx int
	sourceNode      string
	sortColumn      SortColumn // Current sort column
	sortAsc         bool       // Sort ascending (true) or descending (false)

	// Criteria state
	criteriaState views.CriteriaState

	// VM selection state (for ModeSpecific)
	vmCursorIdx int

	// Analysis results
	result *analyzer.AnalysisResult

	// Results view scroll state
	resultsScrollPos int
	resultsCursorPos int // Current cursor position in results list

	// UI state
	width      int
	height     int
	showHelp   bool
	loading    bool
	loadingMsg string

	// Auto-refresh state
	refreshCountdown int    // seconds until next refresh
	refreshing       bool   // true when actively refreshing data
	refreshProgress  string // progress message during refresh
	refreshCurrent   int    // current progress count
	refreshTotal     int    // total items to refresh
}

// NewModel creates a new application model
func NewModel(cluster *proxmox.Cluster, client proxmox.ProxmoxClient) Model {
	return NewModelWithVersion(cluster, client, "dev")
}

// NewModelWithVersion creates a new application model with version info
func NewModelWithVersion(cluster *proxmox.Cluster, client proxmox.ProxmoxClient, version string) Model {
	return Model{
		cluster:         cluster,
		client:          client,
		currentView:     ViewDashboard,
		selectedNodeIdx: 0,
		version:         version,
		criteriaState: views.CriteriaState{
			SelectedMode: analyzer.ModeVMCount,
			SelectedVMs:  make(map[int]bool),
		},
		width:            80,
		height:           24,
		refreshCountdown: refreshInterval,
	}
}

// Init initializes the model
func (m Model) Init() tea.Cmd {
	return tickCmd()
}

// tickCmd returns a command that sends a tick message every second
func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// tickMsg is sent every second for the refresh countdown
type tickMsg time.Time

// refreshCompleteMsg is sent when cluster data refresh is complete
type refreshCompleteMsg struct {
	cluster *proxmox.Cluster
	err     error
}

// refreshProgressMsg is sent to update refresh progress
type refreshProgressMsg struct {
	stage   string
	current int
	total   int
}

// Update handles messages and updates the model
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKeyPress(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tickMsg:
		// Only decrement countdown on dashboard view
		if m.currentView == ViewDashboard && !m.refreshing {
			m.refreshCountdown--
			if m.refreshCountdown <= 0 {
				// Start refresh with initial progress info
				m.refreshing = true
				m.refreshProgress = fmt.Sprintf("Refreshing %d nodes", len(m.cluster.Nodes))
				m.refreshTotal = len(m.cluster.Nodes)
				m.refreshCurrent = 0
				return m, tea.Batch(tickCmd(), m.refreshClusterData())
			}
		}
		return m, tickCmd()

	case refreshProgressMsg:
		m.refreshProgress = msg.stage
		m.refreshCurrent = msg.current
		m.refreshTotal = msg.total
		return m, nil

	case refreshCompleteMsg:
		m.refreshing = false
		m.refreshCountdown = refreshInterval
		m.refreshProgress = ""
		m.refreshCurrent = 0
		m.refreshTotal = 0
		if msg.err == nil && msg.cluster != nil {
			m.cluster = msg.cluster
			// Re-apply current sort order to new data
			m.sortNodes()
			// Keep selection valid
			if m.selectedNodeIdx >= len(m.cluster.Nodes) {
				m.selectedNodeIdx = len(m.cluster.Nodes) - 1
			}
			if m.selectedNodeIdx < 0 {
				m.selectedNodeIdx = 0
			}
		}
		return m, nil

	case errMsg:
		m.err = msg.err
		m.currentView = ViewError
		m.loading = false
		return m, nil

	case analysisCompleteMsg:
		m.result = msg.result
		m.currentView = ViewResults
		m.loading = false
		m.resultsScrollPos = 0
		m.resultsCursorPos = 0
		return m, nil
	}

	return m, nil
}

// refreshClusterData creates a command to refresh cluster data
func (m Model) refreshClusterData() tea.Cmd {
	// Get current node count for progress display
	nodeCount := len(m.cluster.Nodes)

	return func() tea.Msg {
		// Note: We can't easily send progress updates from here in Bubble Tea
		// The progress is shown during initial load in main.go
		// During refresh, we just show "Refreshing X nodes..."
		_ = nodeCount // Used for context

		cluster, err := proxmox.CollectClusterData(m.client)
		return refreshCompleteMsg{cluster: cluster, err: err}
	}
}

// handleKeyPress handles keyboard input
func (m Model) handleKeyPress(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Global keys
	switch msg.String() {
	case "ctrl+c", "q":
		if m.currentView != ViewCriteria || !m.criteriaState.InputFocused {
			return m, tea.Quit
		}
	case "?":
		m.showHelp = !m.showHelp
		return m, nil
	}

	// View-specific keys
	switch m.currentView {
	case ViewDashboard:
		return m.handleDashboardKeys(msg)
	case ViewCriteria:
		return m.handleCriteriaKeys(msg)
	case ViewVMSelection:
		return m.handleVMSelectionKeys(msg)
	case ViewResults:
		return m.handleResultsKeys(msg)
	case ViewError:
		return m.handleErrorKeys(msg)
	}

	return m, nil
}

func (m Model) handleDashboardKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	nodeCount := len(m.cluster.Nodes)
	pageSize := 10 // Number of nodes to jump with PgUp/PgDown

	switch msg.String() {
	case "up", "k":
		if m.selectedNodeIdx > 0 {
			m.selectedNodeIdx--
		}
	case "down", "j":
		if m.selectedNodeIdx < nodeCount-1 {
			m.selectedNodeIdx++
		}
	case "home":
		m.selectedNodeIdx = 0
	case "end":
		m.selectedNodeIdx = nodeCount - 1
	case "pgup":
		m.selectedNodeIdx -= pageSize
		if m.selectedNodeIdx < 0 {
			m.selectedNodeIdx = 0
		}
	case "pgdown":
		m.selectedNodeIdx += pageSize
		if m.selectedNodeIdx >= nodeCount {
			m.selectedNodeIdx = nodeCount - 1
		}
	case "enter":
		// Select source node and move to criteria
		m.sourceNode = m.cluster.Nodes[m.selectedNodeIdx].Name
		m.criteriaState = views.CriteriaState{
			SelectedMode: analyzer.ModeVMCount,
			SelectedVMs:  make(map[int]bool),
		}
		m.currentView = ViewCriteria
		return m, tea.ClearScreen
	case "1":
		m.toggleSort(SortByName)
	case "2":
		m.toggleSort(SortByStatus)
	case "3":
		m.toggleSort(SortByVMs)
	case "4":
		m.toggleSort(SortByVCPUs)
	case "5":
		m.toggleSort(SortByCPUPercent)
	case "6":
		m.toggleSort(SortByLA)
	case "7":
		m.toggleSort(SortByRAM)
	case "8":
		m.toggleSort(SortByDisk)
	case "r":
		// Manual refresh
		if !m.refreshing {
			m.refreshing = true
			m.refreshProgress = fmt.Sprintf("Refreshing %d nodes", len(m.cluster.Nodes))
			m.refreshTotal = len(m.cluster.Nodes)
			m.refreshCurrent = 0
			return m, m.refreshClusterData()
		}
	}
	return m, nil
}

// toggleSort toggles the sort column and direction
func (m *Model) toggleSort(col SortColumn) {
	if m.sortColumn == col {
		// Same column - toggle direction
		m.sortAsc = !m.sortAsc
	} else {
		// New column - set ascending by default
		m.sortColumn = col
		m.sortAsc = true
	}
	m.sortNodes()
	m.selectedNodeIdx = 0 // Reset selection after sort
}

// sortNodes sorts the cluster nodes based on current sort settings
func (m *Model) sortNodes() {
	nodes := m.cluster.Nodes
	sort.Slice(nodes, func(i, j int) bool {
		var less bool
		switch m.sortColumn {
		case SortByName:
			less = nodes[i].Name < nodes[j].Name
		case SortByStatus:
			less = nodes[i].Status < nodes[j].Status
		case SortByVMs:
			less = len(nodes[i].VMs) < len(nodes[j].VMs)
		case SortByVCPUs:
			less = nodes[i].GetRunningVCPUs() < nodes[j].GetRunningVCPUs()
		case SortByCPUPercent:
			less = nodes[i].GetCPUPercent() < nodes[j].GetCPUPercent()
		case SortByLA:
			// Compare 1-minute load average
			laI := 0.0
			laJ := 0.0
			if len(nodes[i].LoadAverage) > 0 {
				laI = nodes[i].LoadAverage[0]
			}
			if len(nodes[j].LoadAverage) > 0 {
				laJ = nodes[j].LoadAverage[0]
			}
			less = laI < laJ
		case SortByRAM:
			less = nodes[i].GetMemPercent() < nodes[j].GetMemPercent()
		case SortByDisk:
			less = nodes[i].GetDiskPercent() < nodes[j].GetDiskPercent()
		default:
			less = nodes[i].Name < nodes[j].Name
		}
		if m.sortAsc {
			return less
		}
		return !less
	})
}

func (m Model) handleCriteriaKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.criteriaState.InputFocused {
		return m.handleCriteriaInput(msg)
	}

	switch msg.String() {
	case "up", "k":
		if m.criteriaState.CursorPosition > 0 {
			m.criteriaState.CursorPosition--
		}
	case "down", "j":
		if m.criteriaState.CursorPosition < 5 {
			m.criteriaState.CursorPosition++
		}
	case "enter":
		// Select mode
		m.criteriaState.SelectedMode = analyzer.MigrationMode(m.criteriaState.CursorPosition)

		// If specific VMs mode, go to VM selection
		if m.criteriaState.SelectedMode == analyzer.ModeSpecific {
			m.currentView = ViewVMSelection
			m.vmCursorIdx = 0
			return m, tea.ClearScreen
		}
		// Otherwise, enable input
		m.criteriaState.InputFocused = true
	case "esc":
		m.currentView = ViewDashboard
		return m, tea.ClearScreen
	}
	return m, nil
}

func (m Model) handleCriteriaInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		// Clear previous error
		m.criteriaState.ErrorMessage = ""

		// Validate input before starting analysis
		validationErr := m.validateCriteriaInput()
		if validationErr != "" {
			m.criteriaState.ErrorMessage = validationErr
			return m, nil
		}

		// Validate and start analysis
		m.criteriaState.InputFocused = false
		return m, tea.Batch(tea.ClearScreen, m.startAnalysis())
	case "esc":
		m.criteriaState.InputFocused = false
		m.criteriaState.ErrorMessage = ""
	case "backspace", "ctrl+h", "delete":
		m.deleteLastChar()
		m.criteriaState.ErrorMessage = "" // Clear error on edit
	default:
		// Only allow digits and decimal point
		if len(msg.String()) == 1 {
			m.appendToInput(msg.String())
			m.criteriaState.ErrorMessage = "" // Clear error on edit
		}
	}
	return m, nil
}

// validateCriteriaInput validates the input value and returns an error message if invalid
func (m *Model) validateCriteriaInput() string {
	switch m.criteriaState.SelectedMode {
	case analyzer.ModeVMCount:
		if m.criteriaState.VMCount == "" {
			return "Please enter a number of VMs"
		}
		count, err := strconv.Atoi(m.criteriaState.VMCount)
		if err != nil {
			return "Invalid number"
		}
		if count <= 0 {
			return "VM count must be greater than 0"
		}
		// Check against actual VMs on source node
		sourceNode := proxmox.GetNodeByName(m.cluster, m.sourceNode)
		if sourceNode != nil && count > len(sourceNode.VMs) {
			return fmt.Sprintf("VM count exceeds available VMs (%d)", len(sourceNode.VMs))
		}

	case analyzer.ModeVCPU:
		if m.criteriaState.VCPUCount == "" {
			return "Please enter a vCPU count"
		}
		count, err := strconv.Atoi(m.criteriaState.VCPUCount)
		if err != nil {
			return "Invalid number"
		}
		if count <= 0 {
			return "vCPU count must be greater than 0"
		}

	case analyzer.ModeCPUUsage:
		if m.criteriaState.CPUUsage == "" {
			return "Please enter a CPU usage percentage"
		}
		usage, err := strconv.ParseFloat(m.criteriaState.CPUUsage, 64)
		if err != nil {
			return "Invalid number"
		}
		if usage <= 0 {
			return "CPU usage must be greater than 0%"
		}
		if usage > 100 {
			return "CPU usage cannot exceed 100%"
		}

	case analyzer.ModeRAM:
		if m.criteriaState.RAMAmount == "" {
			return "Please enter a RAM amount in GB"
		}
		amount, err := strconv.ParseFloat(m.criteriaState.RAMAmount, 64)
		if err != nil {
			return "Invalid number"
		}
		if amount <= 0 {
			return "RAM amount must be greater than 0"
		}

	case analyzer.ModeStorage:
		if m.criteriaState.StorageAmount == "" {
			return "Please enter a storage amount in GB"
		}
		amount, err := strconv.ParseFloat(m.criteriaState.StorageAmount, 64)
		if err != nil {
			return "Invalid number"
		}
		if amount <= 0 {
			return "Storage amount must be greater than 0"
		}
	}

	return "" // No error
}

func (m *Model) appendToInput(char string) {
	switch m.criteriaState.SelectedMode {
	case analyzer.ModeVMCount:
		m.criteriaState.VMCount += char
	case analyzer.ModeVCPU:
		m.criteriaState.VCPUCount += char
	case analyzer.ModeCPUUsage:
		m.criteriaState.CPUUsage += char
	case analyzer.ModeRAM:
		m.criteriaState.RAMAmount += char
	case analyzer.ModeStorage:
		m.criteriaState.StorageAmount += char
	}
}

func (m *Model) deleteLastChar() {
	deleteFrom := func(s string) string {
		if len(s) > 0 {
			return s[:len(s)-1]
		}
		return s
	}

	switch m.criteriaState.SelectedMode {
	case analyzer.ModeVMCount:
		m.criteriaState.VMCount = deleteFrom(m.criteriaState.VMCount)
	case analyzer.ModeVCPU:
		m.criteriaState.VCPUCount = deleteFrom(m.criteriaState.VCPUCount)
	case analyzer.ModeCPUUsage:
		m.criteriaState.CPUUsage = deleteFrom(m.criteriaState.CPUUsage)
	case analyzer.ModeRAM:
		m.criteriaState.RAMAmount = deleteFrom(m.criteriaState.RAMAmount)
	case analyzer.ModeStorage:
		m.criteriaState.StorageAmount = deleteFrom(m.criteriaState.StorageAmount)
	}
}

func (m Model) handleVMSelectionKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	sourceNode := proxmox.GetNodeByName(m.cluster, m.sourceNode)
	if sourceNode == nil {
		return m, nil
	}

	vms := sourceNode.VMs

	switch msg.String() {
	case "up", "k":
		if m.vmCursorIdx > 0 {
			m.vmCursorIdx--
		}
	case "down", "j":
		if m.vmCursorIdx < len(vms)-1 {
			m.vmCursorIdx++
		}
	case " ":
		// Toggle selection
		vmid := vms[m.vmCursorIdx].VMID
		if m.criteriaState.SelectedVMs[vmid] {
			delete(m.criteriaState.SelectedVMs, vmid)
		} else {
			m.criteriaState.SelectedVMs[vmid] = true
		}
	case "enter":
		// Confirm selection and start analysis
		if len(m.criteriaState.SelectedVMs) > 0 {
			return m, tea.Batch(tea.ClearScreen, m.startAnalysis())
		}
	case "esc":
		m.currentView = ViewCriteria
		return m, tea.ClearScreen
	}
	return m, nil
}

func (m Model) handleResultsKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.result == nil || len(m.result.Suggestions) == 0 {
		switch msg.String() {
		case "r":
			m.currentView = ViewCriteria
			m.result = nil
			m.resultsScrollPos = 0
			m.resultsCursorPos = 0
			return m, tea.ClearScreen
		case "esc":
			m.currentView = ViewCriteria
			m.resultsScrollPos = 0
			m.resultsCursorPos = 0
			return m, tea.ClearScreen
		}
		return m, nil
	}

	// Calculate visible area for window-style scrolling
	maxVisible := m.height - 20 // Reserve space for header, summary, etc.
	if maxVisible < 5 {
		maxVisible = 5
	}
	totalItems := len(m.result.Suggestions)

	switch msg.String() {
	case "up", "k":
		if m.resultsCursorPos > 0 {
			m.resultsCursorPos--
			// Window-style scrolling: scroll up when cursor goes above visible area
			if m.resultsCursorPos < m.resultsScrollPos {
				m.resultsScrollPos = m.resultsCursorPos
			}
		}
	case "down", "j":
		if m.resultsCursorPos < totalItems-1 {
			m.resultsCursorPos++
			// Window-style scrolling: scroll down when cursor goes below visible area
			if m.resultsCursorPos >= m.resultsScrollPos+maxVisible {
				m.resultsScrollPos = m.resultsCursorPos - maxVisible + 1
			}
		}
	case "home":
		m.resultsCursorPos = 0
		m.resultsScrollPos = 0
	case "end":
		m.resultsCursorPos = totalItems - 1
		// Scroll to show the last item at the bottom of visible area
		if totalItems > maxVisible {
			m.resultsScrollPos = totalItems - maxVisible
		} else {
			m.resultsScrollPos = 0
		}
	case "pgup":
		// Move cursor up by page size
		m.resultsCursorPos -= maxVisible
		if m.resultsCursorPos < 0 {
			m.resultsCursorPos = 0
		}
		// Scroll to keep cursor visible
		if m.resultsCursorPos < m.resultsScrollPos {
			m.resultsScrollPos = m.resultsCursorPos
		}
	case "pgdown":
		// Move cursor down by page size
		m.resultsCursorPos += maxVisible
		if m.resultsCursorPos >= totalItems {
			m.resultsCursorPos = totalItems - 1
		}
		// Scroll to keep cursor visible
		if m.resultsCursorPos >= m.resultsScrollPos+maxVisible {
			m.resultsScrollPos = m.resultsCursorPos - maxVisible + 1
		}
	case "r":
		// Reset and start new analysis
		m.currentView = ViewCriteria
		m.result = nil
		m.resultsScrollPos = 0
		m.resultsCursorPos = 0
		return m, tea.ClearScreen
	case "esc":
		// Go back to criteria screen (not dashboard)
		m.currentView = ViewCriteria
		m.resultsScrollPos = 0
		m.resultsCursorPos = 0
		return m, tea.ClearScreen
	case "s":
		// TODO: Save results to file
		// For now, just ignore
	}
	return m, nil
}

func (m Model) handleErrorKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter", "esc":
		m.currentView = ViewDashboard
		m.err = nil
		return m, tea.ClearScreen
	}
	return m, nil
}

// View renders the current view
func (m Model) View() string {
	if m.showHelp {
		return components.RenderHelp()
	}

	if m.loading {
		return fmt.Sprintf("\n  %s...\n\n", m.loadingMsg)
	}

	switch m.currentView {
	case ViewDashboard:
		progress := views.RefreshProgress{
			Stage:   m.refreshProgress,
			Current: m.refreshCurrent,
			Total:   m.refreshTotal,
		}
		sortInfo := views.SortInfo{
			Column:    int(m.sortColumn),
			Ascending: m.sortAsc,
		}
		return views.RenderDashboardWithSort(m.cluster, m.selectedNodeIdx, m.width, m.refreshCountdown, m.refreshing, m.version, progress, sortInfo)
	case ViewCriteria:
		sourceNode := proxmox.GetNodeByName(m.cluster, m.sourceNode)
		return views.RenderCriteriaFull(m.criteriaState, m.sourceNode, sourceNode, m.cluster, m.version, m.width)
	case ViewVMSelection:
		sourceNode := proxmox.GetNodeByName(m.cluster, m.sourceNode)
		if sourceNode == nil {
			return "Error: Source node not found"
		}
		return views.RenderVMSelection(sourceNode.VMs, m.criteriaState.SelectedVMs, m.vmCursorIdx, m.width)
	case ViewResults:
		if m.result != nil {
			return views.RenderResultsWithCursor(m.result, m.cluster, m.version, m.width, m.height, m.resultsScrollPos, m.resultsCursorPos)
		}
		return "No results available"
	case ViewError:
		return fmt.Sprintf("\nError: %v\n\nPress Enter to continue", m.err)
	default:
		return "Unknown view"
	}
}

// startAnalysis creates analysis command
func (m Model) startAnalysis() tea.Cmd {
	return func() tea.Msg {
		// Build constraints
		constraints := analyzer.MigrationConstraints{
			SourceNode: m.sourceNode,
		}

		// Parse input based on mode
		var err error
		switch m.criteriaState.SelectedMode {
		case analyzer.ModeVMCount:
			if m.criteriaState.VMCount != "" {
				count, parseErr := strconv.Atoi(m.criteriaState.VMCount)
				if parseErr != nil {
					return errMsg{fmt.Errorf("invalid VM count: %w", parseErr)}
				}
				constraints.VMCount = &count
			}
		case analyzer.ModeVCPU:
			if m.criteriaState.VCPUCount != "" {
				count, parseErr := strconv.Atoi(m.criteriaState.VCPUCount)
				if parseErr != nil {
					return errMsg{fmt.Errorf("invalid vCPU count: %w", parseErr)}
				}
				constraints.VCPUCount = &count
			}
		case analyzer.ModeCPUUsage:
			if m.criteriaState.CPUUsage != "" {
				usage, parseErr := strconv.ParseFloat(m.criteriaState.CPUUsage, 64)
				if parseErr != nil {
					return errMsg{fmt.Errorf("invalid CPU usage: %w", parseErr)}
				}
				constraints.CPUUsage = &usage
			}
		case analyzer.ModeRAM:
			if m.criteriaState.RAMAmount != "" {
				gb, parseErr := strconv.ParseFloat(m.criteriaState.RAMAmount, 64)
				if parseErr != nil {
					return errMsg{fmt.Errorf("invalid RAM amount: %w", parseErr)}
				}
				bytes := int64(gb * 1024 * 1024 * 1024)
				constraints.RAMAmount = &bytes
			}
		case analyzer.ModeStorage:
			if m.criteriaState.StorageAmount != "" {
				gb, parseErr := strconv.ParseFloat(m.criteriaState.StorageAmount, 64)
				if parseErr != nil {
					return errMsg{fmt.Errorf("invalid storage amount: %w", parseErr)}
				}
				bytes := int64(gb * 1024 * 1024 * 1024)
				constraints.StorageAmount = &bytes
			}
		case analyzer.ModeSpecific:
			for vmid := range m.criteriaState.SelectedVMs {
				constraints.SpecificVMs = append(constraints.SpecificVMs, vmid)
			}
		}

		// Run analysis
		result, err := analyzer.Analyze(m.cluster, constraints)
		if err != nil {
			return errMsg{err}
		}

		return analysisCompleteMsg{result}
	}
}

// Messages
type errMsg struct {
	err error
}

type analysisCompleteMsg struct {
	result *analyzer.AnalysisResult
}
