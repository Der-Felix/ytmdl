import { useState } from 'react'
import {
  ArrowUpCircleIcon,
  BookOpenIcon,
  CheckCircle2Icon,
  CheckIcon,
  ClockIcon,
  CopyIcon,
  ExternalLinkIcon,
  FlaskConicalIcon,
  HelpCircleIcon,
  MinusCircleIcon,
  RefreshCwIcon,
  AlertCircleIcon,
  ShieldCheckIcon,
  UndoIcon,
} from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { Button, buttonVariants } from '@/components/ui/button'
import { Panel } from '@/components/ui/panel'
import { checkUpdate, setUpdateChannel } from '@/lib/api/system'
import { formatDateTime, formatRelative } from '@/lib/utils/format'
import { errorMessage, isAbortError } from '@/lib/api/client'
import { cn } from '@/lib/utils'
import type { UpdateChannel, UpdateFailure, UpdateState, UpdateStatus } from '@/types/api'
import { ReleaseNotesMarkdown } from './ReleaseNotesMarkdown'

interface UpdatePanelProps {
  initialData?: UpdateStatus
  onReload?: () => void
}

const channelLabel: Record<UpdateChannel, string> = {
  stable: 'Stabil',
  development: 'Entwicklung',
}

const failureText: Record<UpdateFailure, string> = {
  network_error: 'GitHub ist nicht erreichbar (Netzwerkfehler).',
  rate_limited: 'GitHub begrenzt gerade die Anfragen (Rate-Limit).',
  unexpected_status: 'GitHub hat unerwartet geantwortet.',
  invalid_response: 'Die Release-Angaben auf GitHub sind ungültig.',
  configuration: 'Die Update-Konfiguration des Servers ist ungültig.',
}

function PrereleaseBadge() {
  return (
    <Badge variant="outline" className="gap-1 border-amber-500/40 text-amber-600 dark:text-amber-400">
      <FlaskConicalIcon className="h-3 w-3" />
      Vorabversion
    </Badge>
  )
}

function CommandLine({ command }: { command: string }) {
  const [copied, setCopied] = useState(false)
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(command)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    } catch {
      // Ignore clipboard write failures
    }
  }
  return (
    <div className="flex items-center justify-between gap-2 rounded bg-muted/40 px-3 py-1.5 font-mono text-xs text-foreground border border-border/40">
      <code className="break-all">{command}</code>
      <Button
        variant="ghost"
        size="sm"
        className="h-6 shrink-0 px-2 text-xs gap-1 hover:bg-background/80"
        onClick={copy}
        title="Befehl in Zwischenablage kopieren"
      >
        {copied ? (
          <>
            <CheckIcon className="h-3 w-3 text-emerald-500" />
            <span className="text-emerald-500 text-[11px]">Kopiert!</span>
          </>
        ) : (
          <>
            <CopyIcon className="h-3 w-3 text-muted-foreground" />
            <span className="text-muted-foreground text-[11px]">Kopieren</span>
          </>
        )}
      </Button>
    </div>
  )
}

