package proxmox

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// CacheMaxAge is how long cache entries are valid (24 hours)
const CacheMaxAge = 24 * time.Hour

// VMDiskCache represents cached disk usage data for a VM
type VMDiskCache struct {
	VMID      int
	Node      string
	MaxDisk   int64 // Allocated disk size (used to detect changes)
	UsedDisk  int64 // Actual used disk size (thin provisioning)
	UpdatedAt time.Time
}

// DiskCache manages SQLite-based caching of VM disk usage data
type DiskCache struct {
	db   *sql.DB
	mu   sync.Mutex
	path string
}

// diskCacheInstance is the singleton cache instance
var diskCacheInstance *DiskCache
var diskCacheOnce sync.Once
var diskCacheErr error

// GetDiskCache returns the singleton disk cache instance
// The database is stored in the same directory as the executable
func GetDiskCache() (*DiskCache, error) {
	diskCacheOnce.Do(func() {
		// Get executable directory
		exePath, err := os.Executable()
		if err != nil {
			// Fallback to current directory
			exePath = "."
		}
		exeDir := filepath.Dir(exePath)

		// If running from go run, use current directory instead
		if filepath.Base(exeDir) == "exe" || filepath.Base(exePath) == "main" {
			exeDir = "."
		}

		dbPath := filepath.Join(exeDir, "migsug_cache.db")
		diskCacheInstance, diskCacheErr = newDiskCache(dbPath)
	})
	return diskCacheInstance, diskCacheErr
}

// newDiskCache creates a new disk cache with the given database path
func newDiskCache(dbPath string) (*DiskCache, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open cache database: %w", err)
	}

	cache := &DiskCache{
		db:   db,
		path: dbPath,
	}

	// Initialize the database schema
	if err := cache.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize cache schema: %w", err)
	}

	log.Printf("Disk cache initialized at %s", dbPath)
	return cache, nil
}

// initSchema creates the cache table if it doesn't exist
func (c *DiskCache) initSchema() error {
	_, err := c.db.Exec(`
		CREATE TABLE IF NOT EXISTS vm_disk_cache (
			vmid INTEGER NOT NULL,
			node TEXT NOT NULL,
			max_disk INTEGER NOT NULL,
			used_disk INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			PRIMARY KEY (vmid, node)
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create cache table: %w", err)
	}

	// Create index for faster lookups
	_, err = c.db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_vm_disk_cache_updated
		ON vm_disk_cache(updated_at)
	`)
	if err != nil {
		return fmt.Errorf("failed to create index: %w", err)
	}

	return nil
}

// Get retrieves cached disk usage for a VM
// Returns nil if not found or cache is stale
func (c *DiskCache) Get(vmid int, node string, currentMaxDisk int64) *VMDiskCache {
	c.mu.Lock()
	defer c.mu.Unlock()

	var cache VMDiskCache
	var updatedAtUnix int64

	err := c.db.QueryRow(`
		SELECT vmid, node, max_disk, used_disk, updated_at
		FROM vm_disk_cache
		WHERE vmid = ? AND node = ?
	`, vmid, node).Scan(&cache.VMID, &cache.Node, &cache.MaxDisk, &cache.UsedDisk, &updatedAtUnix)

	if err != nil {
		if err != sql.ErrNoRows {
			log.Printf("Cache read error for VM %d: %v", vmid, err)
		}
		return nil
	}

	cache.UpdatedAt = time.Unix(updatedAtUnix, 0)

	// Check if cache is valid:
	// 1. MaxDisk hasn't changed (disk wasn't resized)
	// 2. Cache is less than 24 hours old
	if cache.MaxDisk != currentMaxDisk {
		// Disk size changed, cache is invalid
		return nil
	}

	if time.Since(cache.UpdatedAt) > CacheMaxAge {
		// Cache is too old
		return nil
	}

	return &cache
}

