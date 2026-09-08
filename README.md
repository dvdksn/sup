# sup

A personal Docker Sandboxes launcher: a Go executable with Lua configuration.

```sh
sup docker/docs                   # Create and attach, or reconnect
sup docker/runtime-kits --kit task
sup docker/docs --pr 12345         # Uses docker-docs-pr-12345
sup docker-docs-pr-12345           # Reconnect using saved configuration
sup docker/docs -d                # Provision without attaching
sup docker/docs --name docs-test --kit browser --plan
```

Repositories are cloned **inside the sandbox** by a kit. No host repository
mounts, per-repository config files, Lua installation, or LuaRocks are needed.

## Build and install

Requirements: macOS/Linux, Go 1.23+ to build, and Docker Sandboxes with `sbx env`
support to run. The example also uses authenticated GitHub CLI (`gh auth token`).

From the checkout:

```sh
go build -o bin/sup ./cmd/sup
./bin/sup --help
# Or install to GOBIN (normally ~/go/bin):
go install ./cmd/sup
```

Only the compiled executable is needed at runtime. The Go module is
`github.com/dvdksn/sup`; once that repository is published, Go users can install
with `go install github.com/dvdksn/sup/cmd/sup@latest`.

Copy the example without replacing an existing config:

```sh
mkdir -p ~/.config/sup
cp -n examples/config.lua ~/.config/sup/config.lua
```

## Lua configuration

`~/.config/sup/config.lua` calls `sup.setup` once:

```lua
local sup = require('sup')

sup.setup({
  kits = {
    browser = 'ghcr.io/dvdksn/browser-kit:codex',
  },
  defaults = function(ctx)
    return {
      agent = 'codex',
      secrets = { github = { command = 'gh auth token' } },
      kits = {
        {
          source = 'git+https://github.com/cdupuis/sbx-kits.git#dir=github-clone',
          args = {
            repo = ctx.repo, ref = ctx.ref, pr = ctx.pr, dir = '/project',
          },
        },
      },
    }
  end,
})
```

See [examples/config.lua](examples/config.lua) for signing and conditional docs
tooling. Aliases work in `defaults`, `repos`, and with `--kit`; full sources work too.
`--kit` appends to the configured kits. Matching source/argument pairs are
deduplicated; conflicting arguments for one source fail. Local kits must use
absolute paths. Sources should be pinned to commits or digests when reproducible
installation matters. Remote sources must also be allowed by sbx's kit allowlist;
`sup` does not change that setting. The example uses GitHub sources under
`cdupuis` and `docker`, and the `ghcr.io/dvdksn/` registry namespace.

`defaults` accepts an environment table or a function for invocation-dependent
values such as clone arguments. Repository-specific additions are declarative:

```lua
repos = {
  ['docker/docs'] = {
    kits = { 'browser', 'vale' },
    sandboxOptions = { memory = '8g' },
  },
},
```

Put `repos` alongside `defaults` in `sup.setup`. Keys match the normalized
`OWNER/REPO` exactly (case-sensitive, no patterns). Composition is:

1. Evaluate `defaults`.
2. Apply the matching `repos` entry: recursively merge mappings, replace scalar
   values, append kits, and replace every other list.
3. Append CLI kits and deduplicate identical source/argument pairs.

An empty kit list adds nothing; it does not remove default kits. An empty list
for another field replaces that list; an empty mapping preserves inherited keys.
Lua `nil` means absent and cannot remove an inherited mapping entry. No deletion
or special replacement syntax is provided. Conflicting arguments for the same
kit remain an error. The final environment is validated after composition.

The `defaults` callback receives:

| Field | Value |
| --- | --- |
| `repo` | `owner/repository`, with a trailing `.git` removed |
| `name` | Explicit `--name`, or the derived repository/PR/ref name |
| `ref` | Ref, or an empty string |
| `pr` | PR number as a string, or an empty string |

It returns native sbxenv fields. `schemaVersion` defaults to `"1"`; `name` defaults
to `ctx.name` and cannot identify a different sandbox. `workspace` and
`additionalWorkspaces` are rejected. The clone behavior is supplied by your kit,
not hardcoded into the launcher. `--ref` and `--pr` are mutually exclusive.

GopherLua embeds Lua 5.1 with some extensions, including `goto`; it is not Lua
5.4 or LuaJIT and does not load native Lua C modules. Standard GopherLua libraries
are available. Config is trusted host code, not a security sandbox. An execution
context limits Lua evaluation to five seconds; it cannot interrupt arbitrary
blocking host-library calls. Config is never discovered in cloned repositories.

For migration, the original `configure(ctx)` callback remains supported, but
cannot be combined with `defaults` or `repos`. A returned config table is also
supported; do not combine it with `sup.setup` in the same file.

## Editor support and validation

[types/sup.lua](types/sup.lua) defines `SupConfig`, `SupContext`, `SupEnvironment`,
and nested native environment types for Lua Language Server. The repository's
`.luarc.json` enables completion and diagnostics for the example.

For your personal config, place a `.luarc.json` alongside it with an absolute
path to the definitions from this checkout:

