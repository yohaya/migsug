# Pre-compiled Binaries

This directory contains pre-compiled binaries for various platforms, automatically built by CI/CD pipeline.

## Available Binaries

- `migsug-linux-amd64` - Linux 64-bit (Intel/AMD) - **For Proxmox hosts**
- `migsug-linux-arm64` - Linux 64-bit (ARM)
- `migsug-darwin-amd64` - macOS Intel
- `migsug-darwin-arm64` - macOS Apple Silicon (M1/M2/M3)
- `migsug-windows-amd64.exe` - Windows 64-bit

## Quick Start

### Linux/Proxmox
```bash
chmod +x bin/migsug-linux-amd64
./bin/migsug-linux-amd64 --version

# Optional: Install system-wide
sudo cp bin/migsug-linux-amd64 /usr/local/bin/migsug
```

### macOS
```bash
chmod +x bin/migsug-darwin-arm64  # or darwin-amd64 for Intel
./bin/migsug-darwin-arm64 --version
```

### Windows
```powershell
.\bin\migsug-windows-amd64.exe --version
```

## Usage

After making executable, run with your Proxmox credentials:

```bash
./bin/migsug-linux-amd64 --api-token=root@pam!mytoken=secret
```

See main README.md for full documentation.

## Verification

SHA256 checksums are available in `checksums.txt`.

```bash
cd bin
sha256sum -c checksums.txt
```

## Build Info

These binaries are automatically built on every commit to main branch with:
- Version number from VERSION file
- Git commit hash
- Build timestamp

Check version: `./migsug-* --version`
