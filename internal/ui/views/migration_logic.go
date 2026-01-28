package views

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// MigrationLogicContent contains all the migration algorithm documentation as a single continuous text
// This should be updated whenever migration logic changes
var MigrationLogicContent = `MIGRATION ALGORITHM OVERVIEW
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

The migration suggestion tool analyzes your Proxmox cluster and recommends optimal
VM placements to balance resources across nodes. It does NOT execute migrations -
it only provides suggestions.

CORE PRINCIPLES:
• Suggestions are based on current cluster state at analysis time
• The tool prioritizes cluster balance over individual node optimization
• Storage locality is not considered (assumes shared storage)
• HA groups and dependencies are not currently considered

MIGRATION MODES:
1. Migrate All    - Evacuate a host by distributing all VMs across the cluster
2. vCPU           - Migrate VMs until a target vCPU count is freed
3. CPU Usage (%)  - Migrate VMs until a target CPU usage percentage is freed
4. RAM (GiB)      - Migrate VMs until a target RAM amount is freed
5. Storage (GiB)  - Migrate VMs until a target storage amount is freed
6. Specific VMs   - Manually select which VMs to migrate


RESOURCE CALCULATIONS
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

CPU USAGE:
• Node CPU% = Actual CPU utilization reported by Proxmox
• vCPU count = Sum of allocated vCPUs for RUNNING VMs only
• vCPU overcommit = (Running vCPUs / Physical threads) × 100%
• Stopped VMs do not contribute to CPU usage

RAM USAGE:
• Only RUNNING VMs count toward "used" RAM
• Powered-off VMs do NOT count as using RAM
• Capacity check uses MaxMem (allocated) to ensure room to power on VM
• This means: a stopped 16GB VM uses 0GB but needs 16GB free to migrate

STORAGE:
• Uses allocated disk size (MaxDisk) when available
• Falls back to actual used disk (UsedDisk) if MaxDisk is 0
• Storage is always counted regardless of VM power state

LOAD AVERAGE:
• Shows 1-minute load average from the host
• Displayed as percentage of total threads: LA / Threads × 100%


MIGRATION IMPACT CALCULATIONS
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

The Migration Impact table shows resource changes before/after migration:

BEFORE VALUES:
• CPU%, RAM%, Storage% are actual values from Proxmox API
• These match the values shown in the dashboard and source node header
• Reflects real current state including all system processes

AFTER VALUES (Predicted):
• CPU After = CPU Before - (sum of migrating VMs' HCPU% contributions)
• RAM After = RAM Before - (sum of migrating VMs' allocated RAM)
• HCPU% = VM CPU% × VM vCPUs / Host Threads

This approach shows:
• Accurate current state (from Proxmox API)
• Estimated improvement based on VM resource contributions
• The "After" values are predictions, actual results may vary slightly


TARGET SELECTION ALGORITHM
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

When selecting a target host for a VM, the algorithm:

1. FILTERS OUT INVALID TARGETS:
   • Source node (can't migrate to self)
   • Excluded nodes (if specified)
   • Nodes without sufficient RAM capacity (must fit VM's MaxMem)
   • Nodes without sufficient storage capacity
   • Nodes exceeding MaxVMsPerHost limit (if set)

2. SCORES REMAINING TARGETS (for standard modes):
   • Utilization Score (70% weight): Lower resource usage = better
   • Balance Score (30% weight): How evenly distributed resources are

   Formula: Total = 0.7 × (100 - Utilization) + 0.3 × Balance

3. SCORING FOR "MIGRATE ALL" MODE:
   • Balance Score (20% weight): Resource distribution evenness
   • Headroom Score: Distance below cluster average
   • Targets must stay below cluster average + 5% margin
   • Prefers targets furthest below average

The highest-scoring valid target is selected for each VM.


MIGRATE ALL MODE - DETAILED ALGORITHM
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Purpose: Evacuate a host by spreading VMs across the cluster while keeping
all targets below the cluster average utilization.

PROCESS:
1. Calculate cluster averages (excluding source node):
   • Average CPU% across all other nodes
   • Average RAM% across all other nodes

2. For each VM on the source (sorted by resource impact):
   a. Find all valid targets
   b. Check if target would stay below average after adding VM
   c. Score targets by headroom (distance below average)
   d. Select target with best headroom + balance score

3. If no target stays below average:
   • Select the "best available" target
   • This is marked in the output

CLUSTER BALANCE TARGET:
• Target CPU% must be ≤ Cluster Average CPU% + 5%
• Target RAM% must be ≤ Cluster Average RAM% + 5%
• This prevents creating new hotspots while evacuating


VM SELECTION CRITERIA
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

When selecting which VMs to migrate (for non-"All" modes):

BY vCPU:
• Sorts VMs by vCPU count (smallest first)
• Selects VMs until total vCPUs reach or exceed target

BY CPU USAGE (Efficiency-Optimized):
• Uses automatic efficiency-based algorithm for fastest CPU relief
• Efficiency = Host CPU% / Disk Size (GiB)
• Higher efficiency = more CPU freed per GiB of data migrated
• VMs are sorted by efficiency (highest first) and selected until target is met

  EXAMPLE:
  VM-A: 5% CPU, 200 GiB disk → efficiency = 0.025
  VM-B: 0.067% CPU, 20 GiB disk → efficiency = 0.0033

  VM-A is selected first because it frees more CPU per migration time.
  Migrating 1×VM-A (200 GiB, 5% CPU) beats 30×VM-B (600 GiB, 2% CPU total).

• A VM's Host CPU% contribution = VM CPU% × vCPUs / Host threads (HCPU%)

BY RAM:
• Sorts VMs by actual RAM usage (smallest first)
• Selects VMs until cumulative RAM reaches target

BY STORAGE:
• Sorts VMs by storage size (smallest first)
• Selects VMs until cumulative storage reaches target

SPECIFIC VMS:
• User manually selects which VMs to migrate
• No automatic selection criteria applied


CONSTRAINTS AND RESTRICTIONS
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

ALWAYS APPLIED:
• RAM capacity check - Target must have room for VM's allocated RAM
• Storage capacity check - Target must have room for VM's storage

OPTIONAL CONSTRAINTS:
• ExcludeNodes - List of nodes that cannot be migration targets
• MaxVMsPerHost - Maximum VMs allowed on any target host
• MinRAMFree - Minimum free RAM required on target after migration
• MinCPUFree - Minimum free CPU% required on target after migration

CAPACITY VS USAGE:
• "Capacity" = Physical limit of the node
• "Usage" = Current utilization by running VMs
• A node may have capacity but be excluded due to balance concerns

REJECTION REASONS:
When a target is rejected, the reason is shown:
• "Insufficient RAM capacity" - Can't fit VM's MaxMem
• "Insufficient storage capacity" - Can't fit VM's storage
• "Would violate minimum RAM free constraint"
• "Would violate minimum CPU free constraint"
• "Already has N VMs (max: M)" - MaxVMsPerHost exceeded


VM CONFIG METADATA RESTRICTIONS
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

The tool reads VM config files to check for migration restrictions.
Config files are located at: /etc/pve/nodes/{node}/qemu-server/{vmid}.conf

COMMENT METADATA FORMAT:
VMs can have metadata in comment lines (starting with #) in CSV format:
#key1=value1,key2=value2,key3=value3,...

EXAMPLE:
#provisionedStorage=344,backup=7,cpuUsage=300,nomigrate=true,managed=no

SUPPORTED RESTRICTIONS:

1. nomigrate=true
   • VM will be completely excluded from migration suggestions
   • VM won't appear in any migration mode (All, CPU, RAM, etc.)
   • Use this for VMs that must stay on their current host
   • Example reasons: hardware passthrough, local storage dependency,
     licensing tied to hardware, etc.

FUTURE RESTRICTIONS (planned):
• migratetokvm={nodename} - Restrict migration to specific target node
• managed=no - Exclude from automated management
• (Additional restrictions can be added based on your metadata)

HOW IT WORKS:
1. During data collection, each VM's config file is read
2. Comment lines are parsed for key=value metadata
3. VMs with nomigrate=true are filtered out before analysis
4. All other VMs are processed normally

NOTE: If the config file cannot be read (permissions, file not found),
the VM is treated as migratable by default.


NODE STATUS INDICATORS
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

The Status column in the main dashboard shows indicators in parentheses:
Example: "online (OP)" means the node is online with both O and P indicators

INDICATORS:

O - OSD Node
   • Shown when the node has VMs with names matching osd*.cloudwm.com
   • Indicates the node hosts Ceph OSD services
   • These VMs are typically storage-critical

P - Provisioning Allowed
   • Shown when the node config has hostprovision=true
   • Config file: /etc/pve/nodes/{nodename}/config
   • Comment format: #hostprovision=true,...
   • Indicates new VMs can be provisioned to this node

EXAMPLES:
• "online"      - Standard online node
• "online (O)"  - Node has OSD VMs
• "online (P)"  - Node allows provisioning
• "online (OP)" - Node has OSD VMs and allows provisioning

NODE CONFIG FORMAT:
The node config file can contain metadata in comment lines:
#hoststate=1,hostprovision=true,otherkey=value

HOW IT WORKS:
1. During data collection, each node's config file is read
2. Comment lines are parsed for key=value metadata
3. VM names are checked against the osd*.cloudwm.com pattern
4. Indicators are displayed in the Status column


SCORE BREAKDOWN EXPLAINED
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

The migration reasoning panel shows detailed score breakdown:

SCORE COMPONENTS:

1. Utilization Score (weight: 70%)
   • Measures how much capacity the target has available
   • Formula: 100 - WeightedUtilization
   • WeightedUtilization = 0.4×CPU% + 0.4×RAM% + 0.2×Storage%
   • Higher score = more free resources

2. Balance Score (weight: 30%)
   • Measures how evenly distributed resources are
   • Based on standard deviation of CPU%, RAM%, Storage%
   • Lower variance = higher score
   • Formula: 100 - (StandardDeviation × 2)

3. Headroom Score (Migrate All mode)
   • Distance below cluster average
   • cpuHeadroom = ClusterAvgCPU% - TargetCPU%
   • ramHeadroom = ClusterAvgRAM% - TargetRAM%
   • Combined: 0.4×cpuHeadroom + 0.4×ramHeadroom

TOTAL SCORE:
Standard modes: 0.7 × Utilization + 0.3 × Balance
Migrate All:    Balance + Headroom (prefers below-average targets)


ALTERNATIVE TARGETS
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

The migration reasoning shows alternative targets that were considered:

WHAT'S SHOWN:
• Up to 3 alternative targets with their scores
• Comparison to the selected target's score
• Example: "kv0039 (score: 90.6) - Lower score (90.6 vs 91.1)"

WHY ALTERNATIVES MATTER:
• If scores are close, multiple hosts are nearly equivalent
• Consider your own knowledge (network topology, HA groups, etc.)
• You can manually choose a different target when executing

REJECTED TARGETS:
• Targets that failed validation are not shown as alternatives
• They are marked as rejected with a specific reason
• You can see all candidates in the detailed view

TIP: If the selected target seems wrong, check:
1. Are there constraints excluding better options?
2. Is the cluster heavily imbalanced?
3. Does the VM have unusually high resource requirements?


KEYBOARD SHORTCUTS
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

NAVIGATION:
  ↑/↓ or j/k        Navigate up/down (line by line)
  PgUp/PgDn         Page up/down
  Home/End          Jump to first/last line

SELECTION:
  Enter             Select / Confirm
  Space             Toggle checkbox (VM selection)
  1-8               Sort by column (Hosts view)

VIEWS:
  Esc               Go back to previous view
  r                 Refresh cluster data (Dashboard)
  ?                 Show this documentation

QUIT:
  q                 Quit application
  Ctrl+C            Force quit

RESULTS VIEW:
  Tab               Switch between Migration Summary / Impact tables
  Enter             View host details (in Impact table)
  ↑/↓               Browse VMs or hosts
  m                 Show migration commands (qm migrate)
  r                 New analysis with different criteria
`

