# sup

A launcher for long-lived, per-project Docker sandboxes. The repository,
agent sessions, and task worktrees live inside the sandbox.

```sh
sup docker/docs                       # Create or reopen the project and open Codex
sup docker/docs --agent claude         # Same sandbox, different agent
sup docker/docs --agent shell
sup docker/docs --via ssh
sup docker/docs --via herdr
sup stop docker/docs
sup recreate docker/docs               # New machine, retained conversations
sup rm docker/docs                     # Remove the machine; retain setup/history
sup ls
```

## Install

```sh
go install ./cmd/sup
```

Build with Go 1.23 or newer. Runtime requires Docker Sandboxes with `sbx env`
support (tested with v0.45.1). Sup has no external Go dependencies or Python
runtime dependency.

The embedded environment uses [dvdksn/kit](https://github.com/dvdksn/kit): a shell,
Codex, Claude, GitHub cloning, SSH signing, and rumdl. Authenticate the host's
`gh` and load your SSH key for those capabilities. SBX handles credential
selection; sup automatically approves creation plans. The clone takes only
`owner/repo` and makes a shallow default-branch checkout in
`/home/agent/workspace`. Fetch additional history or branches inside the
sandbox when needed.

The signing mixin requests runtime-provided Git identity. In a live check,
SBX v0.45.1 accepted the required capability but left Git name/email unset
despite a configured host identity. Until the runtime supplies those defaults,
configure them inside the sandbox before committing.

## Embedded environment

The binary embeds [project.sbxenv.yaml](internal/sup/project.sbxenv.yaml). There
is no environment-file lookup or CLI configuration interface. Kits, credentials,
and lifecycle hooks are defined in that template; edit it and rebuild sup to
change the setup. Its kit revision is pinned to a published build.

Sup fills in the repository, project name, and host history location, then writes
one self-contained native SBX environment to its project state directory. SBX
handles schema validation, credentials, provisioning, and teardown. The separate
Claude and Codex artifacts retain the OAuth workaround used by the kit environment.
Sup does not require an environment file from the kits repository.

```sh
sup docker/docs
sup docker/docs --plan
sup docker/docs -d
```

`--plan` delegates to `sbx env plan`. Creation uses
`sbx env run --detached --auto-approve`; reopening uses `sbx env exec` to start
the existing machine. `-d` prepares without opening an agent. `--force` passes
removal approval for `rm` and `recreate`.
`--via` and `--agent` select how to enter the machine for that invocation.

Each repository has one sandbox, named from its owner and repository (for example,
`docker/docs` becomes `docker-docs`). Ambiguous, normalized, or long names get
a short repository hash suffix to distinguish them. Repository names are
case-insensitive; there is no naming override. You can use the repository or saved name to reopen
it or manage its lifecycle.

The rendered environment is retained for the machine's lifetime. Reopening uses
that saved file; recreation renders the environment embedded in the current
binary. Updating sup therefore changes the setup when you recreate the sandbox.

## Conversation persistence

The generated environment declares a `postCreate` hook containing ordinary
shell commands: create the project's host history directory, call `sbx mount`,
and connect selected agent paths with symlinks. The complete hook is visible in
SBX's plan and the generated file. It uses no callback into sup, installed
helper command, or history kit.

The shared mount keeps Codex sessions and archives on one filesystem. Codex's
SQLite directory and selected Claude conversation/task state also persist.
Authentication and agent configuration stay in the sandbox and are rebuilt
by the kit. Existing local conversations are never silently hidden.

Mounts survive stop/start through SBX. Removing or recreating the machine keeps
history automatically; there is no export or restore step. If a creation hook
fails, sup prevents attachment until `recreate` reruns creation successfully.

```sh
sup history path docker/docs
sup stop docker/docs
sup history clear docker/docs --yes
```

Clearing requires a stopped or removed machine. It removes history files while
retaining the directories and empty history files needed by a stopped machine's
mount and symlinks. Conversation persistence is part of the embedded environment.

History does not back up the repository. Push commits and preserve uncommitted
files before recreation. Desktop clients may keep their own metadata separately.

## Herdr

`--via herdr` registers the sandbox as an SSH machine using Herdr's native remote
installation, installs its Codex/Claude integrations, and creates a workspace at
the project directory. Select that machine in Herdr and run agents in its panes.
Agents started through another frontend are outside those panes.

Stopping disables the machine profile; opening with `--via herdr` enables it.
Removal forgets the profile so recreation can install Herdr into the replacement machine. Herdr is
optional (tested with 0.9.3); its first remote installation may ask for approval.
`HERDR_BIN_PATH` selects its host executable. No sandbox-detection plugin or WSP
integration is required.

## State and development

Project records and the rendered `sbxenv.yaml` live in `~/.local/state/sup/NAME`;
history lives in `~/.local/share/sup/projects/NAME/history`. XDG environment
variables override these locations. `sup inspect NAME` prints the record;
`sup ls --json` adds live status. Sandbox IDs are checked before removal or
reuse so an unrelated same-name machine is never adopted. Bash completion
offers saved repositories as well as sandbox names.

```sh
eval "$(sup completion bash)"
go test -race ./...
go vet ./...
go build -o bin/sup ./cmd/sup
```

Go tests cover lifecycle decisions at the SBX command boundary and execute the
actual shell history hook in temporary directories. Real sandbox checks verify
mount persistence, archive moves, SQLite, and recreation.

MIT. See [LICENSE](LICENSE).
