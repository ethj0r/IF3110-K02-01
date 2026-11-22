import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef, useState } from 'react'
import { Link, useLocation, useNavigate, useParams } from 'react-router-dom'
import { ReadGeneratedMigration } from '../api/schemaDiff'
import type { GeneratedMigration } from '../api/schemaDiff'
import { AccessQueryKeys, ListAccessibleDatabases } from '../api/access'
import { RequestError } from '../components/Feedback'
import {
  AnalyzeQueryPlan,
  ApiError,
  ApplyMigration,
  ApproveMigrationReview,
  CreateMigration,
  DeleteMigrationDraft,
  ForwardMigration,
  GetMigrationReview,
  ListMigrationHistory,
  ListMigrations,
  RetryMigration,
  RollbackMigration,
  SubmitMigrationReview,
  UpdateMigrationDraft,
  type ExecutionKind,
  type Migration,
  type MigrationExecution,
  type QueryPlanAnalysis,
} from '../api/client'

const ReviewActions: ExecutionKind[] = ['apply', 'rollback', 'forward', 'retry']

export function MigrationsPage() {
  const { dbId = '' } = useParams<{ dbId?: string }>()
  const Location = useLocation()
  const InitialDraft = ReadGeneratedMigration(Location.state, dbId)
  return <DatabaseMigrations key={`${dbId}:${Location.key}`} ActiveDatabase={dbId} InitialDraft={InitialDraft} />
}

