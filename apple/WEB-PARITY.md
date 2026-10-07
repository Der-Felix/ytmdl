# Web/App-Abgleich: Musik hören und Sammlungen verwalten

Stand: 7. Oktober 2026, native Vorschau 0.3.0 (32), PR #42.
Dies ist eine Funktionsprüfung, keine Freigabe als vollständiges oder fehlerfreies
Stable-Release. Downloads vom Provider, Serververwaltung und Bibliothekswartung
gehören nicht zum Umfang. „Offline speichern“ auf dem eigenen Gerät gehört dazu.

Verglichen wurden die Web-Seiten `PlaylistDetail`, `Playlists`, `Favorites`,
`Offline`, `NowPlaying`, `Library` und die Player-/Playlist-APIs. Die Apple-Arbeitskopie
enthält auch die Playlist-Cover-Verbesserungen aus PR #42; diese sind in `dev`
noch nicht vollständig enthalten. Ähnliche Funktionen werden nach ihrer Wirkung
verglichen, nicht nach identischen Menünamen.

## Online und offline

| Funktion | Native App | Offline-Verhalten |
| --- | --- | --- |
| Bibliothek, Künstler, Alben, Titel suchen | Vorhanden, serverseitige Suche und paginierte Sammlungen | Gespeicherte Titel und Sammlungen lokal suchen und sortieren |
| Playlist erstellen, umbenennen, Beschreibung ändern, löschen | Vorhanden, bestätigte Serveränderungen; Löschen mit Rückfrage | Serveränderungen benötigen Anmeldung/Verbindung, wie im Web |
| Playlist-Titel hinzufügen, entfernen, Reihenfolge ändern | Vorhanden; Bulk-Anfragen in bestätigten 100er-Blöcken | Gespeicherte Playlist-Mitgliedschaft bleibt ein lokaler Snapshot |
| Intelligente Playlists | Genre, Künstler, Favoriten, Zeitraum, Sortierung und Begrenzung | Gespeicherter Inhalt abspielbar; Neuberechnung benötigt den Server |
| Favoriten | Hinzufügen/entfernen mit Schutz vor verspäteten Antworten | Gespeicherte Favoriten als Sammlung abspielbar; keine Servermutation |
| Sammlungs-Cover | Album-Collagen und authentifizierte Einzelcover | Cover werden als lokale Dateien gespeichert |
| Offline-Sammlungen | iPhone/iPad und jetzt auch Mac: Playlists, Favoriten, Alben, Künstler, alle Titel | Reihenfolge, Suche und Sortierung über lokale Metadaten |
| Lokale Downloads verwalten | Fortschritt, Pause, Wiederholen, Speicherbudget, WLAN, lokale Entfernung | Fertige Dateien funktionieren ohne Sitzung; neue Downloads brauchen Anmeldung |
| Player und Mini-Player | Start/Pause, Vor/Zurück, Seek, Lautstärke/AirPlay, Wiederholung | Nutzt lokale Datei; fehlt sie, kein versteckter Streaming-Fallback im Offline-Modus |
| Warteschlange | Hinzufügen, als Nächstes, freie Verschiebung, Entfernen, kommende Titel leeren, vollständig leeren | Lokale Änderungen und Wiedergabe funktionieren auch offline |
| Warteschlange als Playlist | Jetzt auch im mobilen Player erreichbar | Speichern auf dem Server braucht Verbindung |
| Queue-Filter | Mobil und Mac | Filtert die vorhandene lokale Warteschlange |
| Überblendung/Vorladen | 0–12 Sekunden, Schutz benachbarter Albentitel, schneller Start | Auch mit lokalen Dateien; keine Garantie für samplegenaues Gapless |
| Tempo/Sleep-Timer | 0,5–2× einschließlich 1,75×; 15/30/45/60 Minuten, Titel-/Albumende | Lokal; kein Server erforderlich |
| Zehnband-EQ/Vorverstärkung | Vorhanden, Presets und EQ-Pegelschutz | Lokal auf unterstütztem PCM-Ausgabeformat |
| Lautstärke-Normalisierung | Neu: derselbe Mess-Endpunkt wie im Web; getrennte Werte pro überblendetem Titel | Bereits gespeicherte Werte verwenden; fehlt der Wert, unverändert abspielen und anzeigen |
| Lyrics | Plain/LRC, Folgen und Antippen zum Seek; jetzt auch im Mac-Player | Bereits heruntergeladene Lyrics lokal verwenden |
| Visualizer | Echte FFT, verschiedene Darstellungen, Farben, ohne Cover, Reduce Motion | Lokal; Stile unterscheiden sich vom Web |
| Hörverlauf/Queue wiederherstellen | Lokal, konto- und servergebunden; Wiederherstellen ausdrücklich | Offline-Ereignisse später an dasselbe Konto synchronisieren, wenn aktiviert |
| Song-/Genre-Radio | Server-Metadaten, ähnlich zum Web; Autoplay nach Queue-Ende | Neue Empfehlungen brauchen Verbindung |
| Geräteübergabe | Ausdrückliches Senden/Annehmen mit Schutz vor verspäteten Antworten | Braucht Verbindung; startet nicht eigenständig Musik |
| Sperrbildschirm/Medientasten | Now Playing mit Cover, Seek und Transport | Verwendet lokale Cover; Hardwareprüfung bleibt erforderlich |

