package ui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
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
	ViewDashboard           ViewType = iota
	ViewDashboardHostDetail          // Shows VMs on selected host before migration mode selection
	ViewCriteria
	ViewVMSelection
	ViewAnalyzing
	ViewResults
	ViewHostDetail // Shows VMs added/removed on a specific host
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

	// Dashboard host detail view state (shows VMs on selected host + migration modes)
	dashboardHostDetailScrollPos    int // Scroll position for VM list
	dashboardHostDetailCursorPos    int // Cursor position in VM list
	dashboardHostDetailFocusSection int // 0 = VM list, 1 = migration modes
	dashboardHostDetailModeIdx      int // Selected migration mode index

	// Criteria state
	criteriaState views.CriteriaState

	// VM selection state (for ModeSpecific)
	vmCursorIdx           int
	vmSelectionReturnView ViewType // View to return to when ESC is pressed in VM selection

	// Analysis results
	result *analyzer.AnalysisResult

	// Results view scroll state
	resultsScrollPos int
	resultsCursorPos int // Current cursor position in results list

	// Results view section focus (0 = suggestions table, 1 = impact table)
	resultsSection   int
	impactCursorPos  int      // Cursor position in impact table
	selectedHostName string   // Selected host for detail view
	impactHostNames  []string // Sorted list of host names in impact table

	// Host detail view state
	hostDetailScrollPos       int // Scroll position for VM list
	hostDetailCursorPos       int // Cursor position in VM list
	hostDetailFocusedSection  int // 0 = VM list, 1 = reasoning panel
	hostDetailReasoningScroll int // Scroll position for reasoning panel

	// UI state
	width      int
	height     int
	showHelp   bool
	loading    bool
	loadingMsg string

	// Migration logic documentation view
	showMigrationLogics      bool
	migrationLogicsScrollPos int

	// Migration commands overlay state
	showMigrationCommands      bool
	migrationCommandsScrollPos int

	// VM details overlay state
	showVMDetails      bool
	vmDetailsScrollPos int
	selectedVMID       int    // VMID of VM to show details for
	selectedVMNode     string // Node where the VM is located

	// Auto-refresh state
	refreshCountdown int    // seconds until next refresh
	refreshing       bool   // true when actively refreshing data
	refreshProgress  string // progress message during refresh
	refreshCurrent   int    // current progress count
	refreshTotal     int    // total items to refresh

	// Cluster balance analysis state
	balanceStartTime      time.Time // When balance analysis started (for timer display)
	balanceReturnView     ViewType  // View to return to after Balance Cluster analysis (ESC)
	isBalanceClusterRun   bool      // True if current results are from Balance Cluster mode
	balanceMovementsTried int       // Number of migration candidates evaluated during balance analysis

	// Results view return destination
	resultsReturnView ViewType // View to return to when ESC is pressed in results view
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
			SelectedMode: analyzer.ModeAll, // Default to first mode in list
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

// balanceProgressMsg is sent to update balance analysis progress
type balanceProgressMsg struct {
	stage          string
	movementsTried int
}

// Update handles messages and updates the model
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKeyPress(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		// Clear screen on resize to prevent rendering artifacts
		return m, tea.ClearScreen

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

	case balanceProgressMsg:
		m.loadingMsg = msg.stage
		m.balanceMovementsTried = msg.movementsTried
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

	case clusterBalanceCompleteMsg:
		m.result = msg.result
		m.sourceNode = msg.sourceNode
		m.currentView = ViewResults
		m.loading = false
		m.resultsScrollPos = 0
		m.resultsCursorPos = 0
		return m, tea.ClearScreen
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
			if m.showMigrationLogics {
				m.showMigrationLogics = false
				m.migrationLogicsScrollPos = 0
				return m, tea.ClearScreen
			}
			return m, tea.Quit
		}
	case "?":
		if !m.showMigrationLogics {
			m.showMigrationLogics = true
			m.migrationLogicsScrollPos = 0
		}
		return m, tea.ClearScreen
	}

	// Handle migration logics view navigation
	if m.showMigrationLogics {
		return m.handleMigrationLogicsKeys(msg)
	}

	// Handle migration commands view navigation
	if m.showMigrationCommands {
		return m.handleMigrationCommandsKeys(msg)
	}

	// Handle VM details view navigation
	if m.showVMDetails {
		return m.handleVMDetailsKeys(msg)
	}

	// View-specific keys
	switch m.currentView {
	case ViewDashboard:
		return m.handleDashboardKeys(msg)
	case ViewDashboardHostDetail:
		return m.handleDashboardHostDetailKeys(msg)
	case ViewCriteria:
		return m.handleCriteriaKeys(msg)
	case ViewVMSelection:
		return m.handleVMSelectionKeys(msg)
	case ViewResults:
		return m.handleResultsKeys(msg)
	case ViewHostDetail:
		return m.handleHostDetailKeys(msg)
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
		// Select source node and show host detail view first
		m.sourceNode = m.cluster.Nodes[m.selectedNodeIdx].Name
		m.dashboardHostDetailScrollPos = 0
		m.dashboardHostDetailCursorPos = 0
		m.currentView = ViewDashboardHostDetail
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
	case "b", "B":
		// Balance cluster mode - cluster-wide balancing
		m.loading = true
		m.loadingMsg = "Analyzing cluster balance"
		m.balanceStartTime = time.Now()
		m.balanceReturnView = ViewDashboard // Return to main dashboard on ESC (legacy)
		m.resultsReturnView = ViewDashboard // Return to main dashboard on ESC
		m.isBalanceClusterRun = true
		return m, m.startClusterBalanceAnalysis()
	}
	return m, nil
}

