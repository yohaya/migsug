package ui

import (
	"fmt"
	"strconv"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yourusername/migsug/internal/analyzer"
	"github.com/yourusername/migsug/internal/proxmox"
	"github.com/yourusername/migsug/internal/ui/components"
	"github.com/yourusername/migsug/internal/ui/views"
)

const refreshInterval = 60 // seconds

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

	// Criteria state
	criteriaState views.CriteriaState

	// VM selection state (for ModeSpecific)
	vmCursorIdx int

	// Analysis results
	result *analyzer.AnalysisResult

	// Results view scroll state
	resultsScrollPos int

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
	switch msg.String() {
	case "up", "k":
		if m.selectedNodeIdx > 0 {
			m.selectedNodeIdx--
		}
	case "down", "j":
		if m.selectedNodeIdx < len(m.cluster.Nodes)-1 {
			m.selectedNodeIdx++
		}
	case "enter":
		// Select source node and move to criteria
		m.sourceNode = m.cluster.Nodes[m.selectedNodeIdx].Name
		m.criteriaState = views.CriteriaState{
			SelectedMode: analyzer.ModeVMCount,
			SelectedVMs:  make(map[int]bool),
		}
		m.currentView = ViewCriteria
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
		} else {
			// Otherwise, enable input
			m.criteriaState.InputFocused = true
		}
	case "esc":
		m.currentView = ViewDashboard
	}
	return m, nil
}

func (m Model) handleCriteriaInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		// Validate and start analysis
		m.criteriaState.InputFocused = false
		return m, m.startAnalysis()
	case "esc":
		m.criteriaState.InputFocused = false
	case "backspace":
		m.deleteLastChar()
	default:
		// Only allow digits and decimal point
		if len(msg.String()) == 1 {
			m.appendToInput(msg.String())
		}
	}
	return m, nil
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
			return m, m.startAnalysis()
		}
	case "esc":
		m.currentView = ViewCriteria
	}
	return m, nil
}

func (m Model) handleResultsKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.resultsScrollPos > 0 {
			m.resultsScrollPos--
		}
	case "down", "j":
		// Allow scrolling if there are more suggestions than fit on screen
		if m.result != nil {
			maxScroll := len(m.result.Suggestions) - 1
			if maxScroll < 0 {
				maxScroll = 0
			}
			if m.resultsScrollPos < maxScroll {
				m.resultsScrollPos++
			}
		}
	case "r":
		// Reset and start new analysis
		m.currentView = ViewCriteria
		m.result = nil
		m.resultsScrollPos = 0
	case "esc":
		// Go back to criteria screen (not dashboard)
		m.currentView = ViewCriteria
		m.resultsScrollPos = 0
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
		return views.RenderDashboardWithProgress(m.cluster, m.selectedNodeIdx, m.width, m.refreshCountdown, m.refreshing, m.version, progress)
	case ViewCriteria:
		return views.RenderCriteria(m.criteriaState, m.sourceNode, m.width)
	case ViewVMSelection:
		sourceNode := proxmox.GetNodeByName(m.cluster, m.sourceNode)
		if sourceNode == nil {
			return "Error: Source node not found"
		}
		return views.RenderVMSelection(sourceNode.VMs, m.criteriaState.SelectedVMs, m.vmCursorIdx, m.width)
	case ViewResults:
		if m.result != nil {
			return views.RenderResultsWithScroll(m.result, m.width, m.height, m.resultsScrollPos)
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
