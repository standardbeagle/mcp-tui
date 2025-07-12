package models

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/debug"
)

// ConnectionEntry represents a saved connection configuration
type ConnectionEntry struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Icon        string                 `json:"icon,omitempty"`
	Transport   config.TransportType   `json:"transport"`
	Command     string                 `json:"command,omitempty"`
	Args        []string               `json:"args,omitempty"`
	URL         string                 `json:"url,omitempty"`
	Headers     map[string]string      `json:"headers,omitempty"`
	Environment map[string]string      `json:"env,omitempty"`
	LastUsed    *time.Time             `json:"lastUsed,omitempty"`
	Success     bool                   `json:"success"`
	Tags        []string               `json:"tags,omitempty"`
}

// ConnectionsConfig represents the saved connections configuration file
type ConnectionsConfig struct {
	Version           string                    `json:"version"`
	DefaultServer     string                    `json:"defaultServer,omitempty"`
	Servers           map[string]*ConnectionEntry `json:"servers"`
	RecentConnections []*RecentConnection       `json:"recentConnections,omitempty"`
}

// RecentConnection tracks recently used connections
type RecentConnection struct {
	ServerID string     `json:"serverId"`
	LastUsed time.Time  `json:"lastUsed"`
	Success  bool       `json:"success"`
}

// ConnectionsManager manages saved connections
type ConnectionsManager struct {
	logger   debug.Logger
	config   *ConnectionsConfig
	filePath string
}

// NewConnectionsManager creates a new connections manager
func NewConnectionsManager() *ConnectionsManager {
	cm := &ConnectionsManager{
		logger: debug.Component("connections"),
		config: &ConnectionsConfig{
			Version: "1.0",
			Servers: make(map[string]*ConnectionEntry),
		},
	}

	// Determine config file path
	homeDir, err := os.UserHomeDir()
	if err != nil {
		cm.logger.Error("Failed to get user home directory", debug.F("error", err))
		cm.filePath = "connections.json"
	} else {
		configDir := filepath.Join(homeDir, ".config", "mcp-tui")
		cm.filePath = filepath.Join(configDir, "connections.json")
	}

	cm.logger.Debug("Connections manager initialized", debug.F("configPath", cm.filePath))
	return cm
}

// LoadConnections loads connections from various configuration formats
func (cm *ConnectionsManager) LoadConnections() error {
	// Try to load from multiple sources in priority order
	sources := []string{
		cm.filePath,                                                      // MCP-TUI native config
		cm.getClaudeDesktopConfigPath(),                                 // Claude Desktop config
		".mcp.json",                                                     // Project-local config
		".claude.json",                                                  // Claude Code config
		filepath.Join(".vscode", "mcp.json"),                          // VS Code config
	}

	for _, source := range sources {
		if cm.loadFromSource(source) {
			cm.logger.Info("Loaded connections from source", debug.F("source", source))
			return nil
		}
	}

	cm.logger.Info("No existing connections found, starting with empty configuration")
	return nil
}

// loadFromSource attempts to load from a specific source
func (cm *ConnectionsManager) loadFromSource(filePath string) bool {
	if filePath == "" {
		return false
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		cm.logger.Debug("Could not read config file", debug.F("path", filePath), debug.F("error", err))
		return false
	}

	// Try to parse as MCP-TUI native format first
	if cm.loadNativeFormat(data) {
		return true
	}

	// Try to parse as Claude Desktop format
	if cm.loadClaudeDesktopFormat(data) {
		return true
	}

	// Try to parse as VS Code MCP format
	if cm.loadVSCodeFormat(data) {
		return true
	}

	cm.logger.Debug("Failed to parse config file", debug.F("path", filePath))
	return false
}

// loadNativeFormat loads MCP-TUI native format
func (cm *ConnectionsManager) loadNativeFormat(data []byte) bool {
	var config ConnectionsConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return false
	}

	// Validate that it's our format by checking for version field
	if config.Version == "" {
		return false
	}

	cm.config = &config
	cm.logger.Debug("Loaded native format", debug.F("serverCount", len(config.Servers)))
	return true
}

