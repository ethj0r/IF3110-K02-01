import { ApiRequest } from './client'

export interface ManagedConnection {
  id: string
  name: string
  host: string
  port: number
  dbname: string
  username: string
  created_at: string
  updated_at: string
}

export interface ConnectionInput {
  name: string
  host: string
  port: number
  dbname: string
  username: string
  password: string
}

export const ConnectionQueryKeys = {
  connections: ['admin', 'connections'] as const,
}

export function ListConnections() {
  return ApiRequest<ManagedConnection[]>('/api/connections')
}

export function CreateConnection(Input: ConnectionInput) {
  return ApiRequest<ManagedConnection>('/api/connections', { method: 'POST', body: JSON.stringify(Input) })
}

export function UpdateConnection(ID: string, Input: ConnectionInput) {
  return ApiRequest<ManagedConnection>(`/api/connections/${encodeURIComponent(ID)}`, {
    method: 'PUT',
    body: JSON.stringify(Input),
  })
}

export function DeleteConnection(ID: string) {
  return ApiRequest<void>(`/api/connections/${encodeURIComponent(ID)}`, { method: 'DELETE' })
}

export function TestConnection(ID: string) {
  return ApiRequest<{ status: string }>(`/api/connections/${encodeURIComponent(ID)}/test`, { method: 'POST' })
}
