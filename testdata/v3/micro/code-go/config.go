// Package config provides hierarchical configuration loading
// with environment variable overrides and validation.
//
// This is a representative Go file for the micro-fixture, exercising
// the tree-sitter Go grammar with structs, interfaces, methods, and
// error handling patterns typical of real-world Go code.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all application configuration.
type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	Logging  LoggingConfig  `yaml:"logging"`
}

// ServerConfig defines HTTP server parameters.
type ServerConfig struct {
	Host         string        `yaml:"host"`
	Port         int           `yaml:"port"`
	ReadTimeout  time.Duration `yaml:"read_timeout"`
	WriteTimeout time.Duration `yaml:"write_timeout"`
	MaxBodySize  int64         `yaml:"max_body_size"`
}

// DatabaseConfig defines database connection parameters.
type DatabaseConfig struct {
	Driver          string        `yaml:"driver"`
	DSN             string        `yaml:"dsn"`
	MaxOpenConns    int           `yaml:"max_open_conns"`
	MaxIdleConns    int           `yaml:"max_idle_conns"`
	ConnMaxLifetime time.Duration `yaml:"conn_max_lifetime"`
}

// LoggingConfig defines structured logging parameters.
type LoggingConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"` // "json" or "text"
	Output string `yaml:"output"` // "stdout", "stderr", or file path
}

// Validator validates configuration values.
type Validator interface {
	Validate() error
}

// Validate checks all configuration sections.
func (c *Config) Validate() error {
	var errs []error
	if err := c.Server.Validate(); err != nil {
		errs = append(errs, fmt.Errorf("server: %w", err))
	}
	if err := c.Database.Validate(); err != nil {
		errs = append(errs, fmt.Errorf("database: %w", err))
	}
	if err := c.Logging.Validate(); err != nil {
		errs = append(errs, fmt.Errorf("logging: %w", err))
	}
	return errors.Join(errs...)
}

// Validate checks server configuration values.
func (s *ServerConfig) Validate() error {
	if s.Port < 1 || s.Port > 65535 {
		return fmt.Errorf("port must be 1-65535, got %d", s.Port)
	}
	if s.ReadTimeout <= 0 {
		return errors.New("read_timeout must be positive")
	}
	if s.MaxBodySize <= 0 {
		return errors.New("max_body_size must be positive")
	}
	return nil
}

// Validate checks database configuration values.
func (d *DatabaseConfig) Validate() error {
	if d.DSN == "" {
		return errors.New("dsn is required")
	}
	validDrivers := map[string]bool{"postgres": true, "sqlite3": true, "mysql": true}
	if !validDrivers[d.Driver] {
		return fmt.Errorf("unsupported driver %q", d.Driver)
	}
	if d.MaxOpenConns < 1 {
		return errors.New("max_open_conns must be >= 1")
	}
	return nil
}

// Validate checks logging configuration values.
func (l *LoggingConfig) Validate() error {
	validLevels := map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
	if !validLevels[l.Level] {
		return fmt.Errorf("unsupported log level %q", l.Level)
	}
	if l.Format != "json" && l.Format != "text" {
		return fmt.Errorf("format must be 'json' or 'text', got %q", l.Format)
	}
	return nil
}

// Defaults returns a Config with sensible default values.
func Defaults() Config {
	return Config{
		Server: ServerConfig{
			Host:         "0.0.0.0",
			Port:         8080,
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 30 * time.Second,
			MaxBodySize:  10 << 20, // 10 MB
		},
		Database: DatabaseConfig{
			Driver:          "sqlite3",
			DSN:             "file:app.db?_journal=WAL",
			MaxOpenConns:    1,
			MaxIdleConns:    1,
			ConnMaxLifetime: 5 * time.Minute,
		},
		Logging: LoggingConfig{
			Level:  "info",
			Format: "json",
			Output: "stderr",
		},
	}
}

// FromEnv overlays environment variable overrides on top of defaults.
// Environment variables follow the pattern MYAPP_SECTION_KEY.
func FromEnv(base Config) Config {
	if v := os.Getenv("MYAPP_SERVER_HOST"); v != "" {
		base.Server.Host = v
	}
	if v := os.Getenv("MYAPP_SERVER_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil {
			base.Server.Port = port
		}
	}
	if v := os.Getenv("MYAPP_DATABASE_DSN"); v != "" {
		base.Database.DSN = v
	}
	if v := os.Getenv("MYAPP_DATABASE_DRIVER"); v != "" {
		base.Database.Driver = v
	}
	if v := os.Getenv("MYAPP_LOG_LEVEL"); v != "" {
		base.Logging.Level = strings.ToLower(v)
	}
	return base
}
