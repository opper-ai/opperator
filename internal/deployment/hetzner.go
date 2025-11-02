package deployment

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

// HetznerClient wraps the Hetzner Cloud API client
type HetznerClient struct {
	client *hcloud.Client
}

// NewHetznerClient creates a new Hetzner Cloud API client
func NewHetznerClient(apiKey string) *HetznerClient {
	return &HetznerClient{
		client: hcloud.NewClient(hcloud.WithToken(apiKey)),
	}
}

// ValidateAPIKey validates that the API key is valid by making a test API call
func (h *HetznerClient) ValidateAPIKey(ctx context.Context) error {
	// Try to list server types as a lightweight validation check
	_, _, err := h.client.ServerType.List(ctx, hcloud.ServerTypeListOpts{
		ListOpts: hcloud.ListOpts{
			PerPage: 1,
		},
	})
	if err != nil {
		// Check if it's an authentication error
		if hcloud.IsError(err, hcloud.ErrorCodeUnauthorized) {
			return fmt.Errorf("invalid API key: unauthorized")
		}
		return fmt.Errorf("failed to validate API key: %w", err)
	}
	return nil
}

// ServerTypeOption represents a server type option for the UI
type ServerTypeOption struct {
	Name               string
	Description        string
	Cores              int
	Memory             float32 // GB
	Disk               int     // GB
	PriceMonthly       string  // Price as string from API
	AvailableLocations []string // Location names where this type is available
}

// GetServerTypes fetches available server types from Hetzner API
func (h *HetznerClient) GetServerTypes(ctx context.Context) ([]ServerTypeOption, error) {
	serverTypes, err := h.client.ServerType.All(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch server types: %w", err)
	}

	var options []ServerTypeOption
	for _, st := range serverTypes {
		// Filter to only show x86 architecture and exclude CCX types
		if st.Architecture == hcloud.ArchitectureX86 && !strings.HasPrefix(st.Name, "ccx") {
			// Get available locations for this server type
			var availableLocations []string
			for _, price := range st.Pricings {
				availableLocations = append(availableLocations, price.Location.Name)
			}

			option := ServerTypeOption{
				Name:               st.Name,
				Description:        st.Description,
				Cores:              st.Cores,
				Memory:             st.Memory,
				Disk:               st.Disk,
				PriceMonthly:       st.Pricings[0].Monthly.Gross,
				AvailableLocations: availableLocations,
			}
			options = append(options, option)
		}
	}

	// Sort by price (convert string to float for comparison)
	sort.Slice(options, func(i, j int) bool {
		priceI, _ := strconv.ParseFloat(options[i].PriceMonthly, 64)
		priceJ, _ := strconv.ParseFloat(options[j].PriceMonthly, 64)
		return priceI < priceJ
	})

	return options, nil
}

// LocationOption represents a location option for the UI
type LocationOption struct {
	Name        string
	City        string
	Country     string
	Description string
}

// GetLocations fetches available locations from Hetzner API
func (h *HetznerClient) GetLocations(ctx context.Context) ([]LocationOption, error) {
	locations, err := h.client.Location.All(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch locations: %w", err)
	}

	var options []LocationOption
	for _, loc := range locations {
		option := LocationOption{
			Name:        loc.Name,
			City:        loc.City,
			Country:     loc.Country,
			Description: fmt.Sprintf("%s, %s", loc.City, loc.Country),
		}
		options = append(options, option)
	}

	return options, nil
}

// ServerInfo contains information about a created server
type ServerInfo struct {
	ID         int64
	Name       string
	PublicIP   string
	Status     string
	Type       string
	Location   string
	CreatedAt  time.Time
	PrivateKey string // SSH private key for access
	SSHKeyName string // Name of SSH key in Hetzner
}