// handleDashboardHostDetailKeys handles keyboard input for the dashboard host detail view
func (m Model) handleDashboardHostDetailKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Get the source node and its VMs
	sourceNodeObj := proxmox.GetNodeByName(m.cluster, m.sourceNode)
	if sourceNodeObj == nil {
		m.currentView = ViewDashboard
		return m, tea.ClearScreen
	}

	// Handle inline input mode when focused
	if m.criteriaState.InputFocused {
		return m.handleHostDetailInput(msg)
	}

	vmCount := len(sourceNodeObj.VMs)
	pageSize := 10
	numModes := 8 // Number of migration modes

	// Calculate max visible rows for VM list - MUST match RenderDashboardHostDetailWithInput calculation
	// Reserve: title(2) + border(1) + cluster summary(2) + source node header(1) + host info(2) + blank(1)
	//          + VM header+sep(2) + VM closing(1) + modes header+sep(2) + modes(8) + modes closing(1)
	//          + input area(3) + help(1) + buffer(2)
	fixedOverhead := 2 + 1 + 2 + 1 + 2 + 1 + 2 + 1 + 2 + numModes + 1 + 3 + 1 + 2
	availableHeight := m.height - fixedOverhead
	maxVisible := availableHeight
	if maxVisible < 5 {
		maxVisible = 5
	}
	// No cap - use all available rows for VM list

	switch msg.String() {
	case "tab":
		// Switch between VM list (0) and migration modes (1)
		m.dashboardHostDetailFocusSection = 1 - m.dashboardHostDetailFocusSection
		return m, nil

	case "up", "k":
		if m.dashboardHostDetailFocusSection == 0 {
			// VM list navigation
			if m.dashboardHostDetailCursorPos > 0 {
				m.dashboardHostDetailCursorPos--
				if m.dashboardHostDetailCursorPos < m.dashboardHostDetailScrollPos {
					m.dashboardHostDetailScrollPos = m.dashboardHostDetailCursorPos
				}
			}
		} else {
			// Migration mode navigation
			if m.dashboardHostDetailModeIdx > 0 {
				m.dashboardHostDetailModeIdx--
			}
		}

	case "down", "j":
		if m.dashboardHostDetailFocusSection == 0 {
			// VM list navigation
			if m.dashboardHostDetailCursorPos < vmCount-1 {
				m.dashboardHostDetailCursorPos++
				if m.dashboardHostDetailCursorPos >= m.dashboardHostDetailScrollPos+maxVisible {
					m.dashboardHostDetailScrollPos = m.dashboardHostDetailCursorPos - maxVisible + 1
				}
			}
		} else {
			// Migration mode navigation
			if m.dashboardHostDetailModeIdx < numModes-1 {
				m.dashboardHostDetailModeIdx++
			}
		}

	case "home":
		if m.dashboardHostDetailFocusSection == 0 {
			m.dashboardHostDetailCursorPos = 0
			m.dashboardHostDetailScrollPos = 0
		} else {
			m.dashboardHostDetailModeIdx = 0
		}

	case "end":
		if m.dashboardHostDetailFocusSection == 0 {
			m.dashboardHostDetailCursorPos = vmCount - 1
			if vmCount > maxVisible {
				m.dashboardHostDetailScrollPos = vmCount - maxVisible
			}
		} else {
			m.dashboardHostDetailModeIdx = numModes - 1
		}

	case "pgup":
		if m.dashboardHostDetailFocusSection == 0 {
			m.dashboardHostDetailCursorPos -= pageSize
			if m.dashboardHostDetailCursorPos < 0 {
				m.dashboardHostDetailCursorPos = 0
			}
			m.dashboardHostDetailScrollPos -= pageSize
			if m.dashboardHostDetailScrollPos < 0 {
				m.dashboardHostDetailScrollPos = 0
			}
		}

	case "pgdown":
		if m.dashboardHostDetailFocusSection == 0 {
			m.dashboardHostDetailCursorPos += pageSize
			if m.dashboardHostDetailCursorPos >= vmCount {
				m.dashboardHostDetailCursorPos = vmCount - 1
			}
			m.dashboardHostDetailScrollPos += pageSize
			maxScroll := vmCount - maxVisible
			if maxScroll < 0 {
				maxScroll = 0
			}
			if m.dashboardHostDetailScrollPos > maxScroll {
				m.dashboardHostDetailScrollPos = maxScroll
			}
		}

	case "enter":
		if m.dashboardHostDetailFocusSection == 0 {
			// Show VM details for the selected VM
			vmid, nodeName := m.getDashboardHostDetailVMAtCursor()
			if vmid > 0 {
				m.selectedVMID = vmid
				m.selectedVMNode = nodeName
				m.showVMDetails = true
				m.vmDetailsScrollPos = 0
				return m, tea.ClearScreen
			}
		} else {
			// Start migration analysis with selected mode
			return m.startMigrationFromHostDetail()
		}

	case "esc":
		// Go back to dashboard
		m.currentView = ViewDashboard
		return m, tea.ClearScreen
	}
	return m, nil
}

// handleHostDetailInput handles keyboard input when in inline input mode on the host detail view
func (m Model) handleHostDetailInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		// Clear previous error
		m.criteriaState.ErrorMessage = ""

		// Validate input before proceeding
		validationErr := m.validateCriteriaInput()
		if validationErr != "" {
			m.criteriaState.ErrorMessage = validationErr
			return m, nil
		}

		// Start analysis
		m.criteriaState.InputFocused = false
		m.loading = true
		m.loadingMsg = "Analyzing migrations"
		m.resultsReturnView = ViewDashboardHostDetail // Return to host detail on ESC from results
		return m, m.startAnalysis()

	case "esc":
		// Cancel input mode and clear input
		m.criteriaState.InputFocused = false
		m.criteriaState.ErrorMessage = ""
		// Clear input field for current mode
		m.clearCurrentInput()
		return m, nil

	case "backspace", "ctrl+h", "delete":
		m.deleteLastChar()
		m.criteriaState.ErrorMessage = "" // Clear error on edit
		return m, nil

	default:
		// Only allow digits and decimal point for numeric input
		char := msg.String()
		if len(char) == 1 && (char >= "0" && char <= "9" || char == ".") {
			m.appendToInput(char)
			m.criteriaState.ErrorMessage = "" // Clear error on edit
		}
		return m, nil
	}
}

