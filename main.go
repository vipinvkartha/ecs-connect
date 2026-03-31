package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"ecs-connect/internal/cloud"
	"ecs-connect/internal/tui"
)

func main() {
	cfg := parseFlags()

	if _, err := exec.LookPath("session-manager-plugin"); err != nil {
		fatal("session-manager-plugin not found in PATH.\n"+
			"  Install: https://docs.aws.amazon.com/systems-manager/latest/userguide/session-manager-working-with-install-plugin.html")
	}

	var opts tui.Options
	opts.DefaultProfile = cfg.Profile
	opts.Region = cfg.Region

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

	cfg.Profile = result.Profile
	if !cfg.Quiet {
		printBanner()
	}
	printSummary(result, cfg.Command)

	if err := startSession(cfg, client, result); err != nil {
		fatal("Session failed: %v", err)
	}
}

// -------------------------------------------------------------------------
// Config
// -------------------------------------------------------------------------

type config struct {
	Profile         string
	Region          string
	Command         string
	Quiet           bool
	profileExplicit bool
}

func parseFlags() config {
	var c config
	flag.StringVar(&c.Profile, "profile", os.Getenv("AWS_PROFILE"),
		"AWS CLI profile (env: AWS_PROFILE)")
	flag.StringVar(&c.Region, "region", envOr("AWS_REGION", "eu-west-1"),
		"AWS region (env: AWS_REGION)")
	flag.StringVar(&c.Command, "command", envOr("COMMAND", "bundle exec rails c -- --noautocomplete"),
		"Command to execute in container (env: COMMAND)")
	flag.BoolVar(&c.Quiet, "quiet", os.Getenv("ECS_CONNECT_QUIET") == "1",
		"Suppress documentation banner (env: ECS_CONNECT_QUIET=1)")
	flag.Parse()

	flag.Visit(func(f *flag.Flag) {
		if f.Name == "profile" {
			c.profileExplicit = true
		}
	})
	return c
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// -------------------------------------------------------------------------
// Banner / summary
// -------------------------------------------------------------------------

func printBanner() {
	fmt.Println(`
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  ecs-connect — interactive ECS Exec into a running task

  Flow
    [Profile →] Environment → Auth check → Cluster → Service
    → [production confirm] → Task → Container → execute-command

  Flags
    --profile    AWS CLI profile  (env: AWS_PROFILE)
    --region     AWS region       (env: AWS_REGION, default: eu-west-1)
    --command    Container cmd    (env: COMMAND)
    --quiet      Suppress banner  (env: ECS_CONNECT_QUIET=1)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━`)
}

func printSummary(r *tui.Result, command string) {
	fmt.Printf(`
  Profile     : %s
  Environment : %s
  Cluster     : %s
  Service     : %s
  Task        : %s
  Container   : %s
  Command     : %s

`, r.Profile, r.Environment, r.Cluster, r.Service, r.TaskShortID, r.Container, command)
}

// -------------------------------------------------------------------------
// ECS Exec → session-manager-plugin
// -------------------------------------------------------------------------

func startSession(cfg config, client *cloud.Client, r *tui.Result) error {
	fmt.Println("  Starting session...")

	sess, err := client.ExecuteCommand(r.Cluster, r.TaskARN, r.Container, cfg.Command)
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
	endpoint := fmt.Sprintf("https://ssm.%s.amazonaws.com", cfg.Region)

	pluginPath, _ := exec.LookPath("session-manager-plugin")

	// Replace our process with session-manager-plugin so signals and terminal
	// I/O are handled natively by the plugin.
	return syscall.Exec(pluginPath, []string{
		"session-manager-plugin",
		string(sessJSON),
		cfg.Region,
		"StartSession",
		cfg.Profile,
		string(targetJSON),
		endpoint,
	}, os.Environ())
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "\n  Error: "+format+"\n\n", args...)
	os.Exit(1)
}
