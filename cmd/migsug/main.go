package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yourusername/migsug/internal/proxmox"
	"github.com/yourusername/migsug/internal/ui"
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

	// Check for authentication
	if *apiToken == "" && (*username == "" || *password == "") {
		// Try environment variables
		*apiToken = os.Getenv("PVE_API_TOKEN")
		if *apiToken == "" {
			*username = os.Getenv("PVE_USERNAME")
			*password = os.Getenv("PVE_PASSWORD")
		}
	}

	if *apiToken == "" && (*username == "" || *password == "") {
		fmt.Println("Error: Authentication required")
		fmt.Println("\nOptions:")
		fmt.Println("  1. Use API token: --api-token=user@realm!tokenid=secret")
		fmt.Println("  2. Use username/password: --username=root@pam --password=secret")
		fmt.Println("  3. Set environment variables: PVE_API_TOKEN or PVE_USERNAME + PVE_PASSWORD")
		fmt.Println("\nExample:")
		fmt.Println("  migsug --api-token=root@pam!mytoken=12345678-1234-1234-1234-123456789012")
		os.Exit(1)
	}

	// Create Proxmox client
	var client *proxmox.Client
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

	// Test connection
	fmt.Println("Connecting to Proxmox API...")
	if err := client.Ping(); err != nil {
		fmt.Printf("Failed to connect to Proxmox API: %v\n", err)
		fmt.Println("\nTroubleshooting:")
		fmt.Println("  • Check that the API host is correct:", *apiHost)
		fmt.Println("  • Verify that the Proxmox API is accessible")
		fmt.Println("  • Ensure your credentials are valid")
		os.Exit(1)
	}

	// Collect cluster data
	fmt.Println("Loading cluster data...")
	cluster, err := proxmox.CollectClusterData(client)
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
	model := ui.NewModel(cluster, client)

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
