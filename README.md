# ecs-connect

Interactive CLI for exec-ing into running AWS ECS tasks — no more copy-pasting ARNs.

Pick a cluster, service, and task through a guided TUI wizard, and land in a shell inside the container.

## Quick start

```bash
# Build
go build -o ecs-connect .

# Run (interactive wizard — auto-detects credentials)
./ecs-connect

# Run with a specific profile
./ecs-connect --profile my-aws-profile

# Run a specific command in the container
./ecs-connect --command /bin/bash

# Skip straight to a known cluster
./ecs-connect --cluster my-cluster

# Skip straight to a known cluster and service
./ecs-connect --cluster my-cluster --service my-service

# Suppress the banner
ECS_CONNECT_QUIET=1 ./ecs-connect
```

## How it works

### Authentication flow

The tool resolves credentials in this order:

1. **Profile given** (via `--profile`, `AWS_PROFILE`, or config file `profile:`) — uses that profile directly. If the session is expired, automatically runs `aws sso login --profile <name>`.
2. **No profile given** — runs a single STS check using the default credential chain (env vars, `[default]` profile, instance role). If that succeeds, proceeds immediately.
3. **Default chain fails** — scans every profile in `~/.aws/config` for an active SSO session. If one is found, uses it automatically.
4. **No active session found** — prompts you to choose a profile (or type one manually), then runs `aws sso login` for you.

```
 Already logged in?
┌─────────────────────────────────────┐
│ 1. Try default credential chain     │──── ✓ Already authenticated ──▶ proceed
│ 2. Scan all profiles in ~/.aws      │──── ✓ Found session (profile: X) ──▶ proceed
│ 3. Prompt user to pick a profile    │──── aws sso login ──▶ proceed
└─────────────────────────────────────┘
```

When prompted, the profile picker looks like this:

```
  ⚠ No active AWS session.

  Available AWS profiles:

    1) default
    2) sandbox
    3) sandbox-fullaccess-301581146302
    4) production-identity-390571511014
    5) Enter a profile name manually

  Choose an option [1-5]:
```

### Default mode (no config file)

```
 Auth             Cluster              Service            Task        Container
┌────────────┐  ┌──────────────────┐  ┌──────────────┐  ┌──────────┐ ┌──────────┐
│ auto-detect│─▶│ my-cluster-a     │─▶│ my-service-a │─▶│ (auto)   │─▶│ (auto)  │──▶ Session
│ or prompt  │  │ my-cluster-b     │  │ my-service-b │  │ or pick  │ │ or pick  │
│ + SSO login│  └──────────────────┘  │ my-service-c │  └──────────┘ └──────────┘
└────────────┘                        └──────────────┘
                                        ▲ preview panel
                                        │ shows service health
```

1. **Auth** — resolves credentials automatically (see authentication flow above). Only prompts if no active session is found anywhere.
2. **Select cluster** — lists all ECS clusters in the account.
3. **Select service** — lists all services in the selected cluster with a live preview panel.
4. **Select task** — lists RUNNING tasks sorted by creation time (newest first). Auto-selects if only one exists.
5. **Select container** — auto-selects if the task has a single container; prompts otherwise.
6. **Connect** — calls `ExecuteCommand` and hands off to `session-manager-plugin`.

### With config file (environment-based naming)

When a `.ecs-connect.yaml` config file is present with environments defined, the tool adds environment selection and service slug mapping:

```
 Auth           Environment       Cluster            Service          Task
┌────────────┐ ┌────────────┐   ┌────────────────┐  ┌────────────┐  ┌──────────┐
│ auto-detect│▶│ staging    │──▶│ home-staging   │─▶│ web        │─▶│ (auto)   │──▶ Session
│ or prompt  │ │ production │   │ auth-staging   │  │ worker     │  │ or pick  │
│ + SSO login│ └────────────┘   └────────────────┘  │ sidekiq    │  └──────────┘
└────────────┘                                      └────────────┘
                                                      ▲ preview panel
                                                      │ shows status, desired/running
                                                      │ counts, and task definition
```

