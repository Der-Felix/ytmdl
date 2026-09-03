import { useState, type FormEvent } from 'react'
import { MusicIcon, ShieldCheckIcon } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Panel } from '@/components/ui/panel'
import { useAuth } from '@/hooks/useAuth'
import { useNavigate } from '@/lib/router'
import { errorMessage } from '@/lib/api/client'

export function Login() {
  const { setupRequired, login, setup, loading } = useAuth()
  const navigate = useNavigate()

  const [username, setUsername] = useState('')
  const [displayName, setDisplayName] = useState('')
  const [password, setPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [formError, setFormError] = useState<string | null>(null)

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault()
    setFormError(null)

    if (!username.trim()) {
      setFormError('Bitte gib einen Benutzernamen ein.')
      return
    }
    if (username.trim().length < 3) {
      setFormError('Der Benutzername muss mindestens 3 Zeichen lang sein.')
      return
    }
    if (!password) {
      setFormError('Bitte gib ein Passwort ein.')
      return
    }

    if (setupRequired) {
      if (password.length < 8) {
        setFormError('Das Passwort muss mindestens 8 Zeichen lang sein.')
        return
      }
      if (password !== confirmPassword) {
        setFormError('Die Passwörter stimmen nicht überein.')
        return
      }
    }

    try {
      setSubmitting(true)
      if (setupRequired) {
        await setup({
          username: username.trim(),
          display_name: displayName.trim() || undefined,
          password,
        })
      } else {
        await login({
          username: username.trim(),
          password,
        })
      }
      navigate('/')
    } catch (err) {
      setFormError(errorMessage(err))
    } finally {
      setSubmitting(false)
    }
  }

  if (loading) {
    return (
      <div className="flex min-h-[70vh] items-center justify-center">
        <div className="h-8 w-8 animate-spin rounded-full border-2 border-primary border-t-transparent" />
      </div>
    )
  }

  return (
    <div className="flex min-h-[75vh] flex-col items-center justify-center px-4 py-12">
      <div className="w-full max-w-md space-y-6">
        <div className="text-center space-y-2">
          <div className="mx-auto flex h-14 w-14 items-center justify-center rounded-2xl bg-primary/10 text-primary ring-1 ring-primary/25">
            {setupRequired ? (
              <ShieldCheckIcon className="h-7 w-7" />
            ) : (
              <MusicIcon className="h-7 w-7" />
            )}
          </div>
          <h1 className="font-heading text-2xl font-bold tracking-tight text-foreground sm:text-3xl">
            {setupRequired ? 'YTMDL Ersteinrichtung' : 'Anmelden bei YTMDL'}
          </h1>
          <p className="text-sm text-muted-foreground">
            {setupRequired
              ? 'Erstelle das initiale Administrator-Konto für deine Musikbibliothek.'
              : 'Gib deine Zugangsdaten ein, um fortzufahren.'}
          </p>
        </div>

        <Panel className="p-6 sm:p-8">
          <form onSubmit={handleSubmit} className="space-y-4">
            {formError && (
              <div
                role="alert"
                className="rounded-xl border border-destructive/30 bg-destructive/10 p-3.5 text-sm text-destructive"
              >
                {formError}
              </div>
            )}

            <div className="space-y-1.5">
              <Label htmlFor="username">Benutzername</Label>
              <div className="relative">
                <Input
                  id="username"
                  type="text"
                  autoComplete="username"
                  autoFocus
                  required
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                  placeholder="z. B. admin"
                  disabled={submitting}
                />
              </div>
            </div>

            {setupRequired && (
              <div className="space-y-1.5">
                <Label htmlFor="display-name">Anzeigename (optional)</Label>
                <Input
                  id="display-name"
                  type="text"
                  autoComplete="name"
                  value={displayName}
                  onChange={(e) => setDisplayName(e.target.value)}
                  placeholder="z. B. Administrator"
                  disabled={submitting}
                />
              </div>
            )}

            <div className="space-y-1.5">
              <Label htmlFor="password">Passwort</Label>
              <Input
                id="password"
                type="password"
                autoComplete={setupRequired ? 'new-password' : 'current-password'}
                required
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                placeholder={setupRequired ? 'Mindestens 8 Zeichen' : 'Passwort'}
                disabled={submitting}
              />
            </div>

            {setupRequired && (
              <div className="space-y-1.5">
                <Label htmlFor="confirm-password">Passwort bestätigen</Label>
                <Input
                  id="confirm-password"
                  type="password"
                  autoComplete="new-password"
                  required
                  value={confirmPassword}
                  onChange={(e) => setConfirmPassword(e.target.value)}
                  placeholder="Passwort wiederholen"
                  disabled={submitting}
                />
              </div>
            )}

            <Button
              type="submit"
              className="w-full mt-2"
              disabled={submitting}
            >
              {submitting
                ? 'Wird verarbeitet...'
                : setupRequired
                  ? 'Administrator erstellen'
                  : 'Anmelden'}
            </Button>
          </form>
        </Panel>
      </div>
    </div>
  )
}
