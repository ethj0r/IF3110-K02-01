import { afterEach as AfterEach, expect as Expect, test as Test, vi as Vi } from 'vitest'
import { cleanup as Cleanup, render as Render, screen as Screen } from '@testing-library/react'
import UserEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { App } from '../src/App'

const Clients: QueryClient[] = []
const Password = 'correct fixture password'
const CSRF = 'c'.repeat(43)

AfterEach(() => {
  Cleanup()
  Clients.splice(0).forEach(Client => Client.clear())
  Vi.unstubAllGlobals()
})

function Mount() {
  const Client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity } } })
  Clients.push(Client)
  return Render(<QueryClientProvider client={Client}><MemoryRouter initialEntries={['/']}><App /></MemoryRouter></QueryClientProvider>)
}

function InstallAPI() {
  const State = { SignedIn: false, Unavailable: false }
  const Calls: { Path: string; Options?: RequestInit }[] = []
  Vi.stubGlobal('fetch', Vi.fn(async (Path: string, Options?: RequestInit) => {
    Calls.push({ Path, Options })
    if (State.Unavailable) return Response.json({ error: { code: 'authentication_unavailable', message: 'Authentication service unavailable' } }, { status: 503 })
    if (Path === '/api/auth/login') {
      const Input = JSON.parse(String(Options?.body)) as { password: string }
      if (Input.password !== Password) return Response.json({ error: { code: 'invalid_credentials', message: 'Invalid username or password' } }, { status: 401 })
      State.SignedIn = true
    }
    if (!State.SignedIn) return Response.json({ error: { code: 'unauthenticated', message: 'Authentication required' } }, { status: 401 })
    if (Path === '/api/auth/logout') {
      Expect(new Headers(Options?.headers).get('X-CSRF-Token')).toBe(CSRF)
      State.SignedIn = false
      return new Response(null, { status: 204 })
    }
    return Response.json({ user: { id: 'fixture-user', username: 'admin', is_platform_admin: true }, csrf_token: CSRF, expires_at: '2099-01-01T00:00:00Z' })
  }))
  return { State, Calls }
}

Test('protected workspace supports login, session reload, and logout', async () => {
  const API = InstallAPI()
  const First = Mount()
  const User = UserEvent.setup()
  await Screen.findByRole('heading', { name: 'Sign in' })
  await User.type(Screen.getByLabelText('Username'), 'admin')
  await User.type(Screen.getByLabelText('Password'), Password)
  await User.click(Screen.getByRole('button', { name: 'Sign in' }))
  await Screen.findByRole('heading', { name: 'Database Workspace' })
  Expect(Screen.getByText(/Signed in as admin/)).toBeTruthy()
  Expect(API.State.SignedIn).toBe(true)
  Expect(API.Calls.find(Call => Call.Path === '/api/auth/login')?.Options?.credentials).toBe('same-origin')
  First.unmount()
  Mount()
  await Screen.findByRole('heading', { name: 'Database Workspace' })
  await User.click(Screen.getByRole('button', { name: 'Sign out' }))
  await Screen.findByRole('heading', { name: 'Sign in' })
  Expect(API.State.SignedIn).toBe(false)
})

Test('invalid credentials remain on the login form', async () => {
  InstallAPI()
  Mount()
  const User = UserEvent.setup()
  await Screen.findByRole('heading', { name: 'Sign in' })
  await User.type(Screen.getByLabelText('Username'), 'admin')
  await User.type(Screen.getByLabelText('Password'), 'wrong password')
  await User.click(Screen.getByRole('button', { name: 'Sign in' }))
  Expect((await Screen.findByRole('alert')).textContent).toBe('Invalid username or password')
  Expect(Screen.getByRole('heading', { name: 'Sign in' })).toBeTruthy()
  Expect(Screen.getByLabelText('Password').closest('form')?.getAttribute('method')).toBe('post')
})

Test('service failure offers retry instead of treating it as logout', async () => {
  const API = InstallAPI()
  API.State.Unavailable = true
  Mount()
  const User = UserEvent.setup()
  Expect((await Screen.findByRole('alert')).textContent).toBe('Unable to verify your session.')
  API.State.Unavailable = false
  await User.click(Screen.getByRole('button', { name: 'Try again' }))
  await Screen.findByRole('heading', { name: 'Sign in' })
})
