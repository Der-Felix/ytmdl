# Web/App-Abgleich: Musik hören und Sammlungen verwalten

Stand: 25. November 2026, native Vorschau 0.4.3 (37). Die Abschnitte zu den Builds 34 bis 37
beschreiben, was seit Build 32 dazukam; installiert ist auf dem iPhone weiterhin Build 32.
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
| Warteschlange | Abschnitte wie im Web: „Als Nächstes“, „Weiter aus: …“, „Ähnliche Titel“; hinzufügen, als Nächstes, freie Verschiebung, Entfernen, kommende Titel leeren, vollständig leeren, danach „Rückgängig“ wie im Web; ganze Alben, Künstler, Playlists und Mixe einreihen; auf Apple TV über ein Menü per Gedrückthalten | Lokale Änderungen und Wiedergabe funktionieren auch offline |
| Warteschlange als Playlist | Jetzt auch im mobilen Player erreichbar; „Mix als Playlist speichern“ auf der Mix-Seite | Speichern auf dem Server braucht Verbindung; ohne Verbindung ist die Aktion mit Hinweis gesperrt |
| Queue-Filter | Mobil, Mac und Apple TV (Titel, Künstler, Album) | Filtert die vorhandene lokale Warteschlange |
| Überblendung/Vorladen | 0–12 Sekunden, Schutz benachbarter Albentitel, schneller Start | Auch mit lokalen Dateien; keine Garantie für samplegenaues Gapless |
| Tempo/Sleep-Timer | 0,5–2× einschließlich 1,75×; 15/30/45/60 Minuten, Titel-/Albumende; auf Apple TV in „Optionen“ und im Player | Lokal; kein Server erforderlich |
| Zehnband-EQ/Vorverstärkung | Vorhanden, Presets und EQ-Pegelschutz; auf Apple TV als Auswahl in 1-dB-Schritten | Lokal auf unterstütztem PCM-Ausgabeformat |
| Lautstärke-Normalisierung | Neu: derselbe Mess-Endpunkt wie im Web; getrennte Werte pro überblendetem Titel | Bereits gespeicherte Werte verwenden; fehlt der Wert, unverändert abspielen und anzeigen |
| Lyrics | Plain/LRC, Folgen und Antippen zum Seek; jetzt auch im Mac-Player | Bereits heruntergeladene Lyrics lokal verwenden |
| Visualizer | Echte FFT, verschiedene Darstellungen, Farben, ohne Cover, Reduce Motion | Lokal; Stile unterscheiden sich vom Web |
| Hörverlauf/Queue wiederherstellen | Wiedergaben und Überspringen mit den Regeln des Web-Players an `/history` und `/history/skips`; lokal, konto- und servergebunden; Wiederherstellen ausdrücklich, mit Herkunft und Kontext jedes Eintrags | Auch Wiedergaben und Überspringen lokaler Dateien werden gespeichert und später an dasselbe Konto gesendet, wenn aktiviert |
| Song-/Genre-Radio | Server-Metadaten, ähnlich zum Web; „Automatisch weiterspielen“ (standardmäßig an) hängt bei höchstens zwei verbleibenden Titeln bis zu 12 ähnliche Titel an, ohne bereits eingereihte oder in dieser Sitzung gespielte | Neue Empfehlungen brauchen Verbindung |
| Start („Hören“) | Weiterhören, persönliche Mixe, „Zuletzt gehört“ vom Server, „Neu in der Mediathek“; Mac-Start zeigt Mixe und Serververlauf; Apple TV hat einen eigenen Start-Tab mit fokussierbaren Reihen | Offline-Hinweis mit Weg zur gespeicherten Musik; gespeicherte Musik bleibt abspielbar |
| Persönliche Mixe („Für dich“) | „Dein Mix 1…n“ mit Genre, Mixe aus dem Hören, Collage, Titelzahl, Abspielen; „Für dich“ auch auf Apple TV; Mix-Seite mit Abspielen, Zufällig, Als Nächstes, Zur Warteschlange, Als Playlist speichern und Sortieren wie im Web | Mixe berechnet der Server; ohne Verbindung nicht verfügbar |
| Künstlerseite | „Am meisten gehört“ (bis zu zehn Titel), Abspielen/Zufall, ganze Künstler einreihen | Braucht Verbindung; Offline-Künstlersammlungen bleiben abspielbar |
| Geräteübergabe | Ausdrückliches Senden/Annehmen mit Schutz vor verspäteten Antworten | Braucht Verbindung; startet nicht eigenständig Musik |
| Sperrbildschirm/Medientasten | Now Playing mit Cover, Seek und Transport | Verwendet lokale Cover; Hardwareprüfung bleibt erforderlich |

