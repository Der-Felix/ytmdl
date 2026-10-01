/** User-facing explanations use stable codes, never provider output or guessed causes. */
export interface Problem {
  title: string
  explanation: string
  nextStep: string
  retryable: boolean
}

function problem(title: string, explanation: string, nextStep: string, retryable = false): Problem {
  return { title, explanation, nextStep, retryable }
}

const providerUnavailable = problem(
  'Musikquelle nicht erreichbar',
  'Die angefragte Musikquelle hat keine nutzbare Antwort geliefert. Ob die Ursache beim Anbieter, der Verbindung oder dem Proxy liegt, ist noch nicht bekannt.',
  'Versuche es später erneut. Bleibt das Problem bestehen, sollte die Serververwaltung die Verbindung und den Proxy prüfen.',
  true,
)
const sessionWait = problem(
  'Wartet auf eine verfügbare Musikquelle',
  'Derzeit ist keine nutzbare Mediensitzung verfügbar.',
  'Warte auf die Freigabe der Musikquelle. Die Serververwaltung kann den Zustand unter „Medienquellen“ prüfen.',
)
const rateLimit = problem(
  'Der Anbieter begrenzt die Anfragen',
  'Die Musikquelle hat eine vorübergehende Begrenzung gemeldet.',
  'Warte die angezeigte Pause ab. Wiederholte Anfragen verkürzen die Wartezeit nicht.',
)
const login = problem(
  'Anmeldung erforderlich',
  'Deine Anmeldung ist nicht mehr gültig oder fehlt.',
  'Melde dich erneut an und öffne die Ansicht noch einmal.',
)

