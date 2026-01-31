package proxmox

import (
	"fmt"
	"log"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Maximum concurrent node status fetches
const maxConcurrentFetches = 32

// storageLogger is a dedicated logger for VMs with missing storage info
// This always writes to migsug.log regardless of debug mode
var storageLogger *log.Logger
var storageLogFile *os.File
var storageLogOnce sync.Once

// initStorageLogger initializes the storage logger (called once)
func initStorageLogger() {
	storageLogOnce.Do(func() {
		var err error
		storageLogFile, err = os.OpenFile("migsug.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err != nil {
			// If we can't open the log file, use a no-op logger
			storageLogger = log.New(os.Stderr, "", 0)
			return
		}
		storageLogger = log.New(storageLogFile, "", log.LstdFlags)
	})
}

// logMissingStorage logs VMs with missing storage info to migsug.log
func logMissingStorage(vmid int, name, node, vmType, status string, maxDisk, disk int64) {
	initStorageLogger()
	if storageLogger != nil {
		storageLogger.Printf("VM with missing storage: VMID=%d Name=%s Node=%s Type=%s Status=%s MaxDisk=%d Disk=%d (source: /cluster/resources API)",
			vmid, name, node, vmType, status, maxDisk, disk)
	}
}

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
	// Initialize storage logger and write header
	initStorageLogger()
	if storageLogger != nil {
		storageLogger.Printf("=== Starting cluster data collection ===")
	}

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
	missingStorageCount := 0 // Track VMs with missing storage

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
			// Only count storage that matches kv*storage* pattern
			if !strings.HasPrefix(res.Storage, "kv") || !strings.Contains(res.Storage, "storage") {
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

	// Fetch detailed storage info for VMs with MaxDisk=0
	vmsWithMissingStorage := findVMsWithMissingStorage(vmList)
	if len(vmsWithMissingStorage) > 0 {
		if progress != nil {
			progress("Fetching VM storage details", 0, len(vmsWithMissingStorage))
		}
		fetchVMStorageDetails(client, vmList, vmsWithMissingStorage, progress)
		missingStorageCount = countVMsWithMissingStorage(vmList)
	}

	// Log VMs still missing storage info
	for i := range vmList {
		vm := &vmList[i]
		if vm.MaxDisk == 0 && vm.Status == "running" {
			log.Printf("VM %d (%s) on node %s has MaxDisk=0 after detailed fetch",
				vm.VMID, vm.Name, vm.Node)
			logMissingStorage(vm.VMID, vm.Name, vm.Node, vm.Type, vm.Status, vm.MaxDisk, vm.UsedDisk)
		}
	}

	// Fetch config metadata for all VMs (for nomigrate flag, etc.)
	fetchVMConfigMeta(vmList, progress)

	// Fetch actual disk usage from storage content API (thin provisioning actual size)
	fetchVMDiskUsageFromStorage(client, vmList, progress)

	// Fetch config metadata for all nodes (for allowProvisioning flag, OSD detection, etc.)
	fetchNodeConfigMeta(nodeMap, progress)

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

	// Retry logic for nodes with 0 CPU usage but have running VMs
	// This can happen when the API returns stale data
	retryNodes := findNodesNeedingCPURetry(nodeMap, vmList)
	for retry := 0; retry < 2 && len(retryNodes) > 0; retry++ {
		log.Printf("Retrying CPU data for %d nodes (attempt %d/2): %v", len(retryNodes), retry+1, retryNodes)

		// Wait a short time before retry
		time.Sleep(500 * time.Millisecond)

		// Re-fetch cluster resources
		retryResources, err := client.GetClusterResources()
		if err != nil {
			log.Printf("Retry failed: %v", err)
			break
		}

		// Update CPU usage for problematic nodes
		for _, res := range retryResources {
			if res.Type == "node" {
				if node, exists := nodeMap[res.Node]; exists {
					// Only update if this node needed retry and we got a non-zero value
					for _, retryNode := range retryNodes {
						if retryNode == res.Node && res.CPU > 0 {
							node.CPUUsage = res.CPU
							log.Printf("Updated CPU for %s: %.2f%%", res.Node, res.CPU*100)
							break
						}
					}
				}
			}
		}

		// Check if we still have problematic nodes
		retryNodes = findNodesNeedingCPURetry(nodeMap, vmList)
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

	// Update OSD status for nodes (must be done AFTER VMs are assigned)
	updateNodeOSDStatus(nodeMap)

	// Update recently created VMs status for P-flagged nodes (must be done AFTER VMs are assigned)
	updateNodeOldVMsStatus(nodeMap)

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

	// Log summary of collection
	if storageLogger != nil {
		storageLogger.Printf("=== Collection complete: %d nodes, %d VMs (%d running, %d stopped), %d VMs with missing storage ===",
			len(cluster.Nodes), cluster.TotalVMs, cluster.RunningVMs, cluster.StoppedVMs, missingStorageCount)
	}

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

// GetAvailableTargets returns nodes that can accept migrations
// Excludes: source node, offline nodes, explicitly excluded nodes, and provisioning hosts (P flag)
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
		// Skip provisioning hosts - they should not receive migrated VMs
		if node.AllowProvisioning {
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

// findNodesNeedingCPURetry returns nodes that have 0 CPU usage but have running VMs
// This indicates the API returned stale/incorrect data
func findNodesNeedingCPURetry(nodeMap map[string]*Node, vmList []VM) []string {
	// Count running VMs per node
	runningVMsPerNode := make(map[string]int)
	for _, vm := range vmList {
		if vm.Status == "running" {
			runningVMsPerNode[vm.Node]++
		}
	}

	var retryNodes []string
	for nodeName, node := range nodeMap {
		// Node has 0 CPU usage but has running VMs - likely API error
		if node.Status == "online" && node.CPUUsage == 0 && runningVMsPerNode[nodeName] > 0 {
			retryNodes = append(retryNodes, nodeName)
		}
	}
	return retryNodes
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
				// Populate swap information
				node.SwapTotal = result.status.Swap.Total
				node.SwapUsed = result.status.Swap.Used
				// Populate PVE version
				node.PVEVersion = result.status.PVEVersion
			}
		}
	}
}

// findVMsWithMissingStorage returns indices of VMs that have MaxDisk=0 and are running
func findVMsWithMissingStorage(vmList []VM) []int {
	var indices []int
	for i, vm := range vmList {
		if vm.MaxDisk == 0 && vm.Status == "running" {
			indices = append(indices, i)
		}
	}
	return indices
}

// countVMsWithMissingStorage counts VMs that still have MaxDisk=0
func countVMsWithMissingStorage(vmList []VM) int {
	count := 0
	for _, vm := range vmList {
		if vm.MaxDisk == 0 && vm.Status == "running" {
			count++
		}
	}
	return count
}

// vmStorageResult holds the result of fetching VM storage details
type vmStorageResult struct {
	vmIdx  int
	status *VMStatus
	err    error
}

// fetchVMStorageDetails fetches detailed storage info for VMs with missing data
func fetchVMStorageDetails(client ProxmoxClient, vmList []VM, vmIndices []int, progress ProgressCallback) {
	if len(vmIndices) == 0 {
		return
	}

	var completed int32 = 0
	totalVMs := len(vmIndices)

	// Create channels for work distribution and results
	jobs := make(chan int, len(vmIndices))
	results := make(chan vmStorageResult, len(vmIndices))

	// Determine number of workers
	numWorkers := maxConcurrentFetches
	if len(vmIndices) < numWorkers {
		numWorkers = len(vmIndices)
	}

	// Start worker goroutines
	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for vmIdx := range jobs {
				vm := vmList[vmIdx]
				status, err := client.GetVMStatus(vm.Node, vm.VMID)
				results <- vmStorageResult{
					vmIdx:  vmIdx,
					status: status,
					err:    err,
				}
			}
		}()
	}

	// Send jobs to workers
	for _, vmIdx := range vmIndices {
		jobs <- vmIdx
	}
	close(jobs)

	// Wait for all workers to complete in a separate goroutine
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results and update VM list
	// Track VMs that still need config-based lookup
	var vmsNeedingConfig []int
	for result := range results {
		current := int(atomic.AddInt32(&completed, 1))
		if progress != nil {
			progress("Fetching VM storage details", current, totalVMs)
		}

		if result.err == nil && result.status != nil {
			vm := &vmList[result.vmIdx]
			// Update storage info if we got better data
			if result.status.MaxDisk > 0 {
				vm.MaxDisk = result.status.MaxDisk
				if storageLogger != nil {
					storageLogger.Printf("VM %d (%s): Got storage from status: MaxDisk=%d",
						vm.VMID, vm.Name, result.status.MaxDisk)
				}
			} else {
				// Still no MaxDisk, will try config
				vmsNeedingConfig = append(vmsNeedingConfig, result.vmIdx)
			}
			if result.status.Disk > 0 {
				vm.UsedDisk = result.status.Disk
			}
		}
	}

	// For VMs still missing storage, try to parse from config
	if len(vmsNeedingConfig) > 0 {
		fetchVMStorageFromConfig(client, vmList, vmsNeedingConfig, progress)
	}
}

