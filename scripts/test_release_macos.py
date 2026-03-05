import pathlib
import tempfile
import unittest
from argparse import Namespace
from datetime import date
from unittest import mock

from scripts import release_macos


class ReleaseMacOSTests(unittest.TestCase):
    def test_asset_name_uses_version(self):
        asset_name = release_macos.asset_name("0.2.0")

        self.assertEqual(asset_name, "agent-flows-bridge-0.2.0-macos.zip")

    def test_release_url_uses_repo_slug_and_version(self):
        url = release_macos.release_asset_url(
            repo_slug="AppliedAgentics/agent-flows-bridge",
            version="0.2.0",
            asset_name="agent-flows-bridge-0.2.0-macos.zip",
        )

        self.assertEqual(
            url,
            "https://github.com/AppliedAgentics/agent-flows-bridge/releases/download/v0.2.0/agent-flows-bridge-0.2.0-macos.zip",
        )

    def test_next_calendar_version_increments_for_same_day(self):
        version = release_macos.next_calendar_version(
            release_date=date(2026, 3, 5),
            existing_versions=["2026.03.05.01", "2026.03.05.02", "2026.03.04.07"],
        )

        self.assertEqual(version, "2026.03.05.03")

    def test_next_calendar_version_starts_new_day_at_01(self):
        version = release_macos.next_calendar_version(
            release_date=date(2026, 3, 6),
            existing_versions=["2026.03.05.09", "2026.03.05.10"],
        )

        self.assertEqual(version, "2026.03.06.01")

    def test_update_cask_rewrites_version_sha_and_urls(self):
        original = """
# typed: strict
# frozen_string_literal: true

cask "agent-flows-bridge" do
  version "0.1.0"
  sha256 "oldsha"

  url "https://github.com/AppliedAgentics/agent-flows-bridge/releases/download/v#{version}/agent-flows-bridge-#{version}-macos.zip"
  name "Agent Flows Bridge"
  desc "Desktop bridge for connecting local OpenClaw runtimes to Agent Flows"
  homepage "https://github.com/AppliedAgentics/agent-flows-bridge"
end
""".lstrip()

        updated = release_macos.update_cask_text(
            original,
            version="0.2.0",
            sha256="newsha",
            repo_slug="AppliedAgentics/agent-flows-bridge",
        )

        self.assertIn('version "0.2.0"', updated)
        self.assertIn('sha256 "newsha"', updated)
        self.assertIn(
            'url "https://github.com/AppliedAgentics/agent-flows-bridge/releases/download/v#{version}/agent-flows-bridge-#{version}-macos.zip"',
            updated,
        )
        self.assertIn(
            'homepage "https://github.com/AppliedAgentics/agent-flows-bridge"',
            updated,
        )

    def test_render_tap_readme_contains_install_upgrade_and_uninstall(self):
        contents = release_macos.render_tap_readme()

        self.assertIn("brew tap AppliedAgentics/tap", contents)
        self.assertIn("brew install --cask agent-flows-bridge", contents)
        self.assertIn("brew upgrade --cask agent-flows-bridge", contents)
        self.assertIn("brew uninstall --cask agent-flows-bridge", contents)

    def test_plan_release_commands_includes_build_release_and_tap_steps(self):
        plan = release_macos.plan_release_commands(
            repo_dir=pathlib.Path("/tmp/agent-flows-bridge"),
            tap_dir=pathlib.Path("/tmp/homebrew-tap"),
            version="0.2.0",
            bridge_repo_slug="AppliedAgentics/agent-flows-bridge",
            release_notes_path=pathlib.Path("/tmp/release-notes.md"),
            skip_build=False,
        )

        rendered = [" ".join(step.argv) for step in plan]

        self.assertIn(
            "npm run tauri build -- --bundles app",
            rendered[0],
        )
        self.assertTrue(
            any("gh release create v0.2.0" in command for command in rendered),
            rendered,
        )
        self.assertTrue(
            any("git commit -m Release v0.2.0 tap cask update" in command for command in rendered),
            rendered,
        )

    def test_default_release_notes_uses_matching_changelog_entry(self):
        with tempfile.TemporaryDirectory() as temp_dir:
            repo_dir = pathlib.Path(temp_dir)
            (repo_dir / "CHANGELOG.md").write_text(
                "\n".join(
                    [
                        "# Changelog",
                        "",
                        "All notable changes to Agent Flows Bridge are documented in this file.",
                        "",
                        "---",
                        "",
                        "## 2026.03.05.03",
                        "",
                        "### Changes",
                        "",
                        "- Add tag-driven GitHub Actions release publishing",
                        "- Switch the app versioning scheme to calendar versions",
                        "",
                        "## 2026.03.05.02",
                        "",
                        "### Changes",
                        "",
                        "- Older entry",
                        "",
                    ]
                )
            )

            notes = release_macos.default_release_notes(repo_dir, "2026.03.05.03")

            self.assertIn("## Agent Flows Bridge 2026.03.05.03", notes)
            self.assertIn("- Add tag-driven GitHub Actions release publishing", notes)
            self.assertNotIn("Older entry", notes)

    def test_prepare_release_notes_does_not_write_in_dry_run(self):
        with tempfile.TemporaryDirectory() as temp_dir:
            repo_dir = pathlib.Path(temp_dir)
            notes_path = release_macos.prepare_release_notes(
                repo_dir=repo_dir,
                version="0.2.0",
                explicit_path=None,
                dry_run=True,
            )

            self.assertEqual(
                notes_path,
                repo_dir / "release" / "release-notes-v0.2.0.md",
            )
            self.assertFalse(notes_path.exists())

    def test_prepare_release_notes_writes_default_file_for_real_run(self):
        with tempfile.TemporaryDirectory() as temp_dir:
            repo_dir = pathlib.Path(temp_dir)
            notes_path = release_macos.prepare_release_notes(
                repo_dir=repo_dir,
                version="0.2.0",
                explicit_path=None,
                dry_run=False,
            )

            self.assertTrue(notes_path.exists())
            self.assertIn("## Agent Flows Bridge 0.2.0", notes_path.read_text())

    def test_prepare_release_metadata_updates_versions_and_changelog(self):
        with tempfile.TemporaryDirectory() as temp_dir:
            repo_dir = pathlib.Path(temp_dir)
            (repo_dir / "desktop" / "src-tauri").mkdir(parents=True)
            (repo_dir / "desktop" / "package.json").write_text(
                '{"name":"agent-flows-bridge-desktop","version":"0.1.1"}\n'
            )
            (repo_dir / "desktop" / "package-lock.json").write_text(
                '{\n  "version": "0.1.1",\n  "packages": {\n    "": {\n      "version": "0.1.1"\n    }\n  }\n}\n'
            )
            (repo_dir / "desktop" / "src-tauri" / "tauri.conf.json").write_text(
                '{\n  "version": "0.1.0"\n}\n'
            )
            (repo_dir / "desktop" / "src-tauri" / "Cargo.toml").write_text(
                '[package]\nname = "desktop"\nversion = "0.1.1"\n'
            )
            (repo_dir / "desktop" / "src-tauri" / "Cargo.lock").write_text(
                '[[package]]\nname = "desktop"\nversion = "0.1.1"\n'
            )
            (repo_dir / "CHANGELOG.md").write_text(
                "\n".join(
                    [
                        "# Changelog",
                        "",
                        "All notable changes to Agent Flows Bridge are documented in this file.",
                        "",
                        "---",
                        "",
                        "## 2026.03.05.02",
                        "",
                        "### Changes",
                        "",
                        "- Previous entry",
                        "",
                    ]
                )
            )

            changed_paths = release_macos.prepare_release_metadata(
                repo_dir=repo_dir,
                version="2026.03.05.03",
                changes=[
                    "Switch the desktop and cask versioning to calendar versions",
                    "Prepare the first date-versioned release flow",
                ],
                dry_run=False,
            )

            self.assertIn(repo_dir / "desktop" / "package.json", changed_paths)
            self.assertIn(repo_dir / "CHANGELOG.md", changed_paths)
            self.assertIn('"version": "2026.03.05.03"', (repo_dir / "desktop" / "package.json").read_text())
            self.assertIn('"version": "2026.03.05.03"', (repo_dir / "desktop" / "package-lock.json").read_text())
            self.assertIn('"version": "2026.03.05.03"', (repo_dir / "desktop" / "src-tauri" / "tauri.conf.json").read_text())
            self.assertIn('version = "2026.03.05.03"', (repo_dir / "desktop" / "src-tauri" / "Cargo.toml").read_text())
            self.assertIn('version = "2026.03.05.03"', (repo_dir / "desktop" / "src-tauri" / "Cargo.lock").read_text())
            self.assertIn("## 2026.03.05.03", (repo_dir / "CHANGELOG.md").read_text())
            self.assertIn("- Switch the desktop and cask versioning to calendar versions", (repo_dir / "CHANGELOG.md").read_text())

    def test_prepare_release_metadata_dry_run_does_not_write_files(self):
        with tempfile.TemporaryDirectory() as temp_dir:
            repo_dir = pathlib.Path(temp_dir)
            (repo_dir / "desktop" / "src-tauri").mkdir(parents=True)
            (repo_dir / "desktop" / "package.json").write_text('{"version":"0.1.1"}\n')
            (repo_dir / "desktop" / "package-lock.json").write_text('{"version":"0.1.1","packages":{"":{"version":"0.1.1"}}}\n')
            (repo_dir / "desktop" / "src-tauri" / "tauri.conf.json").write_text('{"version":"0.1.1"}\n')
            (repo_dir / "desktop" / "src-tauri" / "Cargo.toml").write_text('[package]\nversion = "0.1.1"\n')
            (repo_dir / "desktop" / "src-tauri" / "Cargo.lock").write_text('[[package]]\nname = "desktop"\nversion = "0.1.1"\n')
            (repo_dir / "CHANGELOG.md").write_text("# Changelog\n\n---\n")

            release_macos.prepare_release_metadata(
                repo_dir=repo_dir,
                version="2026.03.05.03",
                changes=["Dry-run test change"],
                dry_run=True,
            )

            self.assertIn('"version":"0.1.1"', (repo_dir / "desktop" / "package.json").read_text())
            self.assertNotIn("2026.03.05.03", (repo_dir / "CHANGELOG.md").read_text())

    def test_main_does_not_write_release_notes_before_clean_repo_check(self):
        with tempfile.TemporaryDirectory() as temp_dir:
            root = pathlib.Path(temp_dir)
            repo_dir = root / "repo"
            tap_dir = root / "tap"
            (repo_dir / "desktop").mkdir(parents=True)
            (tap_dir).mkdir(parents=True)

            (repo_dir / "desktop" / "package.json").write_text('{"version":"0.2.0"}')

            release_macos.subprocess.run(
                ["git", "init"],
                cwd=repo_dir,
                check=True,
                capture_output=True,
                text=True,
            )
            release_macos.subprocess.run(
                ["git", "init"],
                cwd=tap_dir,
                check=True,
                capture_output=True,
                text=True,
            )
            (repo_dir / "dirty.txt").write_text("dirty\n")

            args = Namespace(
                repo_dir=repo_dir,
                tap_dir=tap_dir,
                prepare_release=False,
                version="",
                release_date="",
                change=[],
                release_notes_file=None,
                bridge_repo_slug=release_macos.DEFAULT_BRIDGE_REPO_SLUG,
                tap_name=release_macos.DEFAULT_TAP_NAME,
                skip_build=True,
                dry_run=False,
            )

            with mock.patch.object(release_macos, "parse_args", return_value=args):
                with self.assertRaises(RuntimeError):
                    release_macos.main()

            self.assertFalse(
                (repo_dir / "release" / "release-notes-v0.2.0.md").exists()
            )


if __name__ == "__main__":
    unittest.main()
