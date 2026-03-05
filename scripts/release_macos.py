from __future__ import annotations

import argparse
import hashlib
import json
import shlex
import subprocess
from dataclasses import dataclass
from pathlib import Path
from typing import Sequence


PRODUCT_NAME = "Agent Flows Bridge"
PRODUCT_SLUG = "agent-flows-bridge"
APP_BUNDLE_NAME = f"{PRODUCT_NAME}.app"
DEFAULT_BRIDGE_REPO_SLUG = "AppliedAgentics/agent-flows-bridge"
DEFAULT_TAP_REPO_SLUG = "AppliedAgentics/homebrew-tap"
DEFAULT_TAP_NAME = "AppliedAgentics/tap"


@dataclass(frozen=True)
class CommandStep:
    description: str
    argv: tuple[str, ...]
    cwd: Path


def asset_name(version: str) -> str:
    return f"{PRODUCT_SLUG}-{version}-macos.zip"


def release_asset_url(repo_slug: str, version: str, asset_name: str) -> str:
    return f"https://github.com/{repo_slug}/releases/download/v{version}/{asset_name}"


def render_tap_readme(
    tap_name: str = DEFAULT_TAP_NAME,
    cask_name: str = PRODUCT_SLUG,
) -> str:
    return f"""# AppliedAgentics Homebrew Tap

Homebrew tap for AppliedAgentics desktop applications.

## Install {PRODUCT_NAME}

```bash
brew tap {tap_name}
brew install --cask {cask_name}
```

## Upgrade

```bash
brew update
brew upgrade --cask {cask_name}
```

## Uninstall

```bash
brew uninstall --cask {cask_name}
brew untap {tap_name}
```
"""


def update_cask_text(text: str, version: str, sha256: str, repo_slug: str) -> str:
    release_url = f"https://github.com/{repo_slug}/releases/download/v#{{version}}/{PRODUCT_SLUG}-#{{version}}-macos.zip"
    homepage = f"https://github.com/{repo_slug}"

    lines = text.splitlines()
    updated_lines: list[str] = []

    for line in lines:
        stripped = line.strip()

        if stripped.startswith("version "):
            updated_lines.append(f'  version "{version}"')
            continue

        if stripped.startswith("sha256 "):
            updated_lines.append(f'  sha256 "{sha256}"')
            continue

        if stripped.startswith("url "):
            updated_lines.append(f'  url "{release_url}"')
            continue

        if stripped.startswith("homepage "):
            updated_lines.append(f'  homepage "{homepage}"')
            continue

        updated_lines.append(line)

    return "\n".join(updated_lines) + "\n"


def read_version(repo_dir: Path) -> str:
    package_json_path = repo_dir / "desktop" / "package.json"
    with package_json_path.open() as package_json_file:
        package_json = json.load(package_json_file)

    return package_json["version"]


def default_release_notes(repo_dir: Path, version: str) -> str:
    return "\n".join(
        [
            f"## {PRODUCT_NAME} {version}",
            "",
            "Automated macOS release.",
            "",
            "### Included",
            "- macOS desktop app bundle",
            "- Go bridge runtime and local webhook delivery service",
            "- Tauri desktop onboarding UI",
            "- Homebrew cask update via AppliedAgentics tap",
            "",
        ]
    )


def release_notes_path(repo_dir: Path, version: str) -> Path:
    return repo_dir / "release" / f"release-notes-v{version}.md"


def prepare_release_notes(
    repo_dir: Path,
    version: str,
    explicit_path: Path | None,
    dry_run: bool,
) -> Path:
    notes_path = explicit_path or release_notes_path(repo_dir, version)

    if explicit_path is None and not dry_run:
        write_text_if_changed(notes_path, default_release_notes(repo_dir, version))

    return notes_path


def default_app_bundle_path(repo_dir: Path) -> Path:
    return repo_dir / "desktop" / "src-tauri" / "target" / "release" / "bundle" / "macos" / APP_BUNDLE_NAME


def default_asset_path(repo_dir: Path, version: str) -> Path:
    return repo_dir / "release" / asset_name(version)


