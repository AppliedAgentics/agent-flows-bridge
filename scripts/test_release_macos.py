import pathlib
import tempfile
import unittest

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


if __name__ == "__main__":
    unittest.main()