// getDashboardHostDetailVMAtCursor returns the VMID and node name for the VM at the current cursor position
func (m Model) getDashboardHostDetailVMAtCursor() (int, string) {
	sourceNodeObj := proxmox.GetNodeByName(m.cluster, m.sourceNode)
	if sourceNodeObj == nil {
		return 0, ""
	}

	// Build sorted list matching view order (sorted by name)
	type vmEntry struct {
		VMID int
		Name string
	}
	var vmList []vmEntry
	for _, vm := range sourceNodeObj.VMs {
		vmList = append(vmList, vmEntry{VMID: vm.VMID, Name: vm.Name})
	}
	// Sort by name to match view order
	sort.Slice(vmList, func(i, j int) bool {
		return vmList[i].Name < vmList[j].Name
	})

	if m.dashboardHostDetailCursorPos >= 0 && m.dashboardHostDetailCursorPos < len(vmList) {
		return vmList[m.dashboardHostDetailCursorPos].VMID, m.sourceNode
	}
	return 0, ""
}

// getMigrationModeFromIndex returns the migration mode for a given index
// Order matches the UI: Migrate All, vCPU, CPU Usage, RAM, Storage, Create Date, Specific VMs, Balance Cluster (last)
func getMigrationModeFromIndex(idx int) analyzer.MigrationMode {
	modes := []analyzer.MigrationMode{
		analyzer.ModeAll,
		analyzer.ModeVCPU,
		analyzer.ModeCPUUsage,
		analyzer.ModeRAM,
		analyzer.ModeStorage,
		analyzer.ModeCreationDate,
		analyzer.ModeSpecific,
		analyzer.ModeBalanceCluster, // Last option
	}
	if idx >= 0 && idx < len(modes) {
		return modes[idx]
	}
	return analyzer.ModeAll
}