1. **Auth** — same auto-detect flow as default mode.
2. **Select environment** — from the environments listed in the config file.
3. **Select cluster** — lists clusters ending with `-{env}` (e.g. `home-staging`).
4. **Select service** — maps ECS services to friendly slugs (`web`, `worker`, …) with a live preview panel showing service health.
5. **Confirmation** — if the selected environment has `confirm: true`, you must type `yes` to proceed.
6. **Select task** — lists RUNNING tasks sorted by creation time (newest first). Auto-selects if only one exists.
7. **Select container** — auto-selects if the task has a single container; prompts otherwise.
8. **Connect** — calls `ExecuteCommand` and hands off to `session-manager-plugin`.

## Configuration

Create a `.ecs-connect.yaml` file to customise the tool for your team. Without this file the tool runs in **generic mode** — all clusters and services are listed as-is with `/bin/sh` as the default command.

### Config file lookup order

1. `--config` flag (or `ECS_CONNECT_CONFIG` env var) — explicit path
2. `.ecs-connect.yaml` or `.ecs-connect.yml` in the current working directory
3. `~/.ecs-connect.yaml` or `~/.ecs-connect.yml` in your home directory

If none are found the tool uses built-in defaults (generic mode).

### Value precedence

When the same setting can come from multiple sources, the first match wins:

**CLI flag → environment variable → config file → built-in default**

For example, `--command /bin/bash` overrides `COMMAND` env var, which overrides the `command:` field in the config file.

### Full config reference

```yaml
# ──────────────────────────────────────────────────────────────────────
# .ecs-connect.yaml — all fields are optional
# ──────────────────────────────────────────────────────────────────────

# profile — AWS CLI profile to use.
# Overridden by --profile flag or AWS_PROFILE env var.
# If not set (and not given via flag/env), the tool auto-detects
# active sessions or prompts you to choose a profile.
profile: my-aws-profile

# environments — list of environment names.
# When present, enables the environment selection step and
# cluster/service naming conventions ({app}-{env}).
# When absent, the tool lists all clusters and services directly.
environments:
  - name: staging
  - name: production
    confirm: true          # require typing "yes" before connecting

# default_slug — friendly name shown for the "bare" service
# (the one matching {app}-{env} with no slug segment).
# Default: "web"
default_slug: web

# command — default command to execute inside the container.
# Overridden by --command flag or COMMAND env var.
# Built-in default (without config file): /bin/sh
command: "bundle exec rails c -- --noautocomplete"

# region — AWS region to use for API calls.
# Overridden by --region flag, AWS_REGION, or AWS_DEFAULT_REGION env vars.
# Built-in default (without config file): resolved from your AWS profile.
region: eu-west-1
```

### Config field details

| Field | Type | Default | Description |
|---|---|---|---|
| `profile` | string | *(auto-detect)* | AWS CLI profile to use. When omitted (and no `--profile` / `AWS_PROFILE`), the tool checks for active sessions automatically and only prompts if none are found. |
| `environments` | list | *(empty — generic mode)* | Defines the selectable environments. Each entry has a `name` (required) and an optional `confirm` flag. When present, enables environment-based cluster/service filtering. |
| `environments[].name` | string | — | Environment name (e.g. `staging`, `production`, `dev`). Clusters ending with `-{name}` are shown for this environment. |
| `environments[].confirm` | bool | `false` | When `true`, the user must type `yes` before connecting. Useful for production or other sensitive environments. |
| `default_slug` | string | `web` | The slug label assigned to services that match `{app}-{env}` exactly (no slug segment in the name). |
| `command` | string | `/bin/sh` | The command to execute inside the container. Common values: `/bin/bash`, `bundle exec rails c -- --noautocomplete`, `python manage.py shell`. |
| `region` | string | *(from profile)* | AWS region for all API calls. When omitted, the region is resolved from `--region` flag, `AWS_REGION` env var, or the profile's region in `~/.aws/config`. |

