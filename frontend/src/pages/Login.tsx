import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import type { FormEvent } from 'react'
import { Navigate, useNavigate } from 'react-router-dom'
import { ApiError, Login } from '../api/client'
import { SessionError, SessionSplash } from '../auth/SessionState'
import { AuthQueryKey, SessionQuery } from '../auth/query'
import { Notice } from '../ui/Feedback'
import { Icon } from '../ui/Icon'

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

  if (Session.isPending) return <SessionSplash />
  if (Session.data) return <Navigate to="/" replace />
  if (Session.error && !(Session.error instanceof ApiError && Session.error.Status === 401)) {
    return <SessionError Message="Unable to reach the authentication service." OnRetry={() => void Session.refetch()} />
  }

  return (
    <main className="auth">
      <span className="auth-orb auth-orb-a" aria-hidden="true" />
      <span className="auth-orb auth-orb-b" aria-hidden="true" />
      <span className="auth-orb auth-orb-c" aria-hidden="true" />
      <div className="auth-panel">
        <div className="auth-head">
          <span className="brand">
            <span className="brand-mark"><Icon Name="database" Size={16} /></span>
            <span className="brand-name">Database Platform</span>
          </span>
          <h1>Sign in</h1>
        </div>
        <form className="auth-form" method="post" onSubmit={HandleSubmit}>
          <div className="field">
            <label htmlFor="username">Username</label>
            <input className="input" id="username" name="username" autoComplete="username" required maxLength={64} autoFocus />
          </div>
          <div className="field">
            <label htmlFor="password">Password</label>
            <input className="input" id="password" name="password" type="password" autoComplete="current-password" required maxLength={1024} />
          </div>
          {Submit.isError && (
            <Notice Tone="danger" Role="alert">
              {Submit.error instanceof ApiError ? Submit.error.message : 'Unable to sign in. Try again.'}
            </Notice>
          )}
          <button className="btn btn-block" type="submit" disabled={Submit.isPending}>{Submit.isPending ? 'Signing in…' : 'Sign in'}</button>
        </form>
      </div>
    </main>
  )
}
