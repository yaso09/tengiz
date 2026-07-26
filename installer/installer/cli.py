import argparse
import os
import sys

from .core import download_and_install_from_ci, download_and_install_from_release
from .github import get_artifacts, get_releases
from .platform import binary_name, detect_os_arch, suggest_install_path


def fmt_size(size: int) -> str:
    if size < 1024 * 1024:
        return f"{size / 1024:.0f} KB"
    return f"{size / (1024 * 1024):.1f} MB"


def _get_token() -> str | None:
    return os.environ.get("GH_TOKEN") or os.environ.get("GITHUB_TOKEN")


def cmd_list_versions(args, token):
    releases = get_releases(token)
    for r in releases:
        tag = r.tag
        prefix = ""
        if r.prerelease:
            prefix = "[pre-release] "
        date = r.published_at[:10] if r.published_at else "?"
        print(f"{tag:20s} {prefix}{r.name} ({date})")


def cmd_list_assets(args, token):
    releases = get_releases(token)
    if args.tag:
        releases = [r for r in releases if r.tag == args.tag]
    if not releases:
        print(f"No releases found matching '{args.tag}'" if args.tag else "No releases found")
        return
    os_name, arch = detect_os_arch()
    target = binary_name(os_name, arch)
    for r in releases[:5]:
        print(f"\n{r.tag}:")
        for a in r.assets:
            marker = " <-- " if a.name == target else ""
            print(f"  {a.name:40s} {fmt_size(a.size):>8s}{marker}")


def cmd_list_artifacts(args, token):
    artifacts = get_artifacts(token)
    if not artifacts:
        print("No CI build artifacts found.")
        print("Set GH_TOKEN or GITHUB_TOKEN for authentication.")
        return

    os_name, arch = detect_os_arch()
    target = binary_name(os_name, arch)
    header = f"{'Branch':20s} {'SHA':10s} {'Artifact':30s} {'Size':>8s}  Status"
    print(header)
    print("-" * len(header))
    for a in artifacts:
        marker = " <--" if a.name == target else ""
        label = "expired" if a.expired else "active"
        print(
            f"{a.head_branch:20s} {a.head_sha[:8]:10s} "
            f"{a.name:30s} {fmt_size(a.size):>8s}  {label}{marker}"
        )


def _progress():
    bar_len = 30

    def inner(pct):
        filled = int(bar_len * pct)
        bar = "█" * filled + "─" * (bar_len - filled)
        sys.stdout.write(f"\r  [{bar}] {int(pct * 100)}%")
        sys.stdout.flush()
        if pct >= 1.0:
            sys.stdout.write("\n")

    return inner


def cmd_install(args):
    token = _get_token()
    os_name = args.os
    arch = args.arch

    if os_name is None or arch is None:
        detected_os, detected_arch = detect_os_arch()
        os_name = os_name or detected_os
        arch = arch or detected_arch

    try:
        if args.ci:
            result = download_and_install_from_ci(
                os_name=os_name,
                arch=arch,
                dest=args.dest,
                dry_run=args.dry_run,
                no_path=args.no_path,
                token=token,
                progress_cb=_progress(),
            )
        else:
            result = download_and_install_from_release(
                version=args.version,
                os_name=os_name,
                arch=arch,
                dest=args.dest,
                dry_run=args.dry_run,
                no_path=args.no_path,
                token=token,
                progress_cb=_progress(),
            )

        if result.get("dry_run"):
            source = result.get("source", "release")
            print(f"  Source:   {source}")
            if source == "ci":
                print(f"  Build:    {result.get('version', '?')}")
                print(f"  Run ID:   {result.get('run_id', '?')}")
            else:
                print(f"  Version:  {result.get('version', '?')}")
            print(f"  Asset:    {result.get('asset', '?')} ({fmt_size(result.get('size', 0))})")
            print(f"  Dest:     {result.get('dest', '?')}")
            return

        print(f"\n  Installed: {result['dest']}")
        if result.get("in_path"):
            print("  PATH:     [OK] already in PATH")
        else:
            parent = os.path.dirname(result["dest"])
            if result.get("path_added"):
                print(f"  PATH:     [OK] added {parent} to PATH")
                print("           (restart terminal to apply)")
            else:
                print(f"  PATH:     [add {parent} to PATH]")

    except RuntimeError as e:
        print(f"  Error: {e}", file=sys.stderr)
        sys.exit(1)


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="tengiz-installer",
        description="Install Tengiz — an open-source Vercel alternative",
    )

    source = parser.add_argument_group("source")
    source.add_argument("--version", "-v", help="Release tag to install (default: latest)")
    source.add_argument("--ci", action="store_true", help="Install from CI build artifacts instead of releases")

    info = parser.add_argument_group("info")
    info.add_argument("--list", "-l", action="store_true", help="List versions (or artifacts with --ci)")
    info.add_argument("--list-versions", action="store_true", help="List available releases")
    info.add_argument("--list-assets", nargs="?", const="", metavar="TAG",
                      help="List assets in releases (optional: filter by tag)")
    info.add_argument("--list-artifacts", action="store_true", help="List CI build artifacts")

    opts = parser.add_argument_group("options")
    opts.add_argument("--os", help="Override OS detection (linux, darwin, windows)")
    opts.add_argument("--arch", help="Override arch detection (amd64, arm64)")
    opts.add_argument("--dest", help=f"Install path (default: {suggest_install_path()})")
    opts.add_argument("--no-path", action="store_true", help="Skip adding install directory to PATH")
    opts.add_argument("--dry-run", action="store_true", help="Show what would be done without downloading")

    return parser


def main(argv: list[str] | None = None) -> int:
    parser = build_parser()
    args = parser.parse_args(argv)
    token = _get_token()

    try:
        if args.list_artifacts or (args.list and args.ci):
            cmd_list_artifacts(args, token)
        elif args.list or args.list_versions:
            cmd_list_versions(args, token)
        elif args.list_assets is not None:
            cmd_list_assets(args, token)
        else:
            cmd_install(args)
    except RuntimeError as e:
        print(f"  Error: {e}", file=sys.stderr)
        return 1

    return 0


if __name__ == "__main__":
    sys.exit(main())
