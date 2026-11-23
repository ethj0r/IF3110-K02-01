import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import type { FormEvent } from 'react'
import { useState } from 'react'
import { AdminQueryKeys, CreateUser, DeleteUser, ListUsers, ResetUserPassword, SetUserActive } from '../../api/admin'
import type { AdminUser } from '../../api/admin'
import { Card, PageHeader } from '../../ui/Card'
import { ConfirmDialog } from '../../ui/ConfirmDialog'
import { Badge, EmptyState, Loading, Notice as NoticeBanner, RequestError, StatusBadge } from '../../ui/Feedback'
import { Icon } from '../../ui/Icon'

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
    <>
      <PageHeader
        Title="Users"
        Description="Create ordinary accounts, deactivate access, and reset credentials. Platform administrators cannot be created or removed here."
        Actions={(
          <button type="button" className={ShowCreate ? 'btn btn-secondary' : 'btn'} onClick={() => SetShowCreate(Value => !Value)}>
            {ShowCreate ? 'Cancel' : <><Icon Name="plus" Size={14} />New user</>}
          </button>
        )}
      />

      {ShowCreate && (
        <Card Title="Create user" Description="The account can sign in immediately and inherits access from its groups.">
          <form className="stack" onSubmit={HandleCreate}>
            <div className="form-grid">
              <div className="field">
                <label htmlFor="new-username">Username</label>
                <input className="input" id="new-username" name="username" required minLength={3} maxLength={64} autoComplete="off" />
                <span className="field-hint">3–64 letters, digits, dots, underscores, or hyphens.</span>
              </div>
              <div className="field">
                <label htmlFor="new-password">Password</label>
                <input className="input" id="new-password" name="password" type="password" required minLength={12} maxLength={1024} autoComplete="new-password" />
                <span className="field-hint">At least 12 characters.</span>
              </div>
            </div>
            <RequestError error={Create.error} />
            <div className="form-actions">
              <button className="btn" type="submit" disabled={Create.isPending}>{Create.isPending ? 'Creating…' : 'Create user'}</button>
            </div>
          </form>
        </Card>
      )}

      {ResetTarget && (
        <Card
          Title={`Reset password for ${ResetTarget.username}`}
          Description="This revokes every existing session for the account."
          Actions={<button className="btn btn-ghost btn-sm" type="button" onClick={() => SetResetTarget(null)}>Cancel</button>}
        >
          <form className="stack" key={ResetTarget.id} onSubmit={HandleReset}>
            <div className="form-grid">
              <div className="field">
                <label htmlFor="reset-password">New password</label>
                <input className="input" id="reset-password" name="password" type="password" required minLength={12} maxLength={1024} autoComplete="new-password" />
                <span className="field-hint">At least 12 characters.</span>
              </div>
            </div>
            <RequestError error={Reset.error} />
            <div className="form-actions">
              <button className="btn" type="submit" disabled={Reset.isPending}>{Reset.isPending ? 'Saving…' : 'Save password'}</button>
            </div>
          </form>
        </Card>
      )}

      {Notice !== '' && <NoticeBanner Tone="success" Role="status">{Notice}</NoticeBanner>}
      <RequestError error={Toggle.error ?? Remove.error} />

      <Card
        Title="All users"
        Description="Accounts that can sign in to the workspace."
        Actions={<Badge Tone="outline">{Users.data?.length ?? 0} total</Badge>}
        Flush
      >
        {Users.isPending && <div className="card-body"><Loading>Loading users…</Loading></div>}
        {Users.error !== null && <div className="card-body"><RequestError error={Users.error} /></div>}
        {Users.data?.length === 0 && <EmptyState IconName="users" Title="No users yet" />}
        {Users.data && Users.data.length > 0 && (
          <div className="table-scroll">
            <table className="table">
              <thead>
                <tr>
                  <th>User</th>
                  <th>Status</th>
                  <th>Groups</th>
                  <th className="cell-actions">Actions</th>
                </tr>
              </thead>
              <tbody>
                {Users.data.map(User => (
                  <tr key={User.id}>
                    <td>
                      <div className="list-item-main">
                        <span className="avatar" aria-hidden="true">{User.username.slice(0, 1)}</span>
                        <strong>{User.username}</strong>
                        {User.is_platform_admin && <Badge Tone="ink">Platform admin</Badge>}
                      </div>
                    </td>
                    <td><StatusBadge active={User.is_active} /></td>
                    <td>
                      {User.groups.length === 0 ? (
                        <span className="muted">No groups</span>
                      ) : (
                        <div className="badge-list">
                          {User.groups.map(Group => <Badge key={Group.id}>{Group.name}</Badge>)}
                        </div>
                      )}
                    </td>
                    <td className="cell-actions">
                      <div className="row-actions">
                        <button
                          className="btn btn-secondary btn-sm"
                          type="button"
                          disabled={Toggle.isPending || User.is_platform_admin}
                          title={User.is_platform_admin ? 'Platform administrators cannot be deactivated here' : undefined}
                          onClick={() => { SetNotice(''); Toggle.mutate({ ID: User.id, Active: !User.is_active }) }}
                        >
                          {User.is_active ? 'Deactivate' : 'Activate'}
                        </button>
                        <button className="btn btn-ghost btn-sm" type="button" onClick={() => { SetNotice(''); SetResetTarget(User) }}>
                          Reset password
                        </button>
                        <button
                          className="btn btn-danger btn-sm"
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
              </tbody>
            </table>
          </div>
        )}
      </Card>

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
    </>
  )
}
