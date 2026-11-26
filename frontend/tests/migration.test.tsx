import { afterEach as AfterEach, expect as Expect, test as Test, vi as Vi } from 'vitest'
import { cleanup as Cleanup, render as Render, screen as Screen, waitFor as WaitFor, within as Within } from '@testing-library/react'
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
    batch_mode: false,
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
    mode: 'normal',
    committed_batches: 0,
    committed_rows: 0,
    reverted_batches: 0,
    reverted_rows: 0,
    checkpoint_cursor: 0,
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

function ReviewFixture(Action = 'apply', Approved = true) {
  return {
    id: 'review-1', action: Action, revision: 1, sql_text: 'SELECT 1;',
    status: Approved ? 'approved' : 'pending_review',
    groups: [{ id: 'group-1', name: 'Database team', leader_user_id: 'u1', approved: Approved, can_approve: !Approved }],
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
  const Details = Screen.getByRole('region', { name: 'Migration details' })
  Expect(Details.textContent).toContain('applied')
  Expect(Details.textContent).toContain('Revision')
})

function JobFixture(Overrides: Record<string, unknown> = {}) {
  return {
    id: 'job-1',
    database_id: 'db-1',
    migration_id: 'mig-1',
    version: 1,
    title: 'Initial Schema',
    action: 'apply',
    status: 'queued',
    attempts: 0,
    max_attempts: 3,
    available_at: '2026-01-01T00:00:00Z',
    committed_batches: 0,
    committed_rows: 0,
    reverted_batches: 0,
    reverted_rows: 0,
    checkpoint_cursor: 0,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...Overrides,
  }
}

const Finished = { status: 'succeeded', attempts: 1, execution_status: 'succeeded', finished_at: '2026-01-01T00:00:01Z' }

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

  let ReviewRequested = false
  let ReviewApproved = false
  let Jobs: unknown[] = []

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
      if (Path.endsWith('/review-1/approvals/group-1') && Options?.method === 'PUT') {
        ReviewApproved = true
        return new Response(null, { status: 204 })
      }
      if (Path.endsWith('/reviews/apply')) {
        ReviewRequested ||= Options?.method === 'POST'
        if (ReviewRequested) return Response.json(ReviewFixture('apply', ReviewApproved))
        return Response.json({ error: { code: 'review_not_found', message: 'Current migration review not found' } }, { status: 404 })
      }
      if (Path.startsWith('/api/databases/db-1/migrations/history')) return Response.json([])
      if (Path === '/api/databases/db-1/migrations' && (!Options || Options.method === 'GET')) return Response.json(Migrations)
      if (Path === '/api/databases/db-1/migrations' && Options?.method === 'POST') {
        const Body = JSON.parse(String(Options.body)) as { title: string; sql_text: string; down_sql: string }
        const Created = MigrationFixture({ id: 'mig-2', version: 2, title: Body.title, sql_text: Body.sql_text, down_sql: Body.down_sql, status: 'draft', locked_at: undefined, can_apply: false, can_rollback: false })
        Migrations = [...Migrations, Created]
        return Response.json(Created, { status: 201 })
      }
      if (Path === '/api/databases/db-1/migrations/mig-1/apply') {
        Jobs = [JobFixture({ title: 'Draft Migration', execution_id: 'exec-1', ...Finished })]
        return Response.json(JobFixture({ title: 'Draft Migration' }), { status: 202 })
      }
      if (Path === '/api/databases/db-1/jobs') return Response.json(Jobs)
      return new Response(null, { status: 404 })
    }),
  )

  const { Client } = Mount()
  const SchemaKey = ['databases', 'db-1', 'schemas'] as const
  Client.setQueryData(SchemaKey, { schemas: ['public'] })
  const User = UserEvent.setup()

  await Screen.findByRole('heading', { name: 'Database Migrations' })
  Expect(await Screen.findByText('Draft Migration')).toBeTruthy()

  await User.click(Screen.getByRole('button', { name: 'New migration' }))
  await User.type(Screen.getByLabelText('Title'), 'Second Migration')
  await User.type(Screen.getByLabelText('Up SQL'), 'CREATE TABLE t2 (id INT);')
  await User.type(Screen.getByLabelText('Down SQL'), 'DROP TABLE t2;')
  await User.click(Screen.getByRole('button', { name: 'Save Draft' }))
  Expect(await Screen.findByText('Migration draft saved successfully.')).toBeTruthy()

  await User.click(Screen.getAllByRole('button', { name: /^Review apply$/ })[0])
  const RequestReview = await Screen.findByRole('button', { name: 'Request review' })
  const Apply = Screen.getByRole<HTMLButtonElement>('button', { name: /^Apply$/ })
  Expect(Apply.disabled).toBe(true)
  await User.click(RequestReview)
  const Approve = await Screen.findByRole('button', { name: 'Approve for Database team' })
  Expect(Apply.disabled).toBe(true)
  await User.click(Approve)
  await Screen.findByText(/All groups approved/)
  await User.click(Apply)
  await User.click(await Screen.findByRole('button', { name: 'Run apply' }))
  Expect(await Screen.findByRole('heading', { name: /Apply job/ })).toBeTruthy()
  Expect(await Screen.findByText('Apply job succeeded', {}, { timeout: 3000 })).toBeTruthy()
  Expect(Screen.getByRole('heading', { name: 'Jobs' })).toBeTruthy()
  await WaitFor(() => Expect(Client.getQueryState(SchemaKey)?.isInvalidated).toBe(true))
})

