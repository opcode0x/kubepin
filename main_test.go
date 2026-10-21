package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadConfigFormats(t *testing.T) {
	for _, tc := range []struct{ name, data string }{
		{"kubepin.json", `{"context":"dev","forwards":[{"service":"db","ports":["5432:5432"]}]}`},
		{"kubepin.yaml", "context: dev\nforwards:\n  - service: db\n    ports:\n      - '5432:5432'\n"},
		{"kubepin.yml", "context: dev\nforwards:\n  - service: db\n    ports:\n      - '5432:5432'\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), tc.name)
			if err := os.WriteFile(path, []byte(tc.data), 0600); err != nil {
				t.Fatal(err)
			}
			cfg, err := readConfig(path)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Context != "dev" || len(cfg.Forwards) != 1 || cfg.Forwards[0].Ports[0] != "5432:5432" {
				t.Fatalf("unexpected config: %+v", cfg)
			}
		})
	}
}

func TestResolveEnvAndDuplicatePort(t *testing.T) {
	vars := map[string]string{"DB_PORT": "15432", "KUBEPIN_FORWARD_CACHE": "16379:6379"}
	forwards := []forward{{Service: "db", Ports: []string{"${DB_PORT}:5432"}}}
	targets, err := resolve(forwards, vars)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 2 || targets[0].Port != "16379:6379" || targets[1].Port != "15432:5432" {
		t.Fatalf("unexpected targets: %+v", targets)
	}
	_, err = resolve(append(forwards, forward{Service: "other", Ports: []string{"15432:5432"}}), vars)
	if err == nil || !strings.Contains(err.Error(), "more than once") {
		t.Fatalf("expected duplicate port error, got %v", err)
	}
}

func TestRunDryRunEnvOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ports.env")
	if err := os.WriteFile(path, []byte("KUBEPIN_CONTEXT=dev\nKUBEPIN_FORWARD_POSTGRES=15432:5432\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := run([]string{"-env", path, "-dry-run"}, &out, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "--context dev --namespace default port-forward service/postgres 15432:5432") {
		t.Fatalf("unexpected output: %s", out.String())
	}
}

func TestMissingVariable(t *testing.T) {
	_, err := resolve([]forward{{Service: "db", Ports: []string{"${MISSING}:5432"}}}, map[string]string{})
	if err == nil || !strings.Contains(err.Error(), "MISSING") {
		t.Fatalf("expected missing variable error, got %v", err)
	}
}
