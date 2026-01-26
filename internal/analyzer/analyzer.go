package analyzer

import (
	"fmt"
	"math"
	"sort"
	"sync"

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

// selectAllVMs returns all running VMs from the node for "Migrate All" mode
func selectAllVMs(node *proxmox.Node) []proxmox.VM {
	// Return all running VMs sorted by resource usage (largest first for better distribution)
	vms := filterRunningVMs(node.VMs)

	// Sort by combined resource score (descending - distribute largest VMs first)
	sort.Slice(vms, func(i, j int) bool {
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

// vmPlacement represents a VM placement decision
type vmPlacement struct {
	vmIndex    int
	targetName string
	score      float64
	reason     string
}

// targetNodeState wraps NodeState with a mutex for thread-safe updates
type targetNodeState struct {
	state NodeState
	mu    sync.Mutex
}

// GenerateSuggestionsBalanced creates migration suggestions using a multi-threaded
// algorithm that ensures no target node exceeds the cluster average usage.
// This is used for "Migrate All" mode.
func GenerateSuggestionsBalanced(vms []proxmox.VM, targets []proxmox.Node, cluster *proxmox.Cluster, constraints MigrationConstraints) []MigrationSuggestion {
	// Calculate target averages for the cluster after migration
	targetAverages := CalculateTargetAverages(cluster, constraints.SourceNode, vms)

	// Initialize target states with mutex for thread-safe updates
	targetStates := make(map[string]*targetNodeState)
	vmsPerTarget := make(map[string]int)
	var statesMu sync.RWMutex

	for _, target := range targets {
		targetStates[target.Name] = &targetNodeState{
			state: NewNodeState(&target),
		}
		vmsPerTarget[target.Name] = len(target.VMs)
	}

	// Process VMs in parallel batches for optimal performance
	suggestions := make([]MigrationSuggestion, len(vms))
	var wg sync.WaitGroup

	// Use a worker pool with number of workers based on VM count
	numWorkers := 4
	if len(vms) < numWorkers {
		numWorkers = len(vms)
	}

	vmChan := make(chan int, len(vms))
	resultChan := make(chan vmPlacement, len(vms))

	// Start workers
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for vmIdx := range vmChan {
				vm := vms[vmIdx]
				placement := findBestTargetBalanced(vm, targetStates, vmsPerTarget, &statesMu, targetAverages, constraints)
				placement.vmIndex = vmIdx
				resultChan <- placement
			}
		}()
	}

	// Send VMs to workers
	go func() {
		for i := range vms {
			vmChan <- i
		}
		close(vmChan)
	}()

	// Collect results in a separate goroutine
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Process placements and update states atomically
	for placement := range resultChan {
		vm := vms[placement.vmIndex]

		if placement.targetName != "" && placement.targetName != "NONE" {
			// Update target state atomically
			ts := targetStates[placement.targetName]
			ts.mu.Lock()
			ts.state = ts.state.CalculateAfterMigration([]proxmox.VM{vm}, nil)
			ts.mu.Unlock()

			statesMu.Lock()
			vmsPerTarget[placement.targetName]++
			statesMu.Unlock()
		}

		// Build suggestion
		storageValue := vm.MaxDisk
		if storageValue == 0 {
			storageValue = vm.UsedDisk
		}

		targetNode := placement.targetName
		reason := placement.reason
		if targetNode == "" {
			targetNode = "NONE"
			reason = "No suitable target (all targets would exceed cluster average)"
		}

		suggestions[placement.vmIndex] = MigrationSuggestion{
			VMID:       vm.VMID,
			VMName:     vm.Name,
			SourceNode: vm.Node,
			TargetNode: targetNode,
			Reason:     reason,
			Score:      placement.score,
			VCPUs:      vm.CPUCores,
			CPUUsage:   vm.CPUUsage,
			RAM:        vm.MaxMem,
			Storage:    storageValue,
		}
	}

	return suggestions
}

// findBestTargetBalanced finds the best target for a VM ensuring the target
// doesn't exceed cluster average after adding the VM
func findBestTargetBalanced(vm proxmox.VM, targetStates map[string]*targetNodeState, vmsPerTarget map[string]int, statesMu *sync.RWMutex, averages ClusterAverages, constraints MigrationConstraints) vmPlacement {
	type candidate struct {
		name   string
		score  float64
		reason string
		state  NodeState
	}

	var candidates []candidate
	var candidatesMu sync.Mutex
	var wg sync.WaitGroup

	// Evaluate all targets in parallel
	for name, ts := range targetStates {
		wg.Add(1)
		go func(targetName string, targetState *targetNodeState) {
			defer wg.Done()

			targetState.mu.Lock()
			currentState := targetState.state
			targetState.mu.Unlock()

			// Check capacity
			if !currentState.HasCapacity(vm, constraints) {
				return
			}

			// Check MaxVMsPerHost constraint
			statesMu.RLock()
			currentVMs := vmsPerTarget[targetName]
			statesMu.RUnlock()

			if constraints.MaxVMsPerHost != nil {
				if currentVMs >= *constraints.MaxVMsPerHost {
					return
				}
			}

			// Calculate state after adding this VM
			newState := currentState.CalculateAfterMigration([]proxmox.VM{vm}, nil)

			// Check if adding this VM would exceed cluster averages
			// Allow a small margin (5%) above average for flexibility
			margin := 5.0
			if newState.CPUPercent > averages.CPUPercent+margin ||
				newState.RAMPercent > averages.RAMPercent+margin {
				// Skip this target - would exceed average
				return
			}

			// Calculate score - prefer targets that stay well below average
			score := calculateBalancedScore(newState, averages)

			reason := fmt.Sprintf("Balanced (CPU: %.1f%%, RAM: %.1f%% - below avg)",
				newState.CPUPercent, newState.RAMPercent)

			candidatesMu.Lock()
			candidates = append(candidates, candidate{
				name:   targetName,
				score:  score,
				reason: reason,
				state:  newState,
			})
			candidatesMu.Unlock()
		}(name, ts)
	}

	wg.Wait()

	if len(candidates) == 0 {
		// No target found below average, try to find any target with capacity
		// This is a fallback for edge cases
		return findFallbackTarget(vm, targetStates, vmsPerTarget, statesMu, constraints)
	}

	// Sort by score (higher is better)
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	best := candidates[0]
	return vmPlacement{
		targetName: best.name,
		score:      best.score,
		reason:     best.reason,
	}
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

// findFallbackTarget finds any target with capacity when no target is below average
func findFallbackTarget(vm proxmox.VM, targetStates map[string]*targetNodeState, vmsPerTarget map[string]int, statesMu *sync.RWMutex, constraints MigrationConstraints) vmPlacement {
	type fallback struct {
		name  string
		score float64
		state NodeState
	}

	var fallbacks []fallback

	for name, ts := range targetStates {
		ts.mu.Lock()
		currentState := ts.state
		ts.mu.Unlock()

		if !currentState.HasCapacity(vm, constraints) {
			continue
		}

		statesMu.RLock()
		currentVMs := vmsPerTarget[name]
		statesMu.RUnlock()

		if constraints.MaxVMsPerHost != nil && currentVMs >= *constraints.MaxVMsPerHost {
			continue
		}

		newState := currentState.CalculateAfterMigration([]proxmox.VM{vm}, nil)
		score := 100 - newState.GetUtilizationScore()

		fallbacks = append(fallbacks, fallback{
			name:  name,
			score: score,
			state: newState,
		})
	}

	if len(fallbacks) == 0 {
		return vmPlacement{targetName: "NONE", reason: "No suitable target with capacity"}
	}

	// Sort by score
	sort.Slice(fallbacks, func(i, j int) bool {
		return fallbacks[i].score > fallbacks[j].score
	})

	best := fallbacks[0]
	return vmPlacement{
		targetName: best.name,
		score:      best.score,
		reason:     fmt.Sprintf("Best available (CPU: %.1f%%, RAM: %.1f%%)", best.state.CPUPercent, best.state.RAMPercent),
	}
}
