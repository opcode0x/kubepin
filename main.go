package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

type options struct {
	configPath string
	envPath    string
	context    string
	namespace  string
	address    string
	dryRun     bool
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "kubepin:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	opts, help, err := parseOptions(args, stderr)
	if err != nil {
		return err
	}
	if help {
		return nil
	}
	plan, err := prepare(opts)
	if err != nil {
		return err
	}
	return runForwards(plan.context, plan.targets, opts.dryRun, stdout, stderr)
}

func parseOptions(args []string, stderr io.Writer) (options, bool, error) {
	flags := flag.NewFlagSet("kubepin", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var opts options
	flags.StringVar(&opts.configPath, "config", "", "JSON or YAML config file (default: discover kubepin.yaml, kubepin.yml, or kubepin.json)")
	flags.StringVar(&opts.envPath, "env", "", "dotenv file (overrides env_file in config)")
	flags.StringVar(&opts.context, "context", "", "Kubernetes context override")
	flags.StringVar(&opts.namespace, "namespace", "", "default namespace override (individual service namespaces take precedence)")
	flags.StringVar(&opts.address, "address", "", "default bind address override (individual service addresses take precedence)")
	flags.BoolVar(&opts.dryRun, "dry-run", false, "print kubectl commands without running them")
	if err := flags.Parse(args); err == flag.ErrHelp {
		return opts, true, nil
	} else if err != nil {
		return opts, false, err
	}
	if flags.NArg() != 0 {
		return opts, false, fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}
	return opts, false, nil
}