// fetchVMStorageFromConfig fetches VM config and parses disk sizes
func fetchVMStorageFromConfig(client ProxmoxClient, vmList []VM, vmIndices []int, progress ProgressCallback) {
	if len(vmIndices) == 0 {
		return
	}

	if progress != nil {
		progress("Parsing VM configs for storage", 0, len(vmIndices))
	}

	var completed int32 = 0
	totalVMs := len(vmIndices)

	// Create channels for work distribution
	type configResult struct {
		vmIdx  int
		config map[string]interface{}
		err    error
	}

	jobs := make(chan int, len(vmIndices))
	results := make(chan configResult, len(vmIndices))

	// Determine number of workers
	numWorkers := maxConcurrentFetches
	if len(vmIndices) < numWorkers {
		numWorkers = len(vmIndices)
	}

	// Start worker goroutines
	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for vmIdx := range jobs {
				vm := vmList[vmIdx]
				config, err := client.GetVMConfig(vm.Node, vm.VMID)
				results <- configResult{
					vmIdx:  vmIdx,
					config: config,
					err:    err,
				}
			}
		}()
	}

	// Send jobs
	for _, vmIdx := range vmIndices {
		jobs <- vmIdx
	}
	close(jobs)

	go func() {
		wg.Wait()
		close(results)
	}()

	// Process results
	for result := range results {
		current := int(atomic.AddInt32(&completed, 1))
		if progress != nil {
			progress("Parsing VM configs for storage", current, totalVMs)
		}

		if result.err == nil && result.config != nil {
			vm := &vmList[result.vmIdx]
			totalSize := parseDiskSizesFromConfig(result.config)
			if totalSize > 0 {
				vm.MaxDisk = totalSize
				if storageLogger != nil {
					storageLogger.Printf("VM %d (%s): Parsed storage from config: MaxDisk=%d bytes (%.1f GB)",
						vm.VMID, vm.Name, totalSize, float64(totalSize)/(1024*1024*1024))
				}
			}
		}
	}
}

