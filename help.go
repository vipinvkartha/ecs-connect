package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/term"
)

func helpNoColor() bool {
	return os.Getenv("NO_COLOR") != ""
}

func helpTermW() int {
	w, _, err := term.GetSize(uintptr(os.Stdout.Fd()))
	if err != nil || w < 48 {
		return 78
	}
	if w > 104 {
		return 104
	}
	return w
}

func helpStyles() (
	title lipgloss.Style,
	accent lipgloss.Style,
	dim lipgloss.Style,
	meta lipgloss.Style,
	flagKey lipgloss.Style,
	usageBox lipgloss.Style,
	section lipgloss.Style,
	bullet lipgloss.Style,
	exCmd lipgloss.Style,
	rule lipgloss.Style,
) {
	title = lipgloss.NewStyle().Bold(true)
	accent = lipgloss.NewStyle().Bold(true)
	dim = lipgloss.NewStyle()
	meta = lipgloss.NewStyle()
	flagKey = lipgloss.NewStyle().Bold(true)
	section = lipgloss.NewStyle().Bold(true)
	bullet = lipgloss.NewStyle()
	exCmd = lipgloss.NewStyle().Bold(true)
	rule = lipgloss.NewStyle()
	usageBox = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)

	if !helpNoColor() {
		title = title.Foreground(lipgloss.Color("51"))
		accent = accent.Foreground(lipgloss.Color("141"))
		dim = dim.Foreground(lipgloss.Color("245"))
		meta = meta.Foreground(lipgloss.Color("243")).Italic(true)
		flagKey = flagKey.Foreground(lipgloss.Color("117"))
		section = section.Foreground(lipgloss.Color("213"))
		bullet = bullet.Foreground(lipgloss.Color("62"))
		exCmd = exCmd.Foreground(lipgloss.Color("80"))
		rule = rule.Foreground(lipgloss.Color("238"))
		usageBox = usageBox.BorderForeground(lipgloss.Color("62"))
	}
	return
}

func printHelp() {
	fmt.Print(renderHelp())
}

