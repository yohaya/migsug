package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yourusername/migsug/internal/proxmox"
	"github.com/yourusername/migsug/internal/ui"
	"golang.org/x/term"
)

var (
	apiToken   = flag.String("api-token", "", "Proxmox API token (format: user@realm!tokenid=secret)")
	apiHost    = flag.String("api-host", "https://localhost:8006", "Proxmox API host URL")
	username   = flag.String("username", "", "Proxmox username (alternative to API token)")
	password   = flag.String("password", "", "Proxmox password (alternative to API token)")
	sourceNode = flag.String("source", "", "Source node to migrate from (optional, can select in UI)")
	debug      = flag.Bool("debug", false, "Enable debug logging")
	version    = flag.Bool("version", false, "Show version information")
)

// Version is set at build time via -ldflags
var appVersion = "dev"
var buildTime = "unknown"
var gitCommit = "unknown"

// promptForInput prompts the user for text input
func promptForInput(prompt string) string {
	reader := bufio.NewReader(os.Stdin)
	fmt.Print(prompt)
	input, _ := reader.ReadString('\n')
	return strings.TrimSpace(input)
}

// promptForPassword prompts the user for password input (hidden)
func promptForPassword(prompt string) string {
	fmt.Print(prompt)
	bytePassword, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println() // Add newline after password input
	if err != nil {
		return ""
	}
	return string(bytePassword)
}

