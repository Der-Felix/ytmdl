import { useState, type FormEvent } from 'react'
import { TvIcon } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Panel, PanelHeader } from '@/components/ui/panel'
import { errorMessage, request, requestVoid } from '@/lib/api/client'

interface Preview { device_name: string; expires_at: string }

export function DevicePairing() {
  const [code, setCode] = useState(() => new URLSearchParams(window.location.search).get('device_code')?.slice(0, 20) ?? '')
  const [preview, setPreview] = useState<Preview | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [success, setSuccess] = useState(false)

  async function inspect(event: FormEvent) {
    event.preventDefault()
    setBusy(true); setError(null); setPreview(null); setSuccess(false)
    try { setPreview(await request<Preview>('/auth/device/preview', { method: 'POST', body: { user_code: code } })) }
    catch (failure) { setError(errorMessage(failure)) }
    finally { setBusy(false) }
  }

  async function confirm() {
    setBusy(true); setError(null)
    try {
      await requestVoid('/auth/device/confirm', { method: 'POST', body: { user_code: code } })
      setPreview(null); setCode(''); setSuccess(true)
      const url = new URL(window.location.href)
      url.searchParams.delete('device_code')
      window.history.replaceState(null, '', `${url.pathname}${url.search}${url.hash}`)
    } catch (failure) { setError(errorMessage(failure)); setPreview(null) }
    finally { setBusy(false) }
  }

  return (
    <section aria-labelledby="device-pairing-heading" className="space-y-3">
      <PanelHeader title={<span id="device-pairing-heading">Apple TV & Geräte anmelden</span>} description="Bestätige den kurzen Code auf einem Gerät, das bereits angemeldet ist." />
      <Panel className="space-y-4 p-6">
        <form onSubmit={inspect} className="max-w-md space-y-3">
          <Label htmlFor="device-code">Code vom gewünschten Gerät</Label>
          <Input id="device-code" value={code} placeholder="ABCD-EFGH" autoCapitalize="characters" autoComplete="off" spellCheck={false} maxLength={20} disabled={busy} onChange={(event) => { setCode(event.target.value.toUpperCase()); setPreview(null); setSuccess(false); setError(null) }} />
          <Button type="submit" disabled={busy || code.replace(/[-\s]/g, '').length !== 8}>Gerät prüfen</Button>
        </form>
        {preview && <div className="space-y-3 rounded-xl border border-primary/20 bg-primary/5 p-4">
          <p className="flex items-center gap-2 font-medium"><TvIcon className="h-5 w-5" aria-hidden="true" />{preview.device_name} anmelden?</p>
          <p className="text-sm text-muted-foreground">Nur freigeben, wenn du diesen Code selbst auf deinem Gerät angefordert hast. Das Gerät erhält die Rechte deines Kontos, einschließlich Verwaltungsrechten bei einem Administratorkonto. Du kannst seine Sitzung hier im Profil jederzeit beenden.</p>
          <Button disabled={busy} onClick={confirm}>Dieses Gerät ausdrücklich freigeben</Button>
        </div>}
        {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
        {success && <p role="status" className="text-sm text-muted-foreground">Gerät freigegeben. Die Anmeldung wird auf dem Gerät abgeschlossen; die Sitzung erscheint nach dem Aktualisieren im Profil.</p>}
      </Panel>
    </section>
  )
}
