import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import type { FormEvent } from 'react'
import { Navigate, useNavigate } from 'react-router-dom'
import { ApiError, Login } from '../api/client'
import { AuthQueryKey, SessionQuery } from '../auth/query'

export function LoginPage() {
  const NavigateTo = useNavigate()
  const QueryCache = useQueryClient()
  const Session = useQuery(SessionQuery)
  const Submit = useMutation({
    mutationFn: Login,
    onSuccess: Result => {
      QueryCache.clear()
      QueryCache.setQueryData(AuthQueryKey, Result)
      void NavigateTo('/', { replace: true })
    },
  })
  function HandleSubmit(Event: FormEvent<HTMLFormElement>) {
    Event.preventDefault()
    const Values = new FormData(Event.currentTarget)
    Submit.mutate({ username: String(Values.get('username') ?? ''), password: String(Values.get('password') ?? '') })
  }
  if (Session.isPending) return <p role="status">Loading session…</p>
  if (Session.data) return <Navigate to="/" replace />
  if (Session.error && !(Session.error instanceof ApiError && Session.error.Status === 401)) {
    return <main><p role="alert">Unable to reach the authentication service.</p><button onClick={() => void Session.refetch()}>Try again</button></main>
  }
  return (
    <main>
      <h1>Sign in</h1>
      <form method="post" onSubmit={HandleSubmit}>
        <p><label htmlFor="username">Username</label><br /><input id="username" name="username" autoComplete="username" required maxLength={64} /></p>
        <p><label htmlFor="password">Password</label><br /><input id="password" name="password" type="password" autoComplete="current-password" required maxLength={1024} /></p>
        {Submit.isError && <p role="alert">{Submit.error instanceof ApiError ? Submit.error.message : 'Unable to sign in. Try again.'}</p>}
        <button type="submit" disabled={Submit.isPending}>{Submit.isPending ? 'Signing in…' : 'Sign in'}</button>
      </form>
    </main>
  )
}
