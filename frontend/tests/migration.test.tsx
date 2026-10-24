import { afterEach as AfterEach, expect as Expect, test as Test, vi as Vi } from 'vitest'
import { cleanup as Cleanup, render as Render, screen as Screen } from '@testing-library/react'
import UserEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { App } from '../src/App'

const Clients: QueryClient[] = []
const Databases = [{ id: 'db-1', name: 'Reporting', host: '127.0.0.1', dbname: 'reporting', level: 'view_edit' }]

AfterEach(() => {
  Cleanup()
  Clients.splice(0).forEach(Client => Client.clear())
  Vi.unstubAllGlobals()
})

function Mount(InitialPath = '/databases/db-1/migrations') {
  const Client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity } } })
  Clients.push(Client)
  const View = Render(
    <QueryClientProvider client={Client}>
      <MemoryRouter initialEntries={[InitialPath]}>
        <App />
      </MemoryRouter>
    </QueryClientProvider>,
  )
  return { ...View, Client }
}

Test('loads and displays linear migrations table and views details', async () => {
  Vi.stubGlobal(
    'fetch',
    Vi.fn(async (Path: string) => {
      if (Path === '/api/databases') return Response.json(Databases)
      if (Path === '/api/auth/me') {
        return Response.json({
          user: { id: 'u1', username: 'tester', is_platform_admin: false },
          csrf_token: 'csrf-token',
          expires_at: '2099-01-01T00:00:00Z',
        })
      }
      if (Path === '/api/databases/db-1/migrations') {
        return Response.json([
          {
            id: 'mig-1',
            database_id: 'db-1',
            version: 1,
            title: 'Initial Schema',
            sql_text: 'CREATE TABLE sample (id INT);',
            status: 'committed',
            created_at: '2026-01-01T00:00:00Z',
            executed_at: '2026-01-01T00:05:00Z',
            executed_by_username: 'operator',
          },
        ])
      }
      return new Response(null, { status: 404 })
    }),
  )

  Mount()
  const User = UserEvent.setup()

  await Screen.findByRole('heading', { name: 'Database Migrations' })
  Expect(await Screen.findByText('Initial Schema')).toBeTruthy()
  Expect(Screen.getByText('v1')).toBeTruthy()
  Expect(Screen.getByText('committed')).toBeTruthy()

  await User.click(Screen.getByRole('button', { name: 'View' }))
  Expect(await Screen.findByText('CREATE TABLE sample (id INT);')).toBeTruthy()
  Expect(Screen.getByText('Executed by:').closest('p')?.textContent).toContain('operator')
})

Test('creates and executes draft migration', async () => {
  let Migrations = [
    {
      id: 'mig-1',
      database_id: 'db-1',
      version: 1,
      title: 'Draft Migration',
      sql_text: 'CREATE TABLE t1 (id INT);',
      status: 'draft',
      created_at: '2026-01-01T00:00:00Z',
    },
  ]

  Vi.stubGlobal('confirm', () => true)

  Vi.stubGlobal(
    'fetch',
    Vi.fn(async (Path: string, Options?: RequestInit) => {
      if (Path === '/api/databases') return Response.json(Databases)
      if (Path === '/api/auth/me') {
        return Response.json({
          user: { id: 'u1', username: 'tester', is_platform_admin: false },
          csrf_token: 'csrf-token',
          expires_at: '2099-01-01T00:00:00Z',
        })
      }
      if (Path === '/api/auth/csrf') {
        return Response.json({ csrf_token: 'csrf-token' })
      }
      if (Path === '/api/databases/db-1/migrations' && (!Options || Options.method === 'GET')) {
        return Response.json(Migrations)
      }
      if (Path === '/api/databases/db-1/migrations' && Options?.method === 'POST') {
        const Body = JSON.parse(String(Options.body)) as { title: string; sql_text: string }
        const Created = {
          id: 'mig-2',
          database_id: 'db-1',
          version: 2,
          title: Body.title,
          sql_text: Body.sql_text,
          status: 'draft' as const,
          created_at: '2026-01-01T00:00:00Z',
        }
        Migrations = [...Migrations, Created]
        return Response.json(Created, { status: 201 })
      }
      if (Path === '/api/databases/db-1/migrations/mig-1/execute') {
        const Executed = {
          ...Migrations[0],
          status: 'committed' as const,
          executed_at: '2026-01-01T00:01:00Z',
        }
        Migrations = [Executed]
        return Response.json(Executed)
      }
      return new Response(null, { status: 404 })
    }),
  )

  const { Client } = Mount()
  const SchemaKey = ['databases', 'db-1', 'schemas'] as const
  Client.setQueryData(SchemaKey, { schemas: ['public'] })
  const User = UserEvent.setup()

  await Screen.findByRole('heading', { name: 'Database Migrations' })
  Expect(await Screen.findByText('Draft Migration')).toBeTruthy()

  // Open editor and create
  await User.click(Screen.getByRole('button', { name: '+ Create Migration' }))
  await User.type(Screen.getByLabelText(/Title:/), 'Second Migration')
  await User.type(Screen.getByLabelText(/SQL Query/), 'CREATE TABLE t2 (id INT);')
  await User.click(Screen.getByRole('button', { name: 'Save Draft' }))

  Expect(await Screen.findByText('Migration draft saved successfully.')).toBeTruthy()

  // Execute
  await User.click(Screen.getAllByRole('button', { name: 'Execute' })[0])
  Expect(await Screen.findByText(/committed successfully/)).toBeTruthy()
  Expect(Client.getQueryState(SchemaKey)?.isInvalidated).toBe(true)
})

