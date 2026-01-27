package analyzer

// MigrationStrategy defines how to prioritize VMs for CPU-based migration
type MigrationStrategy int

const (
	// StrategyThorough migrates all VM sizes as needed to reach CPU target
	StrategyThorough MigrationStrategy = iota
	// StrategyQuick only migrates small VMs (≤100 GiB) for fastest migration
	StrategyQuick
	// StrategyBalanced migrates small and medium VMs (≤500 GiB)
	StrategyBalanced
)

// String returns the string representation of a MigrationStrategy
func (s MigrationStrategy) String() string {
	switch s {
	case StrategyQuick:
		return "Quick Relief (≤100 GiB disks only)"
	case StrategyBalanced:
		return "Balanced (≤500 GiB disks)"
	case StrategyThorough:
		return "Thorough (all disk sizes)"
	default:
		return "Unknown"
	}
}

// DiskSizeTier represents a tier of VMs based on disk size
type DiskSizeTier int

const (
	TierSmall  DiskSizeTier = iota // ≤100 GiB - fast migration
	TierMedium                     // 100-500 GiB - moderate migration
	TierLarge                      // >500 GiB - slow migration
)

// Disk size thresholds in bytes
const (
	SmallDiskThreshold  = 100 * 1024 * 1024 * 1024  // 100 GiB
	MediumDiskThreshold = 500 * 1024 * 1024 * 1024  // 500 GiB
)

// MigrationConstraints defines the user's migration requirements
type MigrationConstraints struct {
	SourceNode string

	// Migration criteria (one or more can be set)
	VMCount       *int     // migrate N VMs
	VCPUCount     *int     // migrate VMs totaling N vCPUs
	CPUUsage      *float64 // migrate VMs using N% CPU
	RAMAmount     *int64   // migrate VMs using N GB RAM (in bytes)
	StorageAmount *int64   // migrate VMs using N GB storage (in bytes)
	SpecificVMs   []int    // migrate these specific VMIDs
	MigrateAll    bool     // migrate all VMs from host, spread across cluster

	// CPU migration strategy (for ModeCPUUsage)
	CPUStrategy MigrationStrategy // How to prioritize VMs by disk size

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
	ModeAll // Migrate all VMs from host, spread across cluster below average
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
	default:
		return "unknown"
	}
}

// GetMode returns the migration mode based on what's set in constraints
func (c *MigrationConstraints) GetMode() MigrationMode {
	if c.MigrateAll {
		return ModeAll
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
		c.MigrateAll

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
