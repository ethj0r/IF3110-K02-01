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

function MigrationFixture(Overrides: Record<string, unknown> = {}) {
  return {
    id: 'mig-1',
    database_id: 'db-1',
    version: 1,
    revision: 1,
    title: 'Initial Schema',
    sql_text: 'CREATE TABLE sample (id INT);',
    down_sql: 'DROP TABLE sample;',
    irreversible: false,
    status: 'applied',
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:05:00Z',
    executed_at: '2026-01-01T00:05:00Z',
    executed_by_username: 'operator',
    locked_at: '2026-01-01T00:05:00Z',
    can_apply: false,
    can_rollback: true,
    can_forward: false,
    can_retry: false,
    ...Overrides,
  }
}

function ExecutionFixture(Overrides: Record<string, unknown> = {}) {
  return {
    id: 'exec-1',
    migration_id: 'mig-1',
    database_id: 'db-1',
    version: 1,
    revision: 1,
    title: 'Initial Schema',
    kind: 'apply',
    direction: 'up',
    status: 'succeeded',
    sql_text: 'CREATE TABLE sample (id INT);',
    up_sql: 'CREATE TABLE sample (id INT);',
    down_sql: 'DROP TABLE sample;',
    requested_by: 'u1',
    requested_by_username: 'operator',
    created_at: '2026-01-01T00:05:00Z',
    started_at: '2026-01-01T00:05:00Z',
    finished_at: '2026-01-01T00:05:01Z',
    attempts: [{
      id: 'attempt-1',
      execution_id: 'exec-1',
      attempt_no: 1,
      version: 1,
      revision: 1,
      actor_username: 'operator',
      sql_text: 'CREATE TABLE sample (id INT);',
      status: 'succeeded',
      started_at: '2026-01-01T00:05:00Z',
      finished_at: '2026-01-01T00:05:01Z',
      duration_ms: 1000,
    }],
    ...Overrides,
  }
}

Test('loads and displays migration definitions and views details', async () => {
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
      if (Path.startsWith('/api/databases/db-1/migrations/history')) return Response.json([ExecutionFixture()])
      if (Path === '/api/databases/db-1/migrations') return Response.json([MigrationFixture()])
      return new Response(null, { status: 404 })
    }),
  )

  Mount()
  const User = UserEvent.setup()

  await Screen.findByRole('heading', { name: 'Database Migrations' })
  Expect(await Screen.findByText('Initial Schema')).toBeTruthy()
  Expect(Screen.getByText('v1')).toBeTruthy()
  Expect(Screen.getAllByText('applied').length).toBeGreaterThan(0)
  Expect(await Screen.findByText('Execution')).toBeTruthy()

  await User.click(Screen.getByRole('button', { name: 'View' }))
  Expect(await Screen.findByText('CREATE TABLE sample (id INT);')).toBeTruthy()
  Expect(Screen.getByText('DROP TABLE sample;')).toBeTruthy()
  Expect(Screen.getByText('Status:').closest('p')?.textContent).toContain('applied')
  Expect(Screen.getByText('Status:').closest('p')?.textContent).toContain('Revision:')
})

