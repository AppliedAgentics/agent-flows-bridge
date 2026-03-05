from __future__ import annotations

import argparse
import hashlib
import json
import re
import shlex
import subprocess
from dataclasses import dataclass
from datetime import date
from pathlib import Path
from typing import Sequence


PRODUCT_NAME = "Agent Flows Bridge"
PRODUCT_SLUG = "agent-flows-bridge"
APP_BUNDLE_NAME = f"{PRODUCT_NAME}.app"
DEFAULT_BRIDGE_REPO_SLUG = "AppliedAgentics/agent-flows-bridge"
DEFAULT_TAP_REPO_SLUG = "AppliedAgentics/homebrew-tap"
DEFAULT_TAP_NAME = "AppliedAgentics/tap"
VERSION_PATTERN = re.compile(r"^\d{4}\.\d{2}\.\d{2}\.\d{2}$")
CHANGELOG_HEADING_PATTERN = re.compile(r"^## (\d{4}\.\d{2}\.\d{2}\.\d{2})$")


@dataclass(frozen=True)
class CommandStep:
    description: str
    argv: tuple[str, ...]
    cwd: Path


def asset_name(version: str) -> str:
    return f"{PRODUCT_SLUG}-{version}-macos.zip"


def release_tag(version: str) -> str:
    return f"v{version}"


def valid_calendar_version(version: str) -> bool:
    return bool(VERSION_PATTERN.fullmatch(version))


def next_calendar_version(release_date: date, existing_versions: Sequence[str]) -> str:
    prefix = release_date.strftime("%Y.%m.%d.")
    suffixes = [
        int(version.rsplit(".", 1)[1])
        for version in existing_versions
        if valid_calendar_version(version) and version.startswith(prefix)
    ]
    next_suffix = max(suffixes, default=0) + 1
    return f"{prefix}{next_suffix:02d}"


def release_asset_url(repo_slug: str, version: str, asset_name: str) -> str:
    return f"https://github.com/{repo_slug}/releases/download/{release_tag(version)}/{asset_name}"


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


def changelog_path(repo_dir: Path) -> Path:
    return repo_dir / "CHANGELOG.md"


def read_changelog(repo_dir: Path) -> str:
    path = changelog_path(repo_dir)
    return path.read_text() if path.exists() else ""


def extract_changelog_versions(text: str) -> list[str]:
    versions: list[str] = []

    for line in text.splitlines():
        match = CHANGELOG_HEADING_PATTERN.match(line.strip())
        if match:
            versions.append(match.group(1))

    return versions


def changelog_entry_body(text: str, version: str) -> str:
    lines = text.splitlines()
    heading = f"## {version}"
    start_index = None

    for index, line in enumerate(lines):
        if line.strip() == heading:
            start_index = index + 1
            break

    if start_index is None:
        return ""

    end_index = len(lines)
    for index in range(start_index, len(lines)):
        if index > start_index and lines[index].startswith("## "):
            end_index = index
            break

    body_lines = lines[start_index:end_index]
    body = "\n".join(body_lines).strip()
    return body


def default_release_notes(repo_dir: Path, version: str) -> str:
    changelog_body = changelog_entry_body(read_changelog(repo_dir), version)

    if changelog_body:
        return f"## {PRODUCT_NAME} {version}\n\n{changelog_body}\n"

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


def update_json_version_file(
    path: Path,
    version: str,
    dry_run: bool,
    package_root_key: str | None = None,
) -> bool:
    payload = json.loads(path.read_text())
    payload["version"] = version

    if package_root_key is not None:
        package_root = payload.setdefault(package_root_key, {}).setdefault("", {})
        package_root["version"] = version

    return write_text_if_changed_or_preview(path, json.dumps(payload, indent=2) + "\n", dry_run)


def update_cargo_toml_text(text: str, version: str) -> str:
    return re.sub(r'^version = "[^"]+"$', f'version = "{version}"', text, count=1, flags=re.MULTILINE)


def update_cargo_lock_text(text: str, version: str) -> str:
    pattern = r'(\[\[package\]\]\nname = "desktop"\nversion = ")[^"]+(")'
    return re.sub(pattern, rf'\g<1>{version}\2', text, count=1)


def changelog_entry_text(version: str, changes: Sequence[str]) -> str:
    bullet_lines = [f"- {change}" for change in changes] or [f"- Prepare release {version}."]
    bullets = "\n".join(bullet_lines)
    return f"## {version}\n\n### Changes\n\n{bullets}\n"


