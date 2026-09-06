import { useEffect, useState } from 'react'
import {
  AlertTriangleIcon,
  ClockIcon,
  InfoIcon,
  KeyIcon,
  LayersIcon,
  PlusIcon,
  RefreshCwIcon,
  Trash2Icon,
  XIcon,
} from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Panel } from '@/components/ui/panel'
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  ErrorState,
  ListSkeleton,
  LoadingRegion,
} from '@/components/ui/state-view'
import { useAsync } from '@/hooks/useAsync'
import {
  createMediaSession,
  deleteMediaSession,
  isLegacySession,
  listMediaSessions,
  mapMediaSessionError,
  probeMediaSession,
  updateMediaSession,
  uploadMediaSessionCookies,
} from '@/lib/api/mediaSessions'
import { formatRelative } from '@/lib/utils/format'
import { cn } from '@/lib/utils'
import type { MediaSession, MediaSessionHealthStatus } from '@/types/api'

interface NotificationState {
  type: 'success' | 'error' | 'info'
  message: string
}

function getHealthBadgeInfo(session: MediaSession): {
  label: string
  variant: 'success' | 'warning' | 'destructive' | 'neutral'
} {
  if (!session.has_credentials) {
    return { label: 'Cookies fehlen', variant: 'destructive' }
  }

  switch (session.health_status) {
    case 'healthy':
      return { label: 'Bereit', variant: 'success' }
    case 'rate_limited':
      return { label: 'Rate-Limit', variant: 'warning' }
    case 'bot_challenge':
      return { label: 'Bot-Prüfung erforderlich', variant: 'destructive' }
    case 'auth_failed':
      return { label: 'Anmeldung erforderlich', variant: 'destructive' }
    case 'cooldown':
      return { label: 'Abkühlphase / Pause', variant: 'warning' }
    case 'unknown':
    default:
      return { label: 'Ungeprüft', variant: 'neutral' }
  }
}

function getHealthExplanation(status: MediaSessionHealthStatus): string | null {
  switch (status) {
    case 'rate_limited':
      return 'YouTube begrenzt diese Session derzeit. YTMDL verwendet sie bis zum Ende der Abkühlzeit nicht.'
    case 'bot_challenge':
      return 'Diese YouTube-Session muss erneuert werden. Exportiere neue Cookies aus einer funktionierenden Browser-Sitzung.'
    case 'auth_failed':
      return 'Die Anmeldung dieser Session ist nicht mehr gültig.'
    default:
      return null
  }
}

function formatCooldown(
  cooldownUntil: string | null | undefined,
  currentNow: number,
): string | null {
  if (!cooldownUntil) return null
  const diffMs = new Date(cooldownUntil).getTime() - currentNow
  if (diffMs <= 0) return null
  const minutes = Math.max(1, Math.ceil(diffMs / 60000))
  return `erneut verfügbar in ${minutes} Min.`
}

