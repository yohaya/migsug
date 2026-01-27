package analyzer

import (
	"fmt"
	"math"
	"sort"

	"github.com/yourusername/migsug/internal/proxmox"
)

// Analyze performs migration analysis and returns suggestions
func Analyze(cluster *proxmox.Cluster, constraints MigrationConstraints) (*AnalysisResult, error) {
	// Validate constraints
	if err := constraints.Validate(); err != nil {
		return nil, fmt.Errorf("invalid constraints: %w", err)
	}

	// Get source node
	sourceNode := proxmox.GetNodeByName(cluster, constraints.SourceNode)
	if sourceNode == nil {
		return nil, fmt.Errorf("source node %s not found", constraints.SourceNode)
	}

	// Get available target nodes
	targets := proxmox.GetAvailableTargets(cluster, constraints.SourceNode, constraints.ExcludeNodes)
	if len(targets) == 0 {
		return nil, fmt.Errorf("no available target nodes for migration")
	}

	// Select VMs to migrate based on constraints
	vmsToMigrate := SelectVMsToMigrate(sourceNode, constraints)
	if len(vmsToMigrate) == 0 {
		return nil, fmt.Errorf("no VMs selected for migration based on given constraints")
	}

	var suggestions []MigrationSuggestion

	// Use specialized algorithm for ModeAll to ensure balanced distribution
	if constraints.MigrateAll {
		suggestions = GenerateSuggestionsBalanced(vmsToMigrate, targets, cluster, sourceNode, constraints)
	} else {
		suggestions = GenerateSuggestions(vmsToMigrate, targets, sourceNode, constraints)
	}

	// Calculate before/after states
	result := BuildAnalysisResult(sourceNode, targets, suggestions, vmsToMigrate)

	return result, nil
}

// SelectVMsToMigrate selects which VMs to migrate based on constraints
func SelectVMsToMigrate(node *proxmox.Node, constraints MigrationConstraints) []proxmox.VM {
	mode := constraints.GetMode()

	switch mode {
	case ModeAll:
		return selectAllVMs(node)
	case ModeSpecific:
		return selectSpecificVMs(node, constraints.SpecificVMs)
	case ModeVMCount:
		return selectByVMCount(node, *constraints.VMCount)
	case ModeVCPU:
		return selectByVCPU(node, *constraints.VCPUCount)
	case ModeCPUUsage:
		// Use the strategy-aware CPU selection
		return selectByCPUUsageWithStrategy(node, *constraints.CPUUsage, constraints.CPUStrategy)
	case ModeRAM:
		return selectByRAM(node, *constraints.RAMAmount)
	case ModeStorage:
		return selectByStorage(node, *constraints.StorageAmount)
	default:
		return []proxmox.VM{}
	}
}

// selectAllVMs returns ALL VMs from the node for "Migrate All" mode (including powered-off)
func selectAllVMs(node *proxmox.Node) []proxmox.VM {
	// Return ALL VMs (running and stopped) sorted by resource usage (largest first for better distribution)
	vms := make([]proxmox.VM, len(node.VMs))
	copy(vms, node.VMs)

	// Sort by combined resource score (descending - distribute largest VMs first)
	// Running VMs get priority, then by size
	sort.Slice(vms, func(i, j int) bool {
		// Running VMs first
		if vms[i].Status != vms[j].Status {
			return vms[i].Status == "running"
		}
		scoreI := float64(vms[i].CPUCores)*10 + vms[i].CPUUsage + float64(vms[i].MaxMem)/(1024*1024*1024)
		scoreJ := float64(vms[j].CPUCores)*10 + vms[j].CPUUsage + float64(vms[j].MaxMem)/(1024*1024*1024)
		return scoreI > scoreJ
	})

	return vms
}

func selectSpecificVMs(node *proxmox.Node, vmids []int) []proxmox.VM {
	vmMap := make(map[int]proxmox.VM)
	for _, vm := range node.VMs {
		vmMap[vm.VMID] = vm
	}

	var selected []proxmox.VM
	for _, vmid := range vmids {
		if vm, exists := vmMap[vmid]; exists {
			selected = append(selected, vm)
		}
	}
	return selected
}

// filterRunningVMs returns only running VMs (excludes stopped/paused)
func filterRunningVMs(vms []proxmox.VM) []proxmox.VM {
	var running []proxmox.VM
	for _, vm := range vms {
		if vm.Status == "running" {
			running = append(running, vm)
		}
	}
	return running
}

func selectByVMCount(node *proxmox.Node, count int) []proxmox.VM {
	// Only select running VMs (don't migrate powered off VMs)
	vms := filterRunningVMs(node.VMs)

	// Sort VMs by resource usage (ascending - move least impactful first)
	sort.Slice(vms, func(i, j int) bool {
		scoreI := vms[i].CPUUsage + vms[i].GetMemPercent()
		scoreJ := vms[j].CPUUsage + vms[j].GetMemPercent()
		return scoreI < scoreJ
	})

	if count > len(vms) {
		count = len(vms)
	}

	return vms[:count]
}

