package proxmox

import "fmt"

// Node represents a Proxmox node in the cluster
type Node struct {
	Name        string
	Status      string
	CPUCores    int       // Total logical CPUs (cores * threads)
	CPUSockets  int       // Physical CPU sockets
	CPUModel    string    // CPU model name
	CPUMHz      float64   // CPU frequency in MHz
	CPUUsage    float64   // Percentage 0-100
	LoadAverage []float64 // 1, 5, 15 minute load averages
	MaxMem      int64     // bytes
	UsedMem     int64     // bytes
	MaxDisk     int64     // bytes
	UsedDisk    int64     // bytes
	SwapTotal   int64     // bytes - total swap configured
	SwapUsed    int64     // bytes - swap currently in use
	VMs         []VM
	Uptime      int64  // seconds
	PVEVersion  string // Proxmox VE version
}

// HasActiveSwap returns true if swap is configured and in use
func (n *Node) HasActiveSwap() bool {
	return n.SwapTotal > 0 && n.SwapUsed > 0
}

// VM represents a virtual machine
type VM struct {
	VMID     int
	Name     string
	Node     string
	Status   string
	Type     string  // qemu or lxc
	CPUCores int     // allocated vCPUs
	CPUUsage float64 // actual usage percentage 0-100
	MaxMem   int64   // allocated RAM in bytes
	UsedMem  int64   // actual RAM usage in bytes
	MaxDisk  int64   // allocated disk in bytes
	UsedDisk int64   // actual disk usage in bytes
	Uptime   int64   // seconds

	// Config metadata parsed from VM config file comments
	NoMigrate bool              // If true, VM should not be migrated (from nomigrate=true in config)
	ConfigMeta map[string]string // All key=value pairs from config comment line
}

// Cluster represents the entire Proxmox cluster
type Cluster struct {
	Nodes        []Node
	TotalVMs     int
	TotalVCPUs   int   // Total vCPUs across all VMs
	RunningVMs   int   // Count of running VMs
	StoppedVMs   int   // Count of stopped VMs
	TotalCPUs    int   // Total physical CPUs
	TotalRAM     int64 // Total RAM across all nodes
	TotalStorage int64 // Total storage across all nodes
	UsedStorage  int64 // Used storage across all nodes
}

// ClusterResource represents a resource from the Proxmox cluster/resources API
type ClusterResource struct {
	ID       string  `json:"id"`
	Type     string  `json:"type"`
	Node     string  `json:"node"`
	Status   string  `json:"status"`
	Name     string  `json:"name"`
	Storage  string  `json:"storage,omitempty"`
	VMID     int     `json:"vmid,omitempty"`
	MaxCPU   int     `json:"maxcpu,omitempty"`
	CPU      float64 `json:"cpu,omitempty"`
	MaxMem   int64   `json:"maxmem,omitempty"`
	Mem      int64   `json:"mem,omitempty"`
	MaxDisk  int64   `json:"maxdisk,omitempty"`
	Disk     int64   `json:"disk,omitempty"`
	Uptime   int64   `json:"uptime,omitempty"`
	Template int     `json:"template,omitempty"`
}

// NodeStatus represents detailed node status
type NodeStatus struct {
	Uptime      int64     `json:"uptime"`
	CPUInfo     CPUInfo   `json:"cpuinfo"`
	Memory      Memory    `json:"memory"`
	Swap        Swap      `json:"swap"`
	RootFS      RootFS    `json:"rootfs"`
	LoadAverage []float64 `json:"loadavg"`
}

// Swap contains swap information
type Swap struct {
	Total int64 `json:"total"`
	Used  int64 `json:"used"`
	Free  int64 `json:"free"`
}

// CPUInfo contains CPU information
type CPUInfo struct {
	Cores   int     `json:"cores"`
	CPUs    int     `json:"cpus"`
	Model   string  `json:"model"`
	Sockets int     `json:"sockets"`
	MHz     float64 `json:"mhz"`
}

// Memory contains memory information
type Memory struct {
	Total int64 `json:"total"`
	Used  int64 `json:"used"`
	Free  int64 `json:"free"`
}

// RootFS contains root filesystem information
type RootFS struct {
	Total int64 `json:"total"`
	Used  int64 `json:"used"`
	Free  int64 `json:"free"`
	Avail int64 `json:"avail"`
}

// VMStatus represents detailed VM status
type VMStatus struct {
	Status  string  `json:"status"`
	VMID    int     `json:"vmid"`
	Name    string  `json:"name"`
	Uptime  int64   `json:"uptime"`
	CPUs    int     `json:"cpus"`
	CPU     float64 `json:"cpu"`
	MaxMem  int64   `json:"maxmem"`
	Mem     int64   `json:"mem"`
	MaxDisk int64   `json:"maxdisk"`
	Disk    int64   `json:"disk"`
}

// APIResponse is a generic API response wrapper
type APIResponse struct {
	Data interface{} `json:"data"`
}

// StorageInfo represents storage information for a node
type StorageInfo struct {
	Storage string `json:"storage"`
	Type    string `json:"type"`
	Content string `json:"content"`
	Total   int64  `json:"total"`
	Used    int64  `json:"used"`
	Avail   int64  `json:"avail"`
	Active  int    `json:"active"`
	Enabled int    `json:"enabled"`
	Shared  int    `json:"shared"`
}

// GetCPUPercent returns CPU usage as a percentage
func (n *Node) GetCPUPercent() float64 {
	return n.CPUUsage * 100
}

// GetRunningVCPUs returns the total vCPUs allocated to running VMs
func (n *Node) GetRunningVCPUs() int {
	total := 0
	for _, vm := range n.VMs {
		if vm.Status == "running" {
			total += vm.CPUCores
		}
	}
	return total
}

// GetMemPercent returns memory usage as a percentage
func (n *Node) GetMemPercent() float64 {
	if n.MaxMem == 0 {
		return 0
	}
	return float64(n.UsedMem) / float64(n.MaxMem) * 100
}

// GetDiskPercent returns disk usage as a percentage
func (n *Node) GetDiskPercent() float64 {
	if n.MaxDisk == 0 {
		return 0
	}
	return float64(n.UsedDisk) / float64(n.MaxDisk) * 100
}

// GetMemPercent returns memory usage as a percentage for VM
func (v *VM) GetMemPercent() float64 {
	if v.MaxMem == 0 {
		return 0
	}
	return float64(v.UsedMem) / float64(v.MaxMem) * 100
}

// GetDiskPercent returns disk usage as a percentage for VM
func (v *VM) GetDiskPercent() float64 {
	if v.MaxDisk == 0 {
		return 0
	}
	return float64(v.UsedDisk) / float64(v.MaxDisk) * 100
}

// FormatBytes converts bytes to human-readable format
func FormatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	units := []string{"KB", "MB", "GB", "TB", "PB"}
	return fmt.Sprintf("%.1f %s", float64(bytes)/float64(div), units[exp])
}
