package main

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type target struct {
	Service   string
	Namespace string
	Port      string
}

var variable = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)
var serviceName = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

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

func resolve(forwards []forward, vars map[string]string, defaultNamespace string) ([]target, error) {
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
		namespace := f.Namespace
		if value := vars["KUBEPIN_NAMESPACE_"+strings.ToUpper(strings.ReplaceAll(service, "-", "_"))]; value != "" {
			namespace = value
		}
		if namespace == "" {
			namespace = defaultNamespace
		}
		if namespace, err = expand(namespace, vars); err != nil {
			return nil, fmt.Errorf("service %s namespace: %w", service, err)
		}
		if !serviceName.MatchString(namespace) || len(namespace) > 63 {
			return nil, fmt.Errorf("service %s: invalid namespace %q", service, namespace)
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
			targets = append(targets, target{Service: service, Namespace: namespace, Port: fmt.Sprintf("%d:%d", l, r)})
		}
	}
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].Service == targets[j].Service && targets[i].Namespace == targets[j].Namespace {
			return targets[i].Port < targets[j].Port
		}
		if targets[i].Service == targets[j].Service {
			return targets[i].Namespace < targets[j].Namespace
		}
		return targets[i].Service < targets[j].Service
	})
	return targets, nil
}
