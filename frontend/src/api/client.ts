export interface User {
  id: string
  username: string
  is_platform_admin: boolean
}

export type MigrationStatus = 'draft' | 'pending' | 'committed' | 'applied' | 'rolled_back' | 'failed'

export type ExecutionKind = 'apply' | 'rollback' | 'forward' | 'retry'
export type ExecutionDirection = 'up' | 'down'
export type ExecutionStatus = 'queued' | 'running' | 'succeeded' | 'failed' | 'partial' | 'partially_reverted'

export interface Migration {
  id: string
  database_id: string
  version: number
  revision: number
  title: string
  sql_text: string
  down_sql?: string
  batch_mode: boolean
  batch_column?: string
  batch_size?: number
  irreversible: boolean
  irreversible_reason?: string
  status: MigrationStatus
  error_message?: string
  created_by?: string
  executed_by?: string
  executed_by_username?: string
  created_at: string
  updated_at: string
  executed_at?: string
  locked_at?: string
  can_apply: boolean
  can_rollback: boolean
  can_forward: boolean
  can_retry: boolean
}

export interface MigrationInput {
  title: string
  sql_text: string
  down_sql: string
  batch_mode: boolean
  batch_column: string
  batch_size: number
  irreversible: boolean
  irreversible_reason: string
}

export interface MigrationAttempt {
  id: string
  execution_id: string
  attempt_no: number
  version: number
  revision: number
  actor_id?: string
  actor_username?: string
  sql_text: string
  status: 'running' | 'succeeded' | 'failed'
  error_message?: string
  started_at: string
  finished_at?: string
  duration_ms?: number
}

export interface MigrationExecution {
  id: string
  migration_id: string
  database_id: string
  version: number
  revision: number
  title: string
  kind: ExecutionKind
  direction: ExecutionDirection
  status: ExecutionStatus
  sql_text: string
  up_sql: string
  down_sql?: string
  mode: 'normal' | 'batch'
  batch_column?: string
  batch_size?: number
  committed_batches: number
  committed_rows: number
  reverted_batches: number
  reverted_rows: number
  checkpoint_cursor: number
  rollback_of_execution_id?: string
  retry_of_execution_id?: string
  requested_by?: string
  requested_by_username?: string
  created_at: string
  started_at?: string
  finished_at?: string
  error_message?: string
  attempts: MigrationAttempt[]
}

export interface QueryPlanAnalysis {
  scan_types: string[]
  indexes_used: string[]
  join_strategies: string[]
  estimated_cost: number
  actual_execution_time: number
  rows_processed: number
  planning_time: number
  raw_plan: unknown
}

export interface AuthResponse {
  user: User
  csrf_token: string
  expires_at: string
}

export class ApiError extends Error {
  Status: number
  Code: string

  constructor(Status: number, Code: string, Message: string) {
    super(Message)
    this.name = 'ApiError'
    this.Status = Status
    this.Code = Code
  }
}

let CSRFToken: string | null = null

export async function ApiRequest<Result>(Path: string, Options: RequestInit = {}): Promise<Result> {
  if (!Path.startsWith('/api/')) throw new Error('API requests must use a relative /api/ path')
  const Method = (Options.method ?? 'GET').toUpperCase()
  const RequestHeaders = new Headers(Options.headers)
  RequestHeaders.set('Accept', 'application/json')
  if (Options.body !== undefined && !RequestHeaders.has('Content-Type')) RequestHeaders.set('Content-Type', 'application/json')
  if (!['GET', 'HEAD', 'OPTIONS'].includes(Method) && Path !== '/api/auth/login') {
    if (!CSRFToken) {
      const Verification = await ApiRequest<{ csrf_token: string }>('/api/auth/csrf', { signal: Options.signal })
      CSRFToken = Verification.csrf_token
    }
    RequestHeaders.set('X-CSRF-Token', CSRFToken)
  }
  const Response = await fetch(Path, { ...Options, method: Method, headers: RequestHeaders, credentials: 'same-origin' })
  if (!Response.ok) {
    let Code = 'request_failed'
    let Message = 'Unable to complete the request'
    try {
      const Body = await Response.json() as { error?: { code?: string; message?: string } }
      Code = Body.error?.code ?? Code
      Message = Body.error?.message ?? Message
    } catch {
      Message = 'Unable to complete the request'
    }
    if (Response.status === 401 || Code === 'csrf_rejected') CSRFToken = null
    throw new ApiError(Response.status, Code, Message)
  }
  if (Response.status === 204) return undefined as Result
  return Response.json() as Promise<Result>
}

export async function GetCurrentUser(Signal?: AbortSignal): Promise<AuthResponse> {
  const Result = await ApiRequest<AuthResponse>('/api/auth/me', { signal: Signal })
  CSRFToken = Result.csrf_token
  return Result
}

