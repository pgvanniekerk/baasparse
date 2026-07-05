// Package settings reads and writes the baasparse instance configuration.
//
// Configuration comes from, in increasing precedence: defaults → the YAML file
// at ~/.baasparse/.config → environment variables → command flags (applied by
// callers). The file is the local/dev layer; environment variables are
// authoritative in containers (K8s ConfigMap/Secret), per TS 16 §16.9. Load()
// reads the file only (used for save round-trips); Resolve() overlays the
// environment (used at runtime).
package settings

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"gopkg.in/yaml.v3"
)

// Defaults.
const (
	DefaultPort   = 8080
	DefaultSchema = "baasparse"
	dirName       = ".baasparse"
	configName    = ".config"
	logsName      = "logs"
	logFileName   = "baasparse.log"
)

// Settings is the on-disk + resolved configuration.
type Settings struct {
	DatabaseURL string          `yaml:"database_url"`
	Port        int             `yaml:"port"`
	DBSchema    string          `yaml:"db_schema"`
	Storage     StorageSettings `yaml:"storage"`
	Log         LogSettings     `yaml:"log"`
	Otel        OtelSettings    `yaml:"otel"`
}

// StorageSettings selects the data-plane storage backend (TS 16 §16.2).
type StorageSettings struct {
	Backend string     `yaml:"backend"` // posix | s3 (default posix)
	Root    string     `yaml:"root"`    // posix base dir; "" = keys are paths
	S3      S3Settings `yaml:"s3"`
}

// S3Settings configures the S3-compatible backend.
type S3Settings struct {
	Endpoint     string `yaml:"endpoint"`
	Region       string `yaml:"region"`
	Bucket       string `yaml:"bucket"`
	AccessKey    string `yaml:"access_key"`
	SecretKey    string `yaml:"secret_key"`
	UseSSL       bool   `yaml:"use_ssl"`
	CreateBucket bool   `yaml:"create_bucket"`
}

// LogSettings controls log rendering and sink.
type LogSettings struct {
	Format string `yaml:"format"` // json (default) | text  — slog handler for the log sink
	Output string `yaml:"output"` // file (default, local) | stdout (containers)
}

// OtelSettings configures OpenTelemetry export (TS 16 §16.8).
type OtelSettings struct {
	Endpoint string `yaml:"endpoint"` // OTLP/HTTP endpoint; empty disables export
	Insecure bool   `yaml:"insecure"`
}

// Dir returns ~/.baasparse.
func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, dirName), nil
}

// ConfigPath returns ~/.baasparse/.config.
func ConfigPath() (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, configName), nil
}

// LogsDir returns ~/.baasparse/logs.
func LogsDir() (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, logsName), nil
}

// LogFilePath returns ~/.baasparse/logs/baasparse.log.
func LogFilePath() (string, error) {
	d, err := LogsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, logFileName), nil
}

// EnsureDirs creates ~/.baasparse and its logs subdirectory.
func EnsureDirs() error {
	logs, err := LogsDir()
	if err != nil {
		return err
	}
	return os.MkdirAll(logs, 0o700)
}

// Load reads the config file (with defaults), WITHOUT the environment overlay —
// used for save round-trips so env values are never persisted to disk.
func Load() (Settings, error) {
	s := defaults()
	p, err := ConfigPath()
	if err != nil {
		// No resolvable home directory (e.g. a container with $HOME unset): there is
		// no config file to read, and that is not an error — configuration comes from
		// the environment overlay (Resolve) in that case. Degrade to defaults.
		return s, nil
	}
	data, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return s, fmt.Errorf("read config %s: %w", p, err)
	}
	if err := yaml.Unmarshal(data, &s); err != nil {
		return s, fmt.Errorf("parse config %s: %w", p, err)
	}
	applyDefaults(&s)
	return s, nil
}

// Resolve reads the config file and overlays environment variables (authoritative
// in containers). Used at runtime and by the CLI commands that connect.
func Resolve() (Settings, error) {
	s, err := Load()
	if err != nil {
		return s, err
	}
	if err := applyEnv(&s); err != nil {
		return s, err
	}
	applyDefaults(&s)
	return s, nil
}

