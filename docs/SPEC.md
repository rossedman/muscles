# muscles — Specification

> One terminal to see your whole engineering life, and a clear answer to
> **"what should I do next?"**

| | |
|---|---|
| **Status** | Draft v0.1 |
| **Author** | Ross Edman |
| **Last updated** | 2026-10-20 |
| **Language** | Go (1.23+) |
| **Core libraries** | Cobra, Viper, Bubble Tea, Bubbles, Lip Gloss, Glamour, Huh, charmbracelet/log |

---

## 1. Problem

An engineer's work is scattered across a dozen tools. On any given morning the
real work queue is split between:

- **GitHub** — review requests, my PRs with failing CI or requested changes,
  mentions, security alerts.
- **JIRA** — sprint tickets, due dates, blockers, comments waiting on me.
- **PagerDuty** — whether I'm on call, open incidents, upcoming shifts.
- **Grafana** — firing alerts, SLO burn, dashboards for things I own.
- **AWS** — CloudWatch alarms, Health events, cost anomalies, security
  findings, expiring certificates.

Each tool has its own inbox, its own idea of urgency and its own notification
noise. Deciding what to do next means opening five tabs and doing the
prioritisation in your head, many times a day. Context switching is the tax,
and the important-but-quiet work (the PR a teammate has waited two days to get
reviewed) loses to the loud-but-unimportant work.

## 2. Goals

1. **Unified view.** Pull work items, signals and status from every connected
   source into one normalised local model.
2. **"Next" answer.** Rank everything into a short, explainable list of what to
   do next, which adapts to context (on call, focus time, triage).
3. **Fast, keyboard-first TUI.** Opens in under 200 ms from the local cache and
   syncs in the background. Everything is reachable without a mouse.
4. **Scriptable CLI.** Every view has a non-interactive equivalent with
   `--json` output so it composes with shell scripts, status bars and other
   tools.
5. **Explainability.** Every ranked item shows *why* it is ranked where it is.
6. **Safe by default.** Read-only unless write actions are explicitly enabled.
   Secrets never touch the config file in plain text or the logs.
7. **Extensible.** Adding a new source means implementing one Go interface.

## 3. Non-goals

- Replacing the web UIs. `muscles` triages and deep-links; complex editing
  (writing a long JIRA description, reviewing a 2,000-line diff) stays in the
  native tool or `$EDITOR`.
- Team-level or manager reporting. This is a personal tool for one engineer.
  (Team views are a possible later extension; see §15.)
- Being a monitoring system. It surfaces what Grafana, PagerDuty and AWS
  already know; it does not evaluate metrics itself.
- A hosted service. Everything runs locally; there is no backend.

## 4. Personas and core scenarios

**Primary persona:** a senior, staff or platform engineer who writes code,
reviews code, carries a pager on rotation and owns some infrastructure.

| # | Scenario | What `muscles` does |
|---|----------|---------------------|
| S1 | **Morning start.** "What's on my plate?" | `muscles` opens on the *Next* view: top 5–10 items across all sources, plus a header showing on-call status, today's meetings and anything on fire. |
| S2 | **Between meetings.** "I have 25 minutes." | `muscles next --fits 25m` favours small items: reviews of small PRs, replying to comments, merging approved PRs. |
| S3 | **Paged.** An incident fires while on call. | The incident pins to the top, the view switches to *Firefight* mode, and the related Grafana alerts, CloudWatch alarms and recent deploys (merged PRs to that service) appear together. |
| S4 | **Standup.** "What did I do yesterday?" | `muscles standup` generates yesterday / today / blockers from activity across sources, ready to paste. |
| S5 | **Deep work.** "Don't distract me." | `muscles focus PROJ-123` sets the current focus. Ranking suppresses unrelated non-urgent items until the focus is cleared or the timebox ends. |
| S6 | **End of week.** "Did anything fall through the cracks?" | The *Stale* view lists items aging without progress: PRs open over 5 days, tickets with no update in the sprint, review requests ignored. |
| S7 | **Handoff.** On-call shift ends. | `muscles oncall handoff` summarises incidents, acknowledged alerts and open follow-ups from the shift. |

## 5. Concepts

### 5.1 Source (provider)

A connector to an external system (GitHub, JIRA, …). A provider fetches data
and turns it into **Items** and **Signals**, and may expose **Actions**.

### 5.2 Item

A unit of work that a person could do something about. All providers map their
data to one normalised struct:

```go
type Item struct {
    ID          string            // stable, globally unique: "<provider>:<native-id>", e.g. "github:pr:org/repo#123"
    Provider    string            // "github", "jira", "pagerduty", "grafana", "aws"
    Kind        Kind              // see table below
    Title       string
    Summary     string            // short, plain text
    Body        string            // markdown; rendered with Glamour in the detail pane
    URL         string            // deep link to the native UI
    Project     string            // repo, JIRA project, PD service, AWS account, ...
    Labels      []string
    Author      string
    Assignees   []string
    State       string            // provider-native state, e.g. "open", "In Progress", "triggered"
    Severity    Severity          // none, low, medium, high, critical
    Role        Role              // why this is mine: assignee, reviewer, author, mentioned, oncall, owner, watcher
    CreatedAt   time.Time
    UpdatedAt   time.Time
    DueAt       *time.Time        // deadline, SLA breach time, cert expiry, ...
    WaitingSince *time.Time       // when someone started waiting on *me*
    Effort      *time.Duration    // estimate (story points mapped, PR size heuristic, ...)
    Relations   []Relation        // links to other items (PR <-> JIRA key, alert <-> incident)
    Meta        map[string]any    // provider-specific extras, available to rules
}
```

