import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import type { FormEvent } from 'react'
import { useState } from 'react'
import { AccessQueryKeys } from '../../api/access'
import { AdminQueryKeys } from '../../api/admin'
import {
  ConnectionQueryKeys,
  CreateConnection,
  DeleteConnection,
  ListConnections,
  TestConnection,
  UpdateConnection,
} from '../../api/connections'
import type { ConnectionInput, ManagedConnection } from '../../api/connections'
import { Card, PageHeader } from '../../ui/Card'
import { ConfirmDialog } from '../../ui/ConfirmDialog'
import { Badge, EmptyState, Loading, Notice as NoticeBanner, RequestError } from '../../ui/Feedback'
import { Icon } from '../../ui/Icon'

function readForm(Form: HTMLFormElement): ConnectionInput | null {
  const Values = new FormData(Form)
  const Port = Number(Values.get('port'))
  if (!Number.isInteger(Port) || Port < 1 || Port > 65535) return null
  return {
    name: String(Values.get('name') ?? '').trim(),
    host: String(Values.get('host') ?? '').trim(),
    port: Port,
    dbname: String(Values.get('dbname') ?? '').trim(),
    username: String(Values.get('username') ?? '').trim(),
    password: String(Values.get('password') ?? ''),
  }
}

export function ConnectionsPage() {
  const Cache = useQueryClient()
  const Connections = useQuery({ queryKey: ConnectionQueryKeys.connections, queryFn: ListConnections })
  const [Notice, SetNotice] = useState('')
  const [ShowCreate, SetShowCreate] = useState(false)
  const [SelectedID, SetSelectedID] = useState<string | null>(null)
  const [DeleteTarget, SetDeleteTarget] = useState<ManagedConnection | null>(null)
  const [TestedID, SetTestedID] = useState<string | null>(null)
  const Selected = Connections.data?.find(Item => Item.id === SelectedID) ?? null

  function Refresh() {
    void Cache.invalidateQueries({ queryKey: ConnectionQueryKeys.connections })
    void Cache.invalidateQueries({ queryKey: AccessQueryKeys.databases })
    void Cache.invalidateQueries({ queryKey: AdminQueryKeys.matrix })
  }

  const Create = useMutation({
    mutationFn: CreateConnection,
    onSuccess: () => { SetNotice('Connection created. Grant group access before it appears in the workspace.'); SetShowCreate(false); Refresh() },
  })
  const Update = useMutation({
    mutationFn: ({ ID, Input }: { ID: string; Input: ConnectionInput }) => UpdateConnection(ID, Input),
    onSuccess: () => { SetNotice('Connection updated.'); Refresh() },
  })
  const Remove = useMutation({
    mutationFn: DeleteConnection,
    onSuccess: () => { SetNotice('Connection deleted with its group grants.'); SetDeleteTarget(null); SetSelectedID(null); Refresh() },
  })
  const Test = useMutation({
    mutationFn: TestConnection,
    onSuccess: (_, ID) => { SetTestedID(ID); SetNotice('Connection test passed.') },
  })

  function HandleCreate(Event: FormEvent<HTMLFormElement>) {
    Event.preventDefault()
    const Form = Event.currentTarget
    const Input = readForm(Form)
    SetNotice('')
    if (!Input || Input.password === '') {
      SetNotice('Fill every field with a valid port (1–65535).')
      return
    }
    Create.mutate(Input, { onSuccess: () => Form.reset() })
  }

  function HandleUpdate(Event: FormEvent<HTMLFormElement>) {
    Event.preventDefault()
    if (!Selected) return
    const Input = readForm(Event.currentTarget)
    SetNotice('')
    if (!Input || Input.password === '') {
      SetNotice('Fill every field with a valid port (1–65535).')
      return
    }
    Update.mutate({ ID: Selected.id, Input })
  }

  return (
    <>
      <PageHeader
        Title="Connections"
        Description="Target PostgreSQL databases the platform can open. Passwords are stored encrypted and never shown back."
        Actions={(
          <button type="button" className={ShowCreate ? 'btn btn-secondary' : 'btn'} onClick={() => SetShowCreate(Value => !Value)}>
            {ShowCreate ? 'Cancel' : <><Icon Name="plus" Size={14} />New connection</>}
          </button>
        )}
      />

      {ShowCreate && (
        <Card Title="Create connection" Description="Grant group access afterwards so members can browse it.">
          <form className="stack" onSubmit={HandleCreate}>
            <div className="form-grid">
              <div className="field">
                <label htmlFor="connection-name">Name</label>
                <input className="input" id="connection-name" name="name" required maxLength={64} autoComplete="off" />
              </div>
              <div className="field">
                <label htmlFor="connection-host">Host</label>
                <input className="input" id="connection-host" name="host" required maxLength={255} autoComplete="off" placeholder="127.0.0.1" />
              </div>
              <div className="field">
                <label htmlFor="connection-port">Port</label>
                <input className="input" id="connection-port" name="port" required inputMode="numeric" defaultValue="5432" autoComplete="off" />
              </div>
              <div className="field">
                <label htmlFor="connection-dbname">Database</label>
                <input className="input" id="connection-dbname" name="dbname" required maxLength={63} autoComplete="off" />
              </div>
              <div className="field">
                <label htmlFor="connection-username">Username</label>
                <input className="input" id="connection-username" name="username" required maxLength={63} autoComplete="off" />
              </div>
              <div className="field">
                <label htmlFor="connection-password">Password</label>
                <input className="input" id="connection-password" name="password" type="password" required autoComplete="new-password" />
                <span className="field-hint">Stored encrypted, never displayed again.</span>
              </div>
            </div>
            <RequestError error={Create.error} />
            <div className="form-actions">
              <button className="btn" type="submit" disabled={Create.isPending}>{Create.isPending ? 'Creating…' : 'Create connection'}</button>
            </div>
          </form>
        </Card>
      )}

      {Notice !== '' && <NoticeBanner Tone={Notice.startsWith('Fill') ? 'warning' : 'success'} Role="status">{Notice}</NoticeBanner>}
      <RequestError error={Update.error ?? Remove.error ?? Test.error} />

      <Card
        Title="All connections"
        Description="Test reachability before granting access."
        Actions={<Badge Tone="outline">{Connections.data?.length ?? 0} total</Badge>}
        Flush
      >
        {Connections.isPending && <div className="card-body"><Loading>Loading connections…</Loading></div>}
        {Connections.error !== null && <div className="card-body"><RequestError error={Connections.error} /></div>}
        {Connections.data?.length === 0 && <EmptyState IconName="plug" Title="No connections yet" />}
        {Connections.data && Connections.data.length > 0 && (
          <div className="table-scroll">
            <table className="table">
              <thead>
                <tr>
                  <th>Connection</th>
                  <th>Target</th>
                  <th className="cell-actions">Actions</th>
                </tr>
              </thead>
              <tbody>
                {Connections.data.map(Item => (
                  <tr key={Item.id} className={Item.id === SelectedID ? 'is-selected' : undefined}>
                    <td>
                      <div className="cell-stack">
                        <strong>{Item.name}</strong>
                        <span className="cell-sub">Updated {new Date(Item.updated_at).toLocaleString()}</span>
                      </div>
                    </td>
                    <td>
                      <div className="list-item-main">
                        <code className="inline-code">{Item.username}@{Item.host}:{Item.port}/{Item.dbname}</code>
                        {TestedID === Item.id && <Badge Tone="success" Dot>Reachable</Badge>}
                      </div>
                    </td>
                    <td className="cell-actions">
                      <div className="row-actions">
                        <button className="btn btn-ghost btn-sm" type="button" disabled={Test.isPending} onClick={() => { SetNotice(''); SetTestedID(null); Test.mutate(Item.id) }}>
                          {Test.isPending ? 'Testing…' : 'Test'}
                        </button>
                        <button className="btn btn-secondary btn-sm" type="button" onClick={() => SetSelectedID(Item.id === SelectedID ? null : Item.id)}>
                          {Item.id === SelectedID ? 'Close' : 'Manage'}
                        </button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Card>

      {Selected && (
        <Card
          Title={Selected.name}
          Description={<code className="inline-code">{Selected.username}@{Selected.host}:{Selected.port}/{Selected.dbname}</code>}
          Actions={(
            <>
              <button className="btn btn-ghost btn-sm" type="button" onClick={() => SetSelectedID(null)}>Close</button>
              <button className="btn btn-danger btn-sm" type="button" onClick={() => SetDeleteTarget(Selected)}>Delete connection</button>
            </>
          )}
        >
          <form className="stack" key={Selected.id} onSubmit={HandleUpdate}>
            <div className="form-grid">
              <div className="field">
                <label htmlFor={`name-${Selected.id}`}>Name</label>
                <input className="input" id={`name-${Selected.id}`} name="name" defaultValue={Selected.name} required maxLength={64} />
              </div>
              <div className="field">
                <label htmlFor={`host-${Selected.id}`}>Host</label>
                <input className="input" id={`host-${Selected.id}`} name="host" defaultValue={Selected.host} required maxLength={255} />
              </div>
              <div className="field">
                <label htmlFor={`port-${Selected.id}`}>Port</label>
                <input className="input" id={`port-${Selected.id}`} name="port" defaultValue={Selected.port} required inputMode="numeric" />
              </div>
              <div className="field">
                <label htmlFor={`dbname-${Selected.id}`}>Database</label>
                <input className="input" id={`dbname-${Selected.id}`} name="dbname" defaultValue={Selected.dbname} required maxLength={63} />
              </div>
              <div className="field">
                <label htmlFor={`username-${Selected.id}`}>Username</label>
                <input className="input" id={`username-${Selected.id}`} name="username" defaultValue={Selected.username} required maxLength={63} />
              </div>
              <div className="field">
                <label htmlFor={`password-${Selected.id}`}>Password</label>
                <input className="input" id={`password-${Selected.id}`} name="password" type="password" required autoComplete="new-password" />
                <span className="field-hint">Re-enter on every change.</span>
              </div>
            </div>
            <div className="form-actions">
              <button className="btn" type="submit" disabled={Update.isPending}>{Update.isPending ? 'Saving…' : 'Save changes'}</button>
            </div>
          </form>
        </Card>
      )}

      {DeleteTarget && (
        <ConfirmDialog
          Title="Delete connection"
          Description={`Delete ${DeleteTarget.name}? Its group grants and platform migration history will be removed. The target database is kept.`}
          ConfirmLabel="Delete connection"
          Pending={Remove.isPending}
          OnConfirm={() => Remove.mutate(DeleteTarget.id)}
          OnCancel={() => SetDeleteTarget(null)}
        />
      )}
    </>
  )
}
