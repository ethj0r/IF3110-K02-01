import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate } from 'react-router-dom'
import { ApiError, Logout } from '../api/client'
import { SessionQuery } from '../auth/query'

export function Workspace() {
  const Session = useQuery(SessionQuery)
  const QueryCache = useQueryClient()
  const NavigateTo = useNavigate()
  const ExitSession = useMutation({
    mutationFn: Logout,
    onSuccess: () => {
      QueryCache.clear()
      void NavigateTo('/login', { replace: true })
    },
    onError: Error => {
      if (Error instanceof ApiError && Error.Status === 401) {
        QueryCache.clear()
        void NavigateTo('/login', { replace: true })
      }
    },
  })
  return (
    <main>
      <header>
        <nav aria-label="Main navigation"><Link to="/">Workspace</Link></nav>
        <p>Signed in as {Session.data?.user.username}{Session.data?.user.is_platform_admin ? ' · Platform Admin' : ''}</p>
        <button onClick={() => ExitSession.mutate()} disabled={ExitSession.isPending}>Sign out</button>
        {ExitSession.isError && <p role="alert">Unable to sign out. Try again.</p>}
      </header>
      <h1>Database Workspace</h1>
      <p>Your session is active. Database and administration features will appear here when available.</p>
    </main>
  )
}
