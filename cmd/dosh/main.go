package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"dosh/internal/bootstrap"
	"dosh/internal/modules/auth/app"
	"dosh/internal/platform/config"
	"dosh/internal/version"

	"golang.org/x/term"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version", "--version", "-v":
			fmt.Printf("dosh %s (%s, %s)\n", version.Version, version.Commit, version.Date)
			return
		case "hash-password":
			if err := hashPassword(); err != nil {
				fmt.Fprintln(os.Stderr, "error:", err)
				os.Exit(1)
			}
			return
		case "help", "--help", "-h":
			usage()
			return
		default:
			fmt.Fprintln(os.Stderr, "unknown command:", os.Args[1])
			usage()
			os.Exit(2)
		}
	}
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "configuration error:", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	application, err := bootstrap.New(ctx, cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "startup error:", err)
		os.Exit(1)
	}
	if err := application.Run(ctx); err != nil {
		application.Logger.Error("server_stopped", "error", err)
		os.Exit(1)
	}
}

func hashPassword() error {
	var password string
	if term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprint(os.Stderr, "Password: ")
		raw, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return err
		}
		password = string(raw)
	} else {
		raw, err := io.ReadAll(os.Stdin)
		if err != nil {
			return err
		}
		password = strings.TrimSuffix(string(raw), "\n")
		password = strings.TrimSuffix(password, "\r")
	}
	hash, err := app.HashPassword(password)
	if err != nil {
		return err
	}
	fmt.Println(hash)
	return nil
}

func usage() {
	fmt.Fprintln(os.Stderr, `Usage:
  dosh                      Start the dashboard
  dosh hash-password        Read a password from stdin and print an Argon2id hash
  dosh version              Print version information`)
}
