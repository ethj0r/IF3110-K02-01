import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import type { FormEvent } from 'react'
import { useState } from 'react'
import {
  AddMember,
  AdminQueryKeys,
  CreateGroup,
  DeleteGroup,
  ListGroups,
  ListMembers,
  ListUsers,
  RemoveMember,
  UpdateGroup,
} from '../../api/admin'
import type { AdminGroup } from '../../api/admin'
import { ConfirmDialog } from '../../components/ConfirmDialog'
import { RequestError } from '../../components/Feedback'
import { PageHeading } from '../../components/PageHeading'

export function GroupsPage() {
  const Cache = useQueryClient()
  const Groups = useQuery({ queryKey: AdminQueryKeys.groups, queryFn: ListGroups })
  const [Notice, SetNotice] = useState('')
  const [ShowCreate, SetShowCreate] = useState(false)
  const [SelectedID, SetSelectedID] = useState<string | null>(null)
  const [DeleteTarget, SetDeleteTarget] = useState<AdminGroup | null>(null)
  const Selected = Groups.data?.find(Group => Group.id === SelectedID) ?? null

  function RefreshGroups() {
    void Cache.invalidateQueries({ queryKey: AdminQueryKeys.groups })
    void Cache.invalidateQueries({ queryKey: AdminQueryKeys.matrix })
    void Cache.invalidateQueries({ queryKey: AdminQueryKeys.users })
  }

  const Create = useMutation({
    mutationFn: CreateGroup,
    onSuccess: () => {
      SetNotice('Group created.')
      SetShowCreate(false)
      RefreshGroups()
    },
  })
  const Update = useMutation({
    mutationFn: ({ ID, Name, Description }: { ID: string; Name: string; Description: string }) => UpdateGroup(ID, { name: Name, description: Description }),
    onSuccess: () => { SetNotice('Group updated.'); RefreshGroups() },
  })
  const Remove = useMutation({
    mutationFn: DeleteGroup,
    onSuccess: () => {
      SetNotice('Group deleted. Member sessions were revoked.')
      SetDeleteTarget(null)
      SetSelectedID(null)
      RefreshGroups()
    },
  })

  function HandleCreate(Event: FormEvent<HTMLFormElement>) {
    Event.preventDefault()
    const Form = Event.currentTarget
    const Values = new FormData(Form)
    SetNotice('')
    Create.mutate(
      { name: String(Values.get('name') ?? ''), description: String(Values.get('description') ?? '') },
      { onSuccess: () => Form.reset() },
    )
  }

  function HandleUpdate(Event: FormEvent<HTMLFormElement>) {
    Event.preventDefault()
    if (!Selected) return
    const Values = new FormData(Event.currentTarget)
    SetNotice('')
    Update.mutate({ ID: Selected.id, Name: String(Values.get('name') ?? ''), Description: String(Values.get('description') ?? '') })
  }

  return (
    <section>
      <PageHeading
        Title="Groups"
        Description="Groups collect users and carry database grants. A user receives the highest level granted by any group they belong to."
        Actions={(
          <button type="button" className={ShowCreate ? 'secondary' : ''} onClick={() => SetShowCreate(Value => !Value)}>
            {ShowCreate ? 'Cancel' : 'New group'}
          </button>
        )}
      />

      {ShowCreate && (
        <form className="panel form-panel" onSubmit={HandleCreate}>
          <header className="panel-header">
            <div>
              <h3>Create group</h3>
              <p>Add members and database grants after creating the group.</p>
            </div>
          </header>
          <div className="form-grid">
            <div className="field">
              <label htmlFor="group-name">Name</label>
              <input id="group-name" name="name" required maxLength={64} autoComplete="off" />
              <span className="hint">1–64 letters, digits, spaces, dots, underscores, or hyphens.</span>
            </div>
            <div className="field grow">
              <label htmlFor="group-description">Description</label>
              <input id="group-description" name="description" maxLength={500} autoComplete="off" />
              <span className="hint">Optional, up to 500 characters.</span>
            </div>
            <button type="submit" disabled={Create.isPending}>{Create.isPending ? 'Creating…' : 'Create group'}</button>
          </div>
          <RequestError error={Create.error} />
        </form>
      )}

      {Notice && <p className="notice-banner" role="status">{Notice}</p>}
      <RequestError error={Update.error ?? Remove.error} />

      <div className="panel">
        <header className="panel-header">
          <div>
            <h3>All groups</h3>
            <p>Select a group to edit its details or manage members.</p>
          </div>
          <span className="count-badge">{Groups.data?.length ?? 0}</span>
        </header>

        {Groups.isPending && <p className="state" role="status">Loading groups…</p>}
        <RequestError error={Groups.error} />

        {Groups.data && (
          <div className="table-wrap">
            <table className="data-table">
              <thead>
                <tr>
                  <th>Group</th>
                  <th>Members</th>
                  <th>Grants</th>
                  <th className="actions-column">Actions</th>
                </tr>
              </thead>
              <tbody>
                {Groups.data.map(Group => (
                  <tr key={Group.id} className={Group.id === SelectedID ? 'selected-row' : undefined}>
                    <td>
                      <div>
                        <strong>{Group.name}</strong>
                        <span className="table-subtext">{Group.description || 'No description'}</span>
                      </div>
                    </td>
                    <td>{Group.member_count}</td>
                    <td>{Group.access_count}</td>
                    <td className="actions-column">
                      <button
                        className="secondary small"
                        type="button"
                        onClick={() => SetSelectedID(Group.id === SelectedID ? null : Group.id)}
                      >
                        {Group.id === SelectedID ? 'Close' : 'Manage'}
                      </button>
                    </td>
                  </tr>
                ))}
                {Groups.data.length === 0 && (
                  <tr>
                    <td colSpan={4} className="empty-cell">No groups yet.</td>
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
              <p>{Selected.description || 'No description'}</p>
            </div>
            <div className="row-actions">
              <button className="ghost small" type="button" onClick={() => SetSelectedID(null)}>Close</button>
              <button className="danger small" type="button" onClick={() => SetDeleteTarget(Selected)}>Delete group</button>
            </div>
          </header>

          <div className="detail-grid">
            <div>
              <h4>Group settings</h4>
              <form className="stack" key={`${Selected.id}-${Selected.updated_at}`} onSubmit={HandleUpdate}>
                <div className="field">
                  <label htmlFor={`name-${Selected.id}`}>Name</label>
                  <input id={`name-${Selected.id}`} name="name" defaultValue={Selected.name} required maxLength={64} />
                </div>
                <div className="field">
                  <label htmlFor={`description-${Selected.id}`}>Description</label>
                  <input id={`description-${Selected.id}`} name="description" defaultValue={Selected.description} maxLength={500} />
                </div>
                <button type="submit" disabled={Update.isPending}>{Update.isPending ? 'Saving…' : 'Save changes'}</button>
              </form>
            </div>
            <div>
              <h4>Members</h4>
              <GroupMembers Group={Selected} key={Selected.id} />
            </div>
          </div>
        </section>
      )}

      {DeleteTarget && (
        <ConfirmDialog
          Title="Delete group"
          Description={`Delete ${DeleteTarget.name}? Memberships and database grants will be removed, and member sessions revoked.`}
          ConfirmLabel="Delete group"
          Pending={Remove.isPending}
          OnConfirm={() => Remove.mutate(DeleteTarget.id)}
          OnCancel={() => SetDeleteTarget(null)}
        />
      )}
    </section>
  )
}

function GroupMembers({ Group }: { Group: AdminGroup }) {
  const Cache = useQueryClient()
  const [Selected, SetSelected] = useState('')
  const Members = useQuery({ queryKey: AdminQueryKeys.members(Group.id), queryFn: () => ListMembers(Group.id) })
  const Users = useQuery({ queryKey: AdminQueryKeys.users, queryFn: ListUsers })
  const Add = useMutation({
    mutationFn: (UserID: string) => AddMember(Group.id, UserID),
    onSuccess: () => { SetSelected(''); Refresh() },
  })
  const Remove = useMutation({
    mutationFn: (UserID: string) => RemoveMember(Group.id, UserID),
    onSuccess: Refresh,
  })

  function Refresh() {
    void Cache.invalidateQueries({ queryKey: AdminQueryKeys.members(Group.id) })
    void Cache.invalidateQueries({ queryKey: AdminQueryKeys.groups })
    void Cache.invalidateQueries({ queryKey: AdminQueryKeys.users })
  }

  function HandleAdd(Event: FormEvent<HTMLFormElement>) {
    Event.preventDefault()
    if (Selected) Add.mutate(Selected)
  }

  const MemberIDs = new Set(Members.data?.map(Member => Member.id) ?? [])
  const Available = Users.data?.filter(User => !MemberIDs.has(User.id)) ?? []

  return (
    <div className="stack">
      <RequestError error={Members.error ?? Users.error ?? Add.error ?? Remove.error} />
      {Members.isPending && <p className="state" role="status">Loading members…</p>}

      {Members.data && Members.data.length > 0 && (
        <ul className="member-list">
          {Members.data.map(Member => (
            <li key={Member.id}>
              <div>
                <strong>{Member.username}</strong>
                {Member.is_platform_admin && <span className="badge admin">Platform admin</span>}
              </div>
              <button className="ghost small" type="button" disabled={Remove.isPending} onClick={() => Remove.mutate(Member.id)}>Remove</button>
            </li>
          ))}
        </ul>
      )}
      {Members.data?.length === 0 && <p className="muted">No members yet.</p>}

      {Available.length > 0 ? (
        <form className="member-add" onSubmit={HandleAdd}>
          <div className="field grow">
            <label htmlFor={`member-${Group.id}`}>Add user</label>
            <select id={`member-${Group.id}`} value={Selected} onChange={Event => SetSelected(Event.target.value)}>
              <option value="">Select a user</option>
              {Available.map(User => <option key={User.id} value={User.id}>{User.username}</option>)}
            </select>
          </div>
          <button className="secondary" type="submit" disabled={!Selected || Add.isPending}>{Add.isPending ? 'Adding…' : 'Add member'}</button>
        </form>
      ) : <p className="muted">Every user is already a member.</p>}
    </div>
  )
}
