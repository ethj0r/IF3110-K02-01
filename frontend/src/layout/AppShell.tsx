import { useQuery } from '@tanstack/react-query'
import { useEffect, useRef, useState } from 'react'
import { Link, NavLink, Outlet, useLocation } from 'react-router-dom'
import { SessionQuery } from '../auth/query'
import { SignOutButton } from '../auth/SignOutButton'
import { Icon } from '../ui/Icon'
import type { IconName } from '../ui/Icon'

const WorkspaceLinks: { To: string; Label: string; Glyph: IconName; End?: boolean }[] = [
  { To: '/', Label: 'Databases', Glyph: 'database', End: true },
  { To: '/migrations', Label: 'Migration center', Glyph: 'layers', End: true },
]

const AdminLinks: { To: string; Label: string; Glyph: IconName }[] = [
  { To: '/admin/users', Label: 'Users', Glyph: 'users' },
  { To: '/admin/groups', Label: 'Groups', Glyph: 'group' },
  { To: '/admin/access', Label: 'Database access', Glyph: 'shield' },
  { To: '/admin/connections', Label: 'Connections', Glyph: 'plug' },
]

export function Brand() {
  return (
    <Link to="/" className="brand" aria-label="Database Platform home">
      <span className="brand-mark"><Icon Name="database" Size={16} /></span>
      <span className="brand-name">Database Platform</span>
    </Link>
  )
}

export function AppShell() {
  const Session = useQuery(SessionQuery)
  const Location = useLocation()
  const [OpenPath, SetOpenPath] = useState<string | null>(null)
  const NavOpen = OpenPath === Location.pathname
  const User = Session.data?.user
  const IsAdmin = User?.is_platform_admin === true
  const MenuButton = useRef<HTMLButtonElement>(null)
  const CloseButton = useRef<HTMLButtonElement>(null)

  function CloseNav() {
    SetOpenPath(null)
    MenuButton.current?.focus()
  }

  useEffect(() => {
    if (!NavOpen) return
    CloseButton.current?.focus()
    function HandleKey(Event: KeyboardEvent) {
      if (Event.key === 'Escape') {
        SetOpenPath(null)
        MenuButton.current?.focus()
      }
    }
    window.addEventListener('keydown', HandleKey)
    return () => window.removeEventListener('keydown', HandleKey)
  }, [NavOpen])

  return (
    <div className={NavOpen ? 'shell nav-open' : 'shell'}>
      <aside className="sidebar" id="app-sidebar" aria-label="Sidebar">
        <div className="sidebar-head">
          <Brand />
          <button ref={CloseButton} className="btn btn-ghost btn-icon btn-sm sidebar-close" type="button" aria-label="Close menu" onClick={CloseNav}>
            <Icon Name="close" />
          </button>
        </div>

        <nav className="nav" aria-label="Workspace">
          {WorkspaceLinks.map(Item => (
            <NavLink key={Item.To} to={Item.To} end={Item.End} className="nav-link">
              <Icon Name={Item.Glyph} />
              {Item.Label}
            </NavLink>
          ))}
        </nav>

        {IsAdmin && (
          <nav className="nav" aria-label="Administration">
            {AdminLinks.map(Item => (
              <NavLink key={Item.To} to={Item.To} className="nav-link">
                <Icon Name={Item.Glyph} />
                {Item.Label}
              </NavLink>
            ))}
          </nav>
        )}

        <div className="sidebar-foot">
          <div className="account">
            <span className="avatar" aria-hidden="true">{User?.username.slice(0, 1) ?? '?'}</span>
            <div className="account-text">
              <strong>{User?.username}</strong>
              <span>{IsAdmin ? 'Platform administrator' : 'Member'}</span>
            </div>
          </div>
          <span className="sr-only">Signed in as {User?.username}</span>
          <SignOutButton />
        </div>
      </aside>

      <div className="scrim" aria-hidden="true" onClick={CloseNav} />

      <div className="main">
        <header className="topbar">
          <Brand />
          <button
            ref={MenuButton}
            className="btn btn-secondary btn-icon btn-sm"
            type="button"
            aria-label="Open menu"
            aria-controls="app-sidebar"
            aria-expanded={NavOpen}
            onClick={() => SetOpenPath(Location.pathname)}
          >
            <Icon Name="menu" />
          </button>
        </header>
        <main className="content">
          <Outlet />
        </main>
      </div>
    </div>
  )
}
