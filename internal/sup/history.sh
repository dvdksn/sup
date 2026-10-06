# Executed by sbx env's postCreate hook on the host.
set -eu
umask 077
if [ -L "$history_root" ]; then
  echo 'History root must be a real directory.' >&2
  exit 1
fi
mkdir -p "$history_root"
sbx mount "$SBX_SANDBOX_NAME" "$history_root:/home/agent/project-history:rw"

sbx exec -i "$SBX_SANDBOX_NAME" sh -eu -s -- /home/agent/project-history <<'GUEST_SETUP'
set -eu
root=$1
# Check every local path before connecting it to the mount.
directories='.codex/sessions .codex/archived_sessions .claude/projects .claude/sessions .claude/shell-snapshots .claude/file-history .claude/todos .claude/tasks'
files='.codex/history.jsonl .codex/session_index.jsonl .claude/history.jsonl'
for relative in $directories $files; do
  link="$HOME/$relative"
  target="$root/${relative#.}"
  if [ -L "$link" ]; then
    test "$(readlink "$link")" = "$target"
  elif [ -d "$link" ]; then
    test -z "$(ls -A "$link")" || {
      echo "Refusing to hide local history: $link" >&2; exit 1;
    }
  elif [ -e "$link" ]; then
    test ! -s "$link" || {
      echo "Refusing to hide local history: $link" >&2; exit 1;
    }
  fi
done

config="$HOME/.codex/config.toml"
setting="sqlite_home = \"$root/codex/sqlite\""
if [ -f "$config" ] && grep -Eq '^[[:space:]]*sqlite_home[[:space:]]*=' "$config"; then
  grep -Fqx "$setting" "$config" || {
    echo 'Codex sqlite_home points somewhere else.' >&2; exit 1;
  }
else
  for database in "$HOME"/.codex/state_*.sqlite*; do
    test ! -e "$database" || {
      echo 'Refusing to redirect existing local Codex SQLite state.' >&2; exit 1;
    }
  done
fi

mkdir -p "$HOME/.codex" "$HOME/.claude" "$root/codex/sqlite"
for relative in $directories $files; do
  link="$HOME/$relative"
  target="$root/${relative#.}"
  case " $directories " in
    *" $relative "*) mkdir -p "$target" ;;
    *) test -e "$target" || : > "$target" ;;
  esac
  if [ -L "$link" ]; then continue; fi
  if [ -d "$link" ]; then rmdir "$link"; else rm -f "$link"; fi
  ln -s "$target" "$link"
done
if ! [ -f "$config" ] || ! grep -Fqx "$setting" "$config"; then
  temporary=$(mktemp "$HOME/.codex/.history-config.XXXXXX")
  trap 'rm -f "$temporary"' EXIT HUP INT TERM
  printf '%s\n' "$setting" > "$temporary"
  if [ -f "$config" ]; then cat "$config" >> "$temporary"; fi
  mv "$temporary" "$config"
fi
GUEST_SETUP
