from __future__ import annotations

import base64
import json
import os
import platform
import re
import secrets
import subprocess
import tempfile
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Mapping


@dataclass(frozen=True)
class ReleaseSigningConfig:
    certificate_base64: str
    certificate_password: str
    signing_identity: str


IDENTITY_LINE_PATTERN = re.compile(r'^\s*\d+\)\s+([0-9A-F]{40})\s+"([^"]+)"$', re.MULTILINE)


def repo_root() -> Path:
    return Path(__file__).resolve().parent.parent


def binary_file_name() -> str:
    return "agent-flows-bridge.exe" if platform.system().lower().startswith("win") else "agent-flows-bridge"


def desktop_version(repo_dir: Path) -> str:
    package_json_path = repo_dir / "desktop" / "package.json"
    with package_json_path.open(encoding="utf-8") as package_json_file:
        package_json = json.load(package_json_file)

    return package_json["version"]


def git_commit(repo_dir: Path) -> str:
    result = subprocess.run(
        ["git", "rev-parse", "HEAD"],
        cwd=repo_dir,
        check=True,
        capture_output=True,
        text=True,
    )
    return result.stdout.strip()


def build_date() -> str:
    return datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


def env_value(env: Mapping[str, str], key: str) -> str:
    return env.get(key, "").strip()


def release_signing_config(
    env: Mapping[str, str] | None = None,
    system_name: str | None = None,
) -> ReleaseSigningConfig | None:
    env = env or os.environ
    normalized_system = (system_name or platform.system()).lower()

    if normalized_system != "darwin":
        return None

    certificate_base64 = env_value(env, "APPLE_CERTIFICATE")
    certificate_password = env_value(env, "APPLE_CERTIFICATE_PASSWORD")
    signing_identity = env_value(env, "APPLE_SIGNING_IDENTITY")

    if not certificate_base64 or not certificate_password:
        return None

    if not signing_identity.startswith("Developer ID Application:"):
        return None

    return ReleaseSigningConfig(
        certificate_base64=certificate_base64,
        certificate_password=certificate_password,
        signing_identity=signing_identity,
    )


def run_command(command: list[str], cwd: Path, env: Mapping[str, str] | None = None) -> None:
    subprocess.run(command, cwd=cwd, env=dict(env) if env is not None else None, check=True)


def capture_command(
    command: list[str],
    cwd: Path,
    env: Mapping[str, str] | None = None,
) -> str:
    result = subprocess.run(
        command,
        cwd=cwd,
        env=dict(env) if env is not None else None,
        capture_output=True,
        text=True,
        check=True,
    )
    return result.stdout


def parse_codesign_identity_reference(identity_output: str, preferred_identity: str) -> str:
    matches = IDENTITY_LINE_PATTERN.findall(identity_output)

    if not matches:
        raise RuntimeError("No valid codesigning identity found in imported keychain.")

    for fingerprint, label in matches:
        if label == preferred_identity:
            return fingerprint

    return matches[0][0]


def parse_keychain_paths(output: str) -> list[str]:
    paths: list[str] = []

    for line in output.splitlines():
        stripped = line.strip().strip('"')
        if stripped:
            paths.append(stripped)

    return paths


def current_user_keychains(cwd: Path) -> list[str]:
    output = capture_command(["security", "list-keychains", "-d", "user"], cwd)
    return parse_keychain_paths(output)


def current_default_keychain(cwd: Path) -> str | None:
    output = capture_command(["security", "default-keychain", "-d", "user"], cwd)
    paths = parse_keychain_paths(output)
    return paths[0] if paths else None


def codesign_command(
    binary_path: Path,
    keychain_path: Path,
    signing_identity_reference: str,
) -> list[str]:
    return [
        "codesign",
        "--force",
        "--sign",
        signing_identity_reference,
        "--keychain",
        str(keychain_path),
        "--timestamp",
        "--options",
        "runtime",
        "--verbose=4",
        str(binary_path),
    ]


