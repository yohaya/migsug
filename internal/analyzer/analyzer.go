package analyzer

import (
	"fmt"
	"log"
	"math"
	"sort"
	"strings"
	"time"

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
	var vmsToMigrate []proxmox.VM
	if constraints.BalanceCluster {
		// Balance cluster mode needs cluster context for calculating averages
		vmsToMigrate = selectForBalanceCluster(sourceNode, cluster, targets)
	} else {
		vmsToMigrate = SelectVMsToMigrate(sourceNode, constraints)
	}
	if len(vmsToMigrate) == 0 {
		return nil, fmt.Errorf("no VMs selected for migration based on given constraints")
	}

	var suggestions []MigrationSuggestion

	// Use specialized algorithm for ModeAll and ModeBalanceCluster to ensure balanced distribution
	if constraints.MigrateAll || constraints.BalanceCluster {
		suggestions = GenerateSuggestionsBalanced(vmsToMigrate, targets, cluster, sourceNode, constraints)
	} else {
		suggestions = GenerateSuggestions(vmsToMigrate, targets, cluster, sourceNode, constraints)
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
		// Use efficiency-based CPU selection (automatic optimization)
		return selectByCPUUsageEfficient(node, *constraints.CPUUsage)
	case ModeRAM:
		return selectByRAM(node, *constraints.RAMAmount)
	case ModeStorage:
		return selectByStorage(node, *constraints.StorageAmount)
	case ModeCreationDate:
		return selectByCreationDate(node, *constraints.CreationAge)
	case ModeBalanceCluster:
		// This requires cluster context, will be handled in Analyze()
		return []proxmox.VM{}
	default:
		return []proxmox.VM{}
	}
}

