import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { ApiError, Logout } from '../api/client'

export function SignOutButton({ className = 'ghost' }: { className?: string }) {
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
    <>
      <button className={className} type="button" onClick={() => ExitSession.mutate()} disabled={ExitSession.isPending}>
        {ExitSession.isPending ? 'Signing out…' : 'Sign out'}
      </button>
      {ExitSession.isError && <span className="alert-inline" role="alert">Unable to sign out. Try again.</span>}
    </>
  )
}