func selectByVCPU(node *proxmox.Node, targetVCPUs int) []proxmox.VM {
	// Only select running VMs (don't migrate powered off VMs)
	vms := filterRunningVMs(node.VMs)

	// Sort VMs by vCPU count
	sort.Slice(vms, func(i, j int) bool {
		return vms[i].CPUCores < vms[j].CPUCores
	})

	// Greedy selection to reach target vCPU count
	var selected []proxmox.VM
	totalVCPUs := 0

	for _, vm := range vms {
		if totalVCPUs >= targetVCPUs {
			break
		}
		selected = append(selected, vm)
		totalVCPUs += vm.CPUCores
	}

	return selected
}

func selectByCPUUsage(node *proxmox.Node, targetUsage float64) []proxmox.VM {
	// Use default strategy (Thorough) for backward compatibility
	return selectByCPUUsageWithStrategy(node, targetUsage, StrategyThorough)
}

// selectByCPUUsageWithStrategy selects VMs to migrate based on CPU usage with a tiered strategy
// The algorithm prioritizes small disk VMs with high CPU first for faster migration times
func selectByCPUUsageWithStrategy(node *proxmox.Node, targetUsage float64, strategy MigrationStrategy) []proxmox.VM {
	// Only select running VMs (don't migrate powered off VMs)
	vms := filterRunningVMs(node.VMs)

	// Calculate each VM's actual host CPU% contribution and categorize by disk tier
	type vmWithMetrics struct {
		vm      proxmox.VM
		hostCPU float64      // Actual host CPU% contribution (HCPU%)
		tier    DiskSizeTier // Disk size tier
		disk    int64        // Disk size in bytes
	}

	vmsWithMetrics := make([]vmWithMetrics, 0, len(vms))
	for _, vm := range vms {
		hostCPU := 0.0
		if node.CPUCores > 0 {
			hostCPU = vm.CPUUsage * float64(vm.CPUCores) / float64(node.CPUCores)
		}

		// Get disk size (prefer MaxDisk, fallback to UsedDisk)
		disk := vm.MaxDisk
		if disk == 0 {
			disk = vm.UsedDisk
		}

		// Determine tier based on disk size
		tier := TierLarge
		if disk <= SmallDiskThreshold {
			tier = TierSmall
		} else if disk <= MediumDiskThreshold {
			tier = TierMedium
		}

		vmsWithMetrics = append(vmsWithMetrics, vmWithMetrics{
			vm:      vm,
			hostCPU: hostCPU,
			tier:    tier,
			disk:    disk,
		})
	}

	// Determine which tiers to include based on strategy
	maxTier := TierLarge
	switch strategy {
	case StrategyQuick:
		maxTier = TierSmall
	case StrategyBalanced:
		maxTier = TierMedium
	case StrategyThorough:
		maxTier = TierLarge
	}

	// Sort VMs by tier (small first), then by host CPU% descending within tier
	// This ensures small, high-CPU VMs are selected first for fastest relief
	sort.Slice(vmsWithMetrics, func(i, j int) bool {
		// First compare by tier (smaller tier = faster migration = higher priority)
		if vmsWithMetrics[i].tier != vmsWithMetrics[j].tier {
			return vmsWithMetrics[i].tier < vmsWithMetrics[j].tier
		}
		// Within same tier, prefer higher CPU usage (migrate high-CPU VMs first)
		return vmsWithMetrics[i].hostCPU > vmsWithMetrics[j].hostCPU
	})

	// Select VMs until we reach target host CPU%, respecting tier limits
	var selected []proxmox.VM
	totalHostCPU := 0.0

	for _, v := range vmsWithMetrics {
		if totalHostCPU >= targetUsage {
			break
		}

		// Skip VMs from tiers above the strategy limit
		if v.tier > maxTier {
			continue
		}

		selected = append(selected, v.vm)
		totalHostCPU += v.hostCPU
	}

	return selected
}

// SelectByCPUUsageDetailed returns selected VMs along with detailed tier information
// This is useful for displaying migration plan details to the user
func SelectByCPUUsageDetailed(node *proxmox.Node, targetUsage float64, strategy MigrationStrategy) ([]proxmox.VM, *CPUMigrationPlan) {
	vms := filterRunningVMs(node.VMs)

	plan := &CPUMigrationPlan{
		TargetCPUReduction: targetUsage,
		Strategy:           strategy,
		TierStats:          make(map[DiskSizeTier]*TierStat),
	}

	// Initialize tier stats
	plan.TierStats[TierSmall] = &TierStat{Name: "Small (≤100 GiB)"}
	plan.TierStats[TierMedium] = &TierStat{Name: "Medium (100-500 GiB)"}
	plan.TierStats[TierLarge] = &TierStat{Name: "Large (>500 GiB)"}

	type vmWithMetrics struct {
		vm      proxmox.VM
		hostCPU float64
		tier    DiskSizeTier
		disk    int64
	}

	vmsWithMetrics := make([]vmWithMetrics, 0, len(vms))
	for _, vm := range vms {
		hostCPU := 0.0
		if node.CPUCores > 0 {
			hostCPU = vm.CPUUsage * float64(vm.CPUCores) / float64(node.CPUCores)
		}

		disk := vm.MaxDisk
		if disk == 0 {
			disk = vm.UsedDisk
		}

		tier := TierLarge
		if disk <= SmallDiskThreshold {
			tier = TierSmall
		} else if disk <= MediumDiskThreshold {
			tier = TierMedium
		}

		// Update tier stats (total available)
		plan.TierStats[tier].TotalVMs++
		plan.TierStats[tier].TotalCPU += hostCPU
		plan.TierStats[tier].TotalDisk += disk

		vmsWithMetrics = append(vmsWithMetrics, vmWithMetrics{
			vm:      vm,
			hostCPU: hostCPU,
			tier:    tier,
			disk:    disk,
		})
	}

	maxTier := TierLarge
	switch strategy {
	case StrategyQuick:
		maxTier = TierSmall
	case StrategyBalanced:
		maxTier = TierMedium
	case StrategyThorough:
		maxTier = TierLarge
	}

	// Sort by tier then by CPU descending
	sort.Slice(vmsWithMetrics, func(i, j int) bool {
		if vmsWithMetrics[i].tier != vmsWithMetrics[j].tier {
			return vmsWithMetrics[i].tier < vmsWithMetrics[j].tier
		}
		return vmsWithMetrics[i].hostCPU > vmsWithMetrics[j].hostCPU
	})

	var selected []proxmox.VM
	totalHostCPU := 0.0

	for _, v := range vmsWithMetrics {
		if totalHostCPU >= targetUsage {
			break
		}
		if v.tier > maxTier {
			continue
		}

		selected = append(selected, v.vm)
		totalHostCPU += v.hostCPU

		// Update selected stats
		plan.TierStats[v.tier].SelectedVMs++
		plan.TierStats[v.tier].SelectedCPU += v.hostCPU
		plan.TierStats[v.tier].SelectedDisk += v.disk
	}

	plan.AchievedCPUReduction = totalHostCPU
	plan.TotalVMsSelected = len(selected)

	// Calculate if target was reached
	plan.TargetReached = totalHostCPU >= targetUsage
	if !plan.TargetReached {
		plan.Shortfall = targetUsage - totalHostCPU
	}

	return selected, plan
}