## Build 37 (0.4.3): Klang und Queue-Filter auf Apple TV

Auf Build 36 aufbauend, gegen die unveränderte Server-API von v1.10.x. Ausgeliefert als 0.4.3 (37).

- „Optionen → Klang & Wiedergabe“ auf Apple TV: Sleep-Timer (alle Stufen, mit Restzeit),
  Tempo, Überblendung, Albentitel ohne Überblendung, Vorladen, schneller Start,
  Automatisch weiterspielen, Lautstärke-Normalisierung mit Status und der Zehnband-EQ mit
  Klangprofilen, Bändern, Vorverstärkung und Pegelschutz. Dieselben gespeicherten Werte
  und derselbe Player wie auf den anderen Plattformen. tvOS hat keinen Schieberegler:
  Werte werden aus Menüs gewählt (EQ und Vorverstärkung in 1-dB-Schritten); −/+-Tasten in einer Formularzeile bekommen auf tvOS keinen Fokus. Geprüft im Code: die
  Klangverarbeitung (`MTAudioProcessingTap` über `AVAudioMix`) und das Tempo
  (`audioTimePitchAlgorithm`) sind auf tvOS verfügbar und laufen dort ohne
  Sonderweg; nichts wurde weggelassen, weil es auf tvOS nicht ginge.
- Im Player: Menüs für Sleep-Timer und Tempo, „Titel-Aktionen“ mit „Song-Radio starten“ und
  „Zur Playlist hinzufügen“; Wiederholen wechselt wie auf dem iPhone zwischen aus,
  Warteschlange und Titel. Der große Titel „Player“ überdeckte beim Scrollen die
  Warteschlange und entfällt (die Tab-Leiste nennt den Bereich).
- Warteschlange filtern auf Apple TV wie auf dem iPhone (Titel, Künstler, Album), über die
  Tastatur von tvOS, mit „Filter leeren“. Zeilen behalten ihre Position in der
  Warteschlange, Bearbeiten trifft also den angezeigten Titel. „Warteschlange leeren“
  entfernt wie „Nächste leeren“ auf dem iPhone alle kommenden Titel, auch ausgefilterte;
  „Rückgängig“ bringt sie zurück.
- Gedrückthalten in der TV-Warteschlange bietet zusätzlich „Zur Playlist hinzufügen“ und
  „Zu Favoriten hinzufügen“/„Aus Favoriten entfernen“ (online).
- „Optionen“ auf Apple TV hat jetzt wie das iPhone „Hörverlauf & Übergabe“: lokalen
  Hörverlauf merken oder leeren, mit dem Server synchronisieren, „Wiedergabe von anderem
  Gerät prüfen“ und „Wiedergabe hier fortsetzen“; „Wiedergabe übergeben“ steht in den
  Titel-Aktionen des Players.
- Bewusst nicht auf Apple TV: der Visualizer (der TV-Player hat keine Cover-Überlagerung;
  eine Bewegung im Raum ohne Bedienung bringt am Fernseher wenig) und Offline-Musik, siehe
  unten.

