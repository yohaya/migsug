package analyzer

import "github.com/yourusername/migsug/internal/proxmox"

// MigrationSuggestion represents a single VM migration recommendation
type MigrationSuggestion struct {
	VMID       int
	VMName     string
	SourceNode string
	TargetNode string
	Reason     string
	Score      float64 // Target selection score
	Status     string  // VM status: "running" or "stopped"

	// VM resources
	VCPUs    int
	CPUUsage float64
	RAM      int64
	Storage  int64
}

// AnalysisResult contains the complete analysis output
type AnalysisResult struct {
	Suggestions   []MigrationSuggestion
	SourceBefore  NodeState
	SourceAfter   NodeState
	TargetsBefore map[string]NodeState
	TargetsAfter  map[string]NodeState

	// Summary
	TotalVMs        int
	TotalVCPUs      int
	TotalRAM        int64
	TotalStorage    int64
	ImprovementInfo string
}

// NodeState represents the state of a node before or after migration
type NodeState struct {
	Name           string
	VMCount        int
	VCPUs          int // Total vCPUs allocated to VMs
	CPUCores       int
	CPUUsageTotal  float64 // Total CPU usage value
	CPUPercent     float64 // CPU usage as percentage
	RAMUsed        int64
	RAMTotal       int64
	RAMPercent     float64
	StorageUsed    int64
	StorageTotal   int64
	StoragePercent float64
}

// NewNodeState creates a NodeState from a proxmox.Node
func NewNodeState(node *proxmox.Node) NodeState {
	// Calculate total vCPUs from running VMs
	totalVCPUs := 0
	for _, vm := range node.VMs {
		if vm.Status == "running" {
			totalVCPUs += vm.CPUCores
		}
	}

	return NodeState{
		Name:           node.Name,
		VMCount:        len(node.VMs),
		VCPUs:          totalVCPUs,
		CPUCores:       node.CPUCores,
		CPUUsageTotal:  node.CPUUsage,
		CPUPercent:     node.GetCPUPercent(),
		RAMUsed:        node.UsedMem,
		RAMTotal:       node.MaxMem,
		RAMPercent:     node.GetMemPercent(),
		StorageUsed:    node.UsedDisk,
		StorageTotal:   node.MaxDisk,
		StoragePercent: node.GetDiskPercent(),
	}
}

// CalculateAfterMigration computes node state after migrations
func (ns NodeState) CalculateAfterMigration(addVMs []proxmox.VM, removeVMs []proxmox.VM) NodeState {
	newState := ns

	// Remove VMs
	for _, vm := range removeVMs {
		newState.VMCount--
		// Only subtract vCPUs/CPU for running VMs (stopped VMs don't contribute)
		if vm.Status == "running" {
			newState.VCPUs -= vm.CPUCores
			newState.CPUUsageTotal -= vm.CPUUsage / 100 // Convert back from percentage
		}
		newState.RAMUsed -= vm.UsedMem
		newState.StorageUsed -= vm.UsedDisk
	}

	// Add VMs
	for _, vm := range addVMs {
		newState.VMCount++
		// Only add vCPUs/CPU for running VMs
		if vm.Status == "running" {
			newState.VCPUs += vm.CPUCores
			newState.CPUUsageTotal += vm.CPUUsage / 100
		}
		newState.RAMUsed += vm.UsedMem
		newState.StorageUsed += vm.UsedDisk
	}

	// Ensure values don't go negative
	if newState.VMCount < 0 {
		newState.VMCount = 0
	}
	if newState.VCPUs < 0 {
		newState.VCPUs = 0
	}
	if newState.CPUUsageTotal < 0 {
		newState.CPUUsageTotal = 0
	}
	if newState.RAMUsed < 0 {
		newState.RAMUsed = 0
	}
	if newState.StorageUsed < 0 {
		newState.StorageUsed = 0
	}

	// When all VMs are migrated away, show 0 for VM-related resources
	if newState.VMCount == 0 {
		newState.VCPUs = 0
		newState.CPUUsageTotal = 0
		newState.RAMUsed = 0
		newState.StorageUsed = 0
	}

	// Recalculate percentages
	if newState.CPUCores > 0 {
		newState.CPUPercent = (newState.CPUUsageTotal / float64(newState.CPUCores)) * 100
	}
	if newState.RAMTotal > 0 {
		newState.RAMPercent = float64(newState.RAMUsed) / float64(newState.RAMTotal) * 100
	}
	if newState.StorageTotal > 0 {
		newState.StoragePercent = float64(newState.StorageUsed) / float64(newState.StorageTotal) * 100
	}

	// Ensure percentages don't go negative
	if newState.CPUPercent < 0 {
		newState.CPUPercent = 0
	}
	if newState.RAMPercent < 0 {
		newState.RAMPercent = 0
	}
	if newState.StoragePercent < 0 {
		newState.StoragePercent = 0
	}

	return newState
}

// HasCapacity checks if the node has capacity for a VM
func (ns NodeState) HasCapacity(vm proxmox.VM, constraints MigrationConstraints) bool {
	// Check RAM capacity
	newRAMUsed := ns.RAMUsed + vm.UsedMem
	if newRAMUsed > ns.RAMTotal {
		return false
	}

	// Check storage capacity
	newStorageUsed := ns.StorageUsed + vm.UsedDisk
	if newStorageUsed > ns.StorageTotal {
		return false
	}

	// Check minimum free requirements
	if constraints.MinRAMFree != nil {
		ramFree := ns.RAMTotal - newRAMUsed
		if ramFree < *constraints.MinRAMFree {
			return false
		}
	}

	if constraints.MinCPUFree != nil {
		newCPUUsage := ns.CPUUsageTotal + (vm.CPUUsage / 100)
		if ns.CPUCores > 0 {
			cpuPercent := (newCPUUsage / float64(ns.CPUCores)) * 100
			cpuFree := 100 - cpuPercent
			if cpuFree < *constraints.MinCPUFree {
				return false
			}
		}
	}

	return true
}

// GetUtilizationScore returns a score representing overall utilization (lower is better for targets)
func (ns NodeState) GetUtilizationScore() float64 {
	// Weighted score based on resource utilization
	// Lower score = more available resources = better target
	cpuWeight := 0.4
	ramWeight := 0.4
	storageWeight := 0.2

	score := (cpuWeight * ns.CPUPercent) +
		(ramWeight * ns.RAMPercent) +
		(storageWeight * ns.StoragePercent)

	return score
}
