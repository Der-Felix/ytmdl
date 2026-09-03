import { useEffect, useState, type FormEvent } from 'react'
import {
  CheckCircle2Icon,
  KeyIcon,
  PlusIcon,
  ShieldIcon,
  Trash2Icon,
  UserCheckIcon,
  UserXIcon,
} from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Panel, PanelHeader } from '@/components/ui/panel'
import { useAuth } from '@/hooks/useAuth'
import {
  createUser,
  deleteUser,
  listUsers,
  resetPassword,
  updateUser,
} from '@/lib/api/users'
import { errorMessage } from '@/lib/api/client'
import type { Role, UserSummary } from '@/types/api'

export function Users() {
  const { user: currentUser } = useAuth()

  const [users, setUsers] = useState<UserSummary[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [success, setSuccess] = useState<string | null>(null)

  // Create User dialog state
  const [createOpen, setCreateOpen] = useState(false)
  const [newUsername, setNewUsername] = useState('')
  const [newDisplayName, setNewDisplayName] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [newRole, setNewRole] = useState<Role>('user')
  const [creating, setCreating] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)

  // Reset Password dialog state
  const [resetTargetUser, setResetTargetUser] = useState<UserSummary | null>(null)
  const [resetNewPassword, setResetNewPassword] = useState('')
  const [resetting, setResetting] = useState(false)
  const [resetError, setResetError] = useState<string | null>(null)

  // Delete User dialog state
  const [deleteTargetUser, setDeleteTargetUser] = useState<UserSummary | null>(null)
  const [deleting, setDeleting] = useState(false)
  const [deleteError, setDeleteError] = useState<string | null>(null)

  const loadUsers = async () => {
    try {
      setLoading(true)
      const res = await listUsers({ limit: 100 })
      setUsers(res.items)
    } catch (err) {
      setError(errorMessage(err))
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    let active = true
    listUsers({ limit: 100 })
      .then((res) => {
        if (active) setUsers(res.items)
      })
      .catch((err) => {
        if (active) setError(errorMessage(err))
      })
      .finally(() => {
        if (active) setLoading(false)
      })

    return () => {
      active = false
    }
  }, [])

  const handleCreateUser = async (e: FormEvent) => {
    e.preventDefault()
    setCreateError(null)

    if (!newUsername.trim()) {
      setCreateError('Benutzername ist erforderlich.')
      return
    }
    if (newPassword.length < 8) {
      setCreateError('Passwort muss mindestens 8 Zeichen lang sein.')
      return
    }

    try {
      setCreating(true)
      await createUser({
        username: newUsername.trim(),
        display_name: newDisplayName.trim() || undefined,
        password: newPassword,
        role: newRole,
      })
      setSuccess(`Benutzer "${newUsername.trim()}" erfolgreich erstellt.`)
      setCreateOpen(false)
      setNewUsername('')
      setNewDisplayName('')
      setNewPassword('')
      setNewRole('user')
      await loadUsers()
    } catch (err) {
      setCreateError(errorMessage(err))
    } finally {
      setCreating(false)
    }
  }

  const handleToggleStatus = async (targetUser: UserSummary) => {
    setError(null)
    setSuccess(null)
    try {
      const nextStatus = !targetUser.enabled
      await updateUser(targetUser.id, { enabled: nextStatus })
      setSuccess(
        `Benutzer "${targetUser.username}" wurde ${nextStatus ? 'aktiviert' : 'deaktiviert'}.`,
      )
      await loadUsers()
    } catch (err) {
      setError(errorMessage(err))
    }
  }

  const handleToggleRole = async (targetUser: UserSummary) => {
    setError(null)
    setSuccess(null)
    try {
      const nextRole: Role = targetUser.role === 'admin' ? 'user' : 'admin'
      await updateUser(targetUser.id, { role: nextRole })
      setSuccess(
        `Rolle von "${targetUser.username}" geändert zu ${nextRole === 'admin' ? 'Administrator' : 'Benutzer'}.`,
      )
      await loadUsers()
    } catch (err) {
      setError(errorMessage(err))
    }
  }

  const handleResetPassword = async (e: FormEvent) => {
    e.preventDefault()
    if (!resetTargetUser) return
    setResetError(null)

    if (resetNewPassword.length < 8) {
      setResetError('Das neue Passwort muss mindestens 8 Zeichen lang sein.')
      return
    }

    try {
      setResetting(true)
      await resetPassword(resetTargetUser.id, resetNewPassword)
      setSuccess(`Passwort für "${resetTargetUser.username}" erfolgreich zurückgesetzt.`)
      setResetTargetUser(null)
      setResetNewPassword('')
    } catch (err) {
      setResetError(errorMessage(err))
    } finally {
      setResetting(false)
    }
  }

  const handleDeleteUser = async () => {
    if (!deleteTargetUser) return
    setDeleteError(null)

    try {
      setDeleting(true)
      await deleteUser(deleteTargetUser.id)
      setSuccess(`Benutzer "${deleteTargetUser.username}" erfolgreich gelöscht.`)
      setDeleteTargetUser(null)
      await loadUsers()
    } catch (err) {
      setDeleteError(errorMessage(err))
    } finally {
      setDeleting(false)
    }
  }

  return (
    <div className="w-full space-y-6">
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <header className="space-y-1.5">
          <h1 className="font-heading text-2xl font-semibold tracking-tight text-foreground sm:text-3xl">
            Benutzerverwaltung
          </h1>
          <p className="text-sm text-muted-foreground">
            Erstelle und verwalte Benutzerkonten, Rollen und Zugriffsrechte.
          </p>
        </header>

        <Dialog open={createOpen} onOpenChange={setCreateOpen}>
          <DialogTrigger render={<Button className="gap-2 shrink-0"><PlusIcon className="h-4 w-4" />Neuer Benutzer</Button>} />
          <DialogContent>
            <DialogHeader>
              <DialogTitle>Neuen Benutzer erstellen</DialogTitle>
              <DialogDescription>
                Erstelle ein neues lokales Benutzerkonto mit festgelegter Rolle.
              </DialogDescription>
            </DialogHeader>

            <form onSubmit={handleCreateUser} className="space-y-4 py-2">
              {createError && (
                <div
                  role="alert"
                  className="rounded-xl border border-destructive/30 bg-destructive/10 p-3 text-sm text-destructive"
                >
                  {createError}
                </div>
              )}

              <div className="space-y-1.5">
                <Label htmlFor="create-username">Benutzername</Label>
                <Input
                  id="create-username"
                  required
                  value={newUsername}
                  onChange={(e) => setNewUsername(e.target.value)}
                  placeholder="z. B. johndoe"
                  disabled={creating}
                />
              </div>

              <div className="space-y-1.5">
                <Label htmlFor="create-display-name">Anzeigename (optional)</Label>
                <Input
                  id="create-display-name"
                  value={newDisplayName}
                  onChange={(e) => setNewDisplayName(e.target.value)}
                  placeholder="z. B. John Doe"
                  disabled={creating}
                />
              </div>

              <div className="space-y-1.5">
                <Label htmlFor="create-password">Initiales Passwort</Label>
                <Input
                  id="create-password"
                  type="password"
                  required
                  value={newPassword}
                  onChange={(e) => setNewPassword(e.target.value)}
                  placeholder="Mindestens 8 Zeichen"
                  disabled={creating}
                />
              </div>

              <div className="space-y-1.5">
                <Label htmlFor="create-role">Rolle</Label>
                <select
                  id="create-role"
                  value={newRole}
                  onChange={(e) => setNewRole(e.target.value as Role)}
                  disabled={creating}
                  className="h-10 w-full rounded-xl border border-input bg-white/4 px-3.5 text-sm text-foreground transition-colors outline-none focus-visible:border-primary/50 focus-visible:bg-white/6"
                >
                  <option value="user" className="bg-[#151926] text-foreground">
                    Benutzer (Standard)
                  </option>
                  <option value="admin" className="bg-[#151926] text-foreground">
                    Administrator (Voller Zugriff)
                  </option>
                </select>
              </div>

              <DialogFooter>
                <DialogClose render={<Button variant="outline" type="button" disabled={creating}>Abbrechen</Button>} />
                <Button type="submit" disabled={creating}>
                  {creating ? 'Wird erstellt...' : 'Benutzer anlegen'}
                </Button>
              </DialogFooter>
            </form>
          </DialogContent>
        </Dialog>
      </div>

      {error && (
        <div
          role="alert"
          className="rounded-xl border border-destructive/30 bg-destructive/10 p-3.5 text-sm text-destructive"
        >
          {error}
        </div>
      )}

      {success && (
        <div
          role="status"
          className="flex items-center gap-2 rounded-xl border border-emerald-500/30 bg-emerald-500/10 p-3.5 text-sm text-emerald-400"
        >
          <CheckCircle2Icon className="h-4 w-4 shrink-0" />
          {success}
        </div>
      )}

      <Panel className="p-6 overflow-x-auto">
        <PanelHeader
          title="Benutzerliste"
          description="Alle im System registrierten Konten."
        />

        {loading ? (
          <div className="py-8 text-center text-sm text-muted-foreground">
            Benutzer werden geladen...
          </div>
        ) : users.length === 0 ? (
          <div className="py-8 text-center text-sm text-muted-foreground">
            Keine Benutzer gefunden.
          </div>
        ) : (
          <table className="w-full text-left text-sm mt-4">
            <thead>
              <tr className="border-b border-white/10 text-xs font-semibold uppercase text-muted-foreground tracking-wider">
                <th className="pb-3 pr-4">Benutzer</th>
                <th className="pb-3 px-4">Rolle</th>
                <th className="pb-3 px-4">Status</th>
                <th className="pb-3 px-4">Zuletzt aktiv</th>
                <th className="pb-3 pl-4 text-right">Aktionen</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-white/6">
              {users.map((u) => {
                const isSelf = u.id === currentUser?.id
                return (
                  <tr key={u.id} className="hover:bg-white/2 transition-colors">
                    <td className="py-3.5 pr-4">
                      <div className="font-medium text-foreground flex items-center gap-2">
                        {u.display_name || u.username}
                        {isSelf && (
                          <Badge variant="outline" className="text-[10px] py-0 px-1.5 border-primary/40 text-primary">
                            Du
                          </Badge>
                        )}
                      </div>
                      <div className="text-xs text-muted-foreground">@{u.username}</div>
                    </td>

                    <td className="py-3.5 px-4">
                      <Badge variant={u.role === 'admin' ? 'default' : 'neutral'} className="capitalize">
                        {u.role === 'admin' ? 'Administrator' : 'Benutzer'}
                      </Badge>
                    </td>

                    <td className="py-3.5 px-4">
                      <Badge
                        variant="outline"
                        className={
                          u.enabled
                            ? 'border-emerald-500/30 bg-emerald-500/10 text-emerald-400'
                            : 'border-destructive/30 bg-destructive/10 text-destructive'
                        }
                      >
                        {u.enabled ? 'Aktiv' : 'Deaktiviert'}
                      </Badge>
                    </td>

                    <td className="py-3.5 px-4 text-xs text-muted-foreground">
                      {u.last_login_at
                        ? new Date(u.last_login_at).toLocaleString('de-DE', {
                            dateStyle: 'short',
                            timeStyle: 'short',
                          })
                        : 'Nie'}
                    </td>

                    <td className="py-3.5 pl-4 text-right space-x-1 whitespace-nowrap">
                      {/* Toggle Role */}
                      <Button
                        variant="ghost"
                        size="sm"
                        title={u.role === 'admin' ? 'Zu Benutzer herabstufen' : 'Zu Admin befördern'}
                        onClick={() => handleToggleRole(u)}
                        className="h-8 px-2 text-muted-foreground hover:text-foreground"
                      >
                        <ShieldIcon className="h-3.5 w-3.5" />
                      </Button>

                      {/* Toggle Enabled */}
                      <Button
                        variant="ghost"
                        size="sm"
                        title={u.enabled ? 'Benutzer deaktivieren' : 'Benutzer aktivieren'}
                        onClick={() => handleToggleStatus(u)}
                        className="h-8 px-2 text-muted-foreground hover:text-foreground"
                      >
                        {u.enabled ? (
                          <UserXIcon className="h-3.5 w-3.5 text-amber-400" />
                        ) : (
                          <UserCheckIcon className="h-3.5 w-3.5 text-emerald-400" />
                        )}
                      </Button>

                      {/* Reset Password */}
                      <Button
                        variant="ghost"
                        size="sm"
                        title="Passwort zurücksetzen"
                        onClick={() => {
                          setResetTargetUser(u)
                          setResetNewPassword('')
                          setResetError(null)
                        }}
                        className="h-8 px-2 text-muted-foreground hover:text-foreground"
                      >
                        <KeyIcon className="h-3.5 w-3.5" />
                      </Button>

                      {/* Delete User */}
                      <Button
                        variant="ghost"
                        size="sm"
                        title="Benutzer löschen"
                        onClick={() => {
                          setDeleteTargetUser(u)
                          setDeleteError(null)
                        }}
                        className="h-8 px-2 text-muted-foreground hover:text-destructive hover:bg-destructive/10"
                      >
                        <Trash2Icon className="h-3.5 w-3.5" />
                      </Button>
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        )}
      </Panel>

      {/* Reset Password Modal */}
      <Dialog
        open={Boolean(resetTargetUser)}
        onOpenChange={(open) => {
          if (!open) setResetTargetUser(null)
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Passwort zurücksetzen</DialogTitle>
            <DialogDescription>
              Setze das Passwort für {resetTargetUser?.username} zurück. Alle bestehenden Sitzungen
              dieses Benutzers werden sofort widerrufen.
            </DialogDescription>
          </DialogHeader>

          <form onSubmit={handleResetPassword} className="space-y-4 py-2">
            {resetError && (
              <div
                role="alert"
                className="rounded-xl border border-destructive/30 bg-destructive/10 p-3 text-sm text-destructive"
              >
                {resetError}
              </div>
            )}

            <div className="space-y-1.5">
              <Label htmlFor="reset-new-password">Neues Passwort</Label>
              <Input
                id="reset-new-password"
                type="password"
                required
                value={resetNewPassword}
                onChange={(e) => setResetNewPassword(e.target.value)}
                placeholder="Mindestens 8 Zeichen"
                disabled={resetting}
              />
            </div>

            <DialogFooter>
              <Button
                variant="outline"
                type="button"
                onClick={() => setResetTargetUser(null)}
                disabled={resetting}
              >
                Abbrechen
              </Button>
              <Button type="submit" disabled={resetting}>
                {resetting ? 'Wird zurückgesetzt...' : 'Passwort zurücksetzen'}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      {/* Delete User Modal */}
      <Dialog
        open={Boolean(deleteTargetUser)}
        onOpenChange={(open) => {
          if (!open) setDeleteTargetUser(null)
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Benutzer löschen</DialogTitle>
            <DialogDescription>
              Möchtest du das Benutzerkonto von{' '}
              <strong className="text-foreground">{deleteTargetUser?.username}</strong> wirklich
              unwiderruflich löschen? Alle Sitzungen dieses Kontos werden sofort beendet.
            </DialogDescription>
          </DialogHeader>

          {deleteError && (
            <div
              role="alert"
              className="rounded-xl border border-destructive/30 bg-destructive/10 p-3 text-sm text-destructive"
            >
              {deleteError}
            </div>
          )}

          <DialogFooter>
            <Button
              variant="outline"
              type="button"
              onClick={() => setDeleteTargetUser(null)}
              disabled={deleting}
            >
              Abbrechen
            </Button>
            <Button
              variant="destructive"
              onClick={handleDeleteUser}
              disabled={deleting}
            >
              {deleting ? 'Wird gelöscht...' : 'Benutzer unwiderruflich löschen'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
