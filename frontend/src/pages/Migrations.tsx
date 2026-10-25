import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { AccessQueryKeys, ListAccessibleDatabases } from '../api/access'
import { RequestError } from '../components/Feedback'
import {
  AnalyzeQueryPlan,
  ApiError,
  CreateMigration,
  DeleteMigrationDraft,
  ExecuteMigration,
  ListMigrations,
  UpdateMigrationDraft,
  type Migration,
  type QueryPlanAnalysis,
} from '../api/client'

export function MigrationsPage() {
  const { dbId = '' } = useParams<{ dbId?: string }>()
  return <DatabaseMigrations key={dbId} ActiveDatabase={dbId} />
}

function DatabaseMigrations({ ActiveDatabase }: { ActiveDatabase: string }) {
  const NavigateTo = useNavigate()
  const Databases = useQuery({ queryKey: AccessQueryKeys.databases, queryFn: ListAccessibleDatabases })
  const Database = Databases.data?.find(Item => Item.id === ActiveDatabase)
  const [SelectedMigrationID, setSelectedMigrationID] = useState<string | null>(null)
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
    enabled: Boolean(Database),
  })
  const History = MigrationsQuery.data ?? []
  const SelectedMigration = History.find(Item => Item.id === SelectedMigrationID)
  const CanEdit = Database?.level === 'view_edit' && !MigrationsQuery.isError
  const Pending = History.some(Item => Item.status === 'pending')
  const LastAttempt = Math.max(0, ...History.filter(Item => Item.status !== 'draft').map(Item => Item.version))
  const CanExecute = (Item: Migration) => !Pending && Item.version >= LastAttempt &&
    !History.some(Earlier => Earlier.version < Item.version && Earlier.status === 'draft')

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
      void QueryCache.invalidateQueries({ queryKey: ['migrations', ActiveDatabase] })
      void QueryCache.invalidateQueries({ queryKey: ['databases', ActiveDatabase] })
    },
    onError: Error => {
      if (Error instanceof ApiError) {
        setExecutionError(Error.message)
      } else {
        setExecutionError('Unable to confirm execution. Reload history before retrying.')
      }
      void QueryCache.invalidateQueries({ queryKey: ['migrations', ActiveDatabase] })
    },
  })

  const DeleteMutation = useMutation({
    mutationFn: (MigrationID: string) => DeleteMigrationDraft(ActiveDatabase, MigrationID),
    onSuccess: () => {
      setSuccessMessage('Draft migration deleted.')
      setSelectedMigrationID(null)
      void QueryCache.invalidateQueries({ queryKey: ['migrations', ActiveDatabase] })
    },
  })

  const DetailsAnalyze = useMutation({
    mutationFn: (SQLText: string) => AnalyzeQueryPlan(ActiveDatabase, SQLText),
  })

  const EditorAnalyze = useMutation({
    mutationFn: (SQLText: string) => AnalyzeQueryPlan(ActiveDatabase, SQLText),
  })

  const Busy = SaveMutation.isPending || ExecuteMutation.isPending || DeleteMutation.isPending

  function openCreateEditor() {
    setEditingMigration(null)
    setEditorTitle('')
    setEditorSQL('')
    setIsEditorOpen(true)
    setExecutionError(null)
    EditorAnalyze.reset()
  }

  function openEditEditor(Item: Migration) {
    setEditingMigration(Item)
    setEditorTitle(Item.title)
    setEditorSQL(Item.sql_text)
    setIsEditorOpen(true)
    setExecutionError(null)
    EditorAnalyze.reset()
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
    <main className="workspace-main">
      <header>
        <nav aria-label="Main navigation" style={{ display: 'flex', gap: '1rem', flexWrap: 'wrap' }}>
          <Link to="/">← Back to Workspace</Link>
          {Database && <Link to={`/databases/${Database.id}`}>Browse database</Link>}
        </nav>
      </header>

      <h1>Database Migrations</h1>
      <p>Manage, inspect, and execute strictly linear transactional database migrations.</p>

      <section style={{ marginBottom: '2rem' }}>
        <div style={{ display: 'flex', gap: '0.5rem', alignItems: 'center' }}>
          <label htmlFor="target-db-id"><strong>Database:</strong></label>
          <select
            id="target-db-id"
            value={ActiveDatabase}
            disabled={Busy || Databases.isPending}
            onChange={Event => void NavigateTo(Event.target.value ? `/databases/${encodeURIComponent(Event.target.value)}/migrations` : '/migrations')}
            style={{ padding: '0.4rem 0.6rem', flex: 1, maxWidth: '24rem' }}
          >
            <option value="">Select a database…</option>
            {Databases.data?.map(Item => <option key={Item.id} value={Item.id}>{Item.name}</option>)}
          </select>
        </div>
        <p className="muted">Changing database clears unsaved editor changes.</p>
        <RequestError error={Databases.error} />
        {Databases.data?.length === 0 && <p>No databases assigned. Ask an administrator for group access.</p>}
        {ActiveDatabase && !Databases.isPending && !Databases.isError && !Database && <p role="alert">No access to this database.</p>}
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

      {Database && (
        <section>
          <div className="migration-toolbar">
            <h2>Migration History (Linear)</h2>
            {CanEdit && <button disabled={Busy} onClick={openCreateEditor}>+ Create Migration</button>}
          </div>
          {Pending && <p role="status">Execution is pending. If it remains pending after a failure, ask an administrator to verify the target outcome before retrying.</p>}

          {MigrationsQuery.isPending && <p role="status">Loading migration history…</p>}
          {MigrationsQuery.isError && (
            <p role="alert" style={{ color: '#c5221f' }}>
              {MigrationsQuery.error instanceof ApiError ? MigrationsQuery.error.message : 'Failed to load migrations.'}
            </p>
          )}

          {MigrationsQuery.data && (
            <>
              {MigrationsQuery.data.length === 0 ? (
                <p>No migrations recorded for this database yet.</p>
              ) : (
                <div className="table-wrap"><table className="data-table migration-table" style={{ marginBottom: '1.5rem' }}>
                  <thead>
                    <tr style={{ borderBottom: '2px solid #ccc' }}>
                      <th style={{ padding: '0.5rem' }}>Version</th>
                      <th style={{ padding: '0.5rem' }}>Title</th>
                      <th style={{ padding: '0.5rem' }}>Status</th>
                      <th style={{ padding: '0.5rem' }}>Executed At</th>
                      <th style={{ padding: '0.5rem' }}>Executed By</th>
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
                            className="migration-status"
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
                        <td style={{ padding: '0.5rem' }}>{Item.executed_by_username ?? Item.executed_by ?? '—'}</td>
                        <td style={{ padding: '0.5rem' }}>
                          <div className="row-actions migration-row-actions">
                            <button
                              onClick={() => {
                                setSelectedMigrationID(Item.id)
                                DetailsAnalyze.reset()
                              }}
                              style={{ fontSize: '0.85rem' }}
                            >
                              View
                            </button>
                            {CanEdit && Item.status === 'draft' && (
                              <>
                                <button disabled={Busy} onClick={() => openEditEditor(Item)} style={{ fontSize: '0.85rem' }}>
                                  Edit
                                </button>
                                <button
                                  onClick={() => handleExecute(Item)}
                                  disabled={Busy || !CanExecute(Item)}
                                  style={{ fontSize: '0.85rem', fontWeight: 'bold', background: '#137333', color: '#fff', border: 'none', borderRadius: '3px', padding: '0.2rem 0.6rem', cursor: 'pointer' }}
                                >
                                  Execute
                                </button>
                                <button disabled={Busy} onClick={() => handleDelete(Item)} style={{ fontSize: '0.85rem', color: '#c5221f' }}>
                                  Delete
                                </button>
                              </>
                            )}
                            {CanEdit && (Item.status === 'rolled_back' || Item.status === 'failed') && (
                              <button
                                onClick={() => handleExecute(Item)}
                                disabled={Busy || !CanExecute(Item)}
                                style={{ fontSize: '0.85rem', background: '#b06000', color: '#fff', border: 'none', borderRadius: '3px', padding: '0.2rem 0.6rem', cursor: 'pointer' }}
                              >
                                Retry
                              </button>
                            )}
                          </div>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table></div>
              )}
            </>
          )}
        </section>
      )}

      {/* Details */}
      {SelectedMigration && (
        <section style={{ background: '#fff', padding: '1.25rem', border: '1px solid #ccc', borderRadius: '6px', marginTop: '1.5rem' }}>
          <div className="migration-toolbar">
            <h3>Migration Details: v{SelectedMigration.version} - {SelectedMigration.title}</h3>
            <button onClick={() => setSelectedMigrationID(null)}>Close</button>
          </div>
          <p>
            <strong>Status:</strong> {SelectedMigration.status} &nbsp;|&nbsp;
            <strong>Created:</strong> {new Date(SelectedMigration.created_at).toLocaleString()} &nbsp;|&nbsp;
            <strong>Executed:</strong> {SelectedMigration.executed_at ? new Date(SelectedMigration.executed_at).toLocaleString() : 'Not executed'}
            {' '}| <strong>Executed by:</strong> {SelectedMigration.executed_by_username ?? SelectedMigration.executed_by ?? '—'}
          </p>

          {SelectedMigration.error_message && (
            <div role="alert" style={{ background: '#fce8e6', color: '#c5221f', padding: '0.75rem', borderRadius: '4px', marginBottom: '1rem' }}>
              <strong>Execution Rollback Reason:</strong>
              <pre style={{ margin: '0.5rem 0 0 0', whiteSpace: 'pre-wrap' }}>{SelectedMigration.error_message}</pre>
            </div>
          )}

          <div>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '0.4rem' }}>
              <strong>SQL Script:</strong>
              <button
                type="button"
                className="secondary"
                disabled={DetailsAnalyze.isPending}
                onClick={() => DetailsAnalyze.mutate(SelectedMigration.sql_text)}
                style={{ fontSize: '0.8rem', minHeight: '28px', padding: '0 8px' }}
              >
                {DetailsAnalyze.isPending ? 'Analyzing…' : 'Analyze Plan (EXPLAIN)'}
              </button>
            </div>
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

          {DetailsAnalyze.isPending && (
            <p role="status" style={{ fontSize: '0.85rem', color: 'var(--muted)', marginTop: '0.75rem' }}>
              Running EXPLAIN (ANALYZE) in rollback transaction…
            </p>
          )}

          {DetailsAnalyze.isError && (
            <div role="alert" style={{ background: '#fce8e6', color: '#c5221f', padding: '0.75rem', borderRadius: '4px', marginTop: '0.75rem', fontSize: '0.85rem' }}>
              <strong>Plan Analysis Failed:</strong>{' '}
              {DetailsAnalyze.error instanceof ApiError ? DetailsAnalyze.error.message : 'Unable to analyze query plan.'}
              <div style={{ marginTop: '0.35rem', fontSize: '0.8rem', color: '#555' }}>
                <em>Catatan:</em> PostgreSQL <code>EXPLAIN</code> hanya mendukung query pemrosesan data (<code>SELECT</code>, <code>INSERT</code>, <code>UPDATE</code>, <code>DELETE</code>). Pernyataan DDL seperti <code>CREATE TABLE</code> tidak memiliki query execution plan.
              </div>
            </div>
          )}

          {DetailsAnalyze.data && (
            <PlanAnalysisReport Analysis={DetailsAnalyze.data} onDismiss={() => DetailsAnalyze.reset()} />
          )}
        </section>
      )}

      {/* Editor */}
      {IsEditorOpen && CanEdit && (
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
              <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '0.3rem' }}>
                <label htmlFor="migration-sql"><strong>SQL Query (Transactional):</strong></label>
                <button
                  type="button"
                  className="secondary"
                  disabled={EditorAnalyze.isPending || !EditorSQL.trim()}
                  onClick={() => EditorAnalyze.mutate(EditorSQL)}
                  style={{ fontSize: '0.8rem', minHeight: '28px', padding: '0 8px' }}
                >
                  {EditorAnalyze.isPending ? 'Analyzing…' : 'Analyze Plan (EXPLAIN)'}
                </button>
              </div>
              <textarea
                id="migration-sql"
                value={EditorSQL}
                onChange={E => {
                  setEditorSQL(E.target.value)
                  if (EditorAnalyze.data || EditorAnalyze.error) {
                    EditorAnalyze.reset()
                  }
                }}
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

            {EditorAnalyze.isPending && (
              <p role="status" style={{ fontSize: '0.85rem', color: 'var(--muted)', marginBottom: '1rem' }}>
                Running EXPLAIN (ANALYZE) in rollback transaction…
              </p>
            )}

            {EditorAnalyze.isError && (
              <div role="alert" style={{ background: '#fce8e6', color: '#c5221f', padding: '0.75rem', borderRadius: '4px', marginBottom: '1rem', fontSize: '0.85rem' }}>
                <strong>Plan Analysis Failed:</strong>{' '}
                {EditorAnalyze.error instanceof ApiError ? EditorAnalyze.error.message : 'Unable to analyze query plan.'}
                <div style={{ marginTop: '0.35rem', fontSize: '0.8rem', color: '#555' }}>
                  <em>Catatan:</em> PostgreSQL <code>EXPLAIN</code> hanya mendukung query pemrosesan data (<code>SELECT</code>, <code>INSERT</code>, <code>UPDATE</code>, <code>DELETE</code>). Pernyataan DDL seperti <code>CREATE TABLE</code> tidak memiliki query execution plan.
                </div>
              </div>
            )}

            {EditorAnalyze.data && (
              <PlanAnalysisReport Analysis={EditorAnalyze.data} onDismiss={() => EditorAnalyze.reset()} />
            )}

            {SaveMutation.isError && (
              <p role="alert" style={{ color: '#c5221f' }}>
                {SaveMutation.error instanceof ApiError ? SaveMutation.error.message : 'Failed to save migration.'}
              </p>
            )}

            <div className="page-actions">
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

