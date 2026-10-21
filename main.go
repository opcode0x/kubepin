package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"gopkg.in/yaml.v3"
)

type config struct {
	Context   string    `json:"context" yaml:"context"`
	Namespace string    `json:"namespace" yaml:"namespace"`
	EnvFile   string    `json:"env_file" yaml:"env_file"`
	Forwards  []forward `json:"forwards" yaml:"forwards"`
}

type forward struct {
	Service string   `json:"service" yaml:"service"`
	Ports   []string `json:"ports" yaml:"ports"`
}

type target struct {
	Service string
	Port    string
}

var variable = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)
var serviceName = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "kubepin:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("kubepin", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", "", "JSON or YAML config file (default: discover kubepin.yaml, kubepin.yml, or kubepin.json)")
	envPath := flags.String("env", "", "dotenv file (overrides env_file in config)")
	contextName := flags.String("context", "", "Kubernetes context override")
	namespace := flags.String("namespace", "", "Kubernetes namespace override")
	dryRun := flags.Bool("dry-run", false, "print kubectl commands without running them")
	if err := flags.Parse(args); err == flag.ErrHelp {
		return nil
	} else if err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}

	path := *configPath
	if path == "" {
		for _, candidate := range []string{"kubepin.yaml", "kubepin.yml", "kubepin.json"} {
			if _, err := os.Stat(candidate); err == nil {
				path = candidate
				break
			}
		}
	}
	cfg, err := readConfig(path)
	if err != nil {
		return err
	}
	file := *envPath
	if file == "" {
		file = cfg.EnvFile
		if file != "" && !filepath.IsAbs(file) {
			file = filepath.Join(filepath.Dir(path), file)
		}
	} else if !filepath.IsAbs(file) {
		// An explicit -env path is relative to the current directory.
		file = filepath.Clean(file)
	}
	vars := map[string]string{}
	if file != "" {
		vars, err = readEnv(file)
		if err != nil {
			return err
		}
	} else if _, err := os.Stat(".env"); err == nil {
		vars, err = readEnv(".env")
		if err != nil {
			return err
		}
	}
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			vars[key] = value // Process environment wins over dotenv.
		}
	}
	if value := vars["KUBEPIN_CONTEXT"]; value != "" {
		cfg.Context = value
	}
	if value := vars["KUBEPIN_NAMESPACE"]; value != "" {
		cfg.Namespace = value
	}
	if *contextName != "" {
		cfg.Context = *contextName
	}
	if *namespace != "" {
		cfg.Namespace = *namespace
	}
	if cfg.Context, err = expand(cfg.Context, vars); err != nil {
		return fmt.Errorf("context: %w", err)
	}
	if cfg.Namespace, err = expand(cfg.Namespace, vars); err != nil {
		return fmt.Errorf("namespace: %w", err)
	}
	if cfg.Namespace == "" {
		cfg.Namespace = "default"
	}
	if cfg.Context == "" {
		return errors.New("Kubernetes context is required in config, KUBEPIN_CONTEXT, or -context")
	}
	targets, err := resolve(cfg.Forwards, vars)
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		return errors.New("no forwards defined in config or KUBEPIN_FORWARD_* environment variables")
	}
	if !*dryRun {
		if _, err := exec.LookPath("kubectl"); err != nil {
			return fmt.Errorf("kubectl is required: %w", err)
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	type result struct {
		target target
		err    error
	}
	results := make(chan result, len(targets))
	started := 0
	for _, t := range targets {
		commandArgs := []string{"--context", cfg.Context, "--namespace", cfg.Namespace, "port-forward", "service/" + t.Service, t.Port, "--address", "127.0.0.1"}
		fmt.Fprintln(stdout, "kubectl", strings.Join(commandArgs, " "))
		if *dryRun {
			continue
		}
		cmd := exec.CommandContext(ctx, "kubectl", commandArgs...)
		cmd.Stdout, cmd.Stderr = stdout, stderr
		if err := cmd.Start(); err != nil {
			stop()
			for i := 0; i < started; i++ {
				<-results
			}
			return fmt.Errorf("start %s %s: %w", t.Service, t.Port, err)
		}
		started++
		go func(t target) { results <- result{t, cmd.Wait()} }(t)
	}
	if *dryRun {
		return nil
	}
	var firstErr error
	for i := 0; i < started; i++ {
		r := <-results
		if ctx.Err() == nil {
			if r.err != nil {
				firstErr = fmt.Errorf("forward %s %s: %w", r.target.Service, r.target.Port, r.err)
			} else {
				firstErr = fmt.Errorf("forward %s %s exited unexpectedly", r.target.Service, r.target.Port)
			}
			stop()
		}
	}
	return firstErr
}

func readConfig(path string) (config, error) {
	var cfg config
	if path == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("read config %q: %w", path, err)
	}
	if strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml") {
		decoder := yaml.NewDecoder(strings.NewReader(string(data)))
		decoder.KnownFields(true)
		if err := decoder.Decode(&cfg); err != nil {
			return cfg, fmt.Errorf("parse config %q: %w", path, err)
		}
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			return cfg, fmt.Errorf("config %q must contain one YAML document", path)
		}
		return cfg, nil
	}
	if !strings.HasSuffix(path, ".json") {
		return cfg, fmt.Errorf("config %q must have .json, .yaml, or .yml extension", path)
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return cfg, fmt.Errorf("parse config %q: %w", path, err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return cfg, fmt.Errorf("config %q must contain one JSON object", path)
	}
	return cfg, nil
}

