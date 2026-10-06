# iPhone-Funktionsprüfung — 0.2.0, Build 27

Stand: 5. Oktober 2026. Prüfung des nativen Clients und seiner API-Verträge;
isolierte temporäre Daten, Loopback-Server und iPhone-Simulator mit iOS 27.
Die Server-Kompatibilitätsbasis ist der geprüfte Router von `v1.2.0`.
Produktionskonten, Musik und Playlists wurden für diese Prüfung nicht verändert.
Dieser Bericht ist keine Freigabe als fehlerfreie oder vollständige Spotify/Plexamp-App.

## Ergebnisse und verbleibende Grenzen

**Geprüft** bedeutet hier die jeweils genannte automatisierte Prüfung, nicht eine
pauschale Bestätigung auf dem echten iPhone. **Gerätetest offen** ist eine
Release-Voraussetzung, kein verstecktes erfolgreiches Testergebnis.

| Bereich | Ergebnis | Nachweis / noch offen |
| --- | --- | --- |
| Anmeldung, falsches Passwort, abgelaufene Sitzung | API-Tests bestanden | CSRF, Cookie-Übernahme, eigene Fehlermeldungen, unsichere Ursprünge; erneute Anmeldung auf dem Gerät offen |
| Server- und Kontowechsel, Abmelden | Fehler korrigiert, Tests bestanden | Filter und Geräteübergabe zurückgesetzt; verspätetes Abmelden kann neue Verbindung nicht löschen; Offline-Dateien bleiben erhalten |
| Start, Bibliothek und Navigation | Simulator bestanden | Tabs, geöffnete Playlist, Player öffnen/schließen; schmale Bibliotheksverknüpfungen umbrechen als ganze Elemente |
| Suche | Simulator bestanden | Eingabe, Ergebnisse und Wechsel aus Favoriten; Antworten aus alten Verbindungen werden verworfen; reale Netzlatenz offen |
| Künstler, Alben, Nachladen und Genre | Code geprüft; Bibliotheks-UI bestanden | Künstler haben eigenes Nachladen, Alben und Titel Seiten; Filter werden nach Kontowechsel zurückgesetzt. Große reale Bibliothek und alle Filterkombinationen offen |
| Cover im Client | Fixture-UI und Bildtests bestanden | Authentifizierte Bildanfragen, gültige/ungültige Bilder und lokale Cover; fehlende Serverbilder zeigen Platzhalter |
| Favoriten | Fehler korrigiert, API- und UI-Tests bestanden | Parallele Klicks werden während einer Mutation gesperrt; entfernte Favoriten verschwinden sofort auch auf iOS |
| Playlist anlegen/ändern/löschen | API-Mutationstests bestanden | Name, Beschreibung, Regeln, Löschen und Fehlerfälle; keine Produktionsplaylist als Test benutzt |
| Playlist hinzufügen/entfernen/ordnen | API-Mutationstests bestanden | Geordnete IDs, 100er Blöcke, Teilerfolg und Kontowechsel; Touch-Abläufe für jeden Unterpunkt auf dem Gerät offen |
| Intelligente Playlists | API- und Simulator-Tests bestanden | Favoritenregel gespeichert und beim erneuten Öffnen ausgewertet; Fehler beim Speichern bleibt sichtbar und ist erneut versuchbar. Weitere Regelkombinationen im Live-Betrieb offen |
| Titel laden, Pause und Player schließen | Simulator bestanden | Wirklich laufender AVPlayer mit ausschließlich Null-PCM; erreichbare Transporttaste und fortschreitende Zeit. Reale Stream-Startzeiten offen |
| Mini-Player | Simulator bestanden | Über der Tab-Leiste, auch in geöffneter Playlist; große Player-Ansicht über Titel oder Toolbar erreichbar |
| Leere Warteschlange / Stop | Fehler korrigiert, Regression bestanden | Stop entfernt Cover, System-Metadaten und Wiedergabeabsicht statt ein altes Lied anzuzeigen |
| Warteschlange, Zufall, Wiederholung | Zustands-/Queue-Tests bestanden | Begrenzung, aktuelle Instanz erhalten, Bearbeitung und Duplikate; lange mobile Listen rendern ihre Zeilen bei Bedarf. Alle Transportabläufe am Gerät offen |
| Titeldauer | Regression bestanden | Bibliotheksdauer hat Vorrang vor überhöhten Stream-Schätzungen; unbekannte Dauer nutzt gültigen Stream-Wert |
| Sperrbildschirm-Cover | Fehler korrigiert, Metadaten-Tests bestanden | `MPMediaItemArtwork` wird sofort nach Bildladen publiziert, bei Zeit-/Pause-Updates erhalten, bei Titelwechsel/Stop verworfen; auch Offline-Cover getestet. Sichtbare Darstellung im echten Sperrbildschirm offen |
| Opus, AAC/M4A, FLAC, MP3 | iOS-Simulator-Decodertest bestanden | Vollständiges Decodieren synthetischer stummer Dateien mit AVAssetReader; keine Tonwiedergabe. Das qualifiziert keine beliebigen beschädigten Dateien oder jeden HTTP-Stream |
| Lautstärke und AirPlay | Native Steuerung eingebunden; Gerätetest offen | iOS-Systemlautstärke über MPVolumeView, AirPlay-Auswahl über native Oberfläche; tatsächliche Audioausgabe nicht ferngesteuert getestet |
| Offline speichern und anzeigen | Transfer- und Simulator-Tests bestanden | Authentifizierter stummer WAV-Download, Fortschritt bis verfügbar, Cover/Lyrics, lokale Audioquelle und Navigation |
| Offline-Neustart, Kontentrennung, lokales Entfernen | Dateiregressionen bestanden | Manifest nach Neustart, lokale Dateien ohne Login, fremde Konten getrennt, Symlinks/HTML/Teiltransfers abgewiesen. Echter Flugmodus mit App-Neustart offen |
| Hintergrunddownload, WLAN, Pause/Retry, Speicherlimit | Code- und Importprüfungen | Herkunft, Versuch-ID und Budget geprüft; Hintergrund-URLSession registriert. Suspendierung, System-Wake, Netzwechsel und echte Speicherknappheit am Gerät offen |
| EQ, Vorverstärker, Pegelschutz | CPU- und Einstellungsprüfungen bestanden | Frequenzantwort, Bypass, Kanaltrennung, Schutz und gespeicherte Grenzen. Audio-Tap auf dem echten Ausgabegerät offen |
| Überblendung, Album-Schutz, Tempo, Sleep-Timer | Implementiert; Gerätetest offen | Vorhandene AVPlayer-Audiotests wurden wegen möglicher hörbarer Testsignale ausdrücklich nicht ausgeführt; diese Funktionen erhalten hier keine neue Audio-Freigabe |
| Visualizer | CPU-FFT und Einstellungen geprüft | Bass/Höhen getrennt, Stille/Unterbrechung, vier mobile Stile, Farben, Cover-Platzierung, Reduce Motion; sichtbare Reaktion während realer Wiedergabe offen |
| Lyrics | Parser-/Cache-Tests bestanden | LRC-Zeiten, Zeilenauswahl, Offline-Text; Tippen/automatisches Folgen bei echten Songs offen |
| Hörverlauf und Queue-Wiederherstellung | Zustandsprüfungen bestanden | Begrenzte, kontogebundene Daten, ausstehende idempotente Ereignisse, deaktivierbarer Verlauf; tatsächlicher Server-Sync am Gerät offen |
| Song-/Genre-Radio und Autoplay | Implementiert, Server-Verträge geprüft | Metadatenempfehlungen aus eigener Bibliothek, Offline-Autoplay aus lokalen Dateien; reale Empfehlungsgüte und Übergänge offen |
| Geräteübergabe | Payload geprüft; Gerätetest offen | Explizites Speichern/Pausieren und Annehmen, kein automatischer Start; angebotene fremde Übergabe wird bei Kontowechsel entfernt |
| Apple-TV-Gerätecode | Benötigt zusätzlichen Backend-Stand | Gerätecode-Routen sind nicht in `v1.2.0`. Ein 404 zeigt jetzt eine verständliche Meldung. Normale Anmeldung bleibt verfügbar |
| Themes und Einstellungen | Navigation und Zustandsprüfungen | System/hell/dunkel, Akzentfarben und lokale Speicherung; sämtliche Kombinationen mit großer Schrift/VoiceOver offen |
| CarPlay, Siri, Widgets, Watch, echtes Gapless, Lautheitsnormalisierung | Nicht implementiert / nicht zugesagt | EQ-Pegelschutz ist keine Lautheitsnormalisierung; keine vollständige Produkt-Parität behaupten |