// startMigrationFromHostDetail starts migration analysis from the host detail view
func (m Model) startMigrationFromHostDetail() (tea.Model, tea.Cmd) {
	selectedMode := getMigrationModeFromIndex(m.dashboardHostDetailModeIdx)

	// For modes that need input, enable inline input mode in the current view
	switch selectedMode {
	case analyzer.ModeVCPU, analyzer.ModeCPUUsage, analyzer.ModeRAM, analyzer.ModeStorage, analyzer.ModeCreationDate:
		// These modes need user input - enable inline input mode
		m.criteriaState = views.CriteriaState{
			SelectedMode:   selectedMode,
			SelectedVMs:    make(map[int]bool),
			CursorPosition: m.dashboardHostDetailModeIdx,
			InputFocused:   true, // Start with input focused
		}
		m.isBalanceClusterRun = false // Not a balance cluster run
		// Stay in the same view but with input focused
		return m, nil

	case analyzer.ModeSpecific:
		// Go to VM selection view
		m.criteriaState = views.CriteriaState{
			SelectedMode: selectedMode,
			SelectedVMs:  make(map[int]bool),
		}
		m.vmCursorIdx = 0
		m.currentView = ViewVMSelection
		m.vmSelectionReturnView = ViewDashboardHostDetail // Return to host detail on ESC
		m.isBalanceClusterRun = false                     // Not a balance cluster run
		return m, tea.ClearScreen

	case analyzer.ModeAll:
		// Start analysis immediately
		m.criteriaState = views.CriteriaState{
			SelectedMode: selectedMode,
			SelectedVMs:  make(map[int]bool),
		}
		m.loading = true
		m.loadingMsg = "Analyzing migrations"
		m.isBalanceClusterRun = false                 // Not a balance cluster run
		m.resultsReturnView = ViewDashboardHostDetail // Return to host detail on ESC
		return m, m.startAnalysis()

	case analyzer.ModeBalanceCluster:
		// Start cluster-wide balance analysis
		m.loading = true
		m.loadingMsg = "Analyzing cluster balance"
		m.balanceStartTime = time.Now()
		m.balanceReturnView = ViewDashboardHostDetail // Return to host detail on ESC (legacy)
		m.resultsReturnView = ViewDashboardHostDetail // Return to host detail on ESC
		m.isBalanceClusterRun = true
		return m, m.startClusterBalanceAnalysis()
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
			less = nodes[i].GetStatusWithIndicators() < nodes[j].GetStatusWithIndicators()
		case SortByVMs:
			less = len(nodes[i].VMs) < len(nodes[j].VMs)
		case SortByVCPUs:
			less = nodes[i].GetRunningVCPUs() < nodes[j].GetRunningVCPUs()
		case SortByCPUPercent:
			less = nodes[i].GetCPUPercent() < nodes[j].GetCPUPercent()
		case SortByLA:
			// Compare 1-minute load average as percentage of CPU cores
			laPctI := 0.0
			laPctJ := 0.0
			if len(nodes[i].LoadAverage) > 0 && nodes[i].CPUCores > 0 {
				laPctI = nodes[i].LoadAverage[0] / float64(nodes[i].CPUCores) * 100
			}
			if len(nodes[j].LoadAverage) > 0 && nodes[j].CPUCores > 0 {
				laPctJ = nodes[j].LoadAverage[0] / float64(nodes[j].CPUCores) * 100
			}
			less = laPctI < laPctJ
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
		if m.criteriaState.CursorPosition < 7 { // 8 modes total (0-7)
			m.criteriaState.CursorPosition++
		}
	case "enter":
		// Select mode based on cursor position
		// Mode order: ModeAll, ModeBalanceCluster, ModeVCPU, ModeCPUUsage, ModeRAM, ModeStorage, ModeCreationDate, ModeSpecific
		modeMap := []analyzer.MigrationMode{
			analyzer.ModeAll,
			analyzer.ModeBalanceCluster,
			analyzer.ModeVCPU,
			analyzer.ModeCPUUsage,
			analyzer.ModeRAM,
			analyzer.ModeStorage,
			analyzer.ModeCreationDate,
			analyzer.ModeSpecific,
		}
		m.criteriaState.SelectedMode = modeMap[m.criteriaState.CursorPosition]

		// ModeAll and ModeBalanceCluster - go directly to analysis (no input needed)
		if m.criteriaState.SelectedMode == analyzer.ModeAll || m.criteriaState.SelectedMode == analyzer.ModeBalanceCluster {
			m.resultsReturnView = ViewCriteria // Return to criteria on ESC from results
			return m, tea.Batch(tea.ClearScreen, m.startAnalysis())
		}

		// If specific VMs mode, go to VM selection
		if m.criteriaState.SelectedMode == analyzer.ModeSpecific {
			m.currentView = ViewVMSelection
			m.vmSelectionReturnView = ViewCriteria // Return to criteria on ESC
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

		// Validate input before proceeding
		validationErr := m.validateCriteriaInput()
		if validationErr != "" {
			m.criteriaState.ErrorMessage = validationErr
			return m, nil
		}

		// Start analysis directly (CPU mode uses auto-optimized efficiency algorithm)
		m.criteriaState.InputFocused = false
		m.resultsReturnView = ViewCriteria // Return to criteria on ESC from results
		return m, tea.Batch(tea.ClearScreen, m.startAnalysis())
	case "esc":
		m.criteriaState.InputFocused = false
		m.criteriaState.ErrorMessage = ""
		// Clear all input fields to reset the form
		m.criteriaState.VMCount = ""
		m.criteriaState.VCPUCount = ""
		m.criteriaState.CPUUsage = ""
		m.criteriaState.RAMAmount = ""
		m.criteriaState.StorageAmount = ""
		m.criteriaState.CreationAge = ""
		return m, tea.ClearScreen
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

	case analyzer.ModeCreationDate:
		if m.criteriaState.CreationAge == "" {
			// Default to 75 days if empty
			m.criteriaState.CreationAge = "75"
		}
		days, err := strconv.Atoi(m.criteriaState.CreationAge)
		if err != nil {
			return "Invalid number"
		}
		if days <= 0 {
			return "Days must be greater than 0"
		}
	}

	return "" // No error
}

func (m *Model) appendToInput(char string) {
	switch m.criteriaState.SelectedMode {
	case analyzer.ModeVCPU:
		m.criteriaState.VCPUCount += char
	case analyzer.ModeCPUUsage:
		m.criteriaState.CPUUsage += char
	case analyzer.ModeRAM:
		m.criteriaState.RAMAmount += char
	case analyzer.ModeStorage:
		m.criteriaState.StorageAmount += char
	case analyzer.ModeCreationDate:
		m.criteriaState.CreationAge += char
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
	case analyzer.ModeVCPU:
		m.criteriaState.VCPUCount = deleteFrom(m.criteriaState.VCPUCount)
	case analyzer.ModeCPUUsage:
		m.criteriaState.CPUUsage = deleteFrom(m.criteriaState.CPUUsage)
	case analyzer.ModeRAM:
		m.criteriaState.RAMAmount = deleteFrom(m.criteriaState.RAMAmount)
	case analyzer.ModeStorage:
		m.criteriaState.StorageAmount = deleteFrom(m.criteriaState.StorageAmount)
	case analyzer.ModeCreationDate:
		m.criteriaState.CreationAge = deleteFrom(m.criteriaState.CreationAge)
	}
}

func (m *Model) clearCurrentInput() {
	switch m.criteriaState.SelectedMode {
	case analyzer.ModeVCPU:
		m.criteriaState.VCPUCount = ""
	case analyzer.ModeCPUUsage:
		m.criteriaState.CPUUsage = ""
	case analyzer.ModeRAM:
		m.criteriaState.RAMAmount = ""
	case analyzer.ModeStorage:
		m.criteriaState.StorageAmount = ""
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
			m.resultsReturnView = m.vmSelectionReturnView // Return to where VM selection was entered from
			return m, tea.Batch(tea.ClearScreen, m.startAnalysis())
		}
	case "esc":
		m.currentView = m.vmSelectionReturnView
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

	// Build sorted list of impact hosts (source + active targets)
	if len(m.impactHostNames) == 0 {
		m.impactHostNames = m.buildImpactHostList()
	}

	// Count active targets (those that receive VMs) - must match results.go calculation
	activeTargets := 0
	for targetName, afterState := range m.result.TargetsAfter {
		beforeState := m.result.TargetsBefore[targetName]
		if afterState.VMCount != beforeState.VMCount {
			activeTargets++
		}
	}

	// Calculate visible area - must match calculateVisibleRowsWithTargets in results.go
	fixedOverhead := 27
	targetLines := activeTargets * 1
	reserved := fixedOverhead + targetLines
	maxVisible := m.height - reserved
	if maxVisible < 3 {
		maxVisible = 3
	}
	totalItems := len(m.result.Suggestions)

	switch msg.String() {
	case "tab":
		// Toggle between suggestions table (0) and impact table (1)
		m.resultsSection = (m.resultsSection + 1) % 2
		return m, nil

	case "up", "k":
		if m.resultsSection == 0 {
			// Suggestions table navigation
			if m.resultsCursorPos > 0 {
				m.resultsCursorPos--
				if m.resultsCursorPos < m.resultsScrollPos {
					m.resultsScrollPos = m.resultsCursorPos
				}
			}
		} else {
			// Impact table navigation
			if m.impactCursorPos > 0 {
				m.impactCursorPos--
			}
		}

	case "down", "j":
		if m.resultsSection == 0 {
			// Suggestions table navigation
			if m.resultsCursorPos < totalItems-1 {
				m.resultsCursorPos++
				if m.resultsCursorPos >= m.resultsScrollPos+maxVisible {
					m.resultsScrollPos = m.resultsCursorPos - maxVisible + 1
				}
			}
		} else {
			// Impact table navigation
			if m.impactCursorPos < len(m.impactHostNames)-1 {
				m.impactCursorPos++
			}
		}

	case "enter":
		if m.resultsSection == 0 && m.resultsCursorPos >= 0 && m.resultsCursorPos < len(m.result.Suggestions) {
			// Show VM details for selected VM in suggestions table
			sug := m.result.Suggestions[m.resultsCursorPos]
			m.selectedVMID = sug.VMID
			m.selectedVMNode = sug.SourceNode
			m.showVMDetails = true
			m.vmDetailsScrollPos = 0
			return m, tea.ClearScreen
		} else if m.resultsSection == 1 && len(m.impactHostNames) > 0 {
			// Show host detail view
			m.selectedHostName = m.impactHostNames[m.impactCursorPos]
			m.currentView = ViewHostDetail
			return m, tea.ClearScreen
		}

	case "home":
		if m.resultsSection == 0 {
			m.resultsCursorPos = 0
			m.resultsScrollPos = 0
		} else {
			m.impactCursorPos = 0
		}

	case "end":
		if m.resultsSection == 0 {
			m.resultsCursorPos = totalItems - 1
			if totalItems > maxVisible {
				m.resultsScrollPos = totalItems - maxVisible
			} else {
				m.resultsScrollPos = 0
			}
		} else {
			m.impactCursorPos = len(m.impactHostNames) - 1
		}

	case "pgup":
		if m.resultsSection == 0 {
			m.resultsCursorPos -= maxVisible
			if m.resultsCursorPos < 0 {
				m.resultsCursorPos = 0
			}
			if m.resultsCursorPos < m.resultsScrollPos {
				m.resultsScrollPos = m.resultsCursorPos
			}
		} else {
			m.impactCursorPos -= 5
			if m.impactCursorPos < 0 {
				m.impactCursorPos = 0
			}
		}

	case "pgdown":
		if m.resultsSection == 0 {
			m.resultsCursorPos += maxVisible
			if m.resultsCursorPos >= totalItems {
				m.resultsCursorPos = totalItems - 1
			}
			if m.resultsCursorPos >= m.resultsScrollPos+maxVisible {
				m.resultsScrollPos = m.resultsCursorPos - maxVisible + 1
			}
		} else {
			m.impactCursorPos += 5
			if m.impactCursorPos >= len(m.impactHostNames) {
				m.impactCursorPos = len(m.impactHostNames) - 1
			}
		}

	case "r":
		// Reset and start new analysis
		m.currentView = ViewCriteria
		m.result = nil
		m.resultsScrollPos = 0
		m.resultsCursorPos = 0
		m.resultsSection = 0
		m.impactCursorPos = 0
		m.impactHostNames = nil
		m.criteriaState.VMCount = ""
		m.criteriaState.VCPUCount = ""
		m.criteriaState.CPUUsage = ""
		m.criteriaState.RAMAmount = ""
		m.criteriaState.StorageAmount = ""
		m.criteriaState.InputFocused = false
		m.criteriaState.ErrorMessage = ""
		m.isBalanceClusterRun = false // Reset balance cluster flag
		return m, tea.ClearScreen

	case "m":
		// Show migration commands
		m.showMigrationCommands = true
		m.migrationCommandsScrollPos = 0
		return m, tea.ClearScreen

	case "esc":
		// Reset results view state
		m.resultsScrollPos = 0
		m.resultsCursorPos = 0
		m.resultsSection = 0
		m.impactCursorPos = 0
		m.impactHostNames = nil
		m.criteriaState.VMCount = ""
		m.criteriaState.VCPUCount = ""
		m.criteriaState.CPUUsage = ""
		m.criteriaState.RAMAmount = ""
		m.criteriaState.StorageAmount = ""
		m.criteriaState.InputFocused = false
		m.criteriaState.ErrorMessage = ""
		m.isBalanceClusterRun = false

		// Return to the saved return view (ViewDashboardHostDetail or ViewCriteria)
		m.currentView = m.resultsReturnView
		return m, tea.ClearScreen
	}
	return m, nil
}

// buildImpactHostList builds a sorted list of hosts in the impact table
func (m *Model) buildImpactHostList() []string {
	var hosts []string

	// Add source node first
	hosts = append(hosts, m.result.SourceBefore.Name)

	// Add target nodes that receive VMs
	var targetNames []string
	for name, afterState := range m.result.TargetsAfter {
		beforeState := m.result.TargetsBefore[name]
		if afterState.VMCount != beforeState.VMCount {
			targetNames = append(targetNames, name)
		}
	}
	sort.Strings(targetNames)
	hosts = append(hosts, targetNames...)

	return hosts
}

func (m Model) handleHostDetailKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Calculate total VM count for this host
	isSource := (m.selectedHostName == m.sourceNode || m.selectedHostName == m.result.SourceBefore.Name)
	var totalVMCount int

	if isSource {
		// Source node: count VMs from source node in cluster
		sourceNode := proxmox.GetNodeByName(m.cluster, m.sourceNode)
		if sourceNode != nil {
			totalVMCount = len(sourceNode.VMs)
		}
	} else {
		// Target node: existing VMs + VMs being migrated in
		targetNode := proxmox.GetNodeByName(m.cluster, m.selectedHostName)
		if targetNode != nil {
			totalVMCount = len(targetNode.VMs)
		}
		// Add VMs being migrated in
		for _, sug := range m.result.Suggestions {
			if sug.TargetNode == m.selectedHostName {
				totalVMCount++
			}
		}
	}

	// Calculate max visible rows (must match fixedOverhead in results.go RenderHostDetailWithReasoningScroll)
	// Fixed overhead: title+border(2) + CPU(1) + before/after(3) + header+sep(2) + closing(1) + scroll info(1) + reasoning panel(20) + help(2) = 32
	fixedOverhead := 32
	maxVisible := m.height - fixedOverhead
	if maxVisible < 5 {
		maxVisible = 5
	}

	switch msg.String() {
	case "tab":
		// Toggle focus between VM list (0) and reasoning panel (1)
		if m.hostDetailFocusedSection == 0 {
			m.hostDetailFocusedSection = 1
			m.hostDetailReasoningScroll = 0
		} else {
			m.hostDetailFocusedSection = 0
		}
		return m, nil

	case "up", "k":
		if m.hostDetailFocusedSection == 0 {
			// VM list navigation
			if m.hostDetailCursorPos > 0 {
				m.hostDetailCursorPos--
				if m.hostDetailCursorPos < m.hostDetailScrollPos {
					m.hostDetailScrollPos = m.hostDetailCursorPos
				}
				// Reset reasoning scroll when changing VMs
				m.hostDetailReasoningScroll = 0
			}
		} else {
			// Reasoning panel scroll up
			if m.hostDetailReasoningScroll > 0 {
				m.hostDetailReasoningScroll--
			}
		}
		return m, nil

	case "down", "j":
		if m.hostDetailFocusedSection == 0 {
			// VM list navigation
			if m.hostDetailCursorPos < totalVMCount-1 {
				m.hostDetailCursorPos++
				if m.hostDetailCursorPos >= m.hostDetailScrollPos+maxVisible {
					m.hostDetailScrollPos = m.hostDetailCursorPos - maxVisible + 1
				}
				// Reset reasoning scroll when changing VMs
				m.hostDetailReasoningScroll = 0
			}
		} else {
			// Reasoning panel scroll down
			m.hostDetailReasoningScroll++
		}
		return m, nil

	case "pgup":
		if m.hostDetailFocusedSection == 0 {
			// VM list page up
			m.hostDetailCursorPos -= maxVisible
			if m.hostDetailCursorPos < 0 {
				m.hostDetailCursorPos = 0
			}
			if m.hostDetailCursorPos < m.hostDetailScrollPos {
				m.hostDetailScrollPos = m.hostDetailCursorPos
			}
			m.hostDetailReasoningScroll = 0
		} else {
			// Reasoning panel page up
			m.hostDetailReasoningScroll -= 10
			if m.hostDetailReasoningScroll < 0 {
				m.hostDetailReasoningScroll = 0
			}
		}
		return m, nil

	case "pgdown":
		if m.hostDetailFocusedSection == 0 {
			// VM list page down
			m.hostDetailCursorPos += maxVisible
			if m.hostDetailCursorPos >= totalVMCount {
				m.hostDetailCursorPos = totalVMCount - 1
			}
			if m.hostDetailCursorPos < 0 {
				m.hostDetailCursorPos = 0
			}
			if m.hostDetailCursorPos >= m.hostDetailScrollPos+maxVisible {
				m.hostDetailScrollPos = m.hostDetailCursorPos - maxVisible + 1
			}
			m.hostDetailReasoningScroll = 0
		} else {
			// Reasoning panel page down
			m.hostDetailReasoningScroll += 10
		}
		return m, nil

	case "home":
		if m.hostDetailFocusedSection == 0 {
			m.hostDetailCursorPos = 0
			m.hostDetailScrollPos = 0
			m.hostDetailReasoningScroll = 0
		} else {
			m.hostDetailReasoningScroll = 0
		}
		return m, nil

	case "end":
		if m.hostDetailFocusedSection == 0 {
			m.hostDetailCursorPos = totalVMCount - 1
			if m.hostDetailCursorPos < 0 {
				m.hostDetailCursorPos = 0
			}
			if totalVMCount > maxVisible {
				m.hostDetailScrollPos = totalVMCount - maxVisible
			} else {
				m.hostDetailScrollPos = 0
			}
			m.hostDetailReasoningScroll = 0
		} else {
			// Scroll to end of reasoning (will be clamped in render)
			m.hostDetailReasoningScroll = 9999
		}
		return m, nil

	case "enter":
		// Show VM details for the selected VM (only when focused on VM list)
		if m.hostDetailFocusedSection == 0 {
			// Get the VM at cursor position
			vmid, nodeName := m.getHostDetailVMAtCursor()
			if vmid > 0 {
				m.selectedVMID = vmid
				m.selectedVMNode = nodeName
				m.showVMDetails = true
				m.vmDetailsScrollPos = 0
				return m, tea.ClearScreen
			}
		}
		return m, nil

	case "esc":
		m.currentView = ViewResults
		m.hostDetailScrollPos = 0
		m.hostDetailCursorPos = 0
		m.hostDetailFocusedSection = 0
		m.hostDetailReasoningScroll = 0
		return m, tea.ClearScreen
	}
	return m, nil
}

