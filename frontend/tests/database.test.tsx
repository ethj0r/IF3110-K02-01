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

Test('viewer preserves target text values, displays definitions, and paginates', async () => {
  const Fetch = Vi.fn(async (Path: string) => {
    if (Path === '/api/auth/me') return Response.json({ user: { id: 'u1', username: 'viewer', is_platform_admin: false }, csrf_token: 'c'.repeat(43), expires_at: '2099-01-01T00:00:00Z' })
    if (Path === '/api/databases') return Response.json([{ id: 'db-1', name: 'Reporting', level: 'view_only' }])
    if (Path.endsWith('/schemas')) return Response.json({ schemas: ['public'] })
    if (Path.includes('/tables?')) return Response.json({ tables: [{ name: 'values', type: 'BASE TABLE' }, { name: 'empty', type: 'BASE TABLE' }] })
    if (Path.includes('/columns?')) return Response.json({ columns: [{ name: 'amount', data_type: 'numeric(30,2)', nullable: false, default: null }] })
    if (Path.includes('/constraints?')) return Response.json({ constraints: [{ name: 'amount_check', type: 'CHECK', columns: 'amount', definition: 'CHECK (amount >= 0)' }] })
    if (Path.includes('/indexes?')) return Response.json({ indexes: [] })
    if (Path.includes('/rows?')) {
      const Page = Number(new URL(Path, 'https://app.test').searchParams.get('page'))
      const Values = ['9223372036854775807', '12345678901234567890.12', '01234567-89ab-cdef-0123-456789abcdef', 'NaN', null]
      return Response.json({
        columns: ['id', 'amount', 'identifier', 'special', 'optional'],
        rows: Path.includes('/empty/') ? [] : Page === 1 ? Array.from({ length: 25 }, () => Values) : [['26', '0', '', '-Infinity', '']],
        page: Page, page_size: 25, total: Path.includes('/empty/') ? 0 : 26,
      })
    }
    throw new Error(`Unexpected request: ${Path}`)
  })
  Vi.stubGlobal('fetch', Fetch)
  const Client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity } } })
  Clients.push(Client)
  Render(<QueryClientProvider client={Client}><MemoryRouter initialEntries={['/databases/db-1']}><App /></MemoryRouter></QueryClientProvider>)
  const User = UserEvent.setup()
  await Screen.findByRole('option', { name: 'values (table)' })
  await User.selectOptions(Screen.getByLabelText('Table'), 'values')
  Expect(await Screen.findByText('numeric(30,2)')).toBeTruthy()
  Expect(await Screen.findByText('CHECK (amount >= 0)')).toBeTruthy()
  for (const Value of ['9223372036854775807', '12345678901234567890.12', '01234567-89ab-cdef-0123-456789abcdef', 'NaN', 'NULL']) {
    Expect((await Screen.findAllByText(Value)).length).toBe(25)
  }
  await User.click(Screen.getByRole('button', { name: 'Next →' }))
  Expect(await Screen.findByText('-Infinity')).toBeTruthy()
  Expect(Fetch.mock.calls.some(([Path]) => Path.includes('schema=public&page=2&page_size=25'))).toBe(true)
  Expect((Screen.getByRole('button', { name: 'Next →' }) as HTMLButtonElement).disabled).toBe(true)
  await User.selectOptions(Screen.getByLabelText('Table'), 'empty')
  Expect(await Screen.findByText('No rows')).toBeTruthy()
  Expect((Screen.getByRole('button', { name: '← Previous' }) as HTMLButtonElement).disabled).toBe(true)
})
