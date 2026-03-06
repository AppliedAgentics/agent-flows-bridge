from __future__ import annotations

import json
import os
import platform
import subprocess
from datetime import datetime, timezone
from pathlib import Path


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


def build_bridge_binary() -> None:
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

    env = os.environ.copy()
    env.setdefault("GO111MODULE", "on")

    subprocess.run(command, cwd=repo_dir / "client", env=env, check=True)
    print(output_path)


if __name__ == "__main__":
    build_bridge_binary()
