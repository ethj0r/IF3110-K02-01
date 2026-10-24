import { afterEach as AfterEach, expect as Expect, test as Test, vi as Vi } from 'vitest'
import { cleanup as Cleanup, render as Render, screen as Screen } from '@testing-library/react'
import UserEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { App } from '../src/App'
import type { AdminGroup, AdminMatrix, AdminUser } from '../src/api/admin'
import type { ManagedConnection } from '../src/api/connections'

const Clients: QueryClient[] = []
const CSRF = 'c'.repeat(43)

AfterEach(() => {
  Cleanup()
  Clients.splice(0).forEach(Client => Client.clear())
  Vi.unstubAllGlobals()
})

function Mount(Path = '/admin/users') {
  const Client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity } } })
  Clients.push(Client)
  return Render(<QueryClientProvider client={Client}><MemoryRouter initialEntries={[Path]}><App /></MemoryRouter></QueryClientProvider>)
}

function Fixture() {
  const Users: AdminUser[] = [
    { id: 'user-1', username: 'reporter', is_platform_admin: false, is_active: true, created_at: '2026-01-01T00:00:00Z', groups: [{ id: 'group-1', name: 'Reporting' }] },
    { id: 'user-2', username: 'retired', is_platform_admin: false, is_active: false, created_at: '2026-01-02T00:00:00Z', groups: [] },
  ]
  const Groups: AdminGroup[] = [
    { id: 'group-1', name: 'Reporting', description: 'Reporting readers', member_count: 1, access_count: 1, created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z' },
  ]
  const Matrix: AdminMatrix = {
    groups: Groups,
    databases: [{ id: 'database-1', name: 'Reporting DB', host: '127.0.0.1', dbname: 'target' }],
    grants: [{ group_id: 'group-1', database_id: 'database-1', level: 'view_only' }],
  }
  return { Users, Groups, Matrix }
}

function InstallAPI(IsAdmin = true) {
  const State = { ...Fixture(), IsAdmin, Connections: [] as ManagedConnection[] }
  Vi.stubGlobal('fetch', Vi.fn(async (Path: string, Options?: RequestInit) => {
    const Method = (Options?.method ?? 'GET').toUpperCase()
    if (Path === '/api/auth/me') {
      return Response.json({ user: { id: 'admin-1', username: 'admin', is_platform_admin: State.IsAdmin }, csrf_token: CSRF, expires_at: '2099-01-01T00:00:00Z' })
    }
    if (Path === '/api/databases') return Response.json([])
    if (!State.IsAdmin) return Response.json({ error: { code: 'forbidden', message: 'Platform administrator access required' } }, { status: 403 })
    if (Path === '/api/connections' && Method === 'GET') return Response.json(State.Connections)
    if (Path === '/api/connections' && Method === 'POST') {
      const Input = JSON.parse(String(Options?.body)) as ManagedConnection
      const Created: ManagedConnection = {
        id: 'connection-1', name: Input.name, host: Input.host, port: Input.port, dbname: Input.dbname, username: Input.username,
        created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z',
      }
      State.Connections = [...State.Connections, Created]
      return Response.json(Created, { status: 201 })
    }
    if (Path === '/api/admin/users' && Method === 'GET') return Response.json(State.Users)
    if (Path === '/api/admin/users' && Method === 'POST') {
      const Input = JSON.parse(String(Options?.body)) as { username: string }
      const Created: AdminUser = { id: 'user-3', username: Input.username, is_platform_admin: false, is_active: true, created_at: '2026-01-03T00:00:00Z', groups: [] }
      State.Users = [...State.Users, Created]
      return Response.json(Created, { status: 201 })
    }
    if (Path === '/api/admin/groups' && Method === 'GET') return Response.json(State.Groups)
    if (Path === '/api/admin/groups/group-1/members' && Method === 'GET') return Response.json([])
    if (Path === '/api/admin/access' && Method === 'GET') return Response.json(State.Matrix)
    return Response.json({ error: { code: 'not_found', message: 'Resource not found' } }, { status: 404 })
  }))
  return State
}

Test('administrator can open user management and create an ordinary user', async () => {
  InstallAPI()
  Mount('/admin/users')
  const User = UserEvent.setup()
  await Screen.findByRole('heading', { name: 'Users' })
  await Screen.findByText('reporter')
  Expect(Screen.getByRole('link', { name: /Database access/ })).toBeTruthy()
  await User.click(Screen.getByRole('button', { name: 'New user' }))
  await User.type(Screen.getByLabelText('Username'), 'new-analyst')
  await User.type(Screen.getByLabelText('Password'), 'new analyst password')
  await User.click(Screen.getByRole('button', { name: 'Create user' }))
  await Screen.findByText('new-analyst')
  Expect(Screen.getByRole('status').textContent).toBe('User created.')
})

Test('administrator can inspect a group and add its first member', async () => {
  InstallAPI()
  Mount('/admin/groups')
  const User = UserEvent.setup()
  await Screen.findByRole('heading', { name: 'Groups' })
  await Screen.findByText('Reporting readers')
  await User.click(Screen.getByRole('button', { name: 'Manage' }))
  await Screen.findByRole('heading', { name: 'Reporting' })
  await Screen.findByText('No members yet.')
  Expect(Screen.getByLabelText('Add user')).toBeTruthy()
})

Test('administrator can view the database access matrix', async () => {
  InstallAPI()
  Mount('/admin/access')
  await Screen.findByRole('heading', { name: 'Database access' })
  await Screen.findByText('Reporting DB')
  Expect((Screen.getByLabelText('Reporting access to Reporting DB') as HTMLSelectElement).value).toBe('view_only')
})

Test('ordinary users are redirected away from administration routes', async () => {
  InstallAPI(false)
  Mount('/admin/users')
  await Screen.findByRole('heading', { name: 'Database Workspace' })
  Expect(Screen.queryByRole('heading', { name: 'Users' })).toBeNull()
})

Test('administrator can create a connection without an asynchronous form error', async () => {
  const API = InstallAPI()
  const Errors = Vi.spyOn(console, 'error')
  Mount('/admin/connections')
  const User = UserEvent.setup()
  await Screen.findByRole('heading', { name: 'Connections' })
  await User.click(Screen.getByRole('button', { name: 'New connection' }))
  await User.type(Screen.getByLabelText('Name'), 'Reporting target')
  await User.type(Screen.getByLabelText('Host'), '127.0.0.1')
  await User.type(Screen.getByLabelText('Database'), 'reporting')
  await User.type(Screen.getByLabelText('Username'), 'reader')
  await User.type(Screen.getByLabelText('Password'), 'reader fixture password')
  await User.click(Screen.getByRole('button', { name: 'Create connection' }))
  await Screen.findByText('Reporting target')
  Expect(Screen.queryByRole('heading', { name: 'Create connection' })).toBeNull()
  Expect(API.Connections).toHaveLength(1)
  Expect(Errors).not.toHaveBeenCalled()
})
