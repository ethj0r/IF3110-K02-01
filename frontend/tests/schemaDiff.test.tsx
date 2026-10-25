import { afterEach as AfterEach, expect as Expect, test as Test, vi as Vi } from 'vitest'
import { cleanup as Cleanup, render as Render, screen as Screen } from '@testing-library/react'
import UserEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { App } from '../src/App'
import { ReadGeneratedMigration } from '../src/api/schemaDiff'

const Clients: QueryClient[] = []
const SQL = 'ALTER TABLE "public"."items" ADD COLUMN "enabled" boolean;'

AfterEach(() => {
  Cleanup()
  Clients.splice(0).forEach(Client => Client.clear())
  Vi.unstubAllGlobals()
})

function Mount(Path = '/databases/current/schema-diff', State?: unknown) {
  const Client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity } } })
  Clients.push(Client)
  Render(<QueryClientProvider client={Client}><MemoryRouter initialEntries={[{ pathname: Path, state: State }]}><App /></MemoryRouter></QueryClientProvider>)
}

function InstallAPI(Level = 'view_edit', Changes = true, Fail = false, Phased = false) {
  const Fetch = Vi.fn(async (Path: string, Options?: RequestInit) => {
    if (Path === '/api/auth/me') return Response.json({ user: { id: 'u1', username: 'tester', is_platform_admin: false }, csrf_token: 'c'.repeat(43), expires_at: '2099-01-01T00:00:00Z' })
    if (Path === '/api/databases') return Response.json([
      { id: 'current', name: 'Current', level: Level }, { id: 'reference', name: 'Reference', level: 'view_only' },
    ])
    if (Path.endsWith('/schemas')) return Response.json({ schemas: ['public', 'quoted 表'] })
    if (Path.endsWith('/schema-diff')) {
      if (Fail) return Response.json({ error: { code: 'schema_diff_unavailable', message: 'Unable to compare the database schemas' } }, { status: 502 })
      return Response.json({ current_database_id: 'current', reference_database_id: 'reference', schema: 'public', current_schema_hash: 'hash', requires_recomparison: Phased, statements: Changes ? [...(Phased ? [{ sql: `ALTER TYPE public.state ADD VALUE 'done';`, hazards: [] }] : []), { sql: SQL, hazards: [{ type: 'ACQUIRES_ACCESS_EXCLUSIVE_LOCK', message: 'Locks the table' }] }] : [], sql_text: Changes ? Phased ? `ALTER TYPE public.state ADD VALUE 'done';` : SQL : '' })
    }
    if (Path.endsWith('/migrations') && Options?.method === 'POST') {
      return Response.json({ ...JSON.parse(String(Options.body)) as object, id: 'draft', version: 1, status: 'draft', created_at: '2026-01-01T00:00:00Z' }, { status: 201 })
    }
    if (Path.endsWith('/migrations')) return Response.json([])
    return new Response(null, { status: 404 })
  })
  Vi.stubGlobal('fetch', Fetch)
  return Fetch
}

async function Compare() {
  const User = UserEvent.setup()
  await User.selectOptions(await Screen.findByLabelText('Reference database'), 'reference')
  await Screen.findByRole('option', { name: 'public' })
  await User.click(Screen.getByRole('button', { name: 'Compare schemas' }))
  return User
}

Test('reviews differences and imports editable SQL without saving or executing automatically', async () => {
  const Fetch = InstallAPI()
  Mount()
  const User = await Compare()
  await Screen.findByText(/Locks the table/)
  Expect(Fetch.mock.calls.find(([Path]) => Path.endsWith('/schema-diff'))?.[1]?.body).toBe(JSON.stringify({ reference_database_id: 'reference', schema: 'public' }))
  Expect((Screen.getByLabelText('Generated SQL') as HTMLTextAreaElement).readOnly).toBe(true)
  await User.click(Screen.getByRole('button', { name: 'Use SQL in migration editor' }))
  const Editor = await Screen.findByLabelText('SQL Query (Transactional):')
  Expect((Editor as HTMLTextAreaElement).value).toBe(SQL)
  Expect((Screen.getByLabelText('Title:') as HTMLInputElement).value).toBe('Sync schema public')
  Expect(Fetch.mock.calls.some(([Path, Options]) => Path.endsWith('/execute') || Path.endsWith('/migrations') && Options?.method === 'POST')).toBe(false)
  await User.clear(Editor)
  await User.type(Editor, 'SELECT 42;')
  await User.click(Screen.getByRole('button', { name: 'Save Draft' }))
  await Screen.findByText('Migration draft saved successfully.')
  const Saved = Fetch.mock.calls.find(([Path, Options]) => Path.endsWith('/migrations') && Options?.method === 'POST')
  Expect(JSON.parse(String(Saved?.[1]?.body))).toEqual({ title: 'Sync schema public', sql_text: 'SELECT 42;' })
  Expect(Fetch.mock.calls.some(([Path]) => Path.endsWith('/execute'))).toBe(false)
})

