import Test from 'node:test'
import Assert from 'node:assert/strict'
import {
  AnalyzeQueryPlan,
  CreateMigration,
  DeleteMigrationDraft,
  ExecuteMigration,
  GetMigration,
  ListMigrations,
  UpdateMigrationDraft,
} from '../src/api/client.ts'

function InstallFetch(Context, Callback) {
  const Original = globalThis.fetch
  globalThis.fetch = Callback
  Context.after(() => {
    globalThis.fetch = Original
  })
}

function JSONResponse(Value, Status = 200) {
  return new Response(JSON.stringify(Value), {
    status: Status,
    headers: { 'Content-Type': 'application/json' },
  })
}

Test('migration client calls correct REST endpoints and attaches CSRF token', async Context => {
  const Calls = []
  InstallFetch(Context, async (Path, Options) => {
    Calls.push({ Path, Options })
    if (Path.endsWith('/csrf')) return JSONResponse({ csrf_token: 'mig-csrf' })
    if (Options?.method === 'DELETE') return new Response(null, { status: 204 })
    return JSONResponse({ id: 'm-1', database_id: 'db-test', version: 1, title: 'Init', sql_text: 'SELECT 1;', status: 'draft' })
  })

  // List
  await ListMigrations('db-test')
  Assert.equal(Calls[0].Path, '/api/databases/db-test/migrations')
  Assert.equal(Calls[0].Options.method, 'GET')

  // Create
  await CreateMigration('db-test', { title: 'Create T', sql_text: 'CREATE TABLE t (id INT);' })
  Assert.equal(Calls[1].Path, '/api/auth/csrf')
  Assert.equal(Calls[2].Path, '/api/databases/db-test/migrations')
  Assert.equal(Calls[2].Options.method, 'POST')
  Assert.equal(Calls[2].Options.headers.get('X-CSRF-Token'), 'mig-csrf')
  Assert.deepEqual(JSON.parse(Calls[2].Options.body), { title: 'Create T', sql_text: 'CREATE TABLE t (id INT);' })

  // Get
  await GetMigration('db-test', 'm-1')
  Assert.equal(Calls[3].Path, '/api/databases/db-test/migrations/m-1')
  Assert.equal(Calls[3].Options.method, 'GET')

  // Update
  await UpdateMigrationDraft('db-test', 'm-1', { title: 'Create T Updated', sql_text: 'CREATE TABLE t (id BIGINT);' })
  Assert.equal(Calls[4].Path, '/api/databases/db-test/migrations/m-1')
  Assert.equal(Calls[4].Options.method, 'PUT')

  // Execute
  await ExecuteMigration('db-test', 'm-1')
  Assert.equal(Calls[5].Path, '/api/databases/db-test/migrations/m-1/execute')
  Assert.equal(Calls[5].Options.method, 'POST')

  // Delete
  await DeleteMigrationDraft('db-test', 'm-1')
  Assert.equal(Calls[6].Path, '/api/databases/db-test/migrations/m-1')
  Assert.equal(Calls[6].Options.method, 'DELETE')

  // Analyze
  await AnalyzeQueryPlan('db-test', 'SELECT 1;')
  Assert.equal(Calls[7].Path, '/api/databases/db-test/migrations/analyze')
  Assert.equal(Calls[7].Options.method, 'POST')
  Assert.deepEqual(JSON.parse(Calls[7].Options.body), { sql_text: 'SELECT 1;' })
})

