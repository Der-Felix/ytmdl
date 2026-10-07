# iPhone-Funktionsprüfung — bis Vorschau 0.3.0, Build 32

Stand: 7. Oktober 2026. Prüfung des nativen Clients und seiner API-Verträge;
isolierte temporäre Daten, Loopback-Server und iPhone-Simulator mit iOS 27.
Die Server-Kompatibilitätsbasis ist der geprüfte Router von `v1.2.0`.
Produktionskonten, Musik und Playlists wurden für diese Prüfung nicht verändert.
Dieser Bericht ist keine Freigabe als fehlerfreie oder vollständige Spotify/Plexamp-App.

## Build 32: Abgleich mit dem Web

Der vollständige Funktionsvergleich mit offenen Unterschieden steht in
[WEB-PARITY.md](WEB-PARITY.md). Neu geprüft sind Queue-Verschiebung mit doppelten
Song-IDs, Einfügen als Nächstes, leere Resume-Daten nach ausdrücklichem Leeren,
Lautheitsmessung inklusive ungültiger/später Antworten und kontoabhängige
Offline-Lautheitswerte. Die App kann jetzt auch am Mac gespeicherte Sammlungen
öffnen und synchronisierte Lyrics anzeigen.

50 Swift-Core-/Support-Tests bestehen in einem gemeinsamen seriellen Lauf,
einschließlich des authentifizierten stummen Offline-Transfers. Zwei mögliche
hörbare Audio-Tap-Tests bleiben ausdrücklich übersprungen. Vier gezielte
Simulator-Abläufe bestehen: Playlist-Erstellen/Bearbeiten/Ordnen/Entfernen/Löschen,
Favoriten/Suche, gespeicherte Offline-Playlist sowie Queue-Tools/Normalisierung.
Das ist keine vollständige neue Hardware- oder tvOS-Freigabe.

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
| Home-/Sperrbildschirm-Widgets | Einstieg implementiert; Galerie am Gerät offen | Kleine/mittlere Home-Widgets und drei Sperrbildschirm-Familien. Öffnen Player/Favoriten/Playlists ohne Autoplay; keine Live-Titelanzeige oder Wiedergabetasten im Widget |
| Lautstärke-Normalisierung | Neu in Build 32; API/CPU/UI geprüft | Bestehende Servermessung, separate Gains bei Überblendung, Offline-Cache; fehlende Werte transparent umgehen. Echte Audio-Ausgabe weiterhin offen |
| CarPlay, Siri, Live-Wiedergabe-Widgets, Watch, echtes Gapless | Nicht implementiert / nicht zugesagt | Keine vollständige Produkt-Parität behaupten |

## Build 30: Sammlungsgestaltung und Widget-Einstiege

Playlist und Favoriten erhalten einen gemeinsamen Cover-/Titel-/Metadatenkopf,
gleich breite Abspiel-/Zufallsaktionen mit mindestens 52 Punkten Höhe und ein
einziges Playlist-Menü. Accessibility-Schriftgrößen stapeln die Aktionen.
Die bisherigen elf iPhone-UI-Abläufe bestanden erneut nach der Navigationsänderung.
Ein zusätzlicher UI-Test öffnet echte App-URLs für Player → Favoriten → Playlists,
einschließlich Wechsel aus dem geöffneten Player, und prüft das Ausbleiben von
Autoplay. Die neue Route-Einheit prüft ungültige Hosts, Pfade und URL-Zusätze.

Der erste neue Widget-UI-Test erwartete irrtümlich „Favoriten“ statt der vorhandenen
Überschrift „Lieblingstitel“. Danach zeigte der korrekte Test einen echten Fehler:
Beim Wechsel vom Player zum Favoriten-Einstieg blieb die Bibliotheksübersicht
sichtbar. Explizite, getrennte Navigationspfade für die iPhone-Tabs korrigieren
diesen Übergang; der neue Test und alle elf bestehenden Abläufe bestanden danach.

iOS-Simulator-Debug, iOS-Release und signierter iPhone-Debug-Build mit eingebetteter
Widget-Erweiterung bestanden. Die Entwicklungssignatur wurde geprüft; App und
Erweiterung verwenden 0.3.0 (30). Die Release-Konfiguration ist ohne
Distributionssignierung kompiliert. Die Widget-Galerie, tatsächliche Platzierung,
Sperrbildschirm und getönte Systemdarstellung am Gerät sind noch nicht geprüft.
Die abschließende CI dieses neuen Commits muss separat kontrolliert werden;
die grünen Ergebnisse von Build 29 gelten nicht als neue CI für Build 30.

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

