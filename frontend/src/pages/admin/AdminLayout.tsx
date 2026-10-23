import { useQuery } from '@tanstack/react-query'
import { Link, NavLink, Outlet } from 'react-router-dom'
import { SessionQuery } from '../../auth/query'
import { SignOutButton } from '../../auth/SignOutButton'

export function AdminLayout() {
  const Session = useQuery(SessionQuery)
  return (
    <div className="admin-shell">
      <header className="admin-header">
        <Link to="/" className="app-brand">
          <strong>Database Platform</strong>
          <small>Administration</small>
        </Link>

        <nav className="admin-nav" aria-label="Administration navigation">
          <NavLink to="/admin/users">Users</NavLink>
          <NavLink to="/admin/groups">Groups</NavLink>
          <NavLink to="/admin/access">Database access</NavLink>
        </nav>

        <div className="admin-account">
          <span className="muted">Signed in as {Session.data?.user.username}</span>
          <Link to="/" className="back-link">← Workspace</Link>
          <SignOutButton />
        </div>
      </header>

      <main className="admin-content">
        <Outlet />
      </main>
    </div>
  )
}
