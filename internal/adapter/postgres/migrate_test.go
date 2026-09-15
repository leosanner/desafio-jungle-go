package postgres

import (
	"strings"
	"testing"
)

func TestPinMigrationsTable(t *testing.T) {
	t.Parallel()
	got, err := pinMigrationsTable("pgx5://wagering:wagering@localhost:5432/wagering?sslmode=disable")
	if err != nil {
		t.Fatalf("pinMigrationsTable: %v", err)
	}
	if !strings.Contains(got, "x-migrations-table") {
		t.Fatalf("expected pinned migrations table in %q", got)
	}
	already := `pgx5://localhost/db?x-migrations-table=custom`
	got, err = pinMigrationsTable(already)
	if err != nil {
		t.Fatalf("pinMigrationsTable: %v", err)
	}
	if strings.Contains(got, "schema_migrations") {
		t.Fatalf("should not override an explicit table: %q", got)
	}
}

func TestPgx5DatabaseURL(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		dsn     string
		want    string
		wantErr string
	}{
		{
			name: "postgres scheme",
			dsn:  "postgres://wagering:wagering@localhost:5432/wagering?sslmode=disable",
			want: "pgx5://wagering:wagering@localhost:5432/wagering?sslmode=disable",
		},
		{
			name: "postgresql scheme",
			dsn:  "postgresql://user:pass@db:5432/app",
			want: "pgx5://user:pass@db:5432/app",
		},
		{
			name:    "unsupported scheme",
			dsn:     "mysql://localhost/db",
			wantErr: "unsupported dsn scheme",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := pgx5DatabaseURL(tc.dsn)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want substring %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("pgx5DatabaseURL: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
