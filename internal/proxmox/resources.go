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

// ParseVMConfigMeta reads the VM config file and parses comment metadata
// The config file path is: /etc/pve/nodes/{node}/qemu-server/{vmid}.conf
// Comment format: #key1=value1,key2=value2,nomigrate=true,...
func ParseVMConfigMeta(node string, vmid int, vmType string) (map[string]string, error) {
	meta := make(map[string]string)

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
		// File might not exist or not readable, return empty meta
		return meta, nil
	}

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
				// Parse comma-separated key=value pairs
				pairs := strings.Split(commentContent, ",")
				for _, pair := range pairs {
					kv := strings.SplitN(pair, "=", 2)
					if len(kv) == 2 {
						key := strings.TrimSpace(strings.ToLower(kv[0]))
						value := strings.TrimSpace(kv[1])
						meta[key] = value
					}
				}
			}
		}
	}

	return meta, nil
}

// vmConfigMetaResult holds the result of parsing VM config metadata
type vmConfigMetaResult struct {
	vmIdx int
	meta  map[string]string
	err   error
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
				meta, err := ParseVMConfigMeta(vm.Node, vm.VMID, vm.Type)
				results <- vmConfigMetaResult{
					vmIdx: vmIdx,
					meta:  meta,
					err:   err,
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

		if result.err == nil && result.meta != nil {
			vmList[result.vmIdx].ConfigMeta = result.meta
			// Check for nomigrate flag
			if noMigrate, ok := result.meta["nomigrate"]; ok {
				vmList[result.vmIdx].NoMigrate = strings.ToLower(noMigrate) == "true"
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

	// Read the config file
	content, err := os.ReadFile(configPath)
	if err != nil {
		// File might not exist or not readable, return empty meta
		return meta, nil
	}

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
				// Parse comma-separated key=value pairs
				pairs := strings.Split(commentContent, ",")
				for _, pair := range pairs {
					kv := strings.SplitN(pair, "=", 2)
					if len(kv) == 2 {
						key := strings.TrimSpace(strings.ToLower(kv[0]))
						value := strings.TrimSpace(kv[1])
						meta[key] = value
					}
				}
			}
		}
	}

	return meta, nil
}

// osdVMRegex matches VM names like osd*.cloudwm.com
var osdVMRegex = regexp.MustCompile(`^osd.*\.cloudwm\.com$`)

// CheckNodeHasOSD checks if a node has any VMs with names matching osd*.cloudwm.com
func CheckNodeHasOSD(vms []VM) bool {
	for _, vm := range vms {
		if osdVMRegex.MatchString(vm.Name) {
			return true
		}
	}
	return false
}

// fetchNodeConfigMeta fetches config metadata for all nodes
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
			}
		}

		// Check if node has OSD VMs
		node.HasOSD = CheckNodeHasOSD(node.VMs)
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