Test('creates and applies a draft migration', async () => {
  let Migrations = [
    MigrationFixture({
      id: 'mig-1',
      title: 'Draft Migration',
      status: 'draft',
      locked_at: undefined,
      executed_at: undefined,
      can_apply: true,
      can_rollback: false,
    }),
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
      if (Path === '/api/auth/csrf') return Response.json({ csrf_token: 'csrf-token' })
      if (Path.startsWith('/api/databases/db-1/migrations/history')) return Response.json([])
      if (Path === '/api/databases/db-1/migrations' && (!Options || Options.method === 'GET')) return Response.json(Migrations)
      if (Path === '/api/databases/db-1/migrations' && Options?.method === 'POST') {
        const Body = JSON.parse(String(Options.body)) as { title: string; sql_text: string; down_sql: string }
        const Created = MigrationFixture({ id: 'mig-2', version: 2, title: Body.title, sql_text: Body.sql_text, down_sql: Body.down_sql, status: 'draft', locked_at: undefined, can_apply: false, can_rollback: false })
        Migrations = [...Migrations, Created]
        return Response.json(Created, { status: 201 })
      }
      if (Path === '/api/databases/db-1/migrations/mig-1/apply') {
        return Response.json(ExecutionFixture())
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

  await User.click(Screen.getByRole('button', { name: '+ Create Migration' }))
  await User.type(Screen.getByLabelText(/Title:/), 'Second Migration')
  await User.type(Screen.getByLabelText(/Up SQL:/), 'CREATE TABLE t2 (id INT);')
  await User.type(Screen.getByLabelText(/Down SQL:/), 'DROP TABLE t2;')
  await User.click(Screen.getByRole('button', { name: 'Save Draft' }))
  Expect(await Screen.findByText('Migration draft saved successfully.')).toBeTruthy()

  await User.click(Screen.getAllByRole('button', { name: 'Apply' })[0])
  Expect(await Screen.findByText(/finished with status succeeded/)).toBeTruthy()
  Expect(Client.getQueryState(SchemaKey)?.isInvalidated).toBe(true)
})

function InstallMigrationAPI(Level = 'view_edit', Status = 'draft') {
  const Fetch = Vi.fn(async (Path: string) => {
    if (Path === '/api/auth/me') return Response.json({ user: { id: 'u1', username: 'tester', is_platform_admin: false }, csrf_token: 'c'.repeat(43), expires_at: '2099-01-01T00:00:00Z' })
    if (Path === '/api/databases') return Response.json([...Databases.map(Item => ({ ...Item, level: Level })), { ...Databases[0], id: 'db-2', name: 'Analytics' }])
    if (Path.includes('/migrations/history')) return Response.json(Status === 'pending' ? [ExecutionFixture({ status: 'running', finished_at: undefined })] : [])
    if (Path.startsWith('/api/databases/db-2/migrations')) return Response.json([MigrationFixture({ database_id: 'db-2', title: 'Analytics history' })])
    if (Path.startsWith('/api/databases/db-1/migrations')) return Response.json([
      MigrationFixture({ id: 'first', version: 1, title: 'First migration', status: Status, locked_at: Status === 'draft' ? undefined : '2026-01-01T00:05:00Z', can_apply: Status === 'draft', can_rollback: Status === 'applied' }),
      MigrationFixture({ id: 'second', version: 2, title: 'Second migration', sql_text: 'SELECT 2;', status: 'draft', locked_at: undefined, can_apply: false, can_rollback: false }),
    ])
    if (Path.endsWith('/schemas')) return Response.json({ schemas: ['public'] })
    if (Path.includes('/tables?')) return Response.json({ tables: [] })
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
  for (const Name of ['+ Create Migration', 'Edit', 'Apply', 'Rollback', 'Forward', 'Retry', 'Delete']) {
    Expect(Screen.queryByRole('button', { name: Name })).toBeNull()
  }
  await User.click(Screen.getAllByRole('button', { name: 'View' })[0])
  Expect(await Screen.findByText('CREATE TABLE sample (id INT);')).toBeTruthy()
})

Test('viewer opens its migrations and switching database clears the editor', async () => {
  InstallMigrationAPI()
  Mount('/databases/db-1')
  const User = UserEvent.setup()
  await User.click(await Screen.findByRole('link', { name: 'Migrations' }))
  await Screen.findByText('First migration')
  const Apply = Screen.getAllByRole('button', { name: 'Apply' })
  Expect((Apply[0] as HTMLButtonElement).disabled).toBe(false)
  Expect((Apply[1] as HTMLButtonElement).disabled).toBe(true)
  await User.click(Screen.getAllByRole('button', { name: 'View' })[0])
  await User.click(Screen.getByRole('button', { name: '+ Create Migration' }))
  await User.type(Screen.getByLabelText(/Up SQL:/), 'CREATE TABLE old_database_only (id int);')
  await User.type(Screen.getByLabelText(/Down SQL:/), 'DROP TABLE old_database_only;')
  await User.selectOptions(Screen.getByLabelText('Database:'), 'db-2')
  await Screen.findByText('Analytics history')
  Expect(Screen.queryByLabelText('Up SQL:')).toBeNull()
})

Test('pending execution disables actions and explains recovery', async () => {
  InstallMigrationAPI('view_edit', 'pending')
  Mount()
  await Screen.findByText('First migration')
  for (const Button of Screen.queryAllByRole('button', { name: 'Apply' })) Expect((Button as HTMLButtonElement).disabled).toBe(true)
  Expect(Screen.getByRole('status').textContent).toContain('Verify the target outcome')
})

Test('unknown database routes do not load migration history', async () => {
  const Fetch = InstallMigrationAPI()
  Mount('/databases/no-grant/migrations')
  Expect((await Screen.findByRole('alert')).textContent).toBe('No access to this database.')
  Expect(Fetch.mock.calls.some(([Path]) => Path.includes('/no-grant/migrations'))).toBe(false)
})

Test('analyzes execution plan in editor and details view', async () => {
  const Fetch = Vi.fn(async (Path: string, Options?: RequestInit) => {
    if (Path === '/api/auth/me') return Response.json({ user: { id: 'u1', username: 'tester', is_platform_admin: false }, csrf_token: 'c'.repeat(43), expires_at: '2099-01-01T00:00:00Z' })
    if (Path === '/api/auth/csrf') return Response.json({ csrf_token: 'c'.repeat(43) })
    if (Path === '/api/databases') return Response.json(Databases)
    if (Path.startsWith('/api/databases/db-1/migrations/history')) return Response.json([])
    if (Path === '/api/databases/db-1/migrations') return Response.json([
      MigrationFixture({ id: 'first', title: 'First migration', sql_text: 'SELECT * FROM users;', status: 'draft', locked_at: undefined, can_apply: true, can_rollback: false }),
    ])
    if (Path.endsWith('/migrations/analyze') && Options?.method === 'POST') {
      return Response.json({
        scan_types: ['Seq Scan'],
        indexes_used: ['users_pkey'],
        join_strategies: [],
        estimated_cost: 14.5,
        actual_execution_time: 1.234,
        rows_processed: 42,
        planning_time: 0.123,
        raw_plan: [{ Plan: { 'Node Type': 'Seq Scan' } }],
      })
    }
    return new Response(null, { status: 404 })
  })
  Vi.stubGlobal('fetch', Fetch)

  Mount()
  const User = UserEvent.setup()
  await Screen.findByText('First migration')

  await User.click(Screen.getByRole('button', { name: 'View' }))
  await User.click(Screen.getByRole('button', { name: 'Analyze Plan (EXPLAIN)' }))
  Expect(await Screen.findByText('Execution Plan Analysis (EXPLAIN ANALYZE)')).toBeTruthy()
  Expect(Screen.getByText('Seq Scan')).toBeTruthy()
  Expect(Screen.getByText('users_pkey')).toBeTruthy()
  Expect(Screen.getByText('14.50')).toBeTruthy()
  await User.click(Screen.getByRole('button', { name: 'Close' }))

  await User.click(Screen.getByRole('button', { name: '+ Create Migration' }))
  await User.type(Screen.getByLabelText(/Up SQL:/), 'SELECT 1;')
  await User.type(Screen.getByLabelText(/Down SQL:/), 'SELECT 1;')
  await User.click(Screen.getByRole('button', { name: 'Analyze Plan (EXPLAIN)' }))
  Expect(await Screen.findByText('1.234 ms')).toBeTruthy()
})

Test('rolls back, forwards, and filters successful rollback pairs', async () => {
  let Definitions = [MigrationFixture({ status: 'applied', can_rollback: true })]
  const History = [ExecutionFixture()]
  const Calls: string[] = []
  Vi.stubGlobal('confirm', () => true)
  Vi.stubGlobal('fetch', Vi.fn(async (Path: string, Options?: RequestInit) => {
    Calls.push(Path)
    if (Path === '/api/databases') return Response.json(Databases)
    if (Path === '/api/auth/me') return Response.json({ user: { id: 'u1', username: 'tester', is_platform_admin: false }, csrf_token: 'c'.repeat(43), expires_at: '2099-01-01T00:00:00Z' })
    if (Path === '/api/auth/csrf') return Response.json({ csrf_token: 'c'.repeat(43) })
    if (Path.includes('/migrations/history')) return Response.json(Path.includes('hide_rollback=true') ? [] : History)
    if (Path === '/api/databases/db-1/migrations') return Response.json(Definitions)
    if (Path.endsWith('/rollback') && Options?.method === 'POST') {
      Definitions = [MigrationFixture({ status: 'rolled_back', can_rollback: false, can_forward: true })]
      return Response.json(ExecutionFixture({ id: 'exec-rollback', kind: 'rollback', direction: 'down', rollback_of_execution_id: 'exec-1' }))
    }
    if (Path.endsWith('/forward') && Options?.method === 'POST') {
      Definitions = [MigrationFixture({ status: 'applied', can_rollback: true })]
      return Response.json(ExecutionFixture({ id: 'exec-forward', kind: 'forward', direction: 'up' }))
    }
    return new Response(null, { status: 404 })
  }))
  Mount()
  const User = UserEvent.setup()
  await Screen.findByText('Initial Schema')
  await User.click(Screen.getByRole('button', { name: 'Rollback' }))
  Expect(await Screen.findByText(/Rollback v1 finished with status succeeded/)).toBeTruthy()
  await User.click(await Screen.findByRole('button', { name: 'Forward' }))
  Expect(await Screen.findByText(/Forward v1 finished with status succeeded/)).toBeTruthy()
  await User.click(Screen.getByRole('checkbox', { name: 'Hide rollback' }))
  await Screen.findByText('No execution attempts recorded.')
  Expect(Calls.some(Path => Path.includes('hide_rollback=true'))).toBe(true)
})
