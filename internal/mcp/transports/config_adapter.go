package transports

import (
	"time"

	configPkg "github.com/standardbeagle/mcp-tui/internal/config"
)

// FromConnectionConfig converts the existing ConnectionConfig to TransportConfig
func FromConnectionConfig(config *configPkg.ConnectionConfig, debugMode bool, timeout time.Duration) *TransportConfig {
	if config == nil {
		return nil
	}

	transportConfig := &TransportConfig{
		Type:      TransportType(config.Type),
		Command:   config.Command,
		Args:      config.Args,
		URL:       config.URL,
		Timeout:   timeout,
		DebugMode: debugMode,
	}

	return transportConfig
}

// ToConnectionConfig converts TransportConfig back to ConnectionConfig for compatibility
func ToConnectionConfig(config *TransportConfig) *configPkg.ConnectionConfig {
	if config == nil {
		return nil
	}

	return &configPkg.ConnectionConfig{
		Type:    configPkg.TransportType(config.Type),
		Command: config.Command,
		Args:    config.Args,
		URL:     config.URL,
	}
}
