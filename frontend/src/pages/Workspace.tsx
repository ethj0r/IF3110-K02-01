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
  const Count = Databases.data?.length ?? 0

  return (
    <>
      <PageHeader
        Eyebrow="Workspace"
        Title="Database Workspace"
        Description="Databases appear here when a group gives you access. Your highest level across all groups is shown."
        Actions={<Link to="/migrations" className="btn btn-secondary"><Icon Name="layers" Size={14} />Migration center</Link>}
      />

      <section className="stack" aria-labelledby="databases-heading">
        <div className="toolbar">
          <div className="card-title">
            <h2 id="databases-heading">Available databases</h2>
            <p className="muted">{Databases.data ? `${Count} connection${Count === 1 ? '' : 's'} granted through your groups.` : 'Connections granted through your groups.'}</p>
          </div>
        </div>

        {Databases.isPending && <Loading>Loading databases…</Loading>}
        <RequestError error={Databases.error} />

        {Databases.data?.length === 0 && (
          <div className="card">
            <EmptyState Title="No databases assigned">
              <p>Ask an administrator to add you to a group with database access.</p>
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