Warum Apple TV keinen Offline-Katalog hat: tvOS garantiert Apps keinen dauerhaften Speicher.
Größere Dateien dürfen nur in den Caches liegen, die das System jederzeit leeren kann; der
iPhone-Katalog legt Musik, Cover und `manifest.json` dagegen dauerhaft in Application
Support ab und plant Downloads über Hintergrund-Transfers, Speicherbudget und WLAN-Regel.
Eine ehrliche TV-Version bräuchte einen eigenen Speicherort in den Caches, Umgang mit
verschwundenen Dateien mitten in einer gespeicherten Sammlung, „Offline speichern“ in den
TV-Menüs, eine TV-Ansicht des Katalogs und einen Offline-Einstieg ohne Server. Das sind
deutlich mehr als 300 Zeilen für einen Nutzen, der am Fernseher gering ist (das Gerät hängt
fest am Netz, und gelöschte Caches würden Sammlungen unbemerkt ausdünnen). Apple TV streamt
daher weiter vom Server.

Nachweis: Swift-Tests für die Auswahlwerte (Raster, Grenzen, Zwischenwerte, ungültige Werte), den Filter
(Titel, Künstler, Album, leerer Filter, Positionen) und den Fokus nach dem Entfernen
gefilterter Zeilen; UI-Tests auf Apple TV, die die Überblendung in „Klang & Wiedergabe“
ändern und eine gefilterte Zeile als Nächstes einreihen, nur in der CI. Nicht auf einem
Gerät mit Fernbedienung und nicht hörbar geprüft.

## Build 36 (0.4.2): Apple TV und „Rückgängig“

Auf Build 35 aufbauend, gegen die unveränderte Server-API von v1.10.x. Ausgeliefert als 0.4.2 (36).

- Apple TV „Für dich“: Die erste Mix-Reihe im Start-Tab endet mit „Alle Mixe“. Die Seite
  zeigt alle Mixe des Tages als fokussierbares Raster, „Dein Mix 1…n“ zuerst, dann die Mixe
  aus dem Hören. Auswählen öffnet die Mix-Seite, Gedrückthalten bietet „Abspielen“ und
  „Zufällig abspielen“ wie im Start-Tab. Laden, keine Mixe, Server ohne Mixe und Fehler
  haben je einen eigenen Zustand, mit „Erneut laden“.
- Apple TV Warteschlange bearbeiten: Gedrückthalten auf einem kommenden Titel bietet „Als
  Nächstes abspielen“, „Nach oben“, „Nach unten“ und „Aus Warteschlange entfernen“, jeweils
  nur, wo es geht. Der laufende und schon gespielte Titel bleiben, wo sie sind. Der Fokus
  bleibt beim verschobenen Titel oder beim Nachbarn. „Warteschlange leeren“ entfernt die
  kommenden Titel, „Als Playlist speichern“ speichert die Warteschlange.
- „Rückgängig“ nach dem Leeren (alle Plattformen) wie die Web-Meldung: zehn Sekunden lang,
  bis sich die Warteschlange ändert. Die Titel kommen mit Abschnitten und Kontext zurück;
  nach „Leeren und stoppen“ an der alten Position und nur dann spielend, wenn sie spielten.
  Abweichung mit Absicht: Das Web bietet „Rückgängig“ nur nach dem vollständigen Leeren,
  nativ auch nach „Nächste leeren“, weil das auf Apple TV die einzige Leeren-Aktion ist.
- Mixe sortieren wie auf der Web-Mix-Seite: Mix-Reihenfolge, Titel, Künstler, Album, Dauer,
  Titelnummer, auf- oder absteigend. Abspielen und „Als Playlist speichern“ folgen der
  angezeigten Reihenfolge. Die Übersicht der Mixe hat auch im Web keine Sortierung.

Nachweis: Swift-Tests für Wiederherstellen (auch mit Autoplay dazwischen), Bearbeitungsregeln,
Fokus-Nachbarn und Sortierung; UI-Tests für „Für dich“ und das Entfernen eines Titels auf
Apple TV, nur in der CI. Nicht auf einem Gerät mit Fernbedienung geprüft.

