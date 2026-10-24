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
import { ConfirmDialog } from '../../components/ConfirmDialog'
import { RequestError } from '../../components/Feedback'
import { PageHeading } from '../../components/PageHeading'

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
    const Input = readForm(Event.currentTarget)
    SetNotice('')
    if (!Input || Input.password === '') {
      SetNotice('Fill every field with a valid port (1–65535).')
      return
    }
    Create.mutate(Input, { onSuccess: () => Event.currentTarget.reset() })
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
    <section>
      <PageHeading
        Title="Connections"
        Description="Target PostgreSQL databases the platform can open. Passwords are stored encrypted and never shown back."
        Actions={(
          <button type="button" className={ShowCreate ? 'secondary' : ''} onClick={() => SetShowCreate(Value => !Value)}>
            {ShowCreate ? 'Cancel' : 'New connection'}
          </button>
        )}
      />

      {ShowCreate && (
        <form className="panel form-panel" onSubmit={HandleCreate}>
          <header className="panel-header">
            <div>
              <h3>Create connection</h3>
              <p>Grant group access afterwards so members can browse it.</p>
            </div>
          </header>
          <div className="form-grid">
            <div className="field">
              <label htmlFor="connection-name">Name</label>
              <input id="connection-name" name="name" required maxLength={64} autoComplete="off" />
            </div>
            <div className="field">
              <label htmlFor="connection-host">Host</label>
              <input id="connection-host" name="host" required maxLength={255} autoComplete="off" placeholder="127.0.0.1" />
            </div>
            <div className="field">
              <label htmlFor="connection-port">Port</label>
              <input id="connection-port" name="port" required inputMode="numeric" defaultValue="5432" autoComplete="off" />
            </div>
            <div className="field">
              <label htmlFor="connection-dbname">Database</label>
              <input id="connection-dbname" name="dbname" required maxLength={63} autoComplete="off" />
            </div>
            <div className="field">
              <label htmlFor="connection-username">Username</label>
              <input id="connection-username" name="username" required maxLength={63} autoComplete="off" />
            </div>
            <div className="field">
              <label htmlFor="connection-password">Password</label>
              <input id="connection-password" name="password" type="password" required autoComplete="new-password" />
              <span className="hint">Stored encrypted, never displayed again.</span>
            </div>
            <button type="submit" disabled={Create.isPending}>{Create.isPending ? 'Creating…' : 'Create connection'}</button>
          </div>
          <RequestError error={Create.error} />
        </form>
      )}

      {Notice && <p className="notice-banner" role="status">{Notice}</p>}
      <RequestError error={Update.error ?? Remove.error ?? Test.error} />

      <div className="panel">
        <header className="panel-header">
          <div>
            <h3>All connections</h3>
            <p>Test reachability before granting access.</p>
          </div>
          <span className="count-badge">{Connections.data?.length ?? 0}</span>
        </header>

        {Connections.isPending && <p className="state" role="status">Loading connections…</p>}
        <RequestError error={Connections.error} />

        {Connections.data && (
          <div className="table-wrap">
            <table className="data-table">
              <thead>
                <tr>
                  <th>Connection</th>
                  <th>Target</th>
                  <th className="actions-column">Actions</th>
                </tr>
              </thead>
              <tbody>
                {Connections.data.map(Item => (
                  <tr key={Item.id} className={Item.id === SelectedID ? 'selected-row' : undefined}>
                    <td>
                      <div>
                        <strong>{Item.name}</strong>
                        <span className="table-subtext">Updated {new Date(Item.updated_at).toLocaleString()}</span>
                      </div>
                    </td>
                    <td>
                      <code>{Item.username}@{Item.host}:{Item.port}/{Item.dbname}</code>
                      {TestedID === Item.id && <span className="badge active">Reachable</span>}
                    </td>
                    <td className="actions-column">
                      <div className="row-actions">
                        <button className="ghost small" type="button" disabled={Test.isPending} onClick={() => { SetNotice(''); SetTestedID(null); Test.mutate(Item.id) }}>
                          {Test.isPending ? 'Testing…' : 'Test'}
                        </button>
                        <button
                          className="secondary small"
                          type="button"
                          onClick={() => SetSelectedID(Item.id === SelectedID ? null : Item.id)}
                        >
                          {Item.id === SelectedID ? 'Close' : 'Manage'}
                        </button>
                      </div>
                    </td>
                  </tr>
                ))}
                {Connections.data.length === 0 && (
                  <tr>
                    <td colSpan={3} className="empty-cell">No connections yet.</td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        )}
      </div>

      {Selected && (
        <section className="panel detail-panel">
          <header className="panel-header">
            <div>
              <h3>{Selected.name}</h3>
              <p><code>{Selected.username}@{Selected.host}:{Selected.port}/{Selected.dbname}</code></p>
            </div>
            <div className="row-actions">
              <button className="ghost small" type="button" onClick={() => SetSelectedID(null)}>Close</button>
              <button className="danger small" type="button" onClick={() => SetDeleteTarget(Selected)}>Delete connection</button>
            </div>
          </header>

          <form className="stack" key={Selected.id} onSubmit={HandleUpdate}>
            <div className="form-grid">
              <div className="field">
                <label htmlFor={`name-${Selected.id}`}>Name</label>
                <input id={`name-${Selected.id}`} name="name" defaultValue={Selected.name} required maxLength={64} />
              </div>
              <div className="field">
                <label htmlFor={`host-${Selected.id}`}>Host</label>
                <input id={`host-${Selected.id}`} name="host" defaultValue={Selected.host} required maxLength={255} />
              </div>
              <div className="field">
                <label htmlFor={`port-${Selected.id}`}>Port</label>
                <input id={`port-${Selected.id}`} name="port" defaultValue={Selected.port} required inputMode="numeric" />
              </div>
              <div className="field">
                <label htmlFor={`dbname-${Selected.id}`}>Database</label>
                <input id={`dbname-${Selected.id}`} name="dbname" defaultValue={Selected.dbname} required maxLength={63} />
              </div>
              <div className="field">
                <label htmlFor={`username-${Selected.id}`}>Username</label>
                <input id={`username-${Selected.id}`} name="username" defaultValue={Selected.username} required maxLength={63} />
              </div>
              <div className="field">
                <label htmlFor={`password-${Selected.id}`}>Password</label>
                <input id={`password-${Selected.id}`} name="password" type="password" required autoComplete="new-password" />
                <span className="hint">Re-enter on every change.</span>
              </div>
              <button type="submit" disabled={Update.isPending}>{Update.isPending ? 'Saving…' : 'Save changes'}</button>
            </div>
          </form>
        </section>
      )}

      {DeleteTarget && (
        <ConfirmDialog
          Title="Delete connection"
          Description={`Delete ${DeleteTarget.name}? Its group grants will be removed and members lose access.`}
          ConfirmLabel="Delete connection"
          Pending={Remove.isPending}
          OnConfirm={() => Remove.mutate(DeleteTarget.id)}
          OnCancel={() => SetDeleteTarget(null)}
        />
      )}
    </section>
  )
}