// loadClaudeDesktopFormat loads Claude Desktop configuration format
func (cm *ConnectionsManager) loadClaudeDesktopFormat(data []byte) bool {
	var claudeConfig struct {
		MCPServers map[string]struct {
			Command string            `json:"command"`
			Args    []string          `json:"args"`
			Env     map[string]string `json:"env,omitempty"`
		} `json:"mcpServers"`
	}

	if err := json.Unmarshal(data, &claudeConfig); err != nil {
		return false
	}

	// Check if this looks like Claude Desktop format
	if claudeConfig.MCPServers == nil {
		return false
	}

	// Convert to our format
	for id, server := range claudeConfig.MCPServers {
		entry := &ConnectionEntry{
			ID:          id,
			Name:        strings.Title(strings.ReplaceAll(id, "-", " ")),
			Description: fmt.Sprintf("Imported from Claude Desktop config"),
			Transport:   config.TransportStdio,
			Command:     server.Command,
			Args:        server.Args,
			Environment: server.Env,
			Success:     false,
		}

		// Set icons based on server type
		entry.Icon = cm.getIconForServerType(id)

		cm.config.Servers[id] = entry
	}

	cm.logger.Debug("Loaded Claude Desktop format", debug.F("serverCount", len(claudeConfig.MCPServers)))
	return true
}

// loadVSCodeFormat loads VS Code MCP configuration format
func (cm *ConnectionsManager) loadVSCodeFormat(data []byte) bool {
	var vscodeConfig struct {
		Servers map[string]struct {
			Type    string            `json:"type"`
			Command string            `json:"command,omitempty"`
			Args    []string          `json:"args,omitempty"`
			URL     string            `json:"url,omitempty"`
			Headers map[string]string `json:"headers,omitempty"`
			Env     map[string]string `json:"env,omitempty"`
		} `json:"servers"`
	}

	if err := json.Unmarshal(data, &vscodeConfig); err != nil {
		return false
	}

	// Check if this looks like VS Code MCP format
	if vscodeConfig.Servers == nil {
		return false
	}

	// Convert to our format
	for id, server := range vscodeConfig.Servers {
		entry := &ConnectionEntry{
			ID:          id,
			Name:        strings.Title(strings.ReplaceAll(id, "-", " ")),
			Description: fmt.Sprintf("Imported from VS Code config"),
			Command:     server.Command,
			Args:        server.Args,
			URL:         server.URL,
			Headers:     server.Headers,
			Environment: server.Env,
			Success:     false,
		}

		// Map transport type
		switch server.Type {
		case "stdio":
			entry.Transport = config.TransportStdio
		case "sse":
			entry.Transport = config.TransportSSE
		case "http":
			entry.Transport = config.TransportHTTP
		default:
			entry.Transport = config.TransportStdio
		}

		// Set icons based on server type
		entry.Icon = cm.getIconForServerType(id)

		cm.config.Servers[id] = entry
	}

	cm.logger.Debug("Loaded VS Code format", debug.F("serverCount", len(vscodeConfig.Servers)))
	return true
}

// getClaudeDesktopConfigPath returns the Claude Desktop config path
func (cm *ConnectionsManager) getClaudeDesktopConfigPath() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	// Check macOS path first
	macPath := filepath.Join(homeDir, "Library", "Application Support", "Claude", "claude_desktop_config.json")
	if _, err := os.Stat(macPath); err == nil {
		return macPath
	}

	// Check Windows path
	appData := os.Getenv("APPDATA")
	if appData != "" {
		winPath := filepath.Join(appData, "Claude", "claude_desktop_config.json")
		if _, err := os.Stat(winPath); err == nil {
			return winPath
		}
	}

	// Check Linux path
	linuxPath := filepath.Join(homeDir, ".config", "Claude", "claude_desktop_config.json")
	if _, err := os.Stat(linuxPath); err == nil {
		return linuxPath
	}

	return ""
}

// getIconForServerType returns an appropriate icon for a server type
func (cm *ConnectionsManager) getIconForServerType(serverType string) string {
	icons := map[string]string{
		"filesystem": "📁",
		"github":     "🐙",
		"weather":    "🌤️",
		"sqlite":     "🗄️",
		"postgres":   "🐘",
		"mysql":      "🐬",
		"puppeteer":  "🎭",
		"memory":     "🧠",
		"browser":    "🌐",
		"search":     "🔍",
		"api":        "⚡",
		"everything": "🌟",
	}

	// Check for partial matches
	serverLower := strings.ToLower(serverType)
	for key, icon := range icons {
		if strings.Contains(serverLower, key) {
			return icon
		}
	}

	return "🔧" // Default icon
}