function InstallMigrationAPI(Level = 'view_edit', Status = 'draft') {
  const Fetch = Vi.fn(async (Path: string) => {
    if (Path === '/api/auth/me') return Response.json({ user: { id: 'u1', username: 'tester', is_platform_admin: false }, csrf_token: 'c'.repeat(43), expires_at: '2099-01-01T00:00:00Z' })
    if (Path === '/api/databases') return Response.json([...Databases.map(Item => ({ ...Item, level: Level })), { ...Databases[0], id: 'db-2', name: 'Analytics' }])
    if (Path.includes('/migrations/history')) return Response.json(Status === 'pending' ? [ExecutionFixture({ status: 'running', finished_at: undefined })] : [])
    if (Path.endsWith('/jobs')) return Response.json([])
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
  for (const Name of ['New migration', 'Edit', 'Apply', 'Rollback', 'Forward', 'Retry', 'Delete']) {
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
  const Apply = Screen.getAllByRole('button', { name: /^Review apply$/ })
  Expect((Apply[0] as HTMLButtonElement).disabled).toBe(false)
  Expect((Apply[1] as HTMLButtonElement).disabled).toBe(true)
  await User.click(Screen.getAllByRole('button', { name: 'View' })[0])
  await User.click(Screen.getByRole('button', { name: 'New migration' }))
  await User.type(Screen.getByLabelText('Up SQL'), 'CREATE TABLE old_database_only (id int);')
  await User.type(Screen.getByLabelText('Down SQL'), 'DROP TABLE old_database_only;')
  await User.selectOptions(Screen.getByLabelText('Database'), 'db-2')
  await Screen.findByText('Analytics history')
  Expect(Screen.queryByLabelText('Up SQL')).toBeNull()
})

Test('pending execution disables actions and explains recovery', async () => {
  InstallMigrationAPI('view_edit', 'pending')
  Mount()
  await Screen.findByText('First migration')
  const Apply = Screen.getByRole<HTMLButtonElement>('button', { name: /^Review apply$/ })
  Expect(Apply.disabled).toBe(true)
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

  await User.click(Screen.getByRole('button', { name: 'New migration' }))
  await User.type(Screen.getByLabelText('Up SQL'), 'SELECT 1;')
  await User.type(Screen.getByLabelText('Down SQL'), 'SELECT 1;')
  await User.click(Screen.getByRole('button', { name: 'Analyze Plan (EXPLAIN)' }))
  Expect(await Screen.findByText('1.234 ms')).toBeTruthy()
})

Test('rolls back, forwards, and filters successful rollback pairs', async () => {
  let Definitions = [MigrationFixture({ status: 'applied', can_rollback: true })]
  const History = [ExecutionFixture()]
  const Calls: string[] = []
  let Jobs: unknown[] = []
  Vi.stubGlobal('fetch', Vi.fn(async (Path: string, Options?: RequestInit) => {
    Calls.push(Path)
    if (Path === '/api/databases') return Response.json(Databases)
    if (Path === '/api/auth/me') return Response.json({ user: { id: 'u1', username: 'tester', is_platform_admin: false }, csrf_token: 'c'.repeat(43), expires_at: '2099-01-01T00:00:00Z' })
    if (Path === '/api/auth/csrf') return Response.json({ csrf_token: 'c'.repeat(43) })
    if (Path.includes('/migrations/history')) return Response.json(Path.includes('hide_rollback=true') ? [] : History)
    if (Path === '/api/databases/db-1/migrations') return Response.json(Definitions)
    if (Path.endsWith('/reviews/rollback')) return Response.json(ReviewFixture('rollback'))
    if (Path.endsWith('/reviews/forward')) return Response.json(ReviewFixture('forward'))
    if (Path.endsWith('/rollback') && Options?.method === 'POST') {
      Definitions = [MigrationFixture({ status: 'rolled_back', can_rollback: false, can_forward: true })]
      Jobs = [JobFixture({ id: 'job-rollback', action: 'rollback', execution_id: 'exec-rollback', ...Finished }), ...Jobs]
      return Response.json(JobFixture({ id: 'job-rollback', action: 'rollback' }), { status: 202 })
    }
    if (Path.endsWith('/forward') && Options?.method === 'POST') {
      Definitions = [MigrationFixture({ status: 'applied', can_rollback: true })]
      Jobs = [JobFixture({ id: 'job-forward', action: 'forward', execution_id: 'exec-forward', ...Finished }), ...Jobs]
      return Response.json(JobFixture({ id: 'job-forward', action: 'forward' }), { status: 202 })
    }
    if (Path === '/api/databases/db-1/jobs') return Response.json(Jobs)
    return new Response(null, { status: 404 })
  }))
  Mount()
  const User = UserEvent.setup()
  await Screen.findByText('Initial Schema')
  await User.click(Screen.getByRole('button', { name: /^Review rollback$/ }))
  await Screen.findByText(/All groups approved/)
  await User.click(Screen.getByRole('button', { name: /^Rollback$/ }))
  await User.click(await Screen.findByRole('button', { name: 'Run rollback' }))
  Expect(await Screen.findByText('Rollback job succeeded', {}, { timeout: 3000 })).toBeTruthy()
  await User.click(await Screen.findByRole('button', { name: /^Review forward$/ }))
  await Screen.findByText(/All groups approved/)
  await User.click(Screen.getByRole('button', { name: /^Forward$/ }))
  await User.click(await Screen.findByRole('button', { name: 'Run forward' }))
  Expect(await Screen.findByText('Forward job succeeded', {}, { timeout: 3000 })).toBeTruthy()
  Expect(Screen.getAllByRole('button', { name: 'Inspect' }).length).toBeGreaterThanOrEqual(2)
  await User.click(Screen.getByRole('checkbox', { name: 'Hide rollback' }))
  await Screen.findByText('No execution attempts recorded.')
  Expect(Calls.some(Path => Path.includes('hide_rollback=true'))).toBe(true)
})

Test('keeps the active job visible across navigation and shows partial batch progress', async () => {
  let Jobs: Record<string, unknown>[] = [JobFixture({
    id: 'job-batch',
    title: 'Backfill views',
    status: 'running',
    attempts: 1,
    locked_by: 'worker-a',
    locked_at: '2026-01-01T00:00:01Z',
    requested_by_username: 'operator',
    execution_id: 'exec-batch-1',
    execution_status: 'running',
    mode: 'batch',
    committed_batches: 3,
    committed_rows: 3000,
    checkpoint_cursor: 3000,
  })]
  const Calls: string[] = []
  Vi.stubGlobal('fetch', Vi.fn(async (Path: string) => {
    Calls.push(Path)
    if (Path === '/api/auth/me') return Response.json({ user: { id: 'u1', username: 'tester', is_platform_admin: false }, csrf_token: 'c'.repeat(43), expires_at: '2099-01-01T00:00:00Z' })
    if (Path === '/api/databases') return Response.json(Databases)
    if (Path === '/api/databases/db-1/jobs') return Response.json(Jobs)
    if (Path.includes('/migrations/history')) return Response.json([])
    if (Path === '/api/databases/db-1/migrations') return Response.json([MigrationFixture({ title: 'Backfill views', status: 'pending', can_rollback: false })])
    if (Path.endsWith('/schemas')) return Response.json({ schemas: ['public'] })
    if (Path.includes('/tables')) return Response.json({ tables: [] })
    return new Response(null, { status: 404 })
  }))
  Mount()
  const User = UserEvent.setup()

  const Panel = await Screen.findByRole('region', { name: 'Apply job' })
  Expect(Within(Panel).getByRole('progressbar')).toBeTruthy()
  Expect(Within(Panel).getAllByText('3,000')).toHaveLength(2)
  Expect(Within(Panel).getByText('worker-a')).toBeTruthy()
  Expect((Screen.getByRole('button', { name: 'New migration' }) as HTMLButtonElement).disabled).toBe(true)

  await User.click(Screen.getByRole('link', { name: 'Browse database' }))
  const MigrationsTab = await Screen.findByRole('link', { name: 'Migrations' })
  Expect(MigrationsTab.getAttribute('aria-describedby')).toBeTruthy()
  Expect(Screen.getByText('Apply job running')).toBeTruthy()

  await User.click(MigrationsTab)
  Expect(await Screen.findByRole('region', { name: 'Apply job' })).toBeTruthy()

  await User.click(Screen.getByRole('link', { name: 'Browse database' }))
  const SchemaCalls = Calls.filter(Path => Path.endsWith('/schemas')).length
  Jobs = [{ ...Jobs[0], status: 'failed', execution_status: 'partial', last_error: 'target SQL execution failed', finished_at: '2026-01-01T00:00:09Z' }]
  await WaitFor(() => Expect(Screen.getByRole('link', { name: 'Migrations' }).getAttribute('aria-describedby')).toBeNull(), { timeout: 3000 })
  await WaitFor(() => Expect(Calls.filter(Path => Path.endsWith('/schemas')).length).toBeGreaterThan(SchemaCalls))

  await User.click(Screen.getByRole('link', { name: 'Migrations' }))
  const Row = (await Screen.findByRole('heading', { name: 'Jobs' })).closest('section')!
  Expect(Within(Row).getByText('partial')).toBeTruthy()
  Expect(Screen.queryByRole('region', { name: 'Apply job' })).toBeNull()
  await User.click(Within(Row).getByRole('button', { name: 'Inspect' }))
  const Failed = await Screen.findByRole('region', { name: 'Apply job' })
  Expect(Within(Failed).getByRole('alert').textContent).toBe('target SQL execution failed')
  Expect(Within(Failed).queryByRole('progressbar')).toBeNull()
  await User.click(Within(Failed).getByRole('button', { name: 'Close' }))
  Expect(Screen.queryByRole('region', { name: 'Apply job' })).toBeNull()
})

Test('administrators can reconcile an uncertain execution', async () => {
  const Running = ExecutionFixture({ status: 'running', finished_at: undefined })
  const Reconciled = ExecutionFixture({ status: 'failed', finished_at: '2026-01-01T00:10:00Z', reconciled_at: '2026-01-01T00:10:00Z', reconcile_note: 'verified rolled back' })
  Vi.stubGlobal('fetch', Vi.fn(async (Path: string, Options?: RequestInit) => {
    if (Path === '/api/auth/me') return Response.json({ user: { id: 'admin-1', username: 'admin', is_platform_admin: true }, csrf_token: 'c'.repeat(43), expires_at: '2099-01-01T00:00:00Z' })
    if (Path === '/api/auth/csrf') return Response.json({ csrf_token: 'c'.repeat(43) })
    if (Path === '/api/databases') return Response.json(Databases)
    if (Path === '/api/databases/db-1/jobs') return Response.json([])
    if (Path.includes('/migrations/history')) return Response.json([Running])
    if (Path === '/api/databases/db-1/migrations') return Response.json([MigrationFixture()])
    if (Path.endsWith('/executions/exec-1/evidence')) return Response.json([])
    if (Path.endsWith('/executions/exec-1/reconcile') && Options?.method === 'POST') return Response.json(Reconciled)
    return new Response(null, { status: 404 })
  }))
  Mount()
  const User = UserEvent.setup()
  await Screen.findByText('Initial Schema')
  await User.click(Screen.getByRole('button', { name: 'Inspect' }))
  const Reconcile = await Screen.findByRole('button', { name: 'Reconcile execution' })
  await User.selectOptions(Screen.getByLabelText('Verified outcome'), 'failed')
  await User.type(Screen.getByLabelText('Evidence note'), 'verified rolled back')
  await User.click(Reconcile)
  Expect(await Screen.findByText('Execution reconciled. History refreshed.')).toBeTruthy()
})