## In Build 32 geschlossen

- Warteschlangen-Einträge frei nach oben/unten verschieben. Der aktuelle Eintrag
  bleibt derselbe, auch bei mehrfach vorhandenem Song; Vorladen wird angepasst.
- Einen Bibliotheks- oder Offline-Titel als Nächstes einfügen, ohne die laufende
  Wiedergabe zu ersetzen. Die Grenze von 500 Queue-Einträgen bleibt sichtbar.
- Mobile Queue filtern und als Playlist speichern; komplette Queue leeren/stoppen.
  Eine ausdrücklich geleerte Queue wird nicht aus alten Resume-Daten restauriert.
- Lautheitsmessung über den bestehenden CSRF-geschützten Endpunkt. Ungültige
  Messwerte und verspätete Antworten werden verworfen, Fehler stoppen keine Musik.
  Pro Datei getrennte atomare Gain-Werte verhindern, dass die Überblendung den
  Lautheitswert des falschen Titels übernimmt. Messwerte werden kontoabhängig in
  bestehenden Offline-Metadaten gespeichert, ohne Audio umzuschreiben.
- Mac-Offline-Zugang, Sammlungsspeicherung und lokaler Player statt Platzhalter.
- 45-Minuten-Timer, Tempo 1,75× und synchronisierte Lyrics im Mac-Player.

## Noch keine vollständige Web-Parität

Diese Punkte sind echte offene Unterschiede und dürfen nicht als erledigt gelten:

1. Der Web-EQ hat zusätzlich **parametrische Filter und benannte eigene Presets**.
   Die native App hat bislang einen grafischen Zehnband-EQ mit eingebauten Presets.
2. **Mono, Links-/Rechts-Balance und ein separat schaltbarer Dynamik-Limiter** fehlen
   nativ. Der EQ-Pegelschutz und das Begrenzen von Samples ersetzen diese Optionen nicht.
3. Web-Shuffle ist ein **umschaltbarer Modus mit ursprünglicher Reihenfolge**.
   Native „Nächste Titel mischen“ ist derzeit eine einmalige Queue-Änderung.
4. Der aktuell laufende Queue-Eintrag lässt sich nativ nicht einzeln entfernen;
   „Leeren und stoppen“ sowie Entfernen anderer Einträge sind möglich.
5. Online-Favoriten-/Sammlungssortierung nativ bietet Titel/Künstler A–Z; die weiteren
   Web-Sortierungen nach Album/Dauer und absteigender Richtung fehlen.
