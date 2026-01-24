# Quick Start Guide

## Prerequisites

Before you can build and run `migsug`, you need to install Go.

### Install Go

**macOS (using Homebrew):**
```bash
brew install go
```

**macOS (manual):**
```bash
# Download and install from https://go.dev/dl/
curl -OL https://go.dev/dl/go1.21.6.darwin-amd64.tar.gz
sudo tar -C /usr/local -xzf go1.21.6.darwin-amd64.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.zshrc
source ~/.zshrc
```

**Linux:**
```bash
# Download and install
wget https://go.dev/dl/go1.21.6.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf go1.21.6.linux-amd64.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
source ~/.bashrc
```

**Verify installation:**
```bash
go version
# Should output: go version go1.21.6 ...
```

## Build the Application

```bash
# 1. Navigate to the project directory
cd /Users/yohay/migsug

# 2. Download dependencies
go mod download
go mod tidy

# 3. Build for your platform
make build

# Or build for Linux (Proxmox)
make build-linux
```

## First Run (Development/Testing)

Since you probably don't have a Proxmox cluster running locally, you have a few options:

### Option 1: Mock Mode (Future Enhancement)
This would be a test mode with simulated data. Not implemented yet, but you could add it!

### Option 2: Connect to Real Proxmox
If you have access to a Proxmox cluster:

```bash
# Using API token
./bin/migsug --api-host=https://your-proxmox:8006 --api-token=root@pam!mytoken=secret

# Using username/password
./bin/migsug --api-host=https://your-proxmox:8006 --username=root@pam --password=yourpassword
```

### Option 3: Test Individual Components

Test the analyzer logic without Proxmox:

```go
// Create test/analyzer_test.go
package test

import (
    "testing"
    "github.com/yourusername/migsug/internal/proxmox"
    "github.com/yourusername/migsug/internal/analyzer"
)

func TestAnalyzer(t *testing.T) {
    // Create mock cluster data
    cluster := &proxmox.Cluster{
        Nodes: []proxmox.Node{
            {
                Name: "pve1",
                Status: "online",
                CPUCores: 8,
                CPUUsage: 0.8, // 80%
                MaxMem: 32 * 1024 * 1024 * 1024, // 32GB
                UsedMem: 28 * 1024 * 1024 * 1024, // 28GB
                VMs: []proxmox.VM{
                    {VMID: 100, Name: "test-vm-1", CPUCores: 2, UsedMem: 4 * 1024 * 1024 * 1024},
                    {VMID: 101, Name: "test-vm-2", CPUCores: 2, UsedMem: 4 * 1024 * 1024 * 1024},
                },
            },
            {
                Name: "pve2",
                Status: "online",
                CPUCores: 8,
                CPUUsage: 0.3, // 30%
                MaxMem: 32 * 1024 * 1024 * 1024,
                UsedMem: 10 * 1024 * 1024 * 1024,
                VMs: []proxmox.VM{},
            },
        },
    }

    // Test migration
    vmCount := 2
    constraints := analyzer.MigrationConstraints{
        SourceNode: "pve1",
        VMCount: &vmCount,
    }

    result, err := analyzer.Analyze(cluster, constraints)
    if err != nil {
        t.Fatalf("Analysis failed: %v", err)
    }

    if len(result.Suggestions) != 2 {
        t.Errorf("Expected 2 suggestions, got %d", len(result.Suggestions))
    }

    t.Logf("Analysis complete: %d suggestions", len(result.Suggestions))
    for _, sug := range result.Suggestions {
        t.Logf("VM %d (%s): %s -> %s", sug.VMID, sug.VMName, sug.SourceNode, sug.TargetNode)
    }
}
```

Run the test:
```bash
go test -v ./test
```

## Project Structure Overview

```
migsug/
├── cmd/migsug/main.go           # Entry point - parses CLI args, starts TUI
├── internal/
│   ├── proxmox/                 # Proxmox API integration
│   │   ├── client.go            # HTTP client for API calls
│   │   ├── types.go             # Data structures (Node, VM, Cluster)
│   │   └── resources.go         # Resource collection functions
│   ├── analyzer/                # Migration analysis engine
│   │   ├── analyzer.go          # Main algorithm
│   │   ├── constraints.go       # User input constraints
│   │   └── suggestion.go        # Suggestion data structures
│   └── ui/                      # Terminal user interface
│       ├── app.go               # Main Bubble Tea app
│       ├── views/               # Different screens
│       └── components/          # Reusable UI components
├── go.mod                       # Go module definition
├── Makefile                     # Build commands
└── README.md                    # Full documentation
```

## Next Steps

1. **Install Go** (if not already installed)
2. **Download dependencies**: `go mod download`
3. **Build the app**: `make build`
4. **Test with mock data** (create test cases)
5. **Connect to Proxmox** (when ready)
6. **Deploy to Proxmox host**: `make build-linux && scp bin/migsug-linux-amd64 root@proxmox:/usr/local/bin/migsug`

## Troubleshooting

### "command not found: go"
Go is not installed or not in PATH. Follow the installation steps above.

### "package github.com/charmbracelet/bubbletea is not in GOROOT"
Dependencies not downloaded. Run: `go mod download`

### Build errors about missing imports
Run: `go mod tidy` to fix module dependencies.

## Development Tips

### Hot Reload During Development
```bash
# Install air for hot reload
go install github.com/cosmtrek/air@latest

# Run with hot reload
air
```

### Debug Mode
```bash
# Enable debug logging
./bin/migsug --debug --api-token=...

# Check logs
tail -f migsug.log
```

### Test UI Without Proxmox
Create a mock data file and modify `main.go` to load it instead of calling the API.

## Support

- **GitHub Issues**: https://github.com/yourusername/migsug/issues
- **Documentation**: See README.md for full documentation
- **Proxmox Forums**: https://forum.proxmox.com/
