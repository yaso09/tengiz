import json
import os
import shutil
import ssl
import subprocess
import urllib.error
import urllib.request
import zipfile
from dataclasses import dataclass, field
from typing import Callable, Optional

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
class BuildArtifact:
    id: int
    name: str
    size: int
    created_at: str
    head_sha: str
    head_branch: str
    expired: bool
    run_id: int


def _check_gh() -> bool:
    try:
        subprocess.run(
            ["gh", "--version"],
            capture_output=True,
            check=True,
        )
        return True
    except (subprocess.SubprocessError, FileNotFoundError):
        return False


_HAS_GH = _check_gh()


def _gh_api_json(endpoint: str) -> any:
    result = subprocess.run(
        ["gh", "api", endpoint],
        capture_output=True,
        text=True,
    )
    if result.returncode != 0:
        err = result.stderr.strip()
        raise RuntimeError(f"gh api error: {err}")
    return json.loads(result.stdout)


def _gh_api_raw(endpoint: str) -> bytes:
    result = subprocess.run(
        ["gh", "api", endpoint],
        capture_output=True,
    )
    if result.returncode != 0:
        err = result.stderr.decode().strip()
        raise RuntimeError(f"gh api error: {err}")
    return result.stdout


def _build_opener():
    ctx = ssl.create_default_context()
    ctx.check_hostname = False
    ctx.verify_mode = ssl.CERT_NONE
    return urllib.request.build_opener(urllib.request.HTTPSHandler(context=ctx))


def _request_json(url: str, token: Optional[str] = None) -> list | dict:
    opener = _build_opener()
    headers = {"Accept": "application/vnd.github.v3+json", "User-Agent": "tengiz-installer"}
    if token:
        headers["Authorization"] = f"token {token}"
    req = urllib.request.Request(url, headers=headers)
    try:
        with opener.open(req, timeout=30) as resp:
            return json.loads(resp.read().decode())
    except urllib.error.HTTPError as e:
        body = e.read().decode() if e.fp else ""
        raise RuntimeError(f"GitHub API error {e.code}: {body}")


def _parse_release(data: dict) -> Release:
    assets = [
        Asset(name=a["name"], url=a["browser_download_url"], size=a["size"])
        for a in data.get("assets", [])
    ]
    return Release(
        tag=data["tag_name"],
        name=data.get("name", "") or "",
        prerelease=data.get("prerelease", False),
        published_at=data.get("published_at", ""),
        assets=assets,
    )


def get_releases(token: Optional[str] = None) -> list[Release]:
    releases: list[Release] = []

    if _HAS_GH:
        try:
            data = _gh_api_json(f"repos/{REPO}/releases?per_page=100")
            if isinstance(data, list):
                for r in data:
                    releases.append(_parse_release(r))
                return releases
        except RuntimeError:
            pass

    page = 1
    while True:
        data = _request_json(f"{GH_API}/repos/{REPO}/releases?page={page}&per_page=100", token)
        if not data:
            break
        for r in data:
            releases.append(_parse_release(r))
        page += 1
    return releases


def get_latest_release(token: Optional[str] = None) -> Release:
    if _HAS_GH:
        try:
            return _parse_release(_gh_api_json(f"repos/{REPO}/releases/latest"))
        except RuntimeError:
            pass

    data = _request_json(f"{GH_API}/repos/{REPO}/releases/latest", token)
    return _parse_release(data)


def get_release_by_tag(tag: str, token: Optional[str] = None) -> Optional[Release]:
    if _HAS_GH:
        try:
            return _parse_release(_gh_api_json(f"repos/{REPO}/releases/tags/{tag}"))
        except RuntimeError:
            pass

    try:
        data = _request_json(f"{GH_API}/repos/{REPO}/releases/tags/{tag}", token)
    except RuntimeError:
        return None
    return _parse_release(data)