6. Der Web-Visualizer hat eine Zeitbereichs-Wellenform. Die mobilen nativen Stile
   sind Frequenzdarstellungen; diese sind kein identischer Ersatz für eine Wellenform.
7. **tvOS ist noch nicht gleichwertig**: kein Offline-Katalog; erweiterte Klang-
   und Playlist-Bedienung benötigt eine separate Prüfung mit Fernbedienung.
8. Automatische Tests ersetzen keine Prüfung von Sperrbildschirm, Bluetooth,
   AirPlay, Telefonunterbrechungen und Downloads nach erzwungenem Beenden auf Hardware.

Als nächste Arbeit zuerst Klangoptionen/benannte Presets und reversibles Shuffle,
anschließend die verbleibenden Sortier- und Plattformunterschiede umsetzen. Vor
Stable-Freigabe alle Hardware-Punkte aus `RELEASE-CHECKLIST.md` abnehmen.

## Nachweise für Build 32

- SwiftPM: 50 Core-/Support-Tests bestanden; zwei opt-in Audiotests mit möglichen
  hörbaren Signalen bewusst übersprungen. Der authentifizierte Offline-Transfer
  mit synthetischer Stille, Cover und Lyrics wurde ausgeführt.
- Vier gezielte iPhone-Simulator-Abläufe bestanden: manuelle Playlist-Bearbeitung,
  Favoriten/Suche, Offline-Playlist nach Abmelden sowie Queue-Verschiebung,
  Playlist-Speichern und Normalisierung. Der Queue-Test startet keine Wiedergabe.
- iOS-Simulator-Debug, macOS-Debug, tvOS-Simulator-Debug und signierter iOS-Debug
  mit Xcode 27 bestanden. Ein erfolgreicher Build ist keine Hardware-Abnahme.
- Build 32 wurde als Update auf dem angeschlossenen iPhone installiert, ohne
  Deinstallation oder Start von Testmusik. Sichtbare Hardware-Prüfung bleibt offen.
- Keine Produktionskonten, Playlists oder Serverjobs als Test verändert. Kein
  Release, keine Stable-Promotion und keine vollständige Hardware-Freigabe.

## Nachprüfen

- Online: Playlist erstellen → Titel hinzufügen → Reihenfolge ändern → einen Titel
  entfernen → umbenennen → neu öffnen. Intelligente Playlist-Regeln separat testen.
- Player: aktuelle und doppelte Queue-Einträge verschieben, laufenden Titel und
  Position beobachten; Queue als Playlist speichern; vollständig leeren und prüfen,
  dass „Letzte Wiedergabe fortsetzen“ nicht die alte Queue anbietet.
- Klang: Normalisierung aktivieren. Messstatus kontrollieren, anderen Titel wählen,
  EQ ein-/ausschalten und Überblendung ausprobieren. Kein Wert bedeutet Bypass.
- Offline: Playlist vollständig speichern, Cover und Lyrics kontrollieren, Verbindung
  trennen oder nach Abmelden den Offline-Zugang wählen. Reihenfolge, lokale Suche,
  Seek und nächsten Titel prüfen. Ein Server-Playlist-Editor soll offline nicht senden.
- Dateien entfernen nur in einer Testsammlung: geteilte lokale Dateien können mehreren
  Offline-Sammlungen fehlen; die Server-Bibliothek wird dabei nicht verändert.

Die Implementierung nutzt Apples bestehende AudioMix-/AudioProcessingTap-Verarbeitung:
[AVAudioMix](https://developer.apple.com/documentation/avfoundation/avaudiomix) und
[MTAudioProcessingTap](https://developer.apple.com/documentation/mediatoolbox/mtaudioprocessingtap).
Lautheitswerte stammen ausschließlich aus der serverseitigen Analyse, nicht aus FFT-Pegeln.