// getHostDetailVMAtCursor returns the VMID and node name for the VM at the current cursor position
func (m Model) getHostDetailVMAtCursor() (int, string) {
	isSource := (m.selectedHostName == m.sourceNode || m.selectedHostName == m.result.SourceBefore.Name)

	// Create a map of VMs being migrated for quick lookup
	migratingVMs := make(map[int]analyzer.MigrationSuggestion)
	for _, sug := range m.result.Suggestions {
		migratingVMs[sug.VMID] = sug
	}

	if isSource {
		// Source node: VMs from the source node, sorted by name to match view
		sourceNodeObj := proxmox.GetNodeByName(m.cluster, m.sourceNode)
		if sourceNodeObj != nil {
			// Build sorted list matching view order (sorted by name)
			type vmEntry struct {
				VMID int
				Name string
			}
			var vmList []vmEntry
			for _, vm := range sourceNodeObj.VMs {
				vmList = append(vmList, vmEntry{VMID: vm.VMID, Name: vm.Name})
			}
			// Sort by name to match view order
			sort.Slice(vmList, func(i, j int) bool {
				return vmList[i].Name < vmList[j].Name
			})

			if m.hostDetailCursorPos >= 0 && m.hostDetailCursorPos < len(vmList) {
				return vmList[m.hostDetailCursorPos].VMID, m.sourceNode
			}
		}
	} else {
		// Target node: build combined list (existing + incoming)
		type vmEntry struct {
			VMID     int
			Name     string
			NodeName string
		}
		var vmList []vmEntry

		// Existing VMs on target
		targetNode := proxmox.GetNodeByName(m.cluster, m.selectedHostName)
		if targetNode != nil {
			for _, vm := range targetNode.VMs {
				// Skip VMs being migrated out
				if _, migrating := migratingVMs[vm.VMID]; !migrating {
					vmList = append(vmList, vmEntry{
						VMID:     vm.VMID,
						Name:     vm.Name,
						NodeName: m.selectedHostName,
					})
				}
			}
		}

		// Add VMs being migrated in
		for _, sug := range m.result.Suggestions {
			if sug.TargetNode == m.selectedHostName {
				vmList = append(vmList, vmEntry{
					VMID:     sug.VMID,
					Name:     sug.VMName,
					NodeName: sug.SourceNode,
				})
			}
		}

		// Sort by name to match view order
		sort.Slice(vmList, func(i, j int) bool {
			return vmList[i].Name < vmList[j].Name
		})

		if m.hostDetailCursorPos >= 0 && m.hostDetailCursorPos < len(vmList) {
			return vmList[m.hostDetailCursorPos].VMID, vmList[m.hostDetailCursorPos].NodeName
		}
	}

	return 0, ""
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

func (m Model) handleMigrationLogicsKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	totalLines := views.GetMigrationLogicTotalLines()
	availableHeight := m.height - 4 // Same calculation as in RenderMigrationLogic
	maxScroll := totalLines - availableHeight
	if maxScroll < 0 {
		maxScroll = 0
	}

	switch msg.String() {
	case "esc":
		m.showMigrationLogics = false
		m.migrationLogicsScrollPos = 0
		return m, tea.ClearScreen
	case "up", "k":
		if m.migrationLogicsScrollPos > 0 {
			m.migrationLogicsScrollPos--
		}
	case "down", "j":
		if m.migrationLogicsScrollPos < maxScroll {
			m.migrationLogicsScrollPos++
		}
	case "pgup":
		m.migrationLogicsScrollPos -= availableHeight
		if m.migrationLogicsScrollPos < 0 {
			m.migrationLogicsScrollPos = 0
		}
	case "pgdown":
		m.migrationLogicsScrollPos += availableHeight
		if m.migrationLogicsScrollPos > maxScroll {
			m.migrationLogicsScrollPos = maxScroll
		}
	case "home":
		m.migrationLogicsScrollPos = 0
	case "end":
		m.migrationLogicsScrollPos = maxScroll
	}
	return m, nil
}

