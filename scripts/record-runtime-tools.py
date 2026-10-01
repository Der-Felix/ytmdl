#!/usr/bin/env python3
"""Record public tool versions from release images without provider requests."""
import argparse
import json
from pathlib import Path
import subprocess


def version(image, command):
    result = subprocess.run(["docker", "run", "--rm", "--network", "none",
                             "--entrypoint", command[0], image, *command[1:]],
                            capture_output=True, text=True, timeout=120)
    if result.returncode:
        raise RuntimeError("Runtime version probe failed.")
    lines = (result.stdout + result.stderr).strip().splitlines()
    if not lines:
        raise RuntimeError("Runtime version probe returned no evidence.")
    return lines[0]


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--backend", required=True)
    parser.add_argument("--frontend", required=True)
    parser.add_argument("--output", type=Path, default=Path("dist/runtime-tools.json"))
    args = parser.parse_args()
    tools = {name: version(args.backend, command) for name, command in (
        ("backend", ["/app/musicdl", "--version"]),
        ("yt-dlp", ["yt-dlp", "--version"]),
        ("ffmpeg", ["ffmpeg", "-version"]),
        ("ffprobe", ["ffprobe", "-version"]),
        ("deno", ["deno", "--version"]),
    )}
    tools["nginx"] = version(args.frontend, ["nginx", "-v"])
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(tools, indent=2) + "\n")
    print("PASS: runtime tool versions recorded without network access")
