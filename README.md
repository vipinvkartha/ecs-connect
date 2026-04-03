# ecs-connect

Interactive CLI for **ECS Exec** (shell into a running task) and **DynamoDB** read-only queries — all behind one small TUI so you stop copy-pasting ARNs and table names.

After AWS auth, choose **ECS** or **DynamoDB**. ECS walks cluster → service → task → container, then hands off to `session-manager-plugin`. DynamoDB lists tables (optionally filtered by environment keyword), runs a **Query** on partition key (+ optional sort key), and prints JSON (with mouse scroll and copy).

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

# Skip cluster / service / container pickers (when exactly one task matches, or container name is unambiguous)
./ecs-connect --cluster my-cluster --service my-service
./ecs-connect --cluster my-cluster --service my-service --container app

# Suppress the banner (ECS path only affects banner; Dynamo path still prints JSON)
ECS_CONNECT_QUIET=1 ./ecs-connect

# Skip the wizard: reconnect to the last ECS target saved for this profile
./ecs-connect --reconnect
./ecs-connect --profile prod --reconnect
```

After a successful **ECS** connect, the tool writes **`~/.ecs-connect/recents.json`** (per AWS profile). **`--reconnect`** loads that target, checks the task is still **RUNNING** and the container still exists, then opens a session — see [Reconnect](#reconnect).

See **`ecs-connect.example.yaml`** in the repo for a commented file with every supported config key — copy it to `.ecs-connect.yaml` and edit.

## Homebrew (custom tap)

Install the pre-built binary from the [`homebrew-tap`](https://github.com/vipinvkartha/homebrew-tap) repository:

```bash
brew tap vipinvkartha/tap
brew install ecs-connect
```

Upgrade: `brew upgrade ecs-connect`. Config behaviour is unchanged: use `./.ecs-connect.yaml`, `~/.ecs-connect.yaml`, or `--config` (see [Configuration](#configuration)).

### One-time setup (maintainers)

1. On GitHub, create an **empty** public repo named **`homebrew-tap`** (exact name so `brew tap vipinvkartha/tap` resolves to `github.com/vipinvkartha/homebrew-tap`). Initialise `main` (e.g. add a short `README.md` and push).
2. Create a **classic** personal access token (or fine-grained PAT) that can **push** to `vipinvkartha/homebrew-tap` (`repo` scope for classic, or Contents read/write on that repo).
3. In **`ecs-connect`** → *Settings* → *Secrets and variables* → *Actions*, add **`HOMEBREW_TAP_GITHUB_TOKEN`** with that PAT. The default `GITHUB_TOKEN` in Actions cannot push to another repo, so this secret is required for the formula bump commit.
4. Tag and push a [semantic version](https://github.com/vipinvkartha/ecs-connect/releases) (e.g. `v0.1.0`). The **release** workflow runs [GoReleaser](https://goreleaser.com/), uploads archives to this repo’s GitHub Release, and opens a commit on `homebrew-tap` under `Formula/ecs-connect.rb`.

Local snapshot without publishing: `goreleaser release --snapshot --clean` (does not push the tap).

## How it works

### Choose backend (after auth)

Unless `defaults.backend` is set in your config (`ecs` or `dynamo`), the first screen lets you pick:

- **ECS Exec (containers)** — interactive flow below; requires **session-manager-plugin** and ECS Exec on the service.
- **DynamoDB (query tables)** — no session-manager plugin; uses the AWS SDK for DynamoDB in the configured region.

### DynamoDB flow (read-only v1)

1. Connect DynamoDB client (same profile/region as ECS).
2. **With config `environments`:** pick environment (or use `defaults.environment`) — optional production **confirm** — list tables whose **names contain** that keyword (case-insensitive substring).
3. **Without naming config:** pick `staging` / `production` as the keyword, or set `defaults.dynamo_keyword`.
4. Pick table (or `defaults.dynamo_table` if it matches after filtering).
5. Enter partition key value (+ sort key if the table has one; sort key is **optional** — leave empty to query the whole partition within the limit).
6. View JSON results: **mouse wheel** to scroll, **`[` / `]`** keyboard scroll, **`c` / `y`** copy JSON (OS clipboard, with OSC 52 fallback), **`r`** new query, **`e`** edit keys, **`b`** back, **`Esc`** exit and print the last result.

Binary (`B`) key attributes are not supported in v1.

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

### Default mode (no config file, ECS path)

```
 Auth        Backend?       Cluster              Service            Task        Container