// SaveConnections saves the current connections to disk
func (cm *ConnectionsManager) SaveConnections() error {
	// Ensure config directory exists
	dir := filepath.Dir(cm.filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Marshal to JSON with indentation
	data, err := json.MarshalIndent(cm.config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal connections: %w", err)
	}

	// Write to file
	if err := os.WriteFile(cm.filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write connections file: %w", err)
	}

	cm.logger.Info("Saved connections", debug.F("path", cm.filePath), debug.F("count", len(cm.config.Servers)))
	return nil
}

// GetConnections returns all saved connections
func (cm *ConnectionsManager) GetConnections() map[string]*ConnectionEntry {
	if cm.config.Servers == nil {
		return make(map[string]*ConnectionEntry)
	}
	return cm.config.Servers
}

// GetDefaultConnection returns the default connection if set
func (cm *ConnectionsManager) GetDefaultConnection() *ConnectionEntry {
	if cm.config.DefaultServer == "" {
		return nil
	}
	return cm.config.Servers[cm.config.DefaultServer]
}

// ShouldAutoConnect returns true if we should auto-connect
func (cm *ConnectionsManager) ShouldAutoConnect() bool {
	// Auto-connect if there's a default server or only one server
	return cm.config.DefaultServer != "" || len(cm.config.Servers) == 1
}

// GetAutoConnectEntry returns the entry to auto-connect to
func (cm *ConnectionsManager) GetAutoConnectEntry() *ConnectionEntry {
	if cm.config.DefaultServer != "" {
		return cm.config.Servers[cm.config.DefaultServer]
	}

	if len(cm.config.Servers) == 1 {
		for _, entry := range cm.config.Servers {
			return entry
		}
	}

	return nil
}

// UpdateLastUsed updates the last used time for a connection
func (cm *ConnectionsManager) UpdateLastUsed(serverID string, success bool) {
	if entry, exists := cm.config.Servers[serverID]; exists {
		now := time.Now()
		entry.LastUsed = &now
		entry.Success = success

		// Update recent connections
		cm.updateRecentConnections(serverID, success)

		// Save changes
		cm.SaveConnections()
	}
}

// updateRecentConnections updates the recent connections list
func (cm *ConnectionsManager) updateRecentConnections(serverID string, success bool) {
	if cm.config.RecentConnections == nil {
		cm.config.RecentConnections = make([]*RecentConnection, 0)
	}

	// Remove existing entry for this server
	for i := len(cm.config.RecentConnections) - 1; i >= 0; i-- {
		if cm.config.RecentConnections[i].ServerID == serverID {
			cm.config.RecentConnections = append(
				cm.config.RecentConnections[:i],
				cm.config.RecentConnections[i+1:]...,
			)
			break
		}
	}

	// Add new entry at the beginning
	recent := &RecentConnection{
		ServerID: serverID,
		LastUsed: time.Now(),
		Success:  success,
	}
	cm.config.RecentConnections = append([]*RecentConnection{recent}, cm.config.RecentConnections...)

	// Keep only the last 10 recent connections
	if len(cm.config.RecentConnections) > 10 {
		cm.config.RecentConnections = cm.config.RecentConnections[:10]
	}
}

// ToConnectionConfig converts a ConnectionEntry to config.ConnectionConfig
func (entry *ConnectionEntry) ToConnectionConfig() *config.ConnectionConfig {
	return &config.ConnectionConfig{
		Type:    entry.Transport,
		Command: entry.Command,
		Args:    entry.Args,
		URL:     entry.URL,
	}
}

// GetRecentConnections returns recent connections sorted by last used
func (cm *ConnectionsManager) GetRecentConnections() []*ConnectionEntry {
	var recent []*ConnectionEntry

	for _, recentConn := range cm.config.RecentConnections {
		if entry, exists := cm.config.Servers[recentConn.ServerID]; exists {
			recent = append(recent, entry)
		}
	}

	return recent
}