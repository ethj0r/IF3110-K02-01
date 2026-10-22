import Test from 'node:test'
import Assert from 'node:assert/strict'
import { ApiError, ApiRequest, GetCurrentUser, Login, Logout } from '../src/api/client.ts'

function InstallFetch(Context, Callback) {
  const Original = globalThis.fetch
  globalThis.fetch = Callback
  Context.after(() => { globalThis.fetch = Original })
}

function JSONResponse(Value, Status = 200) {
  return new Response(JSON.stringify(Value), { status: Status, headers: { 'Content-Type': 'application/json' } })
}

Test('login keeps credentials in JSON and logout includes CSRF', async Context => {
  const Calls = []
  InstallFetch(Context, async (Path, Options) => {
    Calls.push({ Path, Options })
    if (Path.endsWith('/login')) return JSONResponse({ user: { id: 'user' }, csrf_token: 'first-csrf' })
    return new Response(null, { status: 204 })
  })
  await Login({ username: 'admin', password: 'test password' })
  await Logout()
  Assert.equal(Calls.length, 2)
  Assert.equal(Calls[0].Path, '/api/auth/login')
  Assert.deepEqual(JSON.parse(Calls[0].Options.body), { username: 'admin', password: 'test password' })
  Assert.equal(Calls[0].Options.credentials, 'same-origin')
  Assert.equal(Calls[0].Options.headers.has('X-CSRF-Token'), false)
  Assert.equal(Calls[1].Options.headers.get('X-CSRF-Token'), 'first-csrf')
})

Test('unsafe requests restore CSRF after reload and refresh after rejection', async Context => {
  const Calls = []
  let Reject = true
  InstallFetch(Context, async (Path, Options) => {
    Calls.push({ Path, Options })
    if (Path.endsWith('/csrf')) return JSONResponse({ csrf_token: 'restored-csrf' })
    if (Reject) {
      Reject = false
      return JSONResponse({ error: { code: 'csrf_rejected', message: 'Request verification failed' } }, 403)
    }
    return JSONResponse({ status: 'ok' })
  })
  await Assert.rejects(ApiRequest('/api/example', { method: 'POST', body: '{}' }), Error => Error instanceof ApiError && Error.Status === 403)
  await ApiRequest('/api/example', { method: 'POST', body: '{}' })
  Assert.deepEqual(Calls.map(Call => Call.Path), ['/api/auth/csrf', '/api/example', '/api/auth/csrf', '/api/example'])
  Assert.equal(Calls[3].Options.headers.get('X-CSRF-Token'), 'restored-csrf')
})

Test('unauthorized responses discard the previous CSRF token', async Context => {
  const Calls = []
  InstallFetch(Context, async (Path, Options) => {
    Calls.push({ Path, Options })
    if (Path.endsWith('/me')) return JSONResponse({ error: { code: 'unauthenticated', message: 'Authentication required' } }, 401)
    if (Path.endsWith('/csrf')) return JSONResponse({ csrf_token: 'new-csrf' })
    return new Response(null, { status: 204 })
  })
  await Assert.rejects(GetCurrentUser(), Error => Error instanceof ApiError && Error.Status === 401)
  await Logout()
  Assert.deepEqual(Calls.map(Call => Call.Path), ['/api/auth/me', '/api/auth/csrf', '/api/auth/logout'])
})

Test('API wrapper rejects external destinations before fetching', async Context => {
  InstallFetch(Context, async () => { throw new Error('fetch must not be called') })
  await Assert.rejects(ApiRequest('https://external.test/api/example', { method: 'POST' }), /relative/)
  await Assert.rejects(ApiRequest('//external.test/api/example'), /relative/)
})