// diskSizeRegex matches size specifications like "100G", "500M", "1T"
var diskSizeRegex = regexp.MustCompile(`size=(\d+)([KMGT]?)`)

// VMConfigResult holds parsed VM config data including metadata and creation time
type VMConfigResult struct {
	Meta          map[string]string
	CreationTime  int64 // Unix timestamp from meta: ctime=
	TotalDiskSize int64 // Total disk size in bytes (sum of all disks)
}

// ParseVMConfigMeta reads the VM config file and parses comment metadata, creation time, and disk sizes
// The config file path is: /etc/pve/nodes/{node}/qemu-server/{vmid}.conf
// GetVMConfigContent reads the raw VM config file content
// Returns the content as a string, or an error message if file cannot be read
func GetVMConfigContent(node string, vmid int) string {
	// Try qemu first
	configPath := fmt.Sprintf("/etc/pve/nodes/%s/qemu-server/%d.conf", node, vmid)
	content, err := os.ReadFile(configPath)
	if err != nil {
		// Try LXC
		configPath = fmt.Sprintf("/etc/pve/nodes/%s/lxc/%d.conf", node, vmid)
		content, err = os.ReadFile(configPath)
		if err != nil {
			return fmt.Sprintf("Error reading config file: %v", err)
		}
	}
	return string(content)
}

