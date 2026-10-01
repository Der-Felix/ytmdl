#!/usr/bin/python3
"""Fixture process boundary: metadata and a generated tone; no network calls."""
import json
from pathlib import Path
import shutil
import sys

args = sys.argv[1:]
if "--version" in args:
    print("2026.08.19")
elif "-o" in args:
    output = Path(args[args.index("-o") + 1].replace("%(ext)s", "flac"))
    output.parent.mkdir(parents=True, exist_ok=True)
    shutil.copyfile("/fixtures/tone.flac", output)
    print(str(output))
else:
    print(json.dumps({
        "id": "fixture0001", "title": "Qualification Artist - Qualification Tone",
        "track": "Qualification Tone", "artist": "Qualification Artist",
        "uploader": "Qualification Artist", "channel": "Qualification Artist",
        "duration": 4, "ext": "flac", "acodec": "flac", "vcodec": "none",
        "webpage_url": "https://www.youtube.com/watch?v=fixture0001",
        "url": "https://example.com/qualification.flac",
        "formats": [{"format_id": "audio-fixture", "url": "https://example.com/qualification.flac",
                     "ext": "flac", "acodec": "flac", "vcodec": "none", "abr": 320}],
    }))
