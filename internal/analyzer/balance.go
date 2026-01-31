package analyzer

import (
	"fmt"
	"log"
	"math"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/yourusername/migsug/internal/proxmox"
)

// BalanceProgressCallback reports progress during cluster balancing analysis
// stage: current stage ("calculating", "optimizing", "generating")
// current: current progress
// total: total items to process
// movementsTried: number of migration candidates evaluated so far
type BalanceProgressCallback func(stage string, current, total, movementsTried int)

// AnalyzeClusterWideBalance performs cluster-wide balancing analysis
// This analyzes ALL nodes in the cluster and generates migrations to balance
// VM count, vCPUs, RAM, and storage utilization across all hosts
func AnalyzeClusterWideBalance(cluster *proxmox.Cluster, progress BalanceProgressCallback) (*AnalysisResult, error) {
	if cluster == nil || len(cluster.Nodes) == 0 {
		return nil, fmt.Errorf("no nodes in cluster")
	}

	// Get only online nodes that are not migration-blocked (hoststate=3)
	var onlineNodes []proxmox.Node
	for _, node := range cluster.Nodes {
		if node.Status == "online" && !node.IsMigrationBlocked() {
			onlineNodes = append(onlineNodes, node)
		}
	}

	if len(onlineNodes) < 2 {
		return nil, fmt.Errorf("need at least 2 online nodes for cluster balancing")
	}

	// Report initial stage
	if progress != nil {
		progress("Calculating cluster metrics", 0, len(onlineNodes), 0)
	}

	// Calculate cluster-wide metrics and targets
	metrics := calculateClusterMetrics(onlineNodes)

	log.Printf("ClusterBalance: Cluster averages - vCPUs: %.1f%%, RAM: %.1f%%, Storage: %.1f%%, VMs: %.1f",
		metrics.avgVCPUPercent, metrics.avgRAMPercent, metrics.avgStoragePercent, metrics.avgVMCount)

	// Report progress
	if progress != nil {
		progress("Analyzing node imbalances", len(onlineNodes)/2, len(onlineNodes), 0)
	}

	// Identify overloaded (donors) and underloaded (receivers) nodes
	donors, receivers := categorizeNodes(onlineNodes, metrics)

	log.Printf("ClusterBalance: %d donor nodes, %d receiver nodes", len(donors), len(receivers))

	if len(donors) == 0 || len(receivers) == 0 {
		return nil, fmt.Errorf("cluster is already balanced - no migrations needed")
	}

	// Report progress
	if progress != nil {
		progress("Finding optimal migrations", len(onlineNodes), len(onlineNodes), 0)
	}

	// Generate optimal migrations using greedy algorithm with optimization
	suggestions, nodeStates, movementsTried := generateBalancedMigrations(donors, receivers, metrics, cluster, progress)

	if len(suggestions) == 0 {
		return nil, fmt.Errorf("no beneficial migrations found")
	}

	// Report progress for vCPU optimization
	if progress != nil {
		progress("Optimizing vCPU distribution", len(onlineNodes), len(onlineNodes), movementsTried)
	}

	// Build simulated states from the results for vCPU swap optimization
	// This represents the cluster state AFTER the initial balancing pass
	swapStates := make(map[string]*simulatedNodeState)
	for _, node := range onlineNodes {
		state := newSimulatedNodeState(&node)
		swapStates[node.Name] = state
	}

	// Apply the initial migrations to the simulated states
	migratedVMs := make(map[int]bool)
	for _, sug := range suggestions {
		migratedVMs[sug.VMID] = true
		// Remove VM from source
		if srcState := swapStates[sug.SourceNode]; srcState != nil {
			if vm, ok := srcState.vms[sug.VMID]; ok {
				srcState.vcpus -= vm.CPUCores
				srcState.ramUsed -= vm.MaxMem
				srcState.storageUsed -= vm.GetEffectiveDisk()
				srcState.vmCount--
				delete(srcState.vms, sug.VMID)
			}
		}
		// Add VM to target
		if tgtState := swapStates[sug.TargetNode]; tgtState != nil {
			vm := proxmox.VM{
				VMID:     sug.VMID,
				Name:     sug.VMName,
				CPUCores: sug.VCPUs,
				MaxMem:   sug.RAM,
				UsedDisk: sug.UsedDisk,
				MaxDisk:  sug.MaxDisk,
				Status:   sug.Status,
			}
			tgtState.vms[sug.VMID] = vm
			tgtState.vcpus += vm.CPUCores
			tgtState.ramUsed += vm.MaxMem
			tgtState.storageUsed += vm.GetEffectiveDisk()
			tgtState.vmCount++
		}
	}

	// Find vCPU swap opportunities (VMs with similar RAM but different vCPUs)
	// Allow up to 20 swaps (40 migrations) for large clusters
	maxSwaps := len(onlineNodes) / 3
	if maxSwaps < 5 {
		maxSwaps = 5
	}
	if maxSwaps > 20 {
		maxSwaps = 20
	}

	swapSuggestions := findVCPUSwapOpportunities(swapStates, metrics, maxSwaps)
	if len(swapSuggestions) > 0 {
		log.Printf("ClusterBalance: Found %d vCPU swap migrations", len(swapSuggestions))
		suggestions = append(suggestions, swapSuggestions...)
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

	// Populate target states (use updated states after swaps)
	for name, states := range nodeStates {
		result.TargetsBefore[name] = states.before
		// Update after state if we have swap data
		if swapState := swapStates[name]; swapState != nil {
			result.TargetsAfter[name] = swapState.toNodeState()
		} else {
			result.TargetsAfter[name] = states.after
		}
	}

	// Calculate totals
	for _, s := range suggestions {
		result.TotalVMs++
		result.TotalVCPUs += s.VCPUs
		result.TotalRAM += s.RAM
		result.TotalStorage += s.Storage
	}

	// Set movements tried counter
	result.MovementsTried = movementsTried

	// Calculate improvement info
	result.ImprovementInfo = calculateImprovementInfo(nodeStates, metrics)

	log.Printf("ClusterBalance: Analyzed %d potential movements, generated %d migrations", movementsTried, len(suggestions))

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
			// Use actual thin provisioning size (UsedDisk) when available
			m.totalStorage += vm.GetEffectiveDisk()
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
// Returns suggestions, node states, and total movements tried
func generateBalancedMigrations(donors, receivers []nodeBalance, metrics clusterMetrics, cluster *proxmox.Cluster, progress BalanceProgressCallback) ([]MigrationSuggestion, map[string]nodeStatesPair, int) {
	var suggestions []MigrationSuggestion
	nodeStates := make(map[string]nodeStatesPair)
	var movementsTried int32

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
	// Use parallel evaluation of candidates for better performance
	maxIterations := 500 // Safety limit
	totalProgress := maxIterations
	currentProgress := 0

	for i := 0; i < maxIterations; i++ {
		if progress != nil && i%10 == 0 {
			currentProgress = i
			progress("Optimizing migrations", currentProgress, totalProgress, int(movementsTried))
		}

		bestMigration, tried := findBestMigrationParallel(donors, receivers, currentStates, migratedVMs, metrics, cluster)
		atomic.AddInt32(&movementsTried, int32(tried))

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
		progress("Finalizing results", totalProgress, totalProgress, int(movementsTried))
	}

	// Update final after states
	for name, state := range currentStates {
		if pair, ok := nodeStates[name]; ok {
			pair.after = state.toNodeState()
			nodeStates[name] = pair
		}
	}

	return suggestions, nodeStates, int(movementsTried)
}

// simulatedNodeState tracks node state during simulation
type simulatedNodeState struct {
	name         string
	vcpus        int
	cpuCores     int
	hostCPUUsage float64 // Current actual host CPU usage percentage (0-100)
	vmCPUSum     float64 // Sum of VM CPU usages (used to estimate host CPU after migrations)
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
		hostCPUUsage: node.CPUUsage, // Actual host CPU usage (0-100 scale, from API it's 0-1)
		ramTotal:     node.MaxMem,
		storageTotal: node.MaxDisk,
		vms:          make(map[int]proxmox.VM),
	}

	// Calculate sum of VM CPU usages for estimation
	for _, vm := range node.VMs {
		s.vms[vm.VMID] = vm
		s.vcpus += vm.CPUCores
		s.ramUsed += vm.MaxMem
		s.storageUsed += vm.GetEffectiveDisk()
		s.vmCount++
		// Sum up VM CPU usage (scaled by vCPUs as a proxy for CPU contribution)
		s.vmCPUSum += vm.CPUUsage * float64(vm.CPUCores)
	}

	return s
}

// getEstimatedHostCPU estimates host CPU usage based on VM CPU contributions
// This helps predict CPU usage after migrations
func (s *simulatedNodeState) getEstimatedHostCPU() float64 {
	if s.cpuCores == 0 || s.vmCPUSum == 0 {
		return s.hostCPUUsage
	}
	// Estimate: total VM CPU contribution / host cores
	// Each VM's contribution is: vmCPUUsage * vmCores
	// Normalize to percentage of host cores
	return s.vmCPUSum / float64(s.cpuCores)
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
	result, _ := findBestMigrationParallel(donors, receivers, states, migratedVMs, metrics, cluster)
	return result
}

// migrationCandidate holds a potential migration for parallel evaluation
type migrationCandidate struct {
	suggestion *MigrationSuggestion
	score      float64
}

// findBestMigrationParallel finds the single best VM migration using parallel evaluation
// Returns the best migration and the number of candidates evaluated
func findBestMigrationParallel(donors, receivers []nodeBalance, states map[string]*simulatedNodeState, migratedVMs map[int]bool, metrics clusterMetrics, cluster *proxmox.Cluster) (*MigrationSuggestion, int) {
	var candidateCount int32

	// Collect all VM-receiver pairs to evaluate in parallel
	type evalJob struct {
		donor         nodeBalance
		donorState    *simulatedNodeState
		vm            proxmox.VM
		receiver      nodeBalance
		receiverState *simulatedNodeState
	}

	var jobs []evalJob

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

			// Add job for each receiver
			for _, receiver := range receivers {
				receiverState := states[receiver.node.Name]
				if receiverState == nil {
					continue
				}
				jobs = append(jobs, evalJob{
					donor:         donor,
					donorState:    donorState,
					vm:            vm,
					receiver:      receiver,
					receiverState: receiverState,
				})
			}
		}
	}

	if len(jobs) == 0 {
		return nil, 0
	}

	// Use parallel evaluation with goroutines
	results := make(chan migrationCandidate, len(jobs))
	var wg sync.WaitGroup

	// Limit concurrency to avoid excessive goroutines
	numWorkers := 32
	if len(jobs) < numWorkers {
		numWorkers = len(jobs)
	}

	jobsChan := make(chan evalJob, len(jobs))
	for _, job := range jobs {
		jobsChan <- job
	}
	close(jobsChan)

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobsChan {
				atomic.AddInt32(&candidateCount, 1)

				// Check if receiver can accept this VM
				if !canAcceptVM(job.receiverState, &job.vm, metrics) {
					continue
				}

				// Calculate improvement score
				score := calculateMigrationScore(job.donorState, job.receiverState, &job.vm, metrics)
				if score > 0 {
					results <- migrationCandidate{
						score: score,
						suggestion: &MigrationSuggestion{
							VMID:        job.vm.VMID,
							VMName:      job.vm.Name,
							SourceNode:  job.donor.node.Name,
							TargetNode:  job.receiver.node.Name,
							Reason:      "Balance cluster",
							Score:       score,
							Status:      job.vm.Status,
							VCPUs:       job.vm.CPUCores,
							CPUUsage:    job.vm.CPUUsage,
							RAM:         job.vm.MaxMem,
							Storage:     job.vm.GetEffectiveDisk(),
							UsedDisk:    job.vm.UsedDisk,
							MaxDisk:     job.vm.MaxDisk,
							SourceCores: job.donorState.cpuCores,
							TargetCores: job.receiverState.cpuCores,
							Details: &MigrationDetails{
								SelectionMode:   "balance_cluster",
								SelectionReason: "Cluster-wide balancing",
								ClusterAvgCPU:   metrics.avgVCPUPercent,
								ClusterAvgRAM:   metrics.avgRAMPercent,
							},
						},
					}
				}
			}
		}()
	}

	// Close results channel when all workers done
	go func() {
		wg.Wait()
		close(results)
	}()

	// Find best result
	var bestMigration *MigrationSuggestion
	bestScore := 0.0
	for candidate := range results {
		if candidate.score > bestScore {
			bestScore = candidate.score
			bestMigration = candidate.suggestion
		}
	}

	return bestMigration, int(candidateCount)
}

