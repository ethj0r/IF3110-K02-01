import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef } from 'react'
import { ListMigrationJobs, type MigrationJob } from './client'

export const MigrationJobKeys = {
  list: (DatabaseID: string) => ['migration-jobs', DatabaseID] as const,
}

export function IsJobActive(Job: MigrationJob) {
  return Job.status === 'queued' || Job.status === 'running'
}

// Server-backed job list shared by every database view, so an active job stays
// visible after navigating away, coming back, or reloading the page.
export function useMigrationJobs(DatabaseID: string, Enabled = true) {
  const QueryCache = useQueryClient()
  const Query = useQuery({
    queryKey: MigrationJobKeys.list(DatabaseID),
    queryFn: ({ signal }) => ListMigrationJobs(DatabaseID, signal),
    enabled: Enabled && Boolean(DatabaseID),
    refetchInterval: Current => (Current.state.data?.some(IsJobActive) ? 1000 : false),
  })
  const Active = Query.data?.find(IsJobActive)
  const ActiveID = Active?.id ?? null
  const Previous = useRef(ActiveID)

  useEffect(() => {
    if (Previous.current && Previous.current !== ActiveID) {
      void QueryCache.invalidateQueries({ queryKey: ['migrations', DatabaseID] })
      void QueryCache.invalidateQueries({ queryKey: ['databases', DatabaseID] })
    }
    Previous.current = ActiveID
  }, [ActiveID, DatabaseID, QueryCache])

  return { Query, Active }
}

// Shows a freshly enqueued job immediately; polling takes over from there.
export function TrackEnqueuedJob(QueryCache: ReturnType<typeof useQueryClient>, Job: MigrationJob) {
  QueryCache.setQueryData<MigrationJob[]>(MigrationJobKeys.list(Job.database_id), Previous => [
    Job,
    ...(Previous ?? []).filter(Item => Item.id !== Job.id),
  ])
}

export function ActionLabel(Action: string): string {
  switch (Action) {
    case 'rollback': return 'Rollback'
    case 'forward': return 'Forward'
    case 'retry': return 'Retry'
    default: return 'Apply'
  }
}
