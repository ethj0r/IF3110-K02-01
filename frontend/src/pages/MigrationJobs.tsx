import type { MigrationJob } from '../api/client'
import { ActionLabel, IsJobActive } from '../api/jobs'
import { Card, Metric } from '../ui/Card'
import { Notice, StateBadge } from '../ui/Feedback'

export function JobPanel({ Job, OnClose }: { Job: MigrationJob; OnClose?: () => void }) {
  const Active = IsJobActive(Job)
  const Label = `${ActionLabel(Job.action)} job`
  const Reverts = Job.action === 'rollback'
  return (
    <Card
      Label={Label}
      Title={<>{Label} <span className="muted">· v{Job.version}</span></>}
      Actions={(
        <>
          <StateBadge Status={Job.status} />
          {IsPartial(Job) && <StateBadge Status={Job.execution_status!} />}
          {!Active && OnClose && <button type="button" className="btn btn-ghost btn-sm" onClick={OnClose}>Close</button>}
        </>
      )}
    >
      <span className="sr-only" role="status">{Label} {Job.status}{IsPartial(Job) ? `, ${Job.execution_status}` : ''}</span>
      {Active && <div className="progress" role="progressbar" aria-label={`${Label} progress`} />}
      <dl className="meta-grid">
        <div><dt>Migration</dt><dd>{Job.title}</dd></div>
        <div><dt>Attempt</dt><dd className="mono">{Job.attempts}/{Job.max_attempts}</dd></div>
        <div><dt>{Active ? 'Elapsed' : 'Duration'}</dt><dd className="mono">{FormatDuration(Job)}</dd></div>
        <div><dt>Requested by</dt><dd>{Job.requested_by_username ?? '—'}</dd></div>
        {Job.locked_by && <div><dt>Worker</dt><dd className="mono">{Job.locked_by}</dd></div>}
        {Job.execution_id && <div><dt>Execution</dt><dd><code title={Job.execution_id}>{Job.execution_id.slice(0, 8)}</code></dd></div>}
      </dl>
      {Job.mode === 'batch' && (Reverts ? (
        <div className="metrics">
          <Metric Label="Reverted batches" Value={FormatCount(Job.reverted_batches)} />
          <Metric Label="Reverted rows" Value={FormatCount(Job.reverted_rows)} />
        </div>
      ) : (
        <div className="metrics">
          <Metric Label="Committed batches" Value={FormatCount(Job.committed_batches)} />
          <Metric Label="Committed rows" Value={FormatCount(Job.committed_rows)} />
          <Metric Label="Checkpoint" Value={FormatCount(Job.checkpoint_cursor)} />
        </div>
      ))}
      {Job.last_error && (Job.status === 'failed'
        ? <Notice Tone="danger" Role="alert">{Job.last_error}</Notice>
        : Active && <Notice Tone="warning">Retrying: {Job.last_error}</Notice>)}
    </Card>
  )
}

export function RecentJobs({ Jobs, SelectedID, OnSelect }: {
  Jobs: MigrationJob[]
  SelectedID?: string
  OnSelect: (ID: string | null) => void
}) {
  return (
    <Card Title="Jobs" Flush>
      <div className="table-scroll">
        <table className="table">
          <thead>
            <tr>
              <th>Job</th>
              <th>Version</th>
              <th>Action</th>
              <th>Status</th>
              <th>Attempts</th>
              <th>Batches</th>
              <th>Rows</th>
              <th>Queued</th>
              <th className="cell-actions"><span className="sr-only">Details</span></th>
            </tr>
          </thead>
          <tbody>
            {Jobs.map(Job => (
              <tr key={Job.id} className={Job.id === SelectedID ? 'is-selected' : undefined}>
                <td><code title={Job.id}>{Job.id.slice(0, 8)}</code></td>
                <td>v{Job.version} · {Job.title}</td>
                <td className="cell-nowrap">{ActionLabel(Job.action)}</td>
                <td className="cell-nowrap">
                  <StateBadge Status={Job.status} />
                  {IsPartial(Job) && <> <StateBadge Status={Job.execution_status!} /></>}
                </td>
                <td className="mono">{Job.attempts}/{Job.max_attempts}</td>
                <td className="mono cell-nowrap">{Job.mode === 'batch' ? BatchCount(Job, 'batches') : '—'}</td>
                <td className="mono cell-nowrap">{Job.mode === 'batch' ? BatchCount(Job, 'rows') : '—'}</td>
                <td className="muted cell-nowrap">{new Date(Job.created_at).toLocaleString()}</td>
                <td className="cell-actions">
                  <button className="btn btn-ghost btn-sm" type="button" onClick={() => OnSelect(Job.id === SelectedID ? null : Job.id)}>
                    {Job.id === SelectedID ? 'Hide' : 'Inspect'}
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </Card>
  )
}

function IsPartial(Job: MigrationJob) {
  return Job.execution_status === 'partial' || Job.execution_status === 'partially_reverted'
}

function BatchCount(Job: MigrationJob, Unit: 'batches' | 'rows') {
  const Committed = Unit === 'batches' ? Job.committed_batches : Job.committed_rows
  const Reverted = Unit === 'batches' ? Job.reverted_batches : Job.reverted_rows
  return Job.action === 'rollback' ? `${FormatCount(Reverted)} reverted` : FormatCount(Committed)
}

function FormatCount(Value: number) {
  return Value.toLocaleString()
}

function FormatDuration(Job: MigrationJob) {
  const Start = Date.parse(Job.locked_at ?? Job.created_at)
  const End = Job.finished_at ? Date.parse(Job.finished_at) : Date.now()
  const Seconds = Math.max(0, (End - Start) / 1000)
  if (Seconds < 60) return `${Seconds.toFixed(1)} s`
  const Minutes = Math.floor(Seconds / 60)
  return `${Minutes} min ${Math.round(Seconds % 60)} s`
}
