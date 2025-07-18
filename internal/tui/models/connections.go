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

// DiscoveredConfigFile represents a configuration file found in the filesystem
type DiscoveredConfigFile struct {
	Path        string        `json:"path"`
	Name        string        `json:"name"`
	Format      string        `json:"format"` // "claude-desktop", "vscode", "mcp-tui", "unknown"
	ServerCount int           `json:"serverCount"`
	Accessible  bool          `json:"accessible"`
	Error       string        `json:"error,omitempty"`
	Servers     []ServerInfo  `json:"servers,omitempty"`
}

type ServerInfo struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Command     string   `json:"command,omitempty"`
	Args        []string `json:"args,omitempty"`
	Transport   string   `json:"transport"`
}

// DiscoverConfigFiles finds configuration files in common locations
func (cm *ConnectionsManager) DiscoverConfigFiles() []*DiscoveredConfigFile {
	var discovered []*DiscoveredConfigFile

	// Current directory patterns
	currentDirPatterns := []string{
		".mcp.json",
		".claude.json", 
		"mcp.json",
		"mcp-config.json",
		"connections.json",
		".vscode/mcp.json",
		"package.json", // May contain MCP server configs
	}

	// Check current directory
	cwd, _ := os.Getwd()
	for _, pattern := range currentDirPatterns {
		fullPath := filepath.Join(cwd, pattern)
		if info, err := os.Stat(fullPath); err == nil && !info.IsDir() {
			config := cm.analyzeConfigFile(fullPath)
			if config != nil { // Only include files with valid MCP config
				discovered = append(discovered, config)
			}
		}
	}

	// Check user config directory
	if homeDir, err := os.UserHomeDir(); err == nil {
		userConfigPaths := []string{
			filepath.Join(homeDir, ".config", "mcp-tui", "connections.json"),
			filepath.Join(homeDir, ".config", "mcp", "config.json"),
		}

		for _, path := range userConfigPaths {
			if info, err := os.Stat(path); err == nil && !info.IsDir() {
				config := cm.analyzeConfigFile(path)
				if config != nil { // Only include files with valid MCP config
					discovered = append(discovered, config)
				}
			}
		}

		// Claude Desktop config
		claudePath := cm.getClaudeDesktopConfigPath()
		if claudePath != "" {
			if info, err := os.Stat(claudePath); err == nil && !info.IsDir() {
				config := cm.analyzeConfigFile(claudePath)
				if config != nil { // Only include files with valid MCP config
					discovered = append(discovered, config)
				}
			}
		}
	}

	// Remove duplicates and sort by relevance
	return cm.deduplicateAndSort(discovered)
}