func (m Model) handleMigrationCommandsKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.result == nil {
		m.showMigrationCommands = false
		return m, tea.ClearScreen
	}

	totalLines := len(m.result.Suggestions) + 10 // commands + header/footer
	availableHeight := m.height - 4
	maxScroll := totalLines - availableHeight
	if maxScroll < 0 {
		maxScroll = 0
	}

	switch msg.String() {
	case "esc", "m":
		m.showMigrationCommands = false
		m.migrationCommandsScrollPos = 0
		return m, tea.ClearScreen
	case "up", "k":
		if m.migrationCommandsScrollPos > 0 {
			m.migrationCommandsScrollPos--
		}
	case "down", "j":
		if m.migrationCommandsScrollPos < maxScroll {
			m.migrationCommandsScrollPos++
		}
	case "pgup":
		m.migrationCommandsScrollPos -= availableHeight
		if m.migrationCommandsScrollPos < 0 {
			m.migrationCommandsScrollPos = 0
		}
	case "pgdown":
		m.migrationCommandsScrollPos += availableHeight
		if m.migrationCommandsScrollPos > maxScroll {
			m.migrationCommandsScrollPos = maxScroll
		}
	case "home":
		m.migrationCommandsScrollPos = 0
	case "end":
		m.migrationCommandsScrollPos = maxScroll
	}
	return m, nil
}

