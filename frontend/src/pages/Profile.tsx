import { useEffect, useState, type FormEvent } from 'react'
import {
  CheckCircle2Icon,
  KeyIcon,
  LaptopIcon,
  LogOutIcon,
  SaveIcon,
  Trash2Icon,
} from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Panel, PanelHeader } from '@/components/ui/panel'
import { useAuth } from '@/hooks/useAuth'
import {
  changePassword,
  listSessions,
  revokeOtherSessions,
  revokeSession,
  updateProfile,
} from '@/lib/api/profile'
import { errorMessage } from '@/lib/api/client'
import type { SessionSummary } from '@/types/api'

export function Profile() {
  const { user, setUser, logout } = useAuth()

  // Profile display name
  const [displayName, setDisplayName] = useState(user?.display_name || '')
  const [savingProfile, setSavingProfile] = useState(false)
  const [profileSuccess, setProfileSuccess] = useState<string | null>(null)
  const [profileError, setProfileError] = useState<string | null>(null)

  // Password change
  const [currentPassword, setCurrentPassword] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [savingPassword, setSavingPassword] = useState(false)
  const [passwordSuccess, setPasswordSuccess] = useState<string | null>(null)
  const [passwordError, setPasswordError] = useState<string | null>(null)

  // Sessions
  const [sessions, setSessions] = useState<SessionSummary[]>([])
  const [loadingSessions, setLoadingSessions] = useState(true)
  const [sessionActionLoading, setSessionActionLoading] = useState(false)
  const [sessionError, setSessionError] = useState<string | null>(null)
  const [sessionSuccess, setSessionSuccess] = useState<string | null>(null)

  const loadSessions = async () => {
    try {
      setLoadingSessions(true)
      const list = await listSessions()
      setSessions(list)
    } catch (err) {
      setSessionError(errorMessage(err))
    } finally {
      setLoadingSessions(false)
    }
  }

  useEffect(() => {
    let active = true
    listSessions()
      .then((list) => {
        if (active) setSessions(list)
      })
      .catch((err) => {
        if (active) setSessionError(errorMessage(err))
      })
      .finally(() => {
        if (active) setLoadingSessions(false)
      })

    return () => {
      active = false
    }
  }, [])

  const handleUpdateProfile = async (e: FormEvent) => {
    e.preventDefault()
    setProfileError(null)
    setProfileSuccess(null)

    try {
      setSavingProfile(true)
      const updated = await updateProfile({ display_name: displayName.trim() })
      setUser(updated)
      setProfileSuccess('Anzeigename erfolgreich gespeichert.')
    } catch (err) {
      setProfileError(errorMessage(err))
    } finally {
      setSavingProfile(false)
    }
  }

  const handleChangePassword = async (e: FormEvent) => {
    e.preventDefault()
    setPasswordError(null)
    setPasswordSuccess(null)

    if (!currentPassword) {
      setPasswordError('Bitte gib dein aktuelles Passwort ein.')
      return
    }
    if (newPassword.length < 8) {
      setPasswordError('Das neue Passwort muss mindestens 8 Zeichen lang sein.')
      return
    }
    if (newPassword !== confirmPassword) {
      setPasswordError('Die Passwörter stimmen nicht überein.')
      return
    }

    try {
      setSavingPassword(true)
      await changePassword({
        current_password: currentPassword,
        new_password: newPassword,
      })
      setPasswordSuccess('Passwort geändert. Alle anderen Sitzungen wurden abgemeldet.')
      setCurrentPassword('')
      setNewPassword('')
      setConfirmPassword('')
      void loadSessions()
    } catch (err) {
      setPasswordError(errorMessage(err))
    } finally {
      setSavingPassword(false)
    }
  }

  const handleRevokeSession = async (id: string, isCurrent: boolean) => {
    try {
      setSessionActionLoading(true)
      setSessionError(null)
      await revokeSession(id)
      if (isCurrent) {
        await logout()
      } else {
        setSessionSuccess('Sitzung erfolgreich abgemeldet.')
        await loadSessions()
      }
    } catch (err) {
      setSessionError(errorMessage(err))
    } finally {
      setSessionActionLoading(false)
    }
  }

  const handleRevokeOthers = async () => {
    try {
      setSessionActionLoading(true)
      setSessionError(null)
      await revokeOtherSessions()
      setSessionSuccess('Alle anderen Sitzungen wurden erfolgreich abgemeldet.')
      await loadSessions()
    } catch (err) {
      setSessionError(errorMessage(err))
    } finally {
      setSessionActionLoading(false)
    }
  }

  return (
    <div className="space-y-10 max-w-4xl">
      <header className="space-y-1.5">
        <h1 className="font-heading text-2xl font-semibold tracking-tight text-foreground sm:text-3xl">
          Profil & Sicherheit
        </h1>
        <p className="text-sm text-muted-foreground">
          Verwalte deine Kontoeinstellungen, Passwort und aktive Sitzungen.
        </p>
      </header>

      {/* Profile Details */}
      <section aria-labelledby="profile-heading" className="space-y-3">
        <PanelHeader
          title={<span id="profile-heading">Kontoinformationen</span>}
          description="Dein Benutzerkonto und Anzeigename."
        />

        <Panel className="p-6">
          <form onSubmit={handleUpdateProfile} className="space-y-4 max-w-md">
            {profileError && (
              <div
                role="alert"
                className="rounded-xl border border-destructive/30 bg-destructive/10 p-3 text-sm text-destructive"
              >
                {profileError}
              </div>
            )}
            {profileSuccess && (
              <div
                role="status"
                className="flex items-center gap-2 rounded-xl border border-emerald-500/30 bg-emerald-500/10 p-3 text-sm text-emerald-400"
              >
                <CheckCircle2Icon className="h-4 w-4 shrink-0" />
                {profileSuccess}
              </div>
            )}

            <div className="space-y-1.5">
              <Label>Benutzername</Label>
              <div className="flex items-center gap-3">
                <Input value={user?.username || ''} disabled className="opacity-75" />
                <Badge variant={user?.role === 'admin' ? 'default' : 'neutral'} className="capitalize">
                  {user?.role === 'admin' ? 'Administrator' : 'Benutzer'}
                </Badge>
              </div>
            </div>

            <div className="space-y-1.5">
              <Label htmlFor="display-name">Anzeigename</Label>
              <Input
                id="display-name"
                value={displayName}
                onChange={(e) => setDisplayName(e.target.value)}
                placeholder="Anzeigename"
                disabled={savingProfile}
              />
            </div>

            <Button type="submit" disabled={savingProfile} className="gap-2">
              <SaveIcon className="h-4 w-4" />
              {savingProfile ? 'Wird gespeichert...' : 'Anzeigename speichern'}
            </Button>
          </form>
        </Panel>
      </section>

      {/* Password change */}
      <section aria-labelledby="password-heading" className="space-y-3">
        <PanelHeader
          title={<span id="password-heading">Passwort ändern</span>}
          description="Beim Ändern des Passworts werden alle anderen Sitzungen automatisch beendet."
        />

        <Panel className="p-6">
          <form onSubmit={handleChangePassword} className="space-y-4 max-w-md">
            {passwordError && (
              <div
                role="alert"
                className="rounded-xl border border-destructive/30 bg-destructive/10 p-3 text-sm text-destructive"
              >
                {passwordError}
              </div>
            )}
            {passwordSuccess && (
              <div
                role="status"
                className="flex items-center gap-2 rounded-xl border border-emerald-500/30 bg-emerald-500/10 p-3 text-sm text-emerald-400"
              >
                <CheckCircle2Icon className="h-4 w-4 shrink-0" />
                {passwordSuccess}
              </div>
            )}

            <div className="space-y-1.5">
              <Label htmlFor="current-password">Aktuelles Passwort</Label>
              <Input
                id="current-password"
                type="password"
                autoComplete="current-password"
                required
                value={currentPassword}
                onChange={(e) => setCurrentPassword(e.target.value)}
                disabled={savingPassword}
              />
            </div>

            <div className="space-y-1.5">
              <Label htmlFor="new-password">Neues Passwort</Label>
              <Input
                id="new-password"
                type="password"
                autoComplete="new-password"
                required
                value={newPassword}
                onChange={(e) => setNewPassword(e.target.value)}
                placeholder="Mindestens 8 Zeichen"
                disabled={savingPassword}
              />
            </div>

            <div className="space-y-1.5">
              <Label htmlFor="confirm-new-password">Neues Passwort bestätigen</Label>
              <Input
                id="confirm-new-password"
                type="password"
                autoComplete="new-password"
                required
                value={confirmPassword}
                onChange={(e) => setConfirmPassword(e.target.value)}
                placeholder="Neues Passwort wiederholen"
                disabled={savingPassword}
              />
            </div>

            <Button type="submit" disabled={savingPassword} className="gap-2">
              <KeyIcon className="h-4 w-4" />
              {savingPassword ? 'Wird geändert...' : 'Passwort aktualisieren'}
            </Button>
          </form>
        </Panel>
      </section>

      {/* Active Sessions */}
      <section aria-labelledby="sessions-heading" className="space-y-3">
        <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
          <PanelHeader
            title={<span id="sessions-heading">Aktive Sitzungen</span>}
            description="Übersicht aller angemeldeten Geräte und Browser."
          />
          {sessions.length > 1 && (
            <Button
              variant="outline"
              size="sm"
              onClick={handleRevokeOthers}
              disabled={sessionActionLoading}
              className="gap-2 shrink-0 border-destructive/30 text-destructive hover:bg-destructive/10"
            >
              <LogOutIcon className="h-4 w-4" />
              Andere Sitzungen beenden
            </Button>
          )}
        </div>

        <Panel className="p-6">
          {sessionError && (
            <div
              role="alert"
              className="mb-4 rounded-xl border border-destructive/30 bg-destructive/10 p-3 text-sm text-destructive"
            >
              {sessionError}
            </div>
          )}
          {sessionSuccess && (
            <div
              role="status"
              className="mb-4 flex items-center gap-2 rounded-xl border border-emerald-500/30 bg-emerald-500/10 p-3 text-sm text-emerald-400"
            >
              <CheckCircle2Icon className="h-4 w-4 shrink-0" />
              {sessionSuccess}
            </div>
          )}

          {loadingSessions ? (
            <div className="py-4 text-center text-sm text-muted-foreground">
              Sitzungen werden geladen...
            </div>
          ) : sessions.length === 0 ? (
            <div className="py-4 text-center text-sm text-muted-foreground">
              Keine aktiven Sitzungen gefunden.
            </div>
          ) : (
            <div className="divide-y divide-white/6">
              {sessions.map((s) => (
                <div
                  key={s.id}
                  className="flex flex-col gap-3 py-4 first:pt-0 last:pb-0 sm:flex-row sm:items-center sm:justify-between"
                >
                  <div className="space-y-1">
                    <div className="flex items-center gap-2">
                      <LaptopIcon className="h-4 w-4 text-primary" />
                      <span className="font-medium text-foreground text-sm">
                        {s.user_agent || 'Unbekannter Browser / Client'}
                      </span>
                      {s.is_current && (
                        <Badge variant="default" className="text-xs">
                          Diese Sitzung
                        </Badge>
                      )}
                    </div>
                    <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground">
                      <span>IP: {s.ip_address || 'Unbekannt'}</span>
                      <span>
                        Zuletzt aktiv:{' '}
                        {new Date(s.last_seen_at).toLocaleString('de-DE', {
                          dateStyle: 'short',
                          timeStyle: 'short',
                        })}
                      </span>
                      <span>
                        Erstellt:{' '}
                        {new Date(s.created_at).toLocaleString('de-DE', {
                          dateStyle: 'short',
                          timeStyle: 'short',
                        })}
                      </span>
                    </div>
                  </div>

                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => handleRevokeSession(s.id, s.is_current)}
                    disabled={sessionActionLoading}
                    className="self-start sm:self-center gap-1.5 text-muted-foreground hover:text-destructive hover:bg-destructive/10"
                  >
                    <Trash2Icon className="h-3.5 w-3.5" />
                    {s.is_current ? 'Abmelden' : 'Beenden'}
                  </Button>
                </div>
              ))}
            </div>
          )}
        </Panel>
      </section>
    </div>
  )
}
