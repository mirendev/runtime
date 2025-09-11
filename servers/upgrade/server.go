package upgrade

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	upgcoord "miren.dev/runtime/components/upgrade"
	"miren.dev/runtime/version"
)

// Server handles upgrade-related operations for the miren server
type Server struct {
	log         *slog.Logger
	coordinator *upgcoord.Coordinator
	dataPath    string

	// Server state that needs to be preserved
	containerdSocket  string
	etcdEndpoints     []string
	clickhouseAddress string
	serverAddress     string
	runnerAddress     string
	runnerID          string
	mode              string
}

// NewServer creates a new upgrade server
func NewServer(log *slog.Logger, dataPath string) *Server {
	return &Server{
		log:         log.With("component", "upgrade-server"),
		coordinator: upgcoord.NewCoordinator(log, dataPath),
		dataPath:    dataPath,
	}
}

// SetServerState sets the current server state for preservation during upgrade
func (s *Server) SetServerState(
	containerdSocket string,
	etcdEndpoints []string,
	clickhouseAddress string,
	serverAddress string,
	runnerAddress string,
	runnerID string,
	mode string,
) {
	s.containerdSocket = containerdSocket
	s.etcdEndpoints = etcdEndpoints
	s.clickhouseAddress = clickhouseAddress
	s.serverAddress = serverAddress
	s.runnerAddress = runnerAddress
	s.runnerID = runnerID
	s.mode = mode
}

// GetVersion returns the current server version
func (s *Server) GetVersion(ctx context.Context) (string, error) {
	return version.Version, nil
}

// InitiateUpgrade starts the upgrade process
func (s *Server) InitiateUpgrade(ctx context.Context, newBinaryPath string, force bool) error {
	s.log.Info("initiating upgrade", "new_binary", newBinaryPath, "force", force)

	// Verify the new binary exists
	if _, err := os.Stat(newBinaryPath); err != nil {
		return fmt.Errorf("new binary not found: %w", err)
	}

	// Create handoff state
	handoffState := &upgcoord.HandoffState{
		ContainerdSocket:  s.containerdSocket,
		EtcdEndpoints:     s.etcdEndpoints,
		ClickHouseAddress: s.clickhouseAddress,
		ServerAddress:     s.serverAddress,
		RunnerAddress:     s.runnerAddress,
		RunnerID:          s.runnerID,
		DataPath:          s.dataPath,
		Mode:              s.mode,
	}

	// Note: Signal handling for SIGUSR1 is done in cli/commands/server.go
	// to avoid duplicate handlers. The server.go handler will call
	// HandleReadinessSignal() on the coordinator directly.

	// Initiate the upgrade
	if err := s.coordinator.InitiateUpgrade(ctx, newBinaryPath, handoffState); err != nil {
		return fmt.Errorf("upgrade failed: %w", err)
	}

	s.log.Info("upgrade initiated successfully, shutting down gracefully")

	// The coordinator has confirmed the new process is ready
	// Now we need to gracefully shutdown this process
	// This should be handled by the caller (e.g., by canceling the context)

	return nil
}

// CheckUpgradeStatus checks if an upgrade is in progress
func (s *Server) CheckUpgradeStatus(ctx context.Context) (bool, *upgcoord.HandoffState, error) {
	state, err := s.coordinator.LoadHandoffState()
	if err != nil {
		return false, nil, err
	}

	return state != nil, state, nil
}

// CancelUpgrade cancels an in-progress upgrade
func (s *Server) CancelUpgrade(ctx context.Context) error {
	return s.coordinator.ClearHandoffState()
}
