package analyzer

import (
	"fmt"
	"log"
	"math"
	"sort"

	"github.com/yourusername/migsug/internal/proxmox"
)

// BalanceProgressCallback reports progress during cluster balancing analysis
// stage: current stage ("calculating", "optimizing", "generating")
// current: current progress
// total: total items to process
type BalanceProgressCallback func(stage string, current, total int)

// AnalyzeClusterWideBalance performs cluster-wide balancing analysis
// This analyzes ALL nodes in the cluster and generates migrations to balance
// VM count, vCPUs, RAM, and storage utilization across all hosts
func AnalyzeClusterWideBalance(cluster *proxmox.Cluster, progress BalanceProgressCallback) (*AnalysisResult, error) {
	if cluster == nil || len(cluster.Nodes) == 0 {
		return nil, fmt.Errorf("no nodes in cluster")
	}

	// Get only online nodes
	var onlineNodes []proxmox.Node
	for _, node := range cluster.Nodes {
		if node.Status == "online" {
			onlineNodes = append(onlineNodes, node)
		}
	}

	if len(onlineNodes) < 2 {
		return nil, fmt.Errorf("need at least 2 online nodes for cluster balancing")
	}

	// Report initial stage
	if progress != nil {
		progress("Calculating cluster metrics", 0, len(onlineNodes))
	}

	// Calculate cluster-wide metrics and targets
	metrics := calculateClusterMetrics(onlineNodes)

	log.Printf("ClusterBalance: Cluster averages - vCPUs: %.1f%%, RAM: %.1f%%, Storage: %.1f%%, VMs: %.1f",
		metrics.avgVCPUPercent, metrics.avgRAMPercent, metrics.avgStoragePercent, metrics.avgVMCount)

	// Report progress
	if progress != nil {
		progress("Analyzing node imbalances", len(onlineNodes)/2, len(onlineNodes))
	}

	// Identify overloaded (donors) and underloaded (receivers) nodes
	donors, receivers := categorizeNodes(onlineNodes, metrics)

	log.Printf("ClusterBalance: %d donor nodes, %d receiver nodes", len(donors), len(receivers))

	if len(donors) == 0 || len(receivers) == 0 {
		return nil, fmt.Errorf("cluster is already balanced - no migrations needed")
	}

	// Report progress
	if progress != nil {
		progress("Finding optimal migrations", len(onlineNodes), len(onlineNodes))
	}

	// Generate optimal migrations using greedy algorithm with optimization
	suggestions, nodeStates := generateBalancedMigrations(donors, receivers, metrics, cluster, progress)

	if len(suggestions) == 0 {
		return nil, fmt.Errorf("no beneficial migrations found")
	}

	// Build result
	result := &AnalysisResult{
		Suggestions:   suggestions,
		TargetsBefore: make(map[string]NodeState),
		TargetsAfter:  make(map[string]NodeState),
	}

	// Set source as the first donor (for UI compatibility)
	if len(donors) > 0 {
		result.SourceBefore = nodeStates[donors[0].node.Name].before
		result.SourceAfter = nodeStates[donors[0].node.Name].after
	}

	// Populate target states
	for name, states := range nodeStates {
		result.TargetsBefore[name] = states.before
		result.TargetsAfter[name] = states.after
	}

	// Calculate totals
	for _, s := range suggestions {
		result.TotalVMs++
		result.TotalVCPUs += s.VCPUs
		result.TotalRAM += s.RAM
		result.TotalStorage += s.Storage
	}

	// Calculate improvement info
	result.ImprovementInfo = calculateImprovementInfo(nodeStates, metrics)

	return result, nil
}

// clusterMetrics holds calculated cluster-wide metrics
type clusterMetrics struct {
	totalVCPUs      int
	totalRAM        int64
	totalStorage    int64
	totalVMs        int
	totalCPUCores   int
	totalRAMMax     int64
	totalStorageMax int64

	avgVCPUPercent    float64 // Average vCPU allocation as % of cores
	avgRAMPercent     float64 // Average RAM usage %
	avgStoragePercent float64 // Average storage usage %
	avgVMCount        float64 // Average VM count per node

	nodeCount int
}

