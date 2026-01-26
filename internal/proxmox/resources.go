package proxmox

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

// Maximum concurrent node status fetches
const maxConcurrentFetches = 32

// ProgressCallback is called to report progress during data collection
// stage: current stage name (e.g., "resources", "nodes", "storage")
// current: current item being processed
// total: total items to process
type ProgressCallback func(stage string, current, total int)

// CollectClusterData gathers complete cluster information
func CollectClusterData(client ProxmoxClient) (*Cluster, error) {
	return CollectClusterDataWithProgress(client, nil)
}

// CollectClusterDataWithProgress gathers complete cluster information with progress reporting
func CollectClusterDataWithProgress(client ProxmoxClient, progress ProgressCallback) (*Cluster, error) {
	// Report initial stage
	if progress != nil {
		progress("Fetching cluster resources", 0, 1)
	}

	// Get all cluster resources
	resources, err := client.GetClusterResources()
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster resources: %w", err)
	}

	if progress != nil {
		progress("Processing resources", 1, 1)
	}

	// Build cluster structure
	cluster := &Cluster{
		Nodes: []Node{},
	}

	// Map to organize data
	nodeMap := make(map[string]*Node)
	vmList := []VM{}

	// Track storage per node (aggregated from storage type resources)
	nodeStorage := make(map[string]struct {
		maxDisk  int64
		usedDisk int64
	})

	// Process resources
	for _, res := range resources {
		switch res.Type {
		case "node":
			node := Node{
				Name:     res.Node,
				Status:   res.Status,
				CPUCores: res.MaxCPU,
				CPUUsage: res.CPU,
				MaxMem:   res.MaxMem,
				UsedMem:  res.Mem,
				MaxDisk:  res.MaxDisk, // This is just rootfs, will be updated
				UsedDisk: res.Disk,    // This is just rootfs, will be updated
				Uptime:   res.Uptime,
				VMs:      []VM{},
			}
			nodeMap[res.Node] = &node

		case "storage":
			// Only count storage that starts with "kv" or "local"
			if !strings.HasPrefix(res.Storage, "kv") && !strings.HasPrefix(res.Storage, "local") {
				continue
			}
			// Aggregate storage from matching storage resources per node
			storage := nodeStorage[res.Node]
			storage.maxDisk += res.MaxDisk
			storage.usedDisk += res.Disk
			nodeStorage[res.Node] = storage

		case "qemu", "lxc":
			// Skip templates
			if res.Template == 1 {
				continue
			}

			vm := VM{
				VMID:     res.VMID,
				Name:     res.Name,
				Node:     res.Node,
				Status:   res.Status,
				Type:     res.Type,
				CPUCores: res.MaxCPU,
				CPUUsage: res.CPU * 100, // Convert to percentage
				MaxMem:   res.MaxMem,
				UsedMem:  res.Mem,
				MaxDisk:  res.MaxDisk,
				UsedDisk: res.Disk,
				Uptime:   res.Uptime,
			}
			vmList = append(vmList, vm)
			cluster.TotalVMs++
		}
	}

	// Update node storage with aggregated values from storage resources
	for nodeName, storage := range nodeStorage {
		if node, exists := nodeMap[nodeName]; exists {
			// Use storage resource totals if available (more accurate than rootfs only)
			if storage.maxDisk > 0 {
				node.MaxDisk = storage.maxDisk
				node.UsedDisk = storage.usedDisk
			}
		}
	}

	// Fetch detailed node status for each node in parallel (CPU model, sockets, MHz, PVE version)
	// Use a worker pool with limited concurrency for large clusters
	fetchNodeDetails(client, nodeMap, progress)

	// Assign VMs to their nodes
	for _, vm := range vmList {
		if node, exists := nodeMap[vm.Node]; exists {
			node.VMs = append(node.VMs, vm)
		}
	}

	// Convert map to slice and calculate totals
	for _, node := range nodeMap {
		cluster.Nodes = append(cluster.Nodes, *node)
		cluster.TotalCPUs += node.CPUCores
		cluster.TotalRAM += node.MaxMem
		cluster.TotalStorage += node.MaxDisk
		cluster.UsedStorage += node.UsedDisk

		// Count vCPUs and VM states
		for _, vm := range node.VMs {
			cluster.TotalVCPUs += vm.CPUCores
			if vm.Status == "running" {
				cluster.RunningVMs++
			} else {
				cluster.StoppedVMs++
			}
		}
	}

	// Sort nodes by name for consistent ordering
	sort.Slice(cluster.Nodes, func(i, j int) bool {
		return cluster.Nodes[i].Name < cluster.Nodes[j].Name
	})

	return cluster, nil
}

// GetNodeVMs retrieves all VMs for a specific node
func GetNodeVMs(cluster *Cluster, nodeName string) []VM {
	for _, node := range cluster.Nodes {
		if node.Name == nodeName {
			return node.VMs
		}
	}
	return []VM{}
}

// GetNodeByName finds a node by name
func GetNodeByName(cluster *Cluster, nodeName string) *Node {
	for i := range cluster.Nodes {
		if cluster.Nodes[i].Name == nodeName {
			return &cluster.Nodes[i]
		}
	}
	return nil
}

