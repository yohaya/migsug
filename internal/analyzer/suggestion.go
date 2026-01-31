package analyzer

import (
	"fmt"

	"github.com/yourusername/migsug/internal/proxmox"
)

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

	// Host info for CPU% calculations
	SourceCores int // Source host's CPU cores/threads
	TargetCores int // Target host's CPU cores/threads

	// Detailed migration reasoning
	Details *MigrationDetails
}

// MigrationDetails contains comprehensive reasoning for why a VM was migrated to a specific host
type MigrationDetails struct {
	// VM Selection Info
	SelectionMode   string // "all", "vm_count", "vcpu", "cpu_usage", "ram", "storage", "specific"
	SelectionReason string // Why this VM was selected for migration

	// Target Selection Scoring
	ScoreBreakdown ScoreBreakdown

	// Target state comparison
	TargetBefore ResourceState // Target node state before this VM
	TargetAfter  ResourceState // Target node state after adding this VM

	// Cluster context (for MigrateAll mode)
	ClusterAvgCPU float64 // Cluster average CPU%
	ClusterAvgRAM float64 // Cluster average RAM%
	BelowAverage  bool    // Is target below cluster average after migration?

	// Alternative targets considered
	Alternatives []AlternativeTarget

	// Constraints applied
	ConstraintsApplied []string // List of constraints that were checked
}

// ScoreBreakdown shows how the target selection score was calculated
type ScoreBreakdown struct {
	TotalScore       float64 // Final combined score
	UtilizationScore float64 // Score based on resource utilization (lower util = higher score)
	BalanceScore     float64 // Score based on resource balance (more balanced = higher score)
	HeadroomScore    float64 // Score based on headroom below cluster average (MigrateAll only)
	CPUPriorityScore float64 // Score based on CPU generation (newer CPU = higher score, 0-100 normalized)

	// Weights used
	UtilizationWeight float64
	BalanceWeight     float64
}

// ResourceState captures resource utilization at a point in time
type ResourceState struct {
	CPUPercent     float64
	RAMPercent     float64
	StoragePercent float64
	VMCount        int
	VCPUs          int
	RAMUsed        int64
	RAMTotal       int64
	StorageUsed    int64
	StorageTotal   int64
}

// AlternativeTarget represents a target that was considered but not chosen
type AlternativeTarget struct {
	Name            string
	Score           float64
	RejectionReason string // Why this target wasn't chosen
	CPUAfter        float64
	RAMAfter        float64
	StorageAfter    float64
}

// UnmigrateableVM represents a VM that could not be migrated and the reason why
type UnmigrateableVM struct {
	VMID    int
	VMName  string
	Status  string // running or stopped
	VCPUs   int
	RAM     int64
	Storage int64
	Reason  string // Why the VM cannot be migrated
}

