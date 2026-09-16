package config

import (
	"errors"
	"testing"
	"time"
)

func setValidEnv(t *testing.T) {
	t.Helper()
	t.Setenv("LOG_LEVEL", "info")
	t.Setenv("HTTP_ADDR", ":8080")
	t.Setenv("POSTGRES_DSN", "postgres://wagering:wagering@localhost:5432/wagering?sslmode=disable")
	t.Setenv("MIGRATIONS_PATH", "./migrations")
	t.Setenv("AWS_REGION", "us-east-1")
	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	t.Setenv("AWS_ENDPOINT_URL", "http://localhost:4566")
	t.Setenv("SQS_WAGER_QUEUE_NAME", "wager-transactions.fifo")
	t.Setenv("SQS_WAGER_DLQ_NAME", "wager-transactions-dlq.fifo")
	t.Setenv("FX_START_TIMEOUT", "15s")
	t.Setenv("FX_STOP_TIMEOUT", "30s")
	t.Setenv("OIDC_ISSUER", "http://localhost:8081/realms/wagering")
	t.Setenv("OIDC_AUDIENCE", "wagering-api")
	t.Setenv("OIDC_INTERNAL_CLIENT", "wagering-internal")
}

func TestLoadSuccess(t *testing.T) {
	setValidEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.HTTPAddr != ":8080" {
		t.Errorf("HTTPAddr = %q", cfg.HTTPAddr)
	}
	if cfg.FXStartTimeout != 15*time.Second {
		t.Errorf("FXStartTimeout = %s", cfg.FXStartTimeout)
	}
	if cfg.FXStopTimeout != 30*time.Second {
		t.Errorf("FXStopTimeout = %s", cfg.FXStopTimeout)
	}
	if cfg.AWSEndpointURL != "http://localhost:4566" {
		t.Errorf("AWSEndpointURL = %q", cfg.AWSEndpointURL)
	}
	if cfg.OIDCIssuer != "http://localhost:8081/realms/wagering" {
		t.Errorf("OIDCIssuer = %q", cfg.OIDCIssuer)
	}
	if cfg.OIDCAudience != "wagering-api" {
		t.Errorf("OIDCAudience = %q", cfg.OIDCAudience)
	}
	if cfg.OIDCInternalClient != "wagering-internal" {
		t.Errorf("OIDCInternalClient = %q", cfg.OIDCInternalClient)
	}
}

func TestLoadDefaultMigrationsPath(t *testing.T) {
	setValidEnv(t)
	t.Setenv("MIGRATIONS_PATH", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.MigrationsPath != defaultMigrationsPath {
		t.Errorf("MigrationsPath = %q, want %q", cfg.MigrationsPath, defaultMigrationsPath)
	}
}

func TestLoadOptionalOIDCJWKSURL(t *testing.T) {
	setValidEnv(t)
	t.Setenv("OIDC_JWKS_URL", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.OIDCJWKSURL != "" {
		t.Errorf("OIDCJWKSURL = %q, want empty", cfg.OIDCJWKSURL)
	}
}

func TestLoadTrimsIssuerSlash(t *testing.T) {
	setValidEnv(t)
	t.Setenv("OIDC_ISSUER", "http://localhost:8081/realms/wagering/")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.OIDCIssuer != "http://localhost:8081/realms/wagering" {
		t.Errorf("OIDCIssuer = %q", cfg.OIDCIssuer)
	}
}

func TestLoadOptionalAWSEndpoint(t *testing.T) {
	setValidEnv(t)
	t.Setenv("AWS_ENDPOINT_URL", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.AWSEndpointURL != "" {
		t.Errorf("AWSEndpointURL = %q, want empty", cfg.AWSEndpointURL)
	}
}

func TestLoadMissingRequired(t *testing.T) {
	keys := []string{
		"LOG_LEVEL",
		"HTTP_ADDR",
		"POSTGRES_DSN",
		"AWS_REGION",
		"AWS_ACCESS_KEY_ID",
		"AWS_SECRET_ACCESS_KEY",
		"SQS_WAGER_QUEUE_NAME",
		"SQS_WAGER_DLQ_NAME",
		"FX_START_TIMEOUT",
		"FX_STOP_TIMEOUT",
		"OIDC_ISSUER",
		"OIDC_AUDIENCE",
		"OIDC_INTERNAL_CLIENT",
	}
	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			setValidEnv(t)
			t.Setenv(key, "")
			_, err := Load()
			if !errors.Is(err, ErrMissingEnv) {
				t.Fatalf("Load missing %s: err = %v, want %v", key, err, ErrMissingEnv)
			}
		})
	}
}

func TestLoadBadLogLevel(t *testing.T) {
	setValidEnv(t)
	t.Setenv("LOG_LEVEL", "verbose")

	_, err := Load()
	if !errors.Is(err, ErrInvalidLogLevel) {
		t.Fatalf("err = %v, want %v", err, ErrInvalidLogLevel)
	}
}

func TestLoadBadDuration(t *testing.T) {
	t.Run("start", func(t *testing.T) {
		setValidEnv(t)
		t.Setenv("FX_START_TIMEOUT", "not-a-duration")
		_, err := Load()
		if !errors.Is(err, ErrInvalidDuration) {
			t.Fatalf("err = %v, want %v", err, ErrInvalidDuration)
		}
	})
	t.Run("stop", func(t *testing.T) {
		setValidEnv(t)
		t.Setenv("FX_STOP_TIMEOUT", "15")
		_, err := Load()
		if !errors.Is(err, ErrInvalidDuration) {
			t.Fatalf("err = %v, want %v", err, ErrInvalidDuration)
		}
	})
	t.Run("non-positive", func(t *testing.T) {
		setValidEnv(t)
		t.Setenv("FX_START_TIMEOUT", "0s")
		_, err := Load()
		if !errors.Is(err, ErrInvalidDuration) {
			t.Fatalf("err = %v, want %v", err, ErrInvalidDuration)
		}
	})
}

func TestSlogLevel(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"debug", "DEBUG"},
		{"INFO", "INFO"},
		{"warn", "WARN"},
		{"error", "ERROR"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			cfg := Config{LogLevel: tc.in}
			if got := cfg.SlogLevel().String(); got != tc.want {
				t.Errorf("SlogLevel() = %s, want %s", got, tc.want)
			}
		})
	}
}
