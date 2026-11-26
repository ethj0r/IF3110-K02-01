import { useQuery } from '@tanstack/react-query'
import { Navigate, Outlet } from 'react-router-dom'
import { ApiError } from '../api/client'
import { Loading, Notice } from '../ui/Feedback'
import { SessionQuery } from './query'

export function AdminGuard() {
  const Session = useQuery(SessionQuery)
  if (Session.isPending) return <Loading>Loading session…</Loading>
  if (Session.error instanceof ApiError && Session.error.Status === 401) return <Navigate to="/login" replace />
  if (Session.isError) {
    return (
      <div className="stack">
        <Notice Tone="danger" Role="alert">Unable to verify your session.</Notice>
        <div><button className="btn btn-secondary" type="button" onClick={() => void Session.refetch()}>Try again</button></div>
      </div>
    )
  }
  if (!Session.data?.user.is_platform_admin) return <Navigate to="/" replace />
  return <Outlet />
}