// CPUMigrationPlan contains detailed information about the CPU migration selection
type CPUMigrationPlan struct {
	TargetCPUReduction  float64
	AchievedCPUReduction float64
	Strategy            MigrationStrategy
	TotalVMsSelected    int
	TargetReached       bool
	Shortfall           float64 // How much CPU% we couldn't free (if target not reached)
	TierStats           map[DiskSizeTier]*TierStat
}

// TierStat contains statistics for a disk size tier
type TierStat struct {
	Name         string
	TotalVMs     int     // Total VMs in this tier on source host
	TotalCPU     float64 // Total HCPU% available in this tier
	TotalDisk    int64   // Total disk space in this tier
	SelectedVMs  int     // VMs selected for migration
	SelectedCPU  float64 // HCPU% being migrated
	SelectedDisk int64   // Disk space being migrated
}

func selectByRAM(node *proxmox.Node, targetRAM int64) []proxmox.VM {
	// Only select running VMs (don't migrate powered off VMs)
	vms := filterRunningVMs(node.VMs)

	// Sort VMs by RAM usage
	sort.Slice(vms, func(i, j int) bool {
		return vms[i].UsedMem < vms[j].UsedMem
	})

	// Select VMs until we reach target RAM
	var selected []proxmox.VM
	totalRAM := int64(0)

	for _, vm := range vms {
		if totalRAM >= targetRAM {
			break
		}
		selected = append(selected, vm)
		totalRAM += vm.UsedMem
	}

	return selected
}

func selectByStorage(node *proxmox.Node, targetStorage int64) []proxmox.VM {
	// Include ALL VMs (even stopped ones) since we need to free storage
	// Sort VMs by storage usage
	vms := make([]proxmox.VM, len(node.VMs))
	copy(vms, node.VMs)

	sort.Slice(vms, func(i, j int) bool {
		return vms[i].UsedDisk < vms[j].UsedDisk
	})

	// Select VMs until we reach target storage
	var selected []proxmox.VM
	totalStorage := int64(0)

	for _, vm := range vms {
		if totalStorage >= targetStorage {
			break
		}
		selected = append(selected, vm)
		totalStorage += vm.UsedDisk
	}

	return selected
}

// GenerateSuggestions creates migration suggestions by finding best targets
func GenerateSuggestions(vms []proxmox.VM, targets []proxmox.Node, sourceNode *proxmox.Node, constraints MigrationConstraints) []MigrationSuggestion {
	var suggestions []MigrationSuggestion

	// Track target states for capacity checking
	targetStates := make(map[string]NodeState)
	targetCoresMap := make(map[string]int)
	for _, target := range targets {
		targetStates[target.Name] = NewNodeState(&target)
		targetCoresMap[target.Name] = target.CPUCores
	}

	// Source node cores for HCPU% calculation
	sourceCores := 0
	if sourceNode != nil {
		sourceCores = sourceNode.CPUCores
	}

	// Track VMs per target for MaxVMsPerHost constraint
	vmsPerTarget := make(map[string]int)
	for _, target := range targets {
		vmsPerTarget[target.Name] = len(target.VMs)
	}

	// Determine selection mode for details
	selectionMode := constraints.GetMode().String()
	selectionReason := getSelectionReason(constraints)

	// For each VM, find the best target
	for _, vm := range vms {
		targetNode, score, reason, details := FindBestTarget(vm, targetStates, vmsPerTarget, constraints)

		// Add selection info to details
		if details != nil {
			details.SelectionMode = selectionMode
			details.SelectionReason = selectionReason
		}

		if targetNode == "" {
			// No suitable target found
			reason = "No suitable target with sufficient capacity"
			targetNode = "NONE"
			score = 0
		} else {
			// Update target state after accepting this VM
			state := targetStates[targetNode]
			state = state.CalculateAfterMigration([]proxmox.VM{vm}, nil)
			targetStates[targetNode] = state
			vmsPerTarget[targetNode]++
		}

		// Use MaxDisk (allocated storage) since UsedDisk is often 0 from the API
		storageValue := vm.MaxDisk
		if storageValue == 0 {
			storageValue = vm.UsedDisk
		}

		suggestion := MigrationSuggestion{
			VMID:        vm.VMID,
			VMName:      vm.Name,
			SourceNode:  vm.Node,
			TargetNode:  targetNode,
			Reason:      reason,
			Score:       score,
			Status:      vm.Status,
			VCPUs:       vm.CPUCores,
			CPUUsage:    vm.CPUUsage,
			RAM:         vm.MaxMem, // Use allocated RAM
			Storage:     storageValue,
			SourceCores: sourceCores,
			TargetCores: targetCoresMap[targetNode],
			Details:     details,
		}

		suggestions = append(suggestions, suggestion)
	}

	return suggestions
}

