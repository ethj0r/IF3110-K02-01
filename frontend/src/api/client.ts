export interface User {
  id: string
  username: string
  is_platform_admin: boolean
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
