import os
import tempfile
from typing import Callable, Optional

from .github import (
    BuildArtifact,
    download_artifact_zip,
    download_file,
    extract_from_artifact,
    get_artifacts,
    get_release_by_tag,
    get_releases,
    install_binary,
)
from .platform import add_to_path, binary_name, detect_os_arch, is_in_path, suggest_install_path


def resolve_release(version: Optional[str], token: Optional[str]):
    if version:
        release = get_release_by_tag(version, token)
        if not release:
            all_releases = get_releases(token)
            for r in all_releases:
                if r.tag == version:
                    release = r
                    break
            if not release:
                raise RuntimeError(f"Version '{version}' not found")
        return release

    all_releases = get_releases(token)
    if all_releases:
        return all_releases[0]
    try:
        from .github import get_latest_release as _glr
        return _glr(token)
    except RuntimeError:
        raise RuntimeError(
            "No releases found in yaso09/tengiz. "
            "The repository may have no releases yet."
        )


def find_matching_asset(release, os_name, arch):
    target = binary_name(os_name, arch)
    for asset in release.assets:
        if asset.name == target:
            return (asset, target)
    for asset in release.assets:
        if os_name in asset.name and arch in asset.name:
            return (asset, target)
    return None


def resolve_artifact(
    os_name: str,
    arch: str,
    token: Optional[str],
) -> tuple[BuildArtifact, str]:
    target = binary_name(os_name, arch)
    target_noext = target.rsplit(".", 1)[0] if "." in target else target
    artifacts = get_artifacts(token)
    if not artifacts:
        raise RuntimeError(
            "No CI build artifacts found. "
            "Set GH_TOKEN or GITHUB_TOKEN for authentication."
        )

    build_artifacts = [
        a for a in artifacts
        if (a.name == target or a.name == target_noext) and not a.expired
    ]
    if not build_artifacts:
        all_names = sorted(set(a.name for a in artifacts))
        raise RuntimeError(
            f"No matching CI artifact for {os_name}/{arch}. "
            f"Available: {', '.join(all_names) or 'none'}"
        )

    build_artifacts.sort(key=lambda a: a.created_at, reverse=True)
    return build_artifacts[0], target


def download_and_install_from_release(
    version: Optional[str],
    os_name: str,
    arch: str,
    dest: str,
    dry_run: bool,
    token: Optional[str],
    progress_cb: Optional[Callable[[float], None]] = None,
    no_path: bool = False,
) -> dict:
    release = resolve_release(version, token)
    match = find_matching_asset(release, os_name, arch)
    if not match:
        all_names = [a.name for a in release.assets]
        raise RuntimeError(
            f"No binary for {os_name}/{arch} in release {release.tag}. "
            f"Available: {', '.join(all_names) or 'none'}"
        )

    asset, target_name = match
    install_path = dest or suggest_install_path()

    parent = os.path.dirname(install_path)

    result = {
        "source": "release",
        "version": release.tag,
        "asset": asset.name,
        "size": asset.size,
        "dest": install_path,
        "dry_run": dry_run,
        "in_path": is_in_path(parent),
        "path_added": False,
    }

    if dry_run:
        return result

    tmp_dir = tempfile.mkdtemp(prefix="tengiz-")
    tmp_path = os.path.join(tmp_dir, target_name)
    try:
        download_file(asset.url, tmp_path, progress_cb)
        install_binary(tmp_path, install_path)
        if not no_path and not result["in_path"]:
            result["path_added"] = add_to_path(parent)
            result["in_path"] = is_in_path(parent)
        return result
    finally:
        if os.path.exists(tmp_dir):
            import shutil
            shutil.rmtree(tmp_dir, ignore_errors=True)


def download_and_install_from_ci(
    os_name: str,
    arch: str,
    dest: str,
    dry_run: bool,
    token: Optional[str],
    progress_cb: Optional[Callable[[float], None]] = None,
    no_path: bool = False,
) -> dict:
    artifact, target_name = resolve_artifact(os_name, arch, token)
    install_path = dest or suggest_install_path()

    parent = os.path.dirname(install_path)

    result = {
        "source": "ci",
        "version": f"{artifact.head_sha[:8]} ({artifact.head_branch})",
        "asset": artifact.name,
        "size": artifact.size,
        "run_id": artifact.run_id,
        "dest": install_path,
        "dry_run": dry_run,
        "in_path": is_in_path(parent),
        "path_added": False,
    }

    if dry_run:
        return result

    tmp_dir = tempfile.mkdtemp(prefix="tengiz-")
    zip_path = os.path.join(tmp_dir, f"artifact-{artifact.id}.zip")
    try:
        download_artifact_zip(artifact.id, zip_path, token)
        extracted = extract_from_artifact(zip_path, target_name, tmp_dir)
        if not extracted:
            raise RuntimeError(
                f"Binary '{target_name}' not found in artifact zip"
            )
        install_binary(extracted, install_path)
        if not no_path and not result["in_path"]:
            result["path_added"] = add_to_path(parent)
            result["in_path"] = is_in_path(parent)
        return result
    finally:
        if os.path.exists(tmp_dir):
            import shutil
            shutil.rmtree(tmp_dir, ignore_errors=True)
