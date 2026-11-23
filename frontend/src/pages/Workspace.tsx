import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { AccessQueryKeys, ListAccessibleDatabases } from '../api/access'
import { SessionQuery } from '../auth/query'
import { PageHeader } from '../ui/Card'
import { EmptyState, LevelBadge, Loading, RequestError } from '../ui/Feedback'
import { Icon } from '../ui/Icon'

export function Workspace() {
  const Session = useQuery(SessionQuery)
  const Databases = useQuery({ queryKey: AccessQueryKeys.databases, queryFn: ListAccessibleDatabases })
  const IsAdmin = Session.data?.user.is_platform_admin === true

  return (
    <>
      <PageHeader Title="Database Workspace" />

      <section className="stack" aria-label="Databases">

        {Databases.isPending && <Loading>Loading databases…</Loading>}
        <RequestError error={Databases.error} />

        {Databases.data?.length === 0 && (
          <div className="card">
            <EmptyState Title="No databases assigned">
              {IsAdmin && <Link to="/admin/access" className="btn btn-secondary btn-sm">Manage database access<Icon Name="arrow-right" Size={14} /></Link>}
            </EmptyState>
          </div>
        )}

        {Databases.data && Databases.data.length > 0 && (
          <ul className="db-grid">
            {Databases.data.map(Database => (
              <li key={Database.id} className="db-card">
                <div className="db-card-top">
                  <span className="db-card-icon"><Icon Name="database" Size={18} /></span>
                  <LevelBadge level={Database.level} />
                </div>
                <div className="db-card-name">
                  <Link to={`/databases/${Database.id}`} className="db-card-link"><strong>{Database.name}</strong></Link>
                  <span className="cell-sub">{Database.host}</span>
                </div>
                <div className="db-card-foot">
                  <code className="inline-code">{Database.dbname}</code>
                  <Icon Name="arrow-right" Size={14} />
                </div>
              </li>
            ))}
          </ul>
        )}
      </section>
    </>
  )
}