const problems: Record<string, Problem> = {
  PROVIDER_UNAVAILABLE: providerUnavailable,
  PROVIDER_RATE_LIMITED: rateLimit,
  RATE_LIMITED: problem('Zu viele Anfragen', 'Der Server begrenzt momentan die Anfragen.', 'Warte kurz, bevor du erneut anfragst.'),
  SESSION_UNAVAILABLE: sessionWait,
  SESSION_RATE_LIMITED: rateLimit,
  SESSION_BOT_CHALLENGE: problem('Die Musikquelle verlangt eine Browser-Prüfung', 'Der Anbieter hat diese Mediensitzung vorübergehend gesperrt.', 'Die Serververwaltung sollte die Sitzung im Browser prüfen. Neue Cookies heben eine laufende Schutzpause nicht auf.'),
  SESSION_AUTH_FAILED: problem('Anmeldung bei der Musikquelle ungültig', 'Die gespeicherten Cookies werden vom Anbieter nicht mehr akzeptiert.', 'Die Serververwaltung kann unter „Medienquellen“ neue Cookies aus einer gültigen Browser-Sitzung hochladen.'),
  SESSION_NOT_CONFIGURED: problem('Mediensitzung fehlt', 'Für diese Musikquelle ist keine passende Mediensitzung eingerichtet.', 'Die Serververwaltung kann unter „Medienquellen“ eine Sitzung einrichten.'),
  TRACK_NOT_FOUND: problem('Keine passende Audioquelle gefunden', 'Für diesen Titel konnte keine verfügbare und geeignete Aufnahme ermittelt werden.', 'Prüfe Titel, Künstler und Version. Ein erneuter Versuch ist sinnvoll, wenn später eine passende Quelle verfügbar ist.'),
  MATCH_FAILED: problem('Aufnahme passt nicht sicher zum Titel', 'Die gefundenen Aufnahmen erfüllen die Zuordnungskriterien nicht.', 'Prüfe Titel, Künstler und Version. YTMDL übernimmt keinen unsicheren Treffer.'),
  DOWNLOAD_FAILED: problem('Download konnte nicht abgeschlossen werden', 'Die Übertragung wurde nicht erfolgreich beendet.', 'Du kannst den betroffenen Track erneut versuchen. Die genaue Ursache ist mit diesem Fehlercode allein nicht bekannt.', true),
  TRACK_TIMEOUT: problem('Download hat zu lange gedauert', 'Der Track hat sein eingestelltes Zeitlimit erreicht.', 'Bei „Wiederholung geplant“ wartet YTMDL automatisch auf den nächsten Versuch. Ein endgültig fehlgeschlagener Track kann erneut gestartet werden.', true),
  NETWORK_TIMEOUT: problem('Verbindung hat zu lange gedauert', 'Die Gegenstelle hat nicht rechtzeitig geantwortet.', 'Versuche es später erneut. Bleibt der Fehler bestehen, sollte die Serververwaltung die Verbindung prüfen.', true),
  TRANSFER_BUDGET_EXCEEDED: problem('Übertragungslimit erreicht', 'Die Übertragung hat das erlaubte Größen- oder Zeitbudget überschritten.', 'Die Serververwaltung kann die eingestellten Limits prüfen. Größere Limits garantieren keinen geeigneten Treffer.'),
  UNSUPPORTED_MEDIA_FORMAT: problem('Audioformat nicht unterstützt', 'Die Quelle enthält kein für diesen Download unterstütztes Audioformat.', 'Prüfe die Quelle. Eine unveränderte Wiederholung behebt das Formatproblem nicht.'),
  INVALID_AUDIO: problem('Audiodatei konnte nicht geprüft werden', 'Die heruntergeladene Datei hat die Audioprüfung nicht bestanden.', 'Ein erneuter Versuch kann helfen. Eine ungeprüfte Datei wird nicht als erfolgreicher Download übernommen.', true),
  MEDIA_VERIFY_FAILED: problem('Dateiprüfung fehlgeschlagen', 'Die Datei hat die Prüfung vor der Übernahme in die Bibliothek nicht bestanden.', 'Versuche den Track erneut. Bleibt der Fehler bestehen, sollte die Serververwaltung die Diagnose prüfen.', true),
  TAGGING_FAILED: problem('Metadaten konnten nicht geschrieben werden', 'Die Verarbeitung der Audio-Metadaten ist fehlgeschlagen.', 'Die Serververwaltung sollte die Dateiverarbeitung prüfen, bevor der Track erneut gestartet wird.'),
  STORAGE_UNAVAILABLE: problem('Bibliotheksspeicher nicht erreichbar', 'YTMDL kann den eingebundenen Bibliotheksspeicher gerade nicht verwenden.', 'Prüfe die Speicherverbindung. Wartende Downloads werden fortgesetzt, sobald der Speicher wieder bereit ist.'),
  STORAGE_GUARD_MISMATCH: problem('Der Speicher stimmt nicht mit der Konfiguration überein', 'Die Speicherprüfung hat eine andere Speicheridentität erkannt.', 'Die Serververwaltung muss die Einbindung und Speicheridentität prüfen. Die Schutzprüfung sollte aktiv bleiben.'),
  STORAGE_READ_ONLY: problem('Bibliotheksspeicher ist schreibgeschützt', 'Auf den eingebundenen Speicher können keine Dateien geschrieben werden.', 'Die Serververwaltung sollte Einbindung und Schreibrechte prüfen.'),
  STORAGE_LOW_SPACE: problem('Zu wenig Platz in der Bibliothek', 'Die eingestellte Speicherreserve ist unterschritten.', 'Schaffe Platz auf dem Bibliotheksspeicher. Wartende Downloads können danach fortgesetzt werden.'),
  STAGING_LOW_SPACE: problem('Zu wenig Platz für Zwischendateien', 'Der Speicher für laufende Übertragungen hat zu wenig freien Platz.', 'Die Serververwaltung sollte den freien Speicher im Datenverzeichnis prüfen.'),
  PATH_CONFLICT: problem('Zielname ist bereits belegt', 'Der Track kann wegen eines Konflikts am vorgesehenen Ziel nicht abgelegt werden.', 'Die Serververwaltung sollte die betroffenen Bibliothekseinträge prüfen. Bestehende Dateien nicht ungeprüft löschen.'),
  TOOL_UNAVAILABLE: problem('Benötigtes Server-Werkzeug fehlt', 'Ein Werkzeug für Download oder Audioverarbeitung ist nicht verfügbar.', 'Die Serververwaltung sollte Installation und Werkzeugstatus prüfen.'),
  SHUTTING_DOWN: problem('Server wird neu gestartet', 'Der Server nimmt während des Herunterfahrens keine neue Arbeit an.', 'Warte, bis der Server wieder erreichbar ist. Laufende Arbeit wird beim Start wieder aufgenommen.', true),
  UNAUTHENTICATED: login,
  CSRF_INVALID: problem('Sicherheitsprüfung der Sitzung fehlgeschlagen', 'Die Anfrage konnte deiner aktuellen Sitzung nicht sicher zugeordnet werden.', 'Lade die Seite neu. Falls das nicht hilft, melde dich erneut an.'),
  INVALID_CREDENTIALS: problem('Anmeldung fehlgeschlagen', 'Benutzername oder Passwort wurden nicht akzeptiert.', 'Prüfe die Eingaben und versuche es erneut.'),
  FORBIDDEN: problem('Keine Berechtigung für diese Aktion', 'Dein Benutzerkonto darf diese Aktion nicht ausführen.', 'Wende dich an die Serververwaltung, wenn du diese Berechtigung benötigst.'),
  INVALID_REQUEST: problem('Eingabe konnte nicht verarbeitet werden', 'Der Server hat die Anfrage als ungültig abgelehnt.', 'Prüfe die eingegebenen Werte und die Hinweise an den Eingabefeldern.'),
  UNSUPPORTED_MEDIA_TYPE: problem('Dateityp nicht unterstützt', 'Diese Datei kann für die gewählte Aktion nicht verwendet werden.', 'Verwende den im Upload beschriebenen Dateityp.'),
  ALREADY_EXISTS: problem('Eintrag ist bereits vorhanden', 'Ein entsprechender Eintrag existiert bereits.', 'Aktualisiere die Ansicht und verwende den vorhandenen Eintrag.'),
  JOB_CANCELLED: problem('Auftrag wurde abgebrochen', 'Der Auftrag wurde ausdrücklich beendet.', 'Starte bei Bedarf einen neuen Auftrag.'),
  INTERNAL_ERROR: problem('Unerwarteter Fehler', 'Bei der Verarbeitung ist ein unerwarteter Fehler aufgetreten. Die genaue Ursache ist noch nicht bekannt.', 'Aktualisiere die Ansicht. Bleibt das Problem bestehen, gib Fehlercode und Anfrage-ID an die Serververwaltung weiter.', true),
  SESSION_IN_USE: problem('Mediensitzung wird gerade verwendet', 'Ein laufender Download verwendet diese Sitzung.', 'Warte, bis der Download beendet ist, bevor du die Sitzung änderst.'),
  LAST_ADMIN: problem('Letztes Administratorkonto wird benötigt', 'Mindestens ein Administratorkonto muss bestehen bleiben.', 'Lege zuerst ein anderes Administratorkonto an.'),
  SETUP_REQUIRED: problem('Server muss eingerichtet werden', 'Die Ersteinrichtung ist noch nicht abgeschlossen.', 'Öffne die Einrichtung und lege das erste Administratorkonto an.'),
  SETUP_COMPLETED: problem('Server ist bereits eingerichtet', 'Die Ersteinrichtung wurde bereits abgeschlossen.', 'Melde dich mit einem vorhandenen Benutzerkonto an.'),
  STALE_REPAIR: problem('Prüfergebnis ist nicht mehr aktuell', 'Der Bibliotheksbestand hat sich seit der Vorschau verändert.', 'Erstelle eine neue Vorschau, bevor du die Reparatur bestätigst.'),
}

