package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/chenhg5/cc-qq-gateway/internal/app"
	"github.com/chenhg5/cc-qq-gateway/internal/config"
	"github.com/chenhg5/cc-qq-gateway/internal/gateway"
)

type cliOptions struct {
	configPath  string
	showVersion bool
}

func parseOptions(args []string) (cliOptions, error) {
	var opts cliOptions
	fs := flag.NewFlagSet("cc-qq-gateway", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&opts.configPath, "config", "config.toml", "path to TOML configuration")
	fs.BoolVar(&opts.showVersion, "version", false, "print version and exit")
	err := fs.Parse(args)
	return opts, err
}

func run(args []string, stdout, stderr io.Writer) int {
	opts, err := parseOptions(args)
	if err != nil {
		fmt.Fprintf(stderr, "usage: cc-qq-gateway [-config config.toml] [-version]\n")
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}
	if opts.showVersion {
		fmt.Fprintf(stdout, "cc-qq-gateway v%s\n", gateway.Version)
		return 0
	}

	cfg, err := config.Load(opts.configPath)
	if err != nil {
		fmt.Fprintf(stderr, "config error: %v\n", err)
		return 1
	}

	logger := log.New(stdout, "", log.LstdFlags)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := app.New(cfg, logger).Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintf(stderr, "gateway stopped: %v\n", err)
		return 1
	}
	return 0
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
