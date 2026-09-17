package domain

import (
	"os/exec"
	"strings"
	"testing"
)

var forbiddenDeps = []string{
	"github.com/prometheus/",
	"go.uber.org/fx",
	"net/http",
	"github.com/coreos/go-oidc",
	"github.com/aws/",
	"github.com/jackc/pgx",
	"database/sql",
}

func TestNoInfrastructureDependencies(t *testing.T) {
	t.Parallel()
	cmd := exec.Command("go", "list", "-f", "{{join .Deps \"\\n\"}}", ".")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, out)
	}
	deps := strings.TrimSpace(string(out))
	if deps == "" {
		return
	}
	for _, line := range strings.Split(deps, "\n") {
		for _, forbidden := range forbiddenDeps {
			if strings.Contains(line, forbidden) {
				t.Errorf("forbidden dependency %q (matched %q)", line, forbidden)
			}
		}
	}
}
