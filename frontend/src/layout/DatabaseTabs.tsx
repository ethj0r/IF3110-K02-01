import { NavLink } from 'react-router-dom'
import type { DatabaseAccess } from '../api/access'
import { ActionLabel, useMigrationJobs } from '../api/jobs'
import { Icon } from '../ui/Icon'

export function DatabaseTabs({ Database }: { Database: DatabaseAccess }) {
  const Base = `/databases/${encodeURIComponent(Database.id)}`
  const { Active } = useMigrationJobs(Database.id)
  const IndicatorID = `migration-job-${Database.id}`
  return (
    <nav className="tabs" aria-label="Database sections">
      <NavLink to={Base} end className="tab"><Icon Name="table" Size={14} />Browse database</NavLink>
      <NavLink to={`${Base}/migrations`} className="tab" aria-describedby={Active ? IndicatorID : undefined}>
        <Icon Name="history" Size={14} />Migrations
        {Active && <span className="tab-pulse" aria-hidden="true" />}
      </NavLink>
      {Database.level === 'view_edit' && (
        <NavLink to={`${Base}/schema-diff`} className="tab"><Icon Name="diff" Size={14} />Compare schemas</NavLink>
      )}
      {Active && <span id={IndicatorID} className="sr-only">{ActionLabel(Active.action)} job {Active.status}</span>}
    </nav>
  )
}
