import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { ApiError, Logout } from '../api/client'
import { Icon } from '../ui/Icon'

export function SignOutButton() {
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
      <button className="btn btn-secondary btn-sm btn-block" type="button" onClick={() => ExitSession.mutate()} disabled={ExitSession.isPending}>
        <Icon Name="logout" Size={14} />
        {ExitSession.isPending ? 'Signing out…' : 'Sign out'}
      </button>
      {ExitSession.isError && <span className="field-hint" role="alert">Unable to sign out. Try again.</span>}
    </>
  )
}
