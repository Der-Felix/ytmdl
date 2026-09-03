# Library Management & Integrity Audits

YTMDL organizes music into a media-server-ready folder hierarchy with strict metadata standards.

## Directory Layout

```text
/music/
├── Pink Floyd/
│   ├── 1973 - The Dark Side of the Moon/
│   │   ├── 01 - Speak to Me.opus
│   │   ├── 01 - Speak to Me.lrc
│   │   ├── ...
│   │   └── cover.jpg
```

- Multi-disc releases use hierarchical numbering (`101 - Track.opus`, `201 - Track.opus`).
- Standard Vorbis comments are embedded directly into each file (TITLE, ARTIST, ALBUM, ALBUMARTIST, DATE, TRACKNUMBER, DISCNUMBER).

## Non-Destructive Auditing Engine

- **Quick Audit:** Validates folder structures, missing cover art, and missing lyrics sidecars.
- **Deep Audit:** Verifies Vorbis tag consistency, embedded art dimensions, and audio stream integrity via `ffprobe`.
- **Repair Preview:** Generates an atomic diff preview before renaming files or rewriting tag blocks. Destructive actions require Administrator privileges.
