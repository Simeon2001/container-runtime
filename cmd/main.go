package main

import (
	"context"
	"embed"
	"fmt"
	runConfig "github.com/Simeon2001/AlpineCell/config"
	"github.com/Simeon2001/AlpineCell/isolator"
	"github.com/fatih/color"
	"github.com/urfave/cli/v3"
	"log"
	"os"
	"path/filepath"
	"strings"
)

//go:embed config.json
var configJSONFile embed.FS

//go:embed alpine-minirootfs.tar.gz
var alpineFS embed.FS

// main is the entry point of the application where the program execution begins.
func main() {

	if len(os.Args) > 1 && os.Args[1] == "child" {
		isolator.SpawnContainer()
		return
	}

	cmd := &cli.Command{
		Name:  "otala-box",
		Usage: "Container runtime guided by Obatala's principles of purity and wise isolation 🏺",
		Description: "Otala-box - Where Ancient Wisdom Meets Modern Containers ✨\n" +
			"Inspired by Obatala, the Yoruba orisha of purity and creation:\n\n" +
			"🤍 PURE ISOLATION: Clean separation, untainted systems\n" +
			"👑 WISE GOVERNANCE: Preserves only what serves the greater good\n\n" +
			"⚖️  JUST EXECUTION - fair resource allocation\n\n" +
			"🕊️  PEACEFUL OPERATION: Calm, stable, reliable execution\n" +
			"🏔️  UNSHAKEABLE: Mountain-solid security and performance\n\n" +
			"Blessed with the patience of the Great White Orisha",
		Version: "1.0.0",
		Commands: []*cli.Command{
			{
				Name:  "run",
				Usage: "Run a container with specified configuration",
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:    "net",
						Aliases: []string{"n"},
						Usage:   "Enable pasta networking (true) or disable networking (false)",
						Value:   true,
					},
					&cli.IntFlag{
						Name:    "memory-limit",
						Aliases: []string{"ml"},
						Usage:   "Memory limit in MB (e.g., 100)",
						Value:   100,
					},
					&cli.StringFlag{
						Name:     "config",
						Aliases:  []string{"cf"},
						Usage:    "Path to container configuration JSON file",
						Required: false,
					},
					&cli.StringFlag{
						Name:    "copy",
						Aliases: []string{"cp"},
						Usage:   "Copy directories into container (host paths only)",
					},
					&cli.StringFlag{
						Name:    "mount",
						Aliases: []string{"m"},
						Usage:   "Mount directories into container (host paths only)",
					},
					&cli.StringFlag{
						Name:     "language",
						Aliases:  []string{"l"},
						Usage:    "Runtime language (javascript, python, go, etc.)",
						Required: false,
					},
					&cli.StringFlag{
						Name:    "script",
						Aliases: []string{"s"},
						Usage:   "Path to the script file to execute",
					},
					&cli.StringFlag{
						Name:    "command",
						Aliases: []string{"cmd"},
						Usage:   "Direct command to execute (e.g., 'ping google.com')",
					},
					&cli.StringSliceFlag{
						Name:    "args",
						Aliases: []string{"a"},
						Usage:   "Arguments to pass to the script (e.g., --args 15 --args 8)",
					},
					&cli.BoolFlag{
						Name:    "delete",
						Aliases: []string{"d"},
						Usage:   "Delete container when execution is complete",
						Value:   false,
					},
				},
				Action: runContainer,
			},
			{
				Name:  "version",
				Usage: "Show version information",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					fmt.Printf("otala-box version %s\n", cmd.Root().Version)
					fmt.Println("Built with the blessing of Obatala")
					return nil
				},
			},
		},
	}

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		log.Fatal(err)
	}
}

