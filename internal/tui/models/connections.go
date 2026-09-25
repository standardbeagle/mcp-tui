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

// displayName renders a server id like "my-server" as "My Server" for the
// connections list. ASCII-only: ids are config keys, so byte-wise casing is
// fine here.
func displayName(id string) string {
	words := strings.Split(strings.ReplaceAll(id, "-", " "), " ")
	for i, w := range words {
		if w != "" {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

// ConnectionEntry represents a saved connection configuration
type ConnectionEntry struct {
	ID          string               `json:"id"`
	Name        string               `json:"name"`
	Description string               `json:"description,omitempty"`
	Icon        string               `json:"icon,omitempty"`
	Transport   config.TransportType `json:"transport"`
	Command     string               `json:"command,omitempty"`
	Args        []string             `json:"args,omitempty"`
	URL         string               `json:"url,omitempty"`
	Headers     map[string]string    `json:"headers,omitempty"`
	Environment map[string]string    `json:"env,omitempty"`
	LastUsed    *time.Time           `json:"lastUsed,omitempty"`
	Success     bool                 `json:"success"`
	Tags        []string             `json:"tags,omitempty"`

	// LastSeenVersion is the negotiated MCP protocol version from the most
	// recent successful connection to this server. Persisted so users can
	// see at a glance which spec a saved server agreed to last time without
	// having to re-connect. Omitted from JSON when empty so newly-imported
	// entries (e.g. from Claude Desktop config) do not carry an empty key.
	LastSeenVersion string `json:"lastSeenVersion,omitempty"`
}

// ConnectionsConfig represents the saved connections configuration file
type ConnectionsConfig struct {
	Version           string                      `json:"version"`
	DefaultServer     string                      `json:"defaultServer,omitempty"`
	Servers           map[string]*ConnectionEntry `json:"servers"`
	RecentConnections []*RecentConnection         `json:"recentConnections,omitempty"`
}

// RecentConnection tracks recently used connections
type RecentConnection struct {
	ServerID string    `json:"serverId"`
	LastUsed time.Time `json:"lastUsed"`
	Success  bool      `json:"success"`
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
		cm.filePath,                          // MCP-TUI native config
		cm.getClaudeDesktopConfigPath(),      // Claude Desktop config
		".mcp.json",                          // Project-local config
		".claude.json",                       // Claude Code config
		filepath.Join(".vscode", "mcp.json"), // VS Code config
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

	//nolint:gosec // G304: filePath is a well-known MCP client config location from LoadConnections.
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
	var nativeCfg ConnectionsConfig
	if err := json.Unmarshal(data, &nativeCfg); err != nil {
		return false
	}

	// Validate that it's our format by checking for version field
	if nativeCfg.Version == "" {
		return false
	}

	cm.config = &nativeCfg
	cm.logger.Debug("Loaded native format", debug.F("serverCount", len(nativeCfg.Servers)))
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
			Name:        displayName(id),
			Description: "Imported from Claude Desktop config",
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
			Name:        displayName(id),
			Description: "Imported from VS Code config",
			Command:     server.Command,
			Args:        server.Args,
			URL:         server.URL,
			Headers:     server.Headers,
			Environment: server.Env,
			Success:     false,
		}

		// Map transport type
		switch server.Type {
		case string(config.TransportStdio):
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
		//nolint:gosec // G703: APPDATA is the OS-standard per-user config root; only fixed segments are joined below it.
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
	// Ensure config directory exists. Entries can carry headers and env
	// values (i.e. credentials), so the directory is owner-only like the
	// OAuth token cache.
	dir := filepath.Dir(cm.filePath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Marshal to JSON with indentation
	data, err := json.MarshalIndent(cm.config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal connections: %w", err)
	}

	// Write to file
	if err := os.WriteFile(cm.filePath, data, 0o600); err != nil {
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
		if err := cm.SaveConnections(); err != nil {
			cm.logger.Error("Failed to save connections", debug.F("error", err))
		}
	}
}

// UpdateLastUsedWithVersion is the version-aware sibling of UpdateLastUsed.
// Callers invoke it after a successful Connect with the server-confirmed
// MCP protocol version so the saved-connections list can surface "MCP <ver>"
// in the UI without re-negotiating. An empty version (e.g. from a failed
// reconnect where Success is false) is treated as "no new information" and
// leaves any previously-recorded value intact — we never want a transient
// failure to wipe the last good observation.
func (cm *ConnectionsManager) UpdateLastUsedWithVersion(serverID string, success bool, version string) {
	entry, exists := cm.config.Servers[serverID]
	if !exists {
		return
	}
	now := time.Now()
	entry.LastUsed = &now
	entry.Success = success
	if version != "" {
		entry.LastSeenVersion = version
	}

	cm.updateRecentConnections(serverID, success)
	if err := cm.SaveConnections(); err != nil {
		cm.logger.Error("Failed to save connections", debug.F("error", err))
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
		Type:        entry.Transport,
		Command:     entry.Command,
		Args:        append([]string(nil), entry.Args...),
		URL:         entry.URL,
		Headers:     cloneStringMap(entry.Headers),
		Environment: cloneStringMap(entry.Environment),
	}
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
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

// Discovered config file formats (DiscoveredConfigFile.Format).
const (
	formatClaudeDesktop = "claude-desktop"
	formatVSCode        = "vscode"
	formatMCPtui        = "mcp-tui"
	formatPackageJSON   = "package.json"
	formatUnknown       = "unknown"
)

// DiscoveredConfigFile represents a configuration file found in the filesystem
type DiscoveredConfigFile struct {
	Path        string       `json:"path"`
	Name        string       `json:"name"`
	Format      string       `json:"format"` // "claude-desktop", "vscode", "mcp-tui", "unknown"
	ServerCount int          `json:"serverCount"`
	Accessible  bool         `json:"accessible"`
	Error       string       `json:"error,omitempty"`
	Servers     []ServerInfo `json:"servers,omitempty"`
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
	}

	// Check current directory. An unreadable working directory skips the
	// cwd patterns; the user-config locations below are absolute and still
	// get checked.
	if cwd, err := os.Getwd(); err == nil {
		for _, pattern := range currentDirPatterns {
			fullPath := filepath.Join(cwd, pattern)
			if info, err := os.Stat(fullPath); err == nil && !info.IsDir() {
				analyzed := cm.analyzeConfigFile(fullPath)
				if analyzed != nil { // Only include files with valid MCP config
					discovered = append(discovered, analyzed)
				}
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
				analyzed := cm.analyzeConfigFile(path)
				if analyzed != nil { // Only include files with valid MCP config
					discovered = append(discovered, analyzed)
				}
			}
		}

		// Claude Desktop config
		claudePath := cm.getClaudeDesktopConfigPath()
		if claudePath != "" {
			if info, err := os.Stat(claudePath); err == nil && !info.IsDir() {
				analyzed := cm.analyzeConfigFile(claudePath)
				if analyzed != nil { // Only include files with valid MCP config
					discovered = append(discovered, analyzed)
				}
			}
		}
	}

	// Remove duplicates and sort by relevance
	return cm.deduplicateAndSort(discovered)
}

// analyzeConfigFile analyzes a configuration file to determine its type and contents
func (cm *ConnectionsManager) analyzeConfigFile(filePath string) *DiscoveredConfigFile {
	dc := &DiscoveredConfigFile{
		Path:       filePath,
		Name:       filepath.Base(filePath),
		Accessible: true,
	}

	// Try to read and parse the file
	//nolint:gosec // G304: filePath comes from discovery over well-known MCP client config locations.
	data, err := os.ReadFile(filePath)
	if err != nil {
		dc.Accessible = false
		dc.Error = err.Error()
		return dc
	}

	// Determine format by trying to parse
	serverCount := 0

	// Try Claude Desktop format - must have mcpServers node
	var claudeConfig struct {
		MCPServers map[string]interface{} `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &claudeConfig); err == nil && claudeConfig.MCPServers != nil && len(claudeConfig.MCPServers) > 0 {
		dc.Format = formatClaudeDesktop
		serverCount = len(claudeConfig.MCPServers)
		dc.Servers = cm.extractClaudeDesktopServers(claudeConfig.MCPServers)
	} else {
		// Try VS Code format - must have servers node
		var vscodeConfig struct {
			Servers map[string]interface{} `json:"servers"`
		}
		if err := json.Unmarshal(data, &vscodeConfig); err == nil && vscodeConfig.Servers != nil && len(vscodeConfig.Servers) > 0 {
			dc.Format = formatVSCode
			serverCount = len(vscodeConfig.Servers)
			dc.Servers = cm.extractVSCodeServers(vscodeConfig.Servers)
		} else {
			// Try MCP-TUI native format - must have servers node with content
			var nativeConfig ConnectionsConfig
			if err := json.Unmarshal(data, &nativeConfig); err == nil && nativeConfig.Servers != nil && len(nativeConfig.Servers) > 0 {
				dc.Format = formatMCPtui
				serverCount = len(nativeConfig.Servers)
				dc.Servers = cm.extractNativeServers(nativeConfig.Servers)
			} else {
				dc.Format = formatUnknown
			}
		}
	}

	dc.ServerCount = serverCount

	// Only return files with valid MCP configuration (serverCount > 0)
	if serverCount == 0 || dc.Format == formatUnknown {
		return nil
	}

	return dc
}

// extractClaudeDesktopServers extracts server information from Claude Desktop format
func (cm *ConnectionsManager) extractClaudeDesktopServers(mcpServers map[string]interface{}) []ServerInfo {
	var servers []ServerInfo

	for name, serverData := range mcpServers {
		serverMap, ok := serverData.(map[string]interface{})
		if !ok {
			continue
		}
		server := ServerInfo{
			Name:      name,
			Transport: string(config.TransportStdio), // Claude Desktop format is typically stdio
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

	return servers
}

// extractVSCodeServers extracts server information from VS Code MCP format
func (cm *ConnectionsManager) extractVSCodeServers(vscodeServers map[string]interface{}) []ServerInfo {
	var servers []ServerInfo

	for name, serverData := range vscodeServers {
		serverMap, ok := serverData.(map[string]interface{})
		if !ok {
			continue
		}
		server := ServerInfo{
			Name:      name,
			Transport: string(config.TransportStdio), // VS Code format is typically stdio
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

	return servers
}

// extractNativeServers extracts server information from MCP-TUI native format
func (cm *ConnectionsManager) extractNativeServers(nativeServers map[string]*ConnectionEntry) []ServerInfo {
	servers := make([]ServerInfo, 0, len(nativeServers))

	for name, entry := range nativeServers {
		server := ServerInfo{
			Name:        name,
			Description: entry.Description,
			Transport:   string(entry.Transport),
		}

		if entry.Transport == config.TransportStdio {
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
	// Current directory files are more relevant. Without a readable working
	// directory there is no cwd boosting; the remaining keys still order.
	cwd, err := os.Getwd()
	if err != nil {
		cwd = ""
	}
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
		formatMCPtui:        1,
		formatClaudeDesktop: 2,
		formatVSCode:        3,
		formatPackageJSON:   4,
		formatUnknown:       5,
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