// selectAllVMs returns ALL VMs from the node for "Migrate All" mode (including powered-off)
// Excludes VMs with nomigrate=true in their config
func selectAllVMs(node *proxmox.Node) []proxmox.VM {
	// Return ALL VMs (running and stopped) sorted by resource usage (largest first for better distribution)
	// Filter out VMs that cannot be migrated
	vms := filterMigratableVMs(node.VMs)

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

// filterMigratableVMs returns only VMs that can be migrated (excludes VMs with NoMigrate=true)
func filterMigratableVMs(vms []proxmox.VM) []proxmox.VM {
	var migratable []proxmox.VM
	for _, vm := range vms {
		if !vm.NoMigrate {
			migratable = append(migratable, vm)
		}
	}
	return migratable
}

// filterRunningMigratableVMs returns only running VMs that can be migrated
func filterRunningMigratableVMs(vms []proxmox.VM) []proxmox.VM {
	return filterMigratableVMs(filterRunningVMs(vms))
}

// VMPlacementConstraint represents reasons why a VM cannot be placed on a target
type VMPlacementConstraint struct {
	Violated bool
	Reason   string
}

// CheckVMPlacementConstraints checks if a VM can be placed on a target node
// based on its placement constraints (hostcpumodel, withvm, without)
// Parameters:
//   - vm: the VM to check
//   - targetNode: the target node to evaluate
//   - cluster: the full cluster (needed to find where other VMs are located)
//   - plannedMigrations: map of VM names to their planned target nodes (for VMs being migrated in the same batch)
func CheckVMPlacementConstraints(vm proxmox.VM, targetNode *proxmox.Node, cluster *proxmox.Cluster, plannedMigrations map[string]string) VMPlacementConstraint {
	// Check hostcpumodel constraint
	// VM can only run on hosts where CPU model contains the required substring
	if vm.HostCPUModel != "" {
		if !strings.Contains(targetNode.CPUModel, vm.HostCPUModel) {
			return VMPlacementConstraint{
				Violated: true,
				Reason:   fmt.Sprintf("CPU model mismatch: requires '%s' in model, target has '%s'", vm.HostCPUModel, targetNode.CPUModel),
			}
		}
	}

	// Check withvm constraint
	// VM must be on the same host as specified VMs
	for _, requiredVMName := range vm.WithVM {
		requiredVMNode := findVMNode(requiredVMName, cluster, plannedMigrations)
		if requiredVMNode == "" {
			// VM not found in cluster, skip this constraint
			continue
		}
		if requiredVMNode != targetNode.Name {
			return VMPlacementConstraint{
				Violated: true,
				Reason:   fmt.Sprintf("Must be with VM '%s' (currently on %s)", requiredVMName, requiredVMNode),
			}
		}
	}

	// Check without constraint
	// VM must NOT be on the same host as specified VMs
	for _, excludedVMName := range vm.WithoutVM {
		excludedVMNode := findVMNode(excludedVMName, cluster, plannedMigrations)
		if excludedVMNode == "" {
			// VM not found in cluster, skip this constraint
			continue
		}
		if excludedVMNode == targetNode.Name {
			return VMPlacementConstraint{
				Violated: true,
				Reason:   fmt.Sprintf("Cannot be with VM '%s' (on %s)", excludedVMName, excludedVMNode),
			}
		}
	}

	return VMPlacementConstraint{Violated: false}
}

// findVMNode finds which node a VM is currently on (or will be after planned migrations)
func findVMNode(vmName string, cluster *proxmox.Cluster, plannedMigrations map[string]string) string {
	// First check if this VM has a planned migration
	if plannedMigrations != nil {
		if targetNode, ok := plannedMigrations[vmName]; ok {
			return targetNode
		}
	}

	// Search the cluster for the VM
	for _, node := range cluster.Nodes {
		for _, vm := range node.VMs {
			if vm.Name == vmName {
				return node.Name
			}
		}
	}
	return ""
}

// HasPlacementConstraints returns true if the VM has any placement constraints
func HasPlacementConstraints(vm proxmox.VM) bool {
	return vm.HostCPUModel != "" || len(vm.WithVM) > 0 || len(vm.WithoutVM) > 0
}

// GetPlacementConstraintsSummary returns a human-readable summary of VM placement constraints
func GetPlacementConstraintsSummary(vm proxmox.VM) string {
	var parts []string
	if vm.HostCPUModel != "" {
		parts = append(parts, fmt.Sprintf("hostcpumodel=%s", vm.HostCPUModel))
	}
	if len(vm.WithVM) > 0 {
		parts = append(parts, fmt.Sprintf("withvm=%s", strings.Join(vm.WithVM, ",")))
	}
	if len(vm.WithoutVM) > 0 {
		parts = append(parts, fmt.Sprintf("without=%s", strings.Join(vm.WithoutVM, ",")))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "; ")
}

func selectByVMCount(node *proxmox.Node, count int) []proxmox.VM {
	// Only select running VMs that can be migrated
	vms := filterRunningMigratableVMs(node.VMs)

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
	// Only select running VMs that can be migrated
	vms := filterRunningMigratableVMs(node.VMs)

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
	return selectByCPUUsageEfficient(node, targetUsage)
}

// selectByCPUUsageEfficient selects VMs to migrate based on CPU efficiency
// Efficiency = Host CPU% / Disk Size (GiB) - maximizes CPU freed per data migrated
// This automatically finds the optimal set of VMs for fastest CPU relief
func selectByCPUUsageEfficient(node *proxmox.Node, targetUsage float64) []proxmox.VM {
	// Only select running VMs that can be migrated
	vms := filterRunningMigratableVMs(node.VMs)

	// Calculate each VM's efficiency score
	type vmWithEfficiency struct {
		vm         proxmox.VM
		hostCPU    float64 // Actual host CPU% contribution (HCPU%)
		diskGiB    float64 // Disk size in GiB
		efficiency float64 // CPU% per GiB - higher is better
	}

	vmsWithEfficiency := make([]vmWithEfficiency, 0, len(vms))
	for _, vm := range vms {
		hostCPU := 0.0
		if node.CPUCores > 0 {
			hostCPU = vm.CPUUsage * float64(vm.CPUCores) / float64(node.CPUCores)
		}

		// Get disk size in GiB (prefer MaxDisk, fallback to UsedDisk)
		disk := vm.MaxDisk
		if disk == 0 {
			disk = vm.UsedDisk
		}
		diskGiB := float64(disk) / (1024 * 1024 * 1024)
		if diskGiB < 1 {
			diskGiB = 1 // Minimum 1 GiB to avoid division issues
		}

		// Efficiency = CPU% freed per GiB migrated
		// Higher efficiency means more CPU relief per unit of migration time
		efficiency := hostCPU / diskGiB

		vmsWithEfficiency = append(vmsWithEfficiency, vmWithEfficiency{
			vm:         vm,
			hostCPU:    hostCPU,
			diskGiB:    diskGiB,
			efficiency: efficiency,
		})
	}

	// Sort VMs by efficiency (highest first) - get most CPU relief per GiB migrated
	sort.Slice(vmsWithEfficiency, func(i, j int) bool {
		return vmsWithEfficiency[i].efficiency > vmsWithEfficiency[j].efficiency
	})

	// Select VMs in efficiency order until we reach target host CPU%
	var selected []proxmox.VM
	totalHostCPU := 0.0

	for _, v := range vmsWithEfficiency {
		if totalHostCPU >= targetUsage {
			break
		}
		selected = append(selected, v.vm)
		totalHostCPU += v.hostCPU
	}

	return selected
}

// SelectByCPUUsageDetailed returns selected VMs along with detailed efficiency information
// This is useful for displaying migration plan details to the user
func SelectByCPUUsageDetailed(node *proxmox.Node, targetUsage float64) ([]proxmox.VM, *CPUMigrationPlan) {
	vms := filterRunningMigratableVMs(node.VMs)

	plan := &CPUMigrationPlan{
		TargetCPUReduction: targetUsage,
	}

	type vmWithEfficiency struct {
		vm         proxmox.VM
		hostCPU    float64
		diskGiB    float64
		efficiency float64
	}

	vmsWithEfficiency := make([]vmWithEfficiency, 0, len(vms))
	var totalAvailableCPU float64
	var totalAvailableDisk float64

	for _, vm := range vms {
		hostCPU := 0.0
		if node.CPUCores > 0 {
			hostCPU = vm.CPUUsage * float64(vm.CPUCores) / float64(node.CPUCores)
		}

		disk := vm.MaxDisk
		if disk == 0 {
			disk = vm.UsedDisk
		}
		diskGiB := float64(disk) / (1024 * 1024 * 1024)
		if diskGiB < 1 {
			diskGiB = 1
		}

		efficiency := hostCPU / diskGiB

		totalAvailableCPU += hostCPU
		totalAvailableDisk += diskGiB

		vmsWithEfficiency = append(vmsWithEfficiency, vmWithEfficiency{
			vm:         vm,
			hostCPU:    hostCPU,
			diskGiB:    diskGiB,
			efficiency: efficiency,
		})
	}

	// Sort by efficiency (highest first)
	sort.Slice(vmsWithEfficiency, func(i, j int) bool {
		return vmsWithEfficiency[i].efficiency > vmsWithEfficiency[j].efficiency
	})

	var selected []proxmox.VM
	totalHostCPU := 0.0
	totalDiskGiB := 0.0

	for _, v := range vmsWithEfficiency {
		if totalHostCPU >= targetUsage {
			break
		}

		selected = append(selected, v.vm)
		totalHostCPU += v.hostCPU
		totalDiskGiB += v.diskGiB
	}

	plan.AchievedCPUReduction = totalHostCPU
	plan.TotalVMsSelected = len(selected)
	plan.TotalDiskGiB = totalDiskGiB
	plan.AvailableCPU = totalAvailableCPU
	plan.AvailableDiskGiB = totalAvailableDisk

	// Calculate efficiency of the selection
	if totalDiskGiB > 0 {
		plan.SelectionEfficiency = totalHostCPU / totalDiskGiB
	}

	// Calculate if target was reached
	plan.TargetReached = totalHostCPU >= targetUsage
	if !plan.TargetReached {
		plan.Shortfall = targetUsage - totalHostCPU
	}

	return selected, plan
}

// CPUMigrationPlan contains detailed information about the CPU migration selection
type CPUMigrationPlan struct {
	TargetCPUReduction   float64
	AchievedCPUReduction float64
	TotalVMsSelected     int
	TotalDiskGiB         float64 // Total disk to migrate
	AvailableCPU         float64 // Total CPU% available on host
	AvailableDiskGiB     float64 // Total disk on host
	SelectionEfficiency  float64 // CPU%/GiB of selected VMs
	TargetReached        bool
	Shortfall            float64 // How much CPU% we couldn't free (if target not reached)
}

func selectByRAM(node *proxmox.Node, targetRAM int64) []proxmox.VM {
	// Only select running VMs that can be migrated
	vms := filterRunningMigratableVMs(node.VMs)

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
	// Filter out VMs that cannot be migrated
	vms := filterMigratableVMs(node.VMs)

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

// selectByCreationDate selects VMs that were created more than N days ago
// This helps migrate older VMs that may be stale or need to be moved to different hosts
func selectByCreationDate(node *proxmox.Node, minAgeDays int) []proxmox.VM {
	// Include ALL VMs (running and stopped) that can be migrated
	vms := filterMigratableVMs(node.VMs)

	// Calculate the threshold timestamp (VMs created before this time are selected)
	now := time.Now()
	thresholdTime := now.Unix() - int64(minAgeDays*24*60*60)
	thresholdDate := time.Unix(thresholdTime, 0).Format("2006-01-02")

	// Debug logging
	log.Printf("selectByCreationDate: Node %s has %d total VMs, %d migratable VMs", node.Name, len(node.VMs), len(vms))
	log.Printf("selectByCreationDate: Today is %s, looking for VMs created before %s (older than %d days)",
		now.Format("2006-01-02"), thresholdDate, minAgeDays)
	log.Printf("selectByCreationDate: Threshold timestamp: %d (VMs with ctime < %d will be selected)", thresholdTime, thresholdTime)

	// Count VMs with valid creation time for debugging
	vmsWithCtime := 0
	vmsWithoutCtime := 0
	vmsOlderThanThreshold := 0
	for _, vm := range vms {
		if vm.CreationTime > 0 {
			vmsWithCtime++
			ageInDays := (now.Unix() - vm.CreationTime) / (24 * 60 * 60)
			createdDate := time.Unix(vm.CreationTime, 0).Format("2006-01-02")
			isOlder := vm.CreationTime < thresholdTime
			if isOlder {
				vmsOlderThanThreshold++
			}
			log.Printf("  VM %d (%s): created=%s (ctime=%d), age=%d days, qualifies: %v",
				vm.VMID, vm.Name, createdDate, vm.CreationTime, ageInDays, isOlder)
		} else {
			vmsWithoutCtime++
			log.Printf("  VM %d (%s): CreationTime=0 (no ctime in config)", vm.VMID, vm.Name)
		}
	}
	log.Printf("selectByCreationDate: Summary: %d with ctime, %d without ctime, %d qualify (older than %d days)",
		vmsWithCtime, vmsWithoutCtime, vmsOlderThanThreshold, minAgeDays)

	// Filter VMs older than the threshold
	var oldVMs []proxmox.VM
	for _, vm := range vms {
		// Only include VMs that have a valid creation time and are older than threshold
		if vm.CreationTime > 0 && vm.CreationTime < thresholdTime {
			oldVMs = append(oldVMs, vm)
		}
	}

	log.Printf("selectByCreationDate: Selected %d VMs older than %d days", len(oldVMs), minAgeDays)

	// Sort by creation time (oldest first)
	sort.Slice(oldVMs, func(i, j int) bool {
		return oldVMs[i].CreationTime < oldVMs[j].CreationTime
	})

	return oldVMs
}

// selectForBalanceCluster selects VMs to migrate from source node to bring it to cluster average
// with minimum VM moves and preferring VMs smaller than 500GiB
func selectForBalanceCluster(sourceNode *proxmox.Node, cluster *proxmox.Cluster, targets []proxmox.Node) []proxmox.VM {
	// Only consider running, migratable VMs since they contribute to CPU/RAM load
	vms := filterRunningMigratableVMs(sourceNode.VMs)
	if len(vms) == 0 {
		return nil
	}

	// Calculate cluster averages (excluding source node for a fair comparison)
	clusterCPUSum := 0.0
	clusterRAMSum := 0.0
	nodeCount := 0
	for _, node := range cluster.Nodes {
		if node.Name != sourceNode.Name && node.Status == "online" {
			clusterCPUSum += node.CPUUsage * 100 // Convert to percentage
			clusterRAMSum += node.GetMemPercent()
			nodeCount++
		}
	}
	// Include source in the average calculation
	clusterCPUSum += sourceNode.CPUUsage * 100
	clusterRAMSum += sourceNode.GetMemPercent()
	nodeCount++

	clusterAvgCPU := clusterCPUSum / float64(nodeCount)
	clusterAvgRAM := clusterRAMSum / float64(nodeCount)

	log.Printf("selectForBalanceCluster: Cluster avg CPU: %.1f%%, RAM: %.1f%%", clusterAvgCPU, clusterAvgRAM)
	log.Printf("selectForBalanceCluster: Source node CPU: %.1f%%, RAM: %.1f%%",
		sourceNode.CPUUsage*100, sourceNode.GetMemPercent())

	// Calculate how much we need to reduce
	currentCPU := sourceNode.CPUUsage * 100
	currentRAM := sourceNode.GetMemPercent()
	targetCPUReduction := currentCPU - clusterAvgCPU
	targetRAMReduction := currentRAM - clusterAvgRAM

	// If source is already at or below average, no migration needed
	if targetCPUReduction <= 0 && targetRAMReduction <= 0 {
		log.Printf("selectForBalanceCluster: Source node already at or below cluster average, no migration needed")
		return nil
	}

	log.Printf("selectForBalanceCluster: Need to reduce CPU by %.1f%%, RAM by %.1f%%",
		math.Max(0, targetCPUReduction), math.Max(0, targetRAMReduction))

	// Calculate each VM's contribution and score
	const maxPreferredStorageGiB = 500.0
	type vmScore struct {
		vm         proxmox.VM
		cpuContrib float64 // Host CPU% contribution
		ramContrib float64 // RAM GiB
		storageGiB float64
		score      float64 // Higher = better candidate (more impact per storage, smaller preferred)
		isSmall    bool    // < 500 GiB
	}

	scored := make([]vmScore, 0, len(vms))
	for _, vm := range vms {
		// Calculate host CPU contribution
		cpuContrib := 0.0
		if sourceNode.CPUCores > 0 {
			cpuContrib = vm.CPUUsage * float64(vm.CPUCores) / float64(sourceNode.CPUCores)
		}

		// RAM contribution in GiB
		ramContribGiB := float64(vm.MaxMem) / (1024 * 1024 * 1024)

		// Storage in GiB
		storage := vm.MaxDisk
		if storage == 0 {
			storage = vm.UsedDisk
		}
		storageGiB := float64(storage) / (1024 * 1024 * 1024)
		if storageGiB < 1 {
			storageGiB = 1
		}

		isSmall := storageGiB <= maxPreferredStorageGiB

		// Score: higher impact per storage = better
		// Combined impact = weighted CPU + RAM contribution
		// We want to minimize storage moved while maximizing resource freed
		combinedImpact := cpuContrib + (ramContribGiB / 10) // Normalize RAM to similar scale
		efficiencyScore := combinedImpact / storageGiB

		// Bonus for small VMs (we strongly prefer them)
		sizeBonus := 1.0
		if isSmall {
			sizeBonus = 2.0 // Double the score for small VMs
		}

		scored = append(scored, vmScore{
			vm:         vm,
			cpuContrib: cpuContrib,
			ramContrib: ramContribGiB,
			storageGiB: storageGiB,
			score:      efficiencyScore * sizeBonus,
			isSmall:    isSmall,
		})
	}

	// Sort by score (highest first) - best candidates first
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	// Select VMs until we reach target CPU and RAM reduction
	var selected []proxmox.VM
	totalCPUReduction := 0.0
	totalRAMReduction := 0.0
	cpuTarget := math.Max(0, targetCPUReduction)
	ramTarget := math.Max(0, targetRAMReduction)

	// First pass: select from small VMs only
	for _, s := range scored {
		if !s.isSmall {
			continue
		}
		// Check if we've reached both targets
		if totalCPUReduction >= cpuTarget && totalRAMReduction >= ramTarget {
			break
		}

		selected = append(selected, s.vm)
		totalCPUReduction += s.cpuContrib
		// RAM reduction is based on MaxMem / total node RAM
		if sourceNode.MaxMem > 0 {
			totalRAMReduction += (float64(s.vm.MaxMem) / float64(sourceNode.MaxMem)) * 100
		}
	}

	// Second pass: if we haven't reached targets, consider large VMs
	if totalCPUReduction < cpuTarget || totalRAMReduction < ramTarget {
		for _, s := range scored {
			if s.isSmall {
				continue // Already considered
			}
			// Check if we've reached both targets
			if totalCPUReduction >= cpuTarget && totalRAMReduction >= ramTarget {
				break
			}

			selected = append(selected, s.vm)
			totalCPUReduction += s.cpuContrib
			if sourceNode.MaxMem > 0 {
				totalRAMReduction += (float64(s.vm.MaxMem) / float64(sourceNode.MaxMem)) * 100
			}

			log.Printf("selectForBalanceCluster: Including large VM %d (%s) - %.0f GiB",
				s.vm.VMID, s.vm.Name, s.storageGiB)
		}
	}

	log.Printf("selectForBalanceCluster: Selected %d VMs to reduce CPU by %.1f%%, RAM by %.1f%%",
		len(selected), totalCPUReduction, totalRAMReduction)

	return selected
}

// GenerateSuggestions creates migration suggestions by finding best targets
func GenerateSuggestions(vms []proxmox.VM, targets []proxmox.Node, cluster *proxmox.Cluster, sourceNode *proxmox.Node, constraints MigrationConstraints) []MigrationSuggestion {
	var suggestions []MigrationSuggestion

	// Track target states for capacity checking
	targetStates := make(map[string]NodeState)
	targetCoresMap := make(map[string]int)
	targetNodesMap := make(map[string]*proxmox.Node)
	for i := range targets {
		target := &targets[i]
		targetStates[target.Name] = NewNodeState(target)
		targetCoresMap[target.Name] = target.CPUCores
		targetNodesMap[target.Name] = target
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

	// Track planned migrations for constraint checking (VM name -> target node name)
	plannedMigrations := make(map[string]string)

	// Determine selection mode for details
	selectionMode := constraints.GetMode().String()
	selectionReason := getSelectionReason(constraints)

	// For each VM, find the best target
	for _, vm := range vms {
		targetNode, score, reason, details := FindBestTarget(vm, targetStates, vmsPerTarget, constraints, cluster, targetNodesMap, plannedMigrations)

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
			// Track this migration for constraint checking of subsequent VMs
			plannedMigrations[vm.Name] = targetNode
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
		return fmt.Sprintf("Selected by efficiency (CPU%%/GiB) to reduce CPU by %.1f%% with minimal data transfer", *constraints.CPUUsage)
	case ModeRAM:
		return fmt.Sprintf("Selected to free up %d GB RAM from source host", *constraints.RAMAmount/(1024*1024*1024))
	case ModeStorage:
		return fmt.Sprintf("Selected to free up %d GB storage from source host", *constraints.StorageAmount/(1024*1024*1024))
	case ModeCreationDate:
		return fmt.Sprintf("Selected because VM was created more than %d days ago", *constraints.CreationAge)
	case ModeBalanceCluster:
		return "Selected to balance cluster (bring host CPU/RAM to cluster average with minimum moves)"
	default:
		return "Selected based on migration criteria"
	}
}

// FindBestTarget finds the best target node for a VM and returns detailed reasoning
func FindBestTarget(vm proxmox.VM, targetStates map[string]NodeState, vmsPerTarget map[string]int, constraints MigrationConstraints, cluster *proxmox.Cluster, targetNodesMap map[string]*proxmox.Node, plannedMigrations map[string]string) (string, float64, string, *MigrationDetails) {
	type candidate struct {
		name           string
		score          float64
		reason         string
		stateBefore    NodeState
		stateAfter     NodeState
		breakdown      ScoreBreakdown
		rejected       bool
		rejectReason   string
		rawCPUPriority int // Absolute CPU priority (higher = newer, e.g., 500 for 4th gen Scalable)
	}

	var allCandidates []candidate
	var constraintsApplied []string

	// Track which constraints are being checked
	constraintsApplied = append(constraintsApplied, "RAM capacity check")
	constraintsApplied = append(constraintsApplied, "Storage capacity check")
	constraintsApplied = append(constraintsApplied, "CPU priority: prefer newer CPU generations")
	if constraints.MinRAMFree != nil {
		constraintsApplied = append(constraintsApplied, fmt.Sprintf("Min RAM free: %d GB", *constraints.MinRAMFree/(1024*1024*1024)))
	}
	if constraints.MinCPUFree != nil {
		constraintsApplied = append(constraintsApplied, fmt.Sprintf("Min CPU free: %.0f%%", *constraints.MinCPUFree))
	}
	if constraints.MaxVMsPerHost != nil {
		constraintsApplied = append(constraintsApplied, fmt.Sprintf("Max VMs per host: %d", *constraints.MaxVMsPerHost))
	}
	// Add VM placement constraints to applied constraints list
	if vm.HostCPUModel != "" {
		constraintsApplied = append(constraintsApplied, fmt.Sprintf("Host CPU model must contain: %s", vm.HostCPUModel))
	}
	if len(vm.WithVM) > 0 {
		constraintsApplied = append(constraintsApplied, fmt.Sprintf("Must be with VMs: %s", strings.Join(vm.WithVM, ", ")))
	}
	if len(vm.WithoutVM) > 0 {
		constraintsApplied = append(constraintsApplied, fmt.Sprintf("Cannot be with VMs: %s", strings.Join(vm.WithoutVM, ", ")))
	}

	for name, state := range targetStates {
		cand := candidate{
			name:        name,
			stateBefore: state,
		}

		// Check VM placement constraints (hostcpumodel, withvm, without)
		if targetNode, ok := targetNodesMap[name]; ok {
			placementCheck := CheckVMPlacementConstraints(vm, targetNode, cluster, plannedMigrations)
			if placementCheck.Violated {
				cand.rejected = true
				cand.rejectReason = placementCheck.Reason
				allCandidates = append(allCandidates, cand)
				continue
			}
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

		// Get CPU priority - both raw (for sorting) and normalized (for display)
		cpuPriorityScore := 0.0
		rawCPUPriority := 100 // Default priority
		if targetNode, ok := targetNodesMap[name]; ok {
			rawCPUPriority = GetCPURawPriority(targetNode.CPUModel)
			cpuPriorityInfo := GetClusterCPUPriorities(cluster)
			cpuPriorityScore = GetCPUPriorityScore(targetNode.CPUModel, cpuPriorityInfo)
		}
		cand.rawCPUPriority = rawCPUPriority

		// CPU priority is now the PRIMARY factor - we strongly prefer newer CPUs
		// Use raw priority (200-600 scale) directly for massive differentiation
		// E.g., 4th gen Scalable (500) vs 1st gen (200) = 300 point difference
		// utilization and balance are secondary factors (tiebreakers within same CPU tier)
		totalScore := float64(rawCPUPriority) + utilizationScore*0.1 + balanceScore*0.1

		cand.breakdown = ScoreBreakdown{
			UtilizationScore:  utilizationScore,
			BalanceScore:      balanceScore,
			CPUPriorityScore:  cpuPriorityScore,
			UtilizationWeight: 0.1,
			BalanceWeight:     0.1,
			TotalScore:        totalScore,
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

	// Sort candidates: prefer newer CPUs first (raw CPU priority), then by overall score
	sort.Slice(validCandidates, func(i, j int) bool {
		// First, strongly prefer newer CPUs (higher raw CPU priority)
		// Raw priority values: 1st gen ~200, 2nd gen ~300, 3rd gen ~400, 4th gen ~500, 5th gen ~600
		// Threshold of 50 catches different generations
		cpuPriorityDiff := validCandidates[i].rawCPUPriority - validCandidates[j].rawCPUPriority
		if cpuPriorityDiff > 50 || cpuPriorityDiff < -50 {
			// Significant CPU generation difference - prefer newer
			return cpuPriorityDiff > 0
		}
		// Within same CPU tier, sort by overall score
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

	// Source before state - use ACTUAL node values from Proxmox API
	result.SourceBefore = NewNodeState(sourceNode)

	// For storage, use VM-aggregated values instead of node.UsedDisk
	// This ensures before/after calculations are consistent (both use VM allocated sizes)
	// The node's UsedDisk includes non-VM data (templates, ISOs, backups) which makes
	// the subtraction of VM allocated sizes incorrect
	var totalVMStorage int64
	for _, vm := range sourceNode.VMs {
		storage := vm.MaxDisk
		if storage == 0 {
			storage = vm.UsedDisk
		}
		totalVMStorage += storage
	}
	result.SourceBefore.StorageUsed = totalVMStorage
	if result.SourceBefore.StorageTotal > 0 {
		result.SourceBefore.StoragePercent = float64(totalVMStorage) / float64(result.SourceBefore.StorageTotal) * 100
	}

	// Calculate VM contributions being removed (CPU, RAM, Storage)
	vmCPUContribution := 0.0
	var vmRAMContribution int64
	var vmStorageContribution int64
	removedVCPUs := 0
	for _, vm := range vmsToMigrate {
		// Storage is always counted regardless of power state
		storage := vm.MaxDisk
		if storage == 0 {
			storage = vm.UsedDisk
		}
		vmStorageContribution += storage

		if vm.Status == "running" {
			// HCPU% = vm.CPUUsage * vm.CPUCores / hostCores
			if sourceNode.CPUCores > 0 {
				vmCPUContribution += vm.CPUUsage * float64(vm.CPUCores) / float64(sourceNode.CPUCores)
			}
			vmRAMContribution += vm.MaxMem
			removedVCPUs += vm.CPUCores
		}
	}

	// Source after state - actual values minus VM contributions being removed
	result.SourceAfter = result.SourceBefore
	result.SourceAfter.CPUPercent = result.SourceBefore.CPUPercent - vmCPUContribution
	if result.SourceAfter.CPUPercent < 0 {
		result.SourceAfter.CPUPercent = 0
	}
	result.SourceAfter.RAMUsed = result.SourceBefore.RAMUsed - vmRAMContribution
	if result.SourceAfter.RAMUsed < 0 {
		result.SourceAfter.RAMUsed = 0
	}
	if result.SourceBefore.RAMTotal > 0 {
		result.SourceAfter.RAMPercent = float64(result.SourceAfter.RAMUsed) / float64(result.SourceBefore.RAMTotal) * 100
	}
	result.SourceAfter.StorageUsed = result.SourceBefore.StorageUsed - vmStorageContribution
	if result.SourceAfter.StorageUsed < 0 {
		result.SourceAfter.StorageUsed = 0
	}
	if result.SourceBefore.StorageTotal > 0 {
		result.SourceAfter.StoragePercent = float64(result.SourceAfter.StorageUsed) / float64(result.SourceBefore.StorageTotal) * 100
	}
	result.SourceAfter.VMCount = result.SourceBefore.VMCount - len(vmsToMigrate)
	result.SourceAfter.VCPUs = result.SourceBefore.VCPUs - removedVCPUs

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
		// Use ACTUAL node values from Proxmox API for "Before"
		beforeState := NewNodeState(&target)
		result.TargetsBefore[target.Name] = beforeState

		// Calculate "After" by adding VM contributions (CPU, RAM, Storage)
		addVMs := targetVMs[target.Name]
		afterState := beforeState

		var addedCPU float64
		var addedRAM int64
		var addedStorage int64
		addedVCPUs := 0
		for _, vm := range addVMs {
			// Storage is always counted regardless of power state
			storage := vm.MaxDisk
			if storage == 0 {
				storage = vm.UsedDisk
			}
			addedStorage += storage

			if vm.Status == "running" {
				// HCPU% contribution on target = vm.CPUUsage * vm.CPUCores / targetCores
				if target.CPUCores > 0 {
					addedCPU += vm.CPUUsage * float64(vm.CPUCores) / float64(target.CPUCores)
				}
				addedRAM += vm.MaxMem
				addedVCPUs += vm.CPUCores
			}
		}

		afterState.CPUPercent = beforeState.CPUPercent + addedCPU
		afterState.RAMUsed = beforeState.RAMUsed + addedRAM
		if beforeState.RAMTotal > 0 {
			afterState.RAMPercent = float64(afterState.RAMUsed) / float64(beforeState.RAMTotal) * 100
		}
		afterState.StorageUsed = beforeState.StorageUsed + addedStorage
		if beforeState.StorageTotal > 0 {
			afterState.StoragePercent = float64(afterState.StorageUsed) / float64(beforeState.StorageTotal) * 100
		}
		afterState.VMCount = beforeState.VMCount + len(addVMs)
		afterState.VCPUs = beforeState.VCPUs + addedVCPUs

		result.TargetsAfter[target.Name] = afterState
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
	VCPUPercent    float64 // vCPUs / threads * 100 (e.g., 400% means 4x overcommit)
}

// CalculateTargetAverages calculates what the cluster average should be after
// all VMs from source are distributed (excluding the source node from calculation)
func CalculateTargetAverages(cluster *proxmox.Cluster, sourceNode string, vmsToMigrate []proxmox.VM) ClusterAverages {
	// Calculate total resources of all target nodes (excluding source)
	var totalCPUCores int
	var totalCPUUsage float64
	var totalRAM, usedRAM int64
	var totalStorage, usedStorage int64
	var totalVCPUs int // Total vCPUs allocated across all target nodes
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
		// Count existing vCPUs on target nodes
		for _, vm := range node.VMs {
			totalVCPUs += vm.CPUCores
		}
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
		// Add vCPUs from migrating VMs
		totalVCPUs += vm.CPUCores
	}

	// Calculate target averages
	averages := ClusterAverages{}
	if totalCPUCores > 0 {
		averages.CPUPercent = (totalCPUUsage / float64(totalCPUCores)) * 100
		// vCPU% = total vCPUs / total threads * 100
		// e.g., 1200 vCPUs / 300 threads = 400%
		averages.VCPUPercent = float64(totalVCPUs) / float64(totalCPUCores) * 100
	}
	if totalRAM > 0 {
		averages.RAMPercent = float64(usedRAM) / float64(totalRAM) * 100
	}
	if totalStorage > 0 {
		averages.StoragePercent = float64(usedStorage) / float64(totalStorage) * 100
	}

	log.Printf("CalculateTargetAverages: totalVCPUs=%d, totalCPUCores=%d, VCPUPercent=%.1f%%",
		totalVCPUs, totalCPUCores, averages.VCPUPercent)

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
	targetNodesMap := make(map[string]*proxmox.Node)

	for i := range targets {
		target := &targets[i]
		targetStates[target.Name] = NewNodeState(target)
		targetCoresMap[target.Name] = target.CPUCores
		vmsPerTarget[target.Name] = len(target.VMs)
		targetNodesMap[target.Name] = target
	}

	// Source node cores for HCPU% calculation
	sourceCores := 0
	if sourceNode != nil {
		sourceCores = sourceNode.CPUCores
	}

	suggestions := make([]MigrationSuggestion, 0, len(vms))

	// Track planned migrations for constraint checking (VM name -> target node name)
	plannedMigrations := make(map[string]string)

	// Process VMs sequentially to ensure accurate state tracking and ALL VMs get assigned
	// VMs are already sorted by size (largest first) from selectAllVMs
	for _, vm := range vms {
		// Find best target for this VM
		targetName, score, reason, details := findBestTargetForMigrateAll(vm, targetStates, vmsPerTarget, targetAverages, constraints, cluster, targetNodesMap, plannedMigrations)

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
			// Track this migration for constraint checking of subsequent VMs
			plannedMigrations[vm.Name] = targetName
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
func findBestTargetForMigrateAll(vm proxmox.VM, targetStates map[string]NodeState, vmsPerTarget map[string]int, averages ClusterAverages, constraints MigrationConstraints, cluster *proxmox.Cluster, targetNodesMap map[string]*proxmox.Node, plannedMigrations map[string]string) (string, float64, string, *MigrationDetails) {
	type candidate struct {
		name           string
		score          float64
		reason         string
		belowAverage   bool
		stateBefore    NodeState
		stateAfter     NodeState
		breakdown      ScoreBreakdown
		rejected       bool
		rejectReason   string
		rawCPUPriority int // Absolute CPU priority (higher = newer, e.g., 500 for 4th gen Scalable)
	}

	var allCandidates []candidate
	var constraintsApplied []string

	constraintsApplied = append(constraintsApplied, "RAM capacity check")
	constraintsApplied = append(constraintsApplied, "Storage capacity check")
	constraintsApplied = append(constraintsApplied, fmt.Sprintf("Cluster balance target (CPU: %.1f%%, RAM: %.1f%%, vCPU: %.1f%%)", averages.CPUPercent, averages.RAMPercent, averages.VCPUPercent))
	constraintsApplied = append(constraintsApplied, "CPU priority: prefer newer CPU generations")
	if constraints.MaxVMsPerHost != nil {
		constraintsApplied = append(constraintsApplied, fmt.Sprintf("Max VMs per host: %d", *constraints.MaxVMsPerHost))
	}

	// Add VM placement constraints to applied constraints list
	if vm.HostCPUModel != "" {
		constraintsApplied = append(constraintsApplied, fmt.Sprintf("Host CPU model must contain: %s", vm.HostCPUModel))
	}
	if len(vm.WithVM) > 0 {
		constraintsApplied = append(constraintsApplied, fmt.Sprintf("Must be with VMs: %s", strings.Join(vm.WithVM, ", ")))
	}
	if len(vm.WithoutVM) > 0 {
		constraintsApplied = append(constraintsApplied, fmt.Sprintf("Cannot be with VMs: %s", strings.Join(vm.WithoutVM, ", ")))
	}

	// Evaluate all targets
	for name, state := range targetStates {
		cand := candidate{
			name:        name,
			stateBefore: state,
		}

		// Check VM placement constraints (hostcpumodel, withvm, without)
		if targetNode, ok := targetNodesMap[name]; ok {
			placementCheck := CheckVMPlacementConstraints(vm, targetNode, cluster, plannedMigrations)
			if placementCheck.Violated {
				cand.rejected = true
				cand.rejectReason = placementCheck.Reason
				allCandidates = append(allCandidates, cand)
				continue
			}
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

		// Calculate vCPU% after adding this VM
		// vCPU% = (current vCPUs + VM vCPUs) / host threads * 100
		newVCPUPercent := 0.0
		if state.CPUCores > 0 {
			newVCPUPercent = float64(state.VCPUs+vm.CPUCores) / float64(state.CPUCores) * 100
		}

		// Check if this target stays below cluster average (CPU%, RAM%, and vCPU%)
		margin := 5.0 // 5% margin
		belowAverage := newState.CPUPercent <= averages.CPUPercent+margin &&
			newState.RAMPercent <= averages.RAMPercent+margin &&
			newVCPUPercent <= averages.VCPUPercent+margin
		cand.belowAverage = belowAverage

		// Calculate detailed score breakdown (include vCPU% headroom and CPU priority)
		cpuHeadroom := averages.CPUPercent - newState.CPUPercent
		ramHeadroom := averages.RAMPercent - newState.RAMPercent
		vcpuHeadroom := averages.VCPUPercent - newVCPUPercent
		headroomScore := cpuHeadroom*0.3 + ramHeadroom*0.4 + vcpuHeadroom*0.1
		balanceScore := calculateBalanceScoreDetailed(newState)

		// Get CPU priority - both raw (for sorting) and normalized (for display)
		cpuPriorityScore := 0.0
		rawCPUPriority := 100 // Default priority
		if targetNode, ok := targetNodesMap[name]; ok {
			rawCPUPriority = GetCPURawPriority(targetNode.CPUModel)
			cpuPriorityInfo := GetClusterCPUPriorities(cluster)
			cpuPriorityScore = GetCPUPriorityScore(targetNode.CPUModel, cpuPriorityInfo)
		}
		cand.rawCPUPriority = rawCPUPriority

		// CPU priority is now the PRIMARY factor - we strongly prefer newer CPUs
		// Use raw priority (200-600 scale) directly for massive differentiation
		// E.g., 4th gen Scalable (500) vs 1st gen (200) = 300 point difference
		// headroom and balance are secondary factors (tiebreakers within same CPU tier)
		totalScore := float64(rawCPUPriority) + headroomScore*0.1 + balanceScore*0.1

		cand.breakdown = ScoreBreakdown{
			HeadroomScore:     headroomScore,
			BalanceScore:      balanceScore,
			CPUPriorityScore:  cpuPriorityScore,
			UtilizationWeight: 0.0, // Not used in MigrateAll mode
			BalanceWeight:     0.2,
			TotalScore:        totalScore,
		}
		cand.score = cand.breakdown.TotalScore

		if belowAverage {
			cand.reason = fmt.Sprintf("Balanced (CPU: %.1f%%, RAM: %.1f%%, vCPU: %.0f%%)", newState.CPUPercent, newState.RAMPercent, newVCPUPercent)
		} else {
			cand.reason = fmt.Sprintf("Best available (CPU: %.1f%%, RAM: %.1f%%, vCPU: %.0f%%)", newState.CPUPercent, newState.RAMPercent, newVCPUPercent)
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

	// Sort candidates: prefer newer CPUs first (raw CPU priority), then balance
	// We use CPU priority as the primary sort key to ensure newer hardware is filled first
	sort.Slice(validCandidates, func(i, j int) bool {
		// First, strongly prefer newer CPUs (higher raw CPU priority)
		// Raw priority values: 1st gen ~200, 2nd gen ~300, 3rd gen ~400, 4th gen ~500, 5th gen ~600
		// Threshold of 50 catches different generations
		cpuPriorityDiff := validCandidates[i].rawCPUPriority - validCandidates[j].rawCPUPriority
		if cpuPriorityDiff > 50 || cpuPriorityDiff < -50 {
			// Significant CPU generation difference - prefer newer
			return cpuPriorityDiff > 0
		}
		// Within same CPU tier (within 50 priority points), prefer below-average hosts
		if validCandidates[i].belowAverage != validCandidates[j].belowAverage {
			return validCandidates[i].belowAverage // below-average first
		}
		// Then by overall score (which includes headroom and balance)
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