func renderHelp() string {
	w := helpTermW()
	innerW := max(56, w-8)

	title, accent, dim, meta, flagKey, usageBox, section, bullet, exCmd, rule := helpStyles()

	head := lipgloss.JoinVertical(lipgloss.Left,
		title.Render("ecs-connect"),
		dim.Render("Interactive ECS Exec into a running task"),
	)

	usageInner := lipgloss.JoinVertical(lipgloss.Left,
		dim.Render("Usage"),
		"",
		accent.Render("  ecs-connect")+dim.Render(" [flags]"),
	)
	usageBlock := usageBox.Width(innerW).Render(usageInner)

	flagPairs := []struct {
		key  string
		env  string
		desc []string
	}{
		{"--profile <name>", "AWS_PROFILE", []string{
			"AWS CLI profile. If not set, auto-detects active sessions.",
			"Prompts to choose a profile only if none found.",
		}},
		{"--region <region>", "AWS_REGION, AWS_DEFAULT_REGION", []string{
			"AWS region for API calls.",
			"Defaults to the region in your AWS profile.",
		}},
		{"--command <cmd>", "COMMAND", []string{
			"Command to run in the container. Default: /bin/sh",
		}},
		{"--config <path>", "ECS_CONNECT_CONFIG", []string{
			"Path to .ecs-connect.yaml (auto-discovers from cwd or home).",
		}},
		{"--cluster <name>", "", []string{"ECS cluster — skip interactive cluster selection."}},
		{"--service <name>", "", []string{"ECS service — skip interactive service selection."}},
		{"--container <name>", "", []string{"ECS container — skip picker when it matches the task (use with cluster + service)."}},
		{"--quiet", "ECS_CONNECT_QUIET=1", []string{"Suppress the startup banner."}},
		{"--help", "", []string{"Show this help message."}},
	}

	var flagBlocks []string
	for _, fp := range flagPairs {
		flagBlocks = append(flagBlocks, renderFlagRow(fp.key, fp.env, fp.desc, innerW, flagKey, meta, dim))
	}
	flagsSection := lipgloss.JoinVertical(lipgloss.Left, append([]string{
		section.Render("Flags"),
		rule.Render(strings.Repeat("─", min(innerW, 72))),
		"",
	}, flagBlocks...)...)

	authSteps := []string{
		"If --profile / AWS_PROFILE / config profile is set → uses that profile.",
		"Otherwise, checks the default credential chain (env vars, default profile).",
		"If that fails, scans all profiles in ~/.aws/config for an active session.",
		"If no session found, prompts you to pick a profile and runs aws sso login.",
	}
	var authLines []string
	for i, line := range authSteps {
		num := accent.Render(fmt.Sprintf("%d.", i+1))
		authLines = append(authLines, fmt.Sprintf("  %s %s", num, dim.Render(line)))
	}
	authSection := lipgloss.JoinVertical(lipgloss.Left, append([]string{
		"",
		section.Render("Authentication"),
		rule.Render(strings.Repeat("─", min(innerW, 72))),
		"",
	}, authLines...)...)

	configBody := strings.Join([]string{
		dim.Render("Looked up in this order:"),
		"",
		"  " + bullet.Render("•") + "  " + dim.Render("--config flag or ECS_CONNECT_CONFIG env var"),
		"  " + bullet.Render("•") + "  " + dim.Render(".ecs-connect.yaml in the current directory"),
		"  " + bullet.Render("•") + "  " + dim.Render("~/.ecs-connect.yaml in your home directory"),
		"",
		dim.Render("Fields: profile, environments, default_slug, command, region,"),
		dim.Render("defaults: profile, backend (ecs|dynamo), environment, cluster,"),
		dim.Render("service, container, dynamo_table, dynamo_keyword (CLI/env override when set)."),
		dim.Render("Full annotated example: ecs-connect.example.yaml (copy to .ecs-connect.yaml)."),
	}, "\n")
	configSection := lipgloss.JoinVertical(lipgloss.Left,
		"",
		section.Render("Config file (.ecs-connect.yaml)"),
		rule.Render(strings.Repeat("─", min(innerW, 72))),
		"",
		configBody,
	)

	examples := []struct {
		cmd  string
		note string
	}{
		{"ecs-connect", "Interactive wizard"},
		{"ecs-connect --profile prod", "Use specific profile"},
		{"ecs-connect --cluster my-cluster --service web", "Skip cluster & service pickers"},
		{"ecs-connect --cluster c --service s --container app", "Also skip container when it matches"},
		{"ecs-connect --command /bin/bash", "Use bash instead of sh"},
		{"AWS_PROFILE=prod ecs-connect", "Profile via env var"},
	}
	var exLines []string
	for _, ex := range examples {
		line := "  " + bullet.Render("›") + "  " + exCmd.Render(ex.cmd)
		if ex.note != "" {
			line += " " + dim.Render("— "+ex.note)
		}
		exLines = append(exLines, line)
	}
	exSection := lipgloss.JoinVertical(lipgloss.Left, append([]string{
		"",
		section.Render("Examples"),
		rule.Render(strings.Repeat("─", min(innerW, 72))),
		"",
	}, exLines...)...)

	doc := lipgloss.JoinVertical(lipgloss.Left,
		head,
		"",
		usageBlock,
		"",
		flagsSection,
		authSection,
		configSection,
		exSection,
		"",
	)

	return lipgloss.NewStyle().Margin(0, 2).Render(doc) + "\n"
}

func renderFlagRow(key, env string, desc []string, maxW int, flagKey, meta, dim lipgloss.Style) string {
	left := flagKey.Render(key)
	if env != "" {
		left += "\n" + meta.Render("env: "+env)
	}
	const leftW = 34
	leftBlock := lipgloss.Place(leftW, lipgloss.Height(left), lipgloss.Left, lipgloss.Top, left)

	var descJoined strings.Builder
	for i, line := range desc {
		if i > 0 {
			descJoined.WriteString("\n")
		}
		descJoined.WriteString(dim.Render(wrapHelpLine(line, max(28, maxW-leftW-4))))
	}
	row := lipgloss.JoinHorizontal(lipgloss.Top, leftBlock, "  ", descJoined.String())
	return row + "\n"
}

func wrapHelpLine(s string, limit int) string {
	if limit < 24 || len(s) <= limit {
		return s
	}
	var out strings.Builder
	words := strings.Fields(s)
	lineLen := 0
	for _, w := range words {
		add := len(w)
		if lineLen > 0 {
			add++
		}
		if lineLen+add > limit && lineLen > 0 {
			out.WriteByte('\n')
			lineLen = 0
		}
		if lineLen > 0 {
			out.WriteByte(' ')
			lineLen++
		}
		out.WriteString(w)
		lineLen += len(w)
	}
	return out.String()
}
