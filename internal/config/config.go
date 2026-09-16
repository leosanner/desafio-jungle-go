package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultMigrationsPath = "./migrations"
)

// Sentinel errors for classification in tests and adapters.
var (
	ErrMissingEnv      = errors.New("missing required environment variable")
	ErrInvalidLogLevel = errors.New("invalid LOG_LEVEL")
	ErrInvalidDuration = errors.New("invalid duration")
	ErrInvalidInt      = errors.New("invalid integer")
)

// Config is process configuration loaded from the environment.
type Config struct {
	LogLevel             string
	HTTPAddr             string
	PostgresDSN          string
	MigrationsPath       string
	AWSRegion            string
	AWSAccessKeyID       string
	AWSSecretAccessKey   string
	AWSEndpointURL       string
	SQSWagerQueueName    string
	SQSWagerDLQName      string
	SQSEventsQueueName   string
	FXStartTimeout       time.Duration
	FXStopTimeout        time.Duration
	OutboxPollInterval   time.Duration
	OutboxBatchSize      int
	OutboxLease          time.Duration
	OutboxBackoffMax     time.Duration
	SQSVisibilityTimeout time.Duration
	SQSWaitTime          time.Duration
	SQSBackoffMax        time.Duration
	OIDCIssuer           string
	OIDCAudience         string
	OIDCJWKSURL          string
	OIDCInternalClient   string
}

