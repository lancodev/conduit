package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config holds all configuration for conduit.
type Config struct {
	DefaultWorkspace  string     `yaml:"default_workspace"`
	DefaultRepository string     `yaml:"default_repository"`
	RefreshInterval   int        `yaml:"refresh_interval"`
	Auth              AuthConfig `yaml:"auth"`
	Favorites         []string   `yaml:"favorites,omitempty"`
}

// AuthConfig holds OAuth credentials.
type AuthConfig struct {
	ClientID     string `yaml:"client_id"`
	ClientSecret string `yaml:"client_secret"`
}

const (
	defaultWorkspace  = "creative2"
	defaultRepository = "fgi-content-library"
	defaultInterval   = 10
)

// configDir returns ~/.config/conduit.
func configDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "conduit")
}

// configPath returns the full path to the config file.
func configPath() string {
	return filepath.Join(configDir(), "config.yml")
}

// LoadConfig reads config from file, then applies env var overrides.
func LoadConfig() Config {
	cfg := Config{
		DefaultWorkspace:  defaultWorkspace,
		DefaultRepository: defaultRepository,
		RefreshInterval:   defaultInterval,
	}

	data, err := os.ReadFile(configPath())
	if err == nil {
		_ = yaml.Unmarshal(data, &cfg)
	}

	// Ensure defaults for missing values.
	if cfg.RefreshInterval <= 0 {
		cfg.RefreshInterval = defaultInterval
	}

	// Env var overrides (higher priority).
	if v := os.Getenv("CONDUIT_CLIENT_ID"); v != "" {
		cfg.Auth.ClientID = v
	}
	if v := os.Getenv("CONDUIT_CLIENT_SECRET"); v != "" {
		cfg.Auth.ClientSecret = v
	}
	if v := os.Getenv("CONDUIT_WORKSPACE"); v != "" {
		cfg.DefaultWorkspace = v
	}
	if v := os.Getenv("CONDUIT_REPOSITORY"); v != "" {
		cfg.DefaultRepository = v
	}

	return cfg
}

// RunAuthSetup runs the interactive auth setup command.
func RunAuthSetup() error {
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("conduit auth setup")
	fmt.Println(strings.Repeat("─", 40))
	fmt.Println()

	clientID := prompt(reader, "Bitbucket OAuth Client ID", "")
	clientSecret := prompt(reader, "Bitbucket OAuth Client Secret", "")
	workspace := prompt(reader, "Default workspace", defaultWorkspace)
	repo := prompt(reader, "Default repository", defaultRepository)

	cfg := Config{
		DefaultWorkspace:  workspace,
		DefaultRepository: repo,
		RefreshInterval:   defaultInterval,
		Auth: AuthConfig{
			ClientID:     clientID,
			ClientSecret: clientSecret,
		},
	}

	data, err := yaml.Marshal(&cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	dir := configDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("failed to create config dir: %w", err)
	}

	if err := os.WriteFile(configPath(), data, 0o600); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	fmt.Println()
	fmt.Printf("Config written to %s\n", configPath())
	return nil
}

// SaveConfig writes the config back to disk.
func SaveConfig(cfg Config) error {
	data, err := yaml.Marshal(&cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	dir := configDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("failed to create config dir: %w", err)
	}

	if err := os.WriteFile(configPath(), data, 0o600); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	return nil
}

// prompt asks the user for input with an optional default value.
func prompt(reader *bufio.Reader, label, defaultVal string) string {
	if defaultVal != "" {
		fmt.Printf("%s [%s]: ", label, defaultVal)
	} else {
		fmt.Printf("%s: ", label)
	}

	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)

	if input == "" {
		return defaultVal
	}
	return input
}
