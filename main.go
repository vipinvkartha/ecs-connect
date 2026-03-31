package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"ecs-connect/internal/cloud"
	appconfig "ecs-connect/internal/config"
	"ecs-connect/internal/tui"
)

func main() {
	cfg := parseFlags()

	if _, err := exec.LookPath("session-manager-plugin"); err != nil {
		fatal("session-manager-plugin not found in PATH.\n"+
			"  Install: https://docs.aws.amazon.com/systems-manager/latest/userguide/session-manager-working-with-install-plugin.html")
	}

	fileCfg := loadConfig(cfg.ConfigPath)
	applyConfigDefaults(&cfg, fileCfg)

	var opts tui.Options
	opts.DefaultProfile = cfg.Profile
	opts.Region = cfg.Region
	opts.Config = fileCfg
	opts.Cluster = cfg.Cluster
	opts.Service = cfg.Service

	if cfg.profileExplicit {
		client, err := cloud.New(cfg.Profile, cfg.Region)
		if err != nil {
			fatal("Failed to initialise AWS client: %v", err)
		}
		opts.Client = client
	} else {
		profiles := cloud.ListProfiles()
		if len(profiles) > 0 {
			opts.Profiles = profiles
		} else {
			client, err := cloud.New(cfg.Profile, cfg.Region)
			if err != nil {
				fatal("Failed to initialise AWS client: %v", err)
			}
			opts.Client = client
		}
	}

	result, client, err := tui.Run(opts)
	if err != nil {
		if err == tui.ErrCancelled {
			fmt.Println("\n  Cancelled.")
			return
		}
		fatal("%v", err)
	}

	if !cfg.Quiet {
		printBanner()
	}
	printSummary(result, cfg.Command)

	if err := startSession(client, result, cfg.Command); err != nil {
		fatal("Session failed: %v", err)
	}
}

// -------------------------------------------------------------------------
// Config
// -------------------------------------------------------------------------

type cliConfig struct {
	Profile         string
	Region          string
	Command         string
	Quiet           bool
	ConfigPath      string
	Cluster         string
	Service         string
	profileExplicit bool
	regionExplicit  bool
	commandExplicit bool
}

func parseFlags() cliConfig {
	var c cliConfig
	flag.StringVar(&c.Profile, "profile", os.Getenv("AWS_PROFILE"),
		"AWS CLI profile (env: AWS_PROFILE)")
	flag.StringVar(&c.Region, "region", regionDefault(),
		"AWS region (env: AWS_REGION, AWS_DEFAULT_REGION; defaults to profile region)")
	flag.StringVar(&c.Command, "command", envOr("COMMAND", "/bin/sh"),
		"Command to execute in container (env: COMMAND)")
	flag.BoolVar(&c.Quiet, "quiet", os.Getenv("ECS_CONNECT_QUIET") == "1",
		"Suppress documentation banner (env: ECS_CONNECT_QUIET=1)")
	flag.StringVar(&c.ConfigPath, "config", os.Getenv("ECS_CONNECT_CONFIG"),
		"Path to config file (env: ECS_CONNECT_CONFIG)")
	flag.StringVar(&c.Cluster, "cluster", "",
		"ECS cluster name (skip interactive selection)")
	flag.StringVar(&c.Service, "service", "",
		"ECS service name (skip interactive selection)")
	flag.Parse()

	flag.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "profile":
			c.profileExplicit = true
		case "region":
			c.regionExplicit = true
		case "command":
			c.commandExplicit = true
		}
	})
	return c
}

func regionDefault() string {
	if v := os.Getenv("AWS_REGION"); v != "" {
		return v
	}
	if v := os.Getenv("AWS_DEFAULT_REGION"); v != "" {
		return v
	}
	return ""
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func loadConfig(path string) *appconfig.Config {
	if path != "" {
		cfg, err := appconfig.Load(path)
		if err != nil {
			fatal("config file: %v", err)
		}
		return cfg
	}
	return appconfig.Discover()
}

// applyConfigDefaults overrides built-in defaults with values from the config
// file, but only when the corresponding flag/env var was not explicitly set.
func applyConfigDefaults(cfg *cliConfig, fileCfg *appconfig.Config) {
	if fileCfg == nil {
		return
	}
	if !cfg.regionExplicit && os.Getenv("AWS_REGION") == "" && os.Getenv("AWS_DEFAULT_REGION") == "" && fileCfg.Region != "" {
		cfg.Region = fileCfg.Region
	}
	if !cfg.commandExplicit && os.Getenv("COMMAND") == "" && fileCfg.Command != "" {
		cfg.Command = fileCfg.Command
	}
}

// -------------------------------------------------------------------------
// Banner / summary
// -------------------------------------------------------------------------

func printBanner() {
	fmt.Println(`
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  ecs-connect — interactive ECS Exec into a running task

  Flags
    --profile    AWS CLI profile     (env: AWS_PROFILE)
    --region     AWS region          (env: AWS_REGION; from profile if unset)
    --command    Container command   (env: COMMAND, default: /bin/sh)
    --config     Config file path    (env: ECS_CONNECT_CONFIG)
    --cluster    Skip cluster picker
    --service    Skip service picker
    --quiet      Suppress banner     (env: ECS_CONNECT_QUIET=1)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━`)
}

func printSummary(r *tui.Result, command string) {
	fmt.Println()
	if r.Profile != "" {
		fmt.Printf("  Profile     : %s\n", r.Profile)
	}
	if r.Environment != "" {
		fmt.Printf("  Environment : %s\n", r.Environment)
	}
	fmt.Printf("  Cluster     : %s\n", r.Cluster)
	fmt.Printf("  Service     : %s\n", r.Service)
	fmt.Printf("  Task        : %s\n", r.TaskShortID)
	fmt.Printf("  Container   : %s\n", r.Container)
	fmt.Printf("  Command     : %s\n", command)
	fmt.Println()
}

// -------------------------------------------------------------------------
// ECS Exec → session-manager-plugin
// -------------------------------------------------------------------------

func startSession(client *cloud.Client, r *tui.Result, command string) error {
	fmt.Println("  Starting session...")

	sess, err := client.ExecuteCommand(r.Cluster, r.TaskARN, r.Container, command)
	if err != nil {
		return err
	}

	sessJSON, _ := json.Marshal(map[string]string{
		"SessionId":  sess.SessionID,
		"StreamUrl":  sess.StreamURL,
		"TokenValue": sess.TokenValue,
	})
	targetJSON, _ := json.Marshal(map[string]string{
		"Target": r.TaskARN,
	})
	endpoint := fmt.Sprintf("https://ssm.%s.amazonaws.com", client.Region)

	pluginPath, _ := exec.LookPath("session-manager-plugin")

	return syscall.Exec(pluginPath, []string{
		"session-manager-plugin",
		string(sessJSON),
		client.Region,
		"StartSession",
		client.Profile,
		string(targetJSON),
		endpoint,
	}, os.Environ())
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "\n  Error: "+format+"\n\n", args...)
	os.Exit(1)
}
