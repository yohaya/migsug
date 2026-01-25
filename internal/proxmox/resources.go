package proxmox

import (
	"fmt"
	"sort"
)

// CollectClusterData gathers complete cluster information
func CollectClusterData(client ProxmoxClient) (*Cluster, error) {
	// Get all cluster resources
	resources, err := client.GetClusterResources()
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster resources: %w", err)
	}

	// Build cluster structure
	cluster := &Cluster{
		Nodes: []Node{},
	}

	// Map to organize data
	nodeMap := make(map[string]*Node)
	vmList := []VM{}

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
				MaxDisk:  res.MaxDisk,
				UsedDisk: res.Disk,
				Uptime:   res.Uptime,
				VMs:      []VM{},
			}
			nodeMap[res.Node] = &node

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

	// Assign VMs to their nodes
	for _, vm := range vmList {
		if node, exists := nodeMap[vm.Node]; exists {
			node.VMs = append(node.VMs, vm)
		}
	}

	// Convert map to slice
	for _, node := range nodeMap {
		cluster.Nodes = append(cluster.Nodes, *node)
		cluster.TotalCPUs += node.CPUCores
		cluster.TotalRAM += node.MaxMem
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
