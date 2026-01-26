package proxmox

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client represents a Proxmox API client
type Client struct {
	BaseURL    string
	HTTPClient *http.Client
	AuthToken  string
	Username   string
	Password   string
	ticket     string
	csrfToken  string
}

// NewClient creates a new Proxmox API client
func NewClient(baseURL, authToken string) *Client {
	// Skip TLS verification for localhost/self-signed certs
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

	return &Client{
		BaseURL: strings.TrimSuffix(baseURL, "/"),
		HTTPClient: &http.Client{
			Timeout:   30 * time.Second,
			Transport: transport,
		},
		AuthToken: authToken,
	}
}

// NewClientWithCredentials creates a new client with username/password
func NewClientWithCredentials(baseURL, username, password string) *Client {
	client := NewClient(baseURL, "")
	client.Username = username
	client.Password = password
	return client
}

// Authenticate obtains a ticket and CSRF token using username/password
func (c *Client) Authenticate() error {
	if c.Username == "" || c.Password == "" {
		return fmt.Errorf("username and password required for authentication")
	}

	data := url.Values{}
	data.Set("username", c.Username)
	data.Set("password", c.Password)

	resp, err := c.HTTPClient.PostForm(
		c.BaseURL+"/api2/json/access/ticket",
		data,
	)
	if err != nil {
		return fmt.Errorf("authentication request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("authentication failed: status %d", resp.StatusCode)
	}

	var result struct {
		Data struct {
			Ticket              string `json:"ticket"`
			CSRFPreventionToken string `json:"CSRFPreventionToken"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to decode auth response: %w", err)
	}

	c.ticket = result.Data.Ticket
	c.csrfToken = result.Data.CSRFPreventionToken

	return nil
}

// doRequest performs an HTTP request with authentication
func (c *Client) doRequest(method, path string) (*http.Response, error) {
	url := c.BaseURL + path

	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add authentication
	if c.ticket != "" {
		// Using ticket-based auth
		req.Header.Set("Cookie", "PVEAuthCookie="+c.ticket)
		if method != "GET" {
			req.Header.Set("CSRFPreventionToken", c.csrfToken)
		}
	} else if c.AuthToken != "" {
		// Using API token
		req.Header.Set("Authorization", "PVEAPIToken="+c.AuthToken)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("unauthorized: check credentials or token")
	}

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	return resp, nil
}

// GetClusterResources retrieves all cluster resources
func (c *Client) GetClusterResources() ([]ClusterResource, error) {
	resp, err := c.doRequest("GET", "/api2/json/cluster/resources")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result APIResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Convert the data to []ClusterResource
	data, err := json.Marshal(result.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal data: %w", err)
	}

	var resources []ClusterResource
	if err := json.Unmarshal(data, &resources); err != nil {
		return nil, fmt.Errorf("failed to unmarshal resources: %w", err)
	}

	return resources, nil
}

// GetNodeStatus retrieves detailed status for a specific node
func (c *Client) GetNodeStatus(node string) (*NodeStatus, error) {
	path := fmt.Sprintf("/api2/json/nodes/%s/status", node)
	resp, err := c.doRequest("GET", path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result APIResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Handle the response with flexible parsing
	rawData, ok := result.Data.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected response format")
	}

	status := &NodeStatus{}

	// Extract cpuinfo if present
	if cpuinfo, ok := rawData["cpuinfo"].(map[string]interface{}); ok {
		if model, ok := cpuinfo["model"].(string); ok {
			status.CPUInfo.Model = model
		}
		if sockets, ok := cpuinfo["sockets"].(float64); ok {
			status.CPUInfo.Sockets = int(sockets)
		}
		if cpus, ok := cpuinfo["cpus"].(float64); ok {
			status.CPUInfo.CPUs = int(cpus)
		}
		if cores, ok := cpuinfo["cores"].(float64); ok {
			status.CPUInfo.Cores = int(cores)
		}
		// MHz can be float64 or string
		if mhz, ok := cpuinfo["mhz"].(float64); ok {
			status.CPUInfo.MHz = mhz
		} else if mhzStr, ok := cpuinfo["mhz"].(string); ok {
			fmt.Sscanf(mhzStr, "%f", &status.CPUInfo.MHz)
		}
	}

	// Extract uptime
	if uptime, ok := rawData["uptime"].(float64); ok {
		status.Uptime = int64(uptime)
	}

	return status, nil
}

// GetVMStatus retrieves detailed status for a specific VM
func (c *Client) GetVMStatus(node string, vmid int) (*VMStatus, error) {
	path := fmt.Sprintf("/api2/json/nodes/%s/qemu/%d/status/current", node, vmid)
	resp, err := c.doRequest("GET", path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result APIResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	data, err := json.Marshal(result.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal data: %w", err)
	}

	var status VMStatus
	if err := json.Unmarshal(data, &status); err != nil {
		return nil, fmt.Errorf("failed to unmarshal VM status: %w", err)
	}

	return &status, nil
}

// GetNodes retrieves a list of all nodes in the cluster
func (c *Client) GetNodes() ([]string, error) {
	resp, err := c.doRequest("GET", "/api2/json/nodes")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result APIResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	data, err := json.Marshal(result.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal data: %w", err)
	}

	var nodes []struct {
		Node string `json:"node"`
	}
	if err := json.Unmarshal(data, &nodes); err != nil {
		return nil, fmt.Errorf("failed to unmarshal nodes: %w", err)
	}

	nodeNames := make([]string, len(nodes))
	for i, n := range nodes {
		nodeNames[i] = n.Node
	}

	return nodeNames, nil
}

// Ping tests the connection to the Proxmox API
func (c *Client) Ping() error {
	resp, err := c.doRequest("GET", "/api2/json/version")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}
