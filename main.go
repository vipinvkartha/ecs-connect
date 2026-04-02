package main

import (
	"context"
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
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

	client := ensureAuth(cfg.Profile, cfg.Region)

	var opts tui.Options
	opts.Client = client
	opts.Config = fileCfg
	opts.Cluster = cfg.Cluster
	opts.Service = cfg.Service

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

	flag.Usage = printHelp
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
	if !cfg.profileExplicit && os.Getenv("AWS_PROFILE") == "" && fileCfg.Profile != "" {
		cfg.Profile = fileCfg.Profile
	}
	if !cfg.regionExplicit && os.Getenv("AWS_REGION") == "" && os.Getenv("AWS_DEFAULT_REGION") == "" && fileCfg.Region != "" {
		cfg.Region = fileCfg.Region
	}
	if !cfg.commandExplicit && os.Getenv("COMMAND") == "" && fileCfg.Command != "" {
		cfg.Command = fileCfg.Command
	}
}

// -------------------------------------------------------------------------
// Auth: check credentials, auto-login via SSO if needed
// -------------------------------------------------------------------------

// ensureAuth checks if the user is already authenticated via STS.
//   - If a profile is given (flag/env/config): use it, auto SSO login if needed.
//   - If no profile is given: try the default credential chain, then scan all
//     profiles for an active session. Only prompts to pick a profile and login
//     if no existing session is found.
func ensureAuth(profile, region string) *cloud.Client {
	if profile != "" {
		return authWithProfile(profile, region)
	}

	// Try default credential chain first (covers AWS_PROFILE, env creds,
	// [default] profile, instance roles, etc.).
	client, err := cloud.New("", region)
	if err == nil {
		if _, err := client.CheckAuth(context.Background()); err == nil {
			fmt.Print("\n  ✓ Already authenticated\n\n")
			return client
		}
	}

	// Default chain failed — SSO tokens are profile-specific, so check
	// each profile for an active session.
	fmt.Print("\n  Checking for active AWS sessions...")
	for _, p := range cloud.ListProfiles() {
		c, err := cloud.New(p, region)
		if err != nil {
			continue
		}
		if _, err := c.CheckAuth(context.Background()); err == nil {
			fmt.Printf(" found!\n\n  ✓ Authenticated (profile: %s)\n\n", p)
			return c
		}
	}

	// No active session on any profile — prompt to choose and login.
	fmt.Print(" none found.\n\n  ⚠ No active AWS session.\n")
	profile = promptProfile()
	return authWithProfile(profile, region)
}

// authWithProfile authenticates with a specific profile, auto-running
// `aws sso login` if credentials are expired or missing.
func authWithProfile(profile, region string) *cloud.Client {
	client, err := cloud.New(profile, region)
	if err != nil {
		fatal("Failed to initialise AWS client: %v", err)
	}

	_, err = client.CheckAuth(context.Background())
	if err == nil {
		fmt.Printf("\n  ✓ Authenticated as profile %q\n\n", profile)
		return client
	}

	fmt.Printf("\n  ⚠ Not logged in (profile: %s). Running SSO login...\n\n", profile)

	if err := runSSOLogin(profile); err != nil {
		fatal("SSO login failed: %v\n\n  You can also try manually:\n    aws sso login --profile %s", err, profile)
	}

	client, err = cloud.New(profile, region)
	if err != nil {
		fatal("Failed to initialise AWS client after login: %v", err)
	}

	_, err = client.CheckAuth(context.Background())
	if err != nil {
		fatal("Still not authenticated after SSO login.\n\n  Details: %v\n\n  Verify the profile %q has valid SSO config and you completed the browser login.", err, profile)
	}

	fmt.Printf("\n  ✓ Authenticated as profile %q\n\n", profile)
	return client
}

