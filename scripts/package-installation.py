#!/usr/bin/env python3
"""Package only public deployment inputs, never working-tree/runtime files."""
import argparse
import gzip
from pathlib import Path
import tarfile

FILES = (
    ".release-version", ".env.example", "compose.ghcr.yaml",
    "compose.ghcr.podman.yaml", "scripts/install.py", "INSTALL.md", "LICENSE",
)


def package(root, output):
    version = (root / ".release-version").read_text().strip()
    output.mkdir(parents=True, exist_ok=True)
    target = output / ("ytmdl-" + version + ".tar.gz")
    with target.open("wb") as raw, gzip.GzipFile(filename="", fileobj=raw, mode="wb", mtime=0) as compressed:
        with tarfile.open(fileobj=compressed, mode="w") as archive:
            for name in FILES:
                source = root / name
                if source.is_symlink() or not source.is_file():
                    raise RuntimeError("Deployment input must be a regular public file.")
                info = archive.gettarinfo(str(source), "ytmdl-" + version + "/" + name)
                info.uid = info.gid = info.mtime = 0
                info.uname = info.gname = ""
                info.mode = 0o755 if name.endswith(".py") else 0o644
                with source.open("rb") as handle:
                    archive.addfile(info, handle)
    print(target.name)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, default=Path("dist"))
    args = parser.parse_args()
    package(Path(__file__).resolve().parent.parent, args.output)