┌────────┐  ┌───────────┐  ┌────────────────┐  ┌──────────────┐  ┌──────────┐ ┌──────────┐
│ detect │─▶│ ECS|Dynamo│─▶│ my-cluster-a   │─▶│ my-service-a │─▶│ (auto)   │─▶│ (auto)   │──▶ Session
│ + SSO  │  │ (if unset)│  │ my-cluster-b   │  │ my-service-b │  │ or pick  │ │ or pick  │
└────────┘  └───────────┘  └────────────────┘  └──────────────┘  └──────────┘ └──────────┘
                                                   ▲ preview: service health + deployments
```

1. **Auth** — resolves credentials automatically (see authentication flow above). Only prompts if no active session is found anywhere.
2. **Backend** — pick ECS or DynamoDB, or skip via `defaults.backend` in YAML.
3. **Select cluster** — lists all ECS clusters in the account.
4. **Select service** — lists all services in the selected cluster with a live preview panel showing service health and the last 10 deployments (rollout state, task definition, age, running/desired counts).
5. **Select task** — lists RUNNING tasks sorted by creation time (newest first). Auto-selects if only one exists.
6. **Select container** — auto-selects if the task has a single container; prompts otherwise. Skipped when `--container` / `defaults.container` matches (see [Flags](#flags-and-environment-variables)).
7. **Connect** — calls `ExecuteCommand` and hands off to `session-manager-plugin`.

### Reconnect

Use the **`--reconnect`** flag to skip the wizard and reuse the last ECS target for the current AWS profile.

Successful **ECS** runs (interactive wizard or flags that complete an exec) save the last target under **`~/.ecs-connect/recents.json`**, keyed by AWS profile: cluster, service, task ARN, container, plus optional naming fields (`environment`, `app_group`, `slug`).

| | |
|---|---|
| **`--reconnect`** | Skip the TUI. Load the saved target for the current profile, call **`DescribeTask`** to confirm the task is **`RUNNING`**, confirm the saved **container** name is still on the task, then start **`session-manager-plugin`** like a normal connect. |
| **No saved target / verify failed** | Exit with an error (e.g. task stopped, new deployment, container renamed). Use the interactive wizard or `--cluster` / `--service` to pick a new target. |
| **DynamoDB** | **`--reconnect`** only applies to ECS (saved recents are ECS targets). |

Examples: `ecs-connect --reconnect`, `ecs-connect --profile prod --reconnect`. Same flag is documented in **`ecs-connect --help`**.

### With config file (environment-based naming, ECS path)

When a `.ecs-connect.yaml` config file is present with environments defined, the tool adds environment selection and service slug mapping:

```
 Auth        Backend?    Environment       Cluster            Service          Task
┌────────┐  ┌─────────┐ ┌────────────┐   ┌────────────────┐  ┌────────────┐  ┌──────────┐
│ detect │─▶│ ECS|DD  │▶│ staging    │──▶│ home-staging   │─▶│ web        │─▶│ (auto)   │──▶ Session
│ + SSO  │  └─────────┘ │ production │   │ auth-staging   │  │ worker     │  │ or pick  │
└────────┘              └────────────┘   └────────────────┘  └────────────┘  └──────────┘
                                                                  ▲ preview: health + deployments