// Comment format: #key1=value1,key2=value2,nomigrate=true,...
// Also parses meta: line for ctime (e.g., meta: creation-qemu=9.2.0,ctime=1767793774)
// Also sums up all disk sizes from scsi*, ide*, virtio*, sata*, efidisk*, tpmstate* entries
func ParseVMConfigMeta(node string, vmid int, vmType string) (*VMConfigResult, error) {
	result := &VMConfigResult{
		Meta:          make(map[string]string),
		CreationTime:  0,
		TotalDiskSize: 0,
	}

	// Determine config path based on VM type
	var configPath string
	if vmType == "lxc" {
		configPath = fmt.Sprintf("/etc/pve/nodes/%s/lxc/%d.conf", node, vmid)
	} else {
		configPath = fmt.Sprintf("/etc/pve/nodes/%s/qemu-server/%d.conf", node, vmid)
	}

	// Read the config file
	content, err := os.ReadFile(configPath)
	if err != nil {
		// File might not exist or not readable, return empty result
		return result, nil
	}

	// Disk prefixes to look for
	diskPrefixes := []string{"scsi", "ide", "virtio", "sata", "efidisk", "tpmstate", "rootfs", "mp"}

	// Parse each line
	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Stop parsing when we hit a snapshot section (e.g., [Backup-2026-01-19-000230])
		// Snapshot sections duplicate disk entries which would multiply our storage count
		if strings.HasPrefix(line, "[") {
			break
		}

		// Look for comment lines that contain key=value pairs (custom metadata)
		if strings.HasPrefix(line, "#") {
			// Remove the # prefix
			commentContent := strings.TrimPrefix(line, "#")
			// Check if this looks like metadata (contains = and ,)
			if strings.Contains(commentContent, "=") {
				// Parse comma-separated key=value pairs
				pairs := strings.Split(commentContent, ",")
				for _, pair := range pairs {
					kv := strings.SplitN(pair, "=", 2)
					if len(kv) == 2 {
						key := strings.TrimSpace(strings.ToLower(kv[0]))
						value := strings.TrimSpace(kv[1])
						result.Meta[key] = value
					}
				}
			}
			continue
		}

		// Look for meta: line which contains ctime (creation time)
		// Format: meta: creation-qemu=9.2.0,ctime=1767793774
		if strings.HasPrefix(line, "meta:") {
			metaContent := strings.TrimPrefix(line, "meta:")
			metaContent = strings.TrimSpace(metaContent)
			// Parse comma-separated key=value pairs
			pairs := strings.Split(metaContent, ",")
			for _, pair := range pairs {
				kv := strings.SplitN(pair, "=", 2)
				if len(kv) == 2 {
					key := strings.TrimSpace(strings.ToLower(kv[0]))
					value := strings.TrimSpace(kv[1])
					if key == "ctime" {
						if ctime, err := strconv.ParseInt(value, 10, 64); err == nil {
							result.CreationTime = ctime
						}
					}
				}
			}
			continue
		}

		// Check for disk entries (scsi0:, ide0:, virtio0:, sata0:, etc.)
		// Format: scsi0: storage:vmid/disk.qcow2,size=100G,other=options
		for _, prefix := range diskPrefixes {
			if strings.HasPrefix(line, prefix) {
				// Extract the part after the colon
				parts := strings.SplitN(line, ":", 2)
				if len(parts) != 2 {
					continue
				}
				diskValue := strings.TrimSpace(parts[1])

				// Skip CD-ROM and empty drives
				if strings.Contains(diskValue, "media=cdrom") || diskValue == "none" {
					continue
				}

				// Extract size from the disk specification using regex
				matches := diskSizeRegex.FindStringSubmatch(diskValue)
				if len(matches) >= 2 {
					sizeNum, err := strconv.ParseInt(matches[1], 10, 64)
					if err != nil {
						continue
					}

					// Apply unit multiplier
					// Note: No unit suffix means bytes (used for small items like tpmstate, efidisk)
					var multiplier int64 = 1 // Default to bytes
					if len(matches) >= 3 && matches[2] != "" {
						switch matches[2] {
						case "K":
							multiplier = 1024
						case "M":
							multiplier = 1024 * 1024
						case "G":
							multiplier = 1024 * 1024 * 1024
						case "T":
							multiplier = 1024 * 1024 * 1024 * 1024
						}
					}

					result.TotalDiskSize += sizeNum * multiplier
				}
				break // Found matching prefix, no need to check others
			}
		}
	}

	return result, nil
}

// vmConfigMetaResult holds the result of parsing VM config metadata
type vmConfigMetaResult struct {
	vmIdx  int
	result *VMConfigResult
	err    error
}

