import { useQuery } from '@tanstack/react-query'
import { Navigate, Outlet } from 'react-router-dom'
import { ApiError } from '../api/client'
import { SessionQuery } from './query'

export function AuthGuard() {
  const Session = useQuery(SessionQuery)
  if (Session.isPending) return <p role="status">Loading session…</p>
  if (Session.error instanceof ApiError && Session.error.Status === 401) return <Navigate to="/login" replace />
  if (Session.isError) return <main><p role="alert">Unable to verify your session.</p><button onClick={() => void Session.refetch()}>Try again</button></main>
  return <Outlet />
}