## Konkrete Korrekturen

- Cover an Apples Now-Playing-Center übergeben, inklusive asynchronem Laden,
  Offline-Cover und Schutz vor verspäteten Bildern früherer Titel.
- Filter, Nachladezustand, offene Favoritenaktionen und Geräteübergabe bei
  Verbindungswechsel vollständig zurücksetzen.
- Verspätete Abmelde-, Such-, Künstler- und Radioantworten gegen die aktuelle
  Verbindung prüfen. Fehler beim lokalen Entfernen der Sitzung weiter melden.
- Favoritenänderungen pro Titel gegen schnelle Mehrfachklicks schützen und die
  iOS-Favoritenliste unmittelbar aktualisieren.
- Ladefehler einer Sammlung nicht mehr als leere Bibliothek ausgeben;
  erneutes Laden und Aktualisieren anbieten.
- Leere Queue beendet Wiedergabe und Systemanzeige vollständig.
- Transportabstände an schmale Ansichten anpassen, Bibliotheksbuttons nicht
  mitten im Wort umbrechen und lange Queues mit LazyVStack darstellen.
- Nicht unterstützte Gerätecode-Prüfung verständlich erklären.

## Prüfungen reproduzieren

24 Swift-Tests bestanden, inklusive des authentifizierten Offline-Transfers.
8 iPhone-UI-/Decodertests und 2 iPad-Navigations-/Player-Tests bestanden.
Signierter iOS-Debug-Build sowie macOS-
und tvOS-Simulator-Debug-Builds erfolgreich. Die acht vorhandenen Tests, die
hörbare AVPlayer-Signale erzeugen können, wurden ausgelassen. Es wurden keine
Testtöne oder Mikrofonaufnahmen abgespielt bzw. gestartet.