// GetVMByID finds a VM by its ID across all nodes
func GetVMByID(cluster *Cluster, vmid int) *VM {
	for _, node := range cluster.Nodes {
		for i := range node.VMs {
			if node.VMs[i].VMID == vmid {
				return &node.VMs[i]
			}
		}
	}
	return nil
}

// CalculateUtilization calculates resource utilization for each node
func CalculateUtilization(cluster *Cluster) map[string]map[string]float64 {
	utilization := make(map[string]map[string]float64)

	for _, node := range cluster.Nodes {
		utilization[node.Name] = map[string]float64{
			"cpu":     node.GetCPUPercent(),
			"memory":  node.GetMemPercent(),
			"storage": node.GetDiskPercent(),
		}
	}

	return utilization
}

// GetClusterSummary returns summary statistics for the cluster
func GetClusterSummary(cluster *Cluster) map[string]interface{} {
	totalCPUUsage := 0.0
	totalMemUsage := int64(0)
	totalMemMax := int64(0)
	totalStorageUsage := int64(0)
	totalStorageMax := int64(0)
	onlineNodes := 0

	for _, node := range cluster.Nodes {
		if node.Status == "online" {
			onlineNodes++
		}
		totalCPUUsage += node.CPUUsage
		totalMemUsage += node.UsedMem
		totalMemMax += node.MaxMem
		totalStorageUsage += node.UsedDisk
		totalStorageMax += node.MaxDisk
	}

	avgCPU := 0.0
	if len(cluster.Nodes) > 0 {
		avgCPU = (totalCPUUsage / float64(len(cluster.Nodes))) * 100
	}

	memPercent := 0.0
	if totalMemMax > 0 {
		memPercent = float64(totalMemUsage) / float64(totalMemMax) * 100
	}

	storagePercent := 0.0
	if totalStorageMax > 0 {
		storagePercent = float64(totalStorageUsage) / float64(totalStorageMax) * 100
	}

	return map[string]interface{}{
		"total_nodes":     len(cluster.Nodes),
		"online_nodes":    onlineNodes,
		"total_vms":       cluster.TotalVMs,
		"avg_cpu_percent": avgCPU,
		"mem_percent":     memPercent,
		"storage_percent": storagePercent,
		"total_memory":    totalMemMax,
		"used_memory":     totalMemUsage,
		"total_storage":   totalStorageMax,
		"used_storage":    totalStorageUsage,
	}
}

// GetAvailableTargets returns nodes that can accept migrations (excluding source and offline nodes)
func GetAvailableTargets(cluster *Cluster, sourceNode string, excludeNodes []string) []Node {
	var targets []Node

	excludeMap := make(map[string]bool)
	excludeMap[sourceNode] = true
	for _, node := range excludeNodes {
		excludeMap[node] = true
	}

	for _, node := range cluster.Nodes {
		if excludeMap[node.Name] {
			continue
		}
		if node.Status != "online" {
			continue
		}
		targets = append(targets, node)
	}

	return targets
}

// nodeStatusResult holds the result of fetching node status
type nodeStatusResult struct {
	nodeName string
	status   *NodeStatus
	err      error
}

// fetchNodeDetails fetches detailed status for all online nodes in parallel
// Uses a worker pool with limited concurrency (maxConcurrentFetches)
func fetchNodeDetails(client ProxmoxClient, nodeMap map[string]*Node, progress ProgressCallback) {
	// Collect online nodes that need fetching
	var onlineNodes []string
	for nodeName, node := range nodeMap {
		if node.Status == "online" {
			onlineNodes = append(onlineNodes, nodeName)
		}
	}

	if len(onlineNodes) == 0 {
		return
	}

	totalNodes := len(onlineNodes)
	var completed int32 = 0

	// Report initial progress
	if progress != nil {
		progress("Fetching node details", 0, totalNodes)
	}

	// Create channels for work distribution and results
	jobs := make(chan string, len(onlineNodes))
	results := make(chan nodeStatusResult, len(onlineNodes))

	// Determine number of workers (min of maxConcurrentFetches and number of nodes)
	numWorkers := maxConcurrentFetches
	if len(onlineNodes) < numWorkers {
		numWorkers = len(onlineNodes)
	}

	// Start worker goroutines
	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for nodeName := range jobs {
				status, err := client.GetNodeStatus(nodeName)
				results <- nodeStatusResult{
					nodeName: nodeName,
					status:   status,
					err:      err,
				}
			}
		}()
	}

	// Send jobs to workers
	for _, nodeName := range onlineNodes {
		jobs <- nodeName
	}
	close(jobs)

	// Wait for all workers to complete in a separate goroutine
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results and update node map
	for result := range results {
		// Update progress
		current := int(atomic.AddInt32(&completed, 1))
		if progress != nil {
			progress("Fetching node details", current, totalNodes)
		}

		if result.err == nil && result.status != nil {
			if node, exists := nodeMap[result.nodeName]; exists {
				node.CPUModel = result.status.CPUInfo.Model
				node.CPUSockets = result.status.CPUInfo.Sockets
				node.CPUMHz = result.status.CPUInfo.MHz
				node.LoadAverage = result.status.LoadAverage
				// CPUCores from resources is total logical CPUs
				// If we got more detailed info, we can verify/update
				if result.status.CPUInfo.CPUs > 0 {
					node.CPUCores = result.status.CPUInfo.CPUs
				}
			}
		}
	}
}