## Ergänzung: Stabilisierung, 0.2.1 (29), 6. Oktober 2026

Bestätigte Fehler und Korrekturen:

- Titel-/Künstlerauswahl nahm Tipps in der freien Fläche einer Zeile nicht an.
  Der gesamte Zeilenbereich ist jetzt anklickbar. Ein iPhone-UI-Test erstellt
  eine Playlist, fügt zwei Titel hinzu, ändert ihre Reihenfolge, entfernt einen,
  benennt die Sammlung um und löscht sie nach Bestätigung.
- Ein vor Item-Bereitschaft ausgelöstes Spulen konnte verloren gehen. Die Position
  wird jetzt bis zur Bereitschaft gehalten. Vor/Zurück bewahrt Pause; am Ende
  einer pausierten Queue wird kein Radio gestartet.
- Verspätete Radio-/Mix-Ergebnisse oder Übergabe-Bestätigungen konnten neuere
  Wiedergabeentscheidungen überschreiben. Eine Aktivitätsrevision verwirft solche
  Antworten nach Titelwahl, Pause, Resume oder Spulen.
- Eine ältere Bibliotheksantwort konnte gerade bestätigte Favoritenänderungen
  zurücksetzen. Ein eigener Änderungsstand schützt diese Änderungen; ein später
  ausdrücklich neu gestarteter Refresh bleibt maßgeblich.
- Bereits eingereihte Download-Abschlussmeldungen konnten nach Abmelden ein
  abgebrochenes Audiofile veröffentlichen. Abmelden entwertet jetzt die Versuch-ID
  und speichert das. Reguläres Benutzer-Pausieren bleibt davon unterschieden.
  Wiederaufgenommene Systemtasks müssen zur gespeicherten Versuch-ID passen.
- Serverformular-Ergebnisse prüfen die noch gewählte Adresse und HTTP-Zustimmung.
  Kontowechsel schließt den großen Player; Offline-Menüs sperren Serveraktionen.
- Privacy-Manifest ergänzt FileTimestamp/C617.1 für Metadaten im eigenen Container.
- Bei maximaler Schriftgröße ragten Mini-Player-Symbole über die feste Systemleiste
  hinaus. Symbole und kompakte Metadaten bleiben jetzt begrenzt, vollständige Texte
  im großen Player skalieren weiter. Mini- und großer Player haben explizite
  44-Punkt-Tippflächen für den Transport;
  Start-Verknüpfungen/Mixe stehen mit Accessibility-Schrift untereinander.

Prüfungen des abschließenden Quellstands:

- **43 Swift-Tests bestanden**: sieben Core- und 36 Support-Tests, einschließlich
  authentifiziertem Offline-Transfer, verzögerten Antworten und Now-Playing-Covern.
  Zwei signalabhängige Audio-Tap/Meter-Tests ausdrücklich übersprungen.
- **Elf iPhone-UI-Abläufe bestanden**, inklusive manueller Playlist-Bearbeitung,
  Smart-Regeln mit Fehler/Retry, Suche/Favoriten, Mini-Player, Einstellungen und
  15 wiederholten Tab-Wechseln ohne automatischen Wiedergabestart. Eine zusätzliche
  Prüfung erzwingt die maximale Schriftgröße und kontrolliert erreichbare Hauptaktion,
  Mini-Player, Titelwechsel-Fläche, Tab-Navigation und Schließen des großen Players.
  Auch die vier sekundären Transporttasten im großen Player werden auf erreichbare
  44-Punkt-Flächen und begrenzte Größe geprüft.
- **Zwei iPad-UI-Prüfungen bestanden**: Navigation und erreichbare Player-Steuerung.
- **Release-Builds iOS, macOS und tvOS-Simulator erfolgreich**; signierter iOS-Debug-
  Build erstellt und Signatur geprüft. Die erfolgreiche Kompilierung ersetzt keine
  Distributionssignierung, Installation oder Hardwarequalifikation.

AVPlayer-Transporttests verwenden jetzt standardmäßig Null-PCM. Überblendung,
Album-Schutz, Pause/Seek/Stop, Wiederholung und Timer wurden dadurch tatsächlich
mit stillen Playern geprüft. CPU-EQ und FFT bleiben getestet. Zwei Tests benötigen
absichtlich ein Signal und laufen nur mit `YTMDL_AUDIBLE_AUDIO_TESTS=1`; sie wurden
hier nicht aktiviert. Die Offline-Transferprüfung erhält in CI ebenfalls einen
isolierten Loopback-Server. Keine Testtöne oder Produktionsdaten verwendet.