## Build 35 (0.4.1): der Rest von „Hören“

Auf Build 34 aufbauend, gegen die unveränderte Server-API von v1.10.x. Ausgeliefert als 0.4.1 (35).

- Autoplay wie im Web 1.10.1: Jede Nachfüllung hängt höchstens 12 ähnliche Titel an
  (`AUTOPLAY_BATCH`), ohne Titel, die schon in der Warteschlange stehen oder in dieser
  Sitzung gespielt wurden (die letzten 200, an Konto und Server gebunden, beim Wechsel
  zurückgesetzt). Ist der Vorlauf wieder erreicht, folgt die nächste Nachfüllung. Ein
  ausdrücklich gestartetes Song-Radio behält seine volle Liste.
- Herkunft (Als Nächstes, Kontext, Autoplay) und Kontext (Art, ID, Titel) jedes Eintrags
  werden mit der Warteschlange gespeichert, gebunden an Konto und Server wie bisher.
  „Letzte Wiedergabe fortsetzen“ zeigt dieselben Abschnitte, Autoplay setzt richtig fort.
  Ältere gespeicherte Warteschlangen ohne diese Angaben werden wie bisher als einfacher
  Kontext wiederhergestellt.
- „Mix als Playlist speichern“ auf der Mix-Seite mit denselben Serveraufrufen wie
  „Warteschlange als Playlist speichern“; vorgeschlagener Name „{Mix} · TT.MM.JJJJ“ wie im
  Web. Ohne Verbindung gesperrt, mit Hinweis. Ein Mix-Menü gibt es nativ sonst nicht.
- Apple TV: Start-Tab mit Weiterhören, Mixen, „Zuletzt gehört“ und „Neu in der Mediathek“
  als fokussierbare Cover-Reihen; Mix-Seite mit Abspielen und Zufällig; Warteschlange im
  Player mit Abschnitten und dem Schalter „Automatisch weiterspielen“.

Nachweis: Swift-Tests für Nachfüllung, Sitzungsliste, Wiederherstellung (auch altes Format)
und Playlist-Namen; UI-Tests für den Start auf iPhone und Apple TV, nur in der CI. Nicht auf
einem Gerät geprüft.

## Build 34 (0.4.0): Hören wie im Web 1.10