// calculateClusterMetrics computes cluster-wide metrics
func calculateClusterMetrics(nodes []proxmox.Node) clusterMetrics {
	m := clusterMetrics{nodeCount: len(nodes)}

	for _, node := range nodes {
		m.totalCPUCores += node.CPUCores
		m.totalRAMMax += node.MaxMem
		m.totalStorageMax += node.MaxDisk
		m.totalVMs += len(node.VMs)

		for _, vm := range node.VMs {
			m.totalVCPUs += vm.CPUCores
			m.totalRAM += vm.MaxMem
			storage := vm.MaxDisk
			if storage == 0 {
				storage = vm.UsedDisk
			}
			m.totalStorage += storage
		}
	}

	if m.nodeCount > 0 {
		m.avgVMCount = float64(m.totalVMs) / float64(m.nodeCount)
	}
	if m.totalCPUCores > 0 {
		m.avgVCPUPercent = float64(m.totalVCPUs) / float64(m.totalCPUCores) * 100
	}
	if m.totalRAMMax > 0 {
		m.avgRAMPercent = float64(m.totalRAM) / float64(m.totalRAMMax) * 100
	}
	if m.totalStorageMax > 0 {
		m.avgStoragePercent = float64(m.totalStorage) / float64(m.totalStorageMax) * 100
	}

	return m
}

// nodeBalance represents a node's balance status
type nodeBalance struct {
	node           proxmox.Node
	vcpuDeviation  float64 // Positive = above avg, negative = below
	ramDeviation   float64
	vmDeviation    float64
	compositeScore float64 // Combined deviation score (higher = more imbalanced)
}

// categorizeNodes identifies donor (over-utilized) and receiver (under-utilized) nodes
func categorizeNodes(nodes []proxmox.Node, metrics clusterMetrics) ([]nodeBalance, []nodeBalance) {
	var donors, receivers []nodeBalance

	for _, node := range nodes {
		balance := nodeBalance{node: node}

		// Calculate vCPU allocation as % of node's cores
		nodeVCPUs := 0
		for _, vm := range node.VMs {
			nodeVCPUs += vm.CPUCores
		}
		nodeVCPUPercent := 0.0
		if node.CPUCores > 0 {
			nodeVCPUPercent = float64(nodeVCPUs) / float64(node.CPUCores) * 100
		}
		balance.vcpuDeviation = nodeVCPUPercent - metrics.avgVCPUPercent

		// RAM deviation
		balance.ramDeviation = node.GetMemPercent() - metrics.avgRAMPercent

		// VM count deviation
		balance.vmDeviation = float64(len(node.VMs)) - metrics.avgVMCount

		// Composite score: weighted combination of deviations
		// Higher weights for RAM since it's often the bottleneck
		balance.compositeScore = (balance.vcpuDeviation * 0.3) +
			(balance.ramDeviation * 0.5) +
			(balance.vmDeviation * 0.2)

		// Categorize: positive composite = donor, negative = receiver
		// Use a small threshold to avoid tiny migrations
		threshold := 2.0
		if balance.compositeScore > threshold {
			donors = append(donors, balance)
		} else if balance.compositeScore < -threshold {
			receivers = append(receivers, balance)
		}
	}

	// Sort donors by composite score (most overloaded first)
	sort.Slice(donors, func(i, j int) bool {
		return donors[i].compositeScore > donors[j].compositeScore
	})

	// Sort receivers by composite score (most underloaded first)
	sort.Slice(receivers, func(i, j int) bool {
		return receivers[i].compositeScore < receivers[j].compositeScore
	})

	return donors, receivers
}

// nodeStatesPair holds before/after states for a node
type nodeStatesPair struct {
	before NodeState
	after  NodeState
}