Build 29 ist für die Geräteinstallation vorbereitet; das echte iPhone war bei
dieser Prüfung nicht erreichbar. Die zuvor bestätigte Installation war Build 27.
Sperrbildschirm-Darstellung, reale Audioausgabe, AirPlay/Unterbrechungen,
Hintergrundtransfer und Flugmodus-Neustart bleiben offen. Release benötigt HTTPS;
eine Debug-HTTP-Verbindung qualifiziert keinen stabilen Release-Betrieb.
Die vollständigen Freigabepunkte stehen in [RELEASE-CHECKLIST.md](RELEASE-CHECKLIST.md).

CI-Nachprüfung: Der erste iPhone-Lauf prüfte die Fortschrittsanzeige erst ungefähr
55 Sekunden nach Start; die synthetischen Titel dauerten 30 Sekunden. Der
Fortschritts-Wait scheiterte, die zehn weiteren iPhone-Abläufe bestanden.
Die Loopback-Fixture verwendet jetzt drei Minuten Null-PCM mit passender
Metadaten-Dauer, um automatische Titelgrenzen während langsamer Accessibility-
Snapshots zu vermeiden. Die bestehende Fortschrittsprüfung bleibt unverändert;
das Ergebnis der erneuten abschließenden CI muss separat kontrolliert werden.

Die erneute CI auf `85df6f9` scheiterte ebenfalls auf dem iPhone: Der Player-Zähler
blieb bei 0:00, und im manuellen Playlist-Test fehlten einmal die auswählbaren
Titel. Die sechs übrigen Jobs bestanden; dieselben elf iPhone-Abläufe bestanden
lokal. Die längere Fixture hat den Unterschied damit nicht erklärt. Die stabile
Freigabe bleibt gesperrt. Für die weitere Eingrenzung startet der iPhone-Test die
Wiedergabe ausdrücklich über den sichtbaren Play-Button im Vordergrund und
verlangt weiterhin echten Zeitfortschritt innerhalb von fünf Sekunden. Fehler
halten ein Fixture-Bild fest; CI exportiert ausschließlich PNG-Fehlerbilder,
keine rohen Result-Bundles, Logs oder Simulator-Diagnosen. Die Playlist-Prüfung
bricht nach der fehlgeschlagenen Auswahl ab, statt weitere ungültige Taps zu senden.
Diese Diagnoseänderung ist noch kein Beleg für eine behobene Produktionsursache.

Nachprüfung auf `506f054`: Die sechs Jobs Backend, Frontend, Browser-Offline,
Apple Core/Builds, iPhone-UI und iPad-UI bestanden. Der ausdrückliche iPhone-Start
mit echtem Zeitfortschritt und die Playlist-Auswahl sind damit auch in CI grün.
Die TV-UI scheiterte diesmal an der Remote-Fokus-Navigation zum Player; die
gesamte Freigabe bleibt deshalb rot. Hardwareprüfung und Release-HTTPS stehen
weiter aus. Der Bilderexport wird korrigiert: `--only-failures` exportierte hier
nur die Issue-Beschreibung und ließ Aktivitäts-Screenshots weg. Ein PNG-Filter
exportiert die Fixture-Bilder, das Upload-Glob schließt alle anderen Dateitypen
aus. Dieser Export wurde lokal an einem Result-Bundle mit drei PNGs geprüft.

## Build 31: Offline-Bibliothek

Zwei stille iPhone-Simulator-Abläufe bestanden: Album speichern/öffnen sowie
Playlist speichern, abmelden, Offline-Sammlung öffnen, Originalreihenfolge prüfen,
Titel sortieren und erfolglos suchen. Die Sammlungsansicht hat lokale Cover,
Verfügbarkeitszahlen und sichtbare Gruppen. Einzelne alte Downloads bleiben unter
„Alle Titel“ erreichbar. Ein zusätzlicher Test prüft Sammlung/Sortierung nach
Neustart, Kontotrennung und geteilte Titel. Manifest und Audiodateien bleiben erhalten.
Der signierte iOS-Debug-Build mit App/Widget-Buildnummer 31 ist erstellt.
Installation und Review auf dem physischen iPhone stehen noch aus.

Der initiale gemeinsame Swift-Testlauf hatte zwei Fehler durch gemeinsam genutzte
Playlist-Fixtures bzw. Now-Playing-Zustand. In getrennten Läufen bestehen alle
44 nicht optionalen Tests; drei weitere Tests benötigen explizite Fixtures und
wurden ausgelassen. Der gemeinsame Lauf wird nicht als erfolgreich ausgewiesen.