Mit [PR #108](https://github.com/Der-Felix/ytmdl/pull/108), gegen die unveränderte Server-API
von v1.10.0. Ausgeliefert als 0.4.0 (34).

- Start ist der Hören-Bereich: Weiterhören, „Deine Mixe“ mit „Alle Mixe“ („Für dich“),
  „Zuletzt gehört“ aus dem Serververlauf aller Geräte und „Neu in der Mediathek“. Jeder
  Abschnitt lädt für sich; Fehler erscheinen im Abschnitt, nie als Meldungsfenster.
- Wiedergabe und Überspringen zählen wie im Web: Wiedergabe nach min(30 s, halbe
  Titeldauer ÷ Tempo) tatsächlicher Wiedergabezeit; Überspringen nach mindestens 2 s und
  vor dieser Grenze, nicht nach mehr als drei Minuten Pause. Abweichung mit Absicht: Auch
  lokale Dateien zählen; ihre Ereignisse warten in der Offline-Warteschlange.
- „Am meisten gehört“ auf der Künstlerseite; Warteschlangen-Abschnitte mit Herkunft
  (Als Nächstes, Weiter aus, Ähnliche Titel), Autoplay-Vorlauf und Schalter
  „Automatisch weiterspielen“; „Als Nächstes“ und „Zur Warteschlange“ für ganze Sammlungen.
- Zufällig mischt wie im Web nur den gestarteten Kontext; eingereihte Titel bleiben vorn.

Nachweis: Swift-Tests für Dekodierung, Zähl-/Überspring-Regeln, Ereignis-Warteschlange und
Warteschlangen-Abschnitte; Build und UI-Tests nur in der CI. Nicht auf einem Gerät geprüft.

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
7. **tvOS ist noch nicht gleichwertig**: kein Offline-Katalog (mit Absicht, siehe Build 37)
   und kein Visualizer. Klang-Einstellungen, Queue-Filter, Hörverlauf-Schalter und
   Geräteübergabe gibt es seit Build 37. „Für dich“, das Bearbeiten der Warteschlange (Build 36) und die
   Klang-Einstellungen (Build 37) brauchen noch eine Prüfung mit der Fernbedienung auf einem
   Gerät.
8. Automatische Tests ersetzen keine Prüfung von Sperrbildschirm, Bluetooth,
   AirPlay, Telefonunterbrechungen und Downloads nach erzwungenem Beenden auf Hardware.

Die in Build 35 offenen Hören-Details (Mixe sortieren, „Rückgängig“ nach dem Leeren,
„Für dich“ und Warteschlange bearbeiten auf Apple TV) sind mit Build 36 umgesetzt.

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

- Hören: Start öffnen, Mixe, „Alle Mixe“, eine Mix-Seite und „Zuletzt gehört“ prüfen; auf
  einer Künstlerseite „Am meisten gehört“. Einen Titel nach 10 s überspringen, einen anderen
  über 30 s hören; im Web erscheint der zweite im Verlauf. Dasselbe im Flugmodus mit
  gespeicherter Musik, danach online: die Ereignisse werden genau einmal gesendet.
- Warteschlange: einen Titel „Als Nächstes“, ein Album „Zur Warteschlange“; Abschnitte und
  Reihenfolge prüfen. Mit „Automatisch weiterspielen“ erscheinen bei zwei verbleibenden
  Titeln „Ähnliche Titel“; mit Wiederholung oder Sleep-Timer nicht. Je Nachfüllung höchstens
  12, nie ein in dieser Sitzung schon gehörter Titel.
- Neustart: Warteschlange mit „Als Nächstes“ und Autoplay-Titeln aufbauen, App beenden,
  neu öffnen, „Weiterhören“ wählen: dieselben Abschnitte, „Weiter aus: …“ mit dem Kontext.
- Mix-Seite: „Als Playlist speichern“, Namen prüfen, speichern, die neue Playlist öffnen;
  im Flugmodus ist die Aktion gesperrt und erklärt.
- Apple TV: Start-Tab mit der Fernbedienung durchgehen, Mix öffnen, abspielen und zufällig
  abspielen; im Player die Abschnitte und „Automatisch weiterspielen“ prüfen. Am Ende von
  „Deine Mixe“ „Alle Mixe“ öffnen, im Raster bewegen, einen Mix gedrückt halten und abspielen.
  Im Player kommende Titel gedrückt halten: nach oben, nach unten, als Nächstes, entfernen;
  der Fokus bleibt beim Titel oder Nachbarn. „Warteschlange leeren“, dann „Rückgängig“.
- Apple TV Klang: „Optionen → Klang & Wiedergabe“, Sleep-Timer 15 Minuten (Restzeit läuft
  auch im Player), Tempo 1,25×, Überblendung 6 s, EQ „Mehr Bass“ und ein Band ändern
  („Eigener Klang“), Normalisierung einschalten und den Status lesen. Die Werte gelten je
  Gerät; das iPhone behält seine eigenen. Im Player die Warteschlange
  nach einem Künstler filtern und einen angezeigten Titel als Nächstes einreihen.
- Rückgängig: auf dem iPhone „Leeren und stoppen“, innerhalb von zehn Sekunden „Rückgängig“:
  dieselben Abschnitte, derselbe Titel an derselben Stelle. Nach Ablauf ist das Angebot weg.
- Mix sortieren: nach Künstler absteigend sortieren, abspielen, als Playlist speichern; die
  Playlist hat die angezeigte Reihenfolge.

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