// fetchVMConfigMeta fetches config metadata for all VMs in parallel
func fetchVMConfigMeta(vmList []VM, progress ProgressCallback) {
	if len(vmList) == 0 {
		return
	}

	totalVMs := len(vmList)
	var completed int32 = 0

	if progress != nil {
		progress("Reading VM config metadata", 0, totalVMs)
	}

	// Create channels for work distribution
	jobs := make(chan int, len(vmList))
	results := make(chan vmConfigMetaResult, len(vmList))

	// Determine number of workers
	numWorkers := maxConcurrentFetches
	if len(vmList) < numWorkers {
		numWorkers = len(vmList)
	}

	// Start worker goroutines
	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for vmIdx := range jobs {
				vm := vmList[vmIdx]
				result, err := ParseVMConfigMeta(vm.Node, vm.VMID, vm.Type)
				results <- vmConfigMetaResult{
					vmIdx:  vmIdx,
					result: result,
					err:    err,
				}
			}
		}()
	}

	// Send jobs
	for i := range vmList {
		jobs <- i
	}
	close(jobs)

	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results
	for result := range results {
		current := int(atomic.AddInt32(&completed, 1))
		if progress != nil {
			progress("Reading VM config metadata", current, totalVMs)
		}

		if result.err == nil && result.result != nil {
			vmList[result.vmIdx].ConfigMeta = result.result.Meta
			vmList[result.vmIdx].CreationTime = result.result.CreationTime
			// Set total disk size from config file (more accurate than API)
			if result.result.TotalDiskSize > 0 {
				vmList[result.vmIdx].MaxDisk = result.result.TotalDiskSize
			}
			// Check for nomigrate flag
			if noMigrate, ok := result.result.Meta["nomigrate"]; ok {
				vmList[result.vmIdx].NoMigrate = strings.ToLower(noMigrate) == "true"
				// Log when NoMigrate is detected for debugging
				if vmList[result.vmIdx].NoMigrate {
					log.Printf("VM %d (%s): NoMigrate=true detected (parsed value: '%s')",
						vmList[result.vmIdx].VMID, vmList[result.vmIdx].Name, noMigrate)
				}
			}
			// Parse migration constraints
			// hostcpumodel=6150 -> VM can only run on hosts with "6150" in CPU model
			if hostCPU, ok := result.result.Meta["hostcpumodel"]; ok {
				vmList[result.vmIdx].HostCPUModel = strings.TrimSpace(hostCPU)
			}
			// withvm=il-fs -> VM must be on same host as VM named "il-fs"
			// Can be comma-separated for multiple VMs: withvm=vm1,vm2
			if withVM, ok := result.result.Meta["withvm"]; ok {
				parts := strings.Split(withVM, ",")
				for _, part := range parts {
					name := strings.TrimSpace(part)
					if name != "" {
						vmList[result.vmIdx].WithVM = append(vmList[result.vmIdx].WithVM, name)
					}
				}
			}
			// without=il-kam01 -> VM must NOT be on same host as VM named "il-kam01"
			// Can be comma-separated for multiple VMs: without=vm1,vm2
			if withoutVM, ok := result.result.Meta["without"]; ok {
				parts := strings.Split(withoutVM, ",")
				for _, part := range parts {
					name := strings.TrimSpace(part)
					if name != "" {
						vmList[result.vmIdx].WithoutVM = append(vmList[result.vmIdx].WithoutVM, name)
					}
				}
			}
		}
	}
}

// ParseNodeConfigMeta reads the node config file and parses comment metadata
// The config file path is: /etc/pve/nodes/{nodename}/config
// Comment format: #key1=value1,key2=value2,allowProvisioning=true,...
func ParseNodeConfigMeta(nodeName string) (map[string]string, error) {
	meta := make(map[string]string)

	// Node config path
	configPath := fmt.Sprintf("/etc/pve/nodes/%s/config", nodeName)

	// Check if file exists and get its info
	fileInfo, statErr := os.Stat(configPath)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			log.Printf("Node config file does not exist: %s", configPath)
		} else {
			log.Printf("Cannot stat node config file %s: %v", configPath, statErr)
		}
		return meta, nil
	}
	log.Printf("Node config file %s exists, size=%d bytes", configPath, fileInfo.Size())

	// Read the config file
	content, err := os.ReadFile(configPath)
	if err != nil {
		log.Printf("Failed to read node config file %s: %v", configPath, err)
		return meta, nil
	}
	log.Printf("Read node config file %s: %d bytes content", configPath, len(content))

	// Parse each line looking for comment lines with metadata
	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Look for comment lines that contain key=value pairs
		if strings.HasPrefix(line, "#") {
			// Remove the # prefix
			commentContent := strings.TrimPrefix(line, "#")
			// Check if this looks like metadata (contains = and ,)
			if strings.Contains(commentContent, "=") {
				log.Printf("Node config %s: Found metadata line: %s", configPath, commentContent)
				// Parse comma-separated key=value pairs
				pairs := strings.Split(commentContent, ",")
				for _, pair := range pairs {
					kv := strings.SplitN(pair, "=", 2)
					if len(kv) == 2 {
						key := strings.TrimSpace(strings.ToLower(kv[0]))
						value := strings.TrimSpace(kv[1])
						meta[key] = value
						log.Printf("Node config %s: Parsed key=%s value=%s", configPath, key, value)
					}
				}
			}
		}
	}

	if len(meta) == 0 {
		log.Printf("Node config %s: No metadata found in file", configPath)
	} else {
		log.Printf("Node config %s: Found %d metadata keys", configPath, len(meta))
	}

	return meta, nil
}

