import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState, type FormEvent } from 'react'
import { Link, useParams } from 'react-router-dom'
import {
  ApiError,
  CreateMigration,
  DeleteMigrationDraft,
  ExecuteMigration,
  ListMigrations,
  UpdateMigrationDraft,
  type Migration,
} from '../api/client'

export function MigrationsPage() {
  const RouteParams = useParams<{ dbId?: string }>()
  const [DatabaseID, setDatabaseID] = useState<string>(RouteParams.dbId ?? '')
  const [ActiveDatabase, setActiveDatabase] = useState<string>(RouteParams.dbId ?? '')
  const [SelectedMigration, setSelectedMigration] = useState<Migration | null>(null)
  const [IsEditorOpen, setIsEditorOpen] = useState(false)
  const [EditingMigration, setEditingMigration] = useState<Migration | null>(null)
  const [EditorTitle, setEditorTitle] = useState('')
  const [EditorSQL, setEditorSQL] = useState('')
  const [ExecutionError, setExecutionError] = useState<string | null>(null)
  const [SuccessMessage, setSuccessMessage] = useState<string | null>(null)

  const QueryCache = useQueryClient()

  const MigrationsQuery = useQuery({
    queryKey: ['migrations', ActiveDatabase],
    queryFn: ({ signal }) => ListMigrations(ActiveDatabase, signal),
    enabled: Boolean(ActiveDatabase),
  })

  const SaveMutation = useMutation({
    mutationFn: async () => {
      if (EditingMigration) {
        return UpdateMigrationDraft(ActiveDatabase, EditingMigration.id, {
          title: EditorTitle,
          sql_text: EditorSQL,
        })
      }
      return CreateMigration(ActiveDatabase, {
        title: EditorTitle,
        sql_text: EditorSQL,
      })
    },
    onSuccess: () => {
      setIsEditorOpen(false)
      setEditingMigration(null)
      setEditorTitle('')
      setEditorSQL('')
      setSuccessMessage('Migration draft saved successfully.')
      void QueryCache.invalidateQueries({ queryKey: ['migrations', ActiveDatabase] })
    },
  })

  const ExecuteMutation = useMutation({
    mutationFn: (MigrationID: string) => ExecuteMigration(ActiveDatabase, MigrationID),
    onSuccess: Executed => {
      setExecutionError(null)
      setSuccessMessage(`Migration v${Executed.version} committed successfully!`)
      if (SelectedMigration?.id === Executed.id) {
        setSelectedMigration(Executed)
      }
      void QueryCache.invalidateQueries({ queryKey: ['migrations', ActiveDatabase] })
    },
    onError: Error => {
      if (Error instanceof ApiError) {
        setExecutionError(Error.message)
      } else {
        setExecutionError('Migration execution failed and was rolled back.')
      }
      void QueryCache.invalidateQueries({ queryKey: ['migrations', ActiveDatabase] })
    },
  })

  const DeleteMutation = useMutation({
    mutationFn: (MigrationID: string) => DeleteMigrationDraft(ActiveDatabase, MigrationID),
    onSuccess: () => {
      setSuccessMessage('Draft migration deleted.')
      if (SelectedMigration) setSelectedMigration(null)
      void QueryCache.invalidateQueries({ queryKey: ['migrations', ActiveDatabase] })
    },
  })

  function handleSelectDatabase(Event: FormEvent) {
    Event.preventDefault()
    if (DatabaseID.trim()) {
      setActiveDatabase(DatabaseID.trim())
      setSelectedMigration(null)
      setExecutionError(null)
      setSuccessMessage(null)
    }
  }

  function openCreateEditor() {
    setEditingMigration(null)
    setEditorTitle('')
    setEditorSQL('')
    setIsEditorOpen(true)
    setExecutionError(null)
  }

  function openEditEditor(Item: Migration) {
    setEditingMigration(Item)
    setEditorTitle(Item.title)
    setEditorSQL(Item.sql_text)
    setIsEditorOpen(true)
    setExecutionError(null)
  }

  function handleExecute(Item: Migration) {
    if (window.confirm(`Are you sure you want to execute migration v${Item.version} ("${Item.title}") in transaction?`)) {
      setSuccessMessage(null)
      setExecutionError(null)
      ExecuteMutation.mutate(Item.id)
    }
  }

  function handleDelete(Item: Migration) {
    if (window.confirm(`Delete draft migration v${Item.version}?`)) {
      DeleteMutation.mutate(Item.id)
    }
  }

  return (
    <main>
      <header>
        <nav aria-label="Main navigation">
          <Link to="/">← Back to Workspace</Link>
        </nav>
      </header>

      <h1>Database Migrations</h1>
      <p>Manage, inspect, and execute strictly linear transactional database migrations.</p>

      <section style={{ marginBottom: '2rem' }}>
        <form onSubmit={handleSelectDatabase} style={{ display: 'flex', gap: '0.5rem', alignItems: 'center' }}>
          <label htmlFor="target-db-id"><strong>Target Database ID:</strong></label>
          <input
            id="target-db-id"
            value={DatabaseID}
            onChange={E => setDatabaseID(E.target.value)}
            placeholder="e.g. UUID of connection"
            required
            style={{ padding: '0.4rem 0.6rem', flex: 1, maxWidth: '24rem' }}
          />
          <button type="submit">Load Migrations</button>
        </form>
      </section>

      {SuccessMessage && (
        <div role="status" style={{ background: '#e6f4ea', color: '#137333', padding: '0.75rem 1rem', borderRadius: '4px', marginBottom: '1rem' }}>
          {SuccessMessage}
        </div>
      )}

      {ExecutionError && (
        <div role="alert" style={{ background: '#fce8e6', color: '#c5221f', padding: '0.75rem 1rem', borderRadius: '4px', marginBottom: '1rem' }}>
          <strong>Execution Error:</strong> {ExecutionError}
        </div>
      )}

      {ActiveDatabase && (
        <section>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1rem' }}>
            <h2>Migration History (Linear)</h2>
            <button onClick={openCreateEditor}>+ Create Migration</button>
          </div>

          {MigrationsQuery.isPending && <p role="status">Loading migration history…</p>}
          {MigrationsQuery.isError && (
            <p role="alert" style={{ color: '#c5221f' }}>
              {MigrationsQuery.error instanceof ApiError ? MigrationsQuery.error.message : 'Failed to load migrations.'}
            </p>
          )}

          {MigrationsQuery.data && (
            <>
              {MigrationsQuery.data.length === 0 ? (
                <p>No migrations recorded for this database yet. Click <strong>+ Create Migration</strong> to prepare one.</p>
              ) : (
                <table style={{ width: '100%', borderCollapse: 'collapse', textAlign: 'left', marginBottom: '1.5rem' }}>
                  <thead>
                    <tr style={{ borderBottom: '2px solid #ccc' }}>
                      <th style={{ padding: '0.5rem' }}>Version</th>
                      <th style={{ padding: '0.5rem' }}>Title</th>
                      <th style={{ padding: '0.5rem' }}>Status</th>
                      <th style={{ padding: '0.5rem' }}>Executed At</th>
                      <th style={{ padding: '0.5rem' }}>Actions</th>
                    </tr>
                  </thead>
                  <tbody>
                    {MigrationsQuery.data.map(Item => (
                      <tr key={Item.id} style={{ borderBottom: '1px solid #eee' }}>
                        <td style={{ padding: '0.5rem', fontWeight: 'bold' }}>v{Item.version}</td>
                        <td style={{ padding: '0.5rem' }}>{Item.title}</td>
                        <td style={{ padding: '0.5rem' }}>
                          <span
                            style={{
                              padding: '0.2rem 0.5rem',
                              borderRadius: '4px',
                              fontSize: '0.85rem',
                              fontWeight: '600',
                              background:
                                Item.status === 'committed' || Item.status === 'applied'
                                  ? '#e6f4ea'
                                  : Item.status === 'rolled_back' || Item.status === 'failed'
                                    ? '#fce8e6'
                                    : '#fef7e0',
                              color:
                                Item.status === 'committed' || Item.status === 'applied'
                                  ? '#137333'
                                  : Item.status === 'rolled_back' || Item.status === 'failed'
                                    ? '#c5221f'
                                    : '#b06000',
                            }}
                          >
                            {Item.status}
                          </span>
                        </td>
                        <td style={{ padding: '0.5rem', fontSize: '0.9rem', color: '#555' }}>
                          {Item.executed_at ? new Date(Item.executed_at).toLocaleString() : '—'}
                        </td>
                        <td style={{ padding: '0.5rem', display: 'flex', gap: '0.4rem' }}>
                          <button onClick={() => setSelectedMigration(Item)} style={{ fontSize: '0.85rem' }}>
                            View
                          </button>
                          {Item.status === 'draft' && (
                            <>
                              <button onClick={() => openEditEditor(Item)} style={{ fontSize: '0.85rem' }}>
                                Edit
                              </button>
                              <button
                                onClick={() => handleExecute(Item)}
                                disabled={ExecuteMutation.isPending}
                                style={{ fontSize: '0.85rem', fontWeight: 'bold', background: '#137333', color: '#fff', border: 'none', borderRadius: '3px', padding: '0.2rem 0.6rem', cursor: 'pointer' }}
                              >
                                Execute
                              </button>
                              <button onClick={() => handleDelete(Item)} style={{ fontSize: '0.85rem', color: '#c5221f' }}>
                                Delete
                              </button>
                            </>
                          )}
                          {(Item.status === 'rolled_back' || Item.status === 'failed') && (
                            <button
                              onClick={() => handleExecute(Item)}
                              disabled={ExecuteMutation.isPending}
                              style={{ fontSize: '0.85rem', background: '#b06000', color: '#fff', border: 'none', borderRadius: '3px', padding: '0.2rem 0.6rem', cursor: 'pointer' }}
                            >
                              Retry
                            </button>
                          )}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
            </>
          )}
        </section>
      )}

      {/* Details */}
      {SelectedMigration && (
        <section style={{ background: '#fff', padding: '1.25rem', border: '1px solid #ccc', borderRadius: '6px', marginTop: '1.5rem' }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
            <h3>Migration Details: v{SelectedMigration.version} - {SelectedMigration.title}</h3>
            <button onClick={() => setSelectedMigration(null)}>Close</button>
          </div>
          <p>
            <strong>Status:</strong> {SelectedMigration.status} &nbsp;|&nbsp;
            <strong>Created:</strong> {new Date(SelectedMigration.created_at).toLocaleString()} &nbsp;|&nbsp;
            <strong>Executed:</strong> {SelectedMigration.executed_at ? new Date(SelectedMigration.executed_at).toLocaleString() : 'Not executed'}
          </p>

          {SelectedMigration.error_message && (
            <div role="alert" style={{ background: '#fce8e6', color: '#c5221f', padding: '0.75rem', borderRadius: '4px', marginBottom: '1rem' }}>
              <strong>Execution Rollback Reason:</strong>
              <pre style={{ margin: '0.5rem 0 0 0', whiteSpace: 'pre-wrap' }}>{SelectedMigration.error_message}</pre>
            </div>
          )}

          <div>
            <strong>SQL Script:</strong>
            <pre
              style={{
                background: '#1e1e1e',
                color: '#d4d4d4',
                padding: '1rem',
                borderRadius: '4px',
                overflowX: 'auto',
                fontSize: '0.9rem',
                fontFamily: 'monospace',
              }}
            >
              {SelectedMigration.sql_text}
            </pre>
          </div>
        </section>
      )}

      {/* Editor */}
      {IsEditorOpen && (
        <section style={{ background: '#fff', padding: '1.5rem', border: '2px solid #333', borderRadius: '6px', marginTop: '1.5rem' }}>
          <h3>{EditingMigration ? `Edit Migration v${EditingMigration.version}` : 'Create New Migration'}</h3>
          <form
            onSubmit={Event => {
              Event.preventDefault()
              SaveMutation.mutate()
            }}
          >
            <div style={{ marginBottom: '1rem' }}>
              <label htmlFor="migration-title"><strong>Title:</strong></label>
              <br />
              <input
                id="migration-title"
                value={EditorTitle}
                onChange={E => setEditorTitle(E.target.value)}
                placeholder="e.g. Create users table"
                required
                maxLength={128}
                style={{ width: '100%', padding: '0.5rem', marginTop: '0.3rem', boxSizing: 'border-box' }}
              />
            </div>

            <div style={{ marginBottom: '1rem' }}>
              <label htmlFor="migration-sql"><strong>SQL Query (Transactional):</strong></label>
              <br />
              <textarea
                id="migration-sql"
                value={EditorSQL}
                onChange={E => setEditorSQL(E.target.value)}
                placeholder="-- Write valid PostgreSQL DDL/DML statements here...&#10;CREATE TABLE sample (id SERIAL PRIMARY KEY, name TEXT);"
                required
                rows={12}
                style={{
                  width: '100%',
                  fontFamily: 'monospace',
                  fontSize: '0.9rem',
                  padding: '0.75rem',
                  marginTop: '0.3rem',
                  boxSizing: 'border-box',
                  background: '#2d3748',
                  color: '#f7fafc',
                  borderRadius: '4px',
                }}
              />
            </div>

            {SaveMutation.isError && (
              <p role="alert" style={{ color: '#c5221f' }}>
                {SaveMutation.error instanceof ApiError ? SaveMutation.error.message : 'Failed to save migration.'}
              </p>
            )}

            <div style={{ display: 'flex', gap: '0.75rem' }}>
              <button type="submit" disabled={SaveMutation.isPending}>
                {SaveMutation.isPending ? 'Saving…' : 'Save Draft'}
              </button>
              <button type="button" onClick={() => setIsEditorOpen(false)} disabled={SaveMutation.isPending}>
                Cancel
              </button>
            </div>
          </form>
        </section>
      )}
    </main>
  )
}