export function UpdatePanel({ initialData, onReload }: UpdatePanelProps) {
  const [data, setData] = useState<UpdateStatus | undefined>(initialData)
  const [isChecking, setIsChecking] = useState(false)
  const [checkError, setCheckError] = useState<string | null>(null)
  const [pendingChannel, setPendingChannel] = useState<UpdateChannel | null>(null)
  const [isSavingChannel, setIsSavingChannel] = useState(false)

  const handleManualCheck = async () => {
    setIsChecking(true)
    setCheckError(null)
    try {
      const refreshed = await checkUpdate()
      setData(refreshed)
      onReload?.()
    } catch (err) {
      if (!isAbortError(err)) {
        setCheckError(errorMessage(err))
      }
    } finally {
      setIsChecking(false)
    }
  }

  const applyChannel = async (channel: UpdateChannel) => {
    setIsSavingChannel(true)
    setCheckError(null)
    try {
      const refreshed = await setUpdateChannel(channel)
      setData(refreshed)
      setPendingChannel(null)
    } catch (err) {
      if (!isAbortError(err)) {
        setCheckError(errorMessage(err))
      }
    } finally {
      setIsSavingChannel(false)
    }
  }

  const current = data || initialData
  if (!current) {
    return null
  }
  const channel: UpdateChannel = current.channel ?? 'stable'

  const selectChannel = (next: UpdateChannel) => {
    if (next === channel) {
      setPendingChannel(null)
      return
    }
    // Prereleases are opted into deliberately; going back to stable needs no
    // confirmation because it never installs or downgrades anything.
    if (next === 'development') {
      setPendingChannel('development')
      return
    }
    void applyChannel(next)
  }

  const stateBadge = (state: UpdateState) => {
    switch (state) {
      case 'up_to_date':
        return (
          <Badge variant="success" className="gap-1">
            <CheckCircle2Icon className="h-3 w-3" />
            Aktuell
          </Badge>
        )
      case 'update_available':
        return (
          <Badge variant="default" className="gap-1 bg-sky-600 text-white hover:bg-sky-500">
            <ArrowUpCircleIcon className="h-3 w-3" />
            Update verfügbar
          </Badge>
        )
      case 'ahead_of_channel':
        return (
          <Badge variant="outline" className="gap-1 border-amber-500/40 text-amber-600 dark:text-amber-400">
            <ShieldCheckIcon className="h-3 w-3" />
            Neuer als Kanal
          </Badge>
        )
      case 'no_public_release':
      case 'no_channel_release':
        return (
          <Badge variant="outline" className="gap-1 text-muted-foreground">
            <HelpCircleIcon className="h-3 w-3" />
            {state === 'no_channel_release' ? 'Keine Vorabversion' : 'Kein Public Release'}
          </Badge>
        )
      case 'disabled':
        return (
          <Badge variant="neutral" className="gap-1">
            <MinusCircleIcon className="h-3 w-3" />
            Deaktiviert
          </Badge>
        )
      case 'development_version':
        return (
          <Badge variant="neutral" className="gap-1">
            Entwicklungsversion
          </Badge>
        )
      case 'unavailable':
      case 'invalid_release':
      default:
        return (
          <Badge variant="outline" className="gap-1 text-amber-500 border-amber-500/30">
            <AlertCircleIcon className="h-3 w-3" />
            Nicht verfügbar
          </Badge>
        )
    }
  }

  const commands = current.update_commands?.length ? current.update_commands : ['ytmdlctl update']

  return (
    <Panel className="space-y-5 p-5">
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div className="space-y-1">
          <div className="flex items-center gap-2">
            <span className="font-medium text-foreground">YTMDL Version</span>
            {stateBadge(current.state)}
          </div>
          <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground">
            <span className="flex items-center gap-1.5">
              Installiert: <strong className="font-semibold text-foreground">{current.current_version}</strong>
              {current.current_prerelease && <PrereleaseBadge />}
            </span>
            {current.latest_version && (
              <span className="flex items-center gap-1.5">
                {channel === 'development' ? 'Neueste Vorabversion:' : 'Neueste Version:'}{' '}
                <strong className="font-semibold text-foreground">{current.latest_version}</strong>
                {current.latest_prerelease && <PrereleaseBadge />}
              </span>
            )}
            {current.checked_at && (
              <span className="flex items-center gap-1">
                <ClockIcon className="h-3 w-3 inline" />
                Geprüft: {formatRelative(current.checked_at)}
              </span>
            )}
          </div>
        </div>

        <Button
          variant="outline"
          size="sm"
          onClick={handleManualCheck}
          disabled={isChecking || current.state === 'disabled'}
          className="self-start sm:self-auto gap-1.5"
        >
          <RefreshCwIcon className={`h-3.5 w-3.5 ${isChecking ? 'animate-spin' : ''}`} />
          {isChecking ? 'Wird geprüft...' : 'Nach Updates suchen'}
        </Button>
      </div>

      <fieldset className="space-y-2" disabled={isSavingChannel}>
        <legend className="text-xs font-medium text-foreground">Update-Kanal</legend>
        <div className="flex flex-wrap gap-2" role="radiogroup" aria-label="Update-Kanal">
          {(['stable', 'development'] as UpdateChannel[]).map((option) => (
            <Button
              key={option}
              type="button"
              role="radio"
              aria-checked={channel === option}
              variant={channel === option ? 'default' : 'outline'}
              size="sm"
              onClick={() => selectChannel(option)}
            >
              {channelLabel[option]}
            </Button>
          ))}
        </div>
        <p className="text-xs text-muted-foreground">
          {channel === 'development'
            ? 'Entwicklung: ausdrücklich veröffentlichte, qualifizierte Vorabversionen aus dem Entwicklungszweig.'
            : 'Stabil: nur reguläre, veröffentlichte Releases. Empfohlen für den laufenden Betrieb.'}{' '}
          Der Kanal bestimmt nur, welche Version angeboten wird. Er installiert nichts und startet nichts neu.
        </p>
        {pendingChannel === 'development' && (
          <div className="space-y-2 rounded-md border border-amber-500/30 bg-amber-500/5 p-3 text-xs">
            <p className="text-foreground">
              Vorabversionen sind qualifiziert, aber noch nicht als stabil freigegeben. Aktualisiert wird weiterhin nur
              über <code>ytmdlctl</code> auf dem Host, mit Backup und Prüfungen.
            </p>
            <div className="flex gap-2">
              <Button size="sm" onClick={() => void applyChannel('development')} disabled={isSavingChannel}>
                Entwicklungskanal aktivieren
              </Button>
              <Button size="sm" variant="outline" onClick={() => setPendingChannel(null)}>
                Abbrechen
              </Button>
            </div>
          </div>
        )}
      </fieldset>

      {checkError && (
        <div className="rounded-md border border-destructive/20 bg-destructive/10 p-3 text-xs text-destructive">
          {checkError}
        </div>
      )}

      {current.state === 'update_available' && (
        <div className="rounded-lg border border-sky-500/20 bg-sky-500/5 p-4 space-y-3">
          <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
            <div>
              <h4 className="text-sm font-semibold text-foreground">
                {current.release_name || `Version ${current.latest_version} verfügbar`}
              </h4>
              {current.published_at && (
                <p className="text-xs text-muted-foreground">
                  Veröffentlicht am {formatDateTime(current.published_at)}
                </p>
              )}
            </div>

            <div className="flex flex-wrap items-center gap-2">
              <a
                href="/ytmdl/updates"
                target="_blank"
                rel="noopener noreferrer"
                className={cn(buttonVariants({ variant: 'outline', size: 'sm' }), 'gap-1.5 self-start sm:self-auto')}
              >
                <BookOpenIcon className="h-3.5 w-3.5" />
                Dokumentation
              </a>
              {current.release_url && (
                <a
                  href={current.release_url}
                  target="_blank"
                  rel="noopener noreferrer"
                  className={cn(buttonVariants({ variant: 'default', size: 'sm' }), 'gap-1.5 self-start sm:self-auto')}
                >
                  <ExternalLinkIcon className="h-3.5 w-3.5" />
                  Auf GitHub ansehen
                </a>
              )}
            </div>
          </div>

          <div className="space-y-1.5 rounded-md border border-border/60 bg-background/50 p-3">
            <p className="text-xs text-muted-foreground">
              Auf dem YTMDL-Host ausführen:
            </p>
            {commands.map((command) => (
              <CommandLine key={command} command={command} />
            ))}
            {current.latest_prerelease && (
              <p className="text-xs text-muted-foreground">
                Vorabversion: dafür <code>ytmdlctl {current.latest_version}</code> aus den Assets desselben Releases
                verwenden und vorher mit <code>SHA256SUMS</code> prüfen.
              </p>
            )}
          </div>

          {current.release_notes && (
            <ReleaseNotesMarkdown content={current.release_notes} />
          )}
        </div>
      )}

      {current.state === 'ahead_of_channel' && (
        <div className="space-y-2 rounded-lg border border-amber-500/20 bg-amber-500/5 p-4 text-xs">
          <p className="text-foreground">
            Die installierte Version {current.current_version} ist neuer als die neueste Version
            {current.latest_version ? ` ${current.latest_version}` : ''} im Kanal {channelLabel[channel]}. Es wird nichts
            automatisch zurückgestuft und kein älteres Image gestartet.
          </p>
          <p className="flex items-center gap-1 text-muted-foreground">
            <UndoIcon className="h-3 w-3" />
            Eine Rückkehr ist nur ausdrücklich über den geprüften Rollback- oder Restore-Ablauf möglich:
          </p>
          <CommandLine command="ytmdlctl rollback" />
          <CommandLine command="ytmdlctl recover status" />
          <p className="text-muted-foreground">
            <code>rollback</code> geht direkt nach einem Update auf den vorherigen Stand zurück, solange das
            Datenbankschema unverändert ist; sonst über ein Backup mit <code>ytmdlctl recover restore</code>.
          </p>
        </div>
      )}

      {current.newer_stable_version && (
        <p className="text-xs text-muted-foreground">
          Ein neueres stabiles Release ({current.newer_stable_version}) ist verfügbar. Zum Installieren den Kanal
          „Stabil“ wählen.
        </p>
      )}

      {current.state === 'no_public_release' && (
        <p className="text-xs text-muted-foreground">
          Noch keine öffentliche Stable-Version auf GitHub verfügbar. Neue Releases werden automatisch hier angezeigt.
        </p>
      )}

      {current.state === 'no_channel_release' && (
        <p className="text-xs text-muted-foreground">
          Im Entwicklungskanal ist derzeit keine qualifizierte Vorabversion veröffentlicht.
        </p>
      )}

      {(current.state === 'unavailable' || current.state === 'invalid_release') && (
        <p className="text-xs text-muted-foreground">
          {current.failure ? `${failureText[current.failure]} ` : ''}
          Updateprüfung momentan nicht verfügbar – das bedeutet nicht, dass kein Update vorliegt. Das Backend versucht es
          bei Bedarf automatisch erneut.
        </p>
      )}

      {current.state === 'disabled' && (
        <p className="text-xs text-muted-foreground">
          Die automatische Updateprüfung ist serverseitig deaktiviert (<code className="text-foreground">MUSICDL_UPDATE_CHECKS_ENABLED=false</code>).
        </p>
      )}
    </Panel>
  )
}