const notFound = problem('Eintrag nicht gefunden', 'Der angefragte Eintrag ist nicht verfügbar.', 'Aktualisiere die Ansicht und wähle den Eintrag erneut aus.')
for (const code of ['PROVIDER_NOT_FOUND', 'ARTIST_NOT_FOUND', 'RELEASE_NOT_FOUND', 'JOB_NOT_FOUND', 'SUBSCRIPTION_NOT_FOUND', 'USER_NOT_FOUND', 'SESSION_NOT_FOUND', 'FILE_NOT_FOUND', 'PLAYLIST_NOT_FOUND']) {
  problems[code] = notFound
}

export function explainProblem(code?: string, status?: number): Problem {
  if (status === 0) {
    return problem('Verbindung zum Server unterbrochen', 'Die Anwendung erreicht den YTMDL-Server gerade nicht.', 'Prüfe deine Verbindung und lade die Ansicht erneut.', true)
  }
  if (code && code !== 'INTERNAL_ERROR' && problems[code]) return problems[code]
  if (status === 401) return login
  if (status === 403) return problems.FORBIDDEN!
  if (status === 404) return notFound
  if (status === 413) return problem('Datei ist zu groß', 'Die Anfrage überschreitet das Upload-Limit des Servers oder eines vorgeschalteten Proxys.', 'Cookie-Dateien dürfen bis zu 25 MiB groß sein. Bei kleineren Dateien sollte die Serververwaltung die Proxy-Limits prüfen.')
  if (status === 429) return problems.RATE_LIMITED!
  if (code === 'INTERNAL_ERROR') return problems.INTERNAL_ERROR!
  return problem('Aktion konnte nicht abgeschlossen werden', 'Die genaue Ursache ist noch nicht bekannt.', 'Aktualisiere die Ansicht. Bleibt das Problem bestehen, gib Fehlercode und Anfrage-ID an die Serververwaltung weiter.', status === undefined || status >= 500)
}
