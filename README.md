# sup

A small launcher for project sandboxes. The sandbox is a long-lived machine for
one project; coding-agent sessions and task worktrees live inside it.

```sh
sup docker/docs --native               # Clone inside a sandbox and open Codex
sup docker-docs                       # Reopen the saved project
sup open docker-docs --agent claude
sup open docker-docs --via ssh
sup open docker-docs --via herdr
sup stop docker-docs
sup recreate docker-docs --force       # New machine, same project history
sup rm docker-docs                     # Remove the machine; keep history/setup
sup ls
```

For a fresh installation, `--native` is optional. If you have an existing Lua
configuration, use it for your first native project; saved native projects are
recognized automatically. Lua configurations and snapshots remain supported
through the [legacy interface](docs/legacy.md), including `--config PATH`.

## Install

Build on macOS or Linux with Go 1.23+:

```sh
go build -o bin/sup ./cmd/sup
# Or install this checkout:
go install ./cmd/sup
```

At runtime you need Docker Sandboxes with native `sbx env` support (tested with
v0.45.1). The bundled environment uses [dvdksn/kit](https://github.com/dvdksn/kit):
a shell workload, Codex, Claude, GitHub cloning, SSH signing, and Markdown tools.
Authenticate `gh` on the host for the GitHub secret source and load your SSH key
for the kit's SSH capabilities. SBX handles credential selection and plan approval.
The kit images must have been published before first use; a branch build can be
selected with `--env-arg revision=FULL_KIT_COMMIT_SHA`.

The compiled binary is the only sup runtime file you need. Keep it at its
installed path while creating projects: the generated SBX post-create hook calls
that binary to attach history. Every open regenerates the hook for the current
binary location and checks history again.

## Configure with native SBX files

Put your preferred environment at `~/.config/sup/sbxenv.yaml`, or use one or more
explicit files:

```sh
sup docker/docs --native --env-file ./sbxenv.yaml -d
sup docker/docs --native --name docs-review --ref feature/docs --plan
sup docker/docs --native --name docs-browser --kit registry.example/browser:latest
sup docker/docs --native --env-arg revision=FULL_KIT_COMMIT_SHA
```

Without a custom file, sup uses its bundled [environment](internal/sup/project.sbxenv.yaml).
Custom files need a `repo` environment argument for the project repository. The
bundled kit clones it to `/home/agent/workspace`; `--cwd` changes the entry
path for a different kit. `--env-arg KEY=VALUE` forwards other native arguments.
`--ref` and `--pr` select an initial checkout and are mutually exclusive. They
share the project's name by default; use `--name` for another project instance.
Use Git worktrees inside the sandbox for ordinary concurrent tasks.

Sup passes your files to SBX in order, followed by a small generated file with
the project name, extra kits, and history hook. SBX owns schema validation,
merging, interpolation, credentials, networking, provisioning, and teardown.
Relative paths in your native files retain their original base directory.
Sup does not interpret or duplicate the environment schema. Custom files may
use any native SBX feature; your choice to add workspace mounts applies normally.
The bundled environment has no host repository mounts.

`--plan` delegates to `sbx env plan` without saving a project or mounting history.
`-d` starts and prepares the machine without opening an agent. `-y` passes native
SBX creation approval; `--force` passes removal approval. Terminal mode defaults
to Codex; `--agent claude` and `--agent shell` select the other entry points.

Sup saves the selected files, their content hashes, arguments, kits, and entry
path. Reopening reuses these inputs. If a file changes, sup asks you to use
`recreate` to apply it. Keep custom files available; they are references, not
copies. `recreate` can take new `--env-file`, `--env-arg`, `--kit`, and `--cwd`
values. It removes the old machine with its original settings, then provisions
the replacement. Explicitly pass `--env-arg ref=` when switching from a saved
ref to a PR, or `--env-arg pr=` for the reverse.

## Automatic project history

Each project gets a private host directory under
`~/.local/share/sup/projects/NAME/history`. Sup mounts that directory at
`/home/agent/project-history` and runs the kit's `sup-history` helper before
opening an agent. The helper connects selected Codex and Claude state paths
and points Codex's SQLite index at the same persistent directory.

Transcripts, archived sessions, conversation indexes, and selected Claude
session/task state survive `rm` and `recreate` automatically. There is no export
or restore step. Agent credentials and configuration remain in the sandbox and
are set up again by the kit. Existing local transcripts or conflicting history
configuration cause setup to fail without hiding them; attachment also stops
if mounting or connecting history fails. Retrying the project reruns this setup.

```sh
sup history path docker-docs
sup stop docker-docs
sup history clear docker-docs --yes    # The only command that deletes history
```

This is conversation persistence, not a backup of your repository. Push commits
and preserve any uncommitted files you need before removal or recreation.
A resumed conversation still depends on its agent version and referenced files.
The host history directory is a writable exception to the sandbox boundary;
`--no-history` on creation disables it for a fully isolated project. A custom
environment must include the `project-history` mixin unless history is disabled.
Desktop clients may retain additional conversation metadata outside the agents'
state directories; sup does not manage that frontend state.

## Herdr

`--via herdr` sets up SBX SSH access, uses native `herdr machine add` to prepare
and register `NAME.sbx`, starts the remote server, installs Herdr's supported
Codex/Claude integrations inside the sandbox, and creates a project workspace
at the saved entry directory. It preserves an explicit remote shell preference
and otherwise uses the sandbox user's login shell.

Select that machine in your existing Herdr window, or launch `herdr`, then run
agents in its panes. Herdr's remote server sees those sessions directly; no
sandbox-detection plugin is needed. Agents opened through a separate terminal
or desktop client are outside Herdr's panes. Stopping disables the saved machine;
reopening enables it. Removal forgets its runtime profile after confirmed SBX
teardown, so recreation can prepare a fresh remote installation. The initial
remote installation may ask for Herdr's own approval in your terminal.

Herdr is optional and only required after choosing that frontend (tested with
0.9.3). `HERDR_BIN_PATH` can select its host executable. This wrapper is standalone;
you do not need the experimental sup Herdr plugin or WSP. Those can be added
inside the sandbox later through your native kit configuration.

## State and completion

`~/.local/state/sup/NAME/project.json` holds the durable project record;
`project.sbxenv.yaml` is its generated native overlay. `sup inspect NAME` prints
the record as JSON. `sup ls --json` includes live status. If SBX is unavailable,
listing retains the saved rows with unknown status and exits nonzero.
`XDG_CONFIG_HOME`, `XDG_STATE_HOME`, and `XDG_DATA_HOME` override the standard
locations. Records and history are private; per-project locks serialize changes.
SBX IDs are verified before operating on an existing machine, so an unrelated
sandbox with the same name is never adopted. A machine deleted outside sup
requires explicit `recreate`.

Load Bash completion with:

```bash
eval "$(sup completion bash)"
```

Completion reads saved project/legacy names without contacting SBX or loading Lua.

## Development

```sh
go test -race ./...
go vet ./...
go build -o bin/sup ./cmd/sup
sh tests/integration.sh
SUP=./bin/sup python3 tests/native.py
```

Go and legacy integration tests cover the compatibility interface. Native CLI
tests exercise creation, history hooks, failure recovery, identity checks,
removal cancellation, recreation, SSH, and Herdr across process boundaries.
These tests use fake runtimes and do not create real sandboxes.

MIT. See [LICENSE](LICENSE).