// getSelectionReason returns a human-readable reason for VM selection
func getSelectionReason(constraints MigrationConstraints) string {
	mode := constraints.GetMode()
	switch mode {
	case ModeAll:
		return "Selected for full host evacuation (Migrate All mode)"
	case ModeSpecific:
		return "Manually selected by user"
	case ModeVMCount:
		return fmt.Sprintf("Selected as one of %d VMs with lowest resource usage", *constraints.VMCount)
	case ModeVCPU:
		return fmt.Sprintf("Selected to free up %d vCPUs from source host", *constraints.VCPUCount)
	case ModeCPUUsage:
		strategyDesc := ""
		switch constraints.CPUStrategy {
		case StrategyQuick:
			strategyDesc = " (Quick Relief - small VMs only)"
		case StrategyBalanced:
			strategyDesc = " (Balanced - small/medium VMs)"
		case StrategyThorough:
			strategyDesc = " (Thorough - all sizes)"
		}
		return fmt.Sprintf("Selected to reduce CPU usage by %.1f%% on source host%s", *constraints.CPUUsage, strategyDesc)
	case ModeRAM:
		return fmt.Sprintf("Selected to free up %d GB RAM from source host", *constraints.RAMAmount/(1024*1024*1024))
	case ModeStorage:
		return fmt.Sprintf("Selected to free up %d GB storage from source host", *constraints.StorageAmount/(1024*1024*1024))
	default:
		return "Selected based on migration criteria"
	}
}

