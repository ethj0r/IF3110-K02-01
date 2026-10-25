import { expect as Expect, test as Test } from '@playwright/test'
import type { APIRequestContext, BrowserContext, Page } from '@playwright/test'
import { randomBytes as RandomBytes, randomUUID as RandomUUID } from 'node:crypto'

interface RecordID { id: string }
interface HistoryItem extends RecordID { status: string }

Test('real browser enforces group access, commits SQL, preserves rollback, and revokes sessions', async ({ browser: Browser, request: AdminRequest, playwright: Playwright }, TestInfo) => {
  const Origin = process.env.APP_ORIGIN || 'http://127.0.0.1:5173'
  const AdminUsername = process.env.BOOTSTRAP_ADMIN_USERNAME
  const AdminPassword = process.env.BOOTSTRAP_ADMIN_PASSWORD
  const TargetURL = process.env.TARGET_DATABASE_URL
  if (!AdminUsername || !AdminPassword || !TargetURL) throw new Error('Run make test-e2e with administrator credentials and TARGET_DATABASE_URL configured')
  if (process.env.APP_ENV === 'production' || !['127.0.0.1', 'localhost', '[::1]'].includes(new URL(Origin).hostname)) throw new Error('Browser fixtures require a local development application')
  const Target = new URL(TargetURL)
  if (!['127.0.0.1', 'localhost', '[::1]'].includes(Target.hostname)) throw new Error('Browser fixture target must be a local PostgreSQL database')
  const Suffix = RandomUUID().replaceAll('-', '').slice(0, 12)
  const Prefix = `e2e_${Suffix}`
  const Schema = `browser ${Suffix} 表`
  const QuotedSchema = `"${Schema}"`
  const Table = `${QuotedSchema}."order items 表"`
  const FirstSQL = `CREATE SCHEMA ${QuotedSchema}; CREATE TABLE ${Table} (id integer PRIMARY KEY, value text NOT NULL); INSERT INTO ${Table} VALUES (1, 'browser value'); CREATE TABLE ${QuotedSchema}.".." (id integer PRIMARY KEY, value text); INSERT INTO ${QuotedSchema}.".." VALUES (1, 'dot value')`
  const Password = RandomBytes(24).toString('base64url')
  const Users: Record<string, RecordID & { username: string }> = {}
  const Groups: Record<string, RecordID> = {}
  const Contexts: BrowserContext[] = []
  const PageErrors: string[] = []
  let Database: RecordID | undefined
  let AdminToken = ''
  let CanCleanup = false
  let EditorRequest: APIRequestContext | undefined
  let EditorToken = ''

  async function SignInAdmin() {
    const Response = await AdminRequest.post('/api/auth/login', {
      headers: { Origin }, data: { username: AdminUsername, password: AdminPassword },
    })
    if (Response.status() !== 200) throw new Error('Administrator login failed; credentials withheld')
    AdminToken = (await Response.json() as { csrf_token: string }).csrf_token
  }

  async function AdminAPI<Value>(Path: string, Method = 'GET', Data?: unknown): Promise<Value> {
    const Send = () => AdminRequest.fetch(Path, { method: Method, headers: { Origin, 'X-CSRF-Token': AdminToken }, data: Data })
    let Response = await Send()
    if (Response.status() === 401) { await SignInAdmin(); Response = await Send() }
    if (!Response.ok()) throw new Error(`Fixture API ${Method} ${Path} returned ${Response.status()}; body withheld`)
    return Response.status() === 204 ? undefined as Value : await Response.json() as Value
  }

  async function SignInRole(Role: string) {
    const Context = await Browser.newContext({ baseURL: Origin, viewport: { width: 1366, height: 900 } })
    Contexts.push(Context)
    const Page = await Context.newPage()
    Page.on('pageerror', Error => PageErrors.push(Error.message))
    Page.on('dialog', Dialog => void Dialog.accept())
    await Page.goto('/login')
    await Page.getByLabel('Username', { exact: true }).fill(Users[Role].username)
    await Page.getByLabel('Password', { exact: true }).fill(Password)
    await Page.getByRole('button', { name: 'Sign in', exact: true }).click()
    await Expect(Page.getByRole('heading', { name: 'Database Workspace', exact: true })).toBeVisible()
    return Page
  }

  async function EditorAPI<Value>(Path: string, Method = 'GET', Data?: unknown): Promise<Value> {
    if (!EditorRequest) throw new Error('Fixture editor session not initialized')
    const Response = await EditorRequest.fetch(Path, { method: Method, headers: { Origin, 'X-CSRF-Token': EditorToken }, data: Data })
    if (!Response.ok()) throw new Error(`Editor fixture API ${Method} ${Path} returned ${Response.status()}; body withheld`)
    return Response.status() === 204 ? undefined as Value : await Response.json() as Value
  }

  async function SaveDraft(Page: Page, Title: string, SQL: string) {
    await Page.getByRole('button', { name: '+ Create Migration', exact: true }).click()
    await Page.getByLabel('Title:', { exact: true }).fill(Title)
    await Page.getByLabel('SQL Query (Transactional):', { exact: true }).fill(SQL)
    await Page.getByRole('button', { name: 'Save Draft', exact: true }).click()
    const Row = Page.getByRole('row').filter({ has: Page.getByRole('cell', { name: Title, exact: true }) })
    await Expect(Row).toBeVisible()
    return Row
  }

  async function ReadOnlyAPI(Request: APIRequestContext, Path: string, Method: string, Data?: unknown) {
    const Verification = await Request.get('/api/auth/csrf')
    const Token = (await Verification.json() as { csrf_token: string }).csrf_token
    return Request.fetch(Path, { method: Method, data: Data, headers: { Origin, 'X-CSRF-Token': Token } })
  }

  async function CleanupFixtures() {
    await Promise.all(Contexts.map(Context => Context.close()))
    if (Database) {
      const Base = `/api/databases/${Database.id}/migrations`
      const History = CanCleanup ? await EditorAPI<HistoryItem[]>(Base) : []
      if (History.some(Item => Item.status === 'pending')) throw new Error(`Fixture ${Prefix} has an uncertain outcome; preserve it for reconciliation`)
      for (const Item of History.filter(Item => Item.status === 'draft')) await EditorAPI(`${Base}/${Item.id}`, 'DELETE')
      if (History.some(Item => Item.status === 'committed')) {
        const Cleanup = await EditorAPI<RecordID>(Base, 'POST', { title: 'Browser fixture cleanup', sql_text: `DROP SCHEMA IF EXISTS ${QuotedSchema} CASCADE` })
        await EditorAPI(`${Base}/${Cleanup.id}/execute`, 'POST')
      }
      await AdminAPI(`/api/connections/${Database.id}`, 'DELETE')
    }
    for (const User of Object.values(Users)) await AdminAPI(`/api/admin/users/${User.id}`, 'DELETE')
    for (const Group of Object.values(Groups)) await AdminAPI(`/api/admin/groups/${Group.id}`, 'DELETE')
    await EditorRequest?.dispose()
    await SignInAdmin()
    await AdminAPI('/api/auth/logout', 'POST')
  }

  await SignInAdmin()
  try {
    Database = await AdminAPI<RecordID>('/api/connections', 'POST', {
      name: Prefix, host: Target.hostname, port: Number(Target.port || '5432'),
      dbname: decodeURIComponent(Target.pathname.slice(1)), username: decodeURIComponent(Target.username), password: decodeURIComponent(Target.password),
    })
    const AdminContext = await Browser.newContext({ baseURL: Origin, storageState: await AdminRequest.storageState() })
    Contexts.push(AdminContext)
    const AdminPage = await AdminContext.newPage()
    AdminPage.on('pageerror', Error => PageErrors.push(Error.message))
    await AdminPage.goto('/admin/users')
    for (const Role of ['viewer', 'editor', 'blocked']) {
      await AdminPage.getByRole('button', { name: 'New user', exact: true }).click()
      await AdminPage.getByLabel('Username', { exact: true }).fill(`${Prefix}_${Role}`)
      await AdminPage.getByLabel('Password', { exact: true }).fill(Password)
      const Created = AdminPage.waitForResponse(Response => Response.url().endsWith('/api/admin/users') && Response.request().method() === 'POST')
      await AdminPage.getByRole('button', { name: 'Create user', exact: true }).click()
      const Response = await Created
      Expect(Response.status()).toBe(201)
      Users[Role] = await Response.json() as RecordID & { username: string }
      await Expect(AdminPage.getByText(Users[Role].username, { exact: true })).toBeVisible()
    }
    for (const Role of ['viewer', 'editor']) {
      await AdminPage.goto('/admin/groups')
      await AdminPage.getByRole('button', { name: 'New group', exact: true }).click()
      await AdminPage.getByLabel('Name', { exact: true }).fill(`${Prefix}_${Role}`)
      const Created = AdminPage.waitForResponse(Response => Response.url().endsWith('/api/admin/groups') && Response.request().method() === 'POST')
      await AdminPage.getByRole('button', { name: 'Create group', exact: true }).click()
      const Response = await Created
      Expect(Response.status()).toBe(201)
      Groups[Role] = await Response.json() as RecordID
      const Row = AdminPage.getByRole('row').filter({ has: AdminPage.getByText(`${Prefix}_${Role}`, { exact: true }) })
      await Row.getByRole('button', { name: 'Manage', exact: true }).click()
      await AdminPage.getByRole('combobox', { name: 'Add user', exact: true }).selectOption(Users[Role].id)
      const Added = AdminPage.waitForResponse(Response => Response.url().endsWith(`/members/${Users[Role].id}`) && Response.request().method() === 'PUT')
      await AdminPage.getByRole('button', { name: 'Add member', exact: true }).click()
      Expect((await Added).status()).toBe(204)
    }
    await AdminPage.goto('/admin/access')
    for (const Role of ['viewer', 'editor']) {
      const Granted = AdminPage.waitForResponse(Response => Response.url().endsWith(`/access/${Database!.id}`) && Response.request().method() === 'PUT')
      await AdminPage.getByRole('combobox', { name: `${Prefix}_${Role} access to ${Prefix}`, exact: true }).selectOption(Role === 'editor' ? 'view_edit' : 'view_only')
      Expect((await Granted).status()).toBe(204)
    }
    EditorRequest = await Playwright.request.newContext({ baseURL: Origin })
    const EditorLogin = await EditorRequest.post('/api/auth/login', { headers: { Origin }, data: { username: Users.editor.username, password: Password } })
    if (EditorLogin.status() !== 200) throw new Error('Fixture editor login failed; credentials withheld')
    EditorToken = (await EditorLogin.json() as { csrf_token: string }).csrf_token
    CanCleanup = true
    const Base = `/api/databases/${Database.id}`
    const PageBase = `/databases/${Database.id}`
    const Editor = await SignInRole('editor')
    await Editor.getByRole('link', { name: Prefix, exact: true }).click()
    await Editor.getByRole('link', { name: 'Migrations', exact: true }).click()
    const First = await SaveDraft(Editor, 'Browser create draft', FirstSQL)
    await First.getByRole('button', { name: 'Edit', exact: true }).click()
    await Editor.getByLabel('Title:', { exact: true }).fill('Browser create committed')
    await Editor.getByRole('button', { name: 'Save Draft', exact: true }).click()
    const Committed = Editor.getByRole('row').filter({ has: Editor.getByRole('cell', { name: 'Browser create committed', exact: true }) })
    await Committed.getByRole('button', { name: 'Execute', exact: true }).click()
    await Expect(Committed.getByRole('cell', { name: 'committed', exact: true })).toBeVisible()
    await Editor.getByRole('link', { name: 'Browse database', exact: true }).click()
    await Editor.getByRole('combobox', { name: 'Schema', exact: true }).selectOption(Schema)
    await Editor.getByRole('combobox', { name: 'Table', exact: true }).selectOption('order items 表')
    await Expect(Editor.getByRole('cell', { name: 'browser value', exact: true })).toBeVisible()
    await Expect(Editor.getByRole('cell', { name: 'PRIMARY KEY', exact: true })).toBeVisible()
    await Editor.screenshot({ path: TestInfo.outputPath('database-viewer.png'), fullPage: true })
    await Editor.getByRole('combobox', { name: 'Table', exact: true }).selectOption('..')
    await Expect(Editor.getByRole('cell', { name: 'dot value', exact: true })).toBeVisible()

    const Viewer = await SignInRole('viewer')
    await Viewer.getByRole('link', { name: Prefix, exact: true }).click()
    await Viewer.getByRole('combobox', { name: 'Schema', exact: true }).selectOption(Schema)
    await Viewer.getByRole('combobox', { name: 'Table', exact: true }).selectOption('order items 表')
    await Expect(Viewer.getByRole('cell', { name: 'browser value', exact: true })).toBeVisible()
    await Viewer.getByRole('link', { name: 'Migrations', exact: true }).click()
    await Expect(Viewer.getByRole('button', { name: '+ Create Migration', exact: true })).toHaveCount(0)
    await Expect(Viewer.getByRole('button', { name: 'Execute', exact: true })).toHaveCount(0)
    await Viewer.getByRole('button', { name: 'View', exact: true }).click()
    await Expect(Viewer.getByText(FirstSQL, { exact: true })).toBeVisible()
    const Denied = await ReadOnlyAPI(Viewer.request, `${Base}/migrations`, 'POST', { title: 'Forbidden', sql_text: 'SELECT 1' })
    Expect(Denied.status()).toBe(403)

    const Blocked = await SignInRole('blocked')
    await Expect(Blocked.getByRole('heading', { name: 'No databases assigned', exact: true })).toBeVisible()
    await Blocked.goto(`${PageBase}/migrations`)
    await Expect(Blocked.getByRole('alert')).toHaveText('No access to this database.')
    Expect((await Blocked.request.get(`${Base}/migrations`)).status()).toBe(403)
    await Blocked.goto('/admin/users')
    await Expect(Blocked.getByRole('heading', { name: 'Database Workspace', exact: true })).toBeVisible()

    await Editor.goto(`${PageBase}/migrations`)
    const Broken = await SaveDraft(Editor, 'Browser rollback', `CREATE TABLE ${QuotedSchema}."must rollback" (id int); SELECT 1/0`)
    await Broken.getByRole('button', { name: 'Execute', exact: true }).click()
    await Expect(Broken.getByRole('cell', { name: 'rolled_back', exact: true })).toBeVisible()
    await Expect(Editor.getByRole('alert')).toContainText('Execution Error:')
    const StoredHistory = await EditorAPI<HistoryItem[]>(`${Base}/migrations`)
    Expect(StoredHistory.map(Item => Item.status)).toEqual(['committed', 'rolled_back'])
    const Tables = await EditorAPI<{ tables: { name: string }[] }>(`${Base}/tables?schema=${encodeURIComponent(Schema)}`)
    Expect(Tables.tables.map(Item => Item.name)).toEqual(['..', 'order items 表'])
    await Viewer.reload()
    await Expect(Viewer.getByRole('cell', { name: 'rolled_back', exact: true })).toBeVisible()
    const Revoked = AdminPage.waitForResponse(Response => Response.url().endsWith(`/access/${Database!.id}`) && Response.request().method() === 'DELETE')
    await AdminPage.getByRole('combobox', { name: `${Prefix}_viewer access to ${Prefix}`, exact: true }).selectOption('')
    Expect((await Revoked).status()).toBe(204)
    await Viewer.reload()
    await Expect(Viewer.getByRole('heading', { name: 'Sign in', exact: true })).toBeVisible()
    await Viewer.getByLabel('Username', { exact: true }).fill(Users.viewer.username)
    await Viewer.getByLabel('Password', { exact: true }).fill(Password)
    await Viewer.getByRole('button', { name: 'Sign in', exact: true }).click()
    await Expect(Viewer.getByRole('heading', { name: 'No databases assigned', exact: true })).toBeVisible()
    await Editor.goto('/')
    await Editor.getByRole('button', { name: 'Sign out', exact: true }).click()
    await Expect(Editor.getByRole('heading', { name: 'Sign in', exact: true })).toBeVisible()
    Expect(PageErrors).toEqual([])
  } finally {
    await CleanupFixtures()
  }
})
