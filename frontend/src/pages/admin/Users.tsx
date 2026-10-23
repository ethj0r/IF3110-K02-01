import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import type { FormEvent } from 'react'
import { useState } from 'react'
import { AdminQueryKeys, CreateUser, DeleteUser, ListUsers, ResetUserPassword, SetUserActive } from '../../api/admin'
import type { AdminUser } from '../../api/admin'
import { ConfirmDialog } from '../../components/ConfirmDialog'
import { RequestError, StatusBadge } from '../../components/Feedback'
import { PageHeading } from '../../components/PageHeading'

export function UsersPage() {
  const Cache = useQueryClient()
  const Users = useQuery({ queryKey: AdminQueryKeys.users, queryFn: ListUsers })
  const [Notice, SetNotice] = useState('')
  const [ShowCreate, SetShowCreate] = useState(false)
  const [ResetTarget, SetResetTarget] = useState<AdminUser | null>(null)
  const [DeleteTarget, SetDeleteTarget] = useState<AdminUser | null>(null)

  function Refresh() {
    void Cache.invalidateQueries({ queryKey: AdminQueryKeys.users })
    void Cache.invalidateQueries({ queryKey: AdminQueryKeys.groups })
    void Cache.invalidateQueries({ queryKey: AdminQueryKeys.matrix })
  }

  const Create = useMutation({
    mutationFn: CreateUser,
    onSuccess: () => {
      SetNotice('User created.')
      SetShowCreate(false)
      Refresh()
    },
  })
  const Toggle = useMutation({
    mutationFn: ({ ID, Active }: { ID: string; Active: boolean }) => SetUserActive(ID, Active),
    onSuccess: () => {
      SetNotice('User status updated.')
      Refresh()
    },
  })
  const Reset = useMutation({
    mutationFn: ({ ID, Password }: { ID: string; Password: string }) => ResetUserPassword(ID, Password),
    onSuccess: () => {
      SetNotice('Password reset. Existing sessions were revoked.')
      SetResetTarget(null)
      Refresh()
    },
  })
  const Remove = useMutation({
    mutationFn: DeleteUser,
    onSuccess: () => {
      SetNotice('User deleted.')
      SetDeleteTarget(null)
      Refresh()
    },
  })

  function HandleCreate(Event: FormEvent<HTMLFormElement>) {
    Event.preventDefault()
    const Form = Event.currentTarget
    const Values = new FormData(Form)
    SetNotice('')
    Create.mutate(
      { username: String(Values.get('username') ?? ''), password: String(Values.get('password') ?? '') },
      { onSuccess: () => Form.reset() },
    )
  }

  function HandleReset(Event: FormEvent<HTMLFormElement>) {
    Event.preventDefault()
    if (!ResetTarget) return
    const Form = Event.currentTarget
    const Values = new FormData(Form)
    SetNotice('')
    Reset.mutate(
      { ID: ResetTarget.id, Password: String(Values.get('password') ?? '') },
      { onSuccess: () => Form.reset() },
    )
  }

  return (
    <section>
      <PageHeading
        Title="Users"
        Description="Create ordinary accounts, deactivate access, and reset credentials. Platform administrators cannot be created or removed here."
        Actions={(
          <button type="button" className={ShowCreate ? 'secondary' : ''} onClick={() => SetShowCreate(Value => !Value)}>
            {ShowCreate ? 'Cancel' : 'New user'}
          </button>
        )}
      />

      {ShowCreate && (
        <form className="panel form-panel" onSubmit={HandleCreate}>
          <header className="panel-header">
            <div>
              <h3>Create user</h3>
              <p>The account can sign in immediately and inherits access from its groups.</p>
            </div>
          </header>
          <div className="form-grid">
            <div className="field">
              <label htmlFor="new-username">Username</label>
              <input id="new-username" name="username" required minLength={3} maxLength={64} autoComplete="off" />
              <span className="hint">3–64 letters, digits, dots, underscores, or hyphens.</span>
            </div>
            <div className="field">
              <label htmlFor="new-password">Password</label>
              <input id="new-password" name="password" type="password" required minLength={12} maxLength={1024} autoComplete="new-password" />
              <span className="hint">At least 12 characters.</span>
            </div>
            <button type="submit" disabled={Create.isPending}>{Create.isPending ? 'Creating…' : 'Create user'}</button>
          </div>
          <RequestError error={Create.error} />
        </form>
      )}

      {ResetTarget && (
        <form className="panel form-panel" key={ResetTarget.id} onSubmit={HandleReset}>
          <header className="panel-header">
            <div>
              <h3>Reset password for {ResetTarget.username}</h3>
              <p>This revokes every existing session for the account.</p>
            </div>
            <button className="ghost small" type="button" onClick={() => SetResetTarget(null)}>Cancel</button>
          </header>
          <div className="form-grid">
            <div className="field">
              <label htmlFor="reset-password">New password</label>
              <input id="reset-password" name="password" type="password" required minLength={12} maxLength={1024} autoComplete="new-password" />
              <span className="hint">At least 12 characters.</span>
            </div>
            <button type="submit" disabled={Reset.isPending}>{Reset.isPending ? 'Saving…' : 'Save password'}</button>
          </div>
          <RequestError error={Reset.error} />
        </form>
      )}

      {Notice && <p className="notice-banner" role="status">{Notice}</p>}
      <RequestError error={Toggle.error ?? Remove.error} />

      <div className="panel">
        <header className="panel-header">
          <div>
            <h3>All users</h3>
            <p>Accounts that can sign in to the workspace.</p>
          </div>
          <span className="count-badge">{Users.data?.length ?? 0}</span>
        </header>

        {Users.isPending && <p className="state" role="status">Loading users…</p>}
        <RequestError error={Users.error} />

        {Users.data && (
          <div className="table-wrap">
            <table className="data-table">
              <thead>
                <tr>
                  <th>User</th>
                  <th>Status</th>
                  <th>Groups</th>
                  <th className="actions-column">Actions</th>
                </tr>
              </thead>
              <tbody>
                {Users.data.map(User => (
                  <tr key={User.id}>
                    <td>
                      <strong>{User.username}</strong>
                      {User.is_platform_admin && <span className="badge admin">Platform admin</span>}
                    </td>
                    <td><StatusBadge active={User.is_active} /></td>
                    <td>
                      {User.groups.length === 0 ? (
                        <span className="muted">No groups</span>
                      ) : (
                        <div className="chip-list">
                          {User.groups.map(Group => <span key={Group.id} className="chip">{Group.name}</span>)}
                        </div>
                      )}
                    </td>
                    <td className="actions-column">
                      <div className="row-actions">
                        <button
                          className="secondary small"
                          type="button"
                          disabled={Toggle.isPending || User.is_platform_admin}
                          title={User.is_platform_admin ? 'Platform administrators cannot be deactivated here' : undefined}
                          onClick={() => { SetNotice(''); Toggle.mutate({ ID: User.id, Active: !User.is_active }) }}
                        >
                          {User.is_active ? 'Deactivate' : 'Activate'}
                        </button>
                        <button
                          className="ghost small"
                          type="button"
                          onClick={() => { SetNotice(''); SetResetTarget(User) }}
                        >
                          Reset password
                        </button>
                        <button
                          className="ghost danger small"
                          type="button"
                          disabled={User.is_platform_admin}
                          title={User.is_platform_admin ? 'Platform administrators cannot be deleted here' : undefined}
                          onClick={() => { SetNotice(''); SetDeleteTarget(User) }}
                        >
                          Delete
                        </button>
                      </div>
                    </td>
                  </tr>
                ))}
                {Users.data.length === 0 && (
                  <tr>
                    <td colSpan={4} className="empty-cell">No users yet.</td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        )}
      </div>

      {DeleteTarget && (
        <ConfirmDialog
          Title="Delete user"
          Description={`Delete ${DeleteTarget.username}? Their group memberships will be removed and existing sessions revoked.`}
          ConfirmLabel="Delete user"
          Pending={Remove.isPending}
          OnConfirm={() => Remove.mutate(DeleteTarget.id)}
          OnCancel={() => SetDeleteTarget(null)}
        />
      )}
    </section>
  )
}