**Item kinds (initial set):**

| Kind | Typical source |
|------|----------------|
| `review_request` | GitHub PR where I'm a requested reviewer |
| `pull_request` | GitHub PR I authored (needs action: CI red, changes requested, conflicts, ready to merge) |
| `issue` | GitHub issue assigned to me |
| `mention` | GitHub / JIRA comment that @-mentions me and has no reply |
| `ticket` | JIRA issue assigned to me |
| `incident` | PagerDuty incident assigned to me or my escalation policy |
| `alert` | Grafana alert or CloudWatch alarm in a firing state on something I own |
| `finding` | AWS Security Hub / GuardDuty / Dependabot / code-scanning finding |
| `expiry` | ACM certificate, secret or access key nearing expiry |
| `cost_anomaly` | AWS Cost Anomaly Detection hit |
| `health_event` | AWS Health event affecting my accounts or regions |

### 5.3 Signal

Context that isn't itself a to-do but changes how items rank and what the
header shows: "I'm on call until 17:00", "meeting in 12 minutes", "sprint ends
Thursday", "deploy freeze active".

```go
type Signal struct {
    Provider string
    Kind     string        // "oncall", "sprint", "calendar_event", "freeze", ...
    Start    time.Time
    End      time.Time
    Data     map[string]any
}
```

### 5.4 Action

Something a provider can do on an item: approve, merge, ack, resolve,
transition, comment, snooze, open in the browser. Actions are declared per item
and are **disabled unless writes are enabled** for that provider (§11).

### 5.5 Mode

A global context that changes the ranking weights:

| Mode | Triggered by | Effect |
|------|--------------|--------|
| `normal` | default | Balanced weights. |
| `firefight` | Automatically when I'm on call and an incident assigned to me is `triggered` or `acknowledged`; or manually | Incidents and related alerts pinned; everything below severity `high` hidden. |
| `focus` | `muscles focus <item>` | Items related to the focus item boosted; unrelated items below `high` hidden; ends after the timebox (default 90m). |
| `triage` | Manually (`t` in TUI) | Ranking by staleness and "someone is waiting"; designed to clear the queue fast. |

## 6. Prioritisation engine ("what next")

This is the heart of the product. It must be **deterministic, explainable and
tunable**. No machine learning in v1.

### 6.1 Score

Each item gets a score from 0 to 100 made of weighted factors. Every factor
records a short reason string, which the TUI shows in the detail pane and
`--explain` prints.

```
score = clamp(0, 100,
          w_sev    * severity
        + w_urg    * urgency
        + w_block  * blocking_others
        + w_role   * ownership
        + w_age    * staleness
        + w_focus  * focus_alignment
        + w_fit    * time_fit
        + rule_adjustments)
```

| Factor | Range | How it's computed |
|--------|-------|-------------------|
| **severity** | 0–1 | `critical=1, high=0.75, medium=0.5, low=0.25, none=0`. Incidents use PD urgency/priority; alerts use labels; tickets use JIRA priority. |
| **urgency** | 0–1 | Time pressure: `1 - (time_until_due / horizon)`. Overdue = 1. Horizon is 7 days by default; 1 hour for SLA-type deadlines (incident ack timeout). |
| **blocking_others** | 0–1 | Someone is waiting on me: a review request, a question in a comment with no reply, a ticket marked as blocking others. Grows with `WaitingSince` (saturates at 2 business days). |
| **ownership** | 0–1 | `assignee/oncall=1, reviewer=0.8, author=0.7, owner=0.6, mentioned=0.5, watcher=0.2`. |
| **staleness** | 0–1 | Time since last progress relative to what's expected for that kind. Pushes forgotten work up. |
| **focus_alignment** | −1–1 | +1 if related to the current focus (same JIRA epic, linked PR, same repo); −1 for unrelated items while in `focus` mode. |
| **time_fit** | 0–1 | Only when `--fits <duration>` is given or the next calendar event is close: favours items whose `Effort` fits the gap. |

Default weights (`normal` mode):

| `w_sev` | `w_urg` | `w_block` | `w_role` | `w_age` | `w_focus` | `w_fit` |
|---|---|---|---|---|---|---|
| 30 | 20 | 20 | 10 | 10 | 10 | 10 |

Each mode ships its own weight preset, and the user can override any weight in
config.

### 6.2 Built-in heuristics

These are encoded as default rules (§6.3) so they can be read and changed:

