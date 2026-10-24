import { afterEach as AfterEach, expect as Expect, test as Test, vi as Vi } from 'vitest'
import { cleanup as Cleanup, render as Render, screen as Screen } from '@testing-library/react'
import UserEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { App } from '../src/App'

const Clients: QueryClient[] = []

AfterEach(() => {
  Cleanup()
  Clients.splice(0).forEach(Client => Client.clear())
  Vi.unstubAllGlobals()
})

function Mount(InitialPath = '/databases/db-1/migrations') {
  const Client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity } } })
  Clients.push(Client)
  return Render(
    <QueryClientProvider client={Client}>
      <MemoryRouter initialEntries={[InitialPath]}>
        <App />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

Test('loads and displays linear migrations table and views details', async () => {
  Vi.stubGlobal(
    'fetch',
    Vi.fn(async (Path: string) => {
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

  Mount()
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
})

