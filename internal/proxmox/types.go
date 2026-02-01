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

	// Node status indicators (parsed from config and VMs)
	HasOSD            bool              // True if node has VMs with name matching osd*.cloudwm.com
	AllowProvisioning bool              // True if node config has hostprovision=true
	HasOldVMs         bool              // True if node has P flag and VMs older than 90 days (C flag)
	HostState         int               // Host state from config (0-3). -1 means not set. 0=maintenance, 3=blocked (no migrations)
	ConfigMeta        map[string]string // All key=value pairs from node config comment line
}

// IsMigrationBlocked returns true if the host state blocks migrations
// hoststate=0: maintenance mode - no migrations to/from
// hoststate=3: blocked state - no migrations to/from
// Note: -1 means not set (migrations allowed)
func (n *Node) IsMigrationBlocked() bool {
	return n.HostState == 0 || n.HostState == 3
}

// HasHostState returns true if hoststate is configured (not -1)
func (n *Node) HasHostState() bool {
	return n.HostState >= 0
}

// HasActiveSwap returns true if swap is configured and in use
func (n *Node) HasActiveSwap() bool {
	return n.SwapTotal > 0 && n.SwapUsed > 0
}

// GetStatusIndicators returns status indicator letters (e.g., "OPC" for OSD + Provisioning + old VMs)
// Note: hoststate is NOT included here - it's shown separately in GetStatusWithIndicators
func (n *Node) GetStatusIndicators() string {
	indicators := ""
	if n.HasOSD {
		indicators += "O"
	}
	if n.AllowProvisioning {
		indicators += "P"
	}
	if n.HasOldVMs {
		indicators += "C"
	}
	return indicators
}

// GetStatusWithIndicators returns status with hoststate and indicators
// Format: "online/3 (OPC)" where hoststate is shown after slash, other flags in parentheses
// Special: hoststate=1 shows "maint" instead of "online/1"
func (n *Node) GetStatusWithIndicators() string {
	// Build status with hoststate (if set, i.e. >= 0)
	status := n.Status
	if n.HasHostState() {
		if n.HostState == 1 {
			// Maintenance mode - show "maint" instead of "online/1"
			status = "maint"
		} else {
			status = fmt.Sprintf("%s/%d", n.Status, n.HostState)
		}
	}

	// Add other indicators in parentheses
	indicators := n.GetStatusIndicators()
	if indicators == "" {
		return status
	}
	return fmt.Sprintf("%s (%s)", status, indicators)
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
	NoMigrate    bool              // If true, VM should not be migrated (from nomigrate=true in config)
	ConfigMeta   map[string]string // All key=value pairs from config comment line
	CreationTime int64             // Unix timestamp of VM creation (from meta: ctime= in config)

	// Migration constraints parsed from config comment line
	HostCPUModel string   // Required CPU model substring (from hostcpumodel=value) - VM can only run on hosts with this in CPU model
	WithVM       []string // VM names that must be on the same host (from withvm=name1,name2)
	WithoutVM    []string // VM names that must NOT be on the same host (from without=name1,name2)
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
	PVEVersion  string    `json:"pveversion"` // Proxmox VE version (e.g., "pve-manager/8.1.2/...")
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

// StorageContentItem represents a volume in storage content
// Used to get actual disk usage for thin-provisioned VMs
type StorageContentItem struct {
	Content string `json:"content"` // "images", "iso", etc.
	Format  string `json:"format"`  // "qcow2", "raw", etc.
	Size    int64  `json:"size"`    // Allocated/provisioned size in bytes
	Used    int64  `json:"used"`    // Actual used size in bytes (thin provisioning)
	VMID    int    `json:"vmid"`    // VM ID (0 for non-VM content like ISOs)
	VolID   string `json:"volid"`   // Volume ID (e.g., "storage:vmid/vm-vmid-disk-0.qcow2")
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

// GetEffectiveDisk returns the actual disk usage (UsedDisk for thin provisioning)
// Falls back to MaxDisk if UsedDisk is not available
func (v *VM) GetEffectiveDisk() int64 {
	if v.UsedDisk > 0 {
		return v.UsedDisk
	}
	return v.MaxDisk
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