// generateBalancedMigrations generates optimal migrations to balance the cluster
func generateBalancedMigrations(donors, receivers []nodeBalance, metrics clusterMetrics, cluster *proxmox.Cluster, progress BalanceProgressCallback) ([]MigrationSuggestion, map[string]nodeStatesPair) {
	var suggestions []MigrationSuggestion
	nodeStates := make(map[string]nodeStatesPair)

	// Initialize node states for all nodes
	for _, d := range donors {
		nodeStates[d.node.Name] = nodeStatesPair{
			before: buildNodeState(&d.node),
			after:  buildNodeState(&d.node),
		}
	}
	for _, r := range receivers {
		nodeStates[r.node.Name] = nodeStatesPair{
			before: buildNodeState(&r.node),
			after:  buildNodeState(&r.node),
		}
	}

	// Track which VMs have been migrated
	migratedVMs := make(map[int]bool)

	// Track current node states (for incremental updates)
	currentStates := make(map[string]*simulatedNodeState)
	for _, d := range donors {
		currentStates[d.node.Name] = newSimulatedNodeState(&d.node)
	}
	for _, r := range receivers {
		currentStates[r.node.Name] = newSimulatedNodeState(&r.node)
	}

	// Greedy algorithm: repeatedly find the best migration until balanced
	maxIterations := 500 // Safety limit
	totalProgress := maxIterations
	currentProgress := 0

	for i := 0; i < maxIterations; i++ {
		if progress != nil && i%10 == 0 {
			currentProgress = i
			progress("Optimizing migrations", currentProgress, totalProgress)
		}

		bestMigration := findBestMigration(donors, receivers, currentStates, migratedVMs, metrics, cluster)
		if bestMigration == nil {
			break // No more beneficial migrations
		}

		// Apply the migration
		suggestions = append(suggestions, *bestMigration)
		migratedVMs[bestMigration.VMID] = true

		// Update simulated states
		updateSimulatedStates(currentStates, bestMigration)

		// Check if we've achieved balance
		if isClusterBalanced(currentStates, metrics) {
			break
		}
	}

	// Report final progress
	if progress != nil {
		progress("Finalizing results", totalProgress, totalProgress)
	}

	// Update final after states
	for name, state := range currentStates {
		if pair, ok := nodeStates[name]; ok {
			pair.after = state.toNodeState()
			nodeStates[name] = pair
		}
	}

	return suggestions, nodeStates
}

// simulatedNodeState tracks node state during simulation
type simulatedNodeState struct {
	name         string
	vcpus        int
	cpuCores     int
	ramUsed      int64
	ramTotal     int64
	storageUsed  int64
	storageTotal int64
	vmCount      int
	vms          map[int]proxmox.VM // Current VMs on this node
}

func newSimulatedNodeState(node *proxmox.Node) *simulatedNodeState {
	s := &simulatedNodeState{
		name:         node.Name,
		cpuCores:     node.CPUCores,
		ramTotal:     node.MaxMem,
		storageTotal: node.MaxDisk,
		vms:          make(map[int]proxmox.VM),
	}

	for _, vm := range node.VMs {
		s.vms[vm.VMID] = vm
		s.vcpus += vm.CPUCores
		s.ramUsed += vm.MaxMem
		storage := vm.MaxDisk
		if storage == 0 {
			storage = vm.UsedDisk
		}
		s.storageUsed += storage
		s.vmCount++
	}

	return s
}

func (s *simulatedNodeState) getVCPUPercent() float64 {
	if s.cpuCores == 0 {
		return 0
	}
	return float64(s.vcpus) / float64(s.cpuCores) * 100
}

func (s *simulatedNodeState) getRAMPercent() float64 {
	if s.ramTotal == 0 {
		return 0
	}
	return float64(s.ramUsed) / float64(s.ramTotal) * 100
}

func (s *simulatedNodeState) toNodeState() NodeState {
	return NodeState{
		Name:           s.name,
		VMCount:        s.vmCount,
		VCPUs:          s.vcpus,
		CPUCores:       s.cpuCores,
		CPUPercent:     s.getVCPUPercent(),
		RAMUsed:        s.ramUsed,
		RAMTotal:       s.ramTotal,
		RAMPercent:     s.getRAMPercent(),
		StorageUsed:    s.storageUsed,
		StorageTotal:   s.storageTotal,
		StoragePercent: float64(s.storageUsed) / float64(s.storageTotal) * 100,
	}
}

