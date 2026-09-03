import { useState } from 'react'
import {
  ArrowUpCircleIcon,
  CheckCircle2Icon,
  ClockIcon,
  ExternalLinkIcon,
  HelpCircleIcon,
  MinusCircleIcon,
  RefreshCwIcon,
  AlertCircleIcon,
} from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { Button, buttonVariants } from '@/components/ui/button'
import { Panel } from '@/components/ui/panel'
import { checkUpdate } from '@/lib/api/system'
import { formatDateTime, formatRelative } from '@/lib/utils/format'
import { errorMessage, isAbortError } from '@/lib/api/client'
import { cn } from '@/lib/utils'
import type { UpdateState, UpdateStatus } from '@/types/api'

interface UpdatePanelProps {
  initialData?: UpdateStatus
  onReload?: () => void
}

export function UpdatePanel({ initialData, onReload }: UpdatePanelProps) {
  const [data, setData] = useState<UpdateStatus | undefined>(initialData)
  const [isChecking, setIsChecking] = useState(false)
  const [checkError, setCheckError] = useState<string | null>(null)

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

  const current = data || initialData
  if (!current) {
    return null
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
      case 'no_public_release':
        return (
          <Badge variant="outline" className="gap-1 text-muted-foreground">
            <HelpCircleIcon className="h-3 w-3" />
            Kein Public Release
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

  return (
    <Panel className="space-y-5 p-5">
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div className="space-y-1">
          <div className="flex items-center gap-2">
            <span className="font-medium text-foreground">YTMDL Version</span>
            {stateBadge(current.state)}
          </div>
          <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground">
            <span>
              Installiert: <strong className="font-semibold text-foreground">{current.current_version}</strong>
            </span>
            {current.latest_version && (
              <span>
                Neueste Version: <strong className="font-semibold text-foreground">{current.latest_version}</strong>
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

          {current.release_notes && (
            <div className="space-y-1">
              <span className="text-xs font-medium text-muted-foreground">Release Notes:</span>
              <pre className="max-h-48 overflow-y-auto whitespace-pre-wrap rounded-md bg-muted/40 p-3 font-mono text-xs text-foreground/90">
                {current.release_notes}
              </pre>
            </div>
          )}
        </div>
      )}

      {current.state === 'no_public_release' && (
        <p className="text-xs text-muted-foreground">
          Noch keine öffentliche Stable-Version auf GitHub verfügbar. Neue Releases werden automatisch hier angezeigt.
        </p>
      )}

      {current.state === 'unavailable' && (
        <p className="text-xs text-muted-foreground">
          Updateprüfung momentan nicht verfügbar. Das Backend versucht es bei Bedarf automatisch erneut.
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