```

1. **Auth** — same auto-detect flow as default mode.
2. **Backend** — pick ECS or DynamoDB, or use `defaults.backend`.
3. **Select environment** — from the environments listed in the config file (skippable via `defaults.environment` when it matches a defined env).
4. **Select cluster** — lists clusters ending with `-{env}` (e.g. `home-staging`).
5. **Select service** — maps ECS services to friendly slugs (`web`, `worker`, …) with a live preview panel showing service health and recent deployments.
6. **Confirmation** — if the selected environment has `confirm: true`, you must type `yes` to proceed (ECS and Dynamo naming flows).
7. **Select task** — lists RUNNING tasks sorted by creation time (newest first). Auto-selects if only one exists.
8. **Select container** — auto-selects if the task has a single container; prompts otherwise, unless `--container` / `defaults.container` matches.
9. **Connect** — calls `ExecuteCommand` and hands off to `session-manager-plugin`.

## Configuration

Create a `.ecs-connect.yaml` file to customise the tool for your team. Without this file the tool runs in **generic mode** — all clusters and services are listed as-is with `/bin/sh` as the default command.

### Config file lookup order

1. `--config` flag (or `ECS_CONNECT_CONFIG` env var) — explicit path
2. `.ecs-connect.yaml` or `.ecs-connect.yml` in the current working directory
3. `~/.ecs-connect.yaml` or `~/.ecs-connect.yml` in your home directory

If none are found the tool uses built-in defaults (generic mode).

If a candidate file **exists** but has **invalid YAML** (for example a mis-indented `defaults:` key), the program exits with a **parse error** pointing at that path instead of silently ignoring the file.

### Value precedence

When the same setting can come from multiple sources, the first match wins:

**CLI flag → environment variable → top-level config fields → `defaults.*` in config → built-in default**

For example, `--command /bin/bash` overrides `COMMAND` env var, which overrides the `command:` field in the config file. For cluster/service/container, CLI overrides both root fields and `defaults.*`.

### Full config reference

```yaml
# ──────────────────────────────────────────────────────────────────────
# .ecs-connect.yaml — all fields are optional
# Copy ecs-connect.example.yaml for a longer annotated template.
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

# command — default command to execute inside the container (ECS only).
# Overridden by --command flag or COMMAND env var.
# Built-in default (without config file): /bin/sh
command: "bundle exec rails c -- --noautocomplete"

# region — AWS region to use for API calls.
# Overridden by --region flag, AWS_REGION, or AWS_DEFAULT_REGION env vars.
# Built-in default (without config file): resolved from your AWS profile.
region: eu-west-1

# defaults — optional wizard shortcuts (all optional).
# Top-level profile/cluster/service still win over defaults.* when set.
# `defaults:` must start at column 0 (same as profile:), not indented under another key.
defaults:
  profile: fallback-profile          # if root profile is omitted and no AWS_PROFILE
  backend: ecs                       # ecs | dynamo — skip backend chooser
  environment: staging             # must match environments[].name when using naming
  cluster: my-app-staging          # exact ECS cluster name
  service: my-app-web-staging      # ECS service name or slug (naming mode)
  container: app                   # ECS container name (with cluster + service)
  dynamo_table: MyApp-staging-data # exact table name after keyword filter
  dynamo_keyword: staging          # table name filter when not using environments
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
| `defaults` | map | *(absent)* | Shortcuts to skip wizard steps when values match; see table below. |
| `defaults.profile` | string | — | Profile if root `profile` and `AWS_PROFILE` are unset. |
| `defaults.backend` | string | — | `ecs` / `exec` or `dynamo` / `dynamodb` / `ddb` — skip backend selection. |
| `defaults.environment` | string | — | Must match an `environments[].name` when naming mode is on; skips env picker. |
| `defaults.cluster` | string | — | Exact ECS cluster name; skips cluster list when it matches. |
| `defaults.service` | string | — | ECS service or slug; skips service list when it matches. |
| `defaults.container` | string | — | ECS container name; skips container picker when the chosen task matches. |
| `defaults.dynamo_table` | string | — | Exact DynamoDB table name after keyword filtering. |
| `defaults.dynamo_keyword` | string | — | Substring filter for table names when **not** using `environments` (generic Dynamo path). |

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

All settings can be passed as flags or environment variables. Flags take precedence over config and `defaults.*`.

