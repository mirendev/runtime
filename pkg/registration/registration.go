package registration

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"miren.dev/runtime/pkg/cloudauth"
)

// Config contains the configuration for cluster registration
type Config struct {
	ClusterName    string            `json:"cluster_name"`
	OrganizationID string            `json:"organization_id,omitempty"` // Optional - user will select in UI
	Tags           map[string]string `json:"tags,omitempty"`
	PublicKey      string            `json:"public_key,omitempty"` // PEM encoded public key
}

// GenerateKeyPair generates a new ED25519 key pair for registration
func GenerateKeyPair() (privateKey string, publicKey string, err error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate key pair: %w", err)
	}

	// Marshal private key
	privKeyBytes, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return "", "", fmt.Errorf("failed to marshal private key: %w", err)
	}

	privBlock := &pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: privKeyBytes,
	}
	privateKey = string(pem.EncodeToMemory(privBlock))

	// Marshal public key
	pubKeyBytes, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return "", "", fmt.Errorf("failed to marshal public key: %w", err)
	}

	pubBlock := &pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pubKeyBytes,
	}
	publicKey = string(pem.EncodeToMemory(pubBlock))

	return privateKey, publicKey, nil
}

// Result contains the result of registration initiation
type Result struct {
	RegistrationID string    `json:"registration_id"`
	AuthURL        string    `json:"auth_url"`
	PollURL        string    `json:"poll_url"`
	ExpiresAt      time.Time `json:"expires_at"`
}

// Status represents the status of a registration during polling
type Status struct {
	Status           string `json:"status"`
	ClusterID        string `json:"cluster_id,omitempty"`
	OrganizationID   string `json:"organization_id,omitempty"`
	ServiceAccountID string `json:"service_account_id,omitempty"`
}

// StoredRegistration contains the registration data stored on disk
type StoredRegistration struct {
	ClusterID        string            `json:"cluster_id"`
	ClusterName      string            `json:"cluster_name"`
	OrganizationID   string            `json:"organization_id"`
	ServiceAccountID string            `json:"service_account_id"`
	PrivateKey       string            `json:"private_key"` // PEM encoded private key
	CloudURL         string            `json:"cloud_url"`
	RegisteredAt     time.Time         `json:"registered_at"`
	Tags             map[string]string `json:"tags,omitempty"`

	// Pending registration fields
	Status         string    `json:"status,omitempty"` // "pending" or "approved"
	RegistrationID string    `json:"registration_id,omitempty"`
	PollURL        string    `json:"poll_url,omitempty"`
	ExpiresAt      time.Time `json:"expires_at,omitempty"`
}

// Client handles the cluster registration flow
type Client struct {
	managementURL string
	config        Config
	httpClient    *http.Client
}

// NewClient creates a new registration client
func NewClient(managementURL string, config Config) *Client {
	if managementURL == "" {
		managementURL = cloudauth.DefaultCloudURL
	}
	return &Client{
		managementURL: managementURL,
		config:        config,
		httpClient:    &http.Client{Timeout: 30 * time.Second},
	}
}

// StartRegistration initiates the registration process
func (c *Client) StartRegistration(ctx context.Context) (*Result, error) {
	url := fmt.Sprintf("%s/api/v1/clusters/register/initiate", c.managementURL)

	jsonData, err := json.Marshal(c.config)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal config: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errResp map[string]string
		json.NewDecoder(resp.Body).Decode(&errResp)
		return nil, fmt.Errorf("registration failed: %s", errResp["error"])
	}

	var result Result
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// PollForApproval polls the registration status until approved or timeout
func (c *Client) PollForApproval(ctx context.Context, pollURL string, pollInterval time.Duration) (*Status, error) {
	return c.PollForApprovalWithCallback(ctx, pollURL, pollInterval, nil)
}

// PollForApprovalWithCallback polls the registration status with optional progress callback
func (c *Client) PollForApprovalWithCallback(ctx context.Context, pollURL string, pollInterval time.Duration, progressCallback func()) (*Status, error) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("polling timeout: %w", ctx.Err())
		case <-ticker.C:
			status, err := c.checkRegistrationStatus(ctx, pollURL)
			if err != nil {
				// Continue polling on error (could be temporary network issue)
				continue
			}

			switch status.Status {
			case "approved":
				return status, nil
			case "rejected":
				return nil, fmt.Errorf("registration was rejected")
			case "pending":
				if progressCallback != nil {
					progressCallback()
				}
			default:
				return nil, fmt.Errorf("unexpected status: %s", status.Status)
			}
			// Continue polling for "pending" or other states
		}
	}
}

// checkRegistrationStatus checks the current status of a registration
func (c *Client) checkRegistrationStatus(ctx context.Context, pollURL string) (*Status, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", pollURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create poll request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send poll request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		// Registration not found yet, return pending status
		return &Status{Status: "pending"}, nil
	}

	if resp.StatusCode != http.StatusOK {
		var errResp map[string]string
		err = json.NewDecoder(resp.Body).Decode(&errResp)
		if err != nil {
			return nil, fmt.Errorf("failed to decode error response: %w", err)
		}

		errmsg := errResp["error"]
		if errmsg == "" {
			errmsg = "unknown error"
		}
		return nil, fmt.Errorf("poll request failed: %s", errmsg)
	}

	var status Status
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("failed to decode poll response: %w", err)
	}

	return &status, nil
}

// SaveRegistration saves the registration data to the specified directory
func SaveRegistration(dir string, reg *StoredRegistration) error {
	// Ensure directory exists
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Save to registration.json
	path := filepath.Join(dir, "registration.json")
	data, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal registration: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write registration file: %w", err)
	}

	// Also save the private key separately for easier access
	keyPath := filepath.Join(dir, "service-account.key")
	if err := os.WriteFile(keyPath, []byte(reg.PrivateKey), 0600); err != nil {
		return fmt.Errorf("failed to write private key: %w", err)
	}

	return nil
}

// LoadRegistration loads the registration data from the specified directory
func LoadRegistration(dir string) (*StoredRegistration, error) {
	path := filepath.Join(dir, "registration.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // No registration exists
		}
		return nil, fmt.Errorf("failed to read registration file: %w", err)
	}

	var reg StoredRegistration
	if err := json.Unmarshal(data, &reg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal registration: %w", err)
	}

	return &reg, nil
}

