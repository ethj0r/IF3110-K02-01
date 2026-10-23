import { ApiRequest } from './client'

export type DatabaseAccessLevel = 'view_only' | 'view_edit'

export interface DatabaseAccess {
  id: string
  name: string
  host: string
  dbname: string
  level: DatabaseAccessLevel
}

export const AccessQueryKeys = {
  databases: ['access', 'databases'] as const,
}

export function ListAccessibleDatabases() {
  return ApiRequest<DatabaseAccess[]>('/api/databases')
}
