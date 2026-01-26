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
	return NodeState{
		Name:           node.Name,
		VMCount:        len(node.VMs),
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
		newState.CPUUsageTotal -= vm.CPUUsage / 100 // Convert back from percentage
		newState.RAMUsed -= vm.UsedMem
		newState.StorageUsed -= vm.UsedDisk
	}

	// Add VMs
	for _, vm := range addVMs {
		newState.VMCount++
		newState.CPUUsageTotal += vm.CPUUsage / 100
		newState.RAMUsed += vm.UsedMem
		newState.StorageUsed += vm.UsedDisk
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
