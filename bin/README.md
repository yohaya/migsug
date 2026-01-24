# Pre-compiled Binaries

This directory contains pre-compiled binaries for various platforms, automatically built by CI/CD pipeline.

## Directory Structure

```
bin/
├── linux-amd64/
│   ├── migsug          # Linux 64-bit Intel/AMD (for Proxmox)
│   └── checksums.txt
├── linux-arm64/
│   ├── migsug          # Linux 64-bit ARM
│   └── checksums.txt
├── darwin-amd64/
│   ├── migsug          # macOS Intel
│   └── checksums.txt
├── darwin-arm64/
│   ├── migsug          # macOS Apple Silicon (M1/M2/M3)
│   └── checksums.txt
└── windows-amd64/
    ├── migsug.exe      # Windows 64-bit
    └── checksums.txt
```

## Quick Start

### Linux/Proxmox (Most Common)
```bash
chmod +x bin/linux-amd64/migsug
./bin/linux-amd64/migsug --version

# Install system-wide
sudo cp bin/linux-amd64/migsug /usr/local/bin/migsug
migsug --api-token=YOUR_TOKEN
```

### macOS (Apple Silicon)
```bash
chmod +x bin/darwin-arm64/migsug
./bin/darwin-arm64/migsug --version
```

### macOS (Intel)
```bash
chmod +x bin/darwin-amd64/migsug
./bin/darwin-amd64/migsug --version
```

### Windows
```powershell
.\bin\windows-amd64\migsug.exe --version
```

## Usage

After making executable, run with your Proxmox credentials:

```bash
# Linux/Proxmox
./bin/linux-amd64/migsug --api-token=root@pam!mytoken=secret

# Or if installed system-wide
migsug --api-token=root@pam!mytoken=secret
```

See main README.md for full documentation and usage examples.

## Verification

Each platform directory includes a `checksums.txt` file with SHA256 checksums.

```bash
# Verify integrity
cd bin/linux-amd64
sha256sum -c checksums.txt
```

## Build Info

These binaries are automatically built on every commit to main branch with:
- **Version number** from VERSION file
- **Git commit hash** for traceability
- **Build timestamp** for version tracking

Check version and build info:
```bash
./bin/linux-amd64/migsug --version
# Output:
# migsug version 1.0.1
# Build time: 2026-01-24T21:30:00Z
# Git commit: abc1234
```

## Platform Selection Guide

| Platform | File | Use Case |
|----------|------|----------|
| **Proxmox/Linux Server** | `linux-amd64/migsug` | Most common - Proxmox hosts |
| **Raspberry Pi/ARM Linux** | `linux-arm64/migsug` | ARM-based Linux systems |
| **Mac (M1/M2/M3)** | `darwin-arm64/migsug` | Apple Silicon Macs |
| **Mac (Intel)** | `darwin-amd64/migsug` | Intel-based Macs |
| **Windows** | `windows-amd64/migsug.exe` | Windows development |

## Updating

To get the latest binaries:

```bash
git pull origin main
chmod +x bin/linux-amd64/migsug
./bin/linux-amd64/migsug --version
```

## Download from GitHub Releases

Alternatively, download from releases:
https://github.com/yohaya/migsug/releases/latest

## Need Help?

See the main [README.md](../README.md) for:
- Full usage documentation
- Configuration options
- Troubleshooting guide
- Development instructions