// FindBestTarget finds the best target node for a VM and returns detailed reasoning
func FindBestTarget(vm proxmox.VM, targetStates map[string]NodeState, vmsPerTarget map[string]int, constraints MigrationConstraints) (string, float64, string, *MigrationDetails) {
	type candidate struct {
		name         string
		score        float64
		reason       string
		stateBefore  NodeState
		stateAfter   NodeState
		breakdown    ScoreBreakdown
		rejected     bool
		rejectReason string
	}

	var allCandidates []candidate
	var constraintsApplied []string

	// Track which constraints are being checked
	constraintsApplied = append(constraintsApplied, "RAM capacity check")
	constraintsApplied = append(constraintsApplied, "Storage capacity check")
	if constraints.MinRAMFree != nil {
		constraintsApplied = append(constraintsApplied, fmt.Sprintf("Min RAM free: %d GB", *constraints.MinRAMFree/(1024*1024*1024)))
	}
	if constraints.MinCPUFree != nil {
		constraintsApplied = append(constraintsApplied, fmt.Sprintf("Min CPU free: %.0f%%", *constraints.MinCPUFree))
	}
	if constraints.MaxVMsPerHost != nil {
		constraintsApplied = append(constraintsApplied, fmt.Sprintf("Max VMs per host: %d", *constraints.MaxVMsPerHost))
	}

	for name, state := range targetStates {
		cand := candidate{
			name:        name,
			stateBefore: state,
		}

		// Check capacity
		if !state.HasCapacity(vm, constraints) {
			cand.rejected = true
			// Determine specific reason - use MaxMem to check if VM can be powered on
			newRAMUsed := state.RAMUsed + vm.MaxMem
			if newRAMUsed > state.RAMTotal {
				cand.rejectReason = "Insufficient RAM capacity"
			} else {
				storage := vm.MaxDisk
				if storage == 0 {
					storage = vm.UsedDisk
				}
				newStorageUsed := state.StorageUsed + storage
				if newStorageUsed > state.StorageTotal {
					cand.rejectReason = "Insufficient storage capacity"
				} else if constraints.MinRAMFree != nil {
					cand.rejectReason = "Would violate minimum RAM free constraint"
				} else if constraints.MinCPUFree != nil {
					cand.rejectReason = "Would violate minimum CPU free constraint"
				}
			}
			allCandidates = append(allCandidates, cand)
			continue
		}

		// Check MaxVMsPerHost constraint
		if constraints.MaxVMsPerHost != nil {
			if vmsPerTarget[name] >= *constraints.MaxVMsPerHost {
				cand.rejected = true
				cand.rejectReason = fmt.Sprintf("Already has %d VMs (max: %d)", vmsPerTarget[name], *constraints.MaxVMsPerHost)
				allCandidates = append(allCandidates, cand)
				continue
			}
		}

		// Calculate score after adding this VM
		newState := state.CalculateAfterMigration([]proxmox.VM{vm}, nil)
		cand.stateAfter = newState

		// Calculate detailed score breakdown
		utilizationScore := 100 - newState.GetUtilizationScore()
		balanceScore := calculateBalanceScoreDetailed(newState)

		cand.breakdown = ScoreBreakdown{
			UtilizationScore:  utilizationScore,
			BalanceScore:      balanceScore,
			UtilizationWeight: 0.7,
			BalanceWeight:     0.3,
			TotalScore:        0.7*utilizationScore + 0.3*balanceScore,
		}
		cand.score = cand.breakdown.TotalScore

		cand.reason = fmt.Sprintf("Good balance (CPU: %.1f%%, RAM: %.1f%%, Storage: %.1f%%)",
			newState.CPUPercent, newState.RAMPercent, newState.StoragePercent)

		allCandidates = append(allCandidates, cand)
	}

	// Separate valid and rejected candidates
	var validCandidates []candidate
	var rejectedCandidates []candidate
	for _, c := range allCandidates {
		if c.rejected {
			rejectedCandidates = append(rejectedCandidates, c)
		} else {
			validCandidates = append(validCandidates, c)
		}
	}

	if len(validCandidates) == 0 {
		// Build details even for failure case
		details := &MigrationDetails{
			ConstraintsApplied: constraintsApplied,
		}
		// Add rejected alternatives
		for _, c := range rejectedCandidates {
			details.Alternatives = append(details.Alternatives, AlternativeTarget{
				Name:            c.name,
				Score:           0,
				RejectionReason: c.rejectReason,
			})
		}
		return "", 0, "No suitable target found", details
	}

	// Sort by score (higher is better)
	sort.Slice(validCandidates, func(i, j int) bool {
		return validCandidates[i].score > validCandidates[j].score
	})

	best := validCandidates[0]

	// Build detailed migration reasoning
	details := &MigrationDetails{
		ScoreBreakdown: best.breakdown,
		TargetBefore: ResourceState{
			CPUPercent:     best.stateBefore.CPUPercent,
			RAMPercent:     best.stateBefore.RAMPercent,
			StoragePercent: best.stateBefore.StoragePercent,
			VMCount:        best.stateBefore.VMCount,
			VCPUs:          best.stateBefore.VCPUs,
			RAMUsed:        best.stateBefore.RAMUsed,
			RAMTotal:       best.stateBefore.RAMTotal,
			StorageUsed:    best.stateBefore.StorageUsed,
			StorageTotal:   best.stateBefore.StorageTotal,
		},
		TargetAfter: ResourceState{
			CPUPercent:     best.stateAfter.CPUPercent,
			RAMPercent:     best.stateAfter.RAMPercent,
			StoragePercent: best.stateAfter.StoragePercent,
			VMCount:        best.stateAfter.VMCount,
			VCPUs:          best.stateAfter.VCPUs,
			RAMUsed:        best.stateAfter.RAMUsed,
			RAMTotal:       best.stateAfter.RAMTotal,
			StorageUsed:    best.stateAfter.StorageUsed,
			StorageTotal:   best.stateAfter.StorageTotal,
		},
		ConstraintsApplied: constraintsApplied,
	}

	// Add alternative targets (other valid options)
	for i, c := range validCandidates {
		if i == 0 {
			continue // Skip the chosen one
		}
		if i > 3 {
			break // Only show top 3 alternatives
		}
		details.Alternatives = append(details.Alternatives, AlternativeTarget{
			Name:            c.name,
			Score:           c.score,
			RejectionReason: fmt.Sprintf("Lower score (%.1f vs %.1f)", c.score, best.score),
			CPUAfter:        c.stateAfter.CPUPercent,
			RAMAfter:        c.stateAfter.RAMPercent,
			StorageAfter:    c.stateAfter.StoragePercent,
		})
	}

	// Add rejected targets
	for _, c := range rejectedCandidates {
		if len(details.Alternatives) >= 5 {
			break
		}
		details.Alternatives = append(details.Alternatives, AlternativeTarget{
			Name:            c.name,
			Score:           0,
			RejectionReason: c.rejectReason,
		})
	}

	return best.name, best.score, best.reason, details
}

// calculateBalanceScoreDetailed calculates balance score with more detail
func calculateBalanceScoreDetailed(state NodeState) float64 {
	resources := []float64{state.CPUPercent, state.RAMPercent, state.StoragePercent}
	mean := (resources[0] + resources[1] + resources[2]) / 3
	variance := 0.0
	for _, r := range resources {
		variance += math.Pow(r-mean, 2)
	}
	stdDev := math.Sqrt(variance / 3)
	return 100 - stdDev
}

// calculateTargetScore computes a score for target selection
func calculateTargetScore(before, after NodeState) float64 {
	// Prefer targets with:
	// 1. Lower final utilization
	// 2. Balanced resource usage
	// 3. More headroom

	// Utilization score (lower utilization = higher score)
	utilizationScore := 100 - after.GetUtilizationScore()

	// Balance score (more balanced = higher score)
	// Penalize if one resource is much higher than others
	balanceScore := 100.0
	resources := []float64{after.CPUPercent, after.RAMPercent, after.StoragePercent}
	mean := (resources[0] + resources[1] + resources[2]) / 3
	variance := 0.0
	for _, r := range resources {
		variance += math.Pow(r-mean, 2)
	}
	stdDev := math.Sqrt(variance / 3)
	balanceScore -= stdDev // Penalize high standard deviation

	// Combine scores with weights
	score := 0.7*utilizationScore + 0.3*balanceScore

	return score
}