def plan_release_commands(
    repo_dir: Path,
    tap_dir: Path,
    version: str,
    bridge_repo_slug: str,
    release_notes_path: Path,
    skip_build: bool,
) -> list[CommandStep]:
    repo_dir = repo_dir.resolve()
    tap_dir = tap_dir.resolve()

    bundle_path = default_app_bundle_path(repo_dir)
    zip_path = default_asset_path(repo_dir, version)

    plan: list[CommandStep] = []

    if not skip_build:
        plan.append(
            CommandStep(
                description="Build the macOS Tauri app bundle.",
                argv=("npm", "run", "tauri", "build", "--", "--bundles", "app"),
                cwd=repo_dir / "desktop",
            )
        )

    plan.append(
        CommandStep(
            description="Package the macOS app into a release zip.",
            argv=(
                "ditto",
                "-c",
                "-k",
                "--sequesterRsrc",
                "--keepParent",
                str(bundle_path),
                str(zip_path),
            ),
            cwd=repo_dir,
        )
    )

    plan.append(
        CommandStep(
            description="Publish the GitHub release asset.",
            argv=(
                "gh",
                "release",
                "create",
                f"v{version}",
                str(zip_path),
                "--repo",
                bridge_repo_slug,
                "--target",
                "main",
                "--title",
                f"v{version}",
                "--notes-file",
                str(release_notes_path),
            ),
            cwd=repo_dir,
        )
    )

    plan.append(
        CommandStep(
            description="Commit the updated tap cask and README.",
            argv=("git", "commit", "-m", f"Release v{version} tap cask update"),
            cwd=tap_dir,
        )
    )

    return plan


def write_text_if_changed(path: Path, contents: str) -> bool:
    if path.exists() and path.read_text() == contents:
        return False

    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(contents)
    return True


def file_sha256(path: Path) -> str:
    digest = hashlib.sha256()

    with path.open("rb") as file_handle:
        while True:
            chunk = file_handle.read(1024 * 1024)
            if not chunk:
                break
            digest.update(chunk)

    return digest.hexdigest()


def run_command(argv: Sequence[str], cwd: Path, dry_run: bool) -> None:
    command_display = " ".join(shlex.quote(part) for part in argv)
    print(f"[run] ({cwd}) {command_display}")

    if dry_run:
        return

    subprocess.run(argv, cwd=cwd, check=True)


def ensure_clean_repo(repo_dir: Path) -> None:
    result = subprocess.run(
        ["git", "status", "--short"],
        cwd=repo_dir,
        capture_output=True,
        text=True,
        check=True,
    )

    if result.stdout.strip():
        raise RuntimeError(f"Git repo is not clean: {repo_dir}")


def release_exists(repo_slug: str, version: str) -> bool:
    result = subprocess.run(
        ["gh", "release", "view", f"v{version}", "--repo", repo_slug],
        capture_output=True,
        text=True,
        check=False,
    )

    return result.returncode == 0


def update_repo_cask(repo_dir: Path, version: str, sha256: str, repo_slug: str) -> Path:
    cask_path = repo_dir / "release" / "homebrew" / f"{PRODUCT_SLUG}.rb"
    updated = update_cask_text(cask_path.read_text(), version, sha256, repo_slug)
    write_text_if_changed(cask_path, updated)
    return cask_path


def update_tap_files(
    tap_dir: Path,
    version: str,
    sha256: str,
    bridge_repo_slug: str,
    tap_name: str,
) -> list[Path]:
    cask_path = tap_dir / "Casks" / f"{PRODUCT_SLUG}.rb"
    readme_path = tap_dir / "README.md"

    updated_paths: list[Path] = []

    cask_contents = update_cask_text(cask_path.read_text(), version, sha256, bridge_repo_slug)
    if write_text_if_changed(cask_path, cask_contents):
        updated_paths.append(cask_path)

    if write_text_if_changed(readme_path, render_tap_readme(tap_name=tap_name)):
        updated_paths.append(readme_path)

    return updated_paths


def commit_and_push_if_changed(repo_dir: Path, message: str, dry_run: bool) -> None:
    status = subprocess.run(
        ["git", "status", "--short"],
        cwd=repo_dir,
        capture_output=True,
        text=True,
        check=True,
    )

    if not status.stdout.strip():
        return

    run_command(("git", "add", "."), repo_dir, dry_run)
    run_command(("git", "commit", "-m", message), repo_dir, dry_run)
    run_command(("git", "push"), repo_dir, dry_run)