def prepend_changelog_entry(text: str, version: str, changes: Sequence[str]) -> str:
    if f"## {version}" in text:
        return text

    separator = "\n---\n"
    entry = f"\n\n{changelog_entry_text(version, changes)}"

    if separator not in text:
        return text.rstrip() + entry + "\n"

    head, tail = text.split(separator, 1)
    return f"{head}{separator}{entry}{tail.lstrip()}"


def collect_existing_versions(repo_dir: Path) -> list[str]:
    versions = set(extract_changelog_versions(read_changelog(repo_dir)))
    current_version = read_version(repo_dir)

    if valid_calendar_version(current_version):
        versions.add(current_version)

    result = subprocess.run(
        ["git", "tag", "--list"],
        cwd=repo_dir,
        capture_output=True,
        text=True,
        check=True,
    )

    for line in result.stdout.splitlines():
        tag = line.strip()
        version = tag[1:] if tag.startswith("v") else tag
        if valid_calendar_version(version):
            versions.add(version)

    return sorted(versions)


def prepare_release_metadata(
    repo_dir: Path,
    version: str,
    changes: Sequence[str],
    dry_run: bool,
) -> list[Path]:
    if not valid_calendar_version(version):
        raise ValueError(f"Invalid release version: {version}")

    changed_paths: list[Path] = []
    package_json_path = repo_dir / "desktop" / "package.json"
    package_lock_path = repo_dir / "desktop" / "package-lock.json"
    tauri_conf_path = repo_dir / "desktop" / "src-tauri" / "tauri.conf.json"
    cargo_toml_path = repo_dir / "desktop" / "src-tauri" / "Cargo.toml"
    cargo_lock_path = repo_dir / "desktop" / "src-tauri" / "Cargo.lock"
    changelog_file_path = changelog_path(repo_dir)

    if update_json_version_file(package_json_path, version, dry_run):
        changed_paths.append(package_json_path)

    if update_json_version_file(package_lock_path, version, dry_run, package_root_key="packages"):
        changed_paths.append(package_lock_path)

    if update_json_version_file(tauri_conf_path, version, dry_run):
        changed_paths.append(tauri_conf_path)

    cargo_toml_text = update_cargo_toml_text(cargo_toml_path.read_text(), version)
    if write_text_if_changed_or_preview(cargo_toml_path, cargo_toml_text, dry_run):
        changed_paths.append(cargo_toml_path)

    cargo_lock_text = update_cargo_lock_text(cargo_lock_path.read_text(), version)
    if write_text_if_changed_or_preview(cargo_lock_path, cargo_lock_text, dry_run):
        changed_paths.append(cargo_lock_path)

    changelog_text = prepend_changelog_entry(read_changelog(repo_dir), version, changes)
    if write_text_if_changed_or_preview(changelog_file_path, changelog_text, dry_run):
        changed_paths.append(changelog_file_path)

    return changed_paths


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
                    release_tag(version),
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


def write_text_if_changed_or_preview(path: Path, contents: str, dry_run: bool) -> bool:
    if path.exists() and path.read_text() == contents:
        return False

    if dry_run:
        return True

    return write_text_if_changed(path, contents)


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
        ["gh", "release", "view", release_tag(version), "--repo", repo_slug],
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
            release_tag(version),
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
            release_tag(version),
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
        default=None,
        help="Path to the local AppliedAgentics homebrew tap clone.",
    )
    parser.add_argument(
        "--prepare-release",
        action="store_true",
        help="Update the app version files and changelog to the next calendar version.",
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
        help="Release version. Defaults to the next calendar version for prepare, or desktop/package.json for publish.",
    )
    parser.add_argument(
        "--release-date",
        default="",
        help="Release date in YYYY-MM-DD for calendar version generation.",
    )
    parser.add_argument(
        "--change",
        action="append",
        default=[],
        help="Changelog bullet for a prepared release. Repeat for multiple bullets.",
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

    if args.prepare_release:
        if not args.dry_run:
            ensure_clean_repo(repo_dir)

        release_date = date.fromisoformat(args.release_date) if args.release_date else date.today()
        version = args.version or next_calendar_version(release_date, collect_existing_versions(repo_dir))
        changed_paths = prepare_release_metadata(
            repo_dir=repo_dir,
            version=version,
            changes=args.change,
            dry_run=args.dry_run,
        )

        print(f"Prepared version: {version}")
        if changed_paths:
            print("Updated files:")
            for path in changed_paths:
                print(f"- {path}")
        else:
            print("No versioned files required changes.")

        return 0

    if args.tap_dir is None:
        raise RuntimeError("--tap-dir is required unless --prepare-release is used")

    tap_dir = args.tap_dir.resolve()
    version = args.version or read_version(repo_dir)

    if not valid_calendar_version(version):
        raise RuntimeError(
            f"Publish version must use YYYY.MM.DD.XX, got: {version}"
        )

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
