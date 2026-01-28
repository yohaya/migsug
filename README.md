# Proxmox VM Migration Suggester (migsug)

A powerful Terminal UI (TUI) application for analyzing and suggesting optimal VM migrations across Proxmox cluster nodes.

## Features

- 📊 **Interactive TUI** - Beautiful terminal interface that works over SSH
- 🎯 **Smart Analysis** - Intelligent algorithms for optimal VM placement
- 🔍 **Multiple Modes** - Migrate by VM count, vCPU, RAM, storage, or specific VMs
- 📈 **Before/After Preview** - See predicted cluster state after migration
- ⚡ **Fast & Lightweight** - Single binary with no dependencies
- 🔐 **Secure** - Supports API tokens and username/password authentication

## Screenshots

```
🖥️  Proxmox VM Migration Suggester

┌─ Cluster Summary ────────────────┐
│ Nodes:   3 online / 3 total      │
│ VMs:     25                       │
│ CPU:     45.2% average            │
│ RAM:     62.5% (128GB / 256GB)    │
│ Storage: 34.1%                    │
└───────────────────────────────────┘

Select source node to migrate from:

Name            Status   VMs   CPU        RAM         Storage
──────────────────────────────────────────────────────────────
→ pve1          online   12    78.5%      85.2%       45.3%
  pve2          online   8     42.1%      55.8%       38.7%
  pve3          online   5     25.3%      38.4%       22.1%
```

## Installation

### Prerequisites

- **Go 1.21+** (for building from source)
- **Proxmox VE 6.x or 7.x** cluster
- **API Token** or username/password for Proxmox API

### Option 1: Build from Source

```bash
# Clone the repository
git clone https://github.com/yourusername/migsug.git
cd migsug

# Download dependencies
make deps

# Build for Linux (Proxmox)
make build-linux

# Copy to Proxmox host
scp bin/linux-amd64/migsug root@proxmox:/usr/local/bin/migsug
ssh root@proxmox "chmod +x /usr/local/bin/migsug"
```

### Option 2: Download Binary

```bash
# Download latest release
curl -L https://github.com/yourusername/migsug/releases/latest/download/migsug-linux-amd64 -o /usr/local/bin/migsug
chmod +x /usr/local/bin/migsug
```

### Option 3: Install on Proxmox Directly

```bash
# If you have Go installed on Proxmox
git clone https://github.com/yourusername/migsug.git
cd migsug
make build
sudo make install
```

## Configuration

### Creating an API Token (Recommended)

1. Log into Proxmox web interface
2. Navigate to **Datacenter > Permissions > API Tokens**
3. Click **Add** and create a token for your user
4. **Important**: Save the token secret (shown only once)
5. Ensure the token has appropriate permissions (PVEAuditor minimum)

Example token format: `root@pam!mytoken=12345678-1234-1234-1234-123456789012`

### Authentication Methods

**Method 1: API Token (Recommended)**
```bash
migsug --api-token=root@pam!mytoken=secret
```

**Method 2: Username/Password**
```bash
migsug --username=root@pam --password=secret
```

**Method 3: Environment Variables**
```bash
export PVE_API_TOKEN="root@pam!mytoken=secret"
migsug

# Or with username/password
export PVE_USERNAME="root@pam"
export PVE_PASSWORD="secret"
migsug
```

## Usage

### Basic Usage

```bash
# Launch interactive TUI
migsug --api-token=root@pam!mytoken=secret

# Specify custom API host
migsug --api-host=https://pve.example.com:8006 --api-token=...

# Enable debug logging
migsug --api-token=... --debug
```

### Workflow

1. **Dashboard** - View cluster overview and select source node
2. **Criteria** - Choose migration mode and specify constraints:
   - **VM Count** - Migrate N VMs (e.g., 5 VMs)
   - **vCPU Count** - Migrate VMs totaling N vCPUs (e.g., 16 vCPUs)
   - **CPU Usage** - Migrate VMs using N% CPU (e.g., 30%)
   - **RAM Amount** - Migrate VMs using N GB RAM (e.g., 64 GB)
   - **Storage** - Migrate VMs using N GB storage (e.g., 500 GB)
   - **Specific VMs** - Select exact VMs to migrate
