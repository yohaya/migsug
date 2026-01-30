package proxmox

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// ShellClient represents a Proxmox client using local shell commands (pvesh)
// This client runs directly on a Proxmox host and requires root privileges
type ShellClient struct {
	// No authentication needed - uses pvesh which accesses local API
}

// NewShellClient creates a new Proxmox shell client
// This should only be used when running on a Proxmox host as root
func NewShellClient() *ShellClient {
	return &ShellClient{}
}

// IsAvailable checks if pvesh command is available (i.e., running on Proxmox host)
func IsAvailable() bool {
	cmd := exec.Command("which", "pvesh")
	err := cmd.Run()
	return err == nil
}

// pvesh executes a pvesh command and returns the JSON output
func (c *ShellClient) pvesh(args ...string) ([]byte, error) {
	// pvesh get /api2/json/path --output-format json
	fullArgs := append(args, "--output-format", "json")
	cmd := exec.Command("pvesh", fullArgs...)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("pvesh command failed: %w\nOutput: %s", err, string(output))
	}

	return output, nil
}

// GetClusterResources retrieves all cluster resources using pvesh
func (c *ShellClient) GetClusterResources() ([]ClusterResource, error) {
	output, err := c.pvesh("get", "/cluster/resources")
	if err != nil {
		return nil, err
	}

	var resources []ClusterResource
	if err := json.Unmarshal(output, &resources); err != nil {
		return nil, fmt.Errorf("failed to unmarshal cluster resources: %w", err)
	}

	return resources, nil
}

// GetNodeStatus retrieves detailed status for a specific node
func (c *ShellClient) GetNodeStatus(node string) (*NodeStatus, error) {
	path := fmt.Sprintf("/nodes/%s/status", node)
	output, err := c.pvesh("get", path)
	if err != nil {
		return nil, err
	}

	// Try to unmarshal with flexible structure
	var rawStatus map[string]interface{}
	if err := json.Unmarshal(output, &rawStatus); err != nil {
		return nil, fmt.Errorf("failed to unmarshal node status: %w", err)
	}

	status := &NodeStatus{}

	// Extract cpuinfo if present
	if cpuinfo, ok := rawStatus["cpuinfo"].(map[string]interface{}); ok {
		if model, ok := cpuinfo["model"].(string); ok {
			status.CPUInfo.Model = model
		}
		if sockets, ok := cpuinfo["sockets"].(float64); ok {
			status.CPUInfo.Sockets = int(sockets)
		}
		if cpus, ok := cpuinfo["cpus"].(float64); ok {
			status.CPUInfo.CPUs = int(cpus)
		}
		if cores, ok := cpuinfo["cores"].(float64); ok {
			status.CPUInfo.Cores = int(cores)
		}
		// MHz can be float64 or string
		if mhz, ok := cpuinfo["mhz"].(float64); ok {
			status.CPUInfo.MHz = mhz
		} else if mhzStr, ok := cpuinfo["mhz"].(string); ok {
			fmt.Sscanf(mhzStr, "%f", &status.CPUInfo.MHz)
		}
	}

	// Extract uptime
	if uptime, ok := rawStatus["uptime"].(float64); ok {
		status.Uptime = int64(uptime)
	}

	// Extract load average (array of 3 floats: 1, 5, 15 minute averages)
	if loadavg, ok := rawStatus["loadavg"].([]interface{}); ok {
		status.LoadAverage = make([]float64, 0, len(loadavg))
		for _, v := range loadavg {
			if f, ok := v.(float64); ok {
				status.LoadAverage = append(status.LoadAverage, f)
			} else if s, ok := v.(string); ok {
				var f float64
				fmt.Sscanf(s, "%f", &f)
				status.LoadAverage = append(status.LoadAverage, f)
			}
		}
	}

	// Extract swap information
	if swap, ok := rawStatus["swap"].(map[string]interface{}); ok {
		if total, ok := swap["total"].(float64); ok {
			status.Swap.Total = int64(total)
		}
		if used, ok := swap["used"].(float64); ok {
			status.Swap.Used = int64(used)
		}
		if free, ok := swap["free"].(float64); ok {
			status.Swap.Free = int64(free)
		}
	}

	// Extract PVE version (format: "pve-manager/8.1.2/...")
	if pveversion, ok := rawStatus["pveversion"].(string); ok {
		status.PVEVersion = pveversion
	}

	return status, nil
}

// GetNodeStorage retrieves storage information for a specific node
func (c *ShellClient) GetNodeStorage(node string) ([]StorageInfo, error) {
	path := fmt.Sprintf("/nodes/%s/storage", node)
	output, err := c.pvesh("get", path)
	if err != nil {
		return nil, err
	}

	var storages []StorageInfo
	if err := json.Unmarshal(output, &storages); err != nil {
		return nil, fmt.Errorf("failed to unmarshal storage info: %w", err)
	}

	return storages, nil
}

// GetVMStatus retrieves detailed status for a specific VM
func (c *ShellClient) GetVMStatus(node string, vmid int) (*VMStatus, error) {
	path := fmt.Sprintf("/nodes/%s/qemu/%d/status/current", node, vmid)
	output, err := c.pvesh("get", path)
	if err != nil {
		return nil, err
	}

	var status VMStatus
	if err := json.Unmarshal(output, &status); err != nil {
		return nil, fmt.Errorf("failed to unmarshal VM status: %w", err)
	}

	return &status, nil
}

// GetVMConfig retrieves VM configuration
func (c *ShellClient) GetVMConfig(node string, vmid int) (map[string]interface{}, error) {
	path := fmt.Sprintf("/nodes/%s/qemu/%d/config", node, vmid)
	output, err := c.pvesh("get", path)
	if err != nil {
		return nil, err
	}

	var config map[string]interface{}
	if err := json.Unmarshal(output, &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal VM config: %w", err)
	}

	return config, nil
}

// GetNodes retrieves a list of all nodes in the cluster
func (c *ShellClient) GetNodes() ([]string, error) {
	output, err := c.pvesh("get", "/nodes")
	if err != nil {
		return nil, err
	}

	var nodes []struct {
		Node string `json:"node"`
	}
	if err := json.Unmarshal(output, &nodes); err != nil {
		return nil, fmt.Errorf("failed to unmarshal nodes: %w", err)
	}

	nodeNames := make([]string, len(nodes))
	for i, n := range nodes {
		nodeNames[i] = n.Node
	}

	return nodeNames, nil
}

// Ping tests if pvesh is working
func (c *ShellClient) Ping() error {
	_, err := c.pvesh("get", "/version")
	return err
}

// Authenticate is a no-op for shell client (no authentication needed)
func (c *ShellClient) Authenticate() error {
	return nil
}

// GetHostname returns the current Proxmox host's hostname
func GetHostname() (string, error) {
	cmd := exec.Command("hostname")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get hostname: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// IsProxmoxHost checks if we're running on a Proxmox VE host
func IsProxmoxHost() bool {
	// Check for /etc/pve directory (Proxmox cluster filesystem)
	cmd := exec.Command("test", "-d", "/etc/pve")
	err := cmd.Run()
	if err != nil {
		return false
	}

	// Check if pvesh is available
	return IsAvailable()
}