// BuildAnalysisResult creates the final analysis result with before/after states
func BuildAnalysisResult(sourceNode *proxmox.Node, targets []proxmox.Node, suggestions []MigrationSuggestion, vmsToMigrate []proxmox.VM) *AnalysisResult {
	result := &AnalysisResult{
		Suggestions:   suggestions,
		TargetsBefore: make(map[string]NodeState),
		TargetsAfter:  make(map[string]NodeState),
	}

	// Source before state - use VM-aggregated values for Migration Impact
	result.SourceBefore = NewNodeStateFromVMs(sourceNode)

	// Source after state (remove migrated VMs)
	result.SourceAfter = result.SourceBefore.CalculateAfterMigration(nil, vmsToMigrate)

	// Target states
	targetVMs := make(map[string][]proxmox.VM)
	for _, suggestion := range suggestions {
		if suggestion.TargetNode != "NONE" {
			for _, vm := range vmsToMigrate {
				if vm.VMID == suggestion.VMID {
					targetVMs[suggestion.TargetNode] = append(targetVMs[suggestion.TargetNode], vm)
					break
				}
			}
		}
	}

	for _, target := range targets {
		// Use VM-aggregated values for Migration Impact
		result.TargetsBefore[target.Name] = NewNodeStateFromVMs(&target)
		addVMs := targetVMs[target.Name]
		result.TargetsAfter[target.Name] = result.TargetsBefore[target.Name].CalculateAfterMigration(addVMs, nil)
	}

	// Calculate summary - use Max values (allocated) since Used values are often 0
	result.TotalVMs = len(suggestions)
	for _, vm := range vmsToMigrate {
		result.TotalVCPUs += vm.CPUCores
		result.TotalRAM += vm.MaxMem // Allocated RAM
		storageVal := vm.MaxDisk
		if storageVal == 0 {
			storageVal = vm.UsedDisk
		}
		result.TotalStorage += storageVal
	}

	// Generate improvement info
	cpuImprovement := result.SourceBefore.CPUPercent - result.SourceAfter.CPUPercent
	ramImprovement := result.SourceBefore.RAMPercent - result.SourceAfter.RAMPercent
	result.ImprovementInfo = fmt.Sprintf("CPU: -%.1f%%, RAM: -%.1f%%", cpuImprovement, ramImprovement)

	return result
}

// ClusterAverages holds the target average resource usage for the cluster
type ClusterAverages struct {
	CPUPercent     float64
	RAMPercent     float64
	StoragePercent float64
}

// CalculateTargetAverages calculates what the cluster average should be after
// all VMs from source are distributed (excluding the source node from calculation)
func CalculateTargetAverages(cluster *proxmox.Cluster, sourceNode string, vmsToMigrate []proxmox.VM) ClusterAverages {
	// Calculate total resources of all target nodes (excluding source)
	var totalCPUCores int
	var totalCPUUsage float64
	var totalRAM, usedRAM int64
	var totalStorage, usedStorage int64
	var nodeCount int

	for _, node := range cluster.Nodes {
		if node.Name == sourceNode || node.Status != "online" {
			continue
		}
		nodeCount++
		totalCPUCores += node.CPUCores
		totalCPUUsage += node.CPUUsage * float64(node.CPUCores)
		totalRAM += node.MaxMem
		usedRAM += node.UsedMem
		totalStorage += node.MaxDisk
		usedStorage += node.UsedDisk
	}

	// Add resources from VMs being migrated to the pool
	for _, vm := range vmsToMigrate {
		totalCPUUsage += vm.CPUUsage / 100 * float64(vm.CPUCores)
		usedRAM += vm.UsedMem
		storage := vm.MaxDisk
		if storage == 0 {
			storage = vm.UsedDisk
		}
		usedStorage += storage
	}

	// Calculate target averages
	averages := ClusterAverages{}
	if totalCPUCores > 0 {
		averages.CPUPercent = (totalCPUUsage / float64(totalCPUCores)) * 100
	}
	if totalRAM > 0 {
		averages.RAMPercent = float64(usedRAM) / float64(totalRAM) * 100
	}
	if totalStorage > 0 {
		averages.StoragePercent = float64(usedStorage) / float64(totalStorage) * 100
	}

	return averages
}