def get_artifacts(token: Optional[str] = None) -> list[BuildArtifact]:
    all_artifacts: list[BuildArtifact] = []

    if _HAS_GH:
        try:
            data = _gh_api_json(f"repos/{REPO}/actions/artifacts?per_page=50")
            entries = data.get("artifacts", []) if isinstance(data, dict) else []
            for a in entries:
                wf_run = a.get("workflow_run") or {}
                all_artifacts.append(BuildArtifact(
                    id=a["id"],
                    name=a["name"],
                    size=a["size_in_bytes"],
                    created_at=a.get("created_at", ""),
                    head_sha=wf_run.get("head_sha", ""),
                    head_branch=wf_run.get("head_branch", ""),
                    expired=a.get("expired", False),
                    run_id=wf_run.get("id", 0),
                ))
            return all_artifacts
        except RuntimeError:
            pass

    page = 1
    while True:
        data = _request_json(
            f"{GH_API}/repos/{REPO}/actions/artifacts?page={page}&per_page=50", token
        )
        entries = data.get("artifacts", []) if isinstance(data, dict) else []
        if not entries:
            break
        for a in entries:
            wf_run = a.get("workflow_run") or {}
            all_artifacts.append(BuildArtifact(
                id=a["id"],
                name=a["name"],
                size=a["size_in_bytes"],
                created_at=a.get("created_at", ""),
                head_sha=wf_run.get("head_sha", ""),
                head_branch=wf_run.get("head_branch", ""),
                expired=a.get("expired", False),
                run_id=wf_run.get("id", 0),
            ))
        page += 1
    return all_artifacts


def download_artifact_zip(artifact_id: int, dest: str, token: Optional[str] = None):
    if _HAS_GH:
        try:
            raw = _gh_api_raw(f"repos/{REPO}/actions/artifacts/{artifact_id}/zip")
            with open(dest, "wb") as f:
                f.write(raw)
            return
        except RuntimeError:
            pass

    ctx = _build_opener()
    headers = {
        "Accept": "application/vnd.github.v3+json",
        "User-Agent": "tengiz-installer",
    }
    if token:
        headers["Authorization"] = f"token {token}"
    req = urllib.request.Request(
        f"{GH_API}/repos/{REPO}/actions/artifacts/{artifact_id}/zip",
        headers=headers,
    )
    try:
        with ctx.open(req, timeout=120) as resp:
            with open(dest, "wb") as f:
                while True:
                    chunk = resp.read(8192)
                    if not chunk:
                        break
                    f.write(chunk)
    except urllib.error.HTTPError as e:
        body = e.read().decode() if e.fp else ""
        raise RuntimeError(f"Artifact download error {e.code}: {body}")


def extract_from_artifact(zip_path: str, target_binary: str, dest_dir: str) -> Optional[str]:
    with zipfile.ZipFile(zip_path, "r") as zf:
        names = zf.namelist()
        matched = [n for n in names if os.path.basename(n) == target_binary]
        if not matched:
            matched = [n for n in names if target_binary in n]
        if not matched:
            return None
        zf.extract(matched[0], dest_dir)
    extracted = os.path.join(dest_dir, matched[0])
    if os.path.exists(extracted):
        return extracted
    return None


def download_file(url: str, dest: str, progress: Optional[Callable[[float], None]] = None):
    ctx = _build_opener()
    req = urllib.request.Request(url, headers={"User-Agent": "tengiz-installer"})
    with ctx.open(req, timeout=120) as resp:
        total = int(resp.headers.get("Content-Length", 0))
        downloaded = 0
        with open(dest, "wb") as f:
            while True:
                chunk = resp.read(8192)
                if not chunk:
                    break
                f.write(chunk)
                downloaded += len(chunk)
                if progress and total:
                    progress(downloaded / total)
        if progress:
            progress(1.0)


def install_binary(src: str, dest: str) -> str:
    dest = os.path.abspath(dest)
    os.makedirs(os.path.dirname(dest), exist_ok=True)
    shutil.copy2(src, dest)
    os.chmod(dest, 0o755)
    return dest