func (m Model) handleVMDetailsKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Get VM config content to calculate scroll limits
	configContent := proxmox.GetVMConfigContent(m.selectedVMNode, m.selectedVMID)
	totalLines := len(strings.Split(configContent, "\n")) + 15 // config + header/footer
	availableHeight := m.height - 6
	maxScroll := totalLines - availableHeight
	if maxScroll < 0 {
		maxScroll = 0
	}

	switch msg.String() {
	case "esc", "enter":
		m.showVMDetails = false
		m.vmDetailsScrollPos = 0
		return m, tea.ClearScreen
	case "up", "k":
		if m.vmDetailsScrollPos > 0 {
			m.vmDetailsScrollPos--
		}
	case "down", "j":
		if m.vmDetailsScrollPos < maxScroll {
			m.vmDetailsScrollPos++
		}
	case "pgup":
		m.vmDetailsScrollPos -= availableHeight
		if m.vmDetailsScrollPos < 0 {
			m.vmDetailsScrollPos = 0
		}
	case "pgdown":
		m.vmDetailsScrollPos += availableHeight
		if m.vmDetailsScrollPos > maxScroll {
			m.vmDetailsScrollPos = maxScroll
		}
	case "home":
		m.vmDetailsScrollPos = 0
	case "end":
		m.vmDetailsScrollPos = maxScroll
	}
	return m, nil
}