function InstallMigrationAPI(Level = 'view_edit', Status = 'draft') {
  const Fetch = Vi.fn(async (Path: string) => {
    if (Path === '/api/auth/me') return Response.json({ user: { id: 'u1', username: 'tester', is_platform_admin: false }, csrf_token: 'c'.repeat(43), expires_at: '2099-01-01T00:00:00Z' })
    if (Path === '/api/databases') return Response.json([...Databases.map(Item => ({ ...Item, level: Level })), { ...Databases[0], id: 'db-2', name: 'Analytics' }])
    if (Path.endsWith('/schemas')) return Response.json({ schemas: ['public'] })
    if (Path.includes('/tables?')) return Response.json({ tables: [] })
    if (Path.endsWith('/migrations')) return Response.json([
      { id: 'first', database_id: 'db-1', version: 1, title: Path.includes('db-2') ? 'Analytics history' : 'First migration', sql_text: 'SELECT 1;', status: Status, created_at: '2026-01-01T00:00:00Z' },
      { id: 'second', database_id: 'db-1', version: 2, title: 'Second migration', sql_text: 'SELECT 2;', status: 'draft', created_at: '2026-01-01T00:00:00Z' },
    ])
    return new Response(null, { status: 404 })
  })
  Vi.stubGlobal('fetch', Fetch)
  return Fetch
}

Test('view-only users can inspect history without mutation controls', async () => {
  InstallMigrationAPI('view_only', 'rolled_back')
  Mount()
  const User = UserEvent.setup()
  await Screen.findByText('First migration')
  for (const Name of ['+ Create Migration', 'Edit', 'Execute', 'Delete', 'Retry']) {
    Expect(Screen.queryByRole('button', { name: Name })).toBeNull()
  }
  await User.click(Screen.getAllByRole('button', { name: 'View' })[0])
  Expect(await Screen.findByText('SELECT 1;')).toBeTruthy()
})

Test('viewer opens its migrations and switching database clears the editor', async () => {
  InstallMigrationAPI()
  Mount('/databases/db-1')
  const User = UserEvent.setup()
  await User.click(await Screen.findByRole('link', { name: 'Migrations' }))
  await Screen.findByText('First migration')
  const Execute = Screen.getAllByRole('button', { name: 'Execute' })
  Expect((Execute[0] as HTMLButtonElement).disabled).toBe(false)
  Expect((Execute[1] as HTMLButtonElement).disabled).toBe(true)
  await User.click(Screen.getAllByRole('button', { name: 'View' })[0])
  await User.click(Screen.getByRole('button', { name: '+ Create Migration' }))
  await User.type(Screen.getByLabelText(/SQL Query/), 'CREATE TABLE old_database_only (id int);')
  await User.selectOptions(Screen.getByLabelText('Database:'), 'db-2')
  await Screen.findByText('Analytics history')
  Expect(Screen.queryByLabelText(/SQL Query/)).toBeNull()
  Expect(Screen.queryByText('SELECT 1;')).toBeNull()
})

Test('pending execution disables later execution and explains recovery', async () => {
  InstallMigrationAPI('view_edit', 'pending')
  Mount()
  await Screen.findByText('First migration')
  Expect((Screen.getByRole('button', { name: 'Execute' }) as HTMLButtonElement).disabled).toBe(true)
  Expect(Screen.getByRole('status').textContent).toContain('verify the target outcome')
})

Test('unknown database routes do not load migration history', async () => {
  const Fetch = InstallMigrationAPI()
  Mount('/databases/no-grant/migrations')
  Expect((await Screen.findByRole('alert')).textContent).toBe('No access to this database.')
  Expect(Fetch.mock.calls.some(([Path]) => Path.includes('/no-grant/migrations'))).toBe(false)
})

