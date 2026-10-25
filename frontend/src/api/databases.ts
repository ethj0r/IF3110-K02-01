import { ApiRequest } from './client'

export interface TableRef {
  name: string
  type: string
}

export interface TableColumn {
  name: string
  data_type: string
  nullable: boolean
  default: string | null
}

export interface TableConstraint {
  name: string
  type: string
  columns: string
  definition: string
}

export interface TableIndex {
  name: string
  definition: string
}

export interface TablePage {
  columns: string[]
  rows: (string | null)[][]
  page: number
  page_size: number
  total: number
}

export const DatabaseQueryKeys = {
  schemas: (id: string) => ['databases', id, 'schemas'] as const,
  tables: (id: string, schema: string) => ['databases', id, 'schemas', schema, 'tables'] as const,
  columns: (id: string, schema: string, table: string) => ['databases', id, 'schemas', schema, 'tables', table, 'columns'] as const,
  constraints: (id: string, schema: string, table: string) => ['databases', id, 'schemas', schema, 'tables', table, 'constraints'] as const,
  indexes: (id: string, schema: string, table: string) => ['databases', id, 'schemas', schema, 'tables', table, 'indexes'] as const,
  rows: (id: string, schema: string, table: string, page: number, pageSize: number) =>
    ['databases', id, 'schemas', schema, 'tables', table, 'rows', page, pageSize] as const,
}

export function ListSchemas(id: string) {
  return ApiRequest<{ schemas: string[] }>(`/api/databases/${encodeURIComponent(id)}/schemas`)
}

export function ListTables(id: string, schema: string) {
  return ApiRequest<{ tables: TableRef[] }>(
    `/api/databases/${encodeURIComponent(id)}/tables?schema=${encodeURIComponent(schema)}`,
  )
}

export function ListColumns(id: string, schema: string, table: string) {
  return ApiRequest<{ columns: TableColumn[] }>(
    TableEndpoint(id, schema, table, 'columns'),
  )
}

export function ListConstraints(id: string, schema: string, table: string) {
  return ApiRequest<{ constraints: TableConstraint[] }>(
    TableEndpoint(id, schema, table, 'constraints'),
  )
}

export function ListIndexes(id: string, schema: string, table: string) {
  return ApiRequest<{ indexes: TableIndex[] }>(
    TableEndpoint(id, schema, table, 'indexes'),
  )
}

export function ListRows(id: string, schema: string, table: string, page: number, pageSize: number) {
  return ApiRequest<TablePage>(
    TableEndpoint(id, schema, table, 'rows', { page: String(page), page_size: String(pageSize) }),
  )
}

function TableEndpoint(DatabaseID: string, Schema: string, Table: string, Endpoint: string, Extra: Record<string, string> = {}) {
  const Params = new URLSearchParams({ schema: Schema, ...Extra })
  const DotName = Table === '.' || Table === '..'
  if (DotName) Params.set('table', Table)
  const Path = DotName ? 'table' : `tables/${encodeURIComponent(Table)}`
  return `/api/databases/${encodeURIComponent(DatabaseID)}/${Path}/${Endpoint}?${Params.toString()}`
}
