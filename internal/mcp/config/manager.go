package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ConfigManager handles loading, validation, and management of configurations
type ConfigManager struct {
	config     *UnifiedConfig
	sources    []ConfigSource
	validators []ConfigValidator
}

// ConfigSource represents a source of configuration data
type ConfigSource interface {
	Name() string
	Load() (*UnifiedConfig, error)
	Priority() int // Higher priority sources override lower priority ones
}

// ConfigValidator validates configuration sections
type ConfigValidator interface {
	Validate(config *UnifiedConfig) error
}

// NewConfigManager creates a new configuration manager
func NewConfigManager() *ConfigManager {
	return &ConfigManager{
		config:     Default(),
		sources:    make([]ConfigSource, 0),
		validators: make([]ConfigValidator, 0),
	}
}

// AddSource adds a configuration source
func (cm *ConfigManager) AddSource(source ConfigSource) {
	cm.sources = append(cm.sources, source)
}

// AddValidator adds a configuration validator
func (cm *ConfigManager) AddValidator(validator ConfigValidator) {
	cm.validators = append(cm.validators, validator)
}

// Load loads configuration from all sources and validates it
func (cm *ConfigManager) Load() error {
	// Start with default configuration
	merged := Default()

	// Sort sources by priority (highest first)
	sources := make([]ConfigSource, len(cm.sources))
	copy(sources, cm.sources)
	for i := 0; i < len(sources)-1; i++ {
		for j := i + 1; j < len(sources); j++ {
			if sources[i].Priority() < sources[j].Priority() {
				sources[i], sources[j] = sources[j], sources[i]
			}
		}
	}

	// Load and merge configurations from sources
	for _, source := range sources {
		config, err := source.Load()
		if err != nil {
			return fmt.Errorf("failed to load config from %s: %w", source.Name(), err)
		}

		if config != nil {
			if err := mergeConfigs(merged, config); err != nil {
				return fmt.Errorf("failed to merge config from %s: %w", source.Name(), err)
			}
		}
	}

	// Validate merged configuration
	if err := merged.Validate(); err != nil {
		return fmt.Errorf("configuration validation failed: %w", err)
	}

	// Run additional validators
	for _, validator := range cm.validators {
		if err := validator.Validate(merged); err != nil {
			return fmt.Errorf("configuration validation failed: %w", err)
		}
	}

	cm.config = merged
	return nil
}

// GetConfig returns the current configuration
func (cm *ConfigManager) GetConfig() *UnifiedConfig {
	return cm.config
}

// mergeConfigs merges source config into target config
func mergeConfigs(target, source *UnifiedConfig) error {
	// Use JSON marshaling/unmarshaling for deep merge
	// This approach preserves zero values and handles nested structures

	targetJSON, err := json.Marshal(target)
	if err != nil {
		return fmt.Errorf("failed to marshal target config: %w", err)
	}

	sourceJSON, err := json.Marshal(source)
	if err != nil {
		return fmt.Errorf("failed to marshal source config: %w", err)
	}

	// Parse into maps for merging
	var targetMap, sourceMap map[string]interface{}

	if err := json.Unmarshal(targetJSON, &targetMap); err != nil {
		return fmt.Errorf("failed to unmarshal target config: %w", err)
	}

	if err := json.Unmarshal(sourceJSON, &sourceMap); err != nil {
		return fmt.Errorf("failed to unmarshal source config: %w", err)
	}

	// Deep merge maps
	mergeMaps(targetMap, sourceMap)

	// Convert back to struct
	mergedJSON, err := json.Marshal(targetMap)
	if err != nil {
		return fmt.Errorf("failed to marshal merged config: %w", err)
	}

	if err := json.Unmarshal(mergedJSON, target); err != nil {
		return fmt.Errorf("failed to unmarshal merged config: %w", err)
	}

	return nil
}

// mergeMaps recursively merges source map into target map
func mergeMaps(target, source map[string]interface{}) {
	for key, sourceValue := range source {
		if targetValue, exists := target[key]; exists {
			// If both values are maps, merge recursively
			if targetMap, ok := targetValue.(map[string]interface{}); ok {
				if sourceMap, ok := sourceValue.(map[string]interface{}); ok {
					mergeMaps(targetMap, sourceMap)
					continue
				}
			}
		}
		// Override or set the value
		target[key] = sourceValue
	}
}

// FileConfigSource loads configuration from a file
type FileConfigSource struct {
	path     string
	priority int
}

// NewFileConfigSource creates a new file configuration source
func NewFileConfigSource(path string, priority int) *FileConfigSource {
	return &FileConfigSource{
		path:     path,
		priority: priority,
	}
}

func (f *FileConfigSource) Name() string {
	return fmt.Sprintf("file:%s", f.path)
}

func (f *FileConfigSource) Priority() int {
	return f.priority
}

