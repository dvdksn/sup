# sup

Run coding agents in a Docker sandbox, with one sandbox per GitHub repository.

```sh
sup docker/docs
```

Sup creates the sandbox, clones the repository, and opens Codex. Run the same
command again to return to it.

## Install

From this checkout, with Go 1.23 or newer:

```sh
go install ./cmd/sup
```

Install [Docker Sandboxes](https://docs.docker.com/ai/sandboxes/) and sign in
with `gh` on the host. Load your SSH key for Git signing. SBX manages agent
credentials.

The bundled environment uses [dvdksn/kit](https://github.com/dvdksn/kit), with
Codex, Claude, a shell, GitHub cloning, Git signing, and rumdl.

Allow `ghcr.io/dvdksn/` in SBX's kit publisher settings. Starting from the
default allowlist:

```sh
sbx settings set kit.allowedSources '["docker.io/","ghcr.io/dvdksn/"]'
```

Keep any other publishers you already allow.

## Use

```sh
sup docker/docs --agent claude
sup docker/docs --agent shell
sup docker/docs --via ssh             # Open a shell over SSH
sup docker/docs --via herdr
sup docker/docs -d                    # Prepare without attaching
```

`--via herdr` registers the sandbox as a Herdr machine and selects a workspace
for the repository. Select that machine in Herdr to run agents in its panes.
This works on first creation too; Herdr's installation may ask for approval.
`-d` prepares only the sandbox and cannot be combined with `--via`.

```sh
sup ls
sup stop docker/docs
sup recreate docker/docs
sup rm docker/docs
```

Stopping preserves the sandbox. Recreation replaces it; removal deletes it.
Both discard its files and sessions, so push work you want to keep first.
Sup asks for confirmation before removal or recreation; `--force` skips it.

You can also address an existing project by its sandbox name: `docker-docs`.

```sh
eval "$(sup completion bash)"
```

## Setup and troubleshooting

Sup embeds its [SBX environment](internal/sup/project.sbxenv.yaml) and approves
creation plans automatically. Edit the template and rebuild to change the
setup. Existing sandboxes get the new setup when recreated.
Creation and recreation use the published `:latest` kits; SBX resolves the tags
and pulls missing content. Reopening an existing sandbox keeps its installed kits.

```sh
sup docker/docs --plan               # Preview the SBX plan
sup docker/docs --verbose            # Show full setup output
sup inspect docker/docs              # Show the saved project as JSON
sup ls --json
```

Project records, environments, and setup logs live in `~/.local/state/sup/NAME`.
`XDG_STATE_HOME` overrides the state directory.

## Development

```sh
go test -race ./...
go vet ./...
go build -o bin/sup ./cmd/sup
```

MIT. See [LICENSE](LICENSE).
