import asyncio
import json
import os
import platform
import shutil
import subprocess
import sys
import tempfile
import zipfile
from dataclasses import dataclass, field
from pathlib import Path
from typing import Callable, Optional

import httpx

REPO = "yaso09/tengiz"
GH_API = "https://api.github.com"


@dataclass
class Asset:
    name: str
    url: str
    size: int


@dataclass
class Release:
    tag: str
    name: str
    prerelease: bool
    published_at: str
    assets: list[Asset] = field(default_factory=list)


@dataclass
class CommitArtifact:
    id: int
    name: str
    size: int
    created_at: str
    head_sha: str
    head_branch: str
    expired: bool


def detect_os_arch() -> tuple[str, str]:
    system = platform.system().lower()
    machine = platform.machine().lower()
    os_map = {"linux": "linux", "darwin": "darwin", "windows": "windows"}
    arch_map = {"x86_64": "amd64", "amd64": "amd64", "arm64": "arm64", "aarch64": "arm64"}
    return os_map.get(system, system), arch_map.get(machine, machine)


def binary_name(os_name: str, arch: str) -> str:
    ext = ".exe" if os_name == "windows" else ""
    return f"tengiz-{os_name}-{arch}{ext}"


def suggest_install_path() -> str:
    system = platform.system().lower()
    if system == "windows":
        return str(Path.home() / ".tengiz" / "bin" / "tengiz.exe")
    return str(Path.home() / ".local" / "bin" / "tengiz")


def is_in_path(dirpath: str) -> bool:
    dirpath = os.path.abspath(dirpath)
    for p in os.environ.get("PATH", "").split(os.pathsep):
        if os.path.abspath(p.strip()) == dirpath:
            return True
    return False


def check_gh() -> bool:
    try:
        subprocess.run(["gh", "--version"], capture_output=True, check=True)
        return True
    except (subprocess.SubprocessError, FileNotFoundError):
        return False


class GitHub:
    def __init__(self):
        self.token = os.environ.get("GH_TOKEN") or os.environ.get("GITHUB_TOKEN")
        self._client: Optional[httpx.AsyncClient] = None
        self._gh = check_gh()

    async def _ensure_client(self):
        if self._client is None:
            headers = {"Accept": "application/vnd.github.v3+json"}
            if self.token:
                headers["Authorization"] = f"token {self.token}"
            self._client = httpx.AsyncClient(base_url=GH_API, headers=headers, timeout=30)

    async def close(self):
        if self._client:
            await self._client.aclose()

    async def get_releases(self) -> list[Release]:
        releases = []

        if self._gh:
            result = subprocess.run(
                ["gh", "api", f"repos/{REPO}/releases", "--jq",
                 '.[] | {tag: .tag_name, name: .name, prerelease: .prerelease, published_at: .published_at, assets: [.assets[] | {name: .name, url: .browser_download_url, size: .size}]}'],
                capture_output=True, text=True
            )
            if result.returncode == 0:
                for line in result.stdout.strip().split("\n"):
                    line = line.strip()
                    if line:
                        try:
                            data = json.loads(line)
                            releases.append(Release(**data))
                        except json.JSONDecodeError:
                            continue
                return releases

        await self._ensure_client()
        page = 1
        while True:
            resp = await self._client.get(f"/repos/{REPO}/releases", params={"page": page, "per_page": 100})
            if resp.status_code != 200:
                break
            data = resp.json()
            if not data:
                break
            for r in data:
                assets = [Asset(name=a["name"], url=a["browser_download_url"], size=a["size"]) for a in r["assets"]]
                releases.append(Release(
                    tag=r["tag_name"],
                    name=r.get("name", "") or "",
                    prerelease=r.get("prerelease", False),
                    published_at=r["published_at"],
                    assets=assets,
                ))
            page += 1
        return releases

    async def get_artifacts(self) -> list[CommitArtifact]:
        if self._gh:
            result = subprocess.run(
                ["gh", "api", f"repos/{REPO}/actions/artifacts", "--jq",
                 '.artifacts[] | {id: .id, name: .name, size: .size_in_bytes, created_at: .created_at, head_sha: .workflow_run.head_sha, head_branch: .workflow_run.head_branch, expired: .expired}'],
                capture_output=True, text=True
            )
            if result.returncode == 0:
                artifacts = []
                for line in result.stdout.strip().split("\n"):
                    line = line.strip()
                    if line:
                        try:
                            data = json.loads(line)
                            artifacts.append(CommitArtifact(**data))
                        except json.JSONDecodeError:
                            continue
                return artifacts
            return []

        if self.token:
            await self._ensure_client()
            resp = await self._client.get(f"/repos/{REPO}/actions/artifacts", params={"per_page": 50})
            if resp.status_code == 200:
                data = resp.json()
                return [CommitArtifact(
                    id=a["id"],
                    name=a["name"],
                    size=a["size_in_bytes"],
                    created_at=a["created_at"],
                    head_sha=a["workflow_run"]["head_sha"],
                    head_branch=a["workflow_run"]["head_branch"],
                    expired=a["expired"],
                ) for a in data.get("artifacts", [])]

        return []

    async def download_release_asset(self, url: str, dest: str, progress: Optional[Callable] = None):
        async with httpx.AsyncClient(timeout=60) as client:
            async with client.stream("GET", url, follow_redirects=True) as resp:
                resp.raise_for_status()
                total = int(resp.headers.get("content-length", 0))
                downloaded = 0
                with open(dest, "wb") as f:
                    async for chunk in resp.aiter_bytes():
                        f.write(chunk)
                        downloaded += len(chunk)
                        if progress and total:
                            progress(downloaded / total)
                if progress:
                    progress(1.0)

    async def download_artifact_zip(self, artifact_id: int, dest: str):
        if self._gh:
            result = subprocess.run(
                ["gh", "api", f"repos/{REPO}/actions/artifacts/{artifact_id}/zip"],
                capture_output=True,
            )
            if result.returncode == 0:
                with open(dest, "wb") as f:
                    f.write(result.stdout)
                return True

        if self.token:
            await self._ensure_client()
            resp = await self._client.get(f"/repos/{REPO}/actions/artifacts/{artifact_id}/zip")
            if resp.status_code == 200:
                with open(dest, "wb") as f:
                    f.write(resp.content)
                return True

        return False

    async def download_and_extract_artifact(self, artifact_id: int, binary: str, dest_dir: str,
                                             progress: Optional[Callable] = None) -> Optional[str]:
        zip_path = os.path.join(dest_dir, f"artifact-{artifact_id}.zip")
        try:
            ok = await self.download_artifact_zip(artifact_id, zip_path)
            if not ok:
                return None
            if progress:
                progress(0.8)
            with zipfile.ZipFile(zip_path, "r") as zf:
                names = zf.namelist()
                matched = [n for n in names if os.path.basename(n) == binary]
                if not matched:
                    matched = [n for n in names if binary in n]
                if not matched:
                    return None
                zf.extract(matched[0], dest_dir)
            if progress:
                progress(1.0)
            extracted = os.path.join(dest_dir, matched[0])
            if not os.path.exists(extracted):
                return None
            return extracted
        finally:
            if os.path.exists(zip_path):
                os.remove(zip_path)

    def install_binary(self, src: str, dest: str) -> str:
        dest = os.path.abspath(dest)
        os.makedirs(os.path.dirname(dest), exist_ok=True)
        shutil.copy2(src, dest)
        os.chmod(dest, 0o755)
        return dest
