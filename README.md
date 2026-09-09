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

## Bash completion

Add this to `~/.bashrc` (and run it in your current shell):

```bash
eval "$(sup completion bash)"
```

`sup rm <Tab>` completes saved environment names, including PR/ref variants.
`sup <Tab>` also completes saved names for reconnecting and command names.
Built-in options and `--config` file paths are completed too. Completion reads
local saved state without loading Lua or calling sbx; config-defined flags and
kit aliases are not completed. Works with Bash 3.2+ without bash-completion.
If your login shell only reads `~/.bash_profile`, source `.bashrc` from there.

## Lua configuration

`~/.config/sup/config.lua` calls `sup.setup` once:

```lua
local sup = require('sup')

sup.setup({
  args = {
    pr = { description = 'Pull request to check out', pattern = '[1-9][0-9]*' },
    ref = { description = 'Branch, tag, or commit' },
  },
  name = function(ctx)
    assert(not (ctx.args.pr and ctx.args.ref), 'pr and ref are mutually exclusive')
    if ctx.args.pr then return ctx.name .. '-pr-' .. ctx.args.pr end
    if ctx.args.ref then return ctx.name .. '-ref-' .. ctx.args.ref end
    return ctx.name
  end,
  kits = {
    browser = 'ghcr.io/dvdksn/browser-kit:codex',
  },
  defaults = function(ctx)
    assert(not (ctx.args.pr and ctx.args.ref), 'pr and ref are mutually exclusive')
    return {
      agent = 'codex',
      secrets = { github = { command = 'gh auth token' } },
      kits = {
        {
          source = 'git+https://github.com/cdupuis/sbx-kits.git#dir=github-clone',
          args = {
            repo = ctx.repo, ref = ctx.args.ref or '', pr = ctx.args.pr or '', dir = '/project',
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
| `name` | Resolved environment name |
| `args` | Declared argument values, including defaults; omitted optional keys are nil |

It returns native sbxenv fields. `schemaVersion` defaults to `"1"`; `name` defaults
to `ctx.name` and cannot identify a different sandbox. `workspace` and
`additionalWorkspaces` are rejected. The clone behavior is supplied by your kit,
not hardcoded into the launcher. The example config enforces mutual exclusion
between its `pr` and `ref` arguments.

GopherLua embeds Lua 5.1 with some extensions, including `goto`; it is not Lua
5.4 or LuaJIT and does not load native Lua C modules. Standard GopherLua libraries
are available. Config is trusted host code, not a security sandbox. An execution
context limits Lua evaluation to five seconds; it cannot interrupt arbitrary
blocking host-library calls. Config is never discovered in cloned repositories.

For migration, the original `configure(ctx)` callback remains supported, but
cannot be combined with `defaults` or `repos`. A returned config table is also
supported; do not combine it with `sup.setup` in the same file.

## Config arguments and naming

```sh
sup -h
sup args
sup args --config /path/to/config.lua
sup docker/docs --pr 123
sup docker/docs --ref feature/auth
sup docker/docs -a pr=123  # Equivalent generic form
```

Declare inputs in the top-level `args` table (distinct from a kit's own `args`):

```lua
args = {
  browser = {
    description = 'Browser engine',
    default = 'chromium',
    choices = { 'chromium', 'firefox' },
  },
  ticket = { description = 'Ticket ID', required = true },
},
```

Each declared key automatically becomes a long flag: `pr` creates `--pr VALUE`.
Both `--pr 123` and `--pr=123` work; all declared flags take string values, even
when the value is `true` or `false`. No per-argument `flag` property is needed.
The launcher reserves `help`, `detached`, `force`, `plan`, `kit`, `name`, `config`,
and `arg`; declarations using those names are rejected.

`sup -h` includes declared flags, descriptions, and constraints. It loads the
trusted config but does not invoke naming or defaults callbacks or require inputs.
If the config is missing or broken, launcher help still appears with a brief
warning. `sup --config PATH -h` uses the selected config.

`sup args` prints descriptions, defaults, required flags, choices, and patterns.
It executes the trusted config file to register its declarations, but does not
call `name` or `defaults`, require argument values, contact sbx, or create state.
Declarations themselves must be valid, including any declared default values.

Every input is a string. `-a` and `--arg` split on the first `=`; values may contain
more `=` characters, colons, or shell-quoted spaces. Empty values are permitted.
Unknown and duplicate keys are rejected, including a key supplied once as a flag
and again via `-a`. Optional declarations may omit both
`default` and `required`; `required=true` cannot be combined with a default.
`choices` validates exact strings. `pattern` uses Go RE2 syntax and matches the
entire value. Cross-argument rules belong in your Lua callbacks.

An optional `name(ctx)` callback receives the base repository-derived name and
resolved args. It returns a string; sup sanitizes unsupported characters and adds
a short hash whenever sanitization or truncation is needed. `--name` bypasses the
callback. Without a naming callback, all arguments use the same `owner-repo`
name; values do not automatically get added to it.

The example config adds PR/ref suffixes. `--pr` and `--ref` exist because that
config declares them; the launcher gives them no special meaning. Read their
values through `ctx.args.pr` / `ctx.args.ref`. New arguments are stored in plaintext
alongside the resolved environment; use native sbx secret sources for credentials.

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

## Listing kit aliases

```sh
sup kits
sup kits --config /path/to/config.lua
```

Lists the top-level `kits` table as sorted `NAME` / `SOURCE` rows. These are the
aliases available to `--kit`; this does not list installed kits or fetch a remote
catalog. Like `sup args`, it loads the trusted Lua config without calling naming
or defaults callbacks, requiring argument values, invoking sbx, or creating state.

## Listing and removal

```sh
sup ls
sup rm docker/docs
sup rm docker/docs --pr 123
sup rm docker-docs-pr-123
sup rm docs-review --force
```

`sup ls` lists saved environments with their repository, saved argument selection, live
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
state. Unknown names are rejected. Repository-based removal loads the config to
resolve its name; removal by saved name does not load it. Neither evaluates
`defaults` or creates environment state. `args`, `kits`, `ls`, `rm`, `completion`, and the internal `__complete` are reserved command names; an existing environment with
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

Each name also has `state.json`, containing its repository, resolved arguments,
and resolved environment. It is saved before provisioning, so failed/cancelled
creates can be retried with identical inputs. Saved arguments include defaults.

Repository-based commands load the config, validate inputs, and invoke `name`
(if provided) to locate state. Existing environments then reuse their saved
snapshot without evaluating `defaults`. Changing naming rules or argument
defaults can select a different name. Reconnect by saved name to bypass the config
entirely when using no config flags (or only `-a`), even if the config has changed
or is unavailable. Declared long flags load the config to check their declarations
and supplied values, even with a saved-name target:

```sh
sup docker/docs -a pr=123  # Config resolves docker-docs-pr-123
sup docker-docs-pr-123     # Saved state; config is not loaded
```

Explicit `--name` also bypasses config on reconnect unless declared long flags
are supplied. Supplied argument values
must match the snapshot; unspecified values keep their saved values. Existing
names reject `--kit` and `--config`. Use a new name for a different setup.
Snapshots from older versions remain readable; the oldest lack any selection
metadata and must be accessed without arguments. Version-2 snapshots can still
match `-a pr=...` and `-a ref=...` against their saved metadata.

`--plan` invokes `sbx env plan` without saving a new environment. It evaluates
the config as needed for naming, evaluates defaults only for a new name, and
uses the stored snapshot for an existing one. It creates
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