// GenerateSuggestionsBalanced creates migration suggestions that distribute
// ALL VMs from the source host across target nodes.
// This is used for "Migrate All" mode - every VM MUST be assigned a target.
func GenerateSuggestionsBalanced(vms []proxmox.VM, targets []proxmox.Node, cluster *proxmox.Cluster, sourceNode *proxmox.Node, constraints MigrationConstraints) []MigrationSuggestion {
	// Calculate target averages for the cluster after migration
	targetAverages := CalculateTargetAverages(cluster, constraints.SourceNode, vms)

	// Initialize target states (sequential processing for accurate state tracking)
	targetStates := make(map[string]NodeState)
	targetCoresMap := make(map[string]int)
	vmsPerTarget := make(map[string]int)

	for _, target := range targets {
		targetStates[target.Name] = NewNodeState(&target)
		targetCoresMap[target.Name] = target.CPUCores
		vmsPerTarget[target.Name] = len(target.VMs)
	}

	// Source node cores for HCPU% calculation
	sourceCores := 0
	if sourceNode != nil {
		sourceCores = sourceNode.CPUCores
	}

	suggestions := make([]MigrationSuggestion, 0, len(vms))

	// Process VMs sequentially to ensure accurate state tracking and ALL VMs get assigned
	// VMs are already sorted by size (largest first) from selectAllVMs
	for _, vm := range vms {
		// Find best target for this VM
		targetName, score, reason, details := findBestTargetForMigrateAll(vm, targetStates, vmsPerTarget, targetAverages, constraints)

		// Add selection info to details
		if details != nil {
			details.SelectionMode = ModeAll.String()
			details.SelectionReason = "Selected for full host evacuation (Migrate All mode)"
		}

		// Update target state after placement
		if targetName != "" && targetName != "NONE" {
			state := targetStates[targetName]
			targetStates[targetName] = state.CalculateAfterMigration([]proxmox.VM{vm}, nil)
			vmsPerTarget[targetName]++
		}

		// Build suggestion
		storageValue := vm.MaxDisk
		if storageValue == 0 {
			storageValue = vm.UsedDisk
		}

		suggestion := MigrationSuggestion{
			VMID:        vm.VMID,
			VMName:      vm.Name,
			SourceNode:  vm.Node,
			TargetNode:  targetName,
			Reason:      reason,
			Score:       score,
			Status:      vm.Status,
			VCPUs:       vm.CPUCores,
			CPUUsage:    vm.CPUUsage,
			RAM:         vm.MaxMem,
			Storage:     storageValue,
			SourceCores: sourceCores,
			TargetCores: targetCoresMap[targetName],
			Details:     details,
		}
		suggestions = append(suggestions, suggestion)
	}

	return suggestions
}

