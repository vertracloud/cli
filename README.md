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
| `vertra image ...` | Docker images: build from a folder or `.zip`, list, inspect, rename and describe, rebuild, build logs, create apps from an image and delete. |
| `vertra template ...` | Templates: browse the catalog, check a manifest folder, use a template (deploy), create private templates and share links. |
| `vertra stack ...` | Stacks (apps, databases and Blob folders created together by a template): list, inspect, rename, update to a newer template version and delete. |
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

### Docker images

```sh
vertra image build --name my-image [--description <text>] [--context <folder|.zip>] [--workspace <id>]
vertra image list [--workspace <id>]
vertra image get <id>
vertra image update <id> [--name <name>] [--description <text>]
vertra image rename <id> <name>
vertra image rebuild <id> [--context <folder|.zip>]
vertra image logs <id> [--follow] [--version <id>]
vertra image deploy <image-id> --name my-app --ram 512 [--web] [--port 3000] [--subdomain my-app] [--env KEY=VALUE]
vertra image delete <id> [--yes]
```

`--context` takes a folder or a `.zip` (default: the current folder). A folder is packed for you: `.dockerignore` is honored (patterns, `!` exceptions and comments), the root `Dockerfile` is always kept, `.env*` files are never sent (not even with `!.env`) and symlinks are skipped. A `.zip` is sent as it is, and the server filters out `.env`. The CLI refuses a context over 100 MB before uploading anything. `build` and `rebuild` return as soon as the build starts: follow it with `vertra image logs <id> --follow`. Apps never restart on their own after a rebuild.

`--description` (up to 128 characters) is optional. `image update` renames the image and/or changes its description without building anything (apps keep running); an empty `--description ""` removes the description, and `image rename <id> <name>` is a shortcut for `update --name`. A name already used by another of your images is refused.

`image deploy` creates an app that runs the image; `--port` and `--subdomain` only apply with `--web`, and `--env` can be repeated. `image delete` also deletes the apps created from the image, so without `--yes` it asks you to type the image name (with `--json` or without a terminal it fails with `CONFIRMATION_REQUIRED`).

`vertra commit` on an app created from an image works as on any app; the server decides what happens to the image.

### Templates

```sh
vertra template list [--search <text>] [--category <c>] [--origin vertra|community|private] [--tag <t>] [--fits] [--sort recommended|popular|newest|name] [--page N] [--limit N] [--workspace <id>]
vertra template info <slug|id>
vertra template versions <slug|id>
vertra template categories
vertra template validate [<folder>]
vertra template deploy <slug> --name my-app [--set <question>=<value>] [--service <service>.<name|ram|subdomain>=<value>] [--version <v>] [--folder <id>] [--workspace <id>] [--yes] [--no-wait]
vertra template deploy --share <link|token> --name my-app [...]
vertra template create [<folder>] [--workspace <id>] [--template <id>] [--changelog <text>] [--changelog-en <text>] [--changelog-es <text>]
vertra template share <template-id> [--mode use|full_copy] [--max-uses N] [--expires-days N]
vertra template share list <template-id>
vertra template share revoke <template-id> <link-id>
```

`validate` reads `vertracloud.template.yaml` from the folder (or the file itself) and checks it with the API without creating anything. Every finding is printed with its file, line, manifest path and code; the command exits with a failure status when there is at least one, and with `--json` it prints `{ valid, file, issues: [{ path, code, message, line }] }`.

`deploy` asks the questions of the template in the terminal (secrets are typed without echo). Answer them ahead with `--set <question>=<value>` (repeatable); with `--yes`, with `--json` or without a terminal nothing is asked, a required question without an answer fails the command listing every missing one, and `--name` is required. Questions generated by the platform are not asked. While the job runs, the steps are shown on stderr; at the end the outputs are printed. An output marked as secret or shown once appears only in that run: save it. `--service <service>.ram=2048` (also `.name` and `.subdomain`) sets the choices per service. Each call sends an idempotency key, and `--no-wait` returns as soon as the job starts. `--share` takes the link or the token of a share link instead of a catalog slug; a link in full-copy mode gives a template, not a deploy, so the CLI refuses it there.

`create` packs the folder of every service whose `source` is `type: code` (the `source.path` of the service, relative to the manifest folder and never outside it) and creates a private template, or a new version of the template given with `--template`. Each service is packed as a zip of up to 100 MB: `.vertraignore` is honored (or `.dockerignore` when there is no `.vertraignore`; same pattern rules), `.env*` files, `.git`, `.vertra` and symlinks are never sent. Services that run an image of your account need no zip. If the API finds a secret in the code it lists the files; if it rejects the manifest it prints the same listing as `validate`. `share` creates a link and prints its token once; it cannot be read again, only revoked.

### Stacks

```sh
vertra stack list [--workspace <id>]
vertra stack get <id>
vertra stack update <id> [--check] [--set <question>=<value>] [--ram <service>=<MB>] [--yes] [--no-wait]
vertra stack rename <id> [<name>] [--description <text>]
vertra stack delete <id> [--delete-all] [--blob keep|delete] [--yes]
```

`get` shows the members (applications, databases, Blob folders), their connections and the template the stack came from. `update` moves a stack that came from a template to its latest version: `--check` only shows what would change (new version, security fix, new questions, memory that rose, services added or removed); without it the CLI asks the new questions, asks for confirmation (`--yes` skips it) and follows the job. A safety snapshot is taken first. `rename` changes the name and/or the description (an empty `--description ""` removes it).

`delete` removes only the stack by default: the apps and databases stay as loose resources and Blob files are kept. `--delete-all` also deletes every member; `--blob delete` (only with `--delete-all`) also deletes the Blob files. Without `--yes` it asks you to type the stack name (with `--json` or without a terminal it fails with `CONFIRMATION_REQUIRED`).

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

### Custom domains

```bash
vertra app domains list [<app-id>]
vertra app domains add example.com [--app <id>] [--port 3000] [--path-prefix /api] [--no-www]
vertra app domains add old.com --redirect-to example.com --redirect-status 301
vertra app domains update <domain-id|hostname> [--app <id>] [--port 8080] [--path-prefix /v1] [--redirect-to <domain>] [--redirect-status 308] [--primary]
vertra app domains dns <domain-id|hostname> [--app <id>]   # records to create at your DNS provider
vertra app domains remove <domain-id|hostname> [--app <id>] [--yes]
```

Every command takes the domain id or its hostname. `add` also creates the automatic `www`/root companion unless you pass `--no-www`; it does not count toward the plan limit. `--port` is 1 to 65535, `--redirect-status` is 301, 302, 307 or 308 (default 308), and `--path-prefix` is a literal path such as `/api`. In `update`, an empty `--path-prefix ""` or `--redirect-to ""` clears the value, and `--primary` swaps the domain with its companion as the main one. `remove` asks for confirmation and also removes the companion, unless the domain you remove is itself the companion. `app list --filter domain=shop` matches any domain of the app.

Run `vertra --help` or `vertra <command> --help` for every flag. When an ID is omitted, the CLI uses the linked application or lets you pick one. Destructive commands, plus `app stop`, `db stop` and `app env remove`, ask for confirmation; pass `--yes` in automation. Declining the prompt exits with status 1 and the message "Operation cancelled." (with `--json`, an error with code `CANCELLED` on stderr); it is never reported as success.

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
