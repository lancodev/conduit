package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	args := os.Args[1:]

	// Handle auth subcommand.
	if len(args) > 0 && args[0] == "auth" {
		if err := RunAuthSetup(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Handle --help / -h.
	for _, a := range args {
		if a == "--help" || a == "-h" {
			printUsage()
			return
		}
	}

	// Parse flags and positional args.
	cfg := LoadConfig()
	buildNumber := 0
	repoFromFlag := false

	i := 0
	for i < len(args) {
		switch args[i] {
		case "-r":
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "Error: -r requires workspace/repo argument\n")
				os.Exit(1)
			}
			parts := strings.SplitN(args[i+1], "/", 2)
			if len(parts) != 2 {
				fmt.Fprintf(os.Stderr, "Error: -r format must be workspace/repo\n")
				os.Exit(1)
			}
			cfg.DefaultWorkspace = parts[0]
			cfg.DefaultRepository = parts[1]
			repoFromFlag = true
			i += 2
		case "-i":
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "Error: -i requires interval in seconds\n")
				os.Exit(1)
			}
			interval, err := strconv.Atoi(args[i+1])
			if err != nil || interval < 1 {
				fmt.Fprintf(os.Stderr, "Error: -i must be a positive integer\n")
				os.Exit(1)
			}
			cfg.RefreshInterval = interval
			i += 2
		default:
			// Positional: build number.
			n, err := strconv.Atoi(args[i])
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: unknown argument %q\n", args[i])
				os.Exit(1)
			}
			buildNumber = n
			i++
		}
	}

	directDetail := buildNumber > 0

	m := initialModel(cfg, buildNumber, directDetail, repoFromFlag)
	p := tea.NewProgram(m, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Print(`conduit — Bitbucket Cloud pipeline monitor

Usage:
  conduit                       Dashboard for default repo
  conduit <build_number>        Detail view for a specific build
  conduit -r workspace/repo     Dashboard for a specific repo
  conduit -i <seconds>          Custom refresh interval
  conduit auth                  Set up credentials

Options:
  -r workspace/repo    Override workspace and repository
  -i seconds           Refresh interval (default: 10)
  -h, --help           Show this help

Environment variables (override config file):
  CONDUIT_CLIENT_ID       Bitbucket OAuth Client ID
  CONDUIT_CLIENT_SECRET   Bitbucket OAuth Client Secret
  CONDUIT_WORKSPACE       Default workspace
  CONDUIT_REPOSITORY      Default repository

Config file: ~/.config/conduit/config.yml
`)
}