// View renders the current view
func (m Model) View() string {
	if m.showMigrationLogics {
		return views.RenderMigrationLogic(m.width, m.height, m.migrationLogicsScrollPos)
	}

	if m.showMigrationCommands && m.result != nil {
		return views.RenderMigrationCommands(m.result, m.sourceNode, m.width, m.height, m.migrationCommandsScrollPos)
	}

	if m.showVMDetails {
		// Find the VM in the cluster
		var vm *proxmox.VM
		for i := range m.cluster.Nodes {
			if m.cluster.Nodes[i].Name == m.selectedVMNode {
				for j := range m.cluster.Nodes[i].VMs {
					if m.cluster.Nodes[i].VMs[j].VMID == m.selectedVMID {
						vm = &m.cluster.Nodes[i].VMs[j]
						break
					}
				}
				break
			}
		}
		return views.RenderVMDetails(vm, m.selectedVMNode, m.selectedVMID, m.width, m.height, m.vmDetailsScrollPos)
	}

	if m.showHelp {
		return components.RenderHelp()
	}

	if m.loading {
		// Show elapsed time and movements counter for Balance Cluster analysis
		if m.isBalanceClusterRun && !m.balanceStartTime.IsZero() {
			elapsed := time.Since(m.balanceStartTime)
			seconds := int(elapsed.Seconds())
			if m.balanceMovementsTried > 0 {
				return fmt.Sprintf("\n  %s... (%d seconds, %d migrations evaluated)\n\n  ████████████████████░░░░░░░░░░\n\n", m.loadingMsg, seconds, m.balanceMovementsTried)
			}
			return fmt.Sprintf("\n  %s... (%d seconds)\n\n  ████████████████████░░░░░░░░░░\n\n", m.loadingMsg, seconds)
		}
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
		return views.RenderDashboardWithHeight(m.cluster, m.selectedNodeIdx, m.width, m.height, m.refreshCountdown, m.refreshing, m.version, progress, sortInfo)
	case ViewDashboardHostDetail:
		sourceNode := proxmox.GetNodeByName(m.cluster, m.sourceNode)
		if sourceNode == nil {
			return "Error: Source node not found"
		}
		// Get input value based on mode index (1=vCPU, 2=CPU%, 3=RAM, 4=Storage, 5=CreateDate)
		var inputValue string
		switch m.dashboardHostDetailModeIdx {
		case 1:
			inputValue = m.criteriaState.VCPUCount
		case 2:
			inputValue = m.criteriaState.CPUUsage
		case 3:
			inputValue = m.criteriaState.RAMAmount
		case 4:
			inputValue = m.criteriaState.StorageAmount
		case 5:
			inputValue = m.criteriaState.CreationAge
		}
		return views.RenderDashboardHostDetailWithInput(sourceNode, m.cluster, m.version, m.width, m.height, m.dashboardHostDetailScrollPos, m.dashboardHostDetailCursorPos, m.dashboardHostDetailFocusSection, m.dashboardHostDetailModeIdx, inputValue, m.criteriaState.InputFocused, m.criteriaState.ErrorMessage)
	case ViewCriteria:
		sourceNode := proxmox.GetNodeByName(m.cluster, m.sourceNode)
		return views.RenderCriteriaFull(m.criteriaState, m.sourceNode, sourceNode, m.cluster, m.version, m.width)
	case ViewVMSelection:
		sourceNode := proxmox.GetNodeByName(m.cluster, m.sourceNode)
		if sourceNode == nil {
			return "Error: Source node not found"
		}
		return views.RenderVMSelectionWithHeight(sourceNode.VMs, m.criteriaState.SelectedVMs, m.vmCursorIdx, m.width, m.height)
	case ViewResults:
		if m.result != nil {
			sourceNode := proxmox.GetNodeByName(m.cluster, m.sourceNode)
			return views.RenderResultsInteractive(m.result, m.cluster, sourceNode, m.version, m.width, m.height, m.resultsScrollPos, m.resultsCursorPos, m.resultsSection, m.impactCursorPos)
		}
		return "No results available"
	case ViewHostDetail:
		if m.result != nil && m.selectedHostName != "" {
			return views.RenderHostDetailWithReasoningScroll(m.result, m.cluster, m.selectedHostName, m.sourceNode, m.width, m.height, m.hostDetailScrollPos, m.hostDetailCursorPos, m.hostDetailFocusedSection, m.hostDetailReasoningScroll)
		}
		return "No host selected"
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
		case analyzer.ModeAll:
			constraints.MigrateAll = true
		case analyzer.ModeBalanceCluster:
			constraints.BalanceCluster = true
		case analyzer.ModeCreationDate:
			// Default to 75 days if not specified
			daysStr := m.criteriaState.CreationAge
			if daysStr == "" {
				daysStr = "75"
			}
			days, parseErr := strconv.Atoi(daysStr)
			if parseErr != nil {
				return errMsg{fmt.Errorf("invalid days value: %w", parseErr)}
			}
			constraints.CreationAge = &days
		}

		// Run analysis
		result, err := analyzer.Analyze(m.cluster, constraints)
		if err != nil {
			return errMsg{err}
		}

		return analysisCompleteMsg{result}
	}
}

// startClusterBalanceAnalysis creates cluster-wide balance analysis command
func (m Model) startClusterBalanceAnalysis() tea.Cmd {
	return func() tea.Msg {
		// Run cluster-wide balance analysis
		result, err := analyzer.AnalyzeClusterWideBalance(m.cluster, nil)
		if err != nil {
			return errMsg{err}
		}

		// For cluster balance, we don't have a single source node
		// Use "CLUSTER" as a marker or the first node with migrations
		sourceNode := "CLUSTER"
		if len(result.Suggestions) > 0 {
			sourceNode = result.Suggestions[0].SourceNode
		}

		return clusterBalanceCompleteMsg{result: result, sourceNode: sourceNode}
	}
}

// Messages
type errMsg struct {
	err error
}

type analysisCompleteMsg struct {
	result *analyzer.AnalysisResult
}

type clusterBalanceCompleteMsg struct {
	result     *analyzer.AnalysisResult
	sourceNode string
}
