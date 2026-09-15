package postgres

import (
	"strings"
	"testing"
)

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