// CheckNodeHasOSD checks if a node has any VMs with names starting with "osd" and containing "cloudwm.com"
// Examples: osd050.vsan001.il.cloudwm.com, osd001.cloudwm.com
func CheckNodeHasOSD(vms []VM) bool {
	for _, vm := range vms {
		nameLower := strings.ToLower(vm.Name)
		if strings.HasPrefix(nameLower, "osd") && strings.Contains(nameLower, "cloudwm.com") {
			return true
		}
	}
	return false
}

// fetchNodeConfigMeta fetches config metadata for all nodes
// Note: This should be called BEFORE VMs are assigned to nodes
// The OSD check should be done separately after VMs are assigned
func fetchNodeConfigMeta(nodeMap map[string]*Node, progress ProgressCallback) {
	if len(nodeMap) == 0 {
		return
	}

	totalNodes := len(nodeMap)
	current := 0

	if progress != nil {
		progress("Reading node config metadata", 0, totalNodes)
	}

	for nodeName, node := range nodeMap {
		current++
		if progress != nil {
			progress("Reading node config metadata", current, totalNodes)
		}

		// Parse node config
		meta, err := ParseNodeConfigMeta(nodeName)
		if err == nil && meta != nil {
			node.ConfigMeta = meta
			// Check for hostprovision flag
			if hostProv, ok := meta["hostprovision"]; ok {
				node.AllowProvisioning = strings.ToLower(hostProv) == "true"
				log.Printf("Node %s: hostprovision=%s, AllowProvisioning=%v", nodeName, hostProv, node.AllowProvisioning)
			}
		}
		// Note: OSD check is done in updateNodeOSDStatus after VMs are assigned
	}
}

// updateNodeOSDStatus checks if nodes have OSD VMs
// This must be called AFTER VMs are assigned to nodes
func updateNodeOSDStatus(nodeMap map[string]*Node) {
	for nodeName, node := range nodeMap {
		node.HasOSD = CheckNodeHasOSD(node.VMs)
		if node.HasOSD {
			log.Printf("Node %s: HasOSD=true (found OSD VM among %d VMs)", nodeName, len(node.VMs))
		}
	}
}

// RecentlyCreatedThresholdDays is the number of days to consider a VM as "recently created"
const RecentlyCreatedThresholdDays = 90

// updateNodeOldVMsStatus checks if P-flagged nodes have VMs older than 90 days
// This indicates hosts with old VMs that may need migration
// This must be called AFTER VMs are assigned to nodes
func updateNodeOldVMsStatus(nodeMap map[string]*Node) {
	// Calculate the threshold timestamp (90 days ago)
	// VMs created BEFORE this time are considered OLD
	now := time.Now()
	thresholdTime := now.Unix() - (RecentlyCreatedThresholdDays * 24 * 60 * 60)
	thresholdDate := time.Unix(thresholdTime, 0).Format("2006-01-02")

	for nodeName, node := range nodeMap {
		// Only check nodes with AllowProvisioning (P flag)
		if !node.AllowProvisioning {
			continue
		}

		// Log all VMs and their creation times for debugging
		vmsWithCtime := 0
		vmsWithoutCtime := 0
		recentVMs := 0
		oldVMs := 0

		log.Printf("C-flag check for node %s: %d VMs, threshold=%s (ctime < %d = old, triggers C)",
			nodeName, len(node.VMs), thresholdDate, thresholdTime)

		for _, vm := range node.VMs {
			if vm.CreationTime > 0 {
				vmsWithCtime++
				ageInDays := (now.Unix() - vm.CreationTime) / (24 * 60 * 60)
				createdDate := time.Unix(vm.CreationTime, 0).Format("2006-01-02")
				isOld := vm.CreationTime < thresholdTime // VM is OLD if created BEFORE threshold

				if isOld {
					oldVMs++
					if !node.HasOldVMs {
						node.HasOldVMs = true // Flag indicates OLD VMs exist
						log.Printf("  VM %d (%s): created=%s, age=%d days - OLD (triggers C flag)",
							vm.VMID, vm.Name, createdDate, ageInDays)
					}
				} else {
					recentVMs++
					log.Printf("  VM %d (%s): created=%s, age=%d days - RECENT (<%d days)",
						vm.VMID, vm.Name, createdDate, ageInDays, RecentlyCreatedThresholdDays)
				}
			} else {
				vmsWithoutCtime++
			}
		}

		log.Printf("C-flag summary for %s: %d with ctime (%d old, %d recent), %d without ctime, C=%v",
			nodeName, vmsWithCtime, oldVMs, recentVMs, vmsWithoutCtime, node.HasOldVMs)
	}
}

