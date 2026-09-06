import { useEffect, useState } from 'react'
import {
  CheckCircle2Icon,
  ClockIcon,
  GaugeIcon,
  HardDriveIcon,
  InfoIcon,
  LayersIcon,
  RadioIcon,
  SaveIcon,
  ServerCogIcon,
  UsersIcon,
  XCircleIcon,
  ZapIcon,
} from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Panel, PanelHeader } from '@/components/ui/panel'
import {
  ErrorState,
  ListSkeleton,
  LoadingRegion,
} from '@/components/ui/state-view'
import { useAsync } from '@/hooks/useAsync'
import {
  getHealth,
  getSettings,
  listProviders,
  updateSettings,
} from '@/lib/api/settings'
import { getStorageStatus } from '@/lib/api/storage'
import { StoragePanel } from '@/components/storage/StoragePanel'
import { getUpdateStatus } from '@/lib/api/system'
import { UpdatePanel } from '@/components/system/UpdatePanel'
import { errorMessage, isAbortError } from '@/lib/api/client'
import { formatNumber } from '@/lib/utils/format'
import { cn } from '@/lib/utils'
import type {
  Health,
  JobPriority,
  ProviderInfo,
  ReleaseFilter,
  Settings as SettingsModel,
} from '@/types/api'

interface SettingsTab {
  id: 'general' | 'downloads' | 'storage' | 'providers'
  label: string
  hash: string
  icon: React.ComponentType<{ className?: string }>
}

const SETTINGS_TABS: SettingsTab[] = [
  {
    id: 'general',
    label: 'Allgemein',
    hash: '#health',
    icon: ServerCogIcon,
  },
  {
    id: 'downloads',
    label: 'Downloads',
    hash: '#downloads',
    icon: ZapIcon,
  },
  {
    id: 'storage',
    label: 'Speicher',
    hash: '#storage',
    icon: HardDriveIcon,
  },
  {
    id: 'providers',
    label: 'Provider',
    hash: '#providers',
    icon: RadioIcon,
  },
]

function getTabFromHash(hash: string): SettingsTab['id'] {
  switch (hash) {
    case '#downloads':
      return 'downloads'
    case '#storage':
      return 'storage'
    case '#providers':
      return 'providers'
    case '#health':
    case '#updates':
    case '#startup':
    case '#general':
    default:
      return 'general'
  }
}

function useHash(): string {
  const [hash, setHash] = useState(() =>
    typeof window !== 'undefined' ? window.location.hash : '',
  )

  useEffect(() => {
    const handleHashChange = () => {
      setHash(window.location.hash)
    }

    window.addEventListener('hashchange', handleHashChange)
    window.addEventListener('popstate', handleHashChange)

    return () => {
      window.removeEventListener('hashchange', handleHashChange)
      window.removeEventListener('popstate', handleHashChange)
    }
  }, [])

  return hash
}

