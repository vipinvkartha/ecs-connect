# ecs-connect

Interactive CLI for exec-ing into running AWS ECS tasks — no more copy-pasting ARNs.

Pick a cluster, service, and task through a guided TUI wizard, and land in a shell inside the container.

## Quick start

```bash
# Build
go build -o ecs-connect .

# Run (interactive wizard)
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

### Default mode (no config file)

```
 Profile          Cluster              Service            Task        Container
┌────────────┐  ┌──────────────────┐  ┌──────────────┐  ┌──────────┐ ┌──────────┐
│ default    │─▶│ my-cluster-a     │─▶│ my-service-a │─▶│ (auto)   │─▶│ (auto)  │──▶ Session
│ my-profile │  │ my-cluster-b     │  │ my-service-b │  │ or pick  │ │ or pick  │
│ (skip if   │  └──────────────────┘  │ my-service-c │  └──────────┘ └──────────┘
│  --profile)│                        └──────────────┘
└────────────┘                          ▲ preview panel
                                        │ shows service health
```

1. **Select profile** — picks an AWS profile from `~/.aws/config`. Skipped if `--profile` is passed.
2. **Auth check** — validates your AWS session (STS); tells you to `aws sso login` if expired.
3. **Select cluster** — lists all ECS clusters in the account.
4. **Select service** — lists all services in the selected cluster with a live preview panel.
5. **Select task** — lists RUNNING tasks sorted by creation time (newest first). Auto-selects if only one exists.
6. **Select container** — auto-selects if the task has a single container; prompts otherwise.
7. **Connect** — calls `ExecuteCommand` and hands off to `session-manager-plugin`.

### With config file (environment-based naming)

When a `.ecs-connect.yaml` config file is present with environments defined, the tool adds environment selection and service slug mapping:

```
 Profile        Environment       Cluster            Service          Task
┌────────────┐ ┌────────────┐   ┌────────────────┐  ┌────────────┐  ┌──────────┐
│ (optional) │▶│ staging    │──▶│ home-staging   │─▶│ web        │─▶│ (auto)   │──▶ Session
└────────────┘ │ production │   │ auth-staging   │  │ worker     │  │ or pick  │
               └────────────┘   └────────────────┘  │ sidekiq    │  └──────────┘
                                                    └────────────┘
                                                      ▲ preview panel
                                                      │ shows status, desired/running
                                                      │ counts, and task definition
```

1. **Select profile** — picks an AWS profile from `~/.aws/config`. Skipped if `--profile` is passed.
2. **Auth check** — validates your AWS session (STS); tells you to `aws sso login` if expired.
3. **Select environment** — from the environments listed in the config file.
4. **Select cluster** — lists clusters ending with `-{env}` (e.g. `home-staging`).
5. **Select service** — maps ECS services to friendly slugs (`web`, `worker`, …) with a live preview panel showing service health.
6. **Confirmation** — if the selected environment has `confirm: true`, you must type `yes` to proceed.
7. **Select task** — lists RUNNING tasks sorted by creation time (newest first). Auto-selects if only one exists.
8. **Select container** — auto-selects if the task has a single container; prompts otherwise.
9. **Connect** — calls `ExecuteCommand` and hands off to `session-manager-plugin`.

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
environments:
  - name: staging
  - name: production
    confirm: true

command: "bundle exec rails c -- --noautocomplete"
region: eu-west-1
```

**Python/Django app with three environments:**

```yaml
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
| `--profile` | `AWS_PROFILE` | *(interactive picker)* | AWS CLI / SSO profile |
| `--region` | `AWS_REGION`, `AWS_DEFAULT_REGION` | *(from profile)* | AWS region |
| `--command` | `COMMAND` | `/bin/sh` | Command to run in the container |
| `--config` | `ECS_CONNECT_CONFIG` | *(auto-discover)* | Path to config file |
| `--cluster` | | | ECS cluster (skip interactive selection) |
| `--service` | | | ECS service (skip interactive selection) |
| `--quiet` | `ECS_CONNECT_QUIET=1` | off | Suppress the startup banner |

## Prerequisites

- **Go 1.22+** (build only)
- **AWS CLI** configured with your SSO profile
- **session-manager-plugin** — [install guide](https://docs.aws.amazon.com/systems-manager/latest/userguide/session-manager-working-with-install-plugin.html)
- **ECS Exec** enabled on the target services/tasks
- IAM permissions for `ecs:ExecuteCommand`, `ecs:DescribeTasks`, `ecs:ListTasks`, `ecs:ListClusters`, `ecs:ListServices`, `ecs:DescribeServices`, `sts:GetCallerIdentity`, and SSM session access

## Project structure

```
ecs-connect/
├── main.go                  Entry point, config, banner, session exec
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
