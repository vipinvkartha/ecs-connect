# ecs-connect

Interactive CLI for exec-ing into running AWS ECS tasks — no more copy-pasting ARNs.

Pick an environment, cluster, service, and task through a guided TUI wizard, and land in a shell (or Rails console) inside the container.

## Quick start

```bash
# Build
go build -o ecs-connect .

# Run (interactive wizard)
./ecs-connect

# Run with a specific profile
./ecs-connect --profile my-aws-profile

# Drop into a bash shell instead of a Rails console
./ecs-connect --command /bin/bash

# Skip the banner
ECS_CONNECT_QUIET=1 ./ecs-connect
```

## How it works

```
 Environment       Cluster            Service          Task        Container
┌────────────┐   ┌────────────────┐  ┌────────────┐  ┌──────────┐ ┌──────────┐
│ staging    │──▶│ home-staging   │─▶│ web        │─▶│ (auto)   │─▶│ (auto)  │──▶ Session
│ production │   │ auth-staging   │  │ worker     │  │ or pick  │ │ or pick  │
└────────────┘   └────────────────┘  │ sidekiq    │  └──────────┘ └──────────┘
                                     └────────────┘
                                       ▲ preview panel
                                       │ shows status, desired/running
                                       │ counts, and task definition
```

1. **Select environment** — `staging` or `production`.
2. **Auth check** — validates your AWS session (STS); tells you to `aws sso login` if expired.
3. **Select cluster** — lists clusters ending with `-{env}` (e.g. `home-staging`).
4. **Select service** — maps ECS services to friendly slugs (`web`, `worker`, …) with a live preview panel showing service health.
5. **Production guard** — if you chose production, you must type `yes` to proceed.
6. **Select task** — lists RUNNING tasks sorted by creation time (newest first). Auto-selects if only one exists.
7. **Select container** — auto-selects if the task has a single container; prompts otherwise.
8. **Connect** — calls `ExecuteCommand` and hands off to `session-manager-plugin`.

## Naming conventions

The tool relies on a consistent naming scheme to discover and display resources:

| Concept | Pattern | Example |
|---|---|---|
| Cluster | `{app}-{env}` | `home-staging` |
| App group | cluster minus env suffix | `home` |
| Service (web) | `{app}-{env}` | `home-staging` → slug **web** |
| Service (other) | `{app}-{slug}-{env}` | `home-worker-staging` → slug **worker** |

## Configuration

All settings can be passed as flags or environment variables. Flags take precedence.

| Flag | Env var | Default | Description |
|---|---|---|---|
| `--profile` | `AWS_PROFILE` | *(none -- interactive picker)* | AWS CLI / SSO profile |
| `--region` | `AWS_REGION` | `eu-west-1` | AWS region |
| `--command` | `COMMAND` | `bundle exec rails c -- --noautocomplete` | Command to run in the container |
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
│   │   └── cloud.go         AWS SDK v2 client (STS + ECS operations)
│   ├── naming/
│   │   ├── naming.go        Cluster/service naming conventions
│   │   └── naming_test.go   Tests
│   └── tui/
│       ├── model.go         Bubbletea model, Init, Update, commands
│       └── view.go          View rendering, lipgloss styles, preview panel
├── go.mod
└── go.sum
```

## Keyboard shortcuts

| Key | Action |
|---|---|
| `↑` / `k` | Move up |
| `↓` / `j` | Move down |
| `Enter` | Select |
| `Esc` | Cancel |
| `Ctrl+C` | Quit |

## Differences from the Bash script

| | Bash (`ecs-connect.sh`) | Go (`ecs-connect`) |
|---|---|---|
| Runtime deps | `aws` CLI, `fzf`, `python3` | `session-manager-plugin` only |
| AWS calls | Shells out to `aws` CLI | Native AWS SDK v2 |
| Task selection | Picks last ARN (arbitrary order) | `DescribeTasks` + sort by `createdAt` |
| Container name | Hardcoded to app group | Resolved dynamically from task |
| Pagination | Manual `nextToken` loops | SDK paginators |
| Distribution | Script file | Single compiled binary |
