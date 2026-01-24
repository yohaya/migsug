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