// runContainer initializes and runs a container with the provided configuration and command-line context.
// It validates input parameters, displays configuration details, and executes the container runtime.
func runContainer(ctx context.Context, cmd *cli.Command) error {
	_ = ctx // Context not currently used but required by CLI framework
	cwd, err := os.Getwd()
	must("getting current dir", err)

	config := runConfig.RunConfig{
		Network:        cmd.Bool("net"),
		MemoryLimit:    cmd.Int("memory-limit"),
		Language:       cmd.String("language"),
		Script:         cmd.String("script"),
		Command:        cmd.String("command"),
		ConfigPath:     cmd.String("config"),
		CopyMounts:     cmd.String("copy"),
		Mounts:         cmd.String("mount"),
		Args:           cmd.StringSlice("args"),
		DeleteWhenDone: cmd.Bool("delete"),
	}

	// If neither copy nor mount is specified, default to copy current directory
	if len(config.CopyMounts) == 0 && len(config.Mounts) == 0 {
		config.CopyMounts = cwd
		config.MountBool = false
	}

	// Validate inputs
	if err = validateConfig(&config); err != nil {
		return fmt.Errorf("configuration validation failed: %w", err)
	}

	// Display configuration
	color.New(color.FgYellow, color.Bold).Println("🏺 Otala-Box: Starting container with Obatala's blessing")
	if config.Network {
		color.New(color.FgGreen).Printf("    Network: pasta networking enabled\n")
	} else {
		color.New(color.FgYellow).Printf("    Network: networking disabled\n")
	}
	color.New(color.FgCyan).Printf("    Memory Limit: %dMB\n", config.MemoryLimit)

	if config.Language != "" {
		color.New(color.FgCyan).Printf("    Language: %s\n", config.Language)
	}

	if config.Script != "" {
		color.New(color.FgCyan).Printf("    Script: %s\n", config.Script)
	}

	if config.Command != "" {
		color.New(color.FgCyan).Printf("    Command: %s\n", config.Command)
	}

	color.New(color.FgCyan).Printf("    Config: %s\n", config.ConfigPath)

	if len(config.Args) > 0 {
		color.New(color.FgCyan).Printf("    Args: %s\n", strings.Join(config.Args, " "))
	}

	if config.CopyMounts != "" {
		color.New(color.FgCyan).Printf("    Copy Mounts: %s\n", config.CopyMounts)
	}

	if config.Mounts != "" {
		color.New(color.FgCyan).Printf("    Mounts: %s\n", config.Mounts)
	}

	if config.DeleteWhenDone {
		color.New(color.FgRed).Printf("    Delete when done: enabled\n")
	} else {
		color.New(color.FgGreen).Printf("    Delete when done: disabled\n")
	}

	// Execute container with the validated config struct
	return executeContainer(&config)
}

// validateConfig validate all input pass to the CLI
func validateConfig(config *runConfig.RunConfig) error {

	var copyOrMountPath string
	// Validate config file exists (only if ConfigPath is provided)
	if config.ConfigPath != "" {
		if _, err := os.Stat(config.ConfigPath); os.IsNotExist(err) {
			return fmt.Errorf("config file does not exist: %s", config.ConfigPath)
		}
	}

	// Must have either script or command, but not both
	if config.Script != "" && config.Command != "" {
		return fmt.Errorf("cannot specify both --script and --command, choose one")
	}

	if config.Script == "" && config.Command == "" {
		return fmt.Errorf("must specify either --script or --command")
	}

	// Must specify either copy or mount, but not both
	hasCopy := config.CopyMounts != ""
	hasMount := config.Mounts != ""

	if !hasCopy && !hasMount {
		return fmt.Errorf("must specify either --copy or --mount")
	}

	if hasCopy && hasMount {
		return fmt.Errorf("cannot use both --copy and --mount flags together, choose one")
	}

	// Validate copy mounts (host paths only)
	if config.CopyMounts != "" {
		if !filepath.IsAbs(config.CopyMounts) {
			return fmt.Errorf("copy path must be absolute: %s", config.CopyMounts)
		}
		if _, err := os.Stat(config.CopyMounts); os.IsNotExist(err) {
			return fmt.Errorf("copy path does not exist: %s", config.CopyMounts)
		}
		config.MountBool = false
		copyOrMountPath = config.CopyMounts
	}

	// Validate mount paths (host paths only)
	if config.Mounts != "" {
		if !filepath.IsAbs(config.Mounts) {
			return fmt.Errorf("mount path must be absolute: %s", config.Mounts)
		}
		if _, err := os.Stat(config.Mounts); os.IsNotExist(err) {
			return fmt.Errorf("mount path does not exist: %s", config.Mounts)
		}
		config.MountBool = true
		copyOrMountPath = config.Mounts
	}

	// If using script, validate script file exists and language is provided
	if config.Script != "" {
		fullScriptPath := filepath.Join(copyOrMountPath, config.Script)
		if _, err := os.Stat(fullScriptPath); os.IsNotExist(err) {
			return fmt.Errorf("script file does not exist: %s at this dir: %s", config.Script, fullScriptPath)
		}

		if config.Language == "" {
			return fmt.Errorf("--language is required when using --script")
		}

		// Validate supported languages
		supportedLangs := map[string]bool{
			"javascript": true,
			"python":     true,
			"golang":     true,
			"rust":       true,
			"java":       true,
			"bash":       true,
		}

		if !supportedLangs[strings.ToLower(config.Language)] {
			return fmt.Errorf("unsupported language: %s", config.Language)
		}
	}

	return nil
}

func executeContainer(config *runConfig.RunConfig) error {

	color.New(color.FgGreen, color.Bold).Println("🚀 Container starting...")
	color.New(color.FgWhite).Println("📦 Setting up isolated environment...")

	switch os.Args[1] {
	case "run":
		InitProcess(&configJSONFile, &alpineFS, config)

	default:
		panic("unknown command")
	}

	return nil
}

func must(reply string, err error) {
	if err != nil {
		log.Printf("[❌] %s: %v", reply, err)
		os.Exit(1)
	}
}
