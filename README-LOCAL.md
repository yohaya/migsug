# Running migsug Locally on Proxmox Host

This guide explains how to run `migsug` directly on a Proxmox host without needing API credentials.

## Overview

`migsug` can run in two modes:

1. **Local Mode** (Recommended on Proxmox hosts)
   - Uses `pvesh` command-line tool
   - No API credentials required
   - Runs directly on Proxmox host as root
   - Faster and more secure

2. **Remote Mode** (For remote management)
   - Uses HTTP API
   - Requires API token or username/password
   - Can run from any machine with network access

## Local Mode (No Credentials Needed)

### Prerequisites

- Running on a Proxmox VE host
- Root privileges (or sudo)
- `pvesh` command available (installed with Proxmox)

### Installation on Proxmox Host

```bash
# Download the binary (or git clone)
wget https://github.com/yohaya/migsug/releases/latest/download/migsug-linux-amd64
chmod +x migsug-linux-amd64
sudo mv migsug-linux-amd64 /usr/local/bin/migsug

# Or clone the repository (binaries included)
git clone https://github.com/yohaya/migsug.git
cd migsug
chmod +x bin/linux-amd64/migsug
sudo cp bin/linux-amd64/migsug /usr/local/bin/migsug
```

### Usage

Simply run the command as root:

```bash
# As root
migsug

# Or with sudo
sudo migsug
```

**That's it!** No credentials needed. The tool automatically detects that it's running on a Proxmox host and uses local `pvesh` commands.

### What Happens Behind the Scenes

When you run `migsug` on a Proxmox host, it:

1. Detects `/etc/pve` directory (Proxmox cluster filesystem)
2. Checks for `pvesh` command availability
3. Automatically switches to "shell client" mode
4. Uses `pvesh` to access local Proxmox API
5. No network calls, no authentication required

### Example Session

```bash
root@pve1:~# migsug
Detected Proxmox host - using local pvesh commands (no credentials needed)
Running on Proxmox host: pve1
Connecting to Proxmox...
Loading cluster data...
Loaded cluster with 3 nodes and 25 VMs

[TUI launches with cluster overview]
```

## Remote Mode (API Credentials Required)

If you're running `migsug` from a different machine (not a Proxmox host), you need to provide credentials.

### Method 1: Interactive Prompt (Easiest)

Just run `migsug` and it will ask for credentials:

```bash
migsug

=== Proxmox Authentication ===
No credentials provided. Please enter your Proxmox credentials.
(Or press Ctrl+C to see other authentication options)

Username (e.g., root@pam): root@pam
Password: ********
```

### Method 2: Command-Line Flags

```bash
# With username/password
migsug --username=root@pam --password=yourpassword --api-host=https://192.168.1.100:8006

# With API token (more secure)
migsug --api-token=root@pam!mytoken=12345678-1234-1234-1234-123456789012 --api-host=https://192.168.1.100:8006
```

### Method 3: Environment Variables

```bash
# Set environment variables
export PVE_USERNAME="root@pam"
export PVE_PASSWORD="yourpassword"
export PVE_API_HOST="https://192.168.1.100:8006"

# Or with API token
export PVE_API_TOKEN="root@pam!mytoken=12345678-1234-1234-1234-123456789012"

# Then run
migsug
```

## Creating a Proxmox API Token

For remote access, API tokens are more secure than passwords:

1. Log into Proxmox web interface
2. Go to **Datacenter** → **Permissions** → **API Tokens**
3. Click **Add**
4. Fill in:
   - User: `root@pam`
   - Token ID: `migsug`
   - Uncheck "Privilege Separation"
5. Click **Add**
6. **Save the token secret** (shown only once)

Use the token like this:
```bash
migsug --api-token=root@pam!migsug=YOUR-SECRET-HERE
```

## Comparison: Local vs Remote

| Feature | Local Mode | Remote Mode |
|---------|-----------|-------------|
| **Location** | On Proxmox host | Any machine |
| **Credentials** | None needed | API token or password |
| **Speed** | Faster (no network) | Network latency |
| **Security** | Most secure | Requires network access |
| **Setup** | Copy binary, run | Configure credentials |
| **Best for** | Quick migrations on host | Remote management |

## Troubleshooting

### "pvesh: command not found"

You're not on a Proxmox host. Use remote mode with credentials:

```bash
migsug --api-token=YOUR-TOKEN --api-host=https://your-proxmox:8006
```

### "Permission denied"

You need to run as root or with sudo:

```bash
sudo migsug
```

### "Failed to connect to Proxmox"

**Local mode:**
- Check if you're root: `whoami`
- Verify pvesh works: `pvesh get /version`
- Check Proxmox services: `systemctl status pve-cluster`

**Remote mode:**
- Verify API host URL: `curl -k https://your-proxmox:8006/api2/json/version`
- Check firewall: Port 8006 must be open
- Test credentials: Try logging into web interface

## Security Recommendations

1. **On Proxmox hosts**: Always use local mode (no credentials)
2. **Remote access**: Use API tokens instead of passwords
3. **API tokens**: Create separate tokens with minimal permissions
4. **Environment variables**: Don't commit `.env` files with credentials
5. **Avoid flags**: Don't use `--password` flag (shows in process list)

## Advanced: Automated Migrations

You can use `migsug` in scripts on Proxmox hosts:

```bash
#!/bin/bash
# run-migrations.sh

# This script runs on Proxmox host, no credentials needed
if [ "$(id -u)" -ne 0 ]; then
    echo "Must run as root"
    exit 1
fi

# Run migsug (interactive TUI)
migsug

# Or in the future when CLI mode is added:
# migsug --auto --source=pve1 --migrate-count=5
```

## Performance

Local mode is significantly faster:

- **Local mode**: ~100ms per API call (direct pvesh)
- **Remote mode**: ~500ms+ per API call (network + HTTPS)

For large clusters, local mode can load data 5-10x faster.

## See Also

- [Main README](README.md) - Full documentation
- [Quick Start Guide](QUICKSTART.md) - Getting started
- [GitHub Releases](https://github.com/yohaya/migsug/releases) - Download binaries