def publish_release(
    repo_dir: Path,
    repo_slug: str,
    version: str,
    asset_path: Path,
    notes_path: Path,
    dry_run: bool,
) -> None:
    if release_exists(repo_slug, version):
        argv = (
            "gh",
            "release",
            "upload",
            f"v{version}",
            str(asset_path),
            "--repo",
            repo_slug,
            "--clobber",
        )
    else:
        argv = (
            "gh",
            "release",
            "create",
            f"v{version}",
            str(asset_path),
            "--repo",
            repo_slug,
            "--target",
            "main",
            "--title",
            f"v{version}",
            "--notes-file",
            str(notes_path),
        )

    run_command(argv, repo_dir, dry_run)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Build and publish the macOS bridge release.")
    parser.add_argument(
        "--repo-dir",
        type=Path,
        default=Path(__file__).resolve().parent.parent,
        help="Path to the agent-flows-bridge repo.",
    )
    parser.add_argument(
        "--tap-dir",
        type=Path,
        required=True,
        help="Path to the local AppliedAgentics homebrew tap clone.",
    )
    parser.add_argument(
        "--bridge-repo-slug",
        default=DEFAULT_BRIDGE_REPO_SLUG,
        help="GitHub repo slug for bridge releases.",
    )
    parser.add_argument(
        "--tap-name",
        default=DEFAULT_TAP_NAME,
        help="Homebrew tap name users install from.",
    )
    parser.add_argument(
        "--version",
        default="",
        help="Release version. Defaults to desktop/package.json version.",
    )
    parser.add_argument(
        "--release-notes-file",
        type=Path,
        default=None,
        help="Optional path to release notes file.",
    )
    parser.add_argument(
        "--skip-build",
        action="store_true",
        help="Skip the Tauri build step and reuse the existing app bundle.",
    )
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="Print the planned commands without executing them.",
    )
    return parser.parse_args()


def main() -> int:
    args = parse_args()

    repo_dir = args.repo_dir.resolve()
    tap_dir = args.tap_dir.resolve()
    version = args.version or read_version(repo_dir)
    asset_path = default_asset_path(repo_dir, version)
    bundle_path = default_app_bundle_path(repo_dir)

    if not args.dry_run:
        ensure_clean_repo(repo_dir)
        ensure_clean_repo(tap_dir)

    notes_path = prepare_release_notes(
        repo_dir=repo_dir,
        version=version,
        explicit_path=args.release_notes_file,
        dry_run=args.dry_run,
    )

    if args.dry_run:
        plan = plan_release_commands(
            repo_dir=repo_dir,
            tap_dir=tap_dir,
            version=version,
            bridge_repo_slug=args.bridge_repo_slug,
            release_notes_path=notes_path,
            skip_build=args.skip_build,
        )

        print(f"Planned version: {version}")
        print(f"Planned bundle: {bundle_path}")
        print(f"Planned asset: {asset_path}")
        print(f"Planned release URL: {release_asset_url(args.bridge_repo_slug, version, asset_name(version))}")
        for step in plan:
            run_command(step.argv, step.cwd, dry_run=True)
        return 0

    if not args.skip_build:
        run_command(("npm", "run", "tauri", "build", "--", "--bundles", "app"), repo_dir / "desktop", dry_run=False)

    asset_path.parent.mkdir(parents=True, exist_ok=True)
    run_command(
        (
            "ditto",
            "-c",
            "-k",
            "--sequesterRsrc",
            "--keepParent",
            str(bundle_path),
            str(asset_path),
        ),
        repo_dir,
        dry_run=False,
    )

    sha256 = file_sha256(asset_path)
    update_repo_cask(repo_dir, version, sha256, args.bridge_repo_slug)
    update_tap_files(
        tap_dir=tap_dir,
        version=version,
        sha256=sha256,
        bridge_repo_slug=args.bridge_repo_slug,
        tap_name=args.tap_name,
    )

    commit_and_push_if_changed(repo_dir, f"Release v{version} metadata update", dry_run=False)
    publish_release(
        repo_dir=repo_dir,
        repo_slug=args.bridge_repo_slug,
        version=version,
        asset_path=asset_path,
        notes_path=notes_path,
        dry_run=False,
    )
    commit_and_push_if_changed(tap_dir, f"Release v{version} tap cask update", dry_run=False)

    print(f"Release completed for v{version}")
    print(f"Release asset: {asset_path}")
    print(f"SHA256: {sha256}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
