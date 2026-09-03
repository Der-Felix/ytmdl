# Third-Party Notices & Licenses

This project incorporates and interacts with several third-party software components and libraries. Below is an overview of the key direct dependencies, external tools, and their respective licenses.

## Backend Dependencies (Go)

The backend is written in Go and utilizes the following direct third-party modules:

| Component | Purpose | License | Project URL |
| --------- | ------- | ------- | ----------- |
| `github.com/go-chi/chi/v5` | HTTP routing & middleware | MIT | https://github.com/go-chi/chi |
| `github.com/jackc/pgx/v5` | PostgreSQL driver and toolkit | MIT | https://github.com/jackc/pgx |
| `golang.org/x/sync` | Concurrency primitives & singleflight | BSD-3-Clause | https://pkg.go.dev/golang.org/x/sync |
| `gopkg.in/yaml.v3` | YAML parsing & configuration | MIT / Apache-2.0 | https://github.com/go-yaml/yaml |

## Frontend Dependencies (TypeScript / React)

The frontend web application utilizes the following direct production dependencies:

| Component | Purpose | License | Project URL |
| --------- | ------- | ------- | ----------- |
| `react`, `react-dom` | UI framework | MIT | https://react.dev |
| `@base-ui/react` | Accessible headless UI primitives | MIT | https://base-ui.com |
| `tailwindcss`, `@tailwindcss/vite` | CSS styling | MIT | https://tailwindcss.com |
| `lucide-react` | Icons | ISC | https://lucide.dev |
| `clsx`, `tailwind-merge` | Class name utilities | MIT | https://github.com/lukeed/clsx |
| `class-variance-authority` | Component variant styling | MIT | https://cva.style |
| `@fontsource-variable/geist` | Typography | MIT / SIL OFL | https://fontsource.org |

## Documentation Tooling

The official documentation site utilizes the following tooling:

| Component | Purpose | License | Project URL |
| --------- | ------- | ------- | ----------- |
| `vitepress` | Static documentation site generator | MIT | https://vitepress.dev |

## External Command-Line Utilities

YTMDL interacts with external multimedia utilities via standard operating system process execution (`exec.Command`). These tools are not statically or dynamically linked into the YTMDL Go binaries:

### yt-dlp
- **Purpose**: Media extraction and stream resolution.
- **License**: The Unlicense (Public Domain)
- **Project URL**: https://github.com/yt-dlp/yt-dlp
- **Interaction**: Invoked as a standalone subprocess (`/usr/bin/yt-dlp`).

### FFmpeg / FFprobe
- **Purpose**: Audio remuxing, stream extraction, and media format probing.
- **License**: GNU Lesser General Public License (LGPL) v2.1+ or GNU General Public License (GPL) v2.0+ (depending on build configurations and enabled external codecs).
- **Project URL**: https://ffmpeg.org
- **Interaction**: Invoked as independent subprocesses (`/usr/bin/ffmpeg`, `/usr/bin/ffprobe`). YTMDL does not link directly against FFmpeg C libraries (libavcodec, libavformat, etc.).