// findBestMigration finds the single best VM migration to improve balance
func findBestMigration(donors, receivers []nodeBalance, states map[string]*simulatedNodeState, migratedVMs map[int]bool, metrics clusterMetrics, cluster *proxmox.Cluster) *MigrationSuggestion {
	var bestMigration *MigrationSuggestion
	bestScore := 0.0

	for _, donor := range donors {
		donorState := states[donor.node.Name]
		if donorState == nil {
			continue
		}

		// Check if donor is still above average
		if donorState.getRAMPercent() <= metrics.avgRAMPercent+1 &&
			donorState.getVCPUPercent() <= metrics.avgVCPUPercent+1 {
			continue // Donor is now balanced
		}

		// Consider each VM on this donor
		for vmid, vm := range donorState.vms {
			// Skip if already migrated, not migratable, or not running
			if migratedVMs[vmid] || vm.NoMigrate || vm.Status != "running" {
				continue
			}

			// Find the best receiver for this VM
			for _, receiver := range receivers {
				receiverState := states[receiver.node.Name]
				if receiverState == nil {
					continue
				}

				// Check if receiver can accept this VM
				if !canAcceptVM(receiverState, &vm, metrics) {
					continue
				}

				// Calculate improvement score
				score := calculateMigrationScore(donorState, receiverState, &vm, metrics)
				if score > bestScore {
					bestScore = score
					storage := vm.MaxDisk
					if storage == 0 {
						storage = vm.UsedDisk
					}
					bestMigration = &MigrationSuggestion{
						VMID:        vm.VMID,
						VMName:      vm.Name,
						SourceNode:  donor.node.Name,
						TargetNode:  receiver.node.Name,
						Reason:      "Balance cluster",
						Score:       score,
						Status:      vm.Status,
						VCPUs:       vm.CPUCores,
						CPUUsage:    vm.CPUUsage,
						RAM:         vm.MaxMem,
						Storage:     storage,
						SourceCores: donorState.cpuCores,
						TargetCores: receiverState.cpuCores,
						Details: &MigrationDetails{
							SelectionMode:   "balance_cluster",
							SelectionReason: "Cluster-wide balancing",
							ClusterAvgCPU:   metrics.avgVCPUPercent,
							ClusterAvgRAM:   metrics.avgRAMPercent,
						},
					}
				}
			}
		}
	}

	return bestMigration
}

// canAcceptVM checks if a receiver can accept a VM without becoming overloaded
func canAcceptVM(receiver *simulatedNodeState, vm *proxmox.VM, metrics clusterMetrics) bool {
	// Calculate projected utilization after adding VM
	newRAMPercent := float64(receiver.ramUsed+vm.MaxMem) / float64(receiver.ramTotal) * 100
	newVCPUPercent := float64(receiver.vcpus+vm.CPUCores) / float64(receiver.cpuCores) * 100

	// Don't allow if it would push receiver above cluster average + margin
	margin := 5.0
	if newRAMPercent > metrics.avgRAMPercent+margin {
		return false
	}
	if newVCPUPercent > metrics.avgVCPUPercent+margin {
		return false
	}

	// Ensure minimum headroom (don't fill to capacity)
	if newRAMPercent > 90 {
		return false
	}

	return true
}

// calculateMigrationScore calculates how much a migration improves cluster balance
func calculateMigrationScore(donor, receiver *simulatedNodeState, vm *proxmox.VM, metrics clusterMetrics) float64 {
	// Calculate current deviations
	donorRAMDev := math.Abs(donor.getRAMPercent() - metrics.avgRAMPercent)
	receiverRAMDev := math.Abs(receiver.getRAMPercent() - metrics.avgRAMPercent)
	currentTotalDev := donorRAMDev + receiverRAMDev

	// Calculate projected deviations after migration
	newDonorRAM := float64(donor.ramUsed-vm.MaxMem) / float64(donor.ramTotal) * 100
	newReceiverRAM := float64(receiver.ramUsed+vm.MaxMem) / float64(receiver.ramTotal) * 100
	newDonorRAMDev := math.Abs(newDonorRAM - metrics.avgRAMPercent)
	newReceiverRAMDev := math.Abs(newReceiverRAM - metrics.avgRAMPercent)
	newTotalDev := newDonorRAMDev + newReceiverRAMDev

	// Improvement in deviation (higher = better)
	improvement := currentTotalDev - newTotalDev

	// Bonus for smaller VMs (prefer moving smaller VMs)
	storage := vm.MaxDisk
	if storage == 0 {
		storage = vm.UsedDisk
	}
	storageGiB := float64(storage) / (1024 * 1024 * 1024)
	if storageGiB < 1 {
		storageGiB = 1
	}
	sizeBonus := 100.0 / storageGiB // Smaller = higher bonus

	return improvement*10 + sizeBonus
}

