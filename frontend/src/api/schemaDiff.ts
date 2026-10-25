import { ApiRequest } from './client'

export interface SchemaChange {
  sql: string
  hazards: { type: string; message: string }[]
}

export interface SchemaComparison {
  current_database_id: string
  reference_database_id: string
  schema: string
  current_schema_hash: string
  requires_recomparison: boolean
  statements: SchemaChange[]
  sql_text: string
}

export interface GeneratedMigration {
  DatabaseID: string
  Title: string
  SQLText: string
}

export function CompareSchemas(DatabaseID: string, ReferenceDatabaseID: string, Schema: string) {
  return ApiRequest<SchemaComparison>(`/api/databases/${encodeURIComponent(DatabaseID)}/schema-diff`, {
    method: 'POST',
    body: JSON.stringify({ reference_database_id: ReferenceDatabaseID, schema: Schema }),
  })
}

export function ReadGeneratedMigration(State: unknown, DatabaseID: string): GeneratedMigration | undefined {
  if (typeof State !== 'object' || State === null || !('GeneratedMigration' in State)) return undefined
  const Draft = State.GeneratedMigration
  if (typeof Draft !== 'object' || Draft === null || !('DatabaseID' in Draft) || Draft.DatabaseID !== DatabaseID || !('Title' in Draft) || typeof Draft.Title !== 'string' || !('SQLText' in Draft) || typeof Draft.SQLText !== 'string' || !Draft.SQLText.trim()) return undefined
  return { DatabaseID, Title: Draft.Title, SQLText: Draft.SQLText }
}