func readEnv(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read env file %q: %w", path, err)
	}
	defer file.Close()
	vars := map[string]string{}
	scanner := bufio.NewScanner(file)
	for line := 1; scanner.Scan(); line++ {
		value := strings.TrimSpace(scanner.Text())
		if value == "" || strings.HasPrefix(value, "#") {
			continue
		}
		value = strings.TrimPrefix(value, "export ")
		key, raw, ok := strings.Cut(value, "=")
		key = strings.TrimSpace(key)
		if !ok || !regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`).MatchString(key) {
			return nil, fmt.Errorf("%s:%d: expected NAME=value", path, line)
		}
		raw = strings.TrimSpace(raw)
		if len(raw) >= 2 && ((raw[0] == '"' && raw[len(raw)-1] == '"') || (raw[0] == '\'' && raw[len(raw)-1] == '\'')) {
			raw = raw[1 : len(raw)-1]
		}
		vars[key] = raw
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read env file %q: %w", path, err)
	}
	return vars, nil
}

func expand(value string, vars map[string]string) (string, error) {
	var missing string
	out := variable.ReplaceAllStringFunc(value, func(match string) string {
		key := match[2 : len(match)-1]
		v, ok := vars[key]
		if !ok {
			missing = key
			return match
		}
		return v
	})
	if missing != "" {
		return "", fmt.Errorf("environment variable %s is unset", missing)
	}
	return out, nil
}

func resolve(forwards []forward, vars map[string]string) ([]target, error) {
	for key, value := range vars {
		if !strings.HasPrefix(key, "KUBEPIN_FORWARD_") {
			continue
		}
		service := strings.ToLower(strings.ReplaceAll(strings.TrimPrefix(key, "KUBEPIN_FORWARD_"), "_", "-"))
		forwards = append(forwards, forward{Service: service, Ports: strings.Split(value, ",")})
	}
	var targets []target
	used := map[int]bool{}
	for _, f := range forwards {
		service, err := expand(f.Service, vars)
		if err != nil {
			return nil, fmt.Errorf("service: %w", err)
		}
		if !serviceName.MatchString(service) || len(service) > 63 {
			return nil, fmt.Errorf("invalid service name %q", service)
		}
		if len(f.Ports) == 0 {
			return nil, fmt.Errorf("service %s has no ports", service)
		}
		for _, p := range f.Ports {
			p, err = expand(strings.TrimSpace(p), vars)
			if err != nil {
				return nil, fmt.Errorf("service %s: %w", service, err)
			}
			local, remote, ok := strings.Cut(p, ":")
			if !ok {
				return nil, fmt.Errorf("service %s: port %q must be LOCAL:REMOTE", service, p)
			}
			l, lerr := strconv.Atoi(local)
			r, rerr := strconv.Atoi(remote)
			if lerr != nil || rerr != nil || l < 1 || l > 65535 || r < 1 || r > 65535 {
				return nil, fmt.Errorf("service %s: invalid port mapping %q", service, p)
			}
			if used[l] {
				return nil, fmt.Errorf("local port %d is defined more than once", l)
			}
			used[l] = true
			targets = append(targets, target{service, fmt.Sprintf("%d:%d", l, r)})
		}
	}
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].Service == targets[j].Service {
			return targets[i].Port < targets[j].Port
		}
		return targets[i].Service < targets[j].Service
	})
	return targets, nil
}
