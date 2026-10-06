#!/usr/bin/env python3
"""Attach selected agent state; never replace existing local conversation data."""
import sys
import json
import os
from pathlib import Path
import tempfile
import tomllib


def connect(home, root, project):
    if json.loads((root / ".sup-project.json").read_text())["project"] != project:
        raise ValueError("history mount belongs to another project")
    directories = {
        ".codex/sessions": "codex/sessions",
        ".codex/archived_sessions": "codex/archived_sessions",
        ".claude/projects": "claude/projects",
        ".claude/sessions": "claude/sessions",
        ".claude/shell-snapshots": "claude/shell-snapshots",
        ".claude/file-history": "claude/file-history",
        ".claude/todos": "claude/todos",
        ".claude/tasks": "claude/tasks",
    }
    files = {
        ".codex/history.jsonl": "codex/history.jsonl",
        ".codex/session_index.jsonl": "codex/session_index.jsonl",
        ".claude/history.jsonl": "claude/history.jsonl",
    }
    for relative, target in (directories | files).items():
        link, destination = home / relative, root / target
        if link.is_symlink():
            if link.readlink() != destination:
                raise ValueError(f"unexpected history symlink: {link}")
        elif link.exists():
            empty = not any(link.iterdir()) if link.is_dir() else link.stat().st_size == 0
            if not empty:
                raise ValueError(f"local state already exists at {link}; refusing to hide it")
    config = home / ".codex/config.toml"
    text = config.read_text() if config.exists() else ""
    settings = tomllib.loads(text)
    sqlite = root / "codex/sqlite"
    if settings.get("sqlite_home", str(sqlite)) != str(sqlite):
        raise ValueError("Codex sqlite_home already points somewhere else")
    if "sqlite_home" not in settings and any((home / ".codex").glob("state_*.sqlite*")):
        raise ValueError("local Codex SQLite state exists; refusing to redirect it")
    sqlite.mkdir(parents=True, exist_ok=True)
    for relative, target in (directories | files).items():
        link, destination = home / relative, root / target
        destination.parent.mkdir(parents=True, exist_ok=True)
        if relative in directories:
            destination.mkdir(exist_ok=True)
        else:
            destination.touch(exist_ok=True)
        link.parent.mkdir(parents=True, exist_ok=True)
        if link.is_symlink():
            continue
        if link.is_dir():
            link.rmdir()
        elif link.exists():
            link.unlink()
        link.symlink_to(destination)
    if "sqlite_home" not in settings:
        config.parent.mkdir(parents=True, exist_ok=True)
        updated = "sqlite_home = " + json.dumps(str(sqlite)) + "\n" + text
        fd, temporary = tempfile.mkstemp(dir=config.parent, prefix=".history-config-")
        try:
            with os.fdopen(fd, "w") as stream:
                stream.write(updated)
            os.replace(temporary, config)
        finally:
            if os.path.exists(temporary):
                os.unlink(temporary)


if __name__ == "__main__":
    try:
        connect(Path.home(), Path(sys.argv[2]), sys.argv[1])
    except (OSError, ValueError, KeyError) as error:
        sys.exit(f"Cannot connect project history: {error}")
