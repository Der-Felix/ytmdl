# ytmdl-frontend

Webfrontend für YTMDL: Künstler suchen, Diskografien ansehen, Downloads starten
und verfolgen, Bibliothek und Servereinstellungen einsehen.

Es ist eine reine Client-Anwendung. Alle Daten kommen aus der Go-API unter
`/api/v1`; eigenen Zustand hält es nur für die Dauer einer Sitzung.

## Stack

```text
Bun · React 19 · TypeScript · Vite · Tailwind CSS 4 · shadcn/ui · Base UI
```

Navigation läuft über die History API (`src/lib/router.tsx`), Datenzugriff über
`fetch` (`src/lib/api/`), Live-Fortschritt über die native `EventSource`
(`src/lib/sse/`). Es gibt bewusst keine Router-, State- oder HTTP-Bibliothek.

## Entwicklung

```sh
bun install
bun run dev
```

Der Dev-Server proxyt `/api` nach `http://127.0.0.1:8080`, weil das Backend
keine CORS-Header sendet und das Frontend deshalb same-origin sein muss. Ein
anderes Ziel lässt sich über `YTMDL_API_TARGET` setzen:

```sh
YTMDL_API_TARGET=http://192.168.1.10:8080 bun run dev
```

```sh
bun test        # Router, Formatierung, Adresserkennung
bun run build   # tsc -b && vite build
bun run lint    # oxlint
```

## Struktur

```text
src/
├── components/
│   ├── ui/          Basiskomponenten (shadcn/ui auf Base UI)
│   ├── layout/      Sidebar, Header, App-Shell
│   ├── music/       Cover, Künstler- und Release-Karten, Suche
│   └── downloads/   Job-Karten, Download-Aktionen, Diskografie-Dialog
├── pages/           Eine Datei je Ansicht
├── lib/
│   ├── api/         HTTP-Zugriff, je Ressource eine Datei
│   ├── sse/         Geteilte EventSource-Verbindung
│   ├── utils/       Formatierung, Adresserkennung
│   └── router.tsx   Navigation über die History API
├── hooks/           useAsync, useJobs
└── types/api.ts     Die Wire-Typen des Backends
```

Das Designsystem steht vollständig in `src/index.css`: Farb-Tokens, Radien, die
Hintergrund-Glows und die `panel`-Utilities. Farben oder Abstände gehören nicht
in einzelne Komponenten.

## Container

Zwei Stufen: Bun baut, `nginx:alpine` liefert aus. Der Webserver übernimmt den
SPA-Fallback und proxyt `/api/*` an `backend:8080` — Details zum Proxy, zur
DNS-Auflösung und zu den Cache-Headern stehen in
[../docs/DEPLOYMENT.md](../docs/DEPLOYMENT.md).

```sh
podman compose build frontend
podman compose up -d frontend
```
