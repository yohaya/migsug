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

	// Generate migration suggestions
	suggestions := GenerateSuggestions(vmsToMigrate, targets, constraints)

	// Calculate before/after states
	result := BuildAnalysisResult(sourceNode, targets, suggestions, vmsToMigrate)

	return result, nil
}

// SelectVMsToMigrate selects which VMs to migrate based on constraints
func SelectVMsToMigrate(node *proxmox.Node, constraints MigrationConstraints) []proxmox.VM {
	mode := constraints.GetMode()

	switch mode {
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

		suggestion := MigrationSuggestion{
			VMID:       vm.VMID,
			VMName:     vm.Name,
			SourceNode: vm.Node,
			TargetNode: targetNode,
			Reason:     reason,
			Score:      score,
			VCPUs:      vm.CPUCores,
			CPUUsage:   vm.CPUUsage,
			RAM:        vm.UsedMem,
			Storage:    vm.UsedDisk,
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

	// Calculate summary
	result.TotalVMs = len(suggestions)
	for _, vm := range vmsToMigrate {
		result.TotalVCPUs += vm.CPUCores
		result.TotalRAM += vm.UsedMem
		result.TotalStorage += vm.UsedDisk
	}

	// Generate improvement info
	cpuImprovement := result.SourceBefore.CPUPercent - result.SourceAfter.CPUPercent
	ramImprovement := result.SourceBefore.RAMPercent - result.SourceAfter.RAMPercent
	result.ImprovementInfo = fmt.Sprintf("CPU: -%.1f%%, RAM: -%.1f%%", cpuImprovement, ramImprovement)

	return result
}
