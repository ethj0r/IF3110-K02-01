import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { AccessQueryKeys, ListAccessibleDatabases } from '../api/access'
import { SessionQuery } from '../auth/query'
import { SignOutButton } from '../auth/SignOutButton'
import { LevelBadge, RequestError } from '../components/Feedback'

export function Workspace() {
  const Session = useQuery(SessionQuery)
  const Databases = useQuery({ queryKey: AccessQueryKeys.databases, queryFn: ListAccessibleDatabases })
  const IsAdmin = Session.data?.user.is_platform_admin === true

  return (
    <div className="workspace-shell">
      <header className="app-header">
        <Link to="/" className="app-brand">
          <strong>Database Platform</strong>
          <small>Workspace</small>
        </Link>
        <div className="app-header-actions">
          {IsAdmin && <Link to="/admin" className="back-link">Administration</Link>}
          <span className="muted">Signed in as {Session.data?.user.username}</span>
          <SignOutButton />
        </div>
      </header>

      <main className="workspace-main">
        <header className="workspace-heading">
          <h1>Database Workspace</h1>
          <p className="muted">
            Databases appear here when a group gives you access. Your highest level across all groups is shown.
          </p>
        </header>

        <section className="panel">
          <header className="panel-header">
            <div>
              <h2>Available databases</h2>
              <p>Connections granted through your groups.</p>
            </div>
            <span className="count-badge">{Databases.data?.length ?? 0}</span>
          </header>

          {Databases.isPending && <p className="state" role="status">Loading databases…</p>}
          <RequestError error={Databases.error} />

          {Databases.data?.length === 0 && (
            <div className="empty-state">
              <h3>No databases assigned</h3>
              <p>Ask an administrator to add you to a group with database access.</p>
              {IsAdmin && <Link to="/admin/access" className="link-button">Manage database access →</Link>}
            </div>
          )}

          {Databases.data && Databases.data.length > 0 && (
            <div className="table-wrap">
              <table className="data-table">
                <thead>
                  <tr>
                    <th>Database</th>
                    <th>Schema</th>
                    <th>Access</th>
                  </tr>
                </thead>
                <tbody>
                  {Databases.data.map(Database => (
                    <tr key={Database.id}>
                      <td>
                        <strong>{Database.name}</strong>
                        <span className="table-subtext">{Database.host}</span>
                      </td>
                      <td><code>{Database.dbname}</code></td>
                      <td><LevelBadge level={Database.level} /></td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </section>
      </main>
    </div>
  )
}
