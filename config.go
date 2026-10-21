package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

type config struct {
	Context   string    `json:"context" yaml:"context"`
	Namespace string    `json:"namespace" yaml:"namespace"`
	Address   string    `json:"address" yaml:"address"`
	EnvFile   string    `json:"env_file" yaml:"env_file"`
	Forwards  []forward `json:"forwards" yaml:"forwards"`
}

type forward struct {
	Service   string   `json:"service" yaml:"service"`
	Namespace string   `json:"namespace" yaml:"namespace"`
	Address   string   `json:"address" yaml:"address"`
	Ports     []string `json:"ports" yaml:"ports"`
}

type forwardPlan struct {
	context string
	targets []target
}

var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func prepare(opts options) (forwardPlan, error) {
	path := opts.configPath
	if path == "" {
		path = discoverConfig()
	}
	cfg, err := readConfig(path)
	if err != nil {
		return forwardPlan{}, err
	}
	vars, err := loadVariables(opts.envPath, cfg.EnvFile, path)
	if err != nil {
		return forwardPlan{}, err
	}
	if value := vars["KUBEPIN_CONTEXT"]; value != "" {
		cfg.Context = value
	}
	if value := vars["KUBEPIN_NAMESPACE"]; value != "" {
		cfg.Namespace = value
	}
	if value := vars["KUBEPIN_ADDRESS"]; value != "" {
		cfg.Address = value
	}
	if opts.context != "" {
		cfg.Context = opts.context
	}
	if opts.namespace != "" {
		cfg.Namespace = opts.namespace
	}
	if opts.address != "" {
		cfg.Address = opts.address
	}
	if cfg.Context, err = expand(cfg.Context, vars); err != nil {
		return forwardPlan{}, fmt.Errorf("context: %w", err)
	}
	if cfg.Namespace, err = expand(cfg.Namespace, vars); err != nil {
		return forwardPlan{}, fmt.Errorf("namespace: %w", err)
	}
	if cfg.Namespace == "" {
		cfg.Namespace = "default"
	}
	if cfg.Context == "" {
		return forwardPlan{}, errors.New("Kubernetes context is required in config, KUBEPIN_CONTEXT, or -context")
	}
	if cfg.Address == "" {
		cfg.Address = "127.0.0.1"
	}
	if cfg.Address, err = expand(cfg.Address, vars); err != nil {
		return forwardPlan{}, fmt.Errorf("address: %w", err)
	}
	if cfg.Address, err = validateAddress(cfg.Address); err != nil {
		return forwardPlan{}, fmt.Errorf("address: %w", err)
	}
	targets, err := resolve(cfg.Forwards, vars, cfg.Namespace, cfg.Address)
	if err != nil {
		return forwardPlan{}, err
	}
	if len(targets) == 0 {
		return forwardPlan{}, errors.New("no forwards defined in config or KUBEPIN_FORWARD_* environment variables")
	}
	return forwardPlan{context: cfg.Context, targets: targets}, nil
}

func discoverConfig() string {
	for _, candidate := range []string{"kubepin.yaml", "kubepin.yml", "kubepin.json"} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

func loadVariables(envPath, configEnvPath, configPath string) (map[string]string, error) {
	file := envPath
	if file == "" {
		file = configEnvPath
		if file != "" && !filepath.IsAbs(file) {
			file = filepath.Join(filepath.Dir(configPath), file)
		}
	} else if !filepath.IsAbs(file) {
		// An explicit -env path is relative to the current directory.
		file = filepath.Clean(file)
	}
	if file == "" {
		if _, err := os.Stat(".env"); err == nil {
			file = ".env"
		}
	}
	vars := map[string]string{}
	if file != "" {
		var err error
		vars, err = readEnv(file)
		if err != nil {
			return nil, err
		}
	}
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			vars[key] = value // Process environment wins over dotenv.
		}
	}
	return vars, nil
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
		if !ok || !envName.MatchString(key) {
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