function PlanAnalysisReport({ Analysis, onDismiss }: { Analysis: QueryPlanAnalysis; onDismiss?: () => void }) {
  return (
    <div
      style={{
        background: '#f7f8f5',
        border: '1px solid var(--line)',
        borderRadius: '4px',
        padding: '1rem',
        marginTop: '0.75rem',
        marginBottom: '1rem',
        fontSize: '0.85rem',
      }}
    >
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '0.75rem' }}>
        <strong>Execution Plan Analysis (EXPLAIN ANALYZE)</strong>
        {onDismiss && (
          <button
            type="button"
            className="ghost"
            style={{ fontSize: '0.8rem', minHeight: 'auto', padding: '2px 6px' }}
            onClick={onDismiss}
          >
            Dismiss
          </button>
        )}
      </div>
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(130px, 1fr))', gap: '0.75rem', marginBottom: '0.75rem' }}>
        <div style={{ background: '#fff', padding: '0.5rem', borderRadius: '4px', border: '1px solid var(--line)' }}>
          <div style={{ color: 'var(--muted)', fontSize: '0.75rem' }}>Estimated Cost</div>
          <div style={{ fontWeight: 600 }}>{Analysis.estimated_cost.toFixed(2)}</div>
        </div>
        <div style={{ background: '#fff', padding: '0.5rem', borderRadius: '4px', border: '1px solid var(--line)' }}>
          <div style={{ color: 'var(--muted)', fontSize: '0.75rem' }}>Execution Time</div>
          <div style={{ fontWeight: 600 }}>{Analysis.actual_execution_time.toFixed(3)} ms</div>
        </div>
        <div style={{ background: '#fff', padding: '0.5rem', borderRadius: '4px', border: '1px solid var(--line)' }}>
          <div style={{ color: 'var(--muted)', fontSize: '0.75rem' }}>Planning Time</div>
          <div style={{ fontWeight: 600 }}>{Analysis.planning_time.toFixed(3)} ms</div>
        </div>
        <div style={{ background: '#fff', padding: '0.5rem', borderRadius: '4px', border: '1px solid var(--line)' }}>
          <div style={{ color: 'var(--muted)', fontSize: '0.75rem' }}>Rows Processed</div>
          <div style={{ fontWeight: 600 }}>{Analysis.rows_processed}</div>
        </div>
      </div>
      <div style={{ display: 'flex', flexDirection: 'column', gap: '0.25rem', marginBottom: '0.5rem' }}>
        <div><strong>Scan Types:</strong> {Analysis.scan_types.length ? Analysis.scan_types.join(', ') : 'None'}</div>
        <div><strong>Indexes Used:</strong> {Analysis.indexes_used.length ? Analysis.indexes_used.join(', ') : 'None'}</div>
        <div><strong>Join Strategies:</strong> {Analysis.join_strategies.length ? Analysis.join_strategies.join(', ') : 'None'}</div>
      </div>
      <details>
        <summary style={{ cursor: 'pointer', color: 'var(--accent)', fontWeight: 500 }}>View Raw Plan JSON</summary>
        <pre
          style={{
            background: '#1e1e1e',
            color: '#d4d4d4',
            padding: '0.75rem',
            borderRadius: '4px',
            overflowX: 'auto',
            maxHeight: '200px',
            marginTop: '0.4rem',
            fontSize: '0.8rem',
            fontFamily: 'monospace',
          }}
        >
          {JSON.stringify(Analysis.raw_plan, null, 2)}
        </pre>
      </details>
    </div>
  )
}

