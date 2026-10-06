import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

spec = importlib.util.spec_from_file_location("history", "internal/sup/history_attach.py")
history = importlib.util.module_from_spec(spec)
spec.loader.exec_module(history)

class ProjectHistory(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.mount = self.root / "history"
        self.mount.mkdir()
        (self.mount / ".sup-project.json").write_text(json.dumps({"project": "docs"}))
        self.home = self.root / "first-home"
        self.home.mkdir()

    def test_recreation_and_archive_keep_data_without_credentials(self):
        codex = self.home / ".codex"
        codex.mkdir()
        (codex / "auth.json").write_text('private auth')
        (codex / "config.toml").write_text('model = "chosen"\n[projects."/workspace"]\ntrust_level = "trusted"\n')
        history.connect(self.home, self.mount, "docs")
        session = codex / "sessions/task.jsonl"
        session.write_text('conversation')
        session.rename(codex / "archived_sessions/task.jsonl")
        (self.home / ".claude/projects/claude.jsonl").write_text('claude conversation')
        (self.mount / "codex/sqlite/state_5.sqlite").write_text('index')
        history.connect(self.home, self.mount, "docs")
        fresh = self.root / "second-home"
        fresh.mkdir()
        history.connect(fresh, self.mount, "docs")
        self.assertEqual((fresh / ".codex/archived_sessions/task.jsonl").read_text(), 'conversation')
        self.assertEqual((fresh / ".claude/projects/claude.jsonl").read_text(), 'claude conversation')
        self.assertEqual((codex / "auth.json").read_text(), 'private auth')
        self.assertFalse((fresh / ".codex/auth.json").exists())
        self.assertEqual((codex / "config.toml").read_text().count("sqlite_home ="), 1)
        self.assertIn('model = "chosen"', (codex / "config.toml").read_text())

    def test_existing_local_transcript_is_never_hidden(self):
        sessions = self.home / ".codex/sessions"
        sessions.mkdir(parents=True)
        (sessions / "local.jsonl").write_text("keep")
        with self.assertRaisesRegex(ValueError, "refusing to hide"):
            history.connect(self.home, self.mount, "docs")
        self.assertFalse(sessions.is_symlink())
        self.assertEqual((sessions / "local.jsonl").read_text(), "keep")
        self.assertFalse((self.mount / "codex").exists())

    def test_other_project_mount_is_refused(self):
        with self.assertRaisesRegex(ValueError, "another project"):
            history.connect(self.home, self.mount, "other")

    def test_existing_sqlite_state_is_refused(self):
        codex = self.home / ".codex"
        codex.mkdir()
        (codex / "state_5.sqlite").write_text("keep")
        with self.assertRaisesRegex(ValueError, "SQLite state exists"):
            history.connect(self.home, self.mount, "docs")
        (codex / "config.toml").write_text('sqlite_home = "/other"')
        with self.assertRaisesRegex(ValueError, "somewhere else"):
            history.connect(self.home, self.mount, "docs")

    def test_clear_and_reconnect_broken_links(self):
        history.connect(self.home, self.mount, "docs")
        import shutil
        for path in list(self.mount.iterdir()):
            if path.is_dir(): shutil.rmtree(path)
        history.connect(self.home, self.mount, "docs")
        self.assertTrue((self.home / ".codex/sessions").is_dir())
        self.assertTrue((self.home / ".claude/history.jsonl").is_file())

if __name__ == "__main__": unittest.main()