### Example configs

**Minimal — just set the command and region:**

```yaml
command: /bin/bash
region: us-west-2
```

**Rails app with staging + production:**

```yaml
profile: my-company-prod
environments:
  - name: staging
  - name: production
    confirm: true

command: "bundle exec rails c -- --noautocomplete"
region: eu-west-1
```

**Python/Django app with three environments:**

```yaml
profile: django-sso
environments:
  - name: dev
  - name: staging
  - name: production
    confirm: true

default_slug: api
command: "python manage.py shell"
region: us-east-1
```

**Node.js app — just a shell, no naming conventions:**

```yaml
command: /bin/bash
region: ap-southeast-1
```

### Naming conventions (config mode)

When `environments` are configured, the tool uses these naming patterns to discover and display resources:

| Concept | Pattern | Example |
|---|---|---|
| Cluster | `{app}-{env}` | `home-staging` |
| App group | cluster minus env suffix | `home` |
| Service (default slug) | `{app}-{env}` | `home-staging` → slug **web** |
| Service (other) | `{app}-{slug}-{env}` | `home-worker-staging` → slug **worker** |

The `default_slug` config field controls what the "bare" service (`{app}-{env}`) is displayed as. Most teams call it `web`, but you can set it to `api`, `main`, or anything else.

## Flags and environment variables

All settings can be passed as flags or environment variables. Flags take precedence.

| Flag | Env var | Default | Description |
|---|---|---|---|
| `--profile` | `AWS_PROFILE` | *(auto-detect)* | AWS CLI / SSO profile. If not set, auto-detects active sessions; prompts only if none found. |
| `--region` | `AWS_REGION`, `AWS_DEFAULT_REGION` | *(from profile)* | AWS region |
| `--command` | `COMMAND` | `/bin/sh` | Command to run in the container |
| `--config` | `ECS_CONNECT_CONFIG` | *(auto-discover)* | Path to config file |
| `--cluster` | | | ECS cluster (skip interactive selection) |
| `--service` | | | ECS service (skip interactive selection) |
| `--quiet` | `ECS_CONNECT_QUIET=1` | off | Suppress the startup banner |

## Prerequisites

- **Go 1.22+** (build only)
- **AWS CLI** configured with your SSO profile(s)
- **session-manager-plugin** — [install guide](https://docs.aws.amazon.com/systems-manager/latest/userguide/session-manager-working-with-install-plugin.html)
- **ECS Exec** enabled on the target services/tasks
- IAM permissions for `ecs:ExecuteCommand`, `ecs:DescribeTasks`, `ecs:ListTasks`, `ecs:ListClusters`, `ecs:ListServices`, `ecs:DescribeServices`, `sts:GetCallerIdentity`, and SSM session access

## Project structure

```
ecs-connect/
├── main.go                  Entry point, config, auth, banner, session exec
├── internal/
│   ├── cloud/
│   │   ├── cloud.go         AWS SDK v2 client (STS + ECS operations)
│   │   └── profiles.go      Parse ~/.aws/config for profile names
│   ├── config/
│   │   └── config.go        YAML config file loading
│   ├── naming/
│   │   ├── naming.go        Cluster/service naming conventions
│   │   └── naming_test.go   Tests
│   └── tui/
│       ├── model.go         Bubbletea model, Init, Update, commands
│       └── view.go          View rendering, lipgloss styles, preview panel
├── .ecs-connect.yaml        Example config file
├── .goreleaser.yaml         GoReleaser build config
├── go.mod
└── go.sum
```

## Keyboard shortcuts

| Key | Action |
|---|---|
| `↑` / `k` | Move up |
| `↓` / `j` | Move down |
| `Enter` | Select |
| `/` | Filter list |
| `Esc` | Clear filter / Cancel |
| `Ctrl+C` | Quit |