// Set stores disk usage data for a VM in the cache
func (c *DiskCache) Set(vmid int, node string, maxDisk, usedDisk int64) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	_, err := c.db.Exec(`
		INSERT OR REPLACE INTO vm_disk_cache (vmid, node, max_disk, used_disk, updated_at)
		VALUES (?, ?, ?, ?, ?)
	`, vmid, node, maxDisk, usedDisk, time.Now().Unix())

	if err != nil {
		return fmt.Errorf("failed to cache disk usage for VM %d: %w", vmid, err)
	}

	return nil
}

// SetBatch stores multiple disk usage entries in a single transaction
func (c *DiskCache) SetBatch(entries []VMDiskCache) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	tx, err := c.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT OR REPLACE INTO vm_disk_cache (vmid, node, max_disk, used_disk, updated_at)
		VALUES (?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	now := time.Now().Unix()
	for _, entry := range entries {
		_, err = stmt.Exec(entry.VMID, entry.Node, entry.MaxDisk, entry.UsedDisk, now)
		if err != nil {
			return fmt.Errorf("failed to cache disk usage for VM %d: %w", entry.VMID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// GetBatch retrieves cached disk usage for multiple VMs
// Returns a map of VMID -> VMDiskCache for valid cache entries
func (c *DiskCache) GetBatch(vms []VM) map[int]*VMDiskCache {
	c.mu.Lock()
	defer c.mu.Unlock()

	result := make(map[int]*VMDiskCache)
	now := time.Now()
	maxAgeUnix := now.Add(-CacheMaxAge).Unix()

	// Build a map of VMID -> current MaxDisk for validation
	vmMaxDisk := make(map[int]int64)
	for _, vm := range vms {
		vmMaxDisk[vm.VMID] = vm.MaxDisk
	}

	// Query all potentially valid cache entries
	rows, err := c.db.Query(`
		SELECT vmid, node, max_disk, used_disk, updated_at
		FROM vm_disk_cache
		WHERE updated_at > ?
	`, maxAgeUnix)
	if err != nil {
		log.Printf("Cache batch read error: %v", err)
		return result
	}
	defer rows.Close()

	for rows.Next() {
		var cache VMDiskCache
		var updatedAtUnix int64

		if err := rows.Scan(&cache.VMID, &cache.Node, &cache.MaxDisk, &cache.UsedDisk, &updatedAtUnix); err != nil {
			log.Printf("Cache row scan error: %v", err)
			continue
		}

		cache.UpdatedAt = time.Unix(updatedAtUnix, 0)

		// Validate: MaxDisk must match current value
		if currentMax, exists := vmMaxDisk[cache.VMID]; exists && currentMax == cache.MaxDisk {
			result[cache.VMID] = &cache
		}
	}

	return result
}

// Cleanup removes old cache entries (older than 7 days)
func (c *DiskCache) Cleanup() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	cutoff := time.Now().Add(-7 * 24 * time.Hour).Unix()
	result, err := c.db.Exec(`DELETE FROM vm_disk_cache WHERE updated_at < ?`, cutoff)
	if err != nil {
		return fmt.Errorf("failed to cleanup cache: %w", err)
	}

	affected, _ := result.RowsAffected()
	if affected > 0 {
		log.Printf("Cleaned up %d old cache entries", affected)
	}

	return nil
}

// Close closes the database connection
func (c *DiskCache) Close() error {
	if c.db != nil {
		return c.db.Close()
	}
	return nil
}

// Stats returns cache statistics
func (c *DiskCache) Stats() (total int, valid int, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	err = c.db.QueryRow(`SELECT COUNT(*) FROM vm_disk_cache`).Scan(&total)
	if err != nil {
		return 0, 0, err
	}

	maxAgeUnix := time.Now().Add(-CacheMaxAge).Unix()
	err = c.db.QueryRow(`SELECT COUNT(*) FROM vm_disk_cache WHERE updated_at > ?`, maxAgeUnix).Scan(&valid)
	if err != nil {
		return 0, 0, err
	}

	return total, valid, nil
}
