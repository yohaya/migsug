package proxmox

// ProxmoxClient defines the interface for interacting with Proxmox
// This interface is implemented by both Client (API-based) and ShellClient (pvesh-based)
type ProxmoxClient interface {
	// GetClusterResources retrieves all cluster resources
	GetClusterResources() ([]ClusterResource, error)

	// GetNodeStatus retrieves detailed status for a specific node
	GetNodeStatus(node string) (*NodeStatus, error)

	// GetVMStatus retrieves detailed status for a specific VM
	GetVMStatus(node string, vmid int) (*VMStatus, error)

	// GetVMConfig retrieves VM configuration (for parsing disk sizes)
	GetVMConfig(node string, vmid int) (map[string]interface{}, error)

	// GetNodes retrieves a list of all nodes in the cluster
	GetNodes() ([]string, error)

	// Ping tests the connection to Proxmox
	Ping() error

	// Authenticate performs authentication (no-op for shell client)
	Authenticate() error
}

// Ensure both client types implement the interface
var _ ProxmoxClient = (*Client)(nil)
var _ ProxmoxClient = (*ShellClient)(nil)
