import { expect as Expect, test as Test } from '@playwright/test'
import type { APIRequestContext, BrowserContext } from '@playwright/test'
import { randomBytes as RandomBytes, randomUUID as RandomUUID } from 'node:crypto'

interface RecordID { id: string }
interface Migration extends RecordID { status: string }

for (const Scenario of ['basic', 'enum'] as const) Test(`compares ${Scenario} schemas, reviews SQL, and verifies execution`, async ({ browser: Browser, request: AdminRequest, playwright: Playwright }, TestInfo) => {
  const Origin = process.env.APP_ORIGIN || 'http://127.0.0.1:5173'
  const Username = process.env.BOOTSTRAP_ADMIN_USERNAME
  const AdminPassword = process.env.BOOTSTRAP_ADMIN_PASSWORD
  const URLs = [process.env.TARGET_DATABASE_URL, process.env.TEST_DATABASE_URL]
  const LocalHosts = ['127.0.0.1', 'localhost', '[::1]']
  if (!Username || !AdminPassword || URLs.some(Value => !Value)) throw new Error('Schema diff fixtures need administrator credentials, TARGET_DATABASE_URL, and TEST_DATABASE_URL')
  if (process.env.APP_ENV === 'production' || !LocalHosts.includes(new URL(Origin).hostname)) throw new Error('Schema diff browser fixtures require a local development application')
  const Targets = URLs.map(Value => new URL(Value!))
  if (Targets.some(Target => !LocalHosts.includes(Target.hostname))) throw new Error('Schema diff fixture databases must be local')
  if (Targets[0].host === Targets[1].host && Targets[0].pathname === Targets[1].pathname) throw new Error('Schema diff fixtures require two different databases')
  const Prefix = `diff_${RandomUUID().replaceAll('-', '').slice(0, 12)}`
  const Schema = `${Prefix} 表`
  const Namespace = `"${Schema}"`
  const Table = `${Namespace}."order items"`
  const UnsupportedSchema = `${Schema} unsupported`
  const UnsupportedNamespace = `"${UnsupportedSchema}"`
  const Password = RandomBytes(24).toString('base64url')
  const Databases: RecordID[] = []
  let User: RecordID | undefined
  let Group: RecordID | undefined
  let Editor: APIRequestContext | undefined
  let Context: BrowserContext | undefined
  let AdminToken = ''
  let EditorToken = ''
  const Errors: string[] = []

  function ConnectionData(Index: number) {
    const Target = Targets[Index]
    return {
      name: `${Prefix}_${Index}`, host: Target.hostname, port: Number(Target.port || '5432'), dbname: decodeURIComponent(Target.pathname.slice(1)),
      username: decodeURIComponent(Target.username), password: decodeURIComponent(Target.password),
    }
  }

  async function AdminLogin() {
    const Response = await AdminRequest.post('/api/auth/login', { headers: { Origin }, data: { username: Username, password: AdminPassword } })
    if (!Response.ok()) throw new Error('Fixture administrator login failed; credentials withheld')
    AdminToken = (await Response.json() as { csrf_token: string }).csrf_token
  }

  async function AdminAPI<Value>(Path: string, Method = 'GET', Data?: unknown): Promise<Value> {
    const Send = () => AdminRequest.fetch(Path, { method: Method, headers: { Origin, 'X-CSRF-Token': AdminToken }, data: Data })
    let Response = await Send()
    if (Response.status() === 401) { await AdminLogin(); Response = await Send() }
    if (!Response.ok()) throw new Error(`Fixture administrator API returned ${Response.status()}; body withheld`)
    return Response.status() === 204 ? undefined as Value : await Response.json() as Value
  }

  async function EditorLogin() {
    await Editor?.dispose()
    Editor = await Playwright.request.newContext({ baseURL: Origin })
    const Response = await Editor.post('/api/auth/login', { headers: { Origin }, data: { username: Prefix, password: Password } })
    if (!Response.ok()) throw new Error('Fixture editor login failed; credentials withheld')
    EditorToken = (await Response.json() as { csrf_token: string }).csrf_token
  }

  async function EditorAPI<Value>(Path: string, Method = 'GET', Data?: unknown): Promise<Value> {
    if (!Editor) throw new Error('Fixture editor is unavailable')
    const Response = await Editor.fetch(Path, { method: Method, headers: { Origin, 'X-CSRF-Token': EditorToken }, data: Data })
    if (!Response.ok()) throw new Error(`Fixture editor API ${Method} ${Path} returned ${Response.status()}; body withheld`)
    return Response.status() === 204 ? undefined as Value : await Response.json() as Value
  }

  async function ExecuteSQL(Database: RecordID, SQL: string) {
    const Base = `/api/databases/${Database.id}/migrations`
    const Draft = await EditorAPI<RecordID>(Base, 'POST', { title: 'Schema diff fixture', sql_text: SQL })
    const Done = await EditorAPI<Migration>(`${Base}/${Draft.id}/execute`, 'POST')
    Expect(Done.status).toBe('committed')
  }

  async function CleanupFixtures() {
    await Context?.close()
    if (Group && User) {
      for (const Database of Databases) await AdminAPI(`/api/admin/groups/${Group.id}/access/${Database.id}`, 'PUT', { level: 'view_edit' })
      await EditorLogin()
      for (const Database of Databases) {
        const Base = `/api/databases/${Database.id}/migrations`
        const History = await EditorAPI<Migration[]>(Base)
        if (History.some(Item => Item.status === 'pending')) throw new Error(`Fixture ${Prefix} has an uncertain migration; preserve it for reconciliation`)
        for (const Draft of History.filter(Item => Item.status === 'draft')) await EditorAPI(`${Base}/${Draft.id}`, 'DELETE')
        await ExecuteSQL(Database, `DROP SCHEMA IF EXISTS ${Namespace} CASCADE; DROP SCHEMA IF EXISTS ${UnsupportedNamespace} CASCADE`)
      }
    }
    await Editor?.dispose()
    for (const Database of Databases) await AdminAPI(`/api/connections/${Database.id}`, 'DELETE')
    if (User) await AdminAPI(`/api/admin/users/${User.id}`, 'DELETE')
    if (Group) await AdminAPI(`/api/admin/groups/${Group.id}`, 'DELETE')
    await AdminAPI('/api/auth/logout', 'POST')
  }

  await AdminLogin()
  try {
    User = await AdminAPI<RecordID>('/api/admin/users', 'POST', { username: Prefix, password: Password })
    Group = await AdminAPI<RecordID>('/api/admin/groups', 'POST', { name: Prefix })
    await AdminAPI(`/api/admin/groups/${Group.id}/members/${User.id}`, 'PUT')
    for (const Index of Targets.keys()) {
      const Database = await AdminAPI<RecordID>('/api/connections', 'POST', ConnectionData(Index))
      Databases.push(Database)
      await AdminAPI(`/api/admin/groups/${Group.id}/access/${Database.id}`, 'PUT', { level: 'view_edit' })
    }
    await EditorLogin()
    const EnumCurrent = Scenario === 'enum' ? `CREATE TYPE ${Namespace}.state AS ENUM ('new'); ` : ''
    const EnumReference = Scenario === 'enum' ? `CREATE TYPE ${Namespace}.state AS ENUM ('new','done'); ` : ''
    const CurrentState = Scenario === 'enum' ? `,state ${Namespace}.state DEFAULT 'new'` : ''
    const ReferenceState = Scenario === 'enum' ? `,state ${Namespace}.state DEFAULT 'done'` : ''
    await ExecuteSQL(Databases[0], `CREATE SCHEMA ${Namespace}; ${EnumCurrent}CREATE TABLE ${Table} (id integer PRIMARY KEY, value text${CurrentState}); INSERT INTO ${Table} (id,value) VALUES (1, 'preserved')`)
    await ExecuteSQL(Databases[1], `CREATE SCHEMA ${Namespace}; ${EnumReference}CREATE TABLE ${Table} (id integer PRIMARY KEY, value text, enabled boolean NOT NULL DEFAULT true${ReferenceState}); CREATE INDEX value_index ON ${Table} (value)`)
    if (Scenario === 'basic') await ExecuteSQL(Databases[1], `CREATE SCHEMA ${UnsupportedNamespace}; CREATE DOMAIN ${UnsupportedNamespace}.custom_type AS text`)
    await AdminAPI(`/api/admin/groups/${Group.id}/access/${Databases[1].id}`, 'PUT', { level: 'view_only' })
    await EditorLogin()
    Context = await Browser.newContext({ baseURL: Origin, storageState: await Editor!.storageState() })
    const Page = await Context.newPage()
    Page.on('pageerror', Error => Errors.push(Error.message))
    Page.on('dialog', Dialog => void Dialog.accept())
    await Page.goto(`/databases/${Databases[0].id}`)
    await Page.getByRole('link', { name: 'Compare schemas', exact: true }).click()
    await Page.getByLabel('Reference database', { exact: true }).selectOption(Databases[1].id)
    await Page.getByLabel('Schema', { exact: true }).selectOption(Schema)
    await Page.getByRole('button', { name: 'Compare schemas', exact: true }).click()
    const Generated = Page.getByLabel('Generated SQL', { exact: true })
    await Expect(Generated).toBeVisible()
    const SQL = await Generated.inputValue()
    if (Scenario === 'enum') {
      await Expect(Page.getByText(/This migration only adds enum values/)).toBeVisible()
      Expect(SQL).toContain('ADD VALUE')
      Expect(SQL).not.toContain('SET DEFAULT')
    } else {
      Expect(SQL).toContain('ADD COLUMN')
      Expect(SQL).toContain('CREATE INDEX')
      await AdminAPI(`/api/connections/${Databases[1].id}`, 'PUT', { ...ConnectionData(1), port: 1 })
      try {
        await Page.getByRole('button', { name: 'Compare schemas', exact: true }).click()
        await Expect(Page.getByRole('alert')).toHaveText('Unable to compare the database schemas')
        await Expect(Generated).toHaveCount(0)
        await Expect(Page.getByRole('button', { name: 'Use SQL in migration editor' })).toHaveCount(0)
      } finally {
        await AdminAPI(`/api/connections/${Databases[1].id}`, 'PUT', ConnectionData(1))
      }
      await Page.getByRole('button', { name: 'Compare schemas', exact: true }).click()
      await Expect(Generated).toBeVisible()
    }
    Expect(SQL).not.toContain('CONCURRENTLY')
    const Columns = await EditorAPI<{ columns: { name: string }[] }>(`/api/databases/${Databases[0].id}/tables/order%20items/columns?schema=${encodeURIComponent(Schema)}`)
    Expect(Columns.columns.some(Column => Column.name === 'enabled')).toBe(false)
    await Page.screenshot({ path: TestInfo.outputPath('schema-diff.png'), fullPage: true })
    await Page.getByRole('button', { name: 'Use SQL in migration editor', exact: true }).click()
    const Input = Page.getByLabel('SQL Query (Transactional):', { exact: true })
    await Expect(Input).toHaveValue(SQL)
    const BeforeSave = await EditorAPI<Migration[]>(`/api/databases/${Databases[0].id}/migrations`)
    Expect(BeforeSave).toHaveLength(1)
    await Input.fill(`${SQL}\nINSERT INTO ${Table} (id, value) VALUES (2, 'reviewed');`)
    await Page.getByLabel('Title:', { exact: true }).fill('Reviewed schema migration')
    await Page.getByRole('button', { name: 'Save Draft', exact: true }).click()
    const Row = Page.getByRole('row').filter({ has: Page.getByRole('cell', { name: 'Reviewed schema migration', exact: true }) })
    await Expect(Row.getByRole('cell', { name: 'draft', exact: true })).toBeVisible()
    await Row.getByRole('button', { name: 'Execute', exact: true }).click()
    await Expect(Row.getByRole('cell', { name: 'committed', exact: true })).toBeVisible()
    await Page.getByRole('link', { name: 'Compare schemas', exact: true }).click()
    await Page.getByLabel('Reference database', { exact: true }).selectOption(Databases[1].id)
    await Page.getByLabel('Schema', { exact: true }).selectOption(Schema)
    await Page.getByRole('button', { name: 'Compare schemas', exact: true }).click()
    if (Scenario === 'enum') {
      await Expect(Generated).toBeVisible()
      const RemainingSQL = await Generated.inputValue()
      Expect(RemainingSQL).toContain('SET DEFAULT')
      Expect(RemainingSQL).not.toContain('ADD VALUE')
      await Expect(Page.getByText(/This migration only adds enum values/)).toHaveCount(0)
      await Page.getByRole('button', { name: 'Use SQL in migration editor', exact: true }).click()
      await Expect(Input).toHaveValue(RemainingSQL)
      await Page.getByLabel('Title:', { exact: true }).fill('Finish enum migration')
      await Page.getByRole('button', { name: 'Save Draft', exact: true }).click()
      const Final = Page.getByRole('row').filter({ has: Page.getByRole('cell', { name: 'Finish enum migration', exact: true }) })
      await Final.getByRole('button', { name: 'Execute', exact: true }).click()
      await Expect(Final.getByRole('cell', { name: 'committed', exact: true })).toBeVisible()
      await Page.getByRole('link', { name: 'Compare schemas', exact: true }).click()
      await Page.getByLabel('Reference database', { exact: true }).selectOption(Databases[1].id)
      await Page.getByLabel('Schema', { exact: true }).selectOption(Schema)
      await Page.getByRole('button', { name: 'Compare schemas', exact: true }).click()
    }
    await Expect(Page.getByText('No schema differences found.', { exact: true })).toBeVisible()
    const Rows = await EditorAPI<{ total: number }>(`/api/databases/${Databases[0].id}/tables/order%20items/rows?schema=${encodeURIComponent(Schema)}`)
    Expect(Rows.total).toBe(2)
    if (Scenario === 'basic') {
      await Page.getByLabel('Schema', { exact: true }).selectOption(UnsupportedSchema)
      await Page.getByRole('button', { name: 'Compare schemas', exact: true }).click()
      await Expect(Page.getByRole('alert')).toContainText('This schema change is not supported')
      await Expect(Generated).toHaveCount(0)
      await Page.getByLabel('Schema', { exact: true }).selectOption(Schema)
      await Page.getByRole('button', { name: 'Compare schemas', exact: true }).click()
      await Expect(Page.getByText('No schema differences found.', { exact: true })).toBeVisible()
      await AdminAPI(`/api/admin/groups/${Group.id}/access/${Databases[1].id}`, 'DELETE')
      await Page.reload()
      await Expect(Page.getByRole('heading', { name: 'Sign in', exact: true })).toBeVisible()
      await Page.getByLabel('Username', { exact: true }).fill(Prefix)
      await Page.getByLabel('Password', { exact: true }).fill(Password)
      await Page.getByRole('button', { name: 'Sign in', exact: true }).click()
      await Expect(Page.getByRole('heading', { name: 'Database Workspace', exact: true })).toBeVisible()
      await Page.goto(`/databases/${Databases[0].id}/schema-diff`)
      await Expect(Page.getByLabel('Reference database').locator(`option[value="${Databases[1].id}"]`)).toHaveCount(0)
      const Session = await Page.request.get('/api/auth/csrf')
      const Token = (await Session.json() as { csrf_token: string }).csrf_token
      const Denied = await Page.request.post(`/api/databases/${Databases[0].id}/schema-diff`, {
        headers: { Origin, 'X-CSRF-Token': Token }, data: { reference_database_id: Databases[1].id, schema: Schema },
      })
      Expect(Denied.status()).toBe(403)
    }
    Expect(Errors).toEqual([])
  } finally {
    await CleanupFixtures()
  }
})
