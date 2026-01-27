package views

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// MigrationLogicContent contains all the migration algorithm documentation
// This should be updated whenever migration logic changes
var MigrationLogicContent = []string{
	// Page 1: Overview
	`MIGRATION ALGORITHM OVERVIEW
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
6. Specific VMs   - Manually select which VMs to migrate`,

	// Page 2: Resource Calculations
	`RESOURCE CALCULATIONS
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
• Displayed as percentage of total threads: LA / Threads × 100%`,

	// Page 3: Target Selection Algorithm
	`TARGET SELECTION ALGORITHM
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

The highest-scoring valid target is selected for each VM.`,

	// Page 4: Migrate All Mode Details
	`MIGRATE ALL MODE - DETAILED ALGORITHM
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
• This prevents creating new hotspots while evacuating`,

	// Page 5: VM Selection Criteria
	`VM SELECTION CRITERIA
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

When selecting which VMs to migrate (for non-"All" modes):

BY VM COUNT:
• Sorts VMs by combined resource impact (vCPUs × 10 + CPU% + RAM GB)
• Selects the specified number of VMs
• Prioritizes VMs with lower resource usage first

BY vCPU:
• Sorts VMs by vCPU count (smallest first)
• Selects VMs until total vCPUs reach or exceed target

BY CPU USAGE:
• Sorts VMs by CPU usage percentage (lowest first)
• Selects VMs until cumulative CPU% reaches target
• Note: A VM's CPU% contribution = its CPU% × vCPUs / Host threads

BY RAM:
• Sorts VMs by actual RAM usage (smallest first)
• Selects VMs until cumulative RAM reaches target

BY STORAGE:
• Sorts VMs by storage size (smallest first)
• Selects VMs until cumulative storage reaches target

SPECIFIC VMS:
• User manually selects which VMs to migrate
• No automatic selection criteria applied`,

	// Page 6: Constraints and Restrictions
	`CONSTRAINTS AND RESTRICTIONS
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
• "Already has N VMs (max: M)" - MaxVMsPerHost exceeded`,

	// Page 7: Score Breakdown
	`SCORE BREAKDOWN EXPLAINED
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
Migrate All:    Balance + Headroom (prefers below-average targets)`,

	// Page 8: Alternative Targets
	`ALTERNATIVE TARGETS
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
3. Does the VM have unusually high resource requirements?`,

	// Page 9: Keyboard Shortcuts
	`KEYBOARD SHORTCUTS
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

NAVIGATION:
  ↑/↓ or j/k        Navigate up/down
  PgUp/PgDn         Page up/down
  Home/End          Jump to first/last item
  Tab               Next section (in Results view)

SELECTION:
  Enter             Select / Confirm
  Space             Toggle checkbox (VM selection)
  1-8               Sort by column (Hosts view)

VIEWS:
  Esc               Go back to previous view
  r                 Refresh cluster data (Dashboard)
  ?                 Show this help page

QUIT:
  q                 Quit application
  Ctrl+C            Force quit

RESULTS VIEW:
  Tab               Switch between Migration Summary / Impact tables
  Enter             View host details
  ↑/↓               Browse VMs or hosts`,
}

// RenderMigrationLogic renders the migration logic explanation page
func RenderMigrationLogic(width, height, scrollPos int) string {
	var sb strings.Builder

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("5"))
	borderStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	contentStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("15"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	scrollTrackStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	scrollThumbStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("6"))

	// Title
	sb.WriteString(titleStyle.Render("Migration Algorithm Documentation") + "\n")
	sb.WriteString(borderStyle.Render(strings.Repeat("━", width)) + "\n")

	// Calculate total lines and pages
	totalPages := len(MigrationLogicContent)

	// Ensure scrollPos is within bounds
	if scrollPos < 0 {
		scrollPos = 0
	}
	if scrollPos >= totalPages {
		scrollPos = totalPages - 1
	}

	// Get current page content
	pageContent := MigrationLogicContent[scrollPos]

	// Split content into lines
	lines := strings.Split(pageContent, "\n")

	// Calculate available height for content (minus header, footer, help)
	availableHeight := height - 6

	// Render content lines with scrollbar
	needsScrollbar := totalPages > 1
	thumbPos := 0
	thumbSize := 1

	if needsScrollbar && totalPages > 0 {
		// Calculate thumb position based on current page
		thumbSize = max(1, availableHeight/totalPages)
		if thumbSize > availableHeight {
			thumbSize = availableHeight
		}
		scrollRange := availableHeight - thumbSize
		if scrollRange > 0 {
			thumbPos = scrollPos * scrollRange / (totalPages - 1)
		}
	}

	// Render each line of content
	for i := 0; i < availableHeight && i < len(lines); i++ {
		line := ""
		if i < len(lines) {
			line = lines[i]
		}

		// Pad line to width
		if len(line) < width-4 {
			line = line + strings.Repeat(" ", width-4-len(line))
		} else if len(line) > width-4 {
			line = line[:width-4]
		}

		sb.WriteString(contentStyle.Render(line))

		// Add scrollbar
		if needsScrollbar {
			if i >= thumbPos && i < thumbPos+thumbSize {
				sb.WriteString(" " + scrollThumbStyle.Render("█"))
			} else {
				sb.WriteString(" " + scrollTrackStyle.Render("│"))
			}
		}
		sb.WriteString("\n")
	}

	// Fill remaining lines if content is shorter
	for i := len(lines); i < availableHeight; i++ {
		sb.WriteString(strings.Repeat(" ", width-2))
		if needsScrollbar {
			if i >= thumbPos && i < thumbPos+thumbSize {
				sb.WriteString(" " + scrollThumbStyle.Render("█"))
			} else {
				sb.WriteString(" " + scrollTrackStyle.Render("│"))
			}
		}
		sb.WriteString("\n")
	}

	// Footer with page info
	sb.WriteString(borderStyle.Render(strings.Repeat("─", width)) + "\n")
	pageInfo := fmt.Sprintf("Page %d of %d", scrollPos+1, totalPages)
	padding := width - len(pageInfo) - 40
	if padding < 0 {
		padding = 0
	}
	sb.WriteString(dimStyle.Render("↑/↓/PgUp/PgDn: Navigate pages │ Esc: Close") +
		strings.Repeat(" ", padding) +
		dimStyle.Render(pageInfo))

	return sb.String()
}