// AnalysisResult contains the complete analysis output
type AnalysisResult struct {
	Suggestions      []MigrationSuggestion
	UnmigrateableVMs []UnmigrateableVM // VMs that could not be migrated
	SourceBefore     NodeState
	SourceAfter      NodeState
	TargetsBefore    map[string]NodeState
	TargetsAfter     map[string]NodeState

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

// NewNodeStateFromVMs creates a NodeState with VM-aggregated values (not node values)
// This is used for Migration Impact to show only VM resource totals
func NewNodeStateFromVMs(node *proxmox.Node) NodeState {
	// Calculate totals from VMs only
	totalVCPUs := 0
	totalCPUUsage := 0.0
	var totalRAM int64
	var totalStorage int64

	for _, vm := range node.VMs {
		if vm.Status == "running" {
			totalVCPUs += vm.CPUCores
			totalCPUUsage += vm.CPUUsage * float64(vm.CPUCores) / 100 // Weighted CPU usage
			totalRAM += vm.MaxMem                                     // Only count RAM for running VMs
		}
		// Storage is always counted (disk space is used regardless of power state)
		storage := vm.MaxDisk
		if storage == 0 {
			storage = vm.UsedDisk
		}
		totalStorage += storage
	}

	// Calculate percentages based on node capacity
	cpuPercent := 0.0
	if node.CPUCores > 0 {
		cpuPercent = (totalCPUUsage / float64(node.CPUCores)) * 100
	}
	ramPercent := 0.0
	if node.MaxMem > 0 {
		ramPercent = float64(totalRAM) / float64(node.MaxMem) * 100
	}
	storagePercent := 0.0
	if node.MaxDisk > 0 {
		storagePercent = float64(totalStorage) / float64(node.MaxDisk) * 100
	}

	return NodeState{
		Name:           node.Name,
		VMCount:        len(node.VMs),
		VCPUs:          totalVCPUs,
		CPUCores:       node.CPUCores,
		CPUUsageTotal:  totalCPUUsage,
		CPUPercent:     cpuPercent,
		RAMUsed:        totalRAM,
		RAMTotal:       node.MaxMem,
		RAMPercent:     ramPercent,
		StorageUsed:    totalStorage,
		StorageTotal:   node.MaxDisk,
		StoragePercent: storagePercent,
	}
}

// CalculateAfterMigration computes node state after migrations
// Uses VM-aggregated values (MaxMem, MaxDisk) consistent with NewNodeStateFromVMs
func (ns NodeState) CalculateAfterMigration(addVMs []proxmox.VM, removeVMs []proxmox.VM) NodeState {
	newState := ns

	// Remove VMs
	for _, vm := range removeVMs {
		newState.VMCount--
		// Only subtract vCPUs/CPU/RAM for running VMs (stopped VMs don't contribute to usage)
		if vm.Status == "running" {
			newState.VCPUs -= vm.CPUCores
			// Weighted CPU usage: vm.CPUUsage * vCPUs / 100
			newState.CPUUsageTotal -= vm.CPUUsage * float64(vm.CPUCores) / 100
			newState.RAMUsed -= vm.MaxMem // Only count RAM for running VMs
		}
		// Storage is always counted (disk space is used regardless of power state)
		storage := vm.MaxDisk
		if storage == 0 {
			storage = vm.UsedDisk
		}
		newState.StorageUsed -= storage
	}

	// Add VMs
	for _, vm := range addVMs {
		newState.VMCount++
		// Only add vCPUs/CPU/RAM for running VMs (stopped VMs don't contribute to usage)
		if vm.Status == "running" {
			newState.VCPUs += vm.CPUCores
			// Weighted CPU usage: vm.CPUUsage * vCPUs / 100
			newState.CPUUsageTotal += vm.CPUUsage * float64(vm.CPUCores) / 100
			newState.RAMUsed += vm.MaxMem // Only count RAM for running VMs
		}
		// Storage is always counted (disk space is used regardless of power state)
		storage := vm.MaxDisk
		if storage == 0 {
			storage = vm.UsedDisk
		}
		newState.StorageUsed += storage
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
// Uses MaxMem for RAM check to ensure there's room to power on the VM
func (ns NodeState) HasCapacity(vm proxmox.VM, constraints MigrationConstraints) bool {
	// Check RAM capacity - use MaxMem to ensure there's room to power on the VM
	// Even if VM is currently stopped, we need capacity for when it's powered on
	newRAMUsed := ns.RAMUsed + vm.MaxMem
	if newRAMUsed > ns.RAMTotal {
		return false
	}

	// Check storage capacity
	storage := vm.MaxDisk
	if storage == 0 {
		storage = vm.UsedDisk
	}
	newStorageUsed := ns.StorageUsed + storage
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

// Storage headroom constants
const (
	// MinStorageHeadroomGiB is the minimum free storage required (500 GiB)
	MinStorageHeadroomGiB = 500
	// LargestVMStorageHeadroomPercent is the percentage of the largest VM's storage to keep free (15%)
	LargestVMStorageHeadroomPercent = 0.15
)

// StorageHeadroomCheck contains the result of a storage headroom check
type StorageHeadroomCheck struct {
	HasSufficientHeadroom bool
	RequiredFreeStorage   int64  // Bytes required to be free
	ActualFreeStorage     int64  // Bytes actually free after migration
	LargestVMStorage      int64  // Storage of largest VM on host after migration
	Reason                string // Explanation if check fails
}

// CheckStorageHeadroom verifies if a target node has sufficient storage headroom after receiving a VM
// The constraint is: host must have at least 500GiB + 15% of largest VM's storage free after migration
func CheckStorageHeadroom(targetNode *proxmox.Node, incomingVM proxmox.VM, currentStorageUsed, storageTotal int64) StorageHeadroomCheck {
	result := StorageHeadroomCheck{}

	// Get storage of the incoming VM
	incomingVMStorage := incomingVM.MaxDisk
	if incomingVMStorage == 0 {
		incomingVMStorage = incomingVM.UsedDisk
	}

	// Find the largest VM's storage on the target (including the incoming VM)
	largestVMStorage := incomingVMStorage
	for _, vm := range targetNode.VMs {
		vmStorage := vm.MaxDisk
		if vmStorage == 0 {
			vmStorage = vm.UsedDisk
		}
		if vmStorage > largestVMStorage {
			largestVMStorage = vmStorage
		}
	}
	result.LargestVMStorage = largestVMStorage

	// Calculate storage used after migration
	storageUsedAfter := currentStorageUsed + incomingVMStorage

	// Calculate required headroom: 500 GiB + 15% of largest VM's storage
	minHeadroomBytes := int64(MinStorageHeadroomGiB) * 1024 * 1024 * 1024
	largestVMHeadroom := int64(float64(largestVMStorage) * LargestVMStorageHeadroomPercent)
	result.RequiredFreeStorage = minHeadroomBytes + largestVMHeadroom

	// Calculate actual free storage after migration
	result.ActualFreeStorage = storageTotal - storageUsedAfter

	// Check if we have enough headroom
	if result.ActualFreeStorage >= result.RequiredFreeStorage {
		result.HasSufficientHeadroom = true
	} else {
		result.HasSufficientHeadroom = false
		requiredGiB := float64(result.RequiredFreeStorage) / (1024 * 1024 * 1024)
		actualGiB := float64(result.ActualFreeStorage) / (1024 * 1024 * 1024)
		result.Reason = fmt.Sprintf("Insufficient storage headroom: need %.0f GiB free (500 + 15%% of %.0f GiB largest VM), only %.0f GiB available",
			requiredGiB, float64(largestVMStorage)/(1024*1024*1024), actualGiB)
	}

	return result
}