// GetMigrationLogicLines returns all documentation lines
func GetMigrationLogicLines() []string {
	return strings.Split(MigrationLogicContent, "\n")
}

// GetMigrationLogicTotalLines returns total number of lines in documentation
func GetMigrationLogicTotalLines() int {
	return len(GetMigrationLogicLines())
}

// RenderMigrationLogic renders the migration logic explanation page with line-by-line scrolling
func RenderMigrationLogic(width, height, scrollPos int) string {
	var sb strings.Builder

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("5"))
	borderStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	contentStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("15"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	scrollTrackStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	scrollThumbStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("6"))

	// Get all lines
	allLines := GetMigrationLogicLines()
	totalLines := len(allLines)

	// Calculate available height for content (minus title, border, footer)
	availableHeight := height - 4

	// Ensure scrollPos is within bounds
	maxScroll := totalLines - availableHeight
	if maxScroll < 0 {
		maxScroll = 0
	}
	if scrollPos < 0 {
		scrollPos = 0
	}
	if scrollPos > maxScroll {
		scrollPos = maxScroll
	}

	// Title
	title := "Migration Algorithm Documentation"
	sb.WriteString(titleStyle.Render(title))
	sb.WriteString(strings.Repeat(" ", width-len(title)-1))
	sb.WriteString("\n")
	sb.WriteString(borderStyle.Render(strings.Repeat("━", width-2)))
	sb.WriteString("\n")

	// Calculate scrollbar
	needsScrollbar := totalLines > availableHeight
	thumbPos := 0
	thumbSize := 1

	if needsScrollbar {
		// Calculate thumb size proportional to visible area
		thumbSize = availableHeight * availableHeight / totalLines
		if thumbSize < 1 {
			thumbSize = 1
		}
		if thumbSize > availableHeight {
			thumbSize = availableHeight
		}

		// Calculate thumb position
		scrollRange := availableHeight - thumbSize
		if maxScroll > 0 && scrollRange > 0 {
			thumbPos = scrollPos * scrollRange / maxScroll
		}
	}

	// Content width (leave space for scrollbar on right edge)
	contentWidth := width - 3

	// Render visible content lines
	for i := 0; i < availableHeight; i++ {
		lineIdx := scrollPos + i
		line := ""
		if lineIdx < totalLines {
			line = allLines[lineIdx]
		}

		// Truncate or pad line to content width
		lineRunes := []rune(line)
		if len(lineRunes) > contentWidth {
			line = string(lineRunes[:contentWidth])
		} else {
			line = line + strings.Repeat(" ", contentWidth-len(lineRunes))
		}

		sb.WriteString(contentStyle.Render(line))

		// Add scrollbar at the rightmost position
		if needsScrollbar {
			if i >= thumbPos && i < thumbPos+thumbSize {
				sb.WriteString(" " + scrollThumbStyle.Render("█"))
			} else {
				sb.WriteString(" " + scrollTrackStyle.Render("│"))
			}
		} else {
			sb.WriteString("  ")
		}
		sb.WriteString("\n")
	}

	// Footer
	sb.WriteString(borderStyle.Render(strings.Repeat("─", width-2)) + "\n")
	helpText := "↑/↓: Scroll │ PgUp/PgDn: Page │ Home/End: Jump │ Esc: Close"
	lineInfo := fmt.Sprintf("Line %d-%d of %d", scrollPos+1, min(scrollPos+availableHeight, totalLines), totalLines)
	padding := width - len(helpText) - len(lineInfo) - 2
	if padding < 1 {
		padding = 1
	}
	sb.WriteString(dimStyle.Render(helpText) + strings.Repeat(" ", padding) + dimStyle.Render(lineInfo))

	return sb.String()
}
