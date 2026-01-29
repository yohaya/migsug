package analyzer

// MigrationConstraints defines the user's migration requirements
type MigrationConstraints struct {
	SourceNode string

	// Migration criteria (one or more can be set)
	VMCount        *int     // migrate N VMs
	VCPUCount      *int     // migrate VMs totaling N vCPUs
	CPUUsage       *float64 // migrate VMs using N% CPU (uses efficiency algorithm)
	RAMAmount      *int64   // migrate VMs using N GB RAM (in bytes)
	StorageAmount  *int64   // migrate VMs using N GB storage (in bytes)
	SpecificVMs    []int    // migrate these specific VMIDs
	MigrateAll     bool     // migrate all VMs from host, spread across cluster
	CreationAge    *int     // migrate VMs older than N days (based on ctime)
	BalanceCluster bool     // migrate VMs to bring host CPU/RAM to cluster average with minimum moves

	// Additional constraints
	ExcludeNodes  []string // don't migrate to these nodes
	MaxVMsPerHost *int     // limit VMs per target host
	MinCPUFree    *float64 // require at least N% CPU free on target
	MinRAMFree    *int64   // require at least N bytes RAM free on target
}

// MigrationMode represents the type of migration strategy
type MigrationMode int

const (
	ModeVMCount MigrationMode = iota
	ModeVCPU
	ModeCPUUsage
	ModeRAM
	ModeStorage
	ModeSpecific
	ModeAll            // Migrate all VMs from host, spread across cluster below average
	ModeCreationDate   // Migrate VMs older than N days based on creation time
	ModeBalanceCluster // Balance cluster: migrate VMs until host reaches cluster average with minimum moves
)

// String returns the string representation of a MigrationMode
func (m MigrationMode) String() string {
	switch m {
	case ModeVMCount:
		return "vm_count"
	case ModeVCPU:
		return "vcpu"
	case ModeCPUUsage:
		return "cpu_usage"
	case ModeRAM:
		return "ram"
	case ModeStorage:
		return "storage"
	case ModeSpecific:
		return "specific"
	case ModeAll:
		return "all"
	case ModeCreationDate:
		return "creation_date"
	case ModeBalanceCluster:
		return "balance_cluster"
	default:
		return "unknown"
	}
}

// GetMode returns the migration mode based on what's set in constraints
func (c *MigrationConstraints) GetMode() MigrationMode {
	if c.MigrateAll {
		return ModeAll
	}
	if c.BalanceCluster {
		return ModeBalanceCluster
	}
	if len(c.SpecificVMs) > 0 {
		return ModeSpecific
	}
	if c.VMCount != nil {
		return ModeVMCount
	}
	if c.VCPUCount != nil {
		return ModeVCPU
	}
	if c.CPUUsage != nil {
		return ModeCPUUsage
	}
	if c.RAMAmount != nil {
		return ModeRAM
	}
	if c.StorageAmount != nil {
		return ModeStorage
	}
	if c.CreationAge != nil {
		return ModeCreationDate
	}
	return ModeVMCount // default
}

// Validate checks if the constraints are valid
func (c *MigrationConstraints) Validate() error {
	if c.SourceNode == "" {
		return &ValidationError{Field: "SourceNode", Message: "source node is required"}
	}

	// Check that at least one criterion is set
	hasConstraint := c.VMCount != nil ||
		c.VCPUCount != nil ||
		c.CPUUsage != nil ||
		c.RAMAmount != nil ||
		c.StorageAmount != nil ||
		len(c.SpecificVMs) > 0 ||
		c.MigrateAll ||
		c.CreationAge != nil ||
		c.BalanceCluster

	if !hasConstraint {
		return &ValidationError{
			Field:   "constraints",
			Message: "at least one migration criterion must be specified",
		}
	}

	// Validate values
	if c.VMCount != nil && *c.VMCount <= 0 {
		return &ValidationError{Field: "VMCount", Message: "must be greater than 0"}
	}
	if c.VCPUCount != nil && *c.VCPUCount <= 0 {
		return &ValidationError{Field: "VCPUCount", Message: "must be greater than 0"}
	}
	if c.CPUUsage != nil && (*c.CPUUsage <= 0 || *c.CPUUsage > 100) {
		return &ValidationError{Field: "CPUUsage", Message: "must be between 0 and 100"}
	}
	if c.RAMAmount != nil && *c.RAMAmount <= 0 {
		return &ValidationError{Field: "RAMAmount", Message: "must be greater than 0"}
	}
	if c.StorageAmount != nil && *c.StorageAmount <= 0 {
		return &ValidationError{Field: "StorageAmount", Message: "must be greater than 0"}
	}
	if c.CreationAge != nil && *c.CreationAge <= 0 {
		return &ValidationError{Field: "CreationAge", Message: "must be greater than 0"}
	}

	return nil
}

// ValidationError represents a constraint validation error
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Field + ": " + e.Message
}
