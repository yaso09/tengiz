import os
import platform
import sys
from pathlib import Path


def detect_os_arch() -> tuple[str, str]:
    system = platform.system().lower()
    machine = platform.machine().lower()

    os_map = {
        "linux": "linux",
        "darwin": "darwin",
        "windows": "windows",
    }
    arch_map = {
        "x86_64": "amd64",
        "amd64": "amd64",
        "arm64": "arm64",
        "aarch64": "arm64",
    }
    return os_map.get(system, system), arch_map.get(machine, machine)


def binary_name(os_name: str, arch: str) -> str:
    ext = ".exe" if os_name == "windows" else ""
    return f"tengiz-{os_name}-{arch}{ext}"


def suggest_install_path() -> str:
    system = platform.system().lower()
    home = Path.home()

    if system == "windows":
        return str(home / ".tengiz" / "bin" / "tengiz.exe")

    return str(home / ".local" / "bin" / "tengiz")


def is_in_path(dirpath: str) -> bool:
    dirpath = os.path.abspath(dirpath)
    for p in os.environ.get("PATH", "").split(os.pathsep):
        if os.path.abspath(p.strip()) == dirpath:
            return True
    return False


def add_to_path(dirpath: str) -> bool:
    dirpath = os.path.abspath(dirpath)
    if is_in_path(dirpath):
        return True

    system = platform.system().lower()
    if system == "windows":
        return _add_to_path_windows(dirpath)

    return _add_to_path_unix(dirpath)


def _add_to_path_windows(dirpath: str) -> bool:
    import subprocess
    current = os.environ.get("PATH", "")
    new_val = f"{dirpath};{current}"
    try:
        subprocess.run(
            ["setx", "PATH", new_val],
            capture_output=True, check=True,
        )
        return True
    except (subprocess.SubprocessError, FileNotFoundError):
        return False


def _add_to_path_unix(dirpath: str) -> bool:
    export_line = f'\nexport PATH="$PATH:{dirpath}"\n'
    home = Path.home()
    candidates = [
        home / ".zshrc",
        home / ".bashrc",
        home / ".bash_profile",
        home / ".profile",
    ]
    for rc in candidates:
        if rc.exists():
            try:
                content = rc.read_text()
                if dirpath not in content:
                    with rc.open("a") as f:
                        f.write(export_line)
                return True
            except OSError:
                continue

    try:
        with (home / ".profile").open("a") as f:
            f.write(export_line)
        return True
    except OSError:
        return False
