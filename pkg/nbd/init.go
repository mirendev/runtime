package nbd

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
)

// InitializeNBDModule ensures the NBD kernel module is loaded with proper configuration
func InitializeNBDModule(log *slog.Logger) error {
	// Check if NBD module is already loaded
	if _, err := os.Stat("/sys/module/nbd"); err == nil {
		log.Info("NBD module already loaded")
		return nil
	}

	// Load NBD module with nbds_max=128 for sufficient devices
	log.Info("Loading NBD kernel module", "nbds_max", 128)
	cmd := exec.Command("modprobe", "nbd", "nbds_max=128")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to load NBD module: %w (output: %s)", err, string(output))
	}

	// Verify module loaded successfully
	if _, err := os.Stat("/sys/module/nbd"); err != nil {
		return fmt.Errorf("NBD module not found after modprobe: %w", err)
	}

	log.Info("NBD kernel module loaded successfully")
	return nil
}