// parseDiskSizesFromConfig extracts total disk size from VM config
// Looks for scsi*, ide*, virtio*, sata* entries and sums their sizes
func parseDiskSizesFromConfig(config map[string]interface{}) int64 {
	var totalSize int64 = 0

	// Disk prefixes to look for
	diskPrefixes := []string{"scsi", "ide", "virtio", "sata", "efidisk", "tpmstate"}

	for key, value := range config {
		// Check if this is a disk entry
		isDisk := false
		for _, prefix := range diskPrefixes {
			if strings.HasPrefix(key, prefix) {
				isDisk = true
				break
			}
		}

		if !isDisk {
			continue
		}

		// Parse the value string
		valueStr, ok := value.(string)
		if !ok {
			continue
		}

		// Skip CD-ROM and empty drives
		if strings.Contains(valueStr, "media=cdrom") || valueStr == "none" {
			continue
		}

		// Extract size from the disk specification
		matches := diskSizeRegex.FindStringSubmatch(valueStr)
		if len(matches) >= 2 {
			sizeNum, err := strconv.ParseInt(matches[1], 10, 64)
			if err != nil {
				continue
			}

			// Apply unit multiplier
			var multiplier int64 = 1
			if len(matches) >= 3 {
				switch matches[2] {
				case "K":
					multiplier = 1024
				case "M":
					multiplier = 1024 * 1024
				case "G":
					multiplier = 1024 * 1024 * 1024
				case "T":
					multiplier = 1024 * 1024 * 1024 * 1024
				case "":
					// No unit means bytes, but Proxmox usually uses G
					multiplier = 1024 * 1024 * 1024 // Assume GB if no unit
				}
			}

			diskSize := sizeNum * multiplier
			totalSize += diskSize

			if storageLogger != nil {
				storageLogger.Printf("  Disk %s: size=%d%s (%d bytes)", key, sizeNum, matches[2], diskSize)
			}
		}
	}

	return totalSize
}