| Flag | Env var | Default | Description |
|---|---|---|---|
| `--profile` | `AWS_PROFILE` | *(auto-detect)* | AWS CLI / SSO profile. If not set, auto-detects active sessions; prompts only if none found. |
| `--region` | `AWS_REGION`, `AWS_DEFAULT_REGION` | *(from profile)* | AWS region (ECS and DynamoDB) |
| `--command` | `COMMAND` | `/bin/sh` | Command to run in the container (**ECS path only**) |
| `--config` | `ECS_CONNECT_CONFIG` | *(auto-discover)* | Path to config file |
| `--cluster` | | | ECS cluster (skip interactive selection) |
| `--service` | | | ECS service (skip interactive selection) |
| `--container` | | | ECS container name — skip picker when it matches the task (use with `--cluster` / `--service`; see code for multi-task rules) |
| `--reconnect` | | off | **ECS only.** Skip the wizard and use the last target from `~/.ecs-connect/recents.json` for this profile; verifies task is RUNNING and container exists ([Reconnect](#reconnect)). |
| `--quiet` | `ECS_CONNECT_QUIET=1` | off | Suppress the startup banner (**ECS** path); Dynamo path still prints JSON |

**session-manager-plugin** is only required when you complete an **ECS** session — it is **not** needed for DynamoDB-only use.

## Prerequisites

- **Go 1.24+** (build only; see `go.mod`)
- **AWS CLI** configured with your SSO profile(s)
- **session-manager-plugin** — [install guide](https://docs.aws.amazon.com/systems-manager/latest/userguide/session-manager-working-with-install-plugin.html) — **ECS Exec path only**
- **ECS Exec** enabled on the target services/tasks
- IAM (ECS path): `ecs:ExecuteCommand`, `ecs:DescribeTasks`, `ecs:ListTasks`, `ecs:ListClusters`, `ecs:ListServices`, `ecs:DescribeServices`, `sts:GetCallerIdentity`, and SSM session access
- IAM (Dynamo path): `dynamodb:ListTables`, `dynamodb:DescribeTable`, `dynamodb:Query` (read-only usage in the tool today)

## Project structure

```
ecs-connect/
├── main.go                  Entry point, config, auth, banner, session exec / Dynamo output
├── help.go                  --help rendering
├── ecs-connect.example.yaml Full annotated config template (copy to .ecs-connect.yaml)
├── internal/
│   ├── cloud/
│   │   ├── cloud.go         AWS SDK v2 client (STS + ECS)
│   │   └── profiles.go      Parse ~/.aws/config for profile names
│   ├── config/
│   │   └── config.go        YAML config, defaults, discovery (strict parse on existing file)
│   ├── ddb/
│   │   └── ddb.go           DynamoDB list / describe / query + JSON helpers
│   ├── naming/
│   │   └── naming.go        Cluster/service naming conventions
│   ├── recents/
│   │   └── recents.go       ~/.ecs-connect/recents.json (last ECS target per profile)
│   └── tui/
│       ├── model.go         Bubble Tea model, ECS + Dynamo flows
│       ├── view.go          Lipgloss UI, preview + Dynamo results
│       ├── outcome.go       ECS result vs Dynamo outcome
│       ├── reconnect.go     --reconnect (verify saved target, no wizard)
│       └── clip.go          Clipboard helper (Dynamo results)
├── .goreleaser.yaml
├── go.mod
└── go.sum
```

## Keyboard shortcuts

### Lists (cluster, service, task, env, …)

| Key | Action |
|---|---|
| `↑` / `k` | Move up |
| `↓` / `j` | Move down |
| `Enter` | Select |
| `/` | Filter list |
| `b` | Go back one step (when filter is not active) |
| `Esc` | Clear filter, or cancel / quit (see on-screen footer) |
| `Ctrl+C` | Quit |

### Service and task steps

| Key | Action |
|---|---|
| `[` / `]` | Scroll service **or** task metadata preview panel |

### DynamoDB query results

| Key | Action |
|---|---|
| Mouse wheel | Scroll results (requires terminal mouse reporting) |
| `[` / `]` | Scroll |
| `c` / `y` | Copy full JSON to clipboard (OSC 52 fallback if OS copy fails) |
| `e` | Edit keys (back one step in PK/SK flow) |
| `r` | New query (same table, clear PK/SK) |
| `b` | Back to table list |
| `Esc` | Exit and print last JSON |

### Choose backend

| Key | Action |
|---|---|
| `b` | Exit wizard from first post-auth screen |
