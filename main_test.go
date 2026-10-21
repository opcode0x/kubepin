package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReadConfigFormats(t *testing.T) {
	for _, tc := range []struct{ name, data string }{
		{"kubepin.json", `{"context":"dev","address":"localhost","forwards":[{"service":"db","namespace":"data","address":"::1","ports":["5432:5432"]}]}`},
		{"kubepin.yaml", "context: dev\naddress: localhost\nforwards:\n  - service: db\n    namespace: data\n    address: ::1\n    ports:\n      - '5432:5432'\n"},
		{"kubepin.yml", "context: dev\naddress: localhost\nforwards:\n  - service: db\n    namespace: data\n    address: ::1\n    ports:\n      - '5432:5432'\n"},
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
			if cfg.Context != "dev" || cfg.Address != "localhost" || len(cfg.Forwards) != 1 || cfg.Forwards[0].Namespace != "data" || cfg.Forwards[0].Address != "::1" || cfg.Forwards[0].Ports[0] != "5432:5432" {
				t.Fatalf("unexpected config: %+v", cfg)
			}
		})
	}
}

func TestResolveEnvAndDuplicatePort(t *testing.T) {
	vars := map[string]string{"DB_PORT": "15432", "KUBEPIN_FORWARD_CACHE": "16379:6379", "KUBEPIN_NAMESPACE_CACHE": "cache-system"}
	forwards := []forward{{Service: "db", Namespace: "data", Ports: []string{"${DB_PORT}:5432"}}}
	targets, err := resolve(forwards, vars, "default", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 2 || targets[0].Port != "16379:6379" || targets[0].Namespace != "cache-system" || targets[0].Address != "127.0.0.1" || targets[1].Port != "15432:5432" || targets[1].Namespace != "data" {
		t.Fatalf("unexpected targets: %+v", targets)
	}
	_, err = resolve(append(forwards, forward{Service: "other", Ports: []string{"15432:5432"}}), vars, "default", "127.0.0.1")
	if err == nil || !strings.Contains(err.Error(), "more than once") {
		t.Fatalf("expected duplicate port error, got %v", err)
	}
}

func TestRunDryRunEnvOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ports.env")
	if err := os.WriteFile(path, []byte("KUBEPIN_CONTEXT=dev\nKUBEPIN_FORWARD_POSTGRES=15432:5432\nKUBEPIN_NAMESPACE_POSTGRES=data\nKUBEPIN_ADDRESS_POSTGRES=localhost\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := run([]string{"-env", path, "-dry-run"}, &out, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "--context dev --namespace data port-forward service/postgres 15432:5432 --address localhost") {
		t.Fatalf("unexpected output: %s", out.String())
	}
}

func TestPreparePrecedenceAndRelativeEnvFile(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "kubepin.yaml")
	if err := os.WriteFile(configPath, []byte("context: config-context\nnamespace: config-ns\nenv_file: vars.env\nforwards:\n  - service: db\n    ports: ['${LOCAL_PORT}:5432']\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "vars.env"), []byte("KUBEPIN_CONTEXT=file-context\nKUBEPIN_NAMESPACE=file-ns\nLOCAL_PORT=15432\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KUBEPIN_CONTEXT", "process-context")
	t.Setenv("KUBEPIN_NAMESPACE", "process-ns")
	plan, err := prepare(options{configPath: configPath, context: "flag-context", namespace: "flag-ns"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.context != "flag-context" || len(plan.targets) != 1 || plan.targets[0].Namespace != "flag-ns" || plan.targets[0].Port != "15432:5432" {
		t.Fatalf("unexpected plan: %+v", plan)
	}
}

func TestAddressPrecedenceAndDryRun(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "kubepin.yaml")
	data := "context: dev\naddress: 127.0.0.1\nenv_file: vars.env\nforwards:\n  - service: api\n    address: ::1\n    ports: ['18080:8080']\n  - service: db\n    ports: ['15432:5432']\n"
	if err := os.WriteFile(configPath, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "vars.env"), []byte("KUBEPIN_ADDRESS=localhost\nKUBEPIN_ADDRESS_API=0.0.0.0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	plan, err := prepare(options{configPath: configPath})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.targets) != 2 || plan.targets[0].Address != "0.0.0.0" || plan.targets[1].Address != "localhost" {
		t.Fatalf("unexpected address precedence: %+v", plan.targets)
	}
	var out strings.Builder
	if err := run([]string{"-config", configPath, "-address", "192.0.2.10", "-dry-run"}, &out, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "service/api 18080:8080 --address 0.0.0.0") || !strings.Contains(out.String(), "service/db 15432:5432 --address 192.0.2.10") {
		t.Fatalf("unexpected commands: %s", out.String())
	}
}

func TestAddressValidation(t *testing.T) {
	for _, address := range []string{"localhost", "0.0.0.0", "::1", "localhost,192.0.2.10"} {
		if _, err := validateAddress(address); err != nil {
			t.Errorf("valid address %q: %v", address, err)
		}
	}
	for _, address := range []string{"", "example.com", "localhost,", "127.0.0.1:8080"} {
		if _, err := validateAddress(address); err == nil {
			t.Errorf("expected invalid address %q to fail", address)
		}
	}
	_, err := resolve([]forward{{Service: "db", Address: "example.com", Ports: []string{"15432:5432"}}}, nil, "default", "127.0.0.1")
	if err == nil || !strings.Contains(err.Error(), "service db address") {
		t.Fatalf("expected service address error, got %v", err)
	}
	targets, err := resolve([]forward{{Service: "db", Address: "::1", Ports: []string{"15432:5432"}}}, nil, "default", "127.0.0.1")
	if err != nil || len(targets) != 1 || targets[0].Address != "::1" {
		t.Fatalf("expected service address override, got %+v, %v", targets, err)
	}
	configPath := filepath.Join(t.TempDir(), "kubepin.json")
	if err := os.WriteFile(configPath, []byte(`{"context":"dev","forwards":[{"service":"db","ports":["15432:5432"]}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := prepare(options{configPath: configPath, address: "example.com"}); err == nil || !strings.Contains(err.Error(), "address: invalid bind address") {
		t.Fatalf("expected global address error, got %v", err)
	}
}

func TestDefaultBindAddress(t *testing.T) {
	t.Setenv("KUBEPIN_ADDRESS", "")
	t.Setenv("KUBEPIN_ADDRESS_DB", "")
	configPath := filepath.Join(t.TempDir(), "kubepin.json")
	if err := os.WriteFile(configPath, []byte(`{"context":"dev","forwards":[{"service":"db","ports":["15432:5432"]}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	plan, err := prepare(options{configPath: configPath})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.targets) != 1 || plan.targets[0].Address != "127.0.0.1" {
		t.Fatalf("unexpected default bind address: %+v", plan.targets)
	}
}

func TestRunHelpDoesNotRequireConfig(t *testing.T) {
	var out strings.Builder
	if err := run([]string{"-help"}, &out, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "-dry-run") {
		t.Fatalf("expected flag help, got %q", out.String())
	}
}

func TestMissingVariable(t *testing.T) {
	_, err := resolve([]forward{{Service: "db", Ports: []string{"${MISSING}:5432"}}}, map[string]string{}, "default", "127.0.0.1")
	if err == nil || !strings.Contains(err.Error(), "MISSING") {
		t.Fatalf("expected missing variable error, got %v", err)
	}
}

func TestSuperviseReconnects(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "kubectl")
	counter := filepath.Join(dir, "attempts")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf x >> \"$1\"\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var out strings.Builder
	done := make(chan struct{})
	go func() {
		supervise(ctx, script, []string{counter}, target{Service: "db", Namespace: "data", Port: "15432:5432"}, &out, &out)
		close(done)
	}()
	deadline := time.After(4 * time.Second)
	for {
		data, _ := os.ReadFile(counter)
		if len(data) >= 2 {
			cancel()
			<-done
			if !strings.Contains(out.String(), "reconnecting") {
				t.Fatalf("expected reconnect notice, got %q", out.String())
			}
			return
		}
		select {
		case <-deadline:
			cancel()
			<-done
			t.Fatalf("expected two starts, got %d", len(data))
		case <-time.After(20 * time.Millisecond):
		}
	}
}