// CreateServer creates a new Hetzner Cloud server
func (h *HetznerClient) CreateServer(ctx context.Context, name, serverType, location string) (*ServerInfo, error) {
	// Get server type
	st, _, err := h.client.ServerType.Get(ctx, serverType)
	if err != nil {
		return nil, fmt.Errorf("failed to get server type: %w", err)
	}
	if st == nil {
		return nil, fmt.Errorf("server type '%s' not found", serverType)
	}

	// Get location
	loc, _, err := h.client.Location.Get(ctx, location)
	if err != nil {
		return nil, fmt.Errorf("failed to get location: %w", err)
	}
	if loc == nil {
		return nil, fmt.Errorf("location '%s' not found", location)
	}

	// Get Ubuntu 24.04 image
	image, _, err := h.client.Image.GetForArchitecture(ctx, "ubuntu-24.04", hcloud.ArchitectureX86)
	if err != nil {
		return nil, fmt.Errorf("failed to get Ubuntu image: %w", err)
	}
	if image == nil {
		return nil, fmt.Errorf("Ubuntu 24.04 image not found")
	}

	// Create SSH key for access
	sshKeyName := fmt.Sprintf("opperator-%s-%d", name, time.Now().Unix())
	publicKey, privateKey, err := generateSSHKeyPair()
	if err != nil {
		return nil, fmt.Errorf("failed to generate SSH key pair: %w", err)
	}

	sshKey, _, err := h.client.SSHKey.Create(ctx, hcloud.SSHKeyCreateOpts{
		Name:      sshKeyName,
		PublicKey: publicKey,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create SSH key: %w", err)
	}

	// Create server
	serverName := fmt.Sprintf("opperator-%s", name)
	res, _, err := h.client.Server.Create(ctx, hcloud.ServerCreateOpts{
		Name:       serverName,
		ServerType: st,
		Image:      image,
		Location:   loc,
		SSHKeys:    []*hcloud.SSHKey{sshKey},
		Labels: map[string]string{
			"managed-by": "opperator",
			"daemon":     name,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create server: %w", err)
	}

	// Wait for server to be running
	if err := h.waitForServer(ctx, res.Server.ID); err != nil {
		return nil, fmt.Errorf("failed waiting for server: %w", err)
	}

	// Get server details (refresh to get public IP)
	server, _, err := h.client.Server.GetByID(ctx, res.Server.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get server details: %w", err)
	}

	info := &ServerInfo{
		ID:         server.ID,
		Name:       server.Name,
		PublicIP:   server.PublicNet.IPv4.IP.String(),
		Status:     string(server.Status),
		Type:       server.ServerType.Name,
		Location:   server.Datacenter.Location.Name,
		CreatedAt:  server.Created,
		PrivateKey: privateKey,
		SSHKeyName: sshKeyName,
	}

	return info, nil
}

// DeleteServer deletes a Hetzner Cloud server by ID
func (h *HetznerClient) DeleteServer(ctx context.Context, serverID int64) error {
	server, _, err := h.client.Server.GetByID(ctx, serverID)
	if err != nil {
		return fmt.Errorf("failed to get server: %w", err)
	}
	if server == nil {
		return fmt.Errorf("server not found")
	}

	// Delete server
	result, _, err := h.client.Server.DeleteWithResult(ctx, server)
	if err != nil {
		return fmt.Errorf("failed to delete server: %w", err)
	}

	// Wait for deletion to complete
	if err := h.client.Action.WaitFor(ctx, result.Action); err != nil {
		return fmt.Errorf("failed waiting for server deletion: %w", err)
	}

	// Clean up SSH key if it exists
	sshKeys, err := h.client.SSHKey.AllWithOpts(ctx, hcloud.SSHKeyListOpts{
		ListOpts: hcloud.ListOpts{
			LabelSelector: fmt.Sprintf("managed-by=opperator"),
		},
	})
	if err == nil {
		for _, key := range sshKeys {
			h.client.SSHKey.Delete(ctx, key)
		}
	}

	return nil
}

// GetServer gets server information by ID
func (h *HetznerClient) GetServer(ctx context.Context, serverID int64) (*ServerInfo, error) {
	server, _, err := h.client.Server.GetByID(ctx, serverID)
	if err != nil {
		return nil, fmt.Errorf("failed to get server: %w", err)
	}
	if server == nil {
		return nil, fmt.Errorf("server not found")
	}

	return &ServerInfo{
		ID:        server.ID,
		Name:      server.Name,
		PublicIP:  server.PublicNet.IPv4.IP.String(),
		Status:    string(server.Status),
		Type:      server.ServerType.Name,
		Location:  server.Datacenter.Location.Name,
		CreatedAt: server.Created,
	}, nil
}

// waitForServer waits for a server to be running
func (h *HetznerClient) waitForServer(ctx context.Context, serverID int64) error {
	timeout := time.After(5 * time.Minute)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout:
			return fmt.Errorf("timeout waiting for server to be running")
		case <-ticker.C:
			server, _, err := h.client.Server.GetByID(ctx, serverID)
			if err != nil {
				return err
			}
			if server.Status == hcloud.ServerStatusRunning {
				return nil
			}
		}
	}
}