func main() {
	flag.Parse()

	// Show version
	if *version {
		fmt.Printf("migsug version %s\n", appVersion)
		fmt.Printf("Build time: %s\n", buildTime)
		fmt.Printf("Git commit: %s\n", gitCommit)
		os.Exit(0)
	}

	// Set up logging
	if *debug {
		logFile, err := os.OpenFile("migsug.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err != nil {
			log.Fatal("Failed to open log file:", err)
		}
		defer logFile.Close()
		log.SetOutput(logFile)
	} else {
		log.SetOutput(io.Discard)
	}

	// Create Proxmox client
	var client proxmox.ProxmoxClient

	// Check if running on Proxmox host - if so, use shell client (no credentials needed)
	if proxmox.IsProxmoxHost() {
		fmt.Println("Detected Proxmox host - using local pvesh commands (no credentials needed)")
		log.Println("Using shell client with pvesh")
		client = proxmox.NewShellClient()

		// Get current hostname
		if hostname, err := proxmox.GetHostname(); err == nil {
			fmt.Printf("Running on Proxmox host: %s\n", hostname)
			log.Printf("Hostname: %s\n", hostname)
		}
	} else {
		// Not on Proxmox host - require API credentials
		fmt.Println("Not running on Proxmox host - API credentials required")

		// Check for authentication
		if *apiToken == "" && (*username == "" || *password == "") {
			// Try environment variables
			*apiToken = os.Getenv("PVE_API_TOKEN")
			if *apiToken == "" {
				*username = os.Getenv("PVE_USERNAME")
				*password = os.Getenv("PVE_PASSWORD")
			}
		}

		// If still no credentials, prompt interactively
		if *apiToken == "" && (*username == "" || *password == "") {
			fmt.Println("\n=== Proxmox Authentication ===")
			fmt.Println("No credentials provided. Please enter your Proxmox credentials.")
			fmt.Println("(Or press Ctrl+C to see other authentication options)")
			fmt.Println()

			// Prompt for username
			if *username == "" {
				*username = promptForInput("Username (e.g., root@pam): ")
			}

			// Prompt for password
			if *password == "" && *username != "" {
				*password = promptForPassword("Password: ")
			}

			// Check if user cancelled or provided empty credentials
			if *username == "" || *password == "" {
				fmt.Println("\nAuthentication cancelled or incomplete.")
				fmt.Println("\nAuthentication options:")
				fmt.Println("  1. Run on Proxmox host as root (no credentials needed)")
				fmt.Println("  2. Interactive prompt (just run 'migsug' and enter credentials)")
				fmt.Println("  3. Use API token: --api-token=user@realm!tokenid=secret")
				fmt.Println("  4. Use username/password flags: --username=root@pam --password=secret")
				fmt.Println("  5. Set environment variables: PVE_API_TOKEN or PVE_USERNAME + PVE_PASSWORD")
				fmt.Println("\nExamples:")
				fmt.Println("  # Interactive (easiest)")
				fmt.Println("  migsug")
				fmt.Println()
				fmt.Println("  # With API token")
				fmt.Println("  migsug --api-token=root@pam!mytoken=12345678-1234-1234-1234-123456789012")
				fmt.Println()
				fmt.Println("  # On Proxmox host")
				fmt.Println("  ssh root@proxmox-host")
				fmt.Println("  migsug")
				os.Exit(1)
			}
		}

		// Create API-based client
		if *apiToken != "" {
			client = proxmox.NewClient(*apiHost, *apiToken)
			log.Println("Using API token authentication")
		} else {
			client = proxmox.NewClientWithCredentials(*apiHost, *username, *password)
			log.Println("Using username/password authentication")

			// Authenticate
			fmt.Println("Authenticating...")
			if err := client.Authenticate(); err != nil {
				fmt.Printf("Authentication failed: %v\n", err)
				os.Exit(1)
			}
		}
	}

	// Test connection
	fmt.Println("Connecting to Proxmox...")
	if err := client.Ping(); err != nil {
		fmt.Printf("Failed to connect to Proxmox: %v\n", err)
		if _, ok := client.(*proxmox.ShellClient); ok {
			fmt.Println("\nTroubleshooting:")
			fmt.Println("  • Ensure you're running as root")
			fmt.Println("  • Check that pvesh command is available")
			fmt.Println("  • Verify Proxmox services are running")
		} else {
			fmt.Println("\nTroubleshooting:")
			fmt.Println("  • Check that the API host is correct:", *apiHost)
			fmt.Println("  • Verify that the Proxmox API is accessible")
			fmt.Println("  • Ensure your credentials are valid")
		}
		os.Exit(1)
	}

	// Collect cluster data with progress bar
	fmt.Println("Loading cluster data...")
	startTime := time.Now()
	cluster, err := proxmox.CollectClusterDataWithProgress(client, func(stage string, current, total int) {
		elapsed := time.Since(startTime).Seconds()
		if total > 0 {
			// Calculate percentage
			percent := float64(current) / float64(total) * 100
			// Create progress bar
			barWidth := 30
			filled := int(float64(barWidth) * float64(current) / float64(total))
			bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
			// Print progress with elapsed time (use \r to overwrite line)
			fmt.Printf("\r  %s: [%s] %d/%d (%.0f%%) %.0fs  ", stage, bar, current, total, percent, elapsed)
		} else {
			fmt.Printf("\r  %s... %.0fs                                    ", stage, elapsed)
		}
	})
	fmt.Println() // New line after progress
	if err != nil {
		fmt.Printf("Failed to collect cluster data: %v\n", err)
		os.Exit(1)
	}

	if len(cluster.Nodes) == 0 {
		fmt.Println("No nodes found in cluster")
		os.Exit(1)
	}

	log.Printf("Loaded cluster with %d nodes and %d VMs\n", len(cluster.Nodes), cluster.TotalVMs)

	// Create and run TUI
	model := ui.NewModelWithVersion(cluster, client, appVersion)

	// If source node is specified, pre-select it
	if *sourceNode != "" {
		for i, node := range cluster.Nodes {
			if node.Name == *sourceNode {
				// We can't easily set the internal state here without modifying the model
				// So we'll just log it
				log.Printf("Source node specified: %s (index %d)\n", *sourceNode, i)
				break
			}
		}
	}

	p := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error running application: %v\n", err)
		os.Exit(1)
	}
}