Test('clears stale comparison results when the chosen schema changes', async () => {
  InstallAPI()
  Mount()
  const User = await Compare()
  await Screen.findByLabelText('Generated SQL')
  await User.selectOptions(Screen.getByLabelText('Schema'), 'quoted 表')
  Expect(Screen.queryByLabelText('Generated SQL')).toBeNull()
  Expect(Screen.queryByRole('button', { name: 'Use SQL in migration editor' })).toBeNull()
})

Test('reports equal schemas without offering an empty migration', async () => {
  InstallAPI('view_edit', false)
  Mount()
  await Compare()
  await Screen.findByText('No schema differences found.')
  Expect(Screen.queryByRole('button', { name: 'Use SQL in migration editor' })).toBeNull()
})

Test('surfaces a generation error without importing SQL', async () => {
  InstallAPI('view_edit', true, true)
  Mount()
  await Compare()
  await Screen.findByText('Unable to compare the database schemas')
  Expect(Screen.queryByRole('button', { name: 'Use SQL in migration editor' })).toBeNull()
})

Test('view-only users cannot generate a migration or query reference schemas', async () => {
  const Fetch = InstallAPI('view_only')
  Mount()
  await Screen.findByText('View and edit access is required to generate a migration.')
  Expect(Screen.queryByLabelText('Reference database')).toBeNull()
  Expect(Fetch.mock.calls.some(([Path]) => Path.endsWith('/schemas') || Path.endsWith('/schema-diff'))).toBe(false)
})

Test('rejects malformed or different database migration route state', () => {
  for (const State of [null, {}, { GeneratedMigration: null }, { GeneratedMigration: { DatabaseID: 'other', Title: 'x', SQLText: SQL } }, { GeneratedMigration: { DatabaseID: 'current', Title: 7, SQLText: SQL } }, { GeneratedMigration: { DatabaseID: 'current', Title: 'x', SQLText: ' ' } }]) {
    Expect(ReadGeneratedMigration(State, 'current')).toBeUndefined()
  }
})

Test('explains staged enum generation and imports only the first transactional phase', async () => {
  InstallAPI('view_edit', true, false, true)
  Mount()
  const User = await Compare()
  await Screen.findByText(/This migration only adds enum values/)
  Expect((Screen.getByLabelText('Generated SQL') as HTMLTextAreaElement).value).toBe("ALTER TYPE public.state ADD VALUE 'done';")
  await User.click(Screen.getByRole('button', { name: 'Use SQL in migration editor' }))
  Expect((await Screen.findByLabelText('SQL Query (Transactional):') as HTMLTextAreaElement).value).not.toContain('ALTER TABLE')
  Expect((Screen.getByLabelText('Title:') as HTMLInputElement).value).toBe('Prepare enums in public')
})

Test('removes an earlier candidate after a failed comparison and can recover', async () => {
  const Fetch = InstallAPI()
  const Original = Fetch.getMockImplementation()!
  let Attempts = 0
  Fetch.mockImplementation(async (Path: string, Options?: RequestInit) => {
    if (Path.endsWith('/schema-diff') && ++Attempts === 2) return Response.json({ error: { code: 'schema_diff_unavailable', message: 'Unable to compare the database schemas' } }, { status: 502 })
    return Original(Path, Options)
  })
  Mount()
  const User = await Compare()
  await Screen.findByLabelText('Generated SQL')
  await User.click(Screen.getByRole('button', { name: 'Compare schemas' }))
  await Screen.findByText('Unable to compare the database schemas')
  Expect(Screen.queryByLabelText('Generated SQL')).toBeNull()
  Expect(Screen.queryByRole('button', { name: 'Use SQL in migration editor' })).toBeNull()
  await User.click(Screen.getByRole('button', { name: 'Compare schemas' }))
  await Screen.findByLabelText('Generated SQL')
})

Test('blocks generation when reference schemas cannot be loaded and recovers after changing reference', async () => {
  const Fetch = InstallAPI()
  const Original = Fetch.getMockImplementation()!
  Fetch.mockImplementation(async (Path: string, Options?: RequestInit) => {
    if (Path === '/api/databases/reference/schemas') return Response.json({ error: { code: 'target_unavailable', message: 'Reference database unavailable' } }, { status: 502 })
    return Original(Path, Options)
  })
  Mount()
  const User = UserEvent.setup()
  await User.selectOptions(await Screen.findByLabelText('Reference database'), 'reference')
  await Screen.findByText('Reference database unavailable')
  Expect((Screen.getByRole('button', { name: 'Compare schemas' }) as HTMLButtonElement).disabled).toBe(true)
  Expect(Fetch.mock.calls.some(([Path]) => Path.endsWith('/schema-diff'))).toBe(false)
  await User.selectOptions(Screen.getByLabelText('Reference database'), 'current')
  await Screen.findByRole('option', { name: 'public' })
  Expect((Screen.getByRole('button', { name: 'Compare schemas' }) as HTMLButtonElement).disabled).toBe(false)
})
