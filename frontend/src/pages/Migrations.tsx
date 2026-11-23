import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef, useState } from 'react'
import { useLocation, useNavigate, useParams } from 'react-router-dom'
import { ReadGeneratedMigration } from '../api/schemaDiff'
import type { GeneratedMigration } from '../api/schemaDiff'
import { AccessQueryKeys, ListAccessibleDatabases } from '../api/access'
import {
  AnalyzeQueryPlan,
  ApplyMigration,
  ApproveMigrationReview,
  CreateMigration,
  DeleteMigrationDraft,
  ForwardMigration,
  GetMigrationJob,
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
import { DatabaseTabs } from '../layout/DatabaseTabs'
import { Card, CodeBlock, PageHeader } from '../ui/Card'
import { ConfirmDialog } from '../ui/ConfirmDialog'
import { Badge, EmptyState, Loading, Notice, RequestError, StateBadge } from '../ui/Feedback'
import { Icon } from '../ui/Icon'
import { ErrorMessage } from '../ui/errors'

const ReviewActions: ExecutionKind[] = ['apply', 'rollback', 'forward', 'retry']

type PendingConfirmation =
  | { Kind: 'action'; Item: Migration; Action: ExecutionKind }
  | { Kind: 'delete'; Item: Migration }

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
  const [SelectedMigrationID, SetSelectedMigrationID] = useState<string | null>(null)
  const [SelectedExecutionID, SetSelectedExecutionID] = useState<string | null>(null)
  const [ReviewSelection, SetReviewSelection] = useState<{ ID: string; Action: ExecutionKind } | null>(null)
  const [IsEditorOpen, SetIsEditorOpen] = useState(Boolean(InitialDraft))
  const [EditingMigration, SetEditingMigration] = useState<Migration | null>(null)
  const [EditorTitle, SetEditorTitle] = useState(InitialDraft?.Title ?? '')
  const [EditorSQL, SetEditorSQL] = useState(InitialDraft?.SQLText ?? '')
  const [EditorDownSQL, SetEditorDownSQL] = useState('')
  const [EditorIrreversible, SetEditorIrreversible] = useState(false)
  const [EditorReason, SetEditorReason] = useState('')
  const [EditorBatchMode, SetEditorBatchMode] = useState(false)
  const [EditorBatchColumn, SetEditorBatchColumn] = useState('id')
  const [EditorBatchSize, SetEditorBatchSize] = useState(1000)
  const [HideRollback, SetHideRollback] = useState(false)
  const [ExecutionError, SetExecutionError] = useState<string | null>(null)
  const [SuccessMessage, SetSuccessMessage] = useState<string | null>(null)
  const [ActiveJobID, SetActiveJobID] = useState<string | null>(null)
  const [Confirmation, SetConfirmation] = useState<PendingConfirmation | null>(null)

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
        batch_mode: EditorBatchMode,
        batch_column: EditorBatchMode ? EditorBatchColumn : '',
        batch_size: EditorBatchMode ? Number(EditorBatchSize) : 0,
        irreversible: EditorIrreversible,
        irreversible_reason: EditorIrreversible ? EditorReason : '',
      }
      if (EditingMigration) return UpdateMigrationDraft(ActiveDatabase, EditingMigration.id, Input)
      return CreateMigration(ActiveDatabase, Input)
    },
    onSuccess: () => {
      SetIsEditorOpen(false)
      SetEditingMigration(null)
      ResetEditor()
      SetSuccessMessage('Migration draft saved successfully.')
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
    onSuccess: Job => {
      SetReviewSelection(null)
      SetExecutionError(null)
      SetConfirmation(null)
      SetSuccessMessage(null)
      SetActiveJobID(Job.id)
    },
    onError: Error => {
      SetConfirmation(null)
      SetExecutionError(ErrorMessage(Error, 'Unable to queue the migration action.'))
      Refresh()
    },
  })

  const JobQuery = useQuery({
    queryKey: ['migration-job', ActiveDatabase, ActiveJobID],
    queryFn: ({ signal }) => GetMigrationJob(ActiveDatabase, ActiveJobID!, signal),
    enabled: Boolean(Database && ActiveJobID),
    refetchInterval: Query => {
      const Status = Query.state.data?.status
      return Status === 'succeeded' || Status === 'failed' || Status === 'cancelled' ? false : 1000
    },
  })

  const Job = JobQuery.data
  const JobFinished = Boolean(Job && (Job.status === 'succeeded' || Job.status === 'failed' || Job.status === 'cancelled'))
  const JobActive = Boolean(ActiveJobID) && !JobFinished

  useEffect(() => {
    if (!JobFinished) return
    void QueryCache.invalidateQueries({ queryKey: ['migrations', ActiveDatabase] })
    void QueryCache.invalidateQueries({ queryKey: ['databases', ActiveDatabase] })
  }, [JobFinished, QueryCache, ActiveDatabase])

  const DeleteMutation = useMutation({
    mutationFn: (ID: string) => DeleteMigrationDraft(ActiveDatabase, ID),
    onSuccess: () => {
      SetConfirmation(null)
      SetSuccessMessage('Draft migration deleted.')
      SetSelectedMigrationID(null)
      Refresh()
    },
    onError: () => SetConfirmation(null),
  })

  const DetailsAnalyze = useMutation({ mutationFn: (SQLText: string) => AnalyzeQueryPlan(ActiveDatabase, SQLText) })
  const EditorAnalyze = useMutation({ mutationFn: (SQLText: string) => AnalyzeQueryPlan(ActiveDatabase, SQLText) })
  const Busy = SaveMutation.isPending || ActionMutation.isPending || DeleteMutation.isPending || JobActive

  function ResetEditor() {
    SetEditorTitle('')
    SetEditorSQL('')
    SetEditorDownSQL('')
    SetEditorIrreversible(false)
    SetEditorReason('')
    SetEditorBatchMode(false)
    SetEditorBatchColumn('id')
    SetEditorBatchSize(1000)
  }

  function OpenCreateEditor() {
    SetEditingMigration(null)
    ResetEditor()
    SetIsEditorOpen(true)
    SetExecutionError(null)
    EditorAnalyze.reset()
  }

  function OpenEditEditor(Item: Migration) {
    SetEditingMigration(Item)
    SetEditorTitle(Item.title)
    SetEditorSQL(Item.sql_text)
    SetEditorDownSQL(Item.down_sql ?? '')
    SetEditorIrreversible(Item.irreversible)
    SetEditorReason(Item.irreversible_reason ?? '')
    SetEditorBatchMode(Item.batch_mode)
    SetEditorBatchColumn(Item.batch_column ?? 'id')
    SetEditorBatchSize(Item.batch_size ?? 1000)
    SetIsEditorOpen(true)
    SetExecutionError(null)
    EditorAnalyze.reset()
  }

  function ConfirmPending() {
    if (!Confirmation) return
    SetSuccessMessage(null)
    SetExecutionError(null)
    if (Confirmation.Kind === 'delete') DeleteMutation.mutate(Confirmation.Item.id)
    else ActionMutation.mutate({ ID: Confirmation.Item.id, Action: Confirmation.Action })
  }

  return (
    <>
      <PageHeader
        Crumbs={Database ? [{ Label: 'Databases', To: '/' }, { Label: Database.name, To: `/databases/${Database.id}` }] : undefined}
        Title="Database Migrations"
        Actions={Database && CanEdit ? (
          <button className="btn" type="button" disabled={Busy} onClick={OpenCreateEditor}><Icon Name="plus" Size={14} />New migration</button>
        ) : undefined}
      />

      {Database && <DatabaseTabs Database={Database} />}

      <Card As="div">
        <div className="toolbar">
          <div className="field">
            <label htmlFor="target-db-id">Database</label>
            <select
              id="target-db-id"
              className="select"
              value={ActiveDatabase}
              disabled={Busy || Databases.isPending}
              onChange={Event => void NavigateTo(Event.target.value ? `/databases/${encodeURIComponent(Event.target.value)}/migrations` : '/migrations')}
            >
              <option value="">Select a database…</option>
              {Databases.data?.map(Item => <option key={Item.id} value={Item.id}>{Item.name}</option>)}
            </select>
          </div>
        </div>
        <RequestError error={Databases.error} />
        {Databases.data?.length === 0 && <p className="muted">No databases assigned.</p>}
        {ActiveDatabase && !Databases.isPending && !Databases.isError && !Database && <Notice Tone="danger" Role="alert">No access to this database.</Notice>}
      </Card>

      {!ActiveDatabase && Databases.data && Databases.data.length > 0 && (
        <div className="card">
          <EmptyState IconName="layers" Title="Choose a database" />
        </div>
      )}

      {SuccessMessage && <Notice Tone="success" Role="status">{SuccessMessage}</Notice>}
      {ExecutionError && (
        <Notice Tone="danger" Role="alert"><strong>Migration action failed:</strong> {ExecutionError}</Notice>
      )}

      {IsEditorOpen && CanEdit && (
        <Card
          Title={EditingMigration ? `Edit migration v${EditingMigration.version}` : 'New migration'}
        >
          <form className="stack" onSubmit={Event => { Event.preventDefault(); SaveMutation.mutate() }}>
            <div className="field">
              <label htmlFor="migration-title">Title</label>
              <input id="migration-title" className="input" value={EditorTitle} onChange={Event => SetEditorTitle(Event.target.value)} placeholder="e.g. Create users table" required maxLength={128} />
            </div>
            <label className="check">
              <input type="checkbox" checked={EditorBatchMode} onChange={Event => SetEditorBatchMode(Event.target.checked)} />
              Batch mode
            </label>
            {EditorBatchMode && (
              <div className="form-grid">
                <div className="field">
                  <label htmlFor="migration-batch-column">Cursor column</label>
                  <input id="migration-batch-column" className="input" value={EditorBatchColumn} onChange={Event => SetEditorBatchColumn(Event.target.value)} required maxLength={63} />
                </div>
                <div className="field">
                  <label htmlFor="migration-batch-size">Batch size</label>
                  <input id="migration-batch-size" className="input" type="number" min={1} max={1000000} value={EditorBatchSize} onChange={Event => SetEditorBatchSize(Number(Event.target.value))} required />
                </div>
              </div>
            )}
            {EditorBatchMode && <p className="muted">Use one statement per up/down script. Up SQL receives <code>$1</code> as the cursor and <code>$2</code> as the batch size, and must return the cursor column. Down SQL receives <code>$1</code> as cursor_to and <code>$2</code> as cursor_from for each committed batch.</p>}
            <SQLInput Label="Up SQL" Value={EditorSQL} OnChange={Value => { SetEditorSQL(Value); if (EditorAnalyze.data || EditorAnalyze.error) EditorAnalyze.reset() }} OnAnalyze={() => EditorAnalyze.mutate(EditorSQL)} Pending={EditorAnalyze.isPending} />
            <label className="check">
              <input type="checkbox" checked={EditorIrreversible} onChange={Event => SetEditorIrreversible(Event.target.checked)} />
              Irreversible migration (no down SQL)
            </label>
            {EditorIrreversible ? (
              <div className="field">
                <label htmlFor="migration-reason">Irreversible reason</label>
                <input id="migration-reason" className="input" value={EditorReason} onChange={Event => SetEditorReason(Event.target.value)} required maxLength={500} />
              </div>
            ) : (
              <SQLInput Label="Down SQL" Value={EditorDownSQL} OnChange={SetEditorDownSQL} />
            )}
            {EditorAnalyze.isError && <Notice Tone="danger" Role="alert">Plan analysis failed: {ErrorMessage(EditorAnalyze.error, 'Unable to analyze query plan.')}</Notice>}
            {EditorAnalyze.data && <PlanAnalysisReport Analysis={EditorAnalyze.data} OnDismiss={() => EditorAnalyze.reset()} />}
            {SaveMutation.isError && <Notice Tone="danger" Role="alert">{ErrorMessage(SaveMutation.error, 'Failed to save migration.')}</Notice>}
            <div className="form-actions">
              <button className="btn" type="submit" disabled={SaveMutation.isPending}>{SaveMutation.isPending ? 'Saving…' : 'Save Draft'}</button>
              <button className="btn btn-secondary" type="button" onClick={() => SetIsEditorOpen(false)} disabled={SaveMutation.isPending}>Cancel</button>
            </div>
          </form>
        </Card>
      )}
      {ActiveJobID && Job && (
        <Notice Tone={Job.status === 'failed' ? 'danger' : Job.status === 'succeeded' ? 'success' : 'warning'} Role={Job.status === 'failed' ? 'alert' : 'status'}>
          <strong>{ActionLabel(Job.action)} job:</strong> {Job.status} (attempt {Job.attempts}/{Job.max_attempts})
          {Job.execution_id && <> · execution <code>{Job.execution_id.slice(0, 8)}</code></>}
          {Job.mode === 'batch' && <> · {Job.committed_batches} batch(es) / {Job.committed_rows} row(s) committed{Job.reverted_batches > 0 ? `, ${Job.reverted_batches} batch(es) / ${Job.reverted_rows} row(s) reverted` : ''}</>}
          {Job.last_error && <div>{Job.last_error}</div>}
          {JobFinished && <button type="button" className="btn btn-ghost btn-sm" onClick={() => SetActiveJobID(null)}>Dismiss</button>}
        </Notice>
      )}
      {ActiveJobID && JobQuery.isError && (
        <Notice Tone="danger" Role="alert">Unable to read migration job status.</Notice>
      )}

      {Database && (
        <Card Title="Definitions" Flush>
          {HasPending && <div className="card-body"><Notice Tone="warning" Role="status">An execution is pending. Verify the target outcome before retrying.</Notice></div>}
          {MigrationsQuery.isPending && <div className="card-body"><Loading>Loading migrations…</Loading></div>}
          {MigrationsQuery.isError && <div className="card-body"><Notice Tone="danger" Role="alert">{ErrorMessage(MigrationsQuery.error, 'Failed to load migrations.')}</Notice></div>}
          {MigrationsQuery.data && MigrationsQuery.data.length === 0 && (
            <EmptyState IconName="layers" Title="No migrations yet" />
          )}
          {MigrationsQuery.data && MigrationsQuery.data.length > 0 && (
            <div className="table-scroll">
              <table className="table">
                <thead>
                  <tr>
                    <th>Version</th>
                    <th>Title</th>
                    <th>Status</th>
                    <th>Revision</th>
                    <th>Updated</th>
                    <th className="cell-actions">Actions</th>
                  </tr>
                </thead>
                <tbody>
                  {MigrationsQuery.data.map(Item => (
                    <tr key={Item.id} className={Item.id === SelectedMigrationID ? 'is-selected' : undefined}>
                      <td className="mono cell-nowrap">v{Item.version}</td>
                      <td>
                        {Item.title}
                        {Item.batch_mode && <> <Badge Tone="outline">batch {Item.batch_column} × {Item.batch_size}</Badge></>}
                        {Item.irreversible && <> <Badge Tone="outline">irreversible</Badge></>}
                      </td>
                      <td><StateBadge Status={Item.status} /></td>
                      <td className="mono">{Item.revision}</td>
                      <td className="muted cell-nowrap">{new Date(Item.updated_at).toLocaleString()}</td>
                      <td className="cell-actions">
                        <div className="row-actions">
                          <button className="btn btn-secondary btn-sm" type="button" onClick={() => { SetSelectedMigrationID(Item.id); DetailsAnalyze.reset() }}>View</button>
                          {CanEdit && Item.status === 'draft' && (
                            <>
                              <button className="btn btn-ghost btn-sm" type="button" disabled={Busy} onClick={() => OpenEditEditor(Item)}>Edit</button>
                              <button className="btn btn-ghost btn-sm" type="button" disabled={Busy} onClick={() => SetConfirmation({ Kind: 'delete', Item })}>Delete</button>
                            </>
                          )}
                          {ReviewActions.filter(Action => Item[`can_${Action}`] || (Action === 'apply' && Item.status === 'draft')).map(Action => (
                            <button key={Action} className="btn btn-sm" type="button" disabled={Busy || !Item[`can_${Action}`]}
                              onClick={() => SetReviewSelection({ ID: Item.id, Action })}>
                              Review {Action}
                            </button>
                          ))}
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </Card>
      )}

      {Database && ReviewSelection && ReviewMigration && (
        <MigrationReviewPanel
          key={`${ReviewMigration.id}:${ReviewMigration.revision}:${ReviewSelection.Action}`}
          Item={ReviewMigration} Action={ReviewSelection.Action} CanEdit={CanEdit} Busy={Busy}
          OnClose={() => SetReviewSelection(null)}
          OnExecute={() => SetConfirmation({ Kind: 'action', Item: ReviewMigration, Action: ReviewSelection.Action })}
        />
      )}

      {SelectedMigration && (
        <Card
          Label="Migration details"
          Title={`v${SelectedMigration.version} · ${SelectedMigration.title}`}
          Actions={<button className="btn btn-secondary btn-sm" type="button" onClick={() => SetSelectedMigrationID(null)}>Close</button>}
        >
          <dl className="meta-grid">
            <div><dt>Status</dt><dd><StateBadge Status={SelectedMigration.status} /></dd></div>
            <div><dt>Revision</dt><dd className="mono">{SelectedMigration.revision}</dd></div>
            <div><dt>Updated</dt><dd>{new Date(SelectedMigration.updated_at).toLocaleString()}</dd></div>
            {SelectedMigration.executed_at && <div><dt>Last applied</dt><dd>{new Date(SelectedMigration.executed_at).toLocaleString()}</dd></div>}
          </dl>
          {SelectedMigration.error_message && (
            <Notice Tone="danger" Role="alert"><strong>Last error:</strong> {SelectedMigration.error_message}</Notice>
          )}
          <SQLBlock Label="Up SQL" SQL={SelectedMigration.sql_text} OnAnalyze={() => DetailsAnalyze.mutate(SelectedMigration.sql_text)} Pending={DetailsAnalyze.isPending} />
          <SQLBlock Label={SelectedMigration.irreversible ? 'Down SQL (irreversible)' : 'Down SQL'} SQL={SelectedMigration.down_sql ?? '—'} />
          {SelectedMigration.irreversible && <p className="muted">Reason: {SelectedMigration.irreversible_reason}</p>}
          {DetailsAnalyze.isError && <Notice Tone="danger" Role="alert">Plan analysis failed: {ErrorMessage(DetailsAnalyze.error, 'Unable to analyze query plan.')}</Notice>}
          {DetailsAnalyze.data && <PlanAnalysisReport Analysis={DetailsAnalyze.data} OnDismiss={() => DetailsAnalyze.reset()} />}
        </Card>
      )}

      {Database && (
        <Card
          Title="History"
          Actions={(
            <label className="check">
              <input type="checkbox" checked={HideRollback} onChange={Event => SetHideRollback(Event.target.checked)} />
              Hide rollback
            </label>
          )}
          Flush
        >
          {HistoryQuery.isPending && <div className="card-body"><Loading>Loading execution history…</Loading></div>}
          {HistoryQuery.isError && <div className="card-body"><Notice Tone="danger" Role="alert">Failed to load execution history.</Notice></div>}
          {HistoryQuery.data && HistoryQuery.data.length === 0 && (
            <EmptyState IconName="history" Title="No execution attempts recorded." />
          )}
          {HistoryQuery.data && HistoryQuery.data.length > 0 && (
            <div className="table-scroll">
              <table className="table">
                <thead>
                  <tr>
                    <th>Execution</th>
                    <th>Version</th>
                    <th>Action</th>
                    <th>Status</th>
                    <th>Attempts</th>
                    <th>Requested by</th>
                    <th>Started</th>
                    <th className="cell-actions"><span className="sr-only">Details</span></th>
                  </tr>
                </thead>
                <tbody>
                  {HistoryQuery.data.map(Item => (
                    <tr key={Item.id} className={Item.id === SelectedExecutionID ? 'is-selected' : undefined}>
                      <td><code title={Item.id}>{Item.id.slice(0, 8)}</code></td>
                      <td>v{Item.version} · {Item.title}</td>
                      <td className="cell-nowrap">{ActionLabel(Item.kind)}{Item.direction === 'down' ? ' (down)' : ''}</td>
                      <td><StateBadge Status={Item.status} /></td>
                      <td className="mono">{Item.attempts.length}</td>
                      <td>{Item.requested_by_username ?? Item.requested_by ?? '—'}</td>
                      <td className="muted cell-nowrap">{new Date(Item.started_at ?? Item.created_at).toLocaleString()}</td>
                      <td className="cell-actions">
                        <button className="btn btn-ghost btn-sm" type="button" onClick={() => SetSelectedExecutionID(Item.id === SelectedExecutionID ? null : Item.id)}>
                          {Item.id === SelectedExecutionID ? 'Hide' : 'Inspect'}
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
          {SelectedExecution && <ExecutionDetails Execution={SelectedExecution} />}
        </Card>
      )}

      {Confirmation && (
        <ConfirmDialog
          Title={Confirmation.Kind === 'delete' ? 'Delete draft migration' : `${ActionLabel(Confirmation.Action)} migration`}
          Description={Confirmation.Kind === 'delete'
            ? `Delete draft migration v${Confirmation.Item.version}? This cannot be undone.`
            : `${ActionLabel(Confirmation.Action)} migration v${Confirmation.Item.version} ("${Confirmation.Item.title}") on ${Database?.name ?? 'this database'}?`}
          ConfirmLabel={Confirmation.Kind === 'delete' ? 'Delete draft' : `Run ${Confirmation.Action}`}
          Pending={Busy}
          OnConfirm={ConfirmPending}
          OnCancel={() => SetConfirmation(null)}
        />
      )}
    </>
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
  const ApprovedCount = Current?.groups.filter(Group => Group.approved).length ?? 0

  return (
    <section className="card" aria-labelledby="migration-review-heading">
      <header className="card-header">
        <div className="card-title">
          <h2 id="migration-review-heading" ref={Heading} tabIndex={-1}>{ActionLabel(Action)} review for v{Item.version}</h2>
        </div>
        <div className="card-actions">
          <button className="btn btn-secondary btn-sm" type="button" onClick={OnClose}>Close review</button>
        </div>
      </header>
      <div className="card-body">
        {Review.isPending && <Loading>Loading review…</Loading>}
        <RequestError error={Review.error ?? Submit.error ?? Approve.error} />
        {Current && !Review.isError && (
          <>
            <Notice Tone={Current.status === 'approved' ? 'success' : 'neutral'} Role="status">
              {Current.status === 'approved' ? 'All groups approved' : 'Waiting for group approvals'} · Revision {Current.revision} · {ApprovedCount}/{Current.groups.length} approved
            </Notice>
            <SQLBlock Label="SQL for review" SQL={Current.sql_text} />
            <ul className="list">
              {Current.groups.map(Group => (
                <li key={Group.id} className="list-item">
                  <div className="list-item-main">
                    <span className="avatar" aria-hidden="true">{Group.name.slice(0, 1)}</span>
                    <strong>{Group.name}</strong>
                    <Badge Tone={Group.approved ? 'success' : Group.leader_user_id ? 'warning' : 'outline'} Dot>
                      {Group.approved ? 'Approved' : Group.leader_user_id ? 'Awaiting approval' : 'No active leader'}
                    </Badge>
                  </div>
                  {Group.can_approve && (
                    <button className="btn btn-secondary btn-sm" type="button" disabled={Pending || !Available}
                      onClick={() => Approve.mutate({ ReviewID: Current.id, GroupID: Group.id })}>
                      <Icon Name="check" Size={14} />Approve for {Group.name}
                    </button>
                  )}
                </li>
              ))}
            </ul>
          </>
        )}
        {CanEdit && (
          <div className="form-actions">
            {Current === null && <button className="btn btn-secondary" type="button" disabled={Pending || !Available} onClick={() => Submit.mutate()}>Request review</button>}
            <button className="btn" type="button" disabled={Pending || !Available || Current?.status !== 'approved'} onClick={OnExecute}>{ActionLabel(Action)}</button>
          </div>
        )}
      </div>
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

function SQLBlock({ Label, SQL, OnAnalyze, Pending }: { Label: string; SQL: string; OnAnalyze?: () => void; Pending?: boolean }) {
  return (
    <CodeBlock
      Label={Label}
      Code={SQL}
      Actions={OnAnalyze && (
        <button type="button" className="btn btn-secondary btn-sm" disabled={Pending} onClick={OnAnalyze}>
          <Icon Name="search" Size={14} />{Pending ? 'Analyzing…' : 'Analyze Plan (EXPLAIN)'}
        </button>
      )}
    />
  )
}

function SQLInput({ Label, Value, OnChange, OnAnalyze, Pending }: { Label: string; Value: string; OnChange: (Value: string) => void; OnAnalyze?: () => void; Pending?: boolean }) {
  const ID = `migration-${Label.toLowerCase().replaceAll(' ', '-')}`
  return (
    <div className="field">
      <div className="code-block-header">
        <label htmlFor={ID}>{Label}</label>
        {OnAnalyze && (
          <button type="button" className="btn btn-secondary btn-sm" disabled={Pending || !Value.trim()} onClick={OnAnalyze}>
            <Icon Name="search" Size={14} />{Pending ? 'Analyzing…' : 'Analyze Plan (EXPLAIN)'}
          </button>
        )}
      </div>
      <textarea id={ID} className="textarea code-input" value={Value} onChange={Event => OnChange(Event.target.value)} required rows={10} spellCheck={false} />
    </div>
  )
}

function ExecutionDetails({ Execution }: { Execution: MigrationExecution }) {
  return (
    <div className="card-body execution-details">
      <hr className="divider" />
      <div className="card-title">
        <h3>Execution <span className="mono muted">{Execution.id}</span></h3>
      </div>
      <dl className="meta-grid">
        <div><dt>Action</dt><dd>{ActionLabel(Execution.kind)} ({Execution.direction})</dd></div>
        <div><dt>Status</dt><dd><StateBadge Status={Execution.status} /></dd></div>
        <div><dt>Requested by</dt><dd>{Execution.requested_by_username ?? Execution.requested_by ?? '—'}</dd></div>
        {Execution.rollback_of_execution_id && <div><dt>Rollback of</dt><dd><code>{Execution.rollback_of_execution_id}</code></dd></div>}
        {Execution.retry_of_execution_id && <div><dt>Retry of</dt><dd><code>{Execution.retry_of_execution_id}</code></dd></div>}
      </dl>
      {Execution.mode === 'batch' && (
        <dl className="meta-grid">
          <div><dt>Batch cursor</dt><dd className="mono">{Execution.batch_column ?? '—'}</dd></div>
          <div><dt>Batch size</dt><dd className="mono">{Execution.batch_size ?? '—'}</dd></div>
          <div><dt>Committed</dt><dd className="mono">{Execution.committed_batches} batch(es) / {Execution.committed_rows} row(s)</dd></div>
          <div><dt>Reverted</dt><dd className="mono">{Execution.reverted_batches} batch(es) / {Execution.reverted_rows} row(s)</dd></div>
          <div><dt>Checkpoint</dt><dd className="mono">{Execution.checkpoint_cursor}</dd></div>
        </dl>
      )}
      <SQLBlock Label="Executed SQL snapshot" SQL={Execution.sql_text} />
      {Execution.error_message && <Notice Tone="danger" Role="alert">{Execution.error_message}</Notice>}
      <div className="table-scroll inset-table">
        <table className="table">
          <thead><tr><th>Attempt</th><th>Status</th><th>Actor</th><th>Started</th><th>Duration</th><th>Error</th></tr></thead>
          <tbody>
            {Execution.attempts.map(Attempt => (
              <tr key={Attempt.id}>
                <td className="mono">{Attempt.attempt_no}</td>
                <td><StateBadge Status={Attempt.status} /></td>
                <td>{Attempt.actor_username ?? Attempt.actor_id ?? '—'}</td>
                <td className="muted cell-nowrap">{new Date(Attempt.started_at).toLocaleString()}</td>
                <td className="mono">{Attempt.duration_ms == null ? '—' : `${Attempt.duration_ms} ms`}</td>
                <td>{Attempt.error_message ?? '—'}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}

function PlanAnalysisReport({ Analysis, OnDismiss }: { Analysis: QueryPlanAnalysis; OnDismiss?: () => void }) {
  return (
    <div className="plan-report">
      <div className="code-block-header">
        <strong>Execution Plan Analysis (EXPLAIN ANALYZE)</strong>
        {OnDismiss && <button type="button" className="btn btn-ghost btn-sm" onClick={OnDismiss}>Dismiss</button>}
      </div>
      <div className="metrics">
        <Metric Label="Estimated cost" Value={Analysis.estimated_cost.toFixed(2)} />
        <Metric Label="Execution time" Value={`${Analysis.actual_execution_time.toFixed(3)} ms`} />
        <Metric Label="Planning time" Value={`${Analysis.planning_time.toFixed(3)} ms`} />
        <Metric Label="Rows processed" Value={String(Analysis.rows_processed)} />
      </div>
      <dl className="meta-grid">
        <div><dt>Scan types</dt><dd><TagList Items={Analysis.scan_types} /></dd></div>
        <div><dt>Indexes used</dt><dd><TagList Items={Analysis.indexes_used} /></dd></div>
        <div><dt>Join strategies</dt><dd><TagList Items={Analysis.join_strategies} /></dd></div>
      </dl>
      <details>
        <summary>View raw plan JSON</summary>
        <pre className="code-pre">{JSON.stringify(Analysis.raw_plan, null, 2)}</pre>
      </details>
    </div>
  )
}

function TagList({ Items }: { Items: string[] }) {
  if (Items.length === 0) return <span className="muted">None</span>
  return <span className="badge-list">{Items.map(Item => <Badge key={Item}>{Item}</Badge>)}</span>
}

function Metric({ Label, Value }: { Label: string; Value: string }) {
  return (
    <div className="metric">
      <span className="label">{Label}</span>
      <span className="metric-value">{Value}</span>
    </div>
  )
}