// fetchVMDiskUsageFromStorage fetches actual disk usage from storage content API
// This queries all storages on all nodes and builds a map of VMID -> UsedDisk
// The storage content API returns the actual used size for thin-provisioned disks
// Results are cached in SQLite for 24 hours or until MaxDisk changes
func fetchVMDiskUsageFromStorage(client ProxmoxClient, vmList []VM, progress ProgressCallback) {
	if len(vmList) == 0 {
		return
	}

	// Initialize cache
	cache, cacheErr := GetDiskCache()
	if cacheErr != nil {
		log.Printf("Warning: disk cache unavailable: %v - will query all storage", cacheErr)
	}

	// Check cache for valid entries
	vmDiskUsage := make(map[int]int64)
	var vmsNeedingFetch []VM
	cacheHits := 0

	if cache != nil {
		// Get batch of cached entries
		cachedData := cache.GetBatch(vmList)

		for i := range vmList {
			vm := &vmList[i]
			if cached, found := cachedData[vm.VMID]; found {
				// Cache hit - use cached value
				vmDiskUsage[vm.VMID] = cached.UsedDisk
				cacheHits++
			} else {
				// Cache miss - need to fetch
				vmsNeedingFetch = append(vmsNeedingFetch, *vm)
			}
		}

		if storageLogger != nil {
			storageLogger.Printf("Disk cache: %d hits, %d misses", cacheHits, len(vmsNeedingFetch))
		}

		// Cleanup old entries periodically
		go cache.Cleanup()
	} else {
		// No cache available, fetch all
		vmsNeedingFetch = vmList
	}

	// If all VMs were cached, we're done
	if len(vmsNeedingFetch) == 0 {
		if progress != nil {
			progress("Using cached disk usage", 1, 1)
		}
		// Update VM list with cached values
		for i := range vmList {
			if usedDisk, found := vmDiskUsage[vmList[i].VMID]; found && usedDisk > 0 {
				vmList[i].UsedDisk = usedDisk
			}
		}
		if storageLogger != nil {
			storageLogger.Printf("All %d VMs served from cache", cacheHits)
		}
		return
	}

	// Get unique nodes from VMs needing fetch
	nodeSet := make(map[string]bool)
	for _, vm := range vmsNeedingFetch {
		nodeSet[vm.Node] = true
	}

	nodes := make([]string, 0, len(nodeSet))
	for node := range nodeSet {
		nodes = append(nodes, node)
	}

	if progress != nil {
		progress("Fetching storage disk usage", 0, len(nodes))
	}

	// Track fresh data for caching
	type freshData struct {
		vmid    int
		node    string
		maxDisk int64
		used    int64
	}
	var freshResults []freshData
	var freshMu sync.Mutex

	// Build set of VMIDs we need to fetch
	needFetchSet := make(map[int]bool)
	vmNodeMap := make(map[int]string)   // VMID -> Node
	vmMaxDiskMap := make(map[int]int64) // VMID -> MaxDisk
	for _, vm := range vmsNeedingFetch {
		needFetchSet[vm.VMID] = true
		vmNodeMap[vm.VMID] = vm.Node
		vmMaxDiskMap[vm.VMID] = vm.MaxDisk
	}

	var mu sync.Mutex

	// Process nodes in parallel
	var wg sync.WaitGroup
	var completed int32 = 0

	// Limit concurrency
	sem := make(chan struct{}, maxConcurrentFetches)

	for _, nodeName := range nodes {
		wg.Add(1)
		go func(node string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			// Extract node prefix for matching local storages
			// Node name format: kv0078-63-250-62-88 -> prefix is "kv0078"
			nodePrefix := node
			if idx := strings.Index(node, "-"); idx > 0 {
				nodePrefix = node[:idx]
			}

			// Get list of storages for this node
			storages, err := client.GetNodeStorages(node)
			if err != nil {
				if storageLogger != nil {
					storageLogger.Printf("Failed to get storages for node %s: %v", node, err)
				}
				return
			}

			// Process each storage that can hold VM images
			for _, storage := range storages {
				// Only query LOCAL storages (storage name starts with node prefix)
				// This avoids querying shared/remote storages on wrong nodes
				if !strings.HasPrefix(storage.Storage, nodePrefix) {
					continue
				}

				// Only query storages that can hold VM images
				if !strings.Contains(storage.Content, "images") {
					continue
				}

				if storageLogger != nil {
					storageLogger.Printf("Querying local storage %s on node %s", storage.Storage, node)
				}

				// Get storage content
				content, err := client.GetStorageContent(node, storage.Storage)
				if err != nil {
					if storageLogger != nil {
						storageLogger.Printf("Failed to get content for storage %s on node %s: %v",
							storage.Storage, node, err)
					}
					continue
				}

				// Process each volume
				for _, item := range content {
					// Only process VM disk images for VMs we need
					if item.Content != "images" || item.VMID == 0 {
						continue
					}

					// Only process if this VM needs fetching
					if !needFetchSet[item.VMID] {
						continue
					}

					mu.Lock()
					vmDiskUsage[item.VMID] += item.Used
					mu.Unlock()

					if storageLogger != nil {
						storageLogger.Printf("Storage content: VMID=%d Storage=%s Used=%d Size=%d",
							item.VMID, storage.Storage, item.Used, item.Size)
					}
				}
			}

			current := int(atomic.AddInt32(&completed, 1))
			if progress != nil {
				progress("Fetching storage disk usage", current, len(nodes))
			}
		}(nodeName)
	}

	wg.Wait()

	// Collect fresh data for caching
	for vmid, used := range vmDiskUsage {
		// Only cache newly fetched data (not from cache)
		if needFetchSet[vmid] && used > 0 {
			freshMu.Lock()
			freshResults = append(freshResults, freshData{
				vmid:    vmid,
				node:    vmNodeMap[vmid],
				maxDisk: vmMaxDiskMap[vmid],
				used:    used,
			})
			freshMu.Unlock()
		}
	}

	// Update cache with fresh data
	if cache != nil && len(freshResults) > 0 {
		var cacheEntries []VMDiskCache
		for _, data := range freshResults {
			cacheEntries = append(cacheEntries, VMDiskCache{
				VMID:     data.vmid,
				Node:     data.node,
				MaxDisk:  data.maxDisk,
				UsedDisk: data.used,
			})
		}
		if err := cache.SetBatch(cacheEntries); err != nil {
			log.Printf("Warning: failed to update cache: %v", err)
		} else if storageLogger != nil {
			storageLogger.Printf("Cached disk usage for %d VMs", len(cacheEntries))
		}
	}

	// Update VM list with actual disk usage (from cache + fresh)
	updatedCount := 0
	for i := range vmList {
		if usedDisk, found := vmDiskUsage[vmList[i].VMID]; found && usedDisk > 0 {
			vmList[i].UsedDisk = usedDisk
			updatedCount++
		}
	}

	if storageLogger != nil {
		storageLogger.Printf("Updated UsedDisk for %d VMs (%d from cache, %d fresh)",
			updatedCount, cacheHits, updatedCount-cacheHits)
	}
}