function DatabaseMigrations({ ActiveDatabase, InitialDraft }: { ActiveDatabase: string; InitialDraft?: GeneratedMigration }) {
  const NavigateTo = useNavigate()
  const Databases = useQuery({ queryKey: AccessQueryKeys.databases, queryFn: ListAccessibleDatabases })
  const Database = Databases.data?.find(Item => Item.id === ActiveDatabase)
  const QueryCache = useQueryClient()
  const [SelectedMigrationID, setSelectedMigrationID] = useState<string | null>(null)
  const [SelectedExecutionID, setSelectedExecutionID] = useState<string | null>(null)
  const [ReviewSelection, setReviewSelection] = useState<{ ID: string; Action: ExecutionKind } | null>(null)
  const [IsEditorOpen, setIsEditorOpen] = useState(Boolean(InitialDraft))
  const [EditingMigration, setEditingMigration] = useState<Migration | null>(null)
  const [EditorTitle, setEditorTitle] = useState(InitialDraft?.Title ?? '')
  const [EditorSQL, setEditorSQL] = useState(InitialDraft?.SQLText ?? '')
  const [EditorDownSQL, setEditorDownSQL] = useState('')
  const [EditorIrreversible, setEditorIrreversible] = useState(false)
  const [EditorReason, setEditorReason] = useState('')
  const [HideRollback, setHideRollback] = useState(false)
  const [ExecutionError, setExecutionError] = useState<string | null>(null)
  const [SuccessMessage, setSuccessMessage] = useState<string | null>(null)

  const MigrationsQuery = useQuery({
    queryKey: ['migrations', ActiveDatabase],
    queryFn: ({ signal }) => ListMigrations(ActiveDatabase, signal),
    enabled: Boolean(Database),
  })
  const HistoryQuery = useQuery({
    queryKey: ['migrations', ActiveDatabase, 'history', HideRollback],
    queryFn: ({ signal }) => ListMigrationHistory(ActiveDatabase, HideRollback, signal),
    enabled: Boolean(Database),
  })
  const Definitions = MigrationsQuery.data ?? []
  const History = HistoryQuery.data ?? []
  const SelectedMigration = Definitions.find(Item => Item.id === SelectedMigrationID)
  const SelectedExecution = History.find(Item => Item.id === SelectedExecutionID)
  const ReviewMigration = Definitions.find(Item => Item.id === ReviewSelection?.ID)
  const CanEdit = Database?.level === 'view_edit' && !MigrationsQuery.isError
  const HasPending = Definitions.some(Item => Item.status === 'pending')

  function Refresh() {
    void QueryCache.invalidateQueries({ queryKey: ['migrations', ActiveDatabase] })
    void QueryCache.invalidateQueries({ queryKey: ['databases', ActiveDatabase] })
  }

  const SaveMutation = useMutation({
    mutationFn: async () => {
      const Input = {
        title: EditorTitle,
        sql_text: EditorSQL,
        down_sql: EditorIrreversible ? '' : EditorDownSQL,
        irreversible: EditorIrreversible,
        irreversible_reason: EditorIrreversible ? EditorReason : '',
      }
      if (EditingMigration) return UpdateMigrationDraft(ActiveDatabase, EditingMigration.id, Input)
      return CreateMigration(ActiveDatabase, Input)
    },
    onSuccess: () => {
      setIsEditorOpen(false)
      setEditingMigration(null)
      resetEditor()
      setSuccessMessage('Migration draft saved successfully.')
      Refresh()
    },
  })

  const ActionMutation = useMutation({
    mutationFn: ({ ID, Action }: { ID: string; Action: ExecutionKind }) => {
      switch (Action) {
        case 'rollback': return RollbackMigration(ActiveDatabase, ID)
        case 'forward': return ForwardMigration(ActiveDatabase, ID)
        case 'retry': return RetryMigration(ActiveDatabase, ID)
        default: return ApplyMigration(ActiveDatabase, ID)
      }
    },
    onSuccess: Executed => {
      setReviewSelection(null)
      setExecutionError(null)
      setSuccessMessage(`${ActionLabel(Executed.kind)} v${Executed.version} finished with status ${Executed.status}.`)
      Refresh()
    },
    onError: Error => {
      setExecutionError(Error instanceof ApiError ? Error.message : 'Unable to complete the migration action.')
      Refresh()
    },
  })

  const DeleteMutation = useMutation({
    mutationFn: (ID: string) => DeleteMigrationDraft(ActiveDatabase, ID),
    onSuccess: () => {
      setSuccessMessage('Draft migration deleted.')
      setSelectedMigrationID(null)
      Refresh()
    },
  })

  const DetailsAnalyze = useMutation({ mutationFn: (SQLText: string) => AnalyzeQueryPlan(ActiveDatabase, SQLText) })
  const EditorAnalyze = useMutation({ mutationFn: (SQLText: string) => AnalyzeQueryPlan(ActiveDatabase, SQLText) })
  const Busy = SaveMutation.isPending || ActionMutation.isPending || DeleteMutation.isPending

  function resetEditor() {
    setEditorTitle('')
    setEditorSQL('')
    setEditorDownSQL('')
    setEditorIrreversible(false)
    setEditorReason('')
  }

  function openCreateEditor() {
    setEditingMigration(null)
    resetEditor()
    setIsEditorOpen(true)
    setExecutionError(null)
    EditorAnalyze.reset()
  }

  function openEditEditor(Item: Migration) {
    setEditingMigration(Item)
    setEditorTitle(Item.title)
    setEditorSQL(Item.sql_text)
    setEditorDownSQL(Item.down_sql ?? '')
    setEditorIrreversible(Item.irreversible)
    setEditorReason(Item.irreversible_reason ?? '')
    setIsEditorOpen(true)
    setExecutionError(null)
    EditorAnalyze.reset()
  }

  function handleAction(Item: Migration, Action: ExecutionKind) {
    const Label = ActionLabel(Action)
    if (!window.confirm(`${Label} migration v${Item.version} ("${Item.title}")?`)) return
    setSuccessMessage(null)
    setExecutionError(null)
    ActionMutation.mutate({ ID: Item.id, Action })
  }

  function handleDelete(Item: Migration) {
    if (window.confirm(`Delete draft migration v${Item.version}?`)) DeleteMutation.mutate(Item.id)
  }

  return (
    <main className="workspace-main">
      <header>
        <nav aria-label="Main navigation" style={{ display: 'flex', gap: '1rem', flexWrap: 'wrap' }}>
          <Link to="/">← Back to Workspace</Link>
          {Database && <Link to={`/databases/${Database.id}`}>Browse database</Link>}
          {Database?.level === 'view_edit' && <Link to={`/databases/${Database.id}/schema-diff`}>Compare schemas</Link>}
        </nav>
      </header>

      <h1>Database Migrations</h1>
      <p>Manage immutable up/down migrations, inspect every execution attempt, and roll back or forward the active migration.</p>

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
          <strong>Migration Action Failed:</strong> {ExecutionError}
        </div>
      )}

      {Database && (
        <section>
          <div className="migration-toolbar">
            <h2>Migration Definitions</h2>
            {CanEdit && <button disabled={Busy} onClick={openCreateEditor}>+ Create Migration</button>}
          </div>
          {HasPending && <p role="status">An execution is pending. Verify the target outcome before retrying.</p>}
          {MigrationsQuery.isPending && <p role="status">Loading migrations…</p>}
          {MigrationsQuery.isError && (
            <p role="alert" style={{ color: '#c5221f' }}>
              {MigrationsQuery.error instanceof ApiError ? MigrationsQuery.error.message : 'Failed to load migrations.'}
            </p>
          )}

          {MigrationsQuery.data && (
            MigrationsQuery.data.length === 0 ? <p>No migrations recorded for this database yet.</p> : (
              <div className="table-wrap"><table className="data-table migration-table" style={{ marginBottom: '1.5rem' }}>
                <thead>
                  <tr>
                    <th>Version</th>
                    <th>Title</th>
                    <th>Status</th>
                    <th>Revision</th>
                    <th>Updated</th>
                    <th>Actions</th>
                  </tr>
                </thead>
                <tbody>
                  {MigrationsQuery.data.map(Item => (
                    <tr key={Item.id}>
                      <td style={{ fontWeight: 'bold' }}>v{Item.version}</td>
                      <td>
                        {Item.title}
                        {Item.irreversible && <span className="badge" style={{ marginLeft: '0.4rem' }}>irreversible</span>}
                      </td>
                      <td><StatusBadge Status={Item.status} /></td>
                      <td>{Item.revision}</td>
                      <td style={{ fontSize: '0.9rem', color: '#555' }}>{new Date(Item.updated_at).toLocaleString()}</td>
                      <td>
                        <div className="row-actions migration-row-actions">
                          <button onClick={() => { setSelectedMigrationID(Item.id); DetailsAnalyze.reset() }} style={{ fontSize: '0.85rem' }}>View</button>
                          {CanEdit && Item.status === 'draft' && (
                            <>
                              <button disabled={Busy} onClick={() => openEditEditor(Item)} style={{ fontSize: '0.85rem' }}>Edit</button>
                              <button disabled={Busy} onClick={() => handleDelete(Item)} style={{ fontSize: '0.85rem', color: '#c5221f' }}>Delete</button>
                            </>
                          )}
                          {ReviewActions.filter(Action => Item[`can_${Action}`] || (Action === 'apply' && Item.status === 'draft')).map(Action => (
                            <button key={Action} disabled={Busy || !Item[`can_${Action}`]}
                              onClick={() => setReviewSelection({ ID: Item.id, Action })}>
                              Review {Action}
                            </button>
                          ))}
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table></div>
            )
          )}
        </section>
      )}

      {Database && ReviewSelection && ReviewMigration && (
        <MigrationReviewPanel
          key={`${ReviewMigration.id}:${ReviewMigration.revision}:${ReviewSelection.Action}`}
          Item={ReviewMigration} Action={ReviewSelection.Action} CanEdit={CanEdit} Busy={Busy}
          OnClose={() => setReviewSelection(null)} OnExecute={() => handleAction(ReviewMigration, ReviewSelection.Action)}
        />
      )}

      {Database && (
        <section style={{ marginTop: '1.5rem' }}>
          <div className="migration-toolbar">
            <h2>Execution History</h2>
            <label style={{ display: 'flex', alignItems: 'center', gap: '0.4rem', fontSize: '0.9rem' }}>
              <input type="checkbox" checked={HideRollback} onChange={Event => setHideRollback(Event.target.checked)} />
              Hide rollback
            </label>
          </div>
          <p className="muted">Rollback pairs are hidden by execution ID. Failed attempts remain visible.</p>
          {HistoryQuery.isPending && <p role="status">Loading execution history…</p>}
          {HistoryQuery.isError && <p role="alert">Failed to load execution history.</p>}
          {HistoryQuery.data && (
            HistoryQuery.data.length === 0 ? <p>No execution attempts recorded.</p> : (
              <div className="table-wrap"><table className="data-table migration-table">
                <thead>
                  <tr>
                    <th>Execution</th>
                    <th>Version</th>
                    <th>Action</th>
                    <th>Status</th>
                    <th>Attempts</th>
                    <th>Requested By</th>
                    <th>Started</th>
                    <th></th>
                  </tr>
                </thead>
                <tbody>
                  {HistoryQuery.data.map(Item => (
                    <tr key={Item.id}>
                      <td><code title={Item.id}>{Item.id.slice(0, 8)}</code></td>
                      <td>v{Item.version} · {Item.title}</td>
                      <td>{ActionLabel(Item.kind)}{Item.direction === 'down' ? ' (down)' : ''}</td>
                      <td><ExecutionBadge Status={Item.status} /></td>
                      <td>{Item.attempts.length}</td>
                      <td>{Item.requested_by_username ?? Item.requested_by ?? '—'}</td>
                      <td style={{ fontSize: '0.9rem', color: '#555' }}>{new Date(Item.started_at ?? Item.created_at).toLocaleString()}</td>
                      <td>
                        <button className="ghost small" onClick={() => setSelectedExecutionID(Item.id === SelectedExecutionID ? null : Item.id)}>
                          {Item.id === SelectedExecutionID ? 'Close' : 'Inspect'}
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table></div>
            )
          )}
          {SelectedExecution && <ExecutionDetails Execution={SelectedExecution} />}
        </section>
      )}

      {SelectedMigration && (
        <section style={{ background: '#fff', padding: '1.25rem', border: '1px solid #ccc', borderRadius: '6px', marginTop: '1.5rem' }}>
          <div className="migration-toolbar">
            <h3>Migration Details: v{SelectedMigration.version} - {SelectedMigration.title}</h3>
            <button onClick={() => setSelectedMigrationID(null)}>Close</button>
          </div>
          <p>
            <strong>Status:</strong> {SelectedMigration.status} &nbsp;|&nbsp;
            <strong>Revision:</strong> {SelectedMigration.revision} &nbsp;|&nbsp;
            <strong>Updated:</strong> {new Date(SelectedMigration.updated_at).toLocaleString()}
            {SelectedMigration.executed_at && <> &nbsp;|&nbsp; <strong>Last applied:</strong> {new Date(SelectedMigration.executed_at).toLocaleString()}</>}
          </p>
          {SelectedMigration.error_message && (
            <div role="alert" style={{ background: '#fce8e6', color: '#c5221f', padding: '0.75rem', borderRadius: '4px', marginBottom: '1rem' }}>
              <strong>Last Error:</strong><pre style={{ margin: '0.5rem 0 0 0', whiteSpace: 'pre-wrap' }}>{SelectedMigration.error_message}</pre>
            </div>
          )}
          <SQLBlock Label="Up SQL" SQL={SelectedMigration.sql_text} OnAnalyze={() => DetailsAnalyze.mutate(SelectedMigration.sql_text)} Pending={DetailsAnalyze.isPending} />
          <SQLBlock Label={SelectedMigration.irreversible ? 'Down SQL (irreversible)' : 'Down SQL'} SQL={SelectedMigration.down_sql ?? '—'} />
          {SelectedMigration.irreversible && <p className="muted">Reason: {SelectedMigration.irreversible_reason}</p>}
          {DetailsAnalyze.isError && <p role="alert" style={{ color: '#c5221f' }}>Plan analysis failed: {DetailsAnalyze.error instanceof ApiError ? DetailsAnalyze.error.message : 'Unable to analyze query plan.'}</p>}
          {DetailsAnalyze.data && <PlanAnalysisReport Analysis={DetailsAnalyze.data} onDismiss={() => DetailsAnalyze.reset()} />}
        </section>
      )}

      {IsEditorOpen && CanEdit && (
        <section style={{ background: '#fff', padding: '1.5rem', border: '2px solid #333', borderRadius: '6px', marginTop: '1.5rem' }}>
          <h3>{EditingMigration ? `Edit Migration v${EditingMigration.version}` : 'Create New Migration'}</h3>
          <form onSubmit={Event => { Event.preventDefault(); SaveMutation.mutate() }}>
            <div style={{ marginBottom: '1rem' }}>
              <label htmlFor="migration-title"><strong>Title:</strong></label>
              <input id="migration-title" value={EditorTitle} onChange={E => setEditorTitle(E.target.value)} placeholder="e.g. Create users table" required maxLength={128} style={{ width: '100%', padding: '0.5rem', marginTop: '0.3rem', boxSizing: 'border-box' }} />
            </div>
            <SQLInput Label="Up SQL" Value={EditorSQL} OnChange={Value => { setEditorSQL(Value); if (EditorAnalyze.data || EditorAnalyze.error) EditorAnalyze.reset() }} OnAnalyze={() => EditorAnalyze.mutate(EditorSQL)} Pending={EditorAnalyze.isPending} />
            <label style={{ display: 'flex', alignItems: 'center', gap: '0.4rem', margin: '0.75rem 0' }}>
              <input type="checkbox" checked={EditorIrreversible} onChange={E => setEditorIrreversible(E.target.checked)} />
              Irreversible migration (no down SQL)
            </label>
            {EditorIrreversible ? (
              <div style={{ marginBottom: '1rem' }}>
                <label htmlFor="migration-reason"><strong>Irreversible reason:</strong></label>
                <input id="migration-reason" value={EditorReason} onChange={E => setEditorReason(E.target.value)} required maxLength={500} style={{ width: '100%', padding: '0.5rem', marginTop: '0.3rem', boxSizing: 'border-box' }} />
              </div>
            ) : (
              <SQLInput Label="Down SQL" Value={EditorDownSQL} OnChange={setEditorDownSQL} />
            )}
            {EditorAnalyze.isError && <p role="alert" style={{ color: '#c5221f' }}>Plan analysis failed: {EditorAnalyze.error instanceof ApiError ? EditorAnalyze.error.message : 'Unable to analyze query plan.'}</p>}
            {EditorAnalyze.data && <PlanAnalysisReport Analysis={EditorAnalyze.data} onDismiss={() => EditorAnalyze.reset()} />}
            {SaveMutation.isError && <p role="alert" style={{ color: '#c5221f' }}>{SaveMutation.error instanceof ApiError ? SaveMutation.error.message : 'Failed to save migration.'}</p>}
            <div className="page-actions">
              <button type="submit" disabled={SaveMutation.isPending}>{SaveMutation.isPending ? 'Saving…' : 'Save Draft'}</button>
              <button type="button" onClick={() => setIsEditorOpen(false)} disabled={SaveMutation.isPending}>Cancel</button>
            </div>
          </form>
        </section>
      )}
    </main>
  )
}

function MigrationReviewPanel({ Item, Action, CanEdit, Busy, OnClose, OnExecute }: {
  Item: Migration
  Action: ExecutionKind
  CanEdit: boolean
  Busy: boolean
  OnClose: () => void
  OnExecute: () => void
}) {
  const Heading = useRef<HTMLHeadingElement>(null)
  useEffect(() => Heading.current?.focus(), [])
  const Review = useQuery({
    queryKey: ['migrations', Item.database_id, 'review', Item.id, Item.revision, Action],
    queryFn: ({ signal }) => GetMigrationReview(Item.database_id, Item.id, Action, signal),
    retry: false,
    refetchInterval: 5000,
  })
  const Submit = useMutation({
    mutationFn: () => SubmitMigrationReview(Item.database_id, Item.id, Action),
    onSuccess: () => Review.refetch(),
  })
  const Approve = useMutation({
    mutationFn: ({ ReviewID, GroupID }: { ReviewID: string; GroupID: string }) =>
      ApproveMigrationReview(Item.database_id, Item.id, Action, ReviewID, GroupID),
    onSettled: () => Review.refetch(),
  })
  const Pending = Busy || Submit.isPending || Approve.isPending || Review.isFetching
  const Available = Item[`can_${Action}`] && !Review.isError
  const Current = Review.data

  return (
    <section className="panel migration-review" aria-labelledby="migration-review-heading">
      <div className="migration-toolbar">
        <h2 id="migration-review-heading" ref={Heading} tabIndex={-1}>{ActionLabel(Action)} review for v{Item.version}</h2>
        <button type="button" onClick={OnClose}>Close review</button>
      </div>
      {Review.isPending && <p role="status">Loading review…</p>}
      <RequestError error={Review.error ?? Submit.error ?? Approve.error} />
      {Current === null && <p>No review has been requested for this action.</p>}
      {Current && !Review.isError && (
        <>
          <p role="status">{Current.status === 'approved' ? 'All groups approved' : 'Waiting for group approvals'} · Revision {Current.revision}</p>
          <SQLBlock Label="SQL for review" SQL={Current.sql_text} />
          <ul>
            {Current.groups.map(Group => (
              <li key={Group.id} className="migration-toolbar">
                <strong>{Group.name}</strong>
                <span>{Group.approved ? 'Approved' : Group.leader_user_id ? 'Awaiting approval' : 'No active leader'}</span>
                {Group.can_approve && <button type="button" disabled={Pending || !Available}
                  onClick={() => Approve.mutate({ ReviewID: Current.id, GroupID: Group.id })}>Approve for {Group.name}</button>}
              </li>
            ))}
          </ul>
        </>
      )}
      {CanEdit && (
        <div className="page-actions">
          {Current === null && <button type="button" disabled={Pending || !Available} onClick={() => Submit.mutate()}>Request review</button>}
          <button type="button" disabled={Pending || !Available || Current?.status !== 'approved'} onClick={OnExecute}>{ActionLabel(Action)}</button>
        </div>
      )}
    </section>
  )
}

function ActionLabel(Action: string): string {
  switch (Action) {
    case 'rollback': return 'Rollback'
    case 'forward': return 'Forward'
    case 'retry': return 'Retry'
    default: return 'Apply'
  }
}

function StatusBadge({ Status }: { Status: string }) {
  const Good = Status === 'applied' || Status === 'committed'
  const Bad = Status === 'failed'
  return <span style={{ padding: '0.2rem 0.5rem', borderRadius: '4px', fontSize: '0.85rem', fontWeight: 600, background: Good ? '#e6f4ea' : Bad ? '#fce8e6' : '#fef7e0', color: Good ? '#137333' : Bad ? '#c5221f' : '#b06000' }}>{Status}</span>
}

function ExecutionBadge({ Status }: { Status: string }) {
  const Good = Status === 'succeeded'
  const Bad = Status === 'failed' || Status === 'partial' || Status === 'partially_reverted'
  return <span style={{ padding: '0.2rem 0.5rem', borderRadius: '4px', fontSize: '0.85rem', fontWeight: 600, background: Good ? '#e6f4ea' : Bad ? '#fce8e6' : '#fef7e0', color: Good ? '#137333' : Bad ? '#c5221f' : '#b06000' }}>{Status}</span>
}

function SQLBlock({ Label, SQL, OnAnalyze, Pending }: { Label: string; SQL: string; OnAnalyze?: () => void; Pending?: boolean }) {
  return (
    <div style={{ marginBottom: '1rem' }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '0.4rem' }}>
        <strong>{Label}:</strong>
        {OnAnalyze && <button type="button" className="secondary" disabled={Pending} onClick={OnAnalyze} style={{ fontSize: '0.8rem', minHeight: '28px', padding: '0 8px' }}>{Pending ? 'Analyzing…' : 'Analyze Plan (EXPLAIN)'}</button>}
      </div>
      <pre style={{ background: '#1e1e1e', color: '#d4d4d4', padding: '1rem', borderRadius: '4px', overflowX: 'auto', fontSize: '0.9rem', fontFamily: 'monospace' }}>{SQL}</pre>
    </div>
  )
}

function SQLInput({ Label, Value, OnChange, OnAnalyze, Pending }: { Label: string; Value: string; OnChange: (Value: string) => void; OnAnalyze?: () => void; Pending?: boolean }) {
  const ID = `migration-${Label.toLowerCase().replaceAll(' ', '-')}`
  return (
    <div style={{ marginBottom: '1rem' }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '0.3rem' }}>
        <label htmlFor={ID}><strong>{Label}:</strong></label>
        {OnAnalyze && <button type="button" className="secondary" disabled={Pending || !Value.trim()} onClick={OnAnalyze} style={{ fontSize: '0.8rem', minHeight: '28px', padding: '0 8px' }}>{Pending ? 'Analyzing…' : 'Analyze Plan (EXPLAIN)'}</button>}
      </div>
      <textarea id={ID} value={Value} onChange={E => OnChange(E.target.value)} required rows={10} style={{ width: '100%', fontFamily: 'monospace', fontSize: '0.9rem', padding: '0.75rem', boxSizing: 'border-box', background: '#2d3748', color: '#f7fafc', borderRadius: '4px' }} />
    </div>
  )
}

function ExecutionDetails({ Execution }: { Execution: MigrationExecution }) {
  return (
    <div style={{ background: '#fff', padding: '1rem', border: '1px solid #ccc', borderRadius: '6px', marginTop: '1rem' }}>
      <h3>Execution {Execution.id}</h3>
      <p style={{ fontSize: '0.9rem' }}>
        <strong>Action:</strong> {ActionLabel(Execution.kind)} ({Execution.direction}) &nbsp;|&nbsp;
        <strong>Status:</strong> {Execution.status} &nbsp;|&nbsp;
        <strong>Requested by:</strong> {Execution.requested_by_username ?? Execution.requested_by ?? '—'}
        {Execution.rollback_of_execution_id && <> &nbsp;|&nbsp; <strong>Rollback of:</strong> <code>{Execution.rollback_of_execution_id}</code></>}
        {Execution.retry_of_execution_id && <> &nbsp;|&nbsp; <strong>Retry of:</strong> <code>{Execution.retry_of_execution_id}</code></>}
      </p>
      <SQLBlock Label="Executed SQL Snapshot" SQL={Execution.sql_text} />
      {Execution.error_message && <p role="alert" style={{ color: '#c5221f' }}>{Execution.error_message}</p>}
      <div className="table-wrap"><table className="data-table">
        <thead><tr><th>Attempt</th><th>Status</th><th>Actor</th><th>Started</th><th>Duration</th><th>Error</th></tr></thead>
        <tbody>
          {Execution.attempts.map(Attempt => (
            <tr key={Attempt.id}>
              <td>{Attempt.attempt_no}</td>
              <td>{Attempt.status}</td>
              <td>{Attempt.actor_username ?? Attempt.actor_id ?? '—'}</td>
              <td>{new Date(Attempt.started_at).toLocaleString()}</td>
              <td>{Attempt.duration_ms == null ? '—' : `${Attempt.duration_ms} ms`}</td>
              <td>{Attempt.error_message ?? '—'}</td>
            </tr>
          ))}
        </tbody>
      </table></div>
    </div>
  )
}

function PlanAnalysisReport({ Analysis, onDismiss }: { Analysis: QueryPlanAnalysis; onDismiss?: () => void }) {
  return (
    <div style={{ background: '#f7f8f5', border: '1px solid var(--line)', borderRadius: '4px', padding: '1rem', marginTop: '0.75rem', marginBottom: '1rem', fontSize: '0.85rem' }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '0.75rem' }}>
        <strong>Execution Plan Analysis (EXPLAIN ANALYZE)</strong>
        {onDismiss && <button type="button" className="ghost" style={{ fontSize: '0.8rem', minHeight: 'auto', padding: '2px 6px' }} onClick={onDismiss}>Dismiss</button>}
      </div>
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(130px, 1fr))', gap: '0.75rem', marginBottom: '0.75rem' }}>
        <Metric Label="Estimated Cost" Value={Analysis.estimated_cost.toFixed(2)} />
        <Metric Label="Execution Time" Value={`${Analysis.actual_execution_time.toFixed(3)} ms`} />
        <Metric Label="Planning Time" Value={`${Analysis.planning_time.toFixed(3)} ms`} />
        <Metric Label="Rows Processed" Value={String(Analysis.rows_processed)} />
      </div>
      <div style={{ display: 'flex', flexDirection: 'column', gap: '0.25rem', marginBottom: '0.5rem' }}>
        <div><strong>Scan Types:</strong> {Analysis.scan_types.length ? Analysis.scan_types.join(', ') : 'None'}</div>
        <div><strong>Indexes Used:</strong> {Analysis.indexes_used.length ? Analysis.indexes_used.join(', ') : 'None'}</div>
        <div><strong>Join Strategies:</strong> {Analysis.join_strategies.length ? Analysis.join_strategies.join(', ') : 'None'}</div>
      </div>
      <details>
        <summary style={{ cursor: 'pointer', color: 'var(--accent)', fontWeight: 500 }}>View Raw Plan JSON</summary>
        <pre style={{ background: '#1e1e1e', color: '#d4d4d4', padding: '0.75rem', borderRadius: '4px', overflowX: 'auto', maxHeight: '200px', marginTop: '0.4rem', fontSize: '0.8rem', fontFamily: 'monospace' }}>{JSON.stringify(Analysis.raw_plan, null, 2)}</pre>
      </details>
    </div>
  )
}

function Metric({ Label, Value }: { Label: string; Value: string }) {
  return (
    <div style={{ background: '#fff', padding: '0.5rem', borderRadius: '4px', border: '1px solid var(--line)' }}>
      <div style={{ color: 'var(--muted)', fontSize: '0.75rem' }}>{Label}</div>
      <div style={{ fontWeight: 600 }}>{Value}</div>
    </div>
  )
}
