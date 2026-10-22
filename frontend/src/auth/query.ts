import { QueryClient } from '@tanstack/react-query'
import { GetCurrentUser } from '../api/client'

export const AuthQueryKey = ['auth', 'me'] as const

export const SessionQuery = {
  queryKey: AuthQueryKey,
  queryFn: ({ signal: Signal }: { signal: AbortSignal }) => GetCurrentUser(Signal),
  retry: false,
  staleTime: 30_000,
}

export const QueryClientInstance = new QueryClient({ defaultOptions: { queries: { retry: false } } })