3. **Analysis** - Algorithm calculates optimal targets
4. **Results** - View suggestions and before/after comparison

### Keyboard Controls

| Key | Action |
|-----|--------|
| `↑` / `↓` or `j` / `k` | Navigate |
| `Enter` | Select / Confirm |
| `Space` | Toggle selection (VM picker) |
| `Tab` | Next field |
| `Esc` | Go back |
| `?` | Toggle help |
| `q` / `Ctrl+C` | Quit |
| `r` | New analysis (results view) |
| `s` | Save results (results view) |

## Examples

### Example 1: Balance Overloaded Node

**Scenario**: Node `pve1` is at 90% CPU and 85% RAM. Migrate 5 VMs to balance load.

```bash
migsug --api-token=root@pam!mytoken=secret

# In TUI:
# 1. Select pve1
# 2. Choose "VM Count"
# 3. Enter: 5
# Result: Suggests 5 least critical VMs to migrate to pve2 and pve3
```

### Example 2: Free Up RAM

**Scenario**: Need to free 64GB RAM on `pve2` for a large VM.

```bash
migsug --api-token=root@pam!mytoken=secret

# In TUI:
# 1. Select pve2
# 2. Choose "RAM Amount"
# 3. Enter: 64
# Result: Suggests VMs totaling ~64GB RAM to migrate
```

### Example 3: Migrate Specific VMs

**Scenario**: Migrate VMs 100, 105, and 110 from `pve3`.

```bash
migsug --api-token=root@pam!mytoken=secret

# In TUI:
# 1. Select pve3
# 2. Choose "Specific VMs"
# 3. Use Space to select VM 100, 105, 110
# 4. Press Enter
# Result: Suggests best target nodes for each VM
```

## Algorithm

The migration analyzer uses a sophisticated scoring algorithm:

1. **VM Selection** - Selects VMs based on criteria (least impactful first)
2. **Target Scoring** - Evaluates each target node considering:
   - Available CPU, RAM, and storage capacity
   - Current utilization levels
   - Resource balance (avoids creating new hotspots)
   - User constraints (excluded nodes, max VMs per host)
3. **Optimization** - Uses greedy bin-packing approach for distribution
4. **Prediction** - Calculates cluster state after migrations

**Scoring Formula**:
```
Score = 0.7 × (100 - UtilizationPercent) + 0.3 × BalanceScore

Where:
- UtilizationPercent = weighted average of CPU, RAM, storage usage
- BalanceScore = 100 - standard deviation of resource usage
- Higher score = better target
```

## Development

### Project Structure

```
migsug/
├── cmd/
│   └── migsug/
│       └── main.go              # Entry point
├── internal/
│   ├── proxmox/
│   │   ├── client.go            # Proxmox API client
│   │   ├── types.go             # Data structures
│   │   └── resources.go         # Resource collection
│   ├── analyzer/
│   │   ├── analyzer.go          # Migration algorithm
│   │   ├── constraints.go       # User constraints
│   │   └── suggestion.go        # Suggestion types
│   └── ui/
│       ├── app.go               # Main TUI app
│       ├── views/
│       │   ├── dashboard.go     # Dashboard view
│       │   ├── criteria.go      # Criteria input
│       │   └── results.go       # Results display
│       └── components/
│           ├── table.go         # Tables
│           ├── resourcebar.go   # Resource bars
│           └── summary.go       # Summary cards
├── go.mod
├── Makefile
└── README.md
```

### Building

```bash
# Build for current platform
make build

# Build for all platforms
make build-all

# Run tests
make test

# Run with arguments
make run ARGS="--api-token=..."

# Format code
make fmt

# Clean build artifacts
make clean
```

### Testing

```bash
# Run all tests
go test ./...

# Run with coverage
make test-coverage

# Test specific package
go test -v ./internal/analyzer
```

### Dependencies

