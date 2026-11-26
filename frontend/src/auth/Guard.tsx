import { useQuery } from '@tanstack/react-query'
import { Navigate, Outlet } from 'react-router-dom'
import { ApiError } from '../api/client'
import { SessionError, SessionSplash } from './SessionState'
import { SessionQuery } from './query'

export function AuthGuard() {
  const Session = useQuery(SessionQuery)
  if (Session.isPending) return <SessionSplash />
  if (Session.error instanceof ApiError && Session.error.Status === 401) return <Navigate to="/login" replace />
  if (Session.isError) return <SessionError Message="Unable to verify your session." OnRetry={() => void Session.refetch()} />
  return <Outlet />
}