// findBestTargetForMigrateAll finds the best target for a VM in "Migrate All" mode.
// Unlike regular mode, this ALWAYS returns a valid target (never "NONE").
func findBestTargetForMigrateAll(vm proxmox.VM, targetStates map[string]NodeState, vmsPerTarget map[string]int, averages ClusterAverages, constraints MigrationConstraints) (string, float64, string, *MigrationDetails) {
	type candidate struct {
		name         string
		score        float64
		reason       string
		belowAverage bool
		stateBefore  NodeState
		stateAfter   NodeState
		breakdown    ScoreBreakdown
		rejected     bool
		rejectReason string
	}

	var allCandidates []candidate
	var constraintsApplied []string

	constraintsApplied = append(constraintsApplied, "RAM capacity check")
	constraintsApplied = append(constraintsApplied, "Storage capacity check")
	constraintsApplied = append(constraintsApplied, fmt.Sprintf("Cluster balance target (CPU: %.1f%%, RAM: %.1f%%)", averages.CPUPercent, averages.RAMPercent))
	if constraints.MaxVMsPerHost != nil {
		constraintsApplied = append(constraintsApplied, fmt.Sprintf("Max VMs per host: %d", *constraints.MaxVMsPerHost))
	}

	// Evaluate all targets
	for name, state := range targetStates {
		cand := candidate{
			name:        name,
			stateBefore: state,
		}

		// Check basic capacity (RAM and storage must fit)
		// Use MaxMem to ensure there's room to power on the VM even if currently stopped
		newRAMUsed := state.RAMUsed + vm.MaxMem
		if newRAMUsed > state.RAMTotal {
			cand.rejected = true
			cand.rejectReason = "Insufficient RAM capacity"
			allCandidates = append(allCandidates, cand)
			continue
		}

		storage := vm.MaxDisk
		if storage == 0 {
			storage = vm.UsedDisk
		}
		newStorageUsed := state.StorageUsed + storage
		if newStorageUsed > state.StorageTotal {
			cand.rejected = true
			cand.rejectReason = "Insufficient storage capacity"
			allCandidates = append(allCandidates, cand)
			continue
		}

		// Check MaxVMsPerHost constraint if set
		if constraints.MaxVMsPerHost != nil && vmsPerTarget[name] >= *constraints.MaxVMsPerHost {
			cand.rejected = true
			cand.rejectReason = fmt.Sprintf("Already has %d VMs (max: %d)", vmsPerTarget[name], *constraints.MaxVMsPerHost)
			allCandidates = append(allCandidates, cand)
			continue
		}

		// Calculate state after adding this VM
		newState := state.CalculateAfterMigration([]proxmox.VM{vm}, nil)
		cand.stateAfter = newState

		// Check if this target stays below cluster average
		margin := 5.0 // 5% margin
		belowAverage := newState.CPUPercent <= averages.CPUPercent+margin &&
			newState.RAMPercent <= averages.RAMPercent+margin
		cand.belowAverage = belowAverage

		// Calculate detailed score breakdown
		cpuHeadroom := averages.CPUPercent - newState.CPUPercent
		ramHeadroom := averages.RAMPercent - newState.RAMPercent
		headroomScore := cpuHeadroom*0.4 + ramHeadroom*0.4
		balanceScore := calculateBalanceScoreDetailed(newState)

		cand.breakdown = ScoreBreakdown{
			HeadroomScore:     headroomScore,
			BalanceScore:      balanceScore,
			UtilizationWeight: 0.0, // Not used in MigrateAll mode
			BalanceWeight:     0.2,
			TotalScore:        headroomScore + balanceScore*0.2,
		}
		cand.score = cand.breakdown.TotalScore

		if belowAverage {
			cand.reason = fmt.Sprintf("Balanced (CPU: %.1f%%, RAM: %.1f%%)", newState.CPUPercent, newState.RAMPercent)
		} else {
			cand.reason = fmt.Sprintf("Best available (CPU: %.1f%%, RAM: %.1f%%)", newState.CPUPercent, newState.RAMPercent)
		}

		allCandidates = append(allCandidates, cand)
	}

	// Separate valid and rejected candidates
	var validCandidates []candidate
	var rejectedCandidates []candidate
	for _, c := range allCandidates {
		if c.rejected {
			rejectedCandidates = append(rejectedCandidates, c)
		} else {
			validCandidates = append(validCandidates, c)
		}
	}

	if len(validCandidates) == 0 {
		details := &MigrationDetails{
			ClusterAvgCPU:      averages.CPUPercent,
			ClusterAvgRAM:      averages.RAMPercent,
			ConstraintsApplied: constraintsApplied,
		}
		for _, c := range rejectedCandidates {
			details.Alternatives = append(details.Alternatives, AlternativeTarget{
				Name:            c.name,
				Score:           0,
				RejectionReason: c.rejectReason,
			})
		}
		return "NONE", 0, "No target has capacity for this VM", details
	}

	// Sort candidates: prefer below-average first, then by score
	sort.Slice(validCandidates, func(i, j int) bool {
		if validCandidates[i].belowAverage != validCandidates[j].belowAverage {
			return validCandidates[i].belowAverage // below-average first
		}
		return validCandidates[i].score > validCandidates[j].score
	})

	best := validCandidates[0]

	// Build detailed migration reasoning
	details := &MigrationDetails{
		ScoreBreakdown: best.breakdown,
		TargetBefore: ResourceState{
			CPUPercent:     best.stateBefore.CPUPercent,
			RAMPercent:     best.stateBefore.RAMPercent,
			StoragePercent: best.stateBefore.StoragePercent,
			VMCount:        best.stateBefore.VMCount,
			VCPUs:          best.stateBefore.VCPUs,
			RAMUsed:        best.stateBefore.RAMUsed,
			RAMTotal:       best.stateBefore.RAMTotal,
			StorageUsed:    best.stateBefore.StorageUsed,
			StorageTotal:   best.stateBefore.StorageTotal,
		},
		TargetAfter: ResourceState{
			CPUPercent:     best.stateAfter.CPUPercent,
			RAMPercent:     best.stateAfter.RAMPercent,
			StoragePercent: best.stateAfter.StoragePercent,
			VMCount:        best.stateAfter.VMCount,
			VCPUs:          best.stateAfter.VCPUs,
			RAMUsed:        best.stateAfter.RAMUsed,
			RAMTotal:       best.stateAfter.RAMTotal,
			StorageUsed:    best.stateAfter.StorageUsed,
			StorageTotal:   best.stateAfter.StorageTotal,
		},
		ClusterAvgCPU:      averages.CPUPercent,
		ClusterAvgRAM:      averages.RAMPercent,
		BelowAverage:       best.belowAverage,
		ConstraintsApplied: constraintsApplied,
	}

	// Add alternative targets
	for i, c := range validCandidates {
		if i == 0 {
			continue
		}
		if i > 3 {
			break
		}
		var rejectReason string
		if c.belowAverage != best.belowAverage {
			rejectReason = "Would exceed cluster average"
		} else {
			rejectReason = fmt.Sprintf("Lower score (%.1f vs %.1f)", c.score, best.score)
		}
		details.Alternatives = append(details.Alternatives, AlternativeTarget{
			Name:            c.name,
			Score:           c.score,
			RejectionReason: rejectReason,
			CPUAfter:        c.stateAfter.CPUPercent,
			RAMAfter:        c.stateAfter.RAMPercent,
			StorageAfter:    c.stateAfter.StoragePercent,
		})
	}

	// Add rejected targets
	for _, c := range rejectedCandidates {
		if len(details.Alternatives) >= 5 {
			break
		}
		details.Alternatives = append(details.Alternatives, AlternativeTarget{
			Name:            c.name,
			Score:           0,
			RejectionReason: c.rejectReason,
		})
	}

	return best.name, best.score, best.reason, details
}

// calculateBalancedScore computes a score that prefers targets further below average
func calculateBalancedScore(state NodeState, averages ClusterAverages) float64 {
	// Calculate how far below average this target is
	cpuHeadroom := averages.CPUPercent - state.CPUPercent
	ramHeadroom := averages.RAMPercent - state.RAMPercent

	// Prefer more headroom (positive values mean below average)
	headroomScore := cpuHeadroom*0.4 + ramHeadroom*0.4

	// Also consider balance between resources
	resources := []float64{state.CPUPercent, state.RAMPercent, state.StoragePercent}
	mean := (resources[0] + resources[1] + resources[2]) / 3
	variance := 0.0
	for _, r := range resources {
		variance += math.Pow(r-mean, 2)
	}
	balanceScore := 100 - math.Sqrt(variance/3)

	// Combine scores
	return headroomScore + balanceScore*0.2
}
