#!/usr/bin/env python3
"""
Açık kaynaklı Vercel alternatiflerini yerel diske klonlar.

Kullanım:
    python clone_alternatives.py
    python clone_alternatives.py --dest ./sources --depth 1
    python clone_alternatives.py --keep-git          # .git klasörünü silme
    python clone_alternatives.py --workers 4          # paralel klon sayısı
"""

from __future__ import annotations

import argparse
import shutil
import subprocess
import sys
from concurrent.futures import ThreadPoolExecutor, as_completed
from dataclasses import dataclass
from pathlib import Path

REPOS = {
    "coolify": "https://github.com/coollabsio/coolify.git",
    "dokploy": "https://github.com/Dokploy/dokploy.git",
    "dokku": "https://github.com/dokku/dokku.git",
    "caprover": "https://github.com/caprover/caprover.git",
    "kamal": "https://github.com/basecamp/kamal.git",
    "komodo": "https://github.com/moghtech/komodo.git",
    "juno": "https://github.com/junobuild/juno.git",
}


@dataclass
class CloneResult:
    name: str
    ok: bool
    message: str


def check_git_available() -> None:
    if shutil.which("git") is None:
        sys.exit("Hata: 'git' komutu bulunamadı. Lütfen git'i kurup tekrar deneyin.")


def clone_repo(name: str, url: str, dest_root: Path, depth: int, keep_git: bool) -> CloneResult:
    target = dest_root / name

    if target.exists():
        return CloneResult(name, True, f"atlandı (zaten var): {target}")

    cmd = ["git", "clone"]
    if depth > 0:
        cmd += ["--depth", str(depth)]
    cmd += [url, str(target)]

    try:
        subprocess.run(
            cmd,
            check=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
        )
    except subprocess.CalledProcessError as e:
        return CloneResult(name, False, e.stderr.strip().splitlines()[-1] if e.stderr else str(e))

    if not keep_git:
        git_dir = target / ".git"
        if git_dir.exists():
            shutil.rmtree(git_dir, ignore_errors=True)

    return CloneResult(name, True, f"klonlandı: {target}")


def main() -> None:
    parser = argparse.ArgumentParser(description="Açık kaynaklı Vercel alternatiflerini klonla.")
    parser.add_argument(
        "--dest", type=Path, default=Path("sources"),
        help="Repoların klonlanacağı ana klasör (varsayılan: ./sources)",
    )
    parser.add_argument(
        "--depth", type=int, default=1,
        help="git clone --depth değeri; geçmişi tam çekmek için 0 verin (varsayılan: 1)",
    )
    parser.add_argument(
        "--keep-git", action="store_true",
        help="Klonlama sonrası .git klasörünü SİLME (varsayılan: siliniyor, RAG için gereksiz yer kaplıyor)",
    )
    parser.add_argument(
        "--workers", type=int, default=4,
        help="Paralel klonlama işçi sayısı (varsayılan: 4)",
    )
    args = parser.parse_args()

    check_git_available()
    args.dest.mkdir(parents=True, exist_ok=True)

    print(f"{len(REPOS)} repo, {args.workers} paralel işçi ile klonlanıyor -> {args.dest.resolve()}\n")

    results: list[CloneResult] = []
    with ThreadPoolExecutor(max_workers=args.workers) as executor:
        futures = {
            executor.submit(clone_repo, name, url, args.dest, args.depth, args.keep_git): name
            for name, url in REPOS.items()
        }
        for future in as_completed(futures):
            result = future.result()
            status = "✔" if result.ok else "✘"
            print(f"  {status} {result.name:10s} {result.message}")
            results.append(result)

    failed = [r for r in results if not r.ok]
    print(f"\nToplam: {len(results)} | Başarılı: {len(results) - len(failed)} | Başarısız: {len(failed)}")

    if failed:
        sys.exit(1)


if __name__ == "__main__":
    main()