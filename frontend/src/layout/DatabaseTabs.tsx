import { NavLink } from 'react-router-dom'
import type { DatabaseAccess } from '../api/access'
import { Icon } from '../ui/Icon'

export function DatabaseTabs({ Database }: { Database: DatabaseAccess }) {
  const Base = `/databases/${encodeURIComponent(Database.id)}`
  return (
    <nav className="tabs" aria-label="Database sections">
      <NavLink to={Base} end className="tab"><Icon Name="table" Size={14} />Browse database</NavLink>
      <NavLink to={`${Base}/migrations`} className="tab"><Icon Name="history" Size={14} />Migrations</NavLink>
      {Database.level === 'view_edit' && (
        <NavLink to={`${Base}/schema-diff`} className="tab"><Icon Name="diff" Size={14} />Compare schemas</NavLink>
      )}
    </nav>
  )
}