// updateSimulatedStates updates states after a migration
func updateSimulatedStates(states map[string]*simulatedNodeState, migration *MigrationSuggestion) {
	source := states[migration.SourceNode]
	target := states[migration.TargetNode]

	if source != nil && target != nil {
		vm, exists := source.vms[migration.VMID]
		if exists {
			// Remove from source
			delete(source.vms, migration.VMID)
			source.vcpus -= vm.CPUCores
			source.ramUsed -= vm.MaxMem
			storage := vm.MaxDisk
			if storage == 0 {
				storage = vm.UsedDisk
			}
			source.storageUsed -= storage
			source.vmCount--

			// Add to target
			target.vms[migration.VMID] = vm
			target.vcpus += vm.CPUCores
			target.ramUsed += vm.MaxMem
			target.storageUsed += storage
			target.vmCount++
		}
	}
}

// isClusterBalanced checks if the cluster has achieved balance
func isClusterBalanced(states map[string]*simulatedNodeState, metrics clusterMetrics) bool {
	threshold := 3.0 // Acceptable deviation from average

	for _, state := range states {
		ramDev := math.Abs(state.getRAMPercent() - metrics.avgRAMPercent)
		vcpuDev := math.Abs(state.getVCPUPercent() - metrics.avgVCPUPercent)

		if ramDev > threshold || vcpuDev > threshold {
			return false
		}
	}

	return true
}

// buildNodeState creates a NodeState from a Node
func buildNodeState(node *proxmox.Node) NodeState {
	vcpus := 0
	ramUsed := int64(0)
	storageUsed := int64(0)

	for _, vm := range node.VMs {
		vcpus += vm.CPUCores
		ramUsed += vm.MaxMem
		storage := vm.MaxDisk
		if storage == 0 {
			storage = vm.UsedDisk
		}
		storageUsed += storage
	}

	return NodeState{
		Name:           node.Name,
		VMCount:        len(node.VMs),
		VCPUs:          vcpus,
		CPUCores:       node.CPUCores,
		CPUPercent:     float64(vcpus) / float64(node.CPUCores) * 100,
		RAMUsed:        ramUsed,
		RAMTotal:       node.MaxMem,
		RAMPercent:     float64(ramUsed) / float64(node.MaxMem) * 100,
		StorageUsed:    storageUsed,
		StorageTotal:   node.MaxDisk,
		StoragePercent: float64(storageUsed) / float64(node.MaxDisk) * 100,
	}
}

// calculateImprovementInfo generates improvement summary
func calculateImprovementInfo(nodeStates map[string]nodeStatesPair, metrics clusterMetrics) string {
	// Calculate standard deviation before and after
	var beforeDevSum, afterDevSum float64
	count := 0

	for _, states := range nodeStates {
		beforeDevSum += math.Pow(states.before.RAMPercent-metrics.avgRAMPercent, 2)
		afterDevSum += math.Pow(states.after.RAMPercent-metrics.avgRAMPercent, 2)
		count++
	}

	if count == 0 {
		return ""
	}

	beforeStdDev := math.Sqrt(beforeDevSum / float64(count))
	afterStdDev := math.Sqrt(afterDevSum / float64(count))
	improvement := ((beforeStdDev - afterStdDev) / beforeStdDev) * 100

	return fmt.Sprintf("Balance improved by %.1f%% (std dev: %.1f%% → %.1f%%)", improvement, beforeStdDev, afterStdDev)
}
