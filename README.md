<div align="center">
  <a href="https://vertracloud.app">
    <picture>
      <source media="(prefers-color-scheme: dark)" srcset="https://vertracloud.app/brand/github-banner.png">
      <source media="(prefers-color-scheme: light)" srcset="https://vertracloud.app/brand/github-banner-light.png">
      <img src="https://vertracloud.app/brand/github-banner-light.png" alt="Vertra Cloud" width="1200">
    </picture>
  </a>
</div>

# Vertra Cloud CLI

[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

Official command-line tool for [Vertra Cloud](https://vertracloud.app): deploy applications, follow logs live and manage databases, snapshots and workspaces from your terminal.

- Single binary for macOS, Linux and Windows — no Node.js or other dependencies.
- Human-friendly tables by default, `--json` on every command for scripts and CI.
- English, Portuguese and Spanish.

## Installation

**macOS and Linux**

```bash
curl -fsSL https://cli.vertracloud.app/install | sh
```

**Windows (PowerShell)**

```powershell
irm https://cli.vertracloud.app/install | iex
```

The installer downloads the latest release for your system and processor (x64 or ARM), verifies its SHA-256 checksum and adds `vertra` to your `PATH`. Run it again to update.

| Variable | Effect |
|---|---|
| `VERTRA_VERSION` | Install a specific version, e.g. `v0.2.0`. |
| `VERTRA_INSTALL_DIR` | Install somewhere else. Defaults to `~/.vertracloud/bin` (`%USERPROFILE%\.vertracloud\bin` on Windows), next to your config. |

Prefer to do it by hand? Download the archive for your platform from [Releases](https://github.com/vertracloud/cli/releases), check it against `checksums.txt` and put `vertra` on your `PATH`. The installer scripts are in [`scripts/`](scripts).

## Getting started

```bash
vertra auth login     # paste an API key created in the dashboard
vertra doctor         # check the project before sending it
vertra deploy         # upload the current directory
vertra logs --follow  # follow the output live
```

Your key is stored in `~/.vertracloud/config.json`, readable only by your user. In CI, set `VERTRA_API_KEY` instead; it takes priority over the saved key.

## Commands

| Command | What it does |
|---|---|
| `vertra auth login\|whoami\|logout` | Connect and inspect your account. |
| `vertra app ...` | List, inspect, start, stop, restart, deploy, configure and delete applications; releases and rollback; logs, metrics, environment variables, files and domains. |
| `vertra db ...` | Create and manage databases, credentials and certificates. |
| `vertra projects` | Applications and databases in one table, with live status. |
| `vertra snapshot ...` | List, create, download and restore snapshots. |
| `vertra workspace ...` | Workspaces, members, invites and roles. |
| `vertra folder ...`, `vertra favorite ...` | Organize your resources. |
| `vertra blob ...` | Blob Storage: list, upload (with a progress bar), download, delete, create temporary links and check usage. |
| `vertra link [<id>]`, `vertra unlink` | Tie the current directory to an application. |
| `vertra doctor`, `vertra zip` | Check and package the project locally. |
| `vertra lang <pt\|en\|es>` | Change the CLI language. |
| `vertra status` | Platform status. |

`vertra app commit [<id>] [-m <message>]` sends a commit message with the upload, shown in the deploy history (up to 500 characters). Without `-m`, the CLI uses the subject of the last git commit in the folder, if there is one; otherwise the upload has no message.

### Blob Storage

```bash
vertra blob ls photos/                       # files and folders under a prefix
vertra blob put ./logo.png images/logo.png   # private by default; --public for a public URL
vertra blob put ./logo.png --rename          # if the name is taken, saves as logo-1.png, logo-2.png, ...
vertra blob get images/logo.png ./logo.png   # downloads through a temporary link; never overwrites
vertra blob link 1234567890 --expires 600    # temporary download link (60 to 86400 seconds)
vertra blob rm 1234567890 --yes              # delete by ID
vertra blob stats                            # usage against the plan quota
```

`get` accepts an object ID or the exact path. Large files are sent in parts automatically.

### Releases and rollback

```bash
vertra app releases [<id>] [--page N] [--limit N]   # versions, newest first, and which ones can be restored
vertra app releases log [<id>] <release-id>         # log of a deploy that failed
vertra app rollback [<id>] <release-id> [--env]     # go back to that version and restart
```

`rollback` is available from the Pro plan. It shows what will happen and asks before it runs; pass `--yes` in automation. With `--env` it also restores the environment variables saved with that version: it lists only their names and whether each one would be added, modified or removed, never the values, and needs an API key with the `apps:envs` scope. Without `--env` your current variables stay as they are.

### Deploy behavior

```bash
vertra app config [<id>] --zero-downtime auto|on|off   # on: Enterprise plan
vertra app config [<id>] --auto-rollback on|off        # Intermediary plan and up
vertra app config [<id>] --health-path /healthz        # or --clear-health-path
```

The health path must start with `/`, use visible characters without spaces and have at most 200 characters. `vertra app info` shows the current values.

Run `vertra --help` or `vertra <command> --help` for every flag. When an ID is omitted, the CLI uses the linked application or lets you pick one. Destructive commands ask for confirmation; pass `--yes` in automation.

## Scripting

```bash
VERTRA_API_KEY=*** vertra deploy --json
```

With `--json`, output is the API response and errors keep the API's `code`, so scripts can branch on them. Colors are only used in a real terminal and respect `NO_COLOR`. `VERTRA_LANG` sets the language for a single run.

For GitHub Actions, see [vertracloud/github-action](https://github.com/vertracloud/github-action).

## Building from source

Requires Go 1.22+.

```bash
make build                   # ./bin/vertra
make test                    # go vet + go test
make dist VERSION=v0.2.0     # release archives for every platform in ./dist
```

## License

[MIT](LICENSE)