// Load reads configuration from the environment and validates it.
func Load() (Config, error) {
	startTimeout, err := parseDuration("FX_START_TIMEOUT")
	if err != nil {
		return Config{}, err
	}
	stopTimeout, err := parseDuration("FX_STOP_TIMEOUT")
	if err != nil {
		return Config{}, err
	}
	pollInterval, err := parseDuration("OUTBOX_POLL_INTERVAL")
	if err != nil {
		return Config{}, err
	}
	lease, err := parseDuration("OUTBOX_LEASE")
	if err != nil {
		return Config{}, err
	}
	backoffMax, err := parseDuration("OUTBOX_BACKOFF_MAX")
	if err != nil {
		return Config{}, err
	}
	batchSize, err := parsePositiveInt("OUTBOX_BATCH_SIZE")
	if err != nil {
		return Config{}, err
	}
	visibility, err := parseDuration("SQS_VISIBILITY_TIMEOUT")
	if err != nil {
		return Config{}, err
	}
	waitTime, err := parseDuration("SQS_WAIT_TIME")
	if err != nil {
		return Config{}, err
	}
	sqsBackoff, err := parseDuration("SQS_BACKOFF_MAX")
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		LogLevel:             os.Getenv("LOG_LEVEL"),
		HTTPAddr:             os.Getenv("HTTP_ADDR"),
		PostgresDSN:          os.Getenv("POSTGRES_DSN"),
		MigrationsPath:       os.Getenv("MIGRATIONS_PATH"),
		AWSRegion:            os.Getenv("AWS_REGION"),
		AWSAccessKeyID:       os.Getenv("AWS_ACCESS_KEY_ID"),
		AWSSecretAccessKey:   os.Getenv("AWS_SECRET_ACCESS_KEY"),
		AWSEndpointURL:       strings.TrimSpace(os.Getenv("AWS_ENDPOINT_URL")),
		SQSWagerQueueName:    os.Getenv("SQS_WAGER_QUEUE_NAME"),
		SQSWagerDLQName:      os.Getenv("SQS_WAGER_DLQ_NAME"),
		SQSEventsQueueName:   os.Getenv("SQS_EVENTS_QUEUE_NAME"),
		FXStartTimeout:       startTimeout,
		FXStopTimeout:        stopTimeout,
		OutboxPollInterval:   pollInterval,
		OutboxBatchSize:      batchSize,
		OutboxLease:          lease,
		OutboxBackoffMax:     backoffMax,
		SQSVisibilityTimeout: visibility,
		SQSWaitTime:          waitTime,
		SQSBackoffMax:        sqsBackoff,
		OIDCIssuer:           strings.TrimSpace(os.Getenv("OIDC_ISSUER")),
		OIDCAudience:         strings.TrimSpace(os.Getenv("OIDC_AUDIENCE")),
		OIDCJWKSURL:          strings.TrimSpace(os.Getenv("OIDC_JWKS_URL")),
		OIDCInternalClient:   strings.TrimSpace(os.Getenv("OIDC_INTERNAL_CLIENT")),
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate checks required fields and enumerations. AWS_ENDPOINT_URL and
// OIDC_JWKS_URL may be empty (real AWS / JWKS derived from OIDC_ISSUER).
// MIGRATIONS_PATH defaults to ./migrations.
func (c *Config) Validate() error {
	if err := require("LOG_LEVEL", c.LogLevel); err != nil {
		return err
	}
	if err := parseLogLevel(c.LogLevel); err != nil {
		return err
	}
	if err := require("HTTP_ADDR", c.HTTPAddr); err != nil {
		return err
	}
	if err := require("POSTGRES_DSN", c.PostgresDSN); err != nil {
		return err
	}
	if strings.TrimSpace(c.MigrationsPath) == "" {
		c.MigrationsPath = defaultMigrationsPath
	}
	if err := require("AWS_REGION", c.AWSRegion); err != nil {
		return err
	}
	if err := require("AWS_ACCESS_KEY_ID", c.AWSAccessKeyID); err != nil {
		return err
	}
	if err := require("AWS_SECRET_ACCESS_KEY", c.AWSSecretAccessKey); err != nil {
		return err
	}
	if err := require("SQS_WAGER_QUEUE_NAME", c.SQSWagerQueueName); err != nil {
		return err
	}
	if err := require("SQS_WAGER_DLQ_NAME", c.SQSWagerDLQName); err != nil {
		return err
	}
	if err := require("SQS_EVENTS_QUEUE_NAME", c.SQSEventsQueueName); err != nil {
		return err
	}
	c.OIDCIssuer = strings.TrimRight(c.OIDCIssuer, "/")
	if err := require("OIDC_ISSUER", c.OIDCIssuer); err != nil {
		return err
	}
	if err := require("OIDC_AUDIENCE", c.OIDCAudience); err != nil {
		return err
	}
	if err := require("OIDC_INTERNAL_CLIENT", c.OIDCInternalClient); err != nil {
		return err
	}
	if c.FXStartTimeout <= 0 {
		return fmt.Errorf("%w: FX_START_TIMEOUT must be positive", ErrInvalidDuration)
	}
	if c.FXStopTimeout <= 0 {
		return fmt.Errorf("%w: FX_STOP_TIMEOUT must be positive", ErrInvalidDuration)
	}
	if c.OutboxPollInterval <= 0 {
		return fmt.Errorf("%w: OUTBOX_POLL_INTERVAL must be positive", ErrInvalidDuration)
	}
	if c.OutboxLease <= 0 {
		return fmt.Errorf("%w: OUTBOX_LEASE must be positive", ErrInvalidDuration)
	}
	if c.OutboxBackoffMax <= 0 {
		return fmt.Errorf("%w: OUTBOX_BACKOFF_MAX must be positive", ErrInvalidDuration)
	}
	if c.OutboxBatchSize <= 0 {
		return fmt.Errorf("%w: OUTBOX_BATCH_SIZE must be positive", ErrInvalidInt)
	}
	if c.SQSVisibilityTimeout <= 0 {
		return fmt.Errorf("%w: SQS_VISIBILITY_TIMEOUT must be positive", ErrInvalidDuration)
	}
	if c.SQSWaitTime <= 0 {
		return fmt.Errorf("%w: SQS_WAIT_TIME must be positive", ErrInvalidDuration)
	}
	if c.SQSWaitTime > 20*time.Second {
		return fmt.Errorf("%w: SQS_WAIT_TIME must be at most 20s", ErrInvalidDuration)
	}
	if c.SQSBackoffMax <= 0 {
		return fmt.Errorf("%w: SQS_BACKOFF_MAX must be positive", ErrInvalidDuration)
	}
	return nil
}

// SlogLevel maps LOG_LEVEL to a slog level. Validate must have succeeded.
func (c Config) SlogLevel() slog.Level {
	switch strings.ToLower(strings.TrimSpace(c.LogLevel)) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// NewLogger returns a JSON slog logger at the configured level.
func NewLogger(cfg Config) *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: cfg.SlogLevel(),
	}))
}

func require(key, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%w: %s", ErrMissingEnv, key)
	}
	return nil
}

func parseLogLevel(value string) error {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "debug", "info", "warn", "error":
		return nil
	default:
		return fmt.Errorf("%w: %q (want debug, info, warn or error)", ErrInvalidLogLevel, value)
	}
}

func parseDuration(key string) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return 0, fmt.Errorf("%w: %s", ErrMissingEnv, key)
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%w: %s: %w", ErrInvalidDuration, key, err)
	}
	return d, nil
}

func parsePositiveInt(key string) (int, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return 0, fmt.Errorf("%w: %s", ErrMissingEnv, key)
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%w: %s: %w", ErrInvalidInt, key, err)
	}
	if n <= 0 {
		return 0, fmt.Errorf("%w: %s must be positive", ErrInvalidInt, key)
	}
	return n, nil
}
