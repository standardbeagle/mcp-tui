package models

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/debug"
	"github.com/standardbeagle/mcp-tui/internal/privatefile"
)

// displayName renders a server id like "my-server" as "My Server" for the
// connections list: hyphens become spaces and the first rune after any
// character other than a letter, digit or underscore is title-cased, as
// the deprecated strings.Title did. Ids are JSON keys, so any Unicode.
func displayName(id string) string {
	atWordStart := true
	return strings.Map(func(r rune) rune {
		if r == '-' {
			r = ' '
		}
		out := r
		if atWordStart {
			out = unicode.ToTitle(r)
		}
		atWordStart = !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_'
		return out
	}, id)
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

	// Source is "servers.json" for a server defined there. connections.json
	// keeps a copy of it for its usage history, which is dropped once the
	// server leaves servers.json.
	Source string `json:"source,omitempty"`
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

// The native config files, both in ~/.config/mcp-tui. servers.json is the
// server list a user writes by hand; connections.json is where mcp-tui saves
// the servers it has connected to, with when and how that went. Both use the
// native format, and a server in servers.json replaces the saved entry of
// the same ID.
const (
	ServersFileName     = "servers.json"
	ConnectionsFileName = "connections.json"

	// nativeFormatVersion is the "version" mcp-tui writes.
	nativeFormatVersion = "1.0"
)

// ConnectionsManager manages saved connections
type ConnectionsManager struct {
	logger debug.Logger
	config *ConnectionsConfig
	// filePath is connections.json, the file SaveConnections writes.
	filePath string
	// serversPath is servers.json, read but never written.
	serversPath string
	// savedDefault is connections.json's own defaultServer. A defaultServer
	// from servers.json overrides it in memory but is never saved, so taking
	// it out of servers.json takes it out of effect.
	savedDefault string
}

// NewConnectionsManager creates a new connections manager
func NewConnectionsManager() *ConnectionsManager {
	cm := &ConnectionsManager{
		logger: debug.Component("connections"),
		config: &ConnectionsConfig{
			Version: nativeFormatVersion,
			Servers: make(map[string]*ConnectionEntry),
		},
	}

	// Determine config file path
	configDir := "."
	if homeDir, err := os.UserHomeDir(); err != nil {
		cm.logger.Error("Failed to get user home directory", debug.F("error", err))
	} else {
		configDir = filepath.Join(homeDir, ".config", "mcp-tui")
	}
	cm.filePath = filepath.Join(configDir, ConnectionsFileName)
	cm.serversPath = filepath.Join(configDir, ServersFileName)

	cm.logger.Debug("Connections manager initialized",
		debug.F("configPath", cm.filePath), debug.F("serversPath", cm.serversPath))
	return cm
}

// LoadConnections loads the configured servers. The native files
// (servers.json and connections.json) come first; only when neither holds a
// server is the first other client config that loads used instead. A native
// file that exists but cannot be read as a server list is reported in the
// returned error, after everything else that could be loaded has been.
func (cm *ConnectionsManager) LoadConnections() error {
	loaded, nativeErr := cm.loadNativeFiles()
	if loaded {
		return nativeErr
	}

	sources := []string{
		cm.getClaudeDesktopConfigPath(),      // Claude Desktop config
		".mcp.json",                          // Project-local config
		".claude.json",                       // Claude Code config
		filepath.Join(".vscode", "mcp.json"), // VS Code config
	}

	for _, source := range sources {
		if cm.loadFromSource(source) {
			cm.logger.Info("Loaded connections from source", debug.F("source", source))
			return nativeErr
		}
	}

	cm.logger.Info("No existing connections found, starting with empty configuration")
	return nativeErr
}

// loadNativeFiles reads connections.json and then servers.json over it.
// loaded reports whether either file contributed a server.
func (cm *ConnectionsManager) loadNativeFiles() (loaded bool, err error) {
	var errs []error

	saved, readErr := readNativeFile(cm.filePath)
	if readErr != nil {
		errs = append(errs, readErr)
	}
	if saved != nil {
		cm.config = saved
		cm.savedDefault = saved.DefaultServer
		cm.logger.Info("Loaded saved connections", debug.F("path", cm.filePath), debug.F("count", len(saved.Servers)))
	}

	servers, readErr := readNativeFile(cm.serversPath)
	if readErr != nil {
		errs = append(errs, readErr)
	}
	if servers == nil && readErr == nil {
		servers = &ConnectionsConfig{Servers: map[string]*ConnectionEntry{}}
	}
	if servers != nil {
		// A saved copy of a server that servers.json no longer defines is
		// gone. When servers.json could not be read, its servers are kept.
		if readErr == nil {
			for id, entry := range cm.config.Servers {
				if entry.Source == ServersFileName && servers.Servers[id] == nil {
					delete(cm.config.Servers, id)
				}
			}
			if cm.config.Servers[cm.savedDefault] == nil {
				cm.savedDefault = ""
				cm.config.DefaultServer = ""
			}
		}
		for id, entry := range servers.Servers {
			entry.Source = ServersFileName
			if prev := cm.config.Servers[id]; prev != nil {
				// Keep what mcp-tui recorded about the server; the
				// definition is the user's.
				entry.LastUsed = prev.LastUsed
				entry.Success = prev.Success
				entry.LastSeenVersion = prev.LastSeenVersion
			}
			cm.config.Servers[id] = entry
		}
		if servers.DefaultServer != "" {
			cm.config.DefaultServer = servers.DefaultServer
		}
		cm.logger.Info("Loaded servers", debug.F("path", cm.serversPath), debug.F("count", len(servers.Servers)))
	}

	return len(cm.config.Servers) > 0, errors.Join(errs...)
}

// readNativeFile parses a native config file. A missing file is (nil, nil).
func readNativeFile(path string) (*ConnectionsConfig, error) {
	//nolint:gosec // G304: path is servers.json or connections.json in the user's mcp-tui config directory.
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	cfg, err := ParseNativeConfig(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

// isNativePath reports whether path is servers.json or connections.json.
func (cm *ConnectionsManager) isNativePath(path string) bool {
	clean := filepath.Clean(path)
	return clean == filepath.Clean(cm.filePath) || clean == filepath.Clean(cm.serversPath)
}

// hasVersionField reports whether data is a JSON object with a non-empty
// "version", the mark of the native format.
func hasVersionField(data []byte) bool {
	var probe struct {
		Version string `json:"version"`
	}
	return json.Unmarshal(data, &probe) == nil && probe.Version != ""
}

// nativeEntry is a server as written in a native file: a ConnectionEntry
// that may also spell its transport "type", as VS Code and Claude Code do.
type nativeEntry struct {
	entryFields
	Type string `json:"type,omitempty"`
}

// entryFields has ConnectionEntry's fields without its methods, so decoding
// into nativeEntry does not recurse.
type entryFields ConnectionEntry

// ParseNativeConfig parses the native format. "servers" is either an object
// keyed by server ID or an array of servers that each carry an "id" or a
// "name"; "version" is optional. A server without a transport is http when
// it has a url and stdio otherwise.
func ParseNativeConfig(data []byte) (*ConnectionsConfig, error) {
	var file struct {
		Version           string              `json:"version"`
		DefaultServer     string              `json:"defaultServer,omitempty"`
		Servers           json.RawMessage     `json:"servers"`
		RecentConnections []*RecentConnection `json:"recentConnections,omitempty"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("not valid JSON: %w", err)
	}
	cfg := &ConnectionsConfig{
		Version:           file.Version,
		DefaultServer:     file.DefaultServer,
		Servers:           make(map[string]*ConnectionEntry),
		RecentConnections: file.RecentConnections,
	}
	if cfg.Version == "" {
		cfg.Version = nativeFormatVersion
	}

	raw := bytes.TrimSpace(file.Servers)
	var err error
	switch {
	case len(raw) == 0 || bytes.Equal(raw, []byte("null")):
		// No servers.
	case raw[0] == '[':
		err = parseServerList(raw, cfg.Servers)
	case raw[0] == '{':
		err = parseServerMap(raw, cfg.Servers)
	default:
		err = errors.New(`"servers" must be an object keyed by server ID or an array of servers`)
	}
	if err != nil {
		return nil, err
	}

	if cfg.DefaultServer != "" && cfg.Servers[cfg.DefaultServer] == nil {
		return nil, fmt.Errorf("defaultServer %q is not one of the servers", cfg.DefaultServer)
	}
	return cfg, nil
}

// parseServerList adds the servers of a JSON array, each named by its "id"
// or "name", to servers.
func parseServerList(raw []byte, servers map[string]*ConnectionEntry) error {
	var list []*nativeEntry
	if err := json.Unmarshal(raw, &list); err != nil {
		return fmt.Errorf("servers: %w", err)
	}
	for i, item := range list {
		if item == nil {
			return fmt.Errorf("servers[%d]: not an object", i)
		}
		id := item.ID
		if id == "" {
			id = item.Name
		}
		if id == "" {
			return fmt.Errorf("servers[%d]: needs an \"id\" or a \"name\"", i)
		}
		if _, dup := servers[id]; dup {
			return fmt.Errorf("servers[%d]: a second server named %q", i, id)
		}
		entry, err := item.normalize(id)
		if err != nil {
			return err
		}
		servers[id] = entry
	}
	return nil
}

// parseServerMap adds the servers of a JSON object keyed by ID to servers.
func parseServerMap(raw []byte, servers map[string]*ConnectionEntry) error {
	var byID map[string]*nativeEntry
	if err := json.Unmarshal(raw, &byID); err != nil {
		return fmt.Errorf("servers: %w", err)
	}
	for id, item := range byID {
		if item == nil {
			return fmt.Errorf("server %q: not an object", id)
		}
		entry, err := item.normalize(id)
		if err != nil {
			return err
		}
		servers[id] = entry
	}
	return nil
}

// parseTransport maps a configured transport name to its TransportType. An
// empty name is http for a server with a URL and stdio otherwise.
func parseTransport(name string, hasURL bool) (config.TransportType, bool) {
	switch strings.ToLower(name) {
	case "":
		if hasURL {
			return config.TransportHTTP, true
		}
		return config.TransportStdio, true
	case string(config.TransportStdio):
		return config.TransportStdio, true
	case string(config.TransportSSE):
		return config.TransportSSE, true
	case string(config.TransportHTTP):
		return config.TransportHTTP, true
	case string(config.TransportStreamableHTTP), "streamablehttp", "streamable_http":
		return config.TransportStreamableHTTP, true
	}
	return "", false
}

// normalize turns a parsed server into a ConnectionEntry with id as its ID,
// a display name and a known transport.
func (item *nativeEntry) normalize(id string) (*ConnectionEntry, error) {
	entry := (*ConnectionEntry)(&item.entryFields)
	entry.ID = id
	if entry.Name == "" {
		entry.Name = displayName(id)
	}
	name := string(entry.Transport)
	if name == "" {
		name = item.Type
	}
	transport, ok := parseTransport(name, entry.URL != "")
	if !ok {
		return nil, fmt.Errorf("server %q: unknown transport %q (use stdio, http, streamable-http or sse)", id, name)
	}
	entry.Transport = transport
	switch {
	case transport == config.TransportStdio && entry.Command == "":
		return nil, fmt.Errorf("server %q: a stdio server needs a \"command\"", id)
	case transport != config.TransportStdio && entry.URL == "":
		return nil, fmt.Errorf("server %q: a %s server needs a \"url\"", id, transport)
	}
	if entry.Icon == "" {
		entry.Icon = iconForServer(id)
	}
	return entry, nil
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

	if cm.isNativePath(filePath) {
		nativeCfg, err := ParseNativeConfig(data)
		if err != nil {
			cm.logger.Warn("Native config does not parse", debug.F("path", filePath), debug.F("error", err))
			return false
		}
		cm.config = nativeCfg
		cm.savedDefault = nativeCfg.DefaultServer
		return true
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

// loadNativeFormat loads MCP-TUI native format from a file found elsewhere
// (a project file, a discovered file). There, a "version" field is what
// tells the native format from VS Code's, which also has "servers".
func (cm *ConnectionsManager) loadNativeFormat(data []byte) bool {
	if !hasVersionField(data) {
		return false
	}
	nativeCfg, err := ParseNativeConfig(data)
	if err != nil {
		cm.logger.Warn("Native config does not parse", debug.F("error", err))
		return false
	}

	cm.config = nativeCfg
	cm.savedDefault = nativeCfg.DefaultServer
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
		entry.Icon = iconForServer(id)

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

		// Map transport type; an unknown one is treated as stdio, as before.
		transport, ok := parseTransport(server.Type, server.URL != "" && server.Command == "")
		if !ok {
			transport = config.TransportStdio
		}
		entry.Transport = transport

		// Set icons based on server type
		entry.Icon = iconForServer(id)

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

// iconForServer returns an appropriate icon for a server ID
func iconForServer(serverType string) string {
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
	// Entries can carry headers and env values (i.e. credentials), so the
	// directory and file are owner-only like the OAuth token cache.
	// MkdirAll leaves an existing directory's mode alone, so a directory
	// created 0755 by an older release is tightened explicitly. The cwd
	// fallback (no home directory) is not ours to chmod.
	dir := filepath.Dir(cm.filePath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}
	if dir != "." {
		//nolint:gosec // G302: a directory needs the execute bit to be entered; 0700 is owner-only.
		if err := os.Chmod(dir, 0o700); err != nil {
			return fmt.Errorf("failed to restrict config directory: %w", err)
		}
	}

	out := *cm.config
	out.DefaultServer = cm.savedDefault
	data, err := json.MarshalIndent(&out, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal connections: %w", err)
	}

	if err := privatefile.Write(cm.filePath, data); err != nil {
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

// ServerIDs returns the configured server IDs in sorted order.
func (cm *ConnectionsManager) ServerIDs() []string {
	ids := make([]string, 0, len(cm.config.Servers))
	for id := range cm.config.Servers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// FindServer returns the server called name: the one with that ID, else the
// only one whose ID or display name matches it ignoring case.
func (cm *ConnectionsManager) FindServer(name string) (*ConnectionEntry, error) {
	if entry := cm.config.Servers[name]; entry != nil {
		return entry, nil
	}
	var match *ConnectionEntry
	for _, id := range cm.ServerIDs() {
		entry := cm.config.Servers[id]
		if strings.EqualFold(id, name) || strings.EqualFold(entry.Name, name) {
			if match != nil {
				return nil, fmt.Errorf("server name %q is ambiguous: it matches %q and %q", name, match.ID, id)
			}
			match = entry
		}
	}
	if match != nil {
		return match, nil
	}
	ids := cm.ServerIDs()
	if len(ids) == 0 {
		return nil, fmt.Errorf("no server %q: no servers are configured (see %s)", name, cm.serversPath)
	}
	return nil, fmt.Errorf("no server %q; configured servers: %s", name, strings.Join(ids, ", "))
}

// LoadServer loads the configured servers and returns the one called name
// (see FindServer) with its connection config resolved.
func LoadServer(name string) (*config.ConnectionConfig, error) {
	cm := NewConnectionsManager()
	loadErr := cm.LoadConnections()
	entry, err := cm.FindServer(name)
	if err != nil {
		if loadErr != nil {
			return nil, fmt.Errorf("%w (config errors: %w)", err, loadErr)
		}
		return nil, err
	}
	return entry.ResolveConnectionConfig()
}

// ResolveConnectionConfig is ToConnectionConfig with every ${VAR} in the
// URL, header values and environment values replaced from the environment,
// so secrets can stay out of the config file. A variable that is unset or
// empty is an error naming the server and the field.
func (entry *ConnectionEntry) ResolveConnectionConfig() (*config.ConnectionConfig, error) {
	conn := entry.ToConnectionConfig()
	var err error
	if conn.URL, err = config.ExpandEnvRefs(conn.URL); err != nil {
		return nil, fmt.Errorf("server %q: url: %w", entry.ID, err)
	}
	for name, value := range conn.Headers {
		if conn.Headers[name], err = config.ExpandEnvRefs(value); err != nil {
			return nil, fmt.Errorf("server %q: header %s: %w", entry.ID, name, err)
		}
	}
	for name, value := range conn.Environment {
		if conn.Environment[name], err = config.ExpandEnvRefs(value); err != nil {
			return nil, fmt.Errorf("server %q: env %s: %w", entry.ID, name, err)
		}
	}
	return conn, nil
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
	FormatClaudeDesktop = "claude-desktop"
	FormatVSCode        = "vscode"
	FormatMCPTUI        = "mcp-tui"
	FormatPackageJSON   = "package.json"
	FormatUnknown       = "unknown"
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
		paths := make([]string, 0, len(currentDirPatterns))
		for _, pattern := range currentDirPatterns {
			paths = append(paths, filepath.Join(cwd, pattern))
		}
		discovered = cm.discoverAtPaths(discovered, paths)
	}

	// Check user config directory
	if homeDir, err := os.UserHomeDir(); err == nil {
		userConfigPaths := []string{
			filepath.Join(homeDir, ".config", "mcp-tui", ServersFileName),
			filepath.Join(homeDir, ".config", "mcp-tui", ConnectionsFileName),
			filepath.Join(homeDir, ".config", "mcp", "config.json"),
		}
		discovered = cm.discoverAtPaths(discovered, userConfigPaths)

		// Claude Desktop config
		if claudePath := cm.getClaudeDesktopConfigPath(); claudePath != "" {
			discovered = cm.discoverAtPaths(discovered, []string{claudePath})
		}
	}

	// Remove duplicates and sort by relevance
	return cm.deduplicateAndSort(discovered)
}

// discoverAtPaths analyzes each existing regular file in paths and appends
// the ones holding a valid MCP config to discovered.
func (cm *ConnectionsManager) discoverAtPaths(
	discovered []*DiscoveredConfigFile, paths []string,
) []*DiscoveredConfigFile {
	for _, path := range paths {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			if analyzed := cm.analyzeConfigFile(path); analyzed != nil {
				discovered = append(discovered, analyzed)
			}
		}
	}
	return discovered
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

	if cm.analyzeNativeFile(dc, data) {
		return dc
	}

	// Try Claude Desktop format - must have mcpServers node
	var claudeConfig struct {
		MCPServers map[string]interface{} `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &claudeConfig); err == nil &&
		claudeConfig.MCPServers != nil && len(claudeConfig.MCPServers) > 0 {
		dc.Format = FormatClaudeDesktop
		serverCount = len(claudeConfig.MCPServers)
		dc.Servers = extractJSONObjectServers(claudeConfig.MCPServers)
	} else {
		// Try VS Code format - must have servers node
		var vscodeConfig struct {
			Servers map[string]interface{} `json:"servers"`
		}
		if err := json.Unmarshal(data, &vscodeConfig); err == nil &&
			vscodeConfig.Servers != nil && len(vscodeConfig.Servers) > 0 {
			dc.Format = FormatVSCode
			serverCount = len(vscodeConfig.Servers)
			dc.Servers = extractJSONObjectServers(vscodeConfig.Servers)
		} else {
			// Try MCP-TUI native format - must have servers node with content
			var nativeConfig ConnectionsConfig
			if err := json.Unmarshal(data, &nativeConfig); err == nil &&
				nativeConfig.Servers != nil && len(nativeConfig.Servers) > 0 {
				dc.Format = FormatMCPTUI
				serverCount = len(nativeConfig.Servers)
				dc.Servers = cm.extractNativeServers(nativeConfig.Servers)
			} else {
				dc.Format = FormatUnknown
			}
		}
	}

	dc.ServerCount = serverCount

	// Only return files with valid MCP configuration (serverCount > 0)
	if serverCount == 0 || dc.Format == FormatUnknown {
		return nil
	}

	return dc
}

// analyzeNativeFile fills dc from a native-format file and reports whether
// it was one. The native files, and any file with a "version", are native
// format; without this check first a native file passes for VS Code's.
func (cm *ConnectionsManager) analyzeNativeFile(dc *DiscoveredConfigFile, data []byte) bool {
	if !cm.isNativePath(dc.Path) && !hasVersionField(data) {
		return false
	}
	nativeCfg, err := ParseNativeConfig(data)
	if err != nil || len(nativeCfg.Servers) == 0 {
		return false
	}
	dc.Format = FormatMCPTUI
	dc.ServerCount = len(nativeCfg.Servers)
	dc.Servers = cm.extractNativeServers(nativeCfg.Servers)
	return true
}

// extractJSONObjectServers extracts server information from a
// name→server-object map in the Claude Desktop / VS Code shape (both carry
// command/args and are typically stdio).
func extractJSONObjectServers(serversMap map[string]interface{}) []ServerInfo {
	var servers []ServerInfo

	for name, serverData := range serversMap {
		serverMap, ok := serverData.(map[string]interface{})
		if !ok {
			continue
		}
		server := ServerInfo{
			Name:      name,
			Transport: string(config.TransportStdio), // Both formats are typically stdio
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
		FormatMCPTUI:        1,
		FormatClaudeDesktop: 2,
		FormatVSCode:        3,
		FormatPackageJSON:   4,
		FormatUnknown:       5,
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
		Version: nativeFormatVersion,
		Servers: make(map[string]*ConnectionEntry),
	}
	cm.savedDefault = ""

	if cm.loadFromSource(discoveredConfig.Path) {
		cm.logger.Info("Loaded connections from discovered file",
			debug.F("path", discoveredConfig.Path),
			debug.F("format", discoveredConfig.Format),
			debug.F("serverCount", discoveredConfig.ServerCount))
		return nil
	}

	return fmt.Errorf("failed to load configuration from %s", discoveredConfig.Path)
}