// canAcceptVM checks if a receiver can accept a VM without becoming overloaded
func canAcceptVM(receiver *simulatedNodeState, vm *proxmox.VM, metrics clusterMetrics) bool {
	// Calculate projected utilization after adding VM
	newRAMPercent := float64(receiver.ramUsed+vm.MaxMem) / float64(receiver.ramTotal) * 100
	newVCPUPercent := float64(receiver.vcpus+vm.CPUCores) / float64(receiver.cpuCores) * 100
	newStoragePercent := float64(receiver.storageUsed+vm.GetEffectiveDisk()) / float64(receiver.storageTotal) * 100

	// Calculate estimated host CPU usage after adding VM
	// VM contribution = VM's CPU usage * VM's vCPUs
	vmCPUContrib := vm.CPUUsage * float64(vm.CPUCores)
	newEstimatedHostCPU := (receiver.vmCPUSum + vmCPUContrib) / float64(receiver.cpuCores)

	// HARD LIMITS:
	// - Host CPU Usage: Never exceed 95% (actual physical CPU utilization)
	// - RAM: Never exceed 90% (cannot be oversubscribed)
	// - Storage: Never exceed 85% (need headroom for snapshots etc.)
	// - vCPU: NO hard cap (oversubscription is normal, 200-600% is common)
	const hardCapHostCPUPercent = 95.0
	const hardCapRAMPercent = 90.0
	const hardCapStoragePercent = 85.0

	// Check host CPU usage (actual physical utilization, not vCPU allocation)
	if newEstimatedHostCPU > hardCapHostCPUPercent {
		return false
	}

	if newRAMPercent > hardCapRAMPercent {
		return false
	}
	if newStoragePercent > hardCapStoragePercent {
		return false
	}

	// SOFT LIMITS: Don't overshoot the cluster average by more than 5%
	// This ensures balanced distribution across all available hosts
	const softMargin = 5.0
	currentRAMPercent := receiver.getRAMPercent()
	currentVCPUPercent := receiver.getVCPUPercent()
	currentStoragePercent := float64(receiver.storageUsed) / float64(receiver.storageTotal) * 100

	// For RAM: don't exceed average + margin (with hard cap as backup)
	if currentRAMPercent >= metrics.avgRAMPercent-2 && newRAMPercent > metrics.avgRAMPercent+softMargin {
		return false
	}

	// For vCPU allocation: don't exceed average + margin (no hard cap - oversubscription is normal)
	if currentVCPUPercent >= metrics.avgVCPUPercent-2 && newVCPUPercent > metrics.avgVCPUPercent+softMargin {
		return false
	}

	// For Storage: don't exceed average + margin (with hard cap as backup)
	if currentStoragePercent >= metrics.avgStoragePercent-2 && newStoragePercent > metrics.avgStoragePercent+softMargin {
		return false
	}

	// Check storage constraints - use actual thin provisioning size
	incomingVMStorage := vm.GetEffectiveDisk()

	// Calculate storage used after migration
	storageUsedAfter := receiver.storageUsed + incomingVMStorage

	// Basic check - never exceed storage capacity (prevent >100%)
	if receiver.storageTotal > 0 && storageUsedAfter > receiver.storageTotal {
		return false
	}

	// Check storage headroom constraint (500 GiB + 15% of largest VM)
	// Find the largest VM on the receiver (including the incoming VM)
	largestVMStorage := incomingVMStorage
	for _, existingVM := range receiver.vms {
		vmStorage := existingVM.GetEffectiveDisk()
		if vmStorage > largestVMStorage {
			largestVMStorage = vmStorage
		}
	}

	// Calculate required headroom: 500 GiB + 15% of largest VM's storage
	minHeadroomBytes := int64(MinStorageHeadroomGiB) * 1024 * 1024 * 1024
	largestVMHeadroom := int64(float64(largestVMStorage) * LargestVMStorageHeadroomPercent)
	requiredFreeStorage := minHeadroomBytes + largestVMHeadroom

	// Check if we have enough headroom
	actualFreeStorage := receiver.storageTotal - storageUsedAfter
	if actualFreeStorage < requiredFreeStorage {
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
	// Use actual thin provisioning size
	storageGiB := float64(vm.GetEffectiveDisk()) / (1024 * 1024 * 1024)
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
			// Use actual thin provisioning size
			storage := vm.GetEffectiveDisk()
			// VM's CPU contribution (for host CPU estimation)
			vmCPUContrib := vm.CPUUsage * float64(vm.CPUCores)

			// Remove from source
			delete(source.vms, migration.VMID)
			source.vcpus -= vm.CPUCores
			source.ramUsed -= vm.MaxMem
			source.storageUsed -= storage
			source.vmCount--
			source.vmCPUSum -= vmCPUContrib

			// Add to target
			target.vms[migration.VMID] = vm
			target.vcpus += vm.CPUCores
			target.ramUsed += vm.MaxMem
			target.storageUsed += storage
			target.vmCount++
			target.vmCPUSum += vmCPUContrib
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
		// Use actual thin provisioning size (UsedDisk) when available
		storageUsed += vm.GetEffectiveDisk()
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

// vmSwapCandidate represents a VM that could be swapped for vCPU balancing
type vmSwapCandidate struct {
	vm       proxmox.VM
	nodeName string
	vcpus    int
	ram      int64
}

// findVCPUSwapOpportunities finds VMs that can be swapped to balance vCPUs
// without significantly affecting RAM balance. Returns pairs of migrations.
func findVCPUSwapOpportunities(states map[string]*simulatedNodeState, metrics clusterMetrics, maxSwaps int) []MigrationSuggestion {
	var suggestions []MigrationSuggestion

	// Find nodes above and below average vCPU
	var highVCPUNodes, lowVCPUNodes []*simulatedNodeState
	for _, state := range states {
		deviation := state.getVCPUPercent() - metrics.avgVCPUPercent
		if deviation > 5 { // 5% above average
			highVCPUNodes = append(highVCPUNodes, state)
		} else if deviation < -5 { // 5% below average
			lowVCPUNodes = append(lowVCPUNodes, state)
		}
	}

	if len(highVCPUNodes) == 0 || len(lowVCPUNodes) == 0 {
		return suggestions
	}

	// Sort nodes by deviation
	sort.Slice(highVCPUNodes, func(i, j int) bool {
		return highVCPUNodes[i].getVCPUPercent() > highVCPUNodes[j].getVCPUPercent()
	})
	sort.Slice(lowVCPUNodes, func(i, j int) bool {
		return lowVCPUNodes[i].getVCPUPercent() < lowVCPUNodes[j].getVCPUPercent()
	})

	swapCount := 0
	usedVMs := make(map[int]bool)

	// For each high-vCPU node, try to find swap opportunities
	for _, highNode := range highVCPUNodes {
		if swapCount >= maxSwaps {
			break
		}

		// Get high-vCPU VMs from this node
		var highVCPUVMs []vmSwapCandidate
		for _, vm := range highNode.vms {
			if vm.NoMigrate || vm.Status != "running" || usedVMs[vm.VMID] {
				continue
			}
			if vm.CPUCores >= 2 { // Only consider VMs with 2+ vCPUs
				highVCPUVMs = append(highVCPUVMs, vmSwapCandidate{
					vm:       vm,
					nodeName: highNode.name,
					vcpus:    vm.CPUCores,
					ram:      vm.MaxMem,
				})
			}
		}

		// Sort by vCPU count descending
		sort.Slice(highVCPUVMs, func(i, j int) bool {
			return highVCPUVMs[i].vcpus > highVCPUVMs[j].vcpus
		})

		// For each high-vCPU VM, find a matching low-vCPU VM to swap
		for _, highVM := range highVCPUVMs {
			if swapCount >= maxSwaps {
				break
			}

			// RAM tolerance: within 20% or 2GB, whichever is greater
			ramTolerance := int64(float64(highVM.ram) * 0.2)
			if ramTolerance < 2*1024*1024*1024 {
				ramTolerance = 2 * 1024 * 1024 * 1024
			}

			// Search low-vCPU nodes for a swap candidate
			for _, lowNode := range lowVCPUNodes {
				if swapCount >= maxSwaps {
					break
				}

				for _, vm := range lowNode.vms {
					if vm.NoMigrate || vm.Status != "running" || usedVMs[vm.VMID] {
						continue
					}

					// Check if RAM is similar
					ramDiff := vm.MaxMem - highVM.ram
					if ramDiff < 0 {
						ramDiff = -ramDiff
					}
					if ramDiff > ramTolerance {
						continue
					}

					// Check if vCPU swap would help balance
					vcpuDiff := highVM.vcpus - vm.CPUCores
					if vcpuDiff <= 0 {
						continue // Low-vCPU VM should have fewer vCPUs
					}

					// This is a valid swap! Create two migration suggestions
					log.Printf("vCPU Swap: VM %d (%s, %d vCPU, %d GB RAM) on %s <-> VM %d (%s, %d vCPU, %d GB RAM) on %s",
						highVM.vm.VMID, highVM.vm.Name, highVM.vcpus, highVM.ram/(1024*1024*1024), highNode.name,
						vm.VMID, vm.Name, vm.CPUCores, vm.MaxMem/(1024*1024*1024), lowNode.name)

					// Swap high-vCPU VM to low-vCPU node
					suggestions = append(suggestions, MigrationSuggestion{
						VMID:        highVM.vm.VMID,
						VMName:      highVM.vm.Name,
						SourceNode:  highNode.name,
						TargetNode:  lowNode.name,
						Reason:      fmt.Sprintf("vCPU balance swap (-%d vCPU)", vcpuDiff),
						Score:       float64(vcpuDiff) * 10,
						Status:      highVM.vm.Status,
						VCPUs:       highVM.vcpus,
						CPUUsage:    highVM.vm.CPUUsage,
						RAM:         highVM.ram,
						Storage:     highVM.vm.GetEffectiveDisk(),
						UsedDisk:    highVM.vm.UsedDisk,
						MaxDisk:     highVM.vm.MaxDisk,
						SourceCores: highNode.cpuCores,
						TargetCores: lowNode.cpuCores,
						Details: &MigrationDetails{
							SelectionMode:   "vcpu_swap",
							SelectionReason: "vCPU balancing via swap",
						},
					})

					// Swap low-vCPU VM to high-vCPU node
					suggestions = append(suggestions, MigrationSuggestion{
						VMID:        vm.VMID,
						VMName:      vm.Name,
						SourceNode:  lowNode.name,
						TargetNode:  highNode.name,
						Reason:      fmt.Sprintf("vCPU balance swap (+%d vCPU)", vcpuDiff),
						Score:       float64(vcpuDiff) * 10,
						Status:      vm.Status,
						VCPUs:       vm.CPUCores,
						CPUUsage:    vm.CPUUsage,
						RAM:         vm.MaxMem,
						Storage:     vm.GetEffectiveDisk(),
						UsedDisk:    vm.UsedDisk,
						MaxDisk:     vm.MaxDisk,
						SourceCores: lowNode.cpuCores,
						TargetCores: highNode.cpuCores,
						Details: &MigrationDetails{
							SelectionMode:   "vcpu_swap",
							SelectionReason: "vCPU balancing via swap",
						},
					})

					// Mark VMs as used
					usedVMs[highVM.vm.VMID] = true
					usedVMs[vm.VMID] = true
					swapCount++
					break // Found a swap for this high-vCPU VM
				}
			}
		}
	}

	// Try multi-VM swaps (2-for-1 or 3-for-1) if 1-for-1 swaps didn't fully balance
	if swapCount < maxSwaps {
		multiSwapSuggestions := findMultiVMSwapOpportunities(states, metrics, maxSwaps-swapCount, usedVMs)
		suggestions = append(suggestions, multiSwapSuggestions...)
	}

	return suggestions
}

// findMultiVMSwapOpportunities finds 2-for-1 or 3-for-1 VM swaps to balance vCPUs/VM count
// For example: swap 1 large VM for 2 smaller VMs with similar total RAM but fewer vCPUs
func findMultiVMSwapOpportunities(states map[string]*simulatedNodeState, metrics clusterMetrics, maxSwaps int, usedVMs map[int]bool) []MigrationSuggestion {
	var suggestions []MigrationSuggestion

	// Find nodes that are imbalanced in VM count or vCPUs
	type nodeImbalance struct {
		state   *simulatedNodeState
		vmDev   float64 // VM count deviation from average
		vcpuDev float64 // vCPU deviation from average
	}

	avgVMCount := float64(metrics.totalVMs) / float64(metrics.nodeCount)
	var imbalanced []nodeImbalance

	for _, state := range states {
		vmDev := float64(state.vmCount) - avgVMCount
		vcpuDev := state.getVCPUPercent() - metrics.avgVCPUPercent
		if math.Abs(vmDev) > 2 || math.Abs(vcpuDev) > 5 {
			imbalanced = append(imbalanced, nodeImbalance{
				state:   state,
				vmDev:   vmDev,
				vcpuDev: vcpuDev,
			})
		}
	}

	if len(imbalanced) < 2 {
		return suggestions
	}

	// Sort: nodes with high VM count first (donors), low VM count last (receivers)
	sort.Slice(imbalanced, func(i, j int) bool {
		return imbalanced[i].vmDev > imbalanced[j].vmDev
	})

	swapCount := 0

	// For each high-VM-count node, try to swap 1 large VM for 2-3 smaller VMs
	for i := 0; i < len(imbalanced) && imbalanced[i].vmDev > 2 && swapCount < maxSwaps; i++ {
		highVMNode := imbalanced[i].state

		// Get large VMs from this node (sorted by RAM descending)
		var largeVMs []proxmox.VM
		for _, vm := range highVMNode.vms {
			if vm.NoMigrate || vm.Status != "running" || usedVMs[vm.VMID] {
				continue
			}
			if vm.MaxMem >= 8*1024*1024*1024 { // Only consider VMs with 8+ GB RAM
				largeVMs = append(largeVMs, vm)
			}
		}
		sort.Slice(largeVMs, func(a, b int) bool {
			return largeVMs[a].MaxMem > largeVMs[b].MaxMem
		})

		// Search for matching 2-3 smaller VMs from low-VM-count nodes
		for j := len(imbalanced) - 1; j > i && imbalanced[j].vmDev < -1 && swapCount < maxSwaps; j-- {
			lowVMNode := imbalanced[j].state

			// Get small VMs from this node
			var smallVMs []proxmox.VM
			for _, vm := range lowVMNode.vms {
				if vm.NoMigrate || vm.Status != "running" || usedVMs[vm.VMID] {
					continue
				}
				smallVMs = append(smallVMs, vm)
			}
			sort.Slice(smallVMs, func(a, b int) bool {
				return smallVMs[a].MaxMem < smallVMs[b].MaxMem
			})

			// Try to find a 2-for-1 or 3-for-1 swap
			for _, largeVM := range largeVMs {
				if usedVMs[largeVM.VMID] || swapCount >= maxSwaps {
					continue
				}

				// Try 2-for-1 swap
				match2 := findMatchingSmallVMs(largeVM, smallVMs, 2, usedVMs)
				if match2 != nil {
					log.Printf("Multi-swap 2-for-1: VM %d (%s, %d vCPU, %d GB) on %s <-> VMs %v on %s",
						largeVM.VMID, largeVM.Name, largeVM.CPUCores, largeVM.MaxMem/(1024*1024*1024), highVMNode.name,
						getVMIDs(match2), lowVMNode.name)

					// Large VM goes from high-VM-count to low-VM-count node
					suggestions = append(suggestions, MigrationSuggestion{
						VMID:        largeVM.VMID,
						VMName:      largeVM.Name,
						SourceNode:  highVMNode.name,
						TargetNode:  lowVMNode.name,
						Reason:      fmt.Sprintf("Multi-swap 2-for-1 (balance VM count: %d→%d)", highVMNode.vmCount, highVMNode.vmCount-1+len(match2)),
						Score:       100,
						Status:      largeVM.Status,
						VCPUs:       largeVM.CPUCores,
						CPUUsage:    largeVM.CPUUsage,
						RAM:         largeVM.MaxMem,
						Storage:     largeVM.GetEffectiveDisk(),
						UsedDisk:    largeVM.UsedDisk,
						MaxDisk:     largeVM.MaxDisk,
						SourceCores: highVMNode.cpuCores,
						TargetCores: lowVMNode.cpuCores,
						Details: &MigrationDetails{
							SelectionMode:   "multi_swap",
							SelectionReason: "VM count/vCPU balancing via 2-for-1 swap",
						},
					})

					// Small VMs go from low-VM-count to high-VM-count node
					for _, smallVM := range match2 {
						suggestions = append(suggestions, MigrationSuggestion{
							VMID:        smallVM.VMID,
							VMName:      smallVM.Name,
							SourceNode:  lowVMNode.name,
							TargetNode:  highVMNode.name,
							Reason:      "Multi-swap 2-for-1 (part of swap group)",
							Score:       100,
							Status:      smallVM.Status,
							VCPUs:       smallVM.CPUCores,
							CPUUsage:    smallVM.CPUUsage,
							RAM:         smallVM.MaxMem,
							Storage:     smallVM.GetEffectiveDisk(),
							UsedDisk:    smallVM.UsedDisk,
							MaxDisk:     smallVM.MaxDisk,
							SourceCores: lowVMNode.cpuCores,
							TargetCores: highVMNode.cpuCores,
							Details: &MigrationDetails{
								SelectionMode:   "multi_swap",
								SelectionReason: "VM count/vCPU balancing via 2-for-1 swap",
							},
						})
						usedVMs[smallVM.VMID] = true
					}
					usedVMs[largeVM.VMID] = true
					swapCount++
					break
				}

				// Try 3-for-1 swap
				match3 := findMatchingSmallVMs(largeVM, smallVMs, 3, usedVMs)
				if match3 != nil {
					log.Printf("Multi-swap 3-for-1: VM %d (%s, %d vCPU, %d GB) on %s <-> VMs %v on %s",
						largeVM.VMID, largeVM.Name, largeVM.CPUCores, largeVM.MaxMem/(1024*1024*1024), highVMNode.name,
						getVMIDs(match3), lowVMNode.name)

					// Large VM goes from high-VM-count to low-VM-count node
					suggestions = append(suggestions, MigrationSuggestion{
						VMID:        largeVM.VMID,
						VMName:      largeVM.Name,
						SourceNode:  highVMNode.name,
						TargetNode:  lowVMNode.name,
						Reason:      fmt.Sprintf("Multi-swap 3-for-1 (balance VM count: %d→%d)", highVMNode.vmCount, highVMNode.vmCount-1+len(match3)),
						Score:       100,
						Status:      largeVM.Status,
						VCPUs:       largeVM.CPUCores,
						CPUUsage:    largeVM.CPUUsage,
						RAM:         largeVM.MaxMem,
						Storage:     largeVM.GetEffectiveDisk(),
						UsedDisk:    largeVM.UsedDisk,
						MaxDisk:     largeVM.MaxDisk,
						SourceCores: highVMNode.cpuCores,
						TargetCores: lowVMNode.cpuCores,
						Details: &MigrationDetails{
							SelectionMode:   "multi_swap",
							SelectionReason: "VM count/vCPU balancing via 3-for-1 swap",
						},
					})

					// Small VMs go from low-VM-count to high-VM-count node
					for _, smallVM := range match3 {
						suggestions = append(suggestions, MigrationSuggestion{
							VMID:        smallVM.VMID,
							VMName:      smallVM.Name,
							SourceNode:  lowVMNode.name,
							TargetNode:  highVMNode.name,
							Reason:      "Multi-swap 3-for-1 (part of swap group)",
							Score:       100,
							Status:      smallVM.Status,
							VCPUs:       smallVM.CPUCores,
							CPUUsage:    smallVM.CPUUsage,
							RAM:         smallVM.MaxMem,
							Storage:     smallVM.GetEffectiveDisk(),
							UsedDisk:    smallVM.UsedDisk,
							MaxDisk:     smallVM.MaxDisk,
							SourceCores: lowVMNode.cpuCores,
							TargetCores: highVMNode.cpuCores,
							Details: &MigrationDetails{
								SelectionMode:   "multi_swap",
								SelectionReason: "VM count/vCPU balancing via 3-for-1 swap",
							},
						})
						usedVMs[smallVM.VMID] = true
					}
					usedVMs[largeVM.VMID] = true
					swapCount++
					break
				}
			}
		}
	}

	return suggestions
}

// findMatchingSmallVMs finds n small VMs that together match the large VM's RAM (within 30%)
// and have fewer total vCPUs
func findMatchingSmallVMs(largeVM proxmox.VM, smallVMs []proxmox.VM, n int, usedVMs map[int]bool) []proxmox.VM {
	if len(smallVMs) < n {
		return nil
	}

	targetRAM := largeVM.MaxMem
	tolerance := int64(float64(targetRAM) * 0.3) // 30% tolerance

	// Try all combinations of n VMs
	var result []proxmox.VM
	var findCombination func(start int, current []proxmox.VM, currentRAM int64, currentVCPU int)
	found := false

	findCombination = func(start int, current []proxmox.VM, currentRAM int64, currentVCPU int) {
		if found {
			return
		}
		if len(current) == n {
			// Check if RAM is within tolerance and vCPUs are fewer
			ramDiff := currentRAM - targetRAM
			if ramDiff < 0 {
				ramDiff = -ramDiff
			}
			if ramDiff <= tolerance && currentVCPU < largeVM.CPUCores {
				result = make([]proxmox.VM, n)
				copy(result, current)
				found = true
			}
			return
		}

		for i := start; i < len(smallVMs) && !found; i++ {
			vm := smallVMs[i]
			if usedVMs[vm.VMID] {
				continue
			}
			findCombination(i+1, append(current, vm), currentRAM+vm.MaxMem, currentVCPU+vm.CPUCores)
		}
	}

	findCombination(0, nil, 0, 0)
	return result
}

// getVMIDs returns a slice of VMID integers from a slice of VMs
func getVMIDs(vms []proxmox.VM) []int {
	ids := make([]int, len(vms))
	for i, vm := range vms {
		ids[i] = vm.VMID
	}
	return ids
}
