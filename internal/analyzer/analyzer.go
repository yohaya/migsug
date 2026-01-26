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
		suggestions = GenerateSuggestionsBalanced(vmsToMigrate, targets, cluster, constraints)
	} else {
		suggestions = GenerateSuggestions(vmsToMigrate, targets, constraints)
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
		return selectByCPUUsage(node, *constraints.CPUUsage)
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
	// Only select running VMs (don't migrate powered off VMs)
	vms := filterRunningVMs(node.VMs)

	// Sort VMs by CPU usage
	sort.Slice(vms, func(i, j int) bool {
		return vms[i].CPUUsage < vms[j].CPUUsage
	})

	// Select VMs until we reach target CPU usage
	var selected []proxmox.VM
	totalUsage := 0.0

	for _, vm := range vms {
		if totalUsage >= targetUsage {
			break
		}
		selected = append(selected, vm)
		totalUsage += vm.CPUUsage
	}

	return selected
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
func GenerateSuggestions(vms []proxmox.VM, targets []proxmox.Node, constraints MigrationConstraints) []MigrationSuggestion {
	var suggestions []MigrationSuggestion

	// Track target states for capacity checking
	targetStates := make(map[string]NodeState)
	for _, target := range targets {
		targetStates[target.Name] = NewNodeState(&target)
	}

	// Track VMs per target for MaxVMsPerHost constraint
	vmsPerTarget := make(map[string]int)
	for _, target := range targets {
		vmsPerTarget[target.Name] = len(target.VMs)
	}

	// For each VM, find the best target
	for _, vm := range vms {
		targetNode, score, reason := FindBestTarget(vm, targetStates, vmsPerTarget, constraints)
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
			VMID:       vm.VMID,
			VMName:     vm.Name,
			SourceNode: vm.Node,
			TargetNode: targetNode,
			Reason:     reason,
			Score:      score,
			VCPUs:      vm.CPUCores,
			CPUUsage:   vm.CPUUsage,
			RAM:        vm.MaxMem, // Use allocated RAM
			Storage:    storageValue,
		}

		suggestions = append(suggestions, suggestion)
	}

	return suggestions
}

// FindBestTarget finds the best target node for a VM
func FindBestTarget(vm proxmox.VM, targetStates map[string]NodeState, vmsPerTarget map[string]int, constraints MigrationConstraints) (string, float64, string) {
	type candidate struct {
		name   string
		score  float64
		reason string
	}

	var candidates []candidate

	for name, state := range targetStates {
		// Check capacity
		if !state.HasCapacity(vm, constraints) {
			continue
		}

		// Check MaxVMsPerHost constraint
		if constraints.MaxVMsPerHost != nil {
			if vmsPerTarget[name] >= *constraints.MaxVMsPerHost {
				continue
			}
		}

		// Calculate score after adding this VM
		newState := state.CalculateAfterMigration([]proxmox.VM{vm}, nil)
		score := calculateTargetScore(state, newState)

		reason := fmt.Sprintf("Good balance (CPU: %.1f%%, RAM: %.1f%%, Storage: %.1f%%)",
			newState.CPUPercent, newState.RAMPercent, newState.StoragePercent)

		candidates = append(candidates, candidate{
			name:   name,
			score:  score,
			reason: reason,
		})
	}

	if len(candidates) == 0 {
		return "", 0, "No suitable target found"
	}

	// Sort by score (higher is better)
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	best := candidates[0]
	return best.name, best.score, best.reason
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

	// Source before state
	result.SourceBefore = NewNodeState(sourceNode)

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
		result.TargetsBefore[target.Name] = NewNodeState(&target)
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
func GenerateSuggestionsBalanced(vms []proxmox.VM, targets []proxmox.Node, cluster *proxmox.Cluster, constraints MigrationConstraints) []MigrationSuggestion {
	// Calculate target averages for the cluster after migration
	targetAverages := CalculateTargetAverages(cluster, constraints.SourceNode, vms)

	// Initialize target states (sequential processing for accurate state tracking)
	targetStates := make(map[string]NodeState)
	vmsPerTarget := make(map[string]int)

	for _, target := range targets {
		targetStates[target.Name] = NewNodeState(&target)
		vmsPerTarget[target.Name] = len(target.VMs)
	}

	suggestions := make([]MigrationSuggestion, 0, len(vms))

	// Process VMs sequentially to ensure accurate state tracking and ALL VMs get assigned
	// VMs are already sorted by size (largest first) from selectAllVMs
	for _, vm := range vms {
		// Find best target for this VM
		targetName, score, reason := findBestTargetForMigrateAll(vm, targetStates, vmsPerTarget, targetAverages, constraints)

		// Update target state after placement
		if targetName != "" {
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
			VMID:       vm.VMID,
			VMName:     vm.Name,
			SourceNode: vm.Node,
			TargetNode: targetName,
			Reason:     reason,
			Score:      score,
			VCPUs:      vm.CPUCores,
			CPUUsage:   vm.CPUUsage,
			RAM:        vm.MaxMem,
			Storage:    storageValue,
		}
		suggestions = append(suggestions, suggestion)
	}

	return suggestions
}

// findBestTargetForMigrateAll finds the best target for a VM in "Migrate All" mode.
// Unlike regular mode, this ALWAYS returns a valid target (never "NONE").
func findBestTargetForMigrateAll(vm proxmox.VM, targetStates map[string]NodeState, vmsPerTarget map[string]int, averages ClusterAverages, constraints MigrationConstraints) (string, float64, string) {
	type candidate struct {
		name         string
		score        float64
		reason       string
		belowAverage bool
	}

	var candidates []candidate

	// Evaluate all targets
	for name, state := range targetStates {
		// Check basic capacity (RAM and storage must fit)
		newRAMUsed := state.RAMUsed + vm.UsedMem
		if newRAMUsed > state.RAMTotal {
			continue // Can't fit - skip this target
		}

		newStorageUsed := state.StorageUsed + vm.UsedDisk
		if newStorageUsed > state.StorageTotal {
			continue // Can't fit - skip this target
		}

		// Check MaxVMsPerHost constraint if set
		if constraints.MaxVMsPerHost != nil && vmsPerTarget[name] >= *constraints.MaxVMsPerHost {
			continue
		}

		// Calculate state after adding this VM
		newState := state.CalculateAfterMigration([]proxmox.VM{vm}, nil)

		// Check if this target stays below cluster average
		margin := 5.0 // 5% margin
		belowAverage := newState.CPUPercent <= averages.CPUPercent+margin &&
			newState.RAMPercent <= averages.RAMPercent+margin

		// Calculate score - prefer targets further below average
		score := calculateBalancedScore(newState, averages)

		var reason string
		if belowAverage {
			reason = fmt.Sprintf("Balanced (CPU: %.1f%%, RAM: %.1f%%)", newState.CPUPercent, newState.RAMPercent)
		} else {
			reason = fmt.Sprintf("Best available (CPU: %.1f%%, RAM: %.1f%%)", newState.CPUPercent, newState.RAMPercent)
		}

		candidates = append(candidates, candidate{
			name:         name,
			score:        score,
			reason:       reason,
			belowAverage: belowAverage,
		})
	}

	if len(candidates) == 0 {
		// No target can fit this VM at all - this shouldn't happen normally
		// but return a message indicating the issue
		return "NONE", 0, "No target has capacity for this VM"
	}

	// Sort candidates: prefer below-average first, then by score
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].belowAverage != candidates[j].belowAverage {
			return candidates[i].belowAverage // below-average first
		}
		return candidates[i].score > candidates[j].score
	})

	best := candidates[0]
	return best.name, best.score, best.reason
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