export function MediaSessionsPanel() {
  const { state: asyncState, reload: reloadSessions, setData: setSessionsData } = useAsync(
    (signal) => listMediaSessions({ signal }),
    [],
  )

  const sessions = asyncState.status === 'success' ? asyncState.data : []
  const loading = asyncState.status === 'loading'
  const error =
    asyncState.status === 'error'
      ? mapMediaSessionError(asyncState.error, 'Fehler beim Laden der Media-Sessions.')
      : null

  const [notification, setNotification] = useState<NotificationState | null>(null)

  // Shared clock for cooldown countdowns (ticking every 15s to prevent per-row timers)
  const [now, setNow] = useState(() => Date.now())

  // In-flight per-row action tracking
  const [probingIds, setProbingIds] = useState<Set<string>>(new Set())
  const [togglingIds, setTogglingIds] = useState<Set<string>>(new Set())

  // Dialog states
  const [addDialogOpen, setAddDialogOpen] = useState(false)
  const [replaceTarget, setReplaceTarget] = useState<MediaSession | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<MediaSession | null>(null)

  // Shared timer interval
  useEffect(() => {
    const timer = window.setInterval(() => {
      setNow(Date.now())
    }, 15000)
    return () => window.clearInterval(timer)
  }, [])

  // Auto-dismiss notification after 6s
  useEffect(() => {
    if (!notification) return
    const timer = window.setTimeout(() => {
      setNotification(null)
    }, 6000)
    return () => window.clearTimeout(timer)
  }, [notification])

  // Probe action
  async function handleProbe(session: MediaSession) {
    if (probingIds.has(session.id)) return

    setProbingIds((prev) => new Set(prev).add(session.id))
    try {
      const res = await probeMediaSession(session.id)
      setSessionsData((prev) =>
        prev.map((s) => (s.id === session.id ? res.session : s)),
      )
      if (res.probe.status === 'healthy') {
        setNotification({
          type: 'success',
          message: `Session-Test für "${session.name}" erfolgreich: Bereit.`,
        })
      } else {
        const badgeInfo = getHealthBadgeInfo(res.session)
        const reason = getHealthExplanation(res.probe.status) || badgeInfo.label
        setNotification({
          type: 'info',
          message: `Session-Test für "${session.name}": ${reason}`,
        })
      }
    } catch (err) {
      setNotification({
        type: 'error',
        message: `Test fehlgeschlagen: ${mapMediaSessionError(err)}`,
      })
    } finally {
      setProbingIds((prev) => {
        const next = new Set(prev)
        next.delete(session.id)
        return next
      })
    }
  }

  // Enable/Disable toggle
  async function handleToggleEnabled(session: MediaSession) {
    if (isLegacySession(session) || togglingIds.has(session.id)) return

    setTogglingIds((prev) => new Set(prev).add(session.id))
    try {
      const updated = await updateMediaSession(session.id, {
        enabled: !session.enabled,
      })
      setSessionsData((prev) =>
        prev.map((s) => (s.id === session.id ? updated : s)),
      )
      setNotification({
        type: 'success',
        message: updated.enabled
          ? `Session "${session.name}" aktiviert.`
          : `Session "${session.name}" deaktiviert.`,
      })
    } catch (err) {
      setNotification({
        type: 'error',
        message: `Änderung fehlgeschlagen: ${mapMediaSessionError(err)}`,
      })
    } finally {
      setTogglingIds((prev) => {
        const next = new Set(prev)
        next.delete(session.id)
        return next
      })
    }
  }

  // Calculate family overview stats
  const enabledSessions = sessions.filter((s) => s.enabled)
  const healthyCount = enabledSessions.filter(
    (s) => s.has_credentials && s.health_status === 'healthy',
  ).length

  let familyStatusText = 'Anonymer Modus'
  let familyStatusVariant: 'outline' | 'success' | 'warning' | 'destructive' | 'neutral' = 'outline'

  if (sessions.length > 0) {
    if (healthyCount > 0 && healthyCount === enabledSessions.length) {
      familyStatusText = 'Betriebsbereit'
      familyStatusVariant = 'success'
    } else if (healthyCount > 0) {
      familyStatusText = 'Eingeschränkt'
      familyStatusVariant = 'warning'
    } else if (enabledSessions.length > 0) {
      familyStatusText = 'Nicht betriebsbereit'
      familyStatusVariant = 'destructive'
    } else {
      familyStatusText = 'Deaktiviert'
      familyStatusVariant = 'neutral'
    }
  }

  return (
    <div className="space-y-4">
      {/* Family Overview Card */}
      <Panel className="space-y-4 p-4 sm:p-5">
        <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <div className="space-y-1">
            <div className="flex flex-wrap items-center gap-2.5">
              <h3 className="text-sm font-semibold text-foreground">YouTube</h3>
              <Badge variant="outline">Audioquelle</Badge>
              <Badge variant={familyStatusVariant}>{familyStatusText}</Badge>
            </div>
            <p className="text-xs text-muted-foreground">
              Suche: YouTube Music → YouTube Fallback ·{' '}
              {sessions.length > 0
                ? `${healthyCount} bereit / ${sessions.length} eingerichtet`
                : 'Standardmäßig anonymer Modus aktiv'}
            </p>
          </div>

          <Button
            variant="default"
            size="sm"
            onClick={() => setAddDialogOpen(true)}
            className="w-full sm:w-auto"
          >
            <PlusIcon className="size-4" />
            Session hinzufügen
          </Button>
        </div>

        {/* Notification Banner */}
        {notification && (
          <div
            role="status"
            aria-live="polite"
            className={cn(
              'flex items-center justify-between gap-3 rounded-xl border p-3 text-xs leading-relaxed transition-all',
              notification.type === 'success' &&
                'border-success/30 bg-success/10 text-success',
              notification.type === 'error' &&
                'border-destructive/30 bg-destructive/10 text-destructive',
              notification.type === 'info' &&
                'border-warning/30 bg-warning/10 text-warning',
            )}
          >
            <span>{notification.message}</span>
            <button
              type="button"
              onClick={() => setNotification(null)}
              className="text-current opacity-70 hover:opacity-100"
              aria-label="Hinweis schließen"
            >
              <XIcon className="size-4" />
            </button>
          </div>
        )}

        {/* Sessions Content */}
        {loading && (
          <LoadingRegion label="YouTube-Sessions werden geladen">
            <ListSkeleton rows={3} />
          </LoadingRegion>
        )}

        {error && (
          <ErrorState error={error} onRetry={reloadSessions} />
        )}

        {!loading && !error && sessions.length === 0 && (
          <div className="flex flex-col items-center justify-center rounded-xl border border-dashed border-border py-8 px-4 text-center">
            <div className="flex size-10 items-center justify-center rounded-full bg-white/5 text-muted-foreground mb-3">
              <LayersIcon className="size-5" />
            </div>
            <h4 className="text-sm font-medium text-foreground">
              Keine YouTube-Session eingerichtet
            </h4>
            <p className="mt-1 max-w-md text-xs leading-relaxed text-muted-foreground">
              YTMDL greift standardmäßig anonym auf YouTube zu. Für höhere Zuverlässigkeit
              und unterbrechungsfreie Downloads können YouTube-Sitzungen mit Cookies hinterlegt werden.
            </p>
            <Button
              variant="outline"
              size="sm"
              onClick={() => setAddDialogOpen(true)}
              className="mt-4"
            >
              <PlusIcon className="size-4" />
              Session hinzufügen
            </Button>
          </div>
        )}

        {!loading && !error && sessions.length > 0 && (
          <div className="space-y-3">
            {/* Desktop Table */}
            <div className="hidden md:block overflow-x-auto">
              <table className="w-full text-left text-xs">
                <thead>
                  <tr className="border-b border-border text-muted-foreground">
                    <th className="pb-2.5 font-medium">Session</th>
                    <th className="pb-2.5 font-medium">Status & Diagnose</th>
                    <th className="pb-2.5 font-medium text-center">Aktiv</th>
                    <th className="pb-2.5 font-medium text-right">Aktionen</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-border/60">
                  {sessions.map((s) => {
                    const isLegacy = isLegacySession(s)
                    const badge = getHealthBadgeInfo(s)
                    const isProbing = probingIds.has(s.id)
                    const isToggling = togglingIds.has(s.id)
                    const cooldownText = formatCooldown(s.cooldown_until, now)
                    const explanation = getHealthExplanation(s.health_status)

                    return (
                      <tr key={s.id} className="transition-colors hover:bg-white/2">
                        <td className="py-3 pr-4 align-top">
                          <div className="space-y-1">
                            <div className="flex items-center gap-2">
                              <span className="font-medium text-foreground">
                                {s.name}
                              </span>
                              {isLegacy && (
                                <Badge variant="outline">Extern konfiguriert</Badge>
                              )}
                              {s.in_use && (
                                <Badge variant="warning">In Verwendung</Badge>
                              )}
                            </div>
                            <div className="text-[0.75rem] text-muted-foreground">
                              {s.last_success_at ? (
                                <span>
                                  Zuletzt erfolgreich{' '}
                                  {formatRelative(s.last_success_at)}
                                </span>
                              ) : (
                                <span>Noch kein Download erfolgreich</span>
                              )}
                            </div>
                          </div>
                        </td>

                        <td className="py-3 px-4 align-top">
                          <div className="space-y-1">
                            <div className="flex items-center gap-2">
                              <Badge variant={badge.variant}>{badge.label}</Badge>
                              {cooldownText && (
                                <span className="flex items-center gap-1 text-[0.75rem] text-warning">
                                  <ClockIcon className="size-3" />
                                  {cooldownText}
                                </span>
                              )}
                            </div>
                            {explanation && (
                              <p className="max-w-xs text-[0.75rem] leading-relaxed text-muted-foreground">
                                {explanation}
                              </p>
                            )}
                          </div>
                        </td>

                        <td className="py-3 px-4 align-top text-center">
                          {isLegacy ? (
                            <span className="text-[0.75rem] text-muted-foreground" title="Extern konfigurierte Sessions können nicht im UI umgeschaltet werden.">
                              Aktiv
                            </span>
                          ) : (
                            <label className="inline-flex cursor-pointer items-center justify-center">
                              <Checkbox
                                checked={s.enabled}
                                disabled={isToggling}
                                onCheckedChange={() => handleToggleEnabled(s)}
                                aria-label={`Session "${s.name}" aktivieren oder deaktivieren`}
                              />
                            </label>
                          )}
                        </td>

                        <td className="py-3 pl-4 align-top text-right">
                          <div className="flex items-center justify-end gap-1.5">
                            <Button
                              variant="ghost"
                              size="icon-sm"
                              title="Session testen"
                              aria-label={`Session "${s.name}" testen`}
                              disabled={isProbing}
                              onClick={() => handleProbe(s)}
                            >
                              <RefreshCwIcon
                                className={cn('size-4', isProbing && 'animate-spin text-primary')}
                              />
                            </Button>

                            {!isLegacy && (
                              <>
                                <Button
                                  variant="ghost"
                                  size="icon-sm"
                                  title="Cookies ersetzen"
                                  aria-label={`Cookies für "${s.name}" ersetzen`}
                                  onClick={() => setReplaceTarget(s)}
                                >
                                  <KeyIcon className="size-4" />
                                </Button>
                                <Button
                                  variant="ghost"
                                  size="icon-sm"
                                  title="Session löschen"
                                  aria-label={`Session "${s.name}" löschen`}
                                  className="text-destructive hover:text-destructive"
                                  onClick={() => setDeleteTarget(s)}
                                >
                                  <Trash2Icon className="size-4" />
                                </Button>
                              </>
                            )}
                          </div>
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </div>

            {/* Mobile Stacked Cards */}
            <div className="md:hidden space-y-3">
              {sessions.map((s) => {
                const isLegacy = isLegacySession(s)
                const badge = getHealthBadgeInfo(s)
                const isProbing = probingIds.has(s.id)
                const isToggling = togglingIds.has(s.id)
                const cooldownText = formatCooldown(s.cooldown_until, now)
                const explanation = getHealthExplanation(s.health_status)

                return (
                  <div
                    key={s.id}
                    className="space-y-3 rounded-xl border border-border bg-white/3 p-3.5 text-xs"
                  >
                    <div className="flex items-start justify-between gap-2">
                      <div className="space-y-1">
                        <div className="flex flex-wrap items-center gap-1.5">
                          <span className="font-medium text-foreground">{s.name}</span>
                          {isLegacy && (
                            <Badge variant="outline">Extern konfiguriert</Badge>
                          )}
                          {s.in_use && (
                            <Badge variant="warning">In Verwendung</Badge>
                          )}
                        </div>
                        <div className="flex flex-wrap items-center gap-2">
                          <Badge variant={badge.variant}>{badge.label}</Badge>
                          {cooldownText && (
                            <span className="flex items-center gap-1 text-[0.75rem] text-warning">
                              <ClockIcon className="size-3" />
                              {cooldownText}
                            </span>
                          )}
                        </div>
                      </div>

                      {/* Enable toggle */}
                      {!isLegacy ? (
                        <div className="flex items-center gap-1.5 shrink-0 pt-0.5">
                          <span className="text-[0.7rem] text-muted-foreground">
                            {s.enabled ? 'Aktiv' : 'Inaktiv'}
                          </span>
                          <Checkbox
                            checked={s.enabled}
                            disabled={isToggling}
                            onCheckedChange={() => handleToggleEnabled(s)}
                            aria-label={`Session "${s.name}" aktivieren oder deaktivieren`}
                          />
                        </div>
                      ) : (
                        <span className="text-[0.7rem] text-muted-foreground pt-0.5">
                          Aktiv
                        </span>
                      )}
                    </div>

                    {explanation && (
                      <p className="text-[0.75rem] leading-relaxed text-muted-foreground">
                        {explanation}
                      </p>
                    )}

                    <div className="flex items-center justify-between gap-2 border-t border-border/50 pt-2.5">
                      <span className="text-[0.7rem] text-muted-foreground">
                        {s.last_success_at
                          ? `Zuletzt: ${formatRelative(s.last_success_at)}`
                          : 'Noch kein Download'}
                      </span>

                      <div className="flex items-center gap-1">
                        <Button
                          variant="ghost"
                          size="sm"
                          disabled={isProbing}
                          onClick={() => handleProbe(s)}
                          className="h-7 px-2 text-[0.75rem] gap-1"
                        >
                          <RefreshCwIcon
                            className={cn('size-3.5', isProbing && 'animate-spin text-primary')}
                          />
                          Testen
                        </Button>

                        {!isLegacy && (
                          <>
                            <Button
                              variant="ghost"
                              size="sm"
                              onClick={() => setReplaceTarget(s)}
                              className="h-7 px-2 text-[0.75rem] gap-1"
                            >
                              <KeyIcon className="size-3.5" />
                              Cookies
                            </Button>
                            <Button
                              variant="ghost"
                              size="sm"
                              onClick={() => setDeleteTarget(s)}
                              className="h-7 px-2 text-[0.75rem] gap-1 text-destructive hover:text-destructive"
                            >
                              <Trash2Icon className="size-3.5" />
                              Löschen
                            </Button>
                          </>
                        )}
                      </div>
                    </div>
                  </div>
                )
              })}
            </div>
          </div>
        )}
      </Panel>

      {/* Add Session Dialog */}
      <AddSessionDialog
        open={addDialogOpen}
        onOpenChange={setAddDialogOpen}
        onSuccess={() => {
          reloadSessions()
          setNotification({
            type: 'success',
            message: 'YouTube-Session wurde hinzugefügt.',
          })
        }}
        onPartialFailure={() => {
          // Metadata was created, but upload failed: refresh list so credential-less session is visible
          reloadSessions()
        }}
      />

      {/* Replace Cookies Dialog */}
      {replaceTarget && (
        <ReplaceCookiesDialog
          session={replaceTarget}
          open={!!replaceTarget}
          onOpenChange={(open) => {
            if (!open) setReplaceTarget(null)
          }}
          onSuccess={() => {
            setReplaceTarget(null)
            reloadSessions()
            setNotification({
              type: 'success',
              message: 'Cookies wurden erfolgreich aktualisiert.',
            })
          }}
        />
      )}

      {/* Delete Confirmation Dialog */}
      {deleteTarget && (
        <DeleteConfirmDialog
          session={deleteTarget}
          open={!!deleteTarget}
          onOpenChange={(open) => {
            if (!open) setDeleteTarget(null)
          }}
          onSuccess={() => {
            setDeleteTarget(null)
            reloadSessions()
            setNotification({
              type: 'success',
              message: 'Session wurde gelöscht.',
            })
          }}
        />
      )}
    </div>
  )
}

function AddSessionDialog({
  open,
  onOpenChange,
  onSuccess,
  onPartialFailure,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSuccess: () => void
  onPartialFailure: () => void
}) {
  const [name, setName] = useState('')
  const [file, setFile] = useState<File | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  function handleFileChange(e: React.ChangeEvent<HTMLInputElement>) {
    const selected = e.target.files?.[0] || null
    setFile(selected)
    setError(null)
  }

  function handleClose(nextOpen: boolean) {
    if (!submitting) {
      if (!nextOpen) {
        setName('')
        setFile(null)
        setError(null)
      }
      onOpenChange(nextOpen)
    }
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    if (submitting) return

    const trimmedName = name.trim()
    if (!trimmedName) {
      setError('Bitte gib einen Namen für die Session ein.')
      return
    }

    if (!file) {
      setError('Bitte wähle eine Cookie-Datei aus.')
      return
    }

    setSubmitting(true)
    setError(null)

    let createdSession: MediaSession | null = null

    try {
      // Step 1: Create session metadata
      createdSession = await createMediaSession({
        name: trimmedName,
        provider_family: 'youtube',
      })
    } catch (err) {
      setError(mapMediaSessionError(err, 'Fehler beim Erstellen der Session.'))
      setSubmitting(false)
      return
    }

    try {
      // Step 2: Upload credentials
      await uploadMediaSessionCookies(createdSession.id, file)
      setName('')
      setFile(null)
      onOpenChange(false)
      onSuccess()
    } catch (uploadErr) {
      onPartialFailure()
      setError(
        `Session "${trimmedName}" wurde erstellt, aber der Cookie-Upload ist fehlgeschlagen (${mapMediaSessionError(
          uploadErr,
        )}). Die Session verbleibt im Zustand "Cookies fehlen" und kann über "Cookies ersetzen" aktualisiert werden.`,
      )
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>Session hinzufügen</DialogTitle>
          <DialogDescription>
            Richte eine authentifizierte YouTube-Session mit Cookies ein.
          </DialogDescription>
        </DialogHeader>

        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="space-y-1.5">
            <label htmlFor="session-name-input" className="block text-xs font-medium text-foreground">
              Name der Session
            </label>
            <Input
              id="session-name-input"
              value={name}
              onChange={(e) => {
                setName(e.target.value)
                setError(null)
              }}
              placeholder="z. B. YouTube Account"
              disabled={submitting}
              autoFocus
            />
          </div>

          <div className="space-y-1.5">
            <label htmlFor="cookie-file-input" className="block text-xs font-medium text-foreground">
              Cookie-Datei (Netscape cookies.txt)
            </label>
            <Input
              id="cookie-file-input"
              type="file"
              accept=".txt,text/plain"
              onChange={handleFileChange}
              disabled={submitting}
              className="cursor-pointer file:cursor-pointer file:border-0 file:bg-transparent file:text-xs file:font-medium file:text-foreground"
            />
            {file && (
              <p className="text-[0.75rem] text-success">Cookie-Datei ausgewählt</p>
            )}
          </div>

          <div className="rounded-xl border border-border bg-white/3 p-3 text-[0.75rem] leading-relaxed text-muted-foreground space-y-1">
            <div className="flex items-center gap-1.5 font-medium text-foreground">
              <InfoIcon className="size-3.5 text-primary" />
              <span>Sicherheit & Format</span>
            </div>
            <p>
              Exportiere Cookies im Netscape cookies.txt-Format aus einer funktionierenden YouTube-Sitzung.
              Die Datei wird serverseitig gespeichert und in YTMDL niemals im Klartext angezeigt.
            </p>
          </div>

          {error && (
            <div
              role="alert"
              className="flex items-start gap-2 rounded-xl border border-destructive/30 bg-destructive/10 p-3 text-xs text-destructive leading-relaxed"
            >
              <AlertTriangleIcon className="size-4 shrink-0 mt-0.5" />
              <span>{error}</span>
            </div>
          )}

          <DialogFooter className="gap-2 sm:gap-0 pt-2">
            <DialogClose
              render={
                <Button variant="ghost" disabled={submitting}>
                  Abbrechen
                </Button>
              }
            />
            <Button type="submit" variant="default" disabled={submitting}>
              {submitting ? (
                <>
                  <RefreshCwIcon className="size-4 animate-spin" />
                  Wird gespeichert...
                </>
              ) : (
                'Session speichern'
              )}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function ReplaceCookiesDialog({
  session,
  open,
  onOpenChange,
  onSuccess,
}: {
  session: MediaSession
  open: boolean
  onOpenChange: (open: boolean) => void
  onSuccess: () => void
}) {
  const [file, setFile] = useState<File | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  function handleClose(nextOpen: boolean) {
    if (!submitting) {
      if (!nextOpen) {
        setFile(null)
        setError(null)
      }
      onOpenChange(nextOpen)
    }
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    if (submitting) return

    if (!file) {
      setError('Bitte wähle eine Cookie-Datei aus.')
      return
    }

    setSubmitting(true)
    setError(null)

    try {
      await uploadMediaSessionCookies(session.id, file)
      setFile(null)
      onOpenChange(false)
      onSuccess()
    } catch (err) {
      const mapped = mapMediaSessionError(err)
      // Check if it was 409 conflict
      if (mapped.includes('wird gerade für einen Download verwendet')) {
        setError(mapped)
      } else {
        setError(
          `Die neuen Cookies konnten nicht verwendet werden. Die bisherigen Cookies bleiben aktiv. (${mapped})`,
        )
      }
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>Cookies ersetzen</DialogTitle>
          <DialogDescription>
            Neue Cookie-Datei für Session &bdquo;{session.name}&ldquo; hochladen.
          </DialogDescription>
        </DialogHeader>

        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="space-y-1.5">
            <label htmlFor="replace-cookie-input" className="block text-xs font-medium text-foreground">
              Neue Cookie-Datei (Netscape cookies.txt)
            </label>
            <Input
              id="replace-cookie-input"
              type="file"
              accept=".txt,text/plain"
              onChange={(e) => {
                setFile(e.target.files?.[0] || null)
                setError(null)
              }}
              disabled={submitting}
              className="cursor-pointer file:cursor-pointer file:border-0 file:bg-transparent file:text-xs file:font-medium file:text-foreground"
            />
            {file && (
              <p className="text-[0.75rem] text-success">Cookie-Datei ausgewählt</p>
            )}
          </div>

          <div className="rounded-xl border border-border bg-white/3 p-3 text-[0.75rem] leading-relaxed text-muted-foreground space-y-1">
            <div className="flex items-center gap-1.5 font-medium text-foreground">
              <InfoIcon className="size-3.5 text-primary" />
              <span>Sicherheitsgarantie</span>
            </div>
            <p>
              Sollte die Prüfung der neuen Cookie-Datei fehlschlagen, bleiben die bisherigen funktionierenden Cookies aktiv.
            </p>
          </div>

          {error && (
            <div
              role="alert"
              className="flex items-start gap-2 rounded-xl border border-destructive/30 bg-destructive/10 p-3 text-xs text-destructive leading-relaxed"
            >
              <AlertTriangleIcon className="size-4 shrink-0 mt-0.5" />
              <span>{error}</span>
            </div>
          )}

          <DialogFooter className="gap-2 sm:gap-0 pt-2">
            <DialogClose
              render={
                <Button variant="ghost" disabled={submitting}>
                  Abbrechen
                </Button>
              }
            />
            <Button type="submit" variant="default" disabled={submitting}>
              {submitting ? (
                <>
                  <RefreshCwIcon className="size-4 animate-spin" />
                  Wird geprüft & aktualisiert...
                </>
              ) : (
                'Cookies aktualisieren'
              )}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function DeleteConfirmDialog({
  session,
  open,
  onOpenChange,
  onSuccess,
}: {
  session: MediaSession
  open: boolean
  onOpenChange: (open: boolean) => void
  onSuccess: () => void
}) {
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  function handleClose(nextOpen: boolean) {
    if (!submitting) {
      if (!nextOpen) setError(null)
      onOpenChange(nextOpen)
    }
  }

  async function handleDelete() {
    if (submitting) return

    setSubmitting(true)
    setError(null)

    try {
      await deleteMediaSession(session.id)
      onOpenChange(false)
      onSuccess()
    } catch (err) {
      setError(mapMediaSessionError(err, 'Fehler beim Löschen der Session.'))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>Session löschen</DialogTitle>
          <DialogDescription>
            Möchtest du die Session &bdquo;{session.name}&ldquo; wirklich löschen?
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-3 text-xs leading-relaxed text-muted-foreground">
          <p>
            Das Löschen entfernt die gespeicherte Cookie-Datei dieser Session unwiderruflich vom Server.
          </p>
          {error && (
            <div
              role="alert"
              className="flex items-start gap-2 rounded-xl border border-destructive/30 bg-destructive/10 p-3 text-xs text-destructive leading-relaxed"
            >
              <AlertTriangleIcon className="size-4 shrink-0 mt-0.5" />
              <span>{error}</span>
            </div>
          )}
        </div>

        <DialogFooter className="gap-2 sm:gap-0 pt-2">
          <DialogClose
            render={
              <Button variant="ghost" disabled={submitting}>
                Abbrechen
              </Button>
            }
          />
          <Button
            type="button"
            variant="destructive"
            disabled={submitting}
            onClick={handleDelete}
          >
            {submitting ? (
              <>
                <RefreshCwIcon className="size-4 animate-spin" />
                Wird gelöscht...
              </>
            ) : (
              'Session löschen'
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
