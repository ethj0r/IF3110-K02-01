import { ApiError } from '../api/client'
import type { DatabaseAccessLevel } from '../api/access'

export function RequestError({ error }: { error: unknown }) {
  if (!error) return null
  const Message = error instanceof ApiError ? error.message : 'Unable to complete the request.'
  return <p role="alert" className="alert">{Message}</p>
}

export function StatusBadge({ active }: { active: boolean }) {
  return <span className={active ? 'badge active' : 'badge inactive'}>{active ? 'Active' : 'Inactive'}</span>
}

export function LevelBadge({ level }: { level: DatabaseAccessLevel }) {
  return (
    <span className={level === 'view_edit' ? 'level-badge edit' : 'level-badge view'}>
      {level === 'view_edit' ? 'View and edit' : 'View only'}
    </span>
  )
}