function Settings() {
  const currentHash = useHash()
  const activeTab = getTabFromHash(currentHash)

  const health = useAsync((signal) => getHealth({ signal }), [])
  const updateStatus = useAsync((signal) => getUpdateStatus(signal), [])
  const storageStatus = useAsync((signal) => getStorageStatus(signal), [])
  const providers = useAsync((signal) => listProviders(signal), [])
  const settings = useAsync((signal) => getSettings(signal), [])

  const handleTabClick = (tabHash: string) => {
    if (typeof window !== 'undefined') {
      window.location.assign(tabHash)
      window.dispatchEvent(new Event('hashchange'))
    }
  }

  useEffect(() => {
    if (!currentHash) return
    const id = currentHash.replace(/^#/, '')
    if (!id) return
    const el = document.getElementById(id)
    if (el) {
      el.scrollIntoView({ behavior: 'smooth' })
    }
  }, [currentHash, activeTab])

  return (
    <div className="space-y-8">
      <header className="space-y-1.5">
        <h1 className="font-heading text-2xl font-semibold tracking-tight text-foreground sm:text-3xl">
          Servereinstellungen
        </h1>
        <p className="text-sm text-muted-foreground">
          Konfiguration, Diagnose und Dienste verwalten.
        </p>
      </header>

      {/* Local Tabs Navigation */}
      <div className="flex items-center gap-2 border-b border-border pb-3 overflow-x-auto scrollbar-none">
        {SETTINGS_TABS.map((tab) => {
          const isActive = activeTab === tab.id
          const Icon = tab.icon
          return (
            <Button
              key={tab.id}
              variant={isActive ? 'default' : 'ghost'}
              size="sm"
              className={cn('text-sm font-medium gap-2', !isActive && 'text-muted-foreground')}
              onClick={() => handleTabClick(tab.hash)}
            >
              <Icon className="size-4" />
              {tab.label}
            </Button>
          )
        })}
      </div>

      {activeTab === 'general' && (
        <div className="space-y-8">
          <section id="health" aria-labelledby="health-heading" className="space-y-3 scroll-mt-24">
            <PanelHeader
              title={<span id="health-heading">Systemdiagnose</span>}
              description="Backend, Datenbank und die externen Werkzeuge."
            />

            {health.state.status === 'loading' && (
              <LoadingRegion label="Systemstatus wird geladen">
                <ListSkeleton rows={2} />
              </LoadingRegion>
            )}
            {health.state.status === 'error' && (
              <Panel>
                <ErrorState error={health.state.error} onRetry={health.reload} />
              </Panel>
            )}
            {health.state.status === 'success' && (
              <HealthPanel health={health.state.data} onReload={health.reload} />
            )}
          </section>

          <section id="updates" aria-labelledby="updates-heading" className="space-y-3 scroll-mt-24">
            <PanelHeader
              title={<span id="updates-heading">System & Updates</span>}
              description="Versionsprüfung und offizielle GitHub-Releases."
            />

            {updateStatus.state.status === 'loading' && (
              <LoadingRegion label="Update-Status wird geladen">
                <ListSkeleton rows={2} />
              </LoadingRegion>
            )}
            {updateStatus.state.status === 'error' && (
              <Panel>
                <ErrorState error={updateStatus.state.error} onRetry={updateStatus.reload} />
              </Panel>
            )}
            {updateStatus.state.status === 'success' && (
              <UpdatePanel
                initialData={updateStatus.state.data}
                onReload={updateStatus.reload}
              />
            )}
          </section>

          {settings.state.status === 'success' && (
            <section id="startup" aria-labelledby="startup-heading" className="space-y-3 scroll-mt-24">
              <StartupSettings settings={settings.state.data} />
            </section>
          )}
        </div>
      )}

      {activeTab === 'storage' && (
        <section id="storage" aria-labelledby="storage-heading" className="space-y-3 scroll-mt-24">
          <PanelHeader
            title={<span id="storage-heading">Speicher & Netzwerk-Storage</span>}
            description="Storage Identity Guard, Mount-Status, Staging und Warteschlangensteuerung."
          />

          {storageStatus.state.status === 'loading' && (
            <LoadingRegion label="Speicherstatus wird geladen">
              <ListSkeleton rows={2} />
            </LoadingRegion>
          )}
          {storageStatus.state.status === 'error' && (
            <Panel>
              <ErrorState error={storageStatus.state.error} onRetry={storageStatus.reload} />
            </Panel>
          )}
          {storageStatus.state.status === 'success' && (
            <StoragePanel
              initialData={storageStatus.state.data}
              onReload={storageStatus.reload}
            />
          )}
        </section>
      )}

      {activeTab === 'downloads' && (
        <section id="downloads" aria-labelledby="downloads-automation-heading" className="space-y-3 scroll-mt-24">
          <PanelHeader
            title={<span id="downloads-automation-heading">Download-Verhalten & Automation</span>}
            description="Worker-Pool, Bandbreitenbegrenzung, Download-Zeitfenster und Abo-Vorgaben."
          />

          {settings.state.status === 'loading' && (
            <LoadingRegion label="Einstellungen werden geladen">
              <ListSkeleton rows={4} />
            </LoadingRegion>
          )}
          {settings.state.status === 'error' && (
            <Panel>
              <ErrorState error={settings.state.error} onRetry={settings.reload} />
            </Panel>
          )}
          {settings.state.status === 'success' && (
            <SettingsForm
              initialSettings={settings.state.data}
              onSaved={settings.reload}
            />
          )}
        </section>
      )}

      {activeTab === 'providers' && (
        <section id="providers" aria-labelledby="providers-heading" className="space-y-3 scroll-mt-24">
          <PanelHeader
            title={<span id="providers-heading">Provider</span>}
            description="Woher Metadaten kommen und woher die Audioquellen."
          />

          {providers.state.status === 'loading' && (
            <LoadingRegion label="Provider werden geladen">
              <ListSkeleton rows={2} />
            </LoadingRegion>
          )}
          {providers.state.status === 'error' && (
            <Panel>
              <ErrorState error={providers.state.error} onRetry={providers.reload} />
            </Panel>
          )}
          {providers.state.status === 'success' && (
            <ProvidersPanel
              providers={providers.state.data}
              configuredMetadataProvider={
                settings.state.status === 'success'
                  ? settings.state.data.default_metadata_provider
                  : undefined
              }
            />
          )}
        </section>
      )}
    </div>
  )
}

function SettingsForm({
  initialSettings,
  onSaved,
}: {
  initialSettings: SettingsModel
  onSaved: () => void
}) {
  const [skipExisting, setSkipExisting] = useState(initialSettings.skip_existing)
  const [embedCover, setEmbedCover] = useState(initialSettings.embed_cover)
  const [writeCoverFile, setWriteCoverFile] = useState(initialSettings.write_cover_file)
  const [lyricsEnabled, setLyricsEnabled] = useState(initialSettings.lyrics_enabled)
  const [lyricsWriteSidecar, setLyricsWriteSidecar] = useState(initialSettings.lyrics_write_sidecar)
  const [lyricsGeniusEnabled, setLyricsGeniusEnabled] = useState(initialSettings.lyrics_genius_enabled || false)

  // Automation & Queue hot-reload settings
  const [maxWorkers, setMaxWorkers] = useState<number>(initialSettings.max_workers || 2)
  const [rateLimit, setRateLimit] = useState<string>(initialSettings.rate_limit || '')
  const [scheduleEnabled, setScheduleEnabled] = useState<boolean>(
    initialSettings.schedule_enabled || false,
  )
  const [scheduleStart, setScheduleStart] = useState<string>(
    initialSettings.schedule_start || '23:00',
  )
  const [scheduleEnd, setScheduleEnd] = useState<string>(
    initialSettings.schedule_end || '06:00',
  )
  const [scheduleTimezone, setScheduleTimezone] = useState<string>(
    initialSettings.schedule_timezone || '',
  )

  // Subscription defaults
  const [subAutoDownload, setSubAutoDownload] = useState<boolean>(
    initialSettings.subscription_default_auto_download || false,
  )
  const [subPriority, setSubPriority] = useState<JobPriority>(
    initialSettings.subscription_default_priority || 'low',
  )
  const [subFilter, setSubFilter] = useState<ReleaseFilter>(
    initialSettings.subscription_default_release_filter || {
      albums: true,
      singles: true,
      eps: true,
      live: true,
      compilations: true,
      remixes: true,
    },
  )

  const [saving, setSaving] = useState(false)
  const [saved, setSaved] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const isDirty =
    skipExisting !== initialSettings.skip_existing ||
    embedCover !== initialSettings.embed_cover ||
    writeCoverFile !== initialSettings.write_cover_file ||
    lyricsEnabled !== initialSettings.lyrics_enabled ||
    lyricsWriteSidecar !== initialSettings.lyrics_write_sidecar ||
    lyricsGeniusEnabled !== (initialSettings.lyrics_genius_enabled || false) ||
    maxWorkers !== (initialSettings.max_workers || 2) ||
    rateLimit !== (initialSettings.rate_limit || '') ||
    scheduleEnabled !== (initialSettings.schedule_enabled || false) ||
    scheduleStart !== (initialSettings.schedule_start || '23:00') ||
    scheduleEnd !== (initialSettings.schedule_end || '06:00') ||
    scheduleTimezone !== (initialSettings.schedule_timezone || '') ||
    subAutoDownload !== (initialSettings.subscription_default_auto_download || false) ||
    subPriority !== (initialSettings.subscription_default_priority || 'low') ||
    JSON.stringify(subFilter) !==
      JSON.stringify(
        initialSettings.subscription_default_release_filter || {
          albums: true,
          singles: true,
          eps: true,
          live: true,
          compilations: true,
          remixes: true,
        },
      )

  async function handleSave() {
    if (!isDirty || saving) return
    setSaving(true)
    setError(null)
    setSaved(false)

    try {
      await updateSettings({
        ...(skipExisting !== initialSettings.skip_existing && { skip_existing: skipExisting }),
        ...(embedCover !== initialSettings.embed_cover && { embed_cover: embedCover }),
        ...(writeCoverFile !== initialSettings.write_cover_file && {
          write_cover_file: writeCoverFile,
        }),
        ...(lyricsEnabled !== initialSettings.lyrics_enabled && {
          lyrics_enabled: lyricsEnabled,
        }),
        ...(lyricsWriteSidecar !== initialSettings.lyrics_write_sidecar && {
          lyrics_write_sidecar: lyricsWriteSidecar,
        }),
        ...(lyricsGeniusEnabled !== (initialSettings.lyrics_genius_enabled || false) && {
          lyrics_genius_enabled: lyricsGeniusEnabled,
        }),
        ...(maxWorkers !== initialSettings.max_workers && { max_workers: maxWorkers }),
        ...(rateLimit !== (initialSettings.rate_limit || '') && { rate_limit: rateLimit }),
        ...(scheduleEnabled !== initialSettings.schedule_enabled && {
          schedule_enabled: scheduleEnabled,
        }),
        ...(scheduleStart !== (initialSettings.schedule_start || '23:00') && {
          schedule_start: scheduleStart,
        }),
        ...(scheduleEnd !== (initialSettings.schedule_end || '06:00') && {
          schedule_end: scheduleEnd,
        }),
        ...(scheduleTimezone !== (initialSettings.schedule_timezone || '') && {
          schedule_timezone: scheduleTimezone,
        }),
        ...(subAutoDownload !== initialSettings.subscription_default_auto_download && {
          subscription_default_auto_download: subAutoDownload,
        }),
        ...(subPriority !== initialSettings.subscription_default_priority && {
          subscription_default_priority: subPriority,
        }),
        ...(JSON.stringify(subFilter) !==
          JSON.stringify(initialSettings.subscription_default_release_filter) && {
          subscription_default_release_filter: subFilter,
        }),
      })
      setSaved(true)
      onSaved()
      window.setTimeout(() => setSaved(false), 3000)
    } catch (caught) {
      if (!isAbortError(caught)) setError(errorMessage(caught))
    } finally {
      setSaving(false)
    }
  }

  return (
    <Panel className="space-y-6 p-4 sm:p-6">
      {/* 1. Worker Pool & Bandwidth */}
      <div className="space-y-4">
        <h3 className="text-sm font-semibold text-foreground flex items-center gap-2">
          <GaugeIcon className="size-4 text-primary" />
          Worker & Bandbreitenbegrenzung
        </h3>
        <div className="grid gap-4 sm:grid-cols-2">
          <div className="space-y-1.5">
            <label htmlFor="max-workers" className="text-xs font-medium text-foreground">
              Parallele Download-Worker (1–4)
            </label>
            <select
              id="max-workers"
              value={maxWorkers}
              onChange={(e) => setMaxWorkers(Number(e.target.value))}
              className="w-full rounded-lg border border-border bg-background px-3 py-2 text-sm text-foreground focus:outline-none focus:ring-1 focus:ring-primary"
            >
              <option value={1}>1 Worker (schonend)</option>
              <option value={2}>2 Worker (Standard)</option>
              <option value={3}>3 Worker (schneller)</option>
              <option value={4}>4 Worker (maximal)</option>
            </select>
            <p className="text-[0.6875rem] text-muted-foreground">
              Gilt sofort für neu gestartete Downloads ohne Server-Neustart.
            </p>
          </div>

          <div className="space-y-1.5">
            <label htmlFor="rate-limit" className="text-xs font-medium text-foreground">
              Download-Bandbreitenlimit
            </label>
            <div className="flex gap-2">
              <select
                id="rate-limit-preset"
                value={
                  rateLimit === '' || ['1M', '2M', '5M', '10M', '25M'].includes(rateLimit)
                    ? rateLimit
                    : 'custom'
                }
                onChange={(e) => {
                  if (e.target.value !== 'custom') {
                    setRateLimit(e.target.value)
                  }
                }}
                className="rounded-lg border border-border bg-background px-3 py-2 text-sm text-foreground focus:outline-none focus:ring-1 focus:ring-primary"
              >
                <option value="">Unbegrenzt</option>
                <option value="1M">1 MB/s</option>
                <option value="2M">2 MB/s</option>
                <option value="5M">5 MB/s</option>
                <option value="10M">10 MB/s</option>
                <option value="25M">25 MB/s</option>
                <option value="custom">Benutzerdefiniert</option>
              </select>
              <Input
                id="rate-limit"
                value={rateLimit}
                onChange={(e) => setRateLimit(e.target.value.trim())}
                placeholder="z.B. 5M, 500K"
                className="flex-1 text-sm font-mono"
              />
            </div>
            <p className="text-[0.6875rem] text-muted-foreground">
              Wird via yt-dlp <code>--limit-rate</code> pro Download-Prozess angewendet.
            </p>
          </div>
        </div>
      </div>

      {/* 2. Download Schedule Window */}
      <div className="space-y-4 border-t border-border/40 pt-4">
        <h3 className="text-sm font-semibold text-foreground flex items-center gap-2">
          <ClockIcon className="size-4 text-primary" />
          Download-Zeitfenster
        </h3>
        <Switch
          checked={scheduleEnabled}
          onChange={setScheduleEnabled}
          label="Downloads nur im Zeitfenster ausführen"
          description="Startet neue Audio-Downloads nur im definierten Zeitraum (z.B. nachts). Staging-Finalisierungen in die Bibliothek werden auch außerhalb abgeschlossen."
        />

        {scheduleEnabled && (
          <div className="grid gap-4 sm:grid-cols-3 pl-7">
            <div className="space-y-1.5">
              <label htmlFor="sched-start" className="text-xs font-medium text-foreground">
                Startzeit (inklusive)
              </label>
              <Input
                id="sched-start"
                type="time"
                value={scheduleStart}
                onChange={(e) => setScheduleStart(e.target.value)}
                className="text-sm"
              />
            </div>
            <div className="space-y-1.5">
              <label htmlFor="sched-end" className="text-xs font-medium text-foreground">
                Endzeit (exklusive)
              </label>
              <Input
                id="sched-end"
                type="time"
                value={scheduleEnd}
                onChange={(e) => setScheduleEnd(e.target.value)}
                className="text-sm"
              />
            </div>
            <div className="space-y-1.5">
              <label htmlFor="sched-tz" className="text-xs font-medium text-foreground">
                Zeitzone (optional)
              </label>
              <Input
                id="sched-tz"
                value={scheduleTimezone}
                onChange={(e) => setScheduleTimezone(e.target.value)}
                placeholder={initialSettings.server_timezone || 'Server-Zeitzone'}
                className="text-sm font-mono"
              />
            </div>
          </div>
        )}
      </div>

      {/* 3. Subscription Defaults */}
      <div className="space-y-4 border-t border-border/40 pt-4">
        <h3 className="text-sm font-semibold text-foreground flex items-center gap-2">
          <UsersIcon className="size-4 text-primary" />
          Standardwerte für neue Abonnements
        </h3>
        <div className="space-y-3 pl-1">
          <Switch
            checked={subAutoDownload}
            onChange={setSubAutoDownload}
            label="Automatischer Download standardmäßig aktivieren"
            description="Neue Abonnements laden gefundene Neuerscheinungen direkt automatisch herunter."
          />

          <div className="space-y-1.5 sm:w-1/2">
            <label htmlFor="sub-default-priority" className="text-xs font-medium text-foreground">
              Standard-Priorität für Abonnements
            </label>
            <select
              id="sub-default-priority"
              value={subPriority}
              onChange={(e) => setSubPriority(e.target.value as JobPriority)}
              className="w-full rounded-lg border border-border bg-background px-3 py-2 text-sm text-foreground focus:outline-none focus:ring-1 focus:ring-primary"
            >
              <option value="low">Niedrig (empfohlen für Hintergrund-Abos)</option>
              <option value="normal">Normal</option>
              <option value="high">Hoch</option>
              <option value="very_high">Sehr hoch</option>
            </select>
          </div>

          <div className="space-y-2 pt-1">
            <span className="text-xs font-medium text-foreground">
              Standardmäßige Release-Filter:
            </span>
            <div className="grid grid-cols-2 sm:grid-cols-3 gap-2">
              <label className="flex items-center gap-2 text-xs text-muted-foreground cursor-pointer">
                <Checkbox
                  checked={subFilter.albums}
                  onCheckedChange={(c) =>
                    setSubFilter((prev) => ({ ...prev, albums: c === true }))
                  }
                />
                Alben
              </label>
              <label className="flex items-center gap-2 text-xs text-muted-foreground cursor-pointer">
                <Checkbox
                  checked={subFilter.singles}
                  onCheckedChange={(c) =>
                    setSubFilter((prev) => ({ ...prev, singles: c === true }))
                  }
                />
                Singles
              </label>
              <label className="flex items-center gap-2 text-xs text-muted-foreground cursor-pointer">
                <Checkbox
                  checked={subFilter.eps}
                  onCheckedChange={(c) =>
                    setSubFilter((prev) => ({ ...prev, eps: c === true }))
                  }
                />
                EPs
              </label>
              <label className="flex items-center gap-2 text-xs text-muted-foreground cursor-pointer">
                <Checkbox
                  checked={subFilter.live}
                  onCheckedChange={(c) =>
                    setSubFilter((prev) => ({ ...prev, live: c === true }))
                  }
                />
                Live
              </label>
              <label className="flex items-center gap-2 text-xs text-muted-foreground cursor-pointer">
                <Checkbox
                  checked={subFilter.compilations}
                  onCheckedChange={(c) =>
                    setSubFilter((prev) => ({ ...prev, compilations: c === true }))
                  }
                />
                Compilations
              </label>
              <label className="flex items-center gap-2 text-xs text-muted-foreground cursor-pointer">
                <Checkbox
                  checked={subFilter.remixes}
                  onCheckedChange={(c) =>
                    setSubFilter((prev) => ({ ...prev, remixes: c === true }))
                  }
                />
                Remixe
              </label>
            </div>
          </div>
        </div>
      </div>

      {/* 4. General Tagging & Metadata */}
      <div className="space-y-3 border-t border-border/40 pt-4">
        <h3 className="text-sm font-semibold text-foreground flex items-center gap-2">
          <LayersIcon className="size-4 text-primary" />
          Metadaten & Dateiverwaltung
        </h3>
        <div className="space-y-1">
          <Switch
            checked={skipExisting}
            onChange={setSkipExisting}
            label="Bereits vorhandene Tracks überspringen"
            description="Tracks, die schon in der Bibliothek liegen, werden nicht erneut geladen."
          />
          <Switch
            checked={embedCover}
            onChange={setEmbedCover}
            label="Cover in Audiodatei einbetten"
            description="Bettet das Album-Cover in die Metadaten der heruntergeladenen Audiodatei ein."
          />
          <Switch
            checked={writeCoverFile}
            onChange={setWriteCoverFile}
            label="cover.jpg im Albumverzeichnis speichern"
            description="Legt eine separate Bilddatei an, wie sie Medienserver wie Jellyfin oder Plex erwarten."
          />
          <Switch
            checked={lyricsEnabled}
            onChange={setLyricsEnabled}
            label="Lyrics automatisch suchen"
            description="Sucht nach synchronisierten (LRC) und Plain-Text Lyrics bei LRCLIB und YouTube Music."
          />
          <Switch
            checked={lyricsWriteSidecar}
            onChange={setLyricsWriteSidecar}
            label="Lyrics als Sidecar-Datei speichern (.lrc / .txt)"
            description="Legt synchronisierte Songtexte (.lrc) oder ungesyncte Texte (.txt) neben die Audiodatei."
          />
          <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-2 rounded-xl px-2 py-3 transition-colors hover:bg-white/4">
            <div className="flex-1">
              <Switch
                checked={lyricsGeniusEnabled}
                onChange={setLyricsGeniusEnabled}
                label="Genius-Fallback"
                description="Optionaler Fallback für unsynchronisierte Liedtexte."
              />
            </div>
            <div className="flex shrink-0 items-center pl-9 sm:pl-0 sm:pr-2">
              <Badge variant={initialSettings.genius_token_configured ? 'outline' : 'neutral'} className="text-[10px] tracking-wider">
                Token: {initialSettings.genius_token_configured ? 'Konfiguriert' : 'Nicht konfiguriert'}
              </Badge>
            </div>
          </div>
        </div>
      </div>

      {error && (
        <p
          role="alert"
          className="rounded-xl border border-destructive/20 bg-destructive/8 px-3.5 py-2.5 text-xs leading-relaxed text-destructive"
        >
          {error}
        </p>
      )}

      <div className="flex items-center justify-end gap-3 border-t border-border pt-4">
        {saved && <span className="text-xs text-success">Gespeichert.</span>}
        <Button variant="default" onClick={handleSave} disabled={!isDirty || saving}>
          <SaveIcon />
          {saving ? 'Speichert …' : 'Speichern'}
        </Button>
      </div>
    </Panel>
  )
}

function Switch({
  checked,
  onChange,
  label,
  description,
}: {
  checked: boolean
  onChange: (value: boolean) => void
  label: string
  description: string
}) {
  return (
    <label className="flex cursor-pointer items-start gap-3 rounded-xl px-2 py-3 transition-colors hover:bg-white/4">
      <Checkbox
        checked={checked}
        onCheckedChange={(value) => onChange(value === true)}
        className="mt-0.5"
      />
      <span className="space-y-1">
        <span className="block text-sm font-medium text-foreground">{label}</span>
        <span className="block text-xs leading-relaxed text-muted-foreground">
          {description}
        </span>
      </span>
    </label>
  )
}

function HealthPanel({ health, onReload }: { health: Health; onReload: () => void }) {
  const entries = Object.entries(health.checks).sort(([a], [b]) =>
    a.localeCompare(b),
  )
  const allOk = health.status === 'ok'

  return (
    <Panel className="space-y-4 p-4 sm:p-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex flex-wrap items-center gap-3">
          <Badge variant={allOk ? 'success' : health.status === 'degraded' ? 'warning' : 'destructive'}>
            {allOk
              ? 'Betriebsbereit'
              : health.status === 'degraded'
                ? 'Eingeschränkt'
                : 'Nicht betriebsbereit'}
          </Badge>
          <span className="text-xs text-muted-foreground">
            Version {health.version} · seit{' '}
            {formatNumber(Math.round(health.uptime_seconds / 60))} Min.
          </span>
        </div>

        <Button variant="ghost" size="sm" onClick={onReload}>
          Erneut prüfen
        </Button>
      </div>

      <div className="grid gap-2.5 sm:grid-cols-2 lg:grid-cols-3">
        {entries.map(([name, check]) => (
          <div
            key={name}
            className="flex items-center justify-between gap-3 rounded-xl border border-border bg-white/3 px-3.5 py-2.5 text-xs"
          >
            <span className="truncate font-medium text-foreground">{name}</span>
            <span
              className={cn(
                'flex shrink-0 items-center gap-1.5',
                check.ok ? 'text-success' : 'text-destructive',
              )}
            >
              {check.ok ? (
                <CheckCircle2Icon className="size-4" />
              ) : (
                <XCircleIcon className="size-4" />
              )}
              {check.ok ? 'OK' : 'Fehler'}
            </span>
          </div>
        ))}
      </div>

      {entries
        .filter(([, check]) => !check.ok && check.detail)
        .map(([name, check]) => (
          <p key={name} className="text-xs leading-relaxed text-warning">
            {name}: {check.detail}
          </p>
        ))}
    </Panel>
  )
}

function ProvidersPanel({
  providers,
  configuredMetadataProvider,
}: {
  providers: ProviderInfo[]
  configuredMetadataProvider?: string
}) {
  const metadata = providers.filter((p) => p.kind === 'metadata')
  const media = providers.filter((p) => p.kind === 'media')

  const configuredButMissing =
    configuredMetadataProvider &&
    !metadata.some(
      (p) => p.name === configuredMetadataProvider && p.available,
    )

  return (
    <div className="space-y-4">
      <Panel className="space-y-4 p-4 sm:p-5">
        <h3 className="text-sm font-semibold text-foreground">Metadaten</h3>
        <ul className="grid gap-3 sm:grid-cols-2">
          {metadata.map((p) => (
            <li
              key={p.name}
              className="flex items-start justify-between gap-3 rounded-xl border border-border bg-white/3 p-3 text-xs"
            >
              <div className="space-y-1">
                <span className="font-medium text-foreground">{p.name}</span>
                {p.detail && (
                  <p className="text-muted-foreground">{p.detail}</p>
                )}
              </div>
              <div className="flex shrink-0 items-center gap-1.5">
                {p.default && <Badge variant="outline">Standard</Badge>}
                <Badge variant={p.available ? 'success' : 'destructive'}>
                  {p.available ? 'Aktiv' : 'Inaktiv'}
                </Badge>
              </div>
            </li>
          ))}
        </ul>
      </Panel>

      <Panel className="space-y-4 p-4 sm:p-5">
        <h3 className="text-sm font-semibold text-foreground">Audioquellen</h3>
        <ul className="grid gap-3 sm:grid-cols-2">
          {media.map((p) => (
            <li
              key={p.name}
              className="flex items-start justify-between gap-3 rounded-xl border border-border bg-white/3 p-3 text-xs"
            >
              <div className="space-y-1">
                <span className="font-medium text-foreground">{p.name}</span>
                {p.detail && (
                  <p className="text-muted-foreground">{p.detail}</p>
                )}
              </div>
              <div className="flex shrink-0 items-center gap-1.5">
                {p.default && <Badge variant="outline">Standard</Badge>}
                <Badge variant={p.available ? 'success' : 'destructive'}>
                  {p.available ? 'Aktiv' : 'Inaktiv'}
                </Badge>
              </div>

            </li>
          ))}
        </ul>
      </Panel>

      {configuredButMissing && (
        <Note>
          Als Standard-Metadatenprovider ist{' '}
          <strong className="text-foreground">{configuredMetadataProvider}</strong>{' '}
          konfiguriert, aber nicht verfügbar.
        </Note>
      )}
    </div>
  )
}

function StartupSettings({ settings }: { settings: SettingsModel }) {
  const rows: { label: string; value: string; note?: string; advanced?: boolean }[] = [
    { label: 'Bibliothekspfad', value: settings.library_path },
    {
      label: 'Matching-Schwelle',
      value: settings.match_min_score.toLocaleString('de-DE'),
      note: `Toleranz der Spieldauer: ${formatNumber(settings.match_duration_tolerance_ms)} ms`,
    },
    {
      label: 'Transcoding erlauben',
      value: settings.allow_transcode ? 'Ja' : 'Nein',
      advanced: true,
      note:
        'Erlaubt eine Neukodierung nur dann, wenn kein geeignetes natives Audioformat verfügbar ist. Bevorzugt wird immer der native Opus-Stream.',
    },
  ]

  return (
    <Panel className="space-y-4 p-4 sm:p-6">
      <PanelHeader
        title="Beim Start festgelegt"
        description="Diese Werte stammen aus der Serverkonfiguration und lassen sich nur dort ändern."
      />

      <dl className="divide-y divide-border">
        {rows.map((row) => (
          <div key={row.label} className="space-y-1 py-3 first:pt-0 last:pb-0">
            <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1">
              <dt className="flex items-center gap-2 text-sm text-foreground">
                {row.label}
                {row.advanced && <Badge variant="outline">Erweitert</Badge>}
              </dt>
              <dd className="font-mono text-xs text-muted-foreground">{row.value}</dd>
            </div>
            {row.note && (
              <p className="text-xs leading-relaxed text-muted-foreground">{row.note}</p>
            )}
          </div>
        ))}
      </dl>
    </Panel>
  )
}

function Note({ children }: { children: React.ReactNode }) {
  return (
    <p className="flex items-start gap-2.5 rounded-xl border border-border bg-white/3 px-3.5 py-3 text-xs leading-relaxed text-muted-foreground">
      <InfoIcon className="mt-0.5 size-3.5 shrink-0" />
      <span>{children}</span>
    </p>
  )
}

export { Settings }