`Scripts/fixture-server.py --audit-failures` stellt echte Fehlversuche und
zustandsbehaftete Playlist-/Favoriten-Fixtures nur auf Loopback bereit. Der
UI-Test für Ladefehler benötigt diese Option. Die Fixture startet keine
Provider-Suche und schreibt keine Produktionsdaten.

Der Decodertest benötigt ausdrücklich `YTMDL_CODEC_FIXTURE_URL` mit einem
Loopback-Server und **synthetisch stummen** `sample.opus`, `sample.m4a`,
`sample.flac`, `sample.mp3`. Ohne diese Variable wird nur dieser Test übersprungen.
Die Test-Scheme-URL wird für lokale Ports vorübergehend geändert und danach
zurückgesetzt; persönliche Port-/Signing-Angaben gehören nicht ins Repository.

## Noch notwendiger Review am echten iPhone

1. Unter Einstellungen die App-Version **0.2.0 (27)** prüfen. Ein verfügbarer
   Build allein bestätigt noch keine erfolgreiche Geräteinstallation.
2. Ein vorhandenes Lied mit Cover selbst starten. Sperren: Titel, Interpret und
   Cover müssen sichtbar sein. Nächsten Titel wählen; das Bild muss mitwechseln.
3. Pause/Resume, Vor/Zurück und Position im Sperrbildschirm ausprobieren; danach
   in der App durch alle Tabs und zurück zum großen Player wechseln.
4. Eine kleine Playlist offline speichern. Flugmodus einschalten, App schließen
   und über **Offline-Musik öffnen** neu starten. Lokalen Titel und Cover prüfen.
5. EQ, Überblendung, Tempo, Timer und Visualizer mit eigener Musik kontrollieren;
   anschließend Bluetooth/AirPlay und eine Unterbrechung durch einen Anruf prüfen.
6. Fehler mit App-Build, betroffener Funktion und Verhalten notieren. Ohne diese
   Gerätetests bleiben Hintergrundaudio und Ausgaberouten ausdrücklich offen.

Apple-Referenzen: [Now Playing](https://developer.apple.com/documentation/mediaplayer/mpnowplayinginfocenter),
[Artwork](https://developer.apple.com/documentation/mediaplayer/mpmediaitemartwork),
[Hintergrunddownloads](https://developer.apple.com/documentation/foundation/downloading-files-in-the-background).

## Ergänzung: mobile Oberfläche, Build 28 (6. Oktober 2026)

Start, Bibliothek und Playlists wurden visuell überarbeitet: eine große Hauptaktion,
neutrale Navigationsflächen, einheitliche Abstände, eigene Genre-Zeile und
Playlist-Collagen mit lesbaren Stunden-/Minutenangaben. Filtern und Sortieren sind
auf dem iPhone direkt erreichbar. Cover-Vorschauen werden zwischen Start und
Playlists geteilt, auf zwölf Sammlungen je Revision begrenzt und bei Kontowechsel
gelöscht. Ein fehlendes Vorschaubild blockiert keine Playlist.

25 stille Swift-Tests (inklusive Grenzen, Wiederverwendung und Kontentrennung der
Vorschauen), sieben iPhone-UI-Abläufe und zwei iPad-UI-Abläufe bestanden.
Die abschließende Layoutprüfung erzeugt Screenshots von Start, Bibliothek und
Playlists und prüft Filter, Rücknavigation und mindestens 44 Punkt hohe Buttons.
Signierter iOS-Debug-Build, macOS Debug und tvOS-Simulator Debug wurden gebaut.
Keine Testtöne und keine Produktionsdaten als Test-Fixtures.

Die offenen Hardwareprüfungen oben gelten weiter. Dieser Layoutwechsel bestätigt
keine zusätzlichen Audiofunktionen und ist kein neuer stabiler Server-Release.