export async function Login(Credentials: { username: string; password: string }): Promise<AuthResponse> {
  CSRFToken = null
  const Result = await ApiRequest<AuthResponse>('/api/auth/login', { method: 'POST', body: JSON.stringify(Credentials) })
  CSRFToken = Result.csrf_token
  return Result
}

export async function Logout(): Promise<void> {
  await ApiRequest<void>('/api/auth/logout', { method: 'POST' })
  CSRFToken = null
}

export async function ListMigrations(DatabaseID: string, Signal?: AbortSignal): Promise<Migration[]> {
  return ApiRequest<Migration[]>(`/api/databases/${encodeURIComponent(DatabaseID)}/migrations`, { signal: Signal })
}

export async function GetMigration(DatabaseID: string, MigrationID: string, Signal?: AbortSignal): Promise<Migration> {
  return ApiRequest<Migration>(`/api/databases/${encodeURIComponent(DatabaseID)}/migrations/${encodeURIComponent(MigrationID)}`, { signal: Signal })
}

export async function CreateMigration(DatabaseID: string, Input: MigrationInput): Promise<Migration> {
  return ApiRequest<Migration>(`/api/databases/${encodeURIComponent(DatabaseID)}/migrations`, {
    method: 'POST',
    body: JSON.stringify(Input),
  })
}

export async function UpdateMigrationDraft(DatabaseID: string, MigrationID: string, Input: MigrationInput): Promise<Migration> {
  return ApiRequest<Migration>(`/api/databases/${encodeURIComponent(DatabaseID)}/migrations/${encodeURIComponent(MigrationID)}`, {
    method: 'PUT',
    body: JSON.stringify(Input),
  })
}

export async function DeleteMigrationDraft(DatabaseID: string, MigrationID: string): Promise<void> {
  return ApiRequest<void>(`/api/databases/${encodeURIComponent(DatabaseID)}/migrations/${encodeURIComponent(MigrationID)}`, {
    method: 'DELETE',
  })
}

export type JobAction = 'apply' | 'rollback' | 'forward' | 'retry'
export type JobStatus = 'queued' | 'running' | 'succeeded' | 'failed' | 'cancelled'

export interface MigrationJob {
  id: string
  database_id: string
  migration_id: string
  version: number
  title: string
  action: JobAction
  status: JobStatus
  attempts: number
  max_attempts: number
  available_at: string
  locked_by?: string
  locked_at?: string
  lease_expires_at?: string
  last_error?: string
  execution_id?: string
  requested_by?: string
  requested_by_username?: string
  traceparent?: string
  mode?: string
  committed_batches: number
  committed_rows: number
  reverted_batches: number
  reverted_rows: number
  checkpoint_cursor: number
  created_at: string
  updated_at: string
  finished_at?: string
}

function ActionRequest(DatabaseID: string, MigrationID: string, Action: JobAction | 'execute'): Promise<MigrationJob> {
  return ApiRequest<MigrationJob>(`/api/databases/${encodeURIComponent(DatabaseID)}/migrations/${encodeURIComponent(MigrationID)}/${Action}`, {
    method: 'POST',
  })
}

export function ExecuteMigration(DatabaseID: string, MigrationID: string): Promise<MigrationJob> {
  return ActionRequest(DatabaseID, MigrationID, 'execute')
}

export function ApplyMigration(DatabaseID: string, MigrationID: string): Promise<MigrationJob> {
  return ActionRequest(DatabaseID, MigrationID, 'apply')
}

export function RollbackMigration(DatabaseID: string, MigrationID: string): Promise<MigrationJob> {
  return ActionRequest(DatabaseID, MigrationID, 'rollback')
}

export function ForwardMigration(DatabaseID: string, MigrationID: string): Promise<MigrationJob> {
  return ActionRequest(DatabaseID, MigrationID, 'forward')
}

export function RetryMigration(DatabaseID: string, MigrationID: string): Promise<MigrationJob> {
  return ActionRequest(DatabaseID, MigrationID, 'retry')
}

export function GetMigrationJob(DatabaseID: string, JobID: string, Signal?: AbortSignal): Promise<MigrationJob> {
  return ApiRequest<MigrationJob>(`/api/databases/${encodeURIComponent(DatabaseID)}/jobs/${encodeURIComponent(JobID)}`, { signal: Signal })
}

export async function ListMigrationHistory(DatabaseID: string, HideRollback: boolean, Signal?: AbortSignal): Promise<MigrationExecution[]> {
  const Query = HideRollback ? '?hide_rollback=true' : ''
  return ApiRequest<MigrationExecution[]>(`/api/databases/${encodeURIComponent(DatabaseID)}/migrations/history${Query}`, { signal: Signal })
}

export async function AnalyzeQueryPlan(DatabaseID: string, SQLText: string): Promise<QueryPlanAnalysis> {
  return ApiRequest<QueryPlanAnalysis>(`/api/databases/${encodeURIComponent(DatabaseID)}/migrations/analyze`, {
    method: 'POST',
    body: JSON.stringify({ sql_text: SQLText }),
  })
}