func defaults() Settings {
	return Settings{
		Port:     DefaultPort,
		DBSchema: DefaultSchema,
		Storage:  StorageSettings{Backend: "posix"},
		Log:      LogSettings{Format: "json", Output: "file"},
	}
}

func applyDefaults(s *Settings) {
	if s.Port == 0 {
		s.Port = DefaultPort
	}
	if s.DBSchema == "" {
		s.DBSchema = DefaultSchema
	}
	if s.Storage.Backend == "" {
		s.Storage.Backend = "posix"
	}
	if s.Log.Format == "" {
		s.Log.Format = "json"
	}
	if s.Log.Output == "" {
		s.Log.Output = "file"
	}
}

// applyEnv overlays environment variables (K8s ConfigMap/Secret) over the file.
func applyEnv(s *Settings) error {
	setStr(&s.DatabaseURL, "DATABASE_URL")
	if v := os.Getenv("BAASPARSE_PORT"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("BAASPARSE_PORT must be a number: %q", v)
		}
		if n < 1 || n > 65535 {
			return fmt.Errorf("BAASPARSE_PORT out of range: %d", n)
		}
		s.Port = n
	}
	setStr(&s.DBSchema, "BAASPARSE_DB_SCHEMA")

	setStr(&s.Storage.Backend, "BAASPARSE_STORAGE_BACKEND")
	setStr(&s.Storage.Root, "BAASPARSE_STORAGE_ROOT")
	setStr(&s.Storage.S3.Endpoint, "BAASPARSE_S3_ENDPOINT")
	setStr(&s.Storage.S3.Region, "BAASPARSE_S3_REGION")
	setStr(&s.Storage.S3.Bucket, "BAASPARSE_S3_BUCKET")
	setStr(&s.Storage.S3.AccessKey, "BAASPARSE_S3_ACCESS_KEY")
	setStr(&s.Storage.S3.AccessKey, "AWS_ACCESS_KEY_ID") // fallback
	setStr(&s.Storage.S3.SecretKey, "BAASPARSE_S3_SECRET_KEY")
	setStr(&s.Storage.S3.SecretKey, "AWS_SECRET_ACCESS_KEY") // fallback
	setBool(&s.Storage.S3.UseSSL, "BAASPARSE_S3_USE_SSL")
	setBool(&s.Storage.S3.CreateBucket, "BAASPARSE_S3_CREATE_BUCKET")

	setStr(&s.Log.Format, "BAASPARSE_LOG_FORMAT")
	setStr(&s.Log.Output, "BAASPARSE_LOG_OUTPUT")

	setStr(&s.Otel.Endpoint, "OTEL_EXPORTER_OTLP_ENDPOINT")
	setStr(&s.Otel.Endpoint, "BAASPARSE_OTEL_ENDPOINT")
	setBool(&s.Otel.Insecure, "BAASPARSE_OTEL_INSECURE")
	return nil
}

func setStr(dst *string, env string) {
	if v := os.Getenv(env); v != "" {
		*dst = v
	}
}

func setBool(dst *bool, env string) {
	if v := os.Getenv(env); v != "" {
		*dst = v == "1" || v == "true" || v == "yes"
	}
}

// Save writes the config file (restricted perms — it can hold credentials).
func Save(s Settings) error {
	if err := EnsureDirs(); err != nil {
		return err
	}
	p, err := ConfigPath()
	if err != nil {
		return err
	}
	out, err := yaml.Marshal(s)
	if err != nil {
		return err
	}
	if err := os.WriteFile(p, out, 0o600); err != nil {
		return fmt.Errorf("write config %s: %w", p, err)
	}
	return nil
}

// SetDatabaseURL updates just the connection string (file only).
func SetDatabaseURL(url string) error {
	s, err := Load()
	if err != nil {
		return err
	}
	s.DatabaseURL = url
	return Save(s)
}

// SetPort updates just the port (file only).
func SetPort(port int) error {
	s, err := Load()
	if err != nil {
		return err
	}
	s.Port = port
	return Save(s)
}