func (f *FileConfigSource) Load() (*UnifiedConfig, error) {
	if _, err := os.Stat(f.path); os.IsNotExist(err) {
		// File doesn't exist, return nil (no config to load)
		return nil, nil
	}

	data, err := os.ReadFile(f.path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	config := &UnifiedConfig{}
	ext := strings.ToLower(filepath.Ext(f.path))

	switch ext {
	case ".json":
		if err := json.Unmarshal(data, config); err != nil {
			return nil, fmt.Errorf("failed to parse JSON config: %w", err)
		}
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, config); err != nil {
			return nil, fmt.Errorf("failed to parse YAML config: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported config file format: %s", ext)
	}

	return config, nil
}

// EnvironmentConfigSource loads configuration from environment variables
type EnvironmentConfigSource struct {
	prefix   string
	priority int
}

// NewEnvironmentConfigSource creates a new environment configuration source
func NewEnvironmentConfigSource(prefix string, priority int) *EnvironmentConfigSource {
	return &EnvironmentConfigSource{
		prefix:   prefix,
		priority: priority,
	}
}

func (e *EnvironmentConfigSource) Name() string {
	return fmt.Sprintf("env:%s", e.prefix)
}

func (e *EnvironmentConfigSource) Priority() int {
	return e.priority
}

func (e *EnvironmentConfigSource) Load() (*UnifiedConfig, error) {
	config := &UnifiedConfig{}

	// Map environment variables to config fields
	envMappings := map[string]func(string) error{
		e.prefix + "_CONNECTION_TYPE": func(value string) error {
			// Parse and set connection type
			return nil // Implementation depends on specific requirements
		},
		e.prefix + "_DEBUG_ENABLED": func(value string) error {
			config.Debug.Enabled = strings.ToLower(value) == "true"
			return nil
		},
		e.prefix + "_LOG_LEVEL": func(value string) error {
			config.Debug.LogLevel = value
			return nil
		},
		e.prefix + "_CONNECTION_TIMEOUT": func(value string) error {
			duration, err := time.ParseDuration(value)
			if err != nil {
				return err
			}
			config.Connection.ConnectionTimeout = duration
			return nil
		},
		// Add more mappings as needed
	}

	// Process environment variables
	for envVar, setter := range envMappings {
		if value := os.Getenv(envVar); value != "" {
			if err := setter(value); err != nil {
				return nil, fmt.Errorf("failed to parse %s: %w", envVar, err)
			}
		}
	}

	return config, nil
}

// CLIConfigSource loads configuration from CLI flags/arguments
type CLIConfigSource struct {
	values   map[string]interface{}
	priority int
}

// NewCLIConfigSource creates a new CLI configuration source
func NewCLIConfigSource(priority int) *CLIConfigSource {
	return &CLIConfigSource{
		values:   make(map[string]interface{}),
		priority: priority,
	}
}

func (c *CLIConfigSource) Name() string {
	return "cli"
}

func (c *CLIConfigSource) Priority() int {
	return c.priority
}

// SetValue sets a configuration value from CLI
func (c *CLIConfigSource) SetValue(path string, value interface{}) {
	c.values[path] = value
}

func (c *CLIConfigSource) Load() (*UnifiedConfig, error) {
	if len(c.values) == 0 {
		return nil, nil
	}

	config := &UnifiedConfig{}

	// Map CLI values to config fields
	for path, value := range c.values {
		if err := c.setConfigValue(config, path, value); err != nil {
			return nil, fmt.Errorf("failed to set CLI value %s: %w", path, err)
		}
	}

	return config, nil
}

// setConfigValue sets a nested config value using dot notation
func (c *CLIConfigSource) setConfigValue(config *UnifiedConfig, path string, value interface{}) error {
	parts := strings.Split(path, ".")
	if len(parts) == 0 {
		return fmt.Errorf("empty config path")
	}

	// Simple implementation for common cases
	switch path {
	case "debug.enabled":
		if b, ok := value.(bool); ok {
			config.Debug.Enabled = b
		}
	case "debug.log_level":
		if s, ok := value.(string); ok {
			config.Debug.LogLevel = s
		}
	case "connection.type":
		if _, ok := value.(string); ok {
			// Convert string to TransportType
			// Implementation depends on specific requirements
		}
	case "connection.command":
		if s, ok := value.(string); ok {
			config.Connection.Command = s
		}
	case "connection.url":
		if s, ok := value.(string); ok {
			config.Connection.URL = s
		}
	// Add more cases as needed
	default:
		return fmt.Errorf("unsupported config path: %s", path)
	}

	return nil
}

// SecurityValidator validates security-related configuration
type SecurityValidator struct{}

func (sv *SecurityValidator) Validate(config *UnifiedConfig) error {
	// Validate command security for STDIO transport
	if config.Connection.Type.String() == "stdio" {
		if config.Connection.DenyUnsafeCommands {
			if err := sv.validateCommand(config.Connection.Command); err != nil {
				return fmt.Errorf("unsafe command detected: %w", err)
			}
		}
	}

	// Validate HTTP security settings
	if config.Transport.HTTP.TLSInsecureSkipVerify {
		// Log warning but don't fail
		fmt.Fprintf(os.Stderr, "WARNING: TLS verification is disabled\n")
	}

	return nil
}

func (sv *SecurityValidator) validateCommand(command string) error {
	// List of potentially dangerous commands
	dangerousCommands := []string{
		"rm", "del", "format", "mkfs", "dd",
		"sudo", "su", "chmod", "chown",
		"curl", "wget", "nc", "netcat",
	}

	for _, dangerous := range dangerousCommands {
		if strings.Contains(command, dangerous) {
			return fmt.Errorf("potentially dangerous command: %s", dangerous)
		}
	}

	return nil
}

// PerformanceValidator validates performance-related configuration
type PerformanceValidator struct{}

func (pv *PerformanceValidator) Validate(config *UnifiedConfig) error {
	// Check for reasonable timeout values
	if config.Connection.ConnectionTimeout > 5*time.Minute {
		fmt.Fprintf(os.Stderr, "WARNING: Connection timeout is very high (%v)\n", config.Connection.ConnectionTimeout)
	}

	// Check for reasonable buffer sizes
	if config.Transport.SSE.BufferSize > 1024*1024 { // 1MB
		fmt.Fprintf(os.Stderr, "WARNING: SSE buffer size is very high (%d bytes)\n", config.Transport.SSE.BufferSize)
	}

	// Check for reasonable max processes
	if config.Transport.STDIO.MaxProcesses > 50 {
		fmt.Fprintf(os.Stderr, "WARNING: Max processes is very high (%d)\n", config.Transport.STDIO.MaxProcesses)
	}

	return nil
}
