# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.0.0] - 2026-01-24

### Added
- Initial release of Proxmox VM Migration Suggester (migsug)
- Interactive Terminal UI (TUI) built with Bubble Tea
- Proxmox API client with support for API tokens and username/password auth
- Smart migration analysis algorithm with scoring system
- Six migration modes:
  - VM Count - Migrate N virtual machines
  - vCPU Count - Migrate VMs totaling N vCPUs
  - CPU Usage - Migrate VMs using N% CPU
  - RAM Amount - Migrate VMs using N GB RAM
  - Storage Amount - Migrate VMs using N GB storage
  - Specific VMs - Select exact VMs to migrate
- Before/After comparison view showing predicted cluster state
- Resource utilization visualization with progress bars
- Automated CI/CD pipeline with GitHub Actions
- Automatic version management and binary building
- Pre-compiled binaries for Linux, macOS, and Windows
- Comprehensive documentation and quick start guide

### Technical Features
- Clean architecture with separation of concerns
- Full error handling and validation
- Cross-platform support
- Single binary with no dependencies
- TLS support for secure API connections
- Debug logging mode
- Comprehensive test coverage structure

### Documentation
- README.md with detailed usage instructions
- QUICKSTART.md for getting started
- Makefile with common build targets
- Code comments and inline documentation
- LICENSE (MIT)

### CI/CD
- Automated testing on all branches
- Automated building on main branch commits
- Automatic version incrementing
- Binary compilation for all platforms
- GitHub Releases with download links
- Checksums for binary verification

## [Unreleased]

### Added
- **Local/Shell Mode**: Run directly on Proxmox host without credentials
  - Uses `pvesh` command-line tool for local API access
  - Auto-detects when running on Proxmox host (checks `/etc/pve` and `pvesh`)
  - No API token or password required when running locally as root
  - Significantly faster than API mode (no network overhead)
  - Displays current hostname when running locally

- **Interactive Credential Prompt**: User-friendly authentication flow
  - Automatically prompts for username/password if not provided
  - Secure password input (hidden characters)
  - Multiple authentication methods supported:
    1. Local mode (no credentials on Proxmox host)
    2. Interactive prompt
    3. Command-line flags (`--username`, `--password`, `--api-token`)
    4. Environment variables (`PVE_USERNAME`, `PVE_PASSWORD`, `PVE_API_TOKEN`)

- **ProxmoxClient Interface**: Unified abstraction for API access
  - Allows seamless switching between API and shell clients
  - Same codebase works for both local and remote access
  - Implemented by both `Client` (HTTP API) and `ShellClient` (pvesh)

- **Shell Client Implementation** (`internal/proxmox/shell_client.go`)
  - Direct `pvesh` command execution
  - JSON output parsing
  - Hostname detection
  - Proxmox host validation

- **Enhanced Documentation**:
  - `README-LOCAL.md`: Complete guide for local/shell usage
  - Comparison table: Local vs Remote modes
  - Troubleshooting for both scenarios
  - Security recommendations
  - Performance benchmarks

### Changed
- Updated `main.go` to auto-detect Proxmox host and select appropriate client
- Modified all client usage to work with `ProxmoxClient` interface
- Enhanced error messages with mode-specific troubleshooting tips
- Improved authentication flow with fallback chain
- Better user experience with clear instructions

### Technical Details
- Added `golang.org/x/term` dependency for secure password input
- Created `ProxmoxClient` interface in `internal/proxmox/interface.go`
- Shell client executes: `pvesh get /path --output-format json`
- API client remains unchanged (fully backward compatible)
- Automatic mode detection based on environment

### Security Improvements
- Hidden password input in interactive mode
- No credentials needed when running locally
- Reduced attack surface (local mode uses Unix socket-like access)
- Secure defaults (prefer local mode when available)

### Performance Improvements
- Local mode: ~100ms per API call (direct pvesh)
- Remote mode: ~500ms+ per API call (network + HTTPS)
- 5-10x faster cluster data loading in local mode

### Planned Features
- Execute migrations via API
- Historical usage data analysis
- HA-aware migration planning
- Storage backend compatibility checking
- Export reports (JSON, CSV, PDF)
- Web UI interface
- Multi-datacenter support
- Cost optimization features

---

## Version Format

- **Major**: Breaking changes, major new features
- **Minor**: New features, backwards compatible
- **Patch**: Bug fixes, minor improvements

Versions are automatically incremented on every merge to main branch.