// promptProfile lists AWS profiles from ~/.aws/config and asks the user
// to pick one. Falls back to "default" if no profiles are found.
func promptProfile() string {
	profiles := cloud.ListProfiles()
	if len(profiles) == 0 {
		return "default"
	}

	fmt.Print("\n  No profile specified. Available AWS profiles:\n\n")
	for i, p := range profiles {
		fmt.Printf("    %d) %s\n", i+1, p)
	}
	fmt.Printf("    %d) Enter a profile name manually\n", len(profiles)+1)
	fmt.Println()

	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Printf("  Choose an option [1-%d]: ", len(profiles)+1)
		line, _ := reader.ReadString('\n')
		line = strings.TrimSpace(line)

		if line == "" {
			continue
		}
		n, err := strconv.Atoi(line)
		if err != nil || n < 1 || n > len(profiles)+1 {
			fmt.Printf("  Invalid choice %q — enter a number between 1 and %d.\n", line, len(profiles)+1)
			continue
		}

		if n == len(profiles)+1 {
			for {
				fmt.Print("  Enter profile name: ")
				name, _ := reader.ReadString('\n')
				name = strings.TrimSpace(name)
				if name != "" {
					fmt.Printf("\n  Selected profile: %s\n", name)
					return name
				}
			}
		}

		selected := profiles[n-1]
		fmt.Printf("\n  Selected profile: %s\n", selected)
		return selected
	}
}

// runSSOLogin shells out to `aws sso login --profile <profile>`.
// The command inherits stdin/stdout/stderr so the user can complete the
// browser-based login flow interactively.
func runSSOLogin(profile string) error {
	awsPath, err := exec.LookPath("aws")
	if err != nil {
		return fmt.Errorf("aws CLI not found in PATH — install it to enable automatic SSO login")
	}
	cmd := exec.Command(awsPath, "sso", "login", "--profile", profile)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// -------------------------------------------------------------------------
// Banner / summary
// -------------------------------------------------------------------------

func printHelp() {
	fmt.Print(`
ecs-connect — interactive ECS Exec into a running task

Usage:
  ecs-connect [flags]

Flags:
  --profile <name>    AWS CLI profile (env: AWS_PROFILE)
                      If not set, auto-detects active sessions.
                      Prompts to choose a profile only if none found.

  --region <region>   AWS region (env: AWS_REGION, AWS_DEFAULT_REGION)
                      Defaults to the region in your AWS profile.

  --command <cmd>     Command to run in the container (env: COMMAND)
                      Default: /bin/sh

  --config <path>     Path to .ecs-connect.yaml config file (env: ECS_CONNECT_CONFIG)
                      Auto-discovers from current dir or home dir if not set.

  --cluster <name>    ECS cluster name — skip interactive cluster selection.

  --service <name>    ECS service name — skip interactive service selection.

  --quiet             Suppress the startup banner (env: ECS_CONNECT_QUIET=1)

  --help              Show this help message.

Authentication:
  1. If --profile / AWS_PROFILE / config profile is set → uses that profile.
  2. Otherwise, checks the default credential chain (env vars, default profile).
  3. If that fails, scans all profiles in ~/.aws/config for an active session.
  4. If no session found, prompts you to pick a profile and runs aws sso login.

Config file (.ecs-connect.yaml):
  Looked up in this order:
    1. --config flag or ECS_CONNECT_CONFIG env var
    2. .ecs-connect.yaml in the current directory
    3. ~/.ecs-connect.yaml in your home directory

  Fields: profile, environments, default_slug, command, region
  See README.md for full reference.

Examples:
  ecs-connect                                      Interactive wizard
  ecs-connect --profile prod                       Use specific profile
  ecs-connect --cluster my-cluster --service web   Skip pickers
  ecs-connect --command /bin/bash                   Use bash instead of sh
  AWS_PROFILE=prod ecs-connect                     Profile via env var

`)
}

func printBanner() {
	fmt.Println(`
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  ecs-connect — interactive ECS Exec into a running task

  Flags
    --profile    AWS CLI profile     (env: AWS_PROFILE; auto-detect if unset)
    --region     AWS region          (env: AWS_REGION; from profile if unset)
    --command    Container command   (env: COMMAND, default: /bin/sh)
    --config     Config file path    (env: ECS_CONNECT_CONFIG)
    --cluster    Skip cluster picker
    --service    Skip service picker
    --quiet      Suppress banner     (env: ECS_CONNECT_QUIET=1)
    --help       Show all options
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
