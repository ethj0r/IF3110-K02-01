import type { ReactNode } from 'react'
import type { DatabaseAccessLevel } from '../api/access'
import { Icon } from './Icon'
import type { IconName } from './Icon'
import { ErrorMessage } from './errors'

export function RequestError({ error }: { error: unknown }) {
  if (!error) return null
  return <Notice Tone="danger" Role="alert">{ErrorMessage(error)}</Notice>
}

export function Notice({ Tone = 'neutral', Role, children }: {
  Tone?: 'neutral' | 'success' | 'danger' | 'warning'
  Role?: 'status' | 'alert'
  children: ReactNode
}) {
  const Glyph: IconName = Tone === 'success' ? 'check' : Tone === 'neutral' ? 'info' : 'alert'
  return (
    <div className={Tone === 'neutral' ? 'notice' : `notice notice-${Tone}`} role={Role}>
      <Icon Name={Glyph} />
      <div>{children}</div>
    </div>
  )
}

export function Loading({ children }: { children: ReactNode }) {
  return (
    <p className="state" role="status">
      <span className="spinner" aria-hidden="true" />
      {children}
    </p>
  )
}

export function EmptyState({ IconName: Glyph = 'database', Title, children }: {
  IconName?: IconName
  Title: string
  children?: ReactNode
}) {
  return (
    <div className="empty">
      <span className="empty-icon"><Icon Name={Glyph} Size={20} /></span>
      <h3>{Title}</h3>
      {children}
    </div>
  )
}

export type Tone = 'neutral' | 'success' | 'danger' | 'warning' | 'info' | 'ink' | 'outline'

export function Badge({ Tone: Variant = 'neutral', Dot = false, children }: { Tone?: Tone; Dot?: boolean; children: ReactNode }) {
  const Classes = ['badge']
  if (Variant !== 'neutral') Classes.push(`badge-${Variant}`)
  if (Dot) Classes.push('badge-dot')
  return <span className={Classes.join(' ')}>{children}</span>
}

export function StatusBadge({ active }: { active: boolean }) {
  return <Badge Tone={active ? 'success' : 'outline'} Dot>{active ? 'Active' : 'Inactive'}</Badge>
}

export function LevelBadge({ level }: { level: DatabaseAccessLevel }) {
  return <Badge Tone={level === 'view_edit' ? 'ink' : 'neutral'}>{level === 'view_edit' ? 'View and edit' : 'View only'}</Badge>
}

const StatusTones: Record<string, Tone> = {
  applied: 'success',
  committed: 'success',
  succeeded: 'success',
  approved: 'success',
  failed: 'danger',
  rolled_back: 'danger',
  partial: 'warning',
  partially_reverted: 'warning',
  pending: 'warning',
  pending_review: 'warning',
  queued: 'info',
  running: 'info',
  draft: 'neutral',
}

export function StateBadge({ Status }: { Status: string }) {
  return <Badge Tone={StatusTones[Status] ?? 'neutral'} Dot>{Status}</Badge>
}