// analyzeConfigFile analyzes a configuration file to determine its type and contents
func (cm *ConnectionsManager) analyzeConfigFile(filePath string) *DiscoveredConfigFile {
	config := &DiscoveredConfigFile{
		Path:        filePath,
		Name:        filepath.Base(filePath),
		Accessible:  true,
	}

	// Try to read and parse the file
	data, err := os.ReadFile(filePath)
	if err != nil {
		config.Accessible = false
		config.Error = err.Error()
		return config
	}

	// Determine format by trying to parse
	serverCount := 0
	
	// Try Claude Desktop format - must have mcpServers node
	var claudeConfig struct {
		MCPServers map[string]interface{} `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &claudeConfig); err == nil && claudeConfig.MCPServers != nil && len(claudeConfig.MCPServers) > 0 {
		config.Format = "claude-desktop"
		serverCount = len(claudeConfig.MCPServers)
		config.Servers = cm.extractClaudeDesktopServers(claudeConfig.MCPServers)
	} else {
		// Try VS Code format - must have servers node
		var vscodeConfig struct {
			Servers map[string]interface{} `json:"servers"`
		}
		if err := json.Unmarshal(data, &vscodeConfig); err == nil && vscodeConfig.Servers != nil && len(vscodeConfig.Servers) > 0 {
			config.Format = "vscode"
			serverCount = len(vscodeConfig.Servers)
			config.Servers = cm.extractVSCodeServers(vscodeConfig.Servers)
		} else {
			// Try MCP-TUI native format - must have servers node with content
			var nativeConfig ConnectionsConfig
			if err := json.Unmarshal(data, &nativeConfig); err == nil && nativeConfig.Servers != nil && len(nativeConfig.Servers) > 0 {
				config.Format = "mcp-tui"
				serverCount = len(nativeConfig.Servers)
				config.Servers = cm.extractNativeServers(nativeConfig.Servers)
			} else {
				// Check for package.json with MCP references - only include if has actual MCP servers
				if filepath.Base(filePath) == "package.json" {
					var packageJSON map[string]interface{}
					if err := json.Unmarshal(data, &packageJSON); err == nil {
						if scripts, ok := packageJSON["scripts"].(map[string]interface{}); ok {
							mcpScriptCount := 0
							for name, script := range scripts {
								if scriptStr, ok := script.(string); ok {
									if strings.Contains(strings.ToLower(name), "mcp") || 
									   strings.Contains(strings.ToLower(scriptStr), "mcp") ||
									   strings.Contains(strings.ToLower(scriptStr), "model-context-protocol") {
										mcpScriptCount++
									}
								}
							}
							if mcpScriptCount > 0 {
								config.Format = "package.json"
								serverCount = mcpScriptCount
							} else {
								// No MCP-related content found
								config.Format = "unknown"
							}
						} else {
							config.Format = "unknown"
						}
					} else {
						config.Format = "unknown"
					}
				} else {
					config.Format = "unknown"
				}
			}
		}
	}

	config.ServerCount = serverCount
	
	// Only return files with valid MCP configuration (serverCount > 0)
	if serverCount == 0 || config.Format == "unknown" {
		return nil
	}
	
	return config
}

// extractClaudeDesktopServers extracts server information from Claude Desktop format
func (cm *ConnectionsManager) extractClaudeDesktopServers(mcpServers map[string]interface{}) []ServerInfo {
	var servers []ServerInfo
	
	for name, serverData := range mcpServers {
		if serverMap, ok := serverData.(map[string]interface{}); ok {
			server := ServerInfo{
				Name:      name,
				Transport: "stdio", // Claude Desktop format is typically stdio
			}
			
			if command, ok := serverMap["command"].(string); ok {
				server.Command = command
			}
			
			if args, ok := serverMap["args"].([]interface{}); ok {
				for _, arg := range args {
					if argStr, ok := arg.(string); ok {
						server.Args = append(server.Args, argStr)
					}
				}
			}
			
			// Try to generate a description
			if server.Command != "" {
				server.Description = fmt.Sprintf("%s %s", server.Command, strings.Join(server.Args, " "))
			}
			
			servers = append(servers, server)
		}
	}
	
	return servers
}

// extractVSCodeServers extracts server information from VS Code MCP format
func (cm *ConnectionsManager) extractVSCodeServers(vscodeServers map[string]interface{}) []ServerInfo {
	var servers []ServerInfo
	
	for name, serverData := range vscodeServers {
		if serverMap, ok := serverData.(map[string]interface{}); ok {
			server := ServerInfo{
				Name:      name,
				Transport: "stdio", // VS Code format is typically stdio
			}
			
			if command, ok := serverMap["command"].(string); ok {
				server.Command = command
			}
			
			if args, ok := serverMap["args"].([]interface{}); ok {
				for _, arg := range args {
					if argStr, ok := arg.(string); ok {
						server.Args = append(server.Args, argStr)
					}
				}
			}
			
			// Try to generate a description
			if server.Command != "" {
				server.Description = fmt.Sprintf("%s %s", server.Command, strings.Join(server.Args, " "))
			}
			
			servers = append(servers, server)
		}
	}
	
	return servers
}

// extractNativeServers extracts server information from MCP-TUI native format
func (cm *ConnectionsManager) extractNativeServers(nativeServers map[string]*ConnectionEntry) []ServerInfo {
	var servers []ServerInfo
	
	for name, entry := range nativeServers {
		server := ServerInfo{
			Name:        name,
			Description: entry.Description,
			Transport:   string(entry.Transport),
		}
		
		if entry.Transport == "stdio" {
			server.Command = entry.Command
			server.Args = entry.Args
		}
		
		servers = append(servers, server)
	}
	
	return servers
}

// deduplicateAndSort removes duplicate configs and sorts by relevance
func (cm *ConnectionsManager) deduplicateAndSort(configs []*DiscoveredConfigFile) []*DiscoveredConfigFile {
	seen := make(map[string]bool)
	var unique []*DiscoveredConfigFile

	for _, config := range configs {
		if !seen[config.Path] {
			seen[config.Path] = true
			unique = append(unique, config)
		}
	}

	// Sort by relevance: current dir first, then by server count, then by format priority
	for i := 0; i < len(unique); i++ {
		for j := i + 1; j < len(unique); j++ {
			if cm.isMoreRelevant(unique[i], unique[j]) {
				unique[i], unique[j] = unique[j], unique[i]
			}
		}
	}

	return unique
}

// isMoreRelevant determines if config a is more relevant than config b
func (cm *ConnectionsManager) isMoreRelevant(a, b *DiscoveredConfigFile) bool {
	// Current directory files are more relevant
	cwd, _ := os.Getwd()
	aInCwd := strings.HasPrefix(a.Path, cwd)
	bInCwd := strings.HasPrefix(b.Path, cwd)
	
	if aInCwd && !bInCwd {
		return false // a is better
	}
	if !aInCwd && bInCwd {
		return true // b is better
	}

	// Higher server count is more relevant
	if a.ServerCount != b.ServerCount {
		return a.ServerCount < b.ServerCount // b is better if it has more servers
	}

	// Format priority: mcp-tui > claude-desktop > vscode > package.json > unknown
	formatPriority := map[string]int{
		"mcp-tui":       1,
		"claude-desktop": 2,
		"vscode":        3,
		"package.json":  4,
		"unknown":       5,
	}

	aPrio, aExists := formatPriority[a.Format]
	bPrio, bExists := formatPriority[b.Format]
	
	if !aExists {
		aPrio = 6
	}
	if !bExists {
		bPrio = 6
	}

	return aPrio > bPrio // b is better if it has lower priority number
}

// LoadFromDiscovered loads connections from a discovered configuration file
func (cm *ConnectionsManager) LoadFromDiscovered(discoveredConfig *DiscoveredConfigFile) error {
	if !discoveredConfig.Accessible {
		return fmt.Errorf("configuration file is not accessible: %s", discoveredConfig.Error)
	}

	// Clear current config and load from the selected file
	cm.config = &ConnectionsConfig{
		Version: "1.0",
		Servers: make(map[string]*ConnectionEntry),
	}

	if cm.loadFromSource(discoveredConfig.Path) {
		cm.logger.Info("Loaded connections from discovered file", 
			debug.F("path", discoveredConfig.Path),
			debug.F("format", discoveredConfig.Format),
			debug.F("serverCount", discoveredConfig.ServerCount))
		return nil
	}

	return fmt.Errorf("failed to load configuration from %s", discoveredConfig.Path)
}