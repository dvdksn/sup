#!/bin/sh
set -eu
SUP=${SUP:-./bin/sup}
TEST_ROOT=$(mktemp -d)
trap 'rm -rf "$TEST_ROOT"' EXIT HUP INT TERM
export XDG_CONFIG_HOME="$TEST_ROOT/config" XDG_STATE_HOME="$TEST_ROOT/state"
export SUP_TEST_LOG="$TEST_ROOT/log"
mkdir -p "$TEST_ROOT/bin" "$XDG_CONFIG_HOME/sup"
cp examples/config.lua "$XDG_CONFIG_HOME/sup/config.lua"
cat > "$TEST_ROOT/bin/sbx" <<'SH'
#!/bin/sh
printf '%s\n' "$@" > "$SUP_TEST_LOG"
exit "${SUP_TEST_EXIT:-0}"
SH
chmod +x "$TEST_ROOT/bin/sbx"
export PATH="$TEST_ROOT/bin:$PATH"
"$SUP" docker/docs --plan
[ ! -f "$XDG_STATE_HOME/sup/docker-docs/state.json" ]
"$SUP" docker/docs --kit browser -d
grep -qx create "$SUP_TEST_LOG"
[ -f "$XDG_STATE_HOME/sup/docker-docs/sbxenv.yaml" ]
# Saved reconnection must work even if the original config is now broken.
printf 'error("should not run")\n' > "$XDG_CONFIG_HOME/sup/config.lua"
"$SUP" docker-docs
grep -qx run "$SUP_TEST_LOG"
if "$SUP" docker-docs --kit vale; then exit 1; fi
if "$SUP" other/repo --name docker-docs; then exit 1; fi
if "$SUP" missing; then exit 1; fi
export SUP_TEST_EXIT=7
set +e
"$SUP" docker-docs
result=$?
set -e
[ "$result" -eq 7 ]
[ ! -d "$XDG_STATE_HOME/sup/.locks/docker-docs" ]
echo 'integration tests passed'