- My PR with **CI failing** or **changes requested**: treat as `blocking_others`
  (it blocks my own delivery and the reviewers' context decays).
- My PR **approved and green**: high fit, low effort ("merge it, 1 minute").
- **Review request** for a PR under 100 changed lines: `Effort = 10m`; under
  500: `30m`; otherwise `1h+`.
- JIRA ticket **in the active sprint** with the sprint ending within 2 days:
  urgency boost.
- **Draft PRs** by others: excluded from review requests.
- **Bot PRs** (Dependabot, Renovate): severity `low` unless they fix a security
  advisory.
- Alerts that are **already linked to an open incident** collapse under the
  incident instead of appearing separately.
- Items **snoozed** by me are hidden until the snooze ends or the item changes
  (new comment, state change, severity increase).

### 6.3 User rules

Rules are written in YAML using [`expr`](https://github.com/expr-lang/expr)
expressions over the `Item` and the current `Signals`. They run after the base
score and can boost, cap, hide or change fields.

```yaml
rules:
  - name: core-repos-matter-more
    when: provider == "github" && project startsWith "acme/core-"
    boost: 15

  - name: ignore-docs-reviews
    when: kind == "review_request" && all(labels, # == "docs")
    boost: -20

  - name: prod-alerts-are-critical
    when: kind == "alert" && meta.env == "prod"
    set:
      severity: critical

  - name: hide-staging-cost-noise
    when: kind == "cost_anomaly" && meta.account_alias == "staging"
    hide: true
```

`muscles rules test` runs every rule against the current cache and prints which
items each rule touched, so rules can be debugged safely.

### 6.4 Grouping and de-duplication

The engine links items across providers with **relations**:

- A PR whose branch, title or body contains a JIRA key (`PROJ-123`) is linked
  to that ticket. The ticket and PR show as one group; the group ranks by its
  highest-scoring member.
- PagerDuty incidents link to Grafana alerts and CloudWatch alarms via the
  alert payload (alert UID, alarm ARN) or a configured service map.
- Service ownership comes from an optional `services` map in config (§9) or a
  `catalog-info.yaml` / CODEOWNERS file, linking repos, PD services, Grafana
  folders and AWS resource tags.

## 7. Providers

Each provider declares its **capabilities** and what it produces. v1 ships the
five providers below; each can be disabled.

### 7.1 GitHub

- **Auth:** fine-grained PAT or `gh auth token` passthrough (default: reuse the
  `gh` CLI's token if present). GitHub Enterprise via `base_url`.
- **API:** GraphQL v4 for PR / review data (one query per sync for search
  results), REST for notifications.
- **Produces:**
  - `review_request`: `is:pr is:open review-requested:@me -is:draft`
  - `pull_request`: `is:pr is:open author:@me`, with check-run state, review
    decision, mergeability, unresolved threads
  - `issue`: `is:issue is:open assignee:@me`
  - `mention`: unread notifications with reason `mention` / `team_mention`
  - `finding`: Dependabot and code-scanning alerts on repos in `github.repos`
- **Signals:** none.
- **Actions:** approve, request changes (opens `$EDITOR` for the body),
  comment, merge (respecting the repo's merge method), re-run failed checks,
  mark notification done, open in browser, check out the branch locally
  (`gh pr checkout`).

### 7.2 JIRA (Cloud and Data Center)

- **Auth:** email + API token (Cloud) or PAT (Data Center).
- **API:** REST v3 / Agile REST.
- **Produces:**
  - `ticket`: configurable JQL, default
    `assignee = currentUser() AND statusCategory != Done ORDER BY priority DESC`
  - `mention`: issues where I'm mentioned in a comment updated in the last 7
    days with no later comment from me
- **Signals:** active sprint (name, start, end, goal) for configured boards.
- **Actions:** transition (pick from the allowed transitions), comment, assign
  to me, log work, open in browser, create a branch named after the ticket.
- **Mapping:** JIRA priority → severity; due date → `DueAt`; story points →
  `Effort` (configurable points-to-hours ratio); "Flagged" → `blocking_others`.

### 7.3 PagerDuty

- **Auth:** user API token (REST v2).
- **Produces:**
  - `incident`: incidents with status `triggered` / `acknowledged` assigned to
    me or to escalation policies I'm on, plus (in the *On-call* view) resolved
    incidents from the current shift
- **Signals:** current and next on-call shifts (from `/oncalls`), including
  escalation level and schedule name. Drives `firefight` mode.
- **Actions:** acknowledge, resolve, reassign, add a note, escalate, snooze,
  open in browser.
- **Mapping:** urgency `high` → severity `critical`; ack timeout → `DueAt`.

### 7.4 Grafana

- **Auth:** service account token per Grafana instance. Several instances are
  supported (e.g. prod and non-prod stacks).
- **API:** unified alerting (`/api/prometheus/grafana/api/v1/alerts` and the
  Alertmanager API), search API for dashboards.
- **Produces:**
  - `alert`: firing alerts filtered by label matchers in config (e.g.
    `team=platform`), or by folders I own
- **Signals:** active silences; SLO burn rate (Grafana SLO plugin, optional).
- **Actions:** silence for a duration (with a comment), open the alert rule,
  open linked dashboards / panels (from alert annotations), open Explore with
  the alert query.

### 7.5 AWS

- **Auth:** standard AWS SDK v2 credential chain. Multiple accounts via named
  profiles listed in config, including SSO profiles (`aws sso login` is
  required first; `muscles doctor` detects an expired SSO session).
- **Produces:**
  - `alert`: CloudWatch alarms in `ALARM` state (filtered by tag or name
    prefix)
  - `health_event`: AWS Health open/upcoming events (scheduled maintenance,
    deprecations)
  - `cost_anomaly`: Cost Anomaly Detection results in the last 7 days
  - `finding`: Security Hub findings with severity ≥ `HIGH` and workflow
    status `NEW` (optional, needs a delegated admin or per-account access)
  - `expiry`: ACM certificates expiring within 30 days; IAM access keys older
    than 90 days belonging to me
- **Signals:** month-to-date spend vs. forecast per account (shown in the
  *Infra* view header).
- **Actions:** open in the console (federated SSO URL where possible), copy the
  ARN. No write actions in v1.
- **Cost control:** Cost Explorer calls are charged per request; they're cached
  for 6 hours by default and only run on explicit sync of that provider or once
  per day.

### 7.6 Later providers (out of scope for v1, interface must allow them)

Google Calendar / Outlook (meeting signals and `time_fit`; high priority for
v1.1), Slack (saved items, unanswered DMs), Linear, Sentry, Datadog,
Opsgenie, Kubernetes (failing workloads in owned namespaces), Argo CD / deploy
status.

## 8. Architecture

### 8.1 Overview

```
┌──────────────────────────────────────────────────────────────┐
│                        cmd/ (Cobra)                          │
│   root(TUI)  next  today  standup  sync  oncall  focus ...   │
└──────────────┬─────────────────────────────┬─────────────────┘
               │                             │
     ┌─────────▼─────────┐         ┌─────────▼─────────┐
     │   internal/tui    │         │  internal/output  │
     │   (Bubble Tea)    │         │ (table/json/md)   │
     └─────────┬─────────┘         └─────────┬─────────┘
               └──────────────┬──────────────┘
                     ┌────────▼────────┐
                     │ internal/engine │  scoring, rules, modes, grouping
                     └────────┬────────┘
                     ┌────────▼────────┐
                     │ internal/store  │  SQLite cache (items, signals,
                     └────────┬────────┘  snoozes, activity, sync state)
                     ┌────────▼────────┐
                     │ internal/sync   │  scheduler, per-provider
                     └────────┬────────┘  intervals, backoff, rate limits
        ┌──────────┬──────────┼──────────┬──────────┐
     github      jira     pagerduty   grafana      aws     internal/providers/*
```

Data flows one way: **providers → sync → store → engine → UI**. The UI never
calls a provider directly except to run an action, and after an action the
affected provider is re-synced for that one item.

### 8.2 Repository layout

```
muscles/
├── main.go
├── cmd/                     # Cobra commands, one file per command
│   ├── root.go              # global flags, Viper binding, launches TUI
│   ├── next.go
│   ├── today.go
│   ├── standup.go
│   ├── sync.go
│   ├── oncall.go
│   ├── focus.go
│   ├── snooze.go
│   ├── open.go
│   ├── auth.go
│   ├── init.go
│   ├── config.go
│   ├── rules.go
│   ├── doctor.go
│   └── daemon.go
├── internal/
│   ├── config/              # Viper setup, schema, validation, defaults
│   ├── secrets/             # keyring / env / command-based secret resolution
│   ├── model/               # Item, Signal, Action, Kind, Severity, Role
│   ├── provider/            # Provider interface, registry, capability flags
│   ├── providers/
│   │   ├── github/
│   │   ├── jira/
│   │   ├── pagerduty/
│   │   ├── grafana/
│   │   └── aws/
│   ├── store/               # SQLite schema, migrations, queries
│   ├── sync/                # scheduler, rate limiting, backoff
│   ├── engine/              # scoring, modes, rules (expr), relations
│   ├── activity/            # activity log for standup / handoff
│   ├── output/              # table, json, markdown renderers for CLI
│   └── tui/
│       ├── app.go           # root model, routing between views
│       ├── theme/           # Lip Gloss styles, adaptive light/dark
│       ├── keys/            # key.Binding maps, help
│       ├── components/      # header, statusbar, itemlist, detail, palette
│       └── views/           # next, inbox, reviews, work, oncall, infra, stale, standup
├── docs/
│   └── SPEC.md
└── testdata/                # recorded HTTP fixtures per provider
```

### 8.3 Provider interface

```go
package provider

type Capability string

const (
    CapItems   Capability = "items"
    CapSignals Capability = "signals"
    CapActions Capability = "actions"
)

type Provider interface {
    // Name is the stable identifier used in config and item IDs.
    Name() string

    // Configure receives the provider's config subtree and resolved secrets.
    Configure(cfg Config, secrets SecretResolver) error

    // Check verifies connectivity and credentials (used by `muscles doctor`).
    Check(ctx context.Context) (Identity, error)

    Capabilities() []Capability

    // Sync returns the full current set of items and signals owned by this
    // provider. The store diffs against the previous set, so providers don't
    // need to track deletions. `since` lets providers do incremental fetches
    // where the API supports it.
    Sync(ctx context.Context, since time.Time) (SyncResult, error)

    // Actions lists the actions available for an item in its current state.
    Actions(item model.Item) []model.Action

    // Do performs an action. Implementations must be idempotent where the
    // underlying API allows (e.g. acknowledging twice is not an error).
    Do(ctx context.Context, item model.Item, action model.ActionID, input ActionInput) error
}

type SyncResult struct {
    Items   []model.Item
    Signals []model.Signal
    Partial bool          // true if some sub-fetch failed; store keeps stale rows for that part
    Errors  []error
}
```

Providers register themselves in an `init()` into a registry, so adding a
provider is: new package + blank import in `internal/providers/all.go`.

### 8.4 Local store

- **Engine:** SQLite via `modernc.org/sqlite` (pure Go, no CGO, so cross
  compilation stays simple).
- **Location:** `$XDG_DATA_HOME/muscles/muscles.db` (`~/.local/share/...` on
  Linux, `~/Library/Application Support/muscles` on macOS), file mode `0600`.
- **Tables:** `items`, `item_history` (state transitions, for activity and
  staleness), `signals`, `snoozes`, `dismissals`, `focus`, `sync_state`
  (per-provider cursor, last success, last error), `activity` (my own actions:
  reviews submitted, tickets transitioned, incidents resolved; captured both
  from sync and from actions taken in `muscles`).
- **Retention:** resolved/closed items are kept 30 days for standup and
  handoff, then pruned.
- **Migrations:** embedded SQL files run at startup (`embed.FS`).

### 8.5 Sync

- Each provider has its own interval (defaults: PagerDuty 1m, Grafana 2m,
  GitHub 3m, JIRA 5m, AWS 15m, cost data 6h).
- `muscles` (TUI) syncs in the background while open; `muscles daemon` keeps
  syncing without a UI and can send desktop notifications.
- Rate-limit aware: honours `Retry-After` / `X-RateLimit-*` headers, and
  backs off exponentially with jitter on errors.
- A failing provider never blocks the others. The header shows a per-provider
  status dot (●) with last sync time; failures show in the status bar and in
  `muscles doctor`.
- A single-instance lock (`flock` on the DB directory) stops two syncers from
  running at once; a second `muscles` process reads the cache and skips
  syncing if the daemon is running.

## 9. Configuration (Viper)

### 9.1 Locations and precedence

1. Command-line flags
2. Environment variables, prefix `MUSCLES_`, with `.` → `_`
   (e.g. `MUSCLES_GITHUB_BASE_URL`)
3. Config file: `--config`, else `$XDG_CONFIG_HOME/muscles/config.yaml`
4. Built-in defaults

`muscles config path`, `muscles config get <key>`, `muscles config set <key>
<value>`, `muscles config edit` (opens `$EDITOR`) and `muscles config validate`
manage it. Viper's `WatchConfig` reloads config in the running TUI.

### 9.2 Example

```yaml
# ~/.config/muscles/config.yaml
me:
  name: Ross Edman
  timezone: America/Chicago
  workday: { start: "09:00", end: "17:30", days: [mon, tue, wed, thu, fri] }

ui:
  theme: auto            # auto | dark | light
  default_view: next
  next_limit: 10

modes:
  focus:
    default_timebox: 90m
  weights:               # override any weight per mode
    normal: { severity: 30, urgency: 20, blocking: 20 }

providers:
  github:
    enabled: true
    token: { from: gh }                  # reuse `gh auth token`
    repos: [acme/core-api, acme/infra]   # for findings; PR/issue search is global
    orgs: [acme]
    actions: { enabled: true }

  jira:
    enabled: true
    base_url: https://acme.atlassian.net
    email: ross@acme.com
    token: { from: keyring }             # stored by `muscles auth jira`
    jql: assignee = currentUser() AND statusCategory != Done
    boards: [42]
    points_to_hours: 4

  pagerduty:
    enabled: true
    token: { from: env, name: PAGERDUTY_TOKEN }
    escalation_policies: [PABC123]

  grafana:
    enabled: true
    instances:
      - name: prod
        url: https://grafana.acme.net
        token: { from: cmd, command: "op read op://work/grafana-prod/token" }
        matchers: ['team="platform"']

  aws:
    enabled: true
    profiles: [acme-prod, acme-staging]
    regions: [us-east-1, us-west-2]
    alarms: { tag: { key: owner, value: platform } }
    cost: { enabled: true }
    security_hub: { enabled: false }

services:                                 # optional ownership map for linking
  - name: core-api
    repos: [acme/core-api]
    pagerduty_service: PSVC123
    grafana_folder: core-api
    aws_tags: { service: core-api }
    jira_components: [API]

rules:
  - name: core-repos-matter-more
    when: provider == "github" && project startsWith "acme/core-"
    boost: 15
```

### 9.3 Secrets

Secrets are **never** stored in `config.yaml` as literal values; the
`config validate` command rejects anything that looks like a token. A secret
reference is one of:

| `from` | Meaning |
|--------|---------|
| `keyring` | OS keychain via `zalando/go-keyring` (macOS Keychain, Secret Service, Windows Credential Manager). Written by `muscles auth <provider>`. |
| `env` | Read an environment variable (`name:`). |
| `cmd` | Run a command and use its stdout (`op`, `pass`, `vault`, …). Cached in memory only. |
| `gh` | GitHub only: `gh auth token`. |
| `aws` | AWS only: the SDK credential chain (implicit). |

## 10. CLI surface (Cobra)

### 10.1 Global flags

| Flag | Description |
|------|-------------|
| `--config <path>` | Config file override |
| `-o, --output <table\|json\|md\|plain>` | Output format for non-interactive commands (default `table` on a TTY, `json` otherwise) |
| `--no-sync` | Use the cache only; never call providers |
| `--provider <name>` | Restrict to one or more providers (repeatable) |
| `--mode <normal\|focus\|firefight\|triage>` | Override the current mode for this command |
| `-v, --verbose` / `--log-level` | Logging via `charmbracelet/log`, to stderr and `$XDG_STATE_HOME/muscles/muscles.log` |
| `--no-color` | Also respects `NO_COLOR` |

### 10.2 Commands

| Command | Description |
|---------|-------------|
| `muscles` | Launch the TUI on the default view. |
| `muscles next [-n 5] [--fits 30m] [--explain]` | Print the top-ranked items. `--explain` shows the score breakdown. Exits 0; prints nothing extra, so it's safe in scripts and status bars. |
| `muscles today` | Morning brief: on-call status, meetings (when calendar is added), sprint status, top items, anything firing. |
| `muscles inbox [--kind review_request] [--provider github]` | All open items, filterable. |
| `muscles standup [--since yesterday] [--copy]` | Yesterday / Today / Blockers generated from activity and the current top items. `--copy` puts it on the clipboard. |
| `muscles oncall [status\|next\|handoff]` | Current/next shift; `handoff` summarises the shift (incidents, resolutions, open follow-ups). |
| `muscles focus <item-id\|JIRA-KEY> [--for 90m]` / `muscles focus clear` | Set or clear focus mode. |
| `muscles snooze <item-id> <duration\|until>` | Hide an item until a time, or until it changes. |
| `muscles done <item-id>` | Dismiss locally (does not change the source). |
| `muscles open <item-id>` | Open the item's URL in the browser. |
| `muscles act <item-id> <action> [--yes]` | Run a provider action from the CLI (e.g. `muscles act github:pr:acme/api#12 approve`). Asks for confirmation unless `--yes`. |
| `muscles sync [--provider x] [--full]` | Sync now; shows a progress spinner per provider. |
| `muscles daemon [--notify]` | Background sync with optional desktop notifications for new high/critical items. |
| `muscles init` | Interactive setup wizard (Huh forms): pick providers, enter URLs, store tokens in the keyring, run `doctor`. |
| `muscles auth <provider>` / `muscles auth status` | Store or check credentials. |
| `muscles doctor` | Check config, credentials, connectivity, rate-limit headroom, SSO session expiry, DB health. |
| `muscles rules [list\|test]` | Show rules and dry-run them against the cache. |
| `muscles config [path\|get\|set\|edit\|validate]` | Manage configuration. |
| `muscles completion [bash\|zsh\|fish\|powershell]` | Shell completion (Cobra built-in), including dynamic completion of item IDs and JIRA keys from the cache. |
| `muscles version` | Version, commit, build date. |

### 10.3 Exit codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | General error |
| 2 | Usage / config error |
| 3 | One or more providers failed (partial results were still printed) |
| 4 | Authentication failure |

### 10.4 JSON output

Every non-interactive command supports `-o json`, emitting a stable,
versioned schema (`"schema": "muscles/v1"`) so scripts don't break across
releases. Example for `muscles next -n 1 -o json --explain`:

```json
{
  "schema": "muscles/v1",
  "generated_at": "2026-10-20T09:02:11-05:00",
  "mode": "normal",
  "items": [
    {
      "id": "github:pr:acme/core-api#481",
      "kind": "review_request",
      "title": "Add retry budget to payment client",
      "url": "https://github.com/acme/core-api/pull/481",
      "score": 82,
      "reasons": [
        "severity medium (+15)",
        "Jane has waited 1d 4h for your review (+18)",
        "small PR, ~10m (+8)",
        "linked to PROJ-212 in active sprint (+6)",
        "rule core-repos-matter-more (+15)"
      ]
    }
  ]
}
```

## 11. Safety and write actions

- **Read-only by default.** Each provider's `actions.enabled` defaults to
  `false`. When disabled, actions are shown greyed out with a hint on how to
  enable them.
- **Confirmation.** Destructive or outward-facing actions (merge, resolve,
  transition, comment, silence) require confirmation in the TUI (a Huh confirm
  dialog showing exactly what will be sent) and `--yes` on the CLI.
- **Least privilege.** The docs list the minimal token scopes per provider,
  separately for read-only and read-write.
- **Secrets hygiene.** Tokens are redacted from logs and error messages; the
  HTTP client's debug logging strips `Authorization` headers.
- **Audit.** Every action taken through `muscles` is written to the `activity`
  table with timestamp, target and result.

## 12. TUI design (Charm)

### 12.1 Libraries

| Library | Use |
|---------|-----|
| **Bubble Tea** | Application framework (Elm architecture). Target the current stable major at implementation time. |
| **Bubbles** | `list`, `table`, `viewport`, `textinput`, `spinner`, `help`, `key`, `paginator`, `progress` |
| **Lip Gloss** | Layout and styles; adaptive colours for light/dark terminals |
| **Glamour** | Rendering markdown bodies (PR descriptions, JIRA descriptions converted from ADF, incident notes) |
| **Huh** | `muscles init` wizard, confirmation dialogs, action input forms (comment body, transition picker, silence duration) |
| **charmbracelet/log** | Structured logging |

### 12.2 Layout

```
┌ muscles ─────────────────────────────────────────────────────────────────────┐
│ ● gh 1m  ● jira 3m  ● pd 20s  ● graf 1m  ◐ aws syncing   │ ON CALL until 17:00 │
│ mode: normal │ sprint "Oct-B" ends Thu │ 2 alerts firing │ next mtg 10:30 (28m) │
├──────────────┬───────────────────────────────────────────────────────────────┤
│ ▸ Next    7  │  #  SCORE  SRC   KIND     TITLE                          AGE   │
│   Inbox  34  │  1   91    pd    incident [P1] core-api 5xx > 2%          4m   │
│   Reviews 5  │     └ graf alert   HighErrorRate core-api              6m    │
│   My Work 9  │  2   82    gh    review   Add retry budget to payment…  1d    │
│   On-call    │  3   74    gh    my PR    ✗ CI failing: infra#88        3h    │
│   Infra   4  │  4   61    jira  ticket   PROJ-212 Payment retries      2d    │
│   Stale   6  │  5   55    gh    my PR    ✓ approved, ready to merge    20m   │
│   Standup    │  6   40    aws   expiry   cert api.acme.net in 9 days   —     │
│              ├───────────────────────────────────────────────────────────────┤
│              │ Add retry budget to payment client          acme/core-api#481 │
│              │ by jane · +84 −12 · 3 files · requested 1d 4h ago             │
│              │ Why #2: Jane waiting 1d 4h (+18) · small (~10m) (+8) · …      │
│              │ ───────────────────────────────────────────────────────────── │
│              │ (Glamour-rendered PR description …)                           │
├──────────────┴───────────────────────────────────────────────────────────────┤
│ ↑/↓ move  enter open  a actions  s snooze  f focus  / filter  ? help  q quit │
└──────────────────────────────────────────────────────────────────────────────┘
```

- **Header:** per-provider sync status, on-call state, mode, sprint, firing
  alert count, next meeting (once calendar exists).
- **Sidebar:** views with counts. Collapses below 100 columns; switch views
  with `1`–`8` or `tab`.
- **Main list:** ranked items with grouping (child rows for related items).
- **Detail pane:** below the list on narrow terminals, to the right on wide
  ones (≥ 160 columns). Shows metadata, the "Why" score breakdown and the
  rendered body.
- **Footer:** context-sensitive key help (Bubbles `help`); `?` expands it.

### 12.3 Views

| View | Contents |
|------|----------|
| **Next** | Ranked top N with reasons. The default. |
| **Inbox** | Everything open, sortable by score / age / source, filterable. |
| **Reviews** | PRs waiting on my review + my PRs waiting on others, side by side. |
| **My Work** | JIRA sprint board (columns by status category) plus linked PRs. |
| **On-call** | Current/next shift, open incidents, incidents this shift, related alerts, recent deploys to affected services. |
| **Infra** | Firing Grafana alerts, CloudWatch alarms, AWS Health events, cost vs. forecast per account, expiries, findings. |
| **Stale** | Items aging without progress, oldest first. |
| **Standup** | Generated standup text in a viewport, editable before copying. |

### 12.4 Key bindings (defaults, remappable in config)

| Key | Action |
|-----|--------|
| `j`/`k`, `↑`/`↓` | Move |
| `g`/`G` | Top / bottom |
| `enter` | Expand detail / open group |
| `o` | Open in browser |
| `a` | Action menu for the item |
| `s` | Snooze (pick duration) |
| `d` | Dismiss locally |
| `f` | Focus on this item |
| `t` | Toggle triage mode |
| `/` | Filter (fuzzy across title, project, labels) |
| `:` or `ctrl+k` | Command palette (any command or view) |
| `r` | Sync now |
| `e` | Explain score |
| `y` | Copy URL / ID |
| `1`–`8`, `tab` | Switch view |
| `?` | Help |
| `q`, `ctrl+c` | Quit |

### 12.5 Behaviour

- **Instant start:** render from cache first, then show a spinner per provider
  while syncing. Items update in place without moving the cursor off the
  selected item.
- **New-item highlight:** items that arrived since the last view show a `•`
  marker for one sync cycle.
- **Firefight transition:** when an incident triggers while the TUI is open,
  the header flashes, the mode switches (with a toast that can be undone with
  `u`), and the incident is selected.
- **Accessibility:** works without colour (`--no-color`); status is never
  conveyed by colour alone (✓ ✗ ● ◐ glyphs plus text).
- **Resizing:** layout recalculates on `tea.WindowSizeMsg`; minimum supported
  size 80×24.

## 13. Non-functional requirements

| Area | Requirement |
|------|-------------|
| Startup | TUI first paint < 200 ms with a warm cache; `muscles next --no-sync` < 100 ms. |
| Sync | Full sync of all five providers < 10 s on a typical account. |
| Footprint | < 100 MB RSS during sync; DB < 50 MB after 30 days. |
| Platforms | macOS (arm64, amd64), Linux (arm64, amd64). Windows best-effort. |
| Distribution | Single static binary via GoReleaser: GitHub Releases, Homebrew tap, `go install`. |
| Offline | Fully usable read-only from cache with no network; header shows "offline". |
| Privacy | No telemetry. No data leaves the machine except calls to the configured providers. |

## 14. Testing strategy

- **Providers:** HTTP interactions recorded with `go-vcr` into `testdata/`,
  with tokens scrubbed. Each provider has golden tests mapping fixtures to
  `[]model.Item`.
- **Engine:** table-driven tests for every factor, each mode, and rule
  evaluation; property tests that scores stay within 0–100 and ordering is
  stable for equal scores.
- **Store:** migration tests from every previous schema version.
- **TUI:** `teatest` golden-file tests for each view at 80×24 and 160×48;
  model update tests for key handling.
- **CLI:** JSON output snapshot tests to protect the `muscles/v1` schema.
- **CI:** `go vet`, `staticcheck`, `golangci-lint`, `go test -race`, GoReleaser
  snapshot build on every PR.

## 15. Milestones

| Milestone | Scope | Outcome |
|-----------|-------|---------|
| **M0 — Skeleton** | Cobra/Viper wiring, config + secrets, SQLite store and migrations, provider interface, `doctor`, `init` stub, logging, CI, GoReleaser | `muscles doctor` runs |
| **M1 — GitHub + Next** | GitHub provider (read), engine with base scoring, `next`, `inbox`, TUI shell with Next/Inbox/Reviews views, detail pane | Useful day-one tool for code review |
| **M2 — JIRA + PagerDuty** | Both providers (read), PR↔ticket linking, sprint and on-call signals, My Work and On-call views, firefight mode | Covers planned + unplanned work |
| **M3 — Grafana + AWS** | Both providers (read), alert↔incident linking, services map, Infra view | Full v1 source coverage |
| **M4 — Act and reflect** | Write actions with confirmations, snooze/dismiss/focus, user rules + `rules test`, `standup`, `oncall handoff`, Stale view, daemon + notifications | **v1.0** |
| **M5 — Context** | Calendar provider, `time_fit`, `--fits`, `today` brief with meetings | v1.1 |
| **M6 — Optional AI layer** | Opt-in LLM summaries: standup prose, incident handoff narrative, "summarise this PR thread". Off by default, explicit config, shows exactly what is sent. | v1.2 |

Possible later work: team view (what my team is blocked on), more providers
(§7.6), a plugin protocol for out-of-tree providers (subprocess over JSON-RPC,
in the style of Terraform/HashiCorp `go-plugin`).

## 16. Success metrics (personal)

- Number of browser tabs opened to triage each morning: from ~5 to 0–1.
- Median time a teammate waits for my review: down 50% within a month.
- "Fell through the cracks" items found in the weekly Stale review: trending
  down.
- I trust the top 3 of `muscles next` enough to just start on #1 most mornings.

## 17. Open questions

1. **Ownership source of truth:** maintain the `services` map by hand, or read
   it from an existing catalog (Backstage `catalog-info.yaml`, CODEOWNERS, AWS
   tags)? Proposal: support all three, with config overriding.
2. **JIRA description format:** Atlassian Document Format → Markdown
   conversion quality varies; is a "good enough" converter acceptable, with
   "open in browser" as the fallback?
3. **Multiple identities:** support separate work/personal GitHub accounts in
   one config (profiles), or keep one identity per config file and use
   `--config`?
4. **Effort estimates:** are PR size and story points good enough proxies, or
   should `muscles` learn from my actual time-to-complete history (kept
   local) in a later version?
5. **Notifications:** desktop notifications from the daemon only, or also a
   terminal bell / tmux status integration (`muscles next -n 1 -o plain` in
   the tmux status line)?
6. **Name:** `muscles` as the binary name, with a short alias (`mx`)?
