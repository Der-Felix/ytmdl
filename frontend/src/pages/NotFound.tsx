import { CompassIcon, FileQuestionIcon, LayoutDashboardIcon } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Panel } from '@/components/ui/panel'
import { EmptyState } from '@/components/ui/state-view'
import { Link, paths } from '@/lib/router'

interface NotFoundProps {
  pathname: string
}

function NotFound({ pathname }: NotFoundProps) {
  return (
    <div className="space-y-8">
      <header className="space-y-1">
        <h1 className="font-heading text-2xl font-semibold tracking-tight text-foreground sm:text-3xl">
          Seite nicht gefunden
        </h1>
        <p className="text-sm text-muted-foreground">
          Die angeforderte Adresse <code className="rounded bg-white/5 px-1 py-0.5">{pathname}</code> existiert nicht.
        </p>
      </header>

      <Panel>
        <EmptyState
          icon={<FileQuestionIcon />}
          title="404 — Nichts gefunden"
          description="Die aufgerufene Seite existiert nicht oder wurde verschoben."
          action={
            <div className="flex flex-wrap items-center gap-3">
              <Button variant="default" render={<Link href={paths.dashboard()} />}>
                <LayoutDashboardIcon />
                Zum Dashboard
              </Button>
              <Button variant="outline" render={<Link href={paths.discover()} />}>
                <CompassIcon />
                Musik suchen
              </Button>
            </div>
          }
        />
      </Panel>
    </div>
  )
}

export { NotFound }
