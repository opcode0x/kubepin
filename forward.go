package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

func runForwards(contextName string, targets []target, dryRun bool, stdout, stderr io.Writer) error {
	kubectlPath := ""
	if !dryRun {
		var err error
		if kubectlPath, err = exec.LookPath("kubectl"); err != nil {
			return fmt.Errorf("kubectl is required: %w", err)
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var workers sync.WaitGroup
	for _, t := range targets {
		commandArgs := []string{"--context", contextName, "--namespace", t.Namespace, "port-forward", "service/" + t.Service, t.Port, "--address", "127.0.0.1"}
		fmt.Fprintln(stdout, "kubectl", strings.Join(commandArgs, " "))
		if dryRun {
			continue
		}
		workers.Add(1)
		go func(t target, args []string) {
			defer workers.Done()
			supervise(ctx, kubectlPath, args, t, stdout, stderr)
		}(t, commandArgs)
	}
	if dryRun {
		return nil
	}
	<-ctx.Done()
	workers.Wait()
	return nil
}

func supervise(ctx context.Context, kubectlPath string, args []string, t target, stdout, stderr io.Writer) {
	delay := time.Second
	for ctx.Err() == nil {
		started := time.Now()
		cmd := exec.CommandContext(ctx, kubectlPath, args...)
		cmd.Stdout, cmd.Stderr = stdout, stderr
		err := cmd.Run()
		if ctx.Err() != nil {
			return
		}
		if time.Since(started) >= 30*time.Second {
			delay = time.Second
		}
		if err == nil {
			fmt.Fprintf(stderr, "kubepin: forward %s/%s %s ended; reconnecting in %s\n", t.Namespace, t.Service, t.Port, delay)
		} else {
			fmt.Fprintf(stderr, "kubepin: forward %s/%s %s failed: %v; reconnecting in %s\n", t.Namespace, t.Service, t.Port, err, delay)
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if delay < 30*time.Second {
			delay *= 2
			if delay > 30*time.Second {
				delay = 30 * time.Second
			}
		}
	}
}