- [Bubble Tea](https://github.com/charmbracelet/bubbletea) - TUI framework
- [Lip Gloss](https://github.com/charmbracelet/lipgloss) - Styling
- [Bubbles](https://github.com/charmbracelet/bubbles) - UI components

## Troubleshooting

### Connection Issues

**Error**: `Failed to connect to Proxmox API`

- Verify API host URL (default: `https://localhost:8006`)
- Check firewall rules allow API access
- Ensure Proxmox API service is running

### Authentication Issues

**Error**: `unauthorized: check credentials or token`

- Verify API token is correct and not expired
- Check token has sufficient permissions (PVEAuditor minimum)
- For username/password, ensure credentials are correct

### No Nodes Found

**Error**: `No nodes found in cluster`

- Check that you're connecting to a Proxmox cluster
- Verify your user has permissions to view cluster resources
- Ensure at least one node is online

### TLS Certificate Errors

If you see TLS verification errors, the application automatically skips verification for localhost and self-signed certificates. For production use, ensure proper CA certificates.

## Limitations

- **Read-Only**: Currently only suggests migrations, doesn't execute them
- **No HA Awareness**: Doesn't consider HA groups or dependencies (planned)
- **Storage Backend**: Doesn't analyze storage backend compatibility
- **Network**: Doesn't consider network configuration or bandwidth

## Roadmap

- [ ] Execute migrations via API
- [ ] Historical usage data analysis
- [ ] HA-aware migration planning
- [ ] Storage backend compatibility checking
- [ ] Export reports (JSON, CSV, PDF)
- [ ] Web UI interface
- [ ] Multi-datacenter support
- [ ] Cost optimization (energy, licensing)

## Contributing

Contributions are welcome! Please:

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## License

MIT License - see [LICENSE](LICENSE) file for details.

## Author

Created with ❤️ for the Proxmox community

## Acknowledgments

- [Proxmox VE](https://www.proxmox.com/) - Virtualization platform
- [Charm](https://charm.sh/) - Beautiful TUI libraries
- Proxmox community for inspiration and feedback

## CI/CD Pipeline

This repository includes automated CI/CD pipelines:

### Automatic Building
- **Every push to main**: Builds binaries for all platforms
- **Auto-versioning**: Automatically increments patch version
- **Commit binaries**: Compiled binaries are committed to `bin/` directory
- **GitHub Releases**: Creates release with binaries and checksums

### Available Binaries
After cloning the repository, binaries are immediately available in `bin/`:
```bash
git clone https://github.com/yohaya/migsug.git
cd migsug
chmod +x bin/linux-amd64/migsug
./bin/linux-amd64/migsug --version
```

### Version Management
Version is stored in `VERSION` file and automatically incremented:
- Format: `MAJOR.MINOR.PATCH`
- Default increment: `PATCH` (on every main branch commit)
- Manual increment: `bash scripts/increment-version.sh [major|minor|patch]`

### Build Information
Each binary includes:
- Version number from VERSION file
- Git commit hash
- Build timestamp

Check with: `./bin/migsug-* --version`

### GitHub Actions Workflows

**Build and Release** (`.github/workflows/build-and-release.yml`)
- Runs on: Push to main/develop, Pull Requests
- Actions:
  1. Run tests
  2. Increment version (main only)
  3. Build binaries for all platforms
  4. Commit binaries to repository
  5. Create GitHub Release
  6. Upload artifacts

**Test** (`.github/workflows/test.yml`)
- Runs on: All branches
- Actions:
  1. Run Go tests
  2. Check code coverage
  3. Verify code formatting
  4. Verify build

### Manual Build
```bash
# Build with version info
bash scripts/build-with-version.sh linux amd64

# Or use Makefile
make build-all
```

## Development Workflow

1. **Create feature branch**: `git checkout -b feature/my-feature`
2. **Make changes**: Edit code
3. **Test locally**: `make test`
4. **Commit changes**: `git commit -am "feat: add feature"`
5. **Push**: `git push origin feature/my-feature`
6. **Create PR**: GitHub will run tests automatically
7. **Merge to main**: Triggers build, version increment, and release