def delete_temp_keychain(keychain_path: Path, cwd: Path) -> None:
    subprocess.run(
        ["security", "delete-keychain", str(keychain_path)],
        cwd=cwd,
        capture_output=True,
        text=True,
        check=False,
    )


def sign_packaged_binary(
    binary_path: Path,
    repo_dir: Path,
    config: ReleaseSigningConfig,
) -> None:
    with tempfile.TemporaryDirectory(prefix="agent-flows-bridge-signing-") as temp_dir_name:
        temp_dir = Path(temp_dir_name)
        certificate_path = temp_dir / "developer-id.p12"
        keychain_path = temp_dir / "agent-flows-bridge-signing.keychain-db"
        keychain_password = secrets.token_urlsafe(24)
        existing_keychains = current_user_keychains(repo_dir)
        default_keychain = current_default_keychain(repo_dir)

        certificate_path.write_bytes(base64.b64decode(config.certificate_base64))
        certificate_path.chmod(0o600)

        try:
            run_command(["security", "create-keychain", "-p", keychain_password, str(keychain_path)], repo_dir)
            run_command(["security", "set-keychain-settings", "-lut", "21600", str(keychain_path)], repo_dir)
            run_command(["security", "unlock-keychain", "-p", keychain_password, str(keychain_path)], repo_dir)
            run_command(
                [
                    "security",
                    "import",
                    str(certificate_path),
                    "-k",
                    str(keychain_path),
                    "-P",
                    config.certificate_password,
                    "-T",
                    "/usr/bin/codesign",
                    "-T",
                    "/usr/bin/security",
                ],
                repo_dir,
            )
            run_command(
                [
                    "security",
                    "set-key-partition-list",
                    "-S",
                    "apple-tool:,apple:",
                    "-s",
                    "-k",
                    keychain_password,
                    str(keychain_path),
                ],
                repo_dir,
            )

            updated_keychains = [str(keychain_path), *existing_keychains]
            run_command(["security", "list-keychains", "-d", "user", "-s", *updated_keychains], repo_dir)
            run_command(["security", "default-keychain", "-d", "user", "-s", str(keychain_path)], repo_dir)

            identity_output = capture_command(
                ["security", "find-identity", "-v", "-p", "codesigning", str(keychain_path)],
                repo_dir,
            )
            signing_identity_reference = parse_codesign_identity_reference(
                identity_output,
                config.signing_identity,
            )

            run_command(codesign_command(binary_path, keychain_path, signing_identity_reference), repo_dir)
            run_command(["codesign", "-dv", "--verbose=4", str(binary_path)], repo_dir)
            run_command(["codesign", "--verify", "--strict", "--verbose=4", str(binary_path)], repo_dir)
        finally:
            if existing_keychains:
                run_command(["security", "list-keychains", "-d", "user", "-s", *existing_keychains], repo_dir)

            if default_keychain:
                run_command(["security", "default-keychain", "-d", "user", "-s", default_keychain], repo_dir)

            delete_temp_keychain(keychain_path, repo_dir)


def build_bridge_binary(env: Mapping[str, str] | None = None) -> None:
    env = env or os.environ
    repo_dir = repo_root()
    output_dir = repo_dir / "desktop" / "generated-resources" / "bridge"
    output_dir.mkdir(parents=True, exist_ok=True)
    output_path = output_dir / binary_file_name()

    ldflags = [
        f"-X main.version={desktop_version(repo_dir)}",
        f"-X main.commit={git_commit(repo_dir)}",
        f"-X main.buildDate={build_date()}",
    ]

    command = [
        "go",
        "build",
        "-ldflags",
        " ".join(ldflags),
        "-o",
        str(output_path),
        "./cmd/agent-flows-bridge",
    ]

    command_env = dict(env)
    command_env.setdefault("GO111MODULE", "on")

    subprocess.run(command, cwd=repo_dir / "client", env=command_env, check=True)

    signing_config = release_signing_config(env=env)

    if signing_config is not None:
        sign_packaged_binary(output_path, repo_dir, signing_config)

    print(output_path)


if __name__ == "__main__":
    build_bridge_binary()