```json
{
  "runtime.version": "Lua 5.1",
  "workspace.library": ["/absolute/path/to/sup/types"],
  "workspace.checkThirdParty": false
}
```

You can copy the `types` directory alongside your config instead and use
`"./types"`. Runtime validation checks unknown fields and value types, including
nested fields, with errors such as `environment.sandboxOptions.cpus: expected
integer, got string`. sbx remains responsible for final semantic validation,
credentials, kit availability, and approvals.

## Listing and removal

```sh
sup ls
sup rm docker/docs
sup rm docker/docs --pr 123
sup rm docker-docs-pr-123
sup rm docs-review --force
```

`sup ls` lists saved environments with their repository, PR/ref selection, live
status from `sbx ls --json`, and number of kits. Sandboxes created outside sup are
omitted. A saved environment absent from sbx is `missing`. If sbx is unavailable,
saved rows are still shown with `unknown` status, a warning, and a nonzero exit.
No Lua configuration is evaluated. Corrupt saved entries are reported and skipped.

`sup rm` resolves the same names as creation, then runs `sbx env rm` with the
saved environment file. sbx shows its destroy plan and requests confirmation.
`--force` / `-f` explicitly skips confirmation and permits removal while in use.
No global bindings are pruned. Push any work you want to retain first; the clone
lives inside the sandbox.

After successful removal, sup checks that the sandbox is absent and deletes its
two managed state files. Declined or failed removal, or failure to verify absence,
keeps saved state. sbx currently reports a declined removal as exit zero plus
`Aborted.` on stderr; sup recognizes that response as cancellation. An already
missing sandbox can still be removed this way to clean up credentials and saved
state. Unknown names are rejected; sup never evaluates config to create state for
removal. `ls` and `rm` are reserved command names; an existing environment with
one of those names can still be selected using its repo and `--name`.

## State and execution

`sup` generates `~/.local/state/sup/<name>/sbxenv.yaml` and invokes:

```text
sbx env run <generated-file>
sbx env create <generated-file>   # -d
```

The file uses JSON syntax, which sbx accepts as YAML. Go serializes it directly;
no YAML dependency is needed. An explicit path means sbx does not load
`~/.sbxenv.yaml`. Lua is the complete configuration source.

The process is launched directly with an argument array and inherited terminal
streams. No shell command is constructed. `-d` waits for provisioning and exits
without attaching; it does not start a background agent task. sbx exit codes are
propagated.

Each name also has `state.json`, containing its repository and resolved config.
It is saved before provisioning, so failed/cancelled creates can be retried with
identical inputs. Reconnection skips Lua evaluation and reuses that snapshot.
Existing names reject `--kit` and `--config`, even when the supplied value
matches. Repeated `--pr` or `--ref` selections reconnect when they match the saved
selection. A different selection with an existing explicit `--name` is rejected.
Use a new name for a different setup. Old snapshots remain readable; since they
lack selection metadata, reconnect to them by saved name without `--pr`/`--ref`.

Names are derived automatically:

```text
sup docker/docs              -> docker-docs
sup docker/docs --pr 123      -> docker-docs-pr-123
sup docker/docs --ref main    -> docker-docs-ref-main
```

`--name` always overrides this derivation. Unsupported name characters in refs
(such as `/`) become hyphens. Whenever sanitization or truncation is necessary,
a ten-character hash is appended to distinguish the original inputs. Derived
names fit within 100 characters. `ctx.ref` retains the original ref for cloning;
`ctx.name` already contains the final name, so config should not add a suffix.
Repeating the same PR/ref command reuses its snapshot without re-evaluating Lua.
Plain `sup docker/docs` continues to select the base environment.

`--plan` invokes `sbx env plan` without saving a new environment. It evaluates
Lua for a new name and uses the stored snapshot for an existing one. It creates
the parent state directory if necessary and removes temporary plan files.

State is not a live sandbox inventory. If a sandbox was deleted outside `sup`,
sbx may create it again. Avoid names already used outside `sup`; this version
does not query sandbox ownership. No automatic deletion or recreation is
performed by the wrapper. Use `sup rm` for deliberate removal. Push work you want to keep before removing
its sandbox.

State files are private and updated via rename while a short-lived per-name lock
is held. The lock releases before create/run attaches; removal holds it through sbx
teardown and state cleanup. Other concurrent sbx operations remain sbx's responsibility. If sup is forcibly killed during snapshot preparation, remove
`<state-root>/.locks/<name>` only after checking no process still owns it.

`XDG_CONFIG_HOME` and `XDG_STATE_HOME` override the standard directories.
`--config PATH` selects a config for a new name. Use absolute paths for host
commands that refer to files: sbx resolves relative paths from its generated
file directory. A mutable kit URL remains mutable even in a saved snapshot.

## Development

```sh
go test -race ./...
go vet ./...
go build -o bin/sup ./cmd/sup
sh tests/integration.sh
```

Tests use a fake sbx and never create sandboxes. GitHub Actions runs on Linux and
macOS. Config tests cover setup registration, legacy configs, field validation,
kit resolution, and empty table conversion. Execution tests cover snapshots,
plans, conflicting inputs, corruption, argument boundaries, and exit codes.

## License

MIT. See [LICENSE](LICENSE).
