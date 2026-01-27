# Migration Guide: New Binary Structure

## What Changed

The binary structure has been reorganized for better clarity:

### Old Structure
```
bin/
├── migsug-linux-amd64
├── migsug-linux-arm64
├── migsug-darwin-amd64
├── migsug-darwin-arm64
├── migsug-windows-amd64.exe
└── checksums.txt
```

### New Structure
```
bin/
├── linux-amd64/
│   ├── migsug
│   └── checksums.txt
├── linux-arm64/
│   ├── migsug
│   └── checksums.txt
├── darwin-amd64/
│   ├── migsug
│   └── checksums.txt
├── darwin-arm64/
│   ├── migsug
│   └── checksums.txt
└── windows-amd64/
    ├── migsug.exe
    └── checksums.txt
```

## Benefits

1. **Cleaner structure** - Each platform in its own directory
2. **Consistent naming** - All executables named simply "migsug"
3. **Platform-specific checksums** - Easier verification
4. **Better organization** - Scales better with more platforms

## Update Your Scripts

If you have scripts that reference the old paths, update them:

### Old
```bash
./bin/migsug-linux-amd64 --version
scp bin/migsug-linux-amd64 root@proxmox:/usr/local/bin/migsug
```

### New
```bash
./bin/linux-amd64/migsug --version
scp bin/linux-amd64/migsug root@proxmox:/usr/local/bin/migsug
```

## Quick Reference

| Old Path | New Path |
|----------|----------|
| `bin/migsug-linux-amd64` | `bin/linux-amd64/migsug` |
| `bin/migsug-linux-arm64` | `bin/linux-arm64/migsug` |
| `bin/migsug-darwin-amd64` | `bin/darwin-amd64/migsug` |
| `bin/migsug-darwin-arm64` | `bin/darwin-arm64/migsug` |
| `bin/migsug-windows-amd64.exe` | `bin/windows-amd64/migsug.exe` |

## No Action Required

If you're cloning fresh from the repository, no action needed! The new structure is already in place.

## Questions?

Open an issue on GitHub: https://github.com/yohaya/migsug/issues
