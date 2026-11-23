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
  SetGroupLeader,
  UpdateGroup,
} from '../../api/admin'
import type { AdminGroup } from '../../api/admin'
import { Card, PageHeader } from '../../ui/Card'
import { ConfirmDialog } from '../../ui/ConfirmDialog'
import { Badge, EmptyState, Loading, Notice as NoticeBanner, RequestError } from '../../ui/Feedback'
import { Icon } from '../../ui/Icon'

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
    <>
      <PageHeader
        Title="Groups"
        Actions={(
          <button type="button" className={ShowCreate ? 'btn btn-secondary' : 'btn'} onClick={() => SetShowCreate(Value => !Value)}>
            {ShowCreate ? 'Cancel' : <><Icon Name="plus" Size={14} />New group</>}
          </button>
        )}
      />

      {ShowCreate && (
        <Card Title="Create group">
          <form className="stack" onSubmit={HandleCreate}>
            <div className="form-grid">
              <div className="field">
                <label htmlFor="group-name">Name</label>
                <input className="input" id="group-name" name="name" required maxLength={64} autoComplete="off" />
              </div>
              <div className="field">
                <label htmlFor="group-description">Description</label>
                <input className="input" id="group-description" name="description" maxLength={500} autoComplete="off" />
              </div>
            </div>
            <RequestError error={Create.error} />
            <div className="form-actions">
              <button className="btn" type="submit" disabled={Create.isPending}>{Create.isPending ? 'Creating…' : 'Create group'}</button>
            </div>
          </form>
        </Card>
      )}

      {Notice !== '' && <NoticeBanner Tone="success" Role="status">{Notice}</NoticeBanner>}
      <RequestError error={Update.error ?? Remove.error} />

      <Card Flush>
        {Groups.isPending && <div className="card-body"><Loading>Loading groups…</Loading></div>}
        {Groups.error !== null && <div className="card-body"><RequestError error={Groups.error} /></div>}
        {Groups.data?.length === 0 && <EmptyState IconName="group" Title="No groups yet" />}
        {Groups.data && Groups.data.length > 0 && (
          <div className="table-scroll">
            <table className="table">
              <thead>
                <tr>
                  <th>Group</th>
                  <th>Members</th>
                  <th>Grants</th>
                  <th>Leader</th>
                  <th className="cell-actions">Actions</th>
                </tr>
              </thead>
              <tbody>
                {Groups.data.map(Group => (
                  <tr key={Group.id} className={Group.id === SelectedID ? 'is-selected' : undefined}>
                    <td>
                      <div className="cell-stack">
                        <strong>{Group.name}</strong>
                        {Group.description && <span className="cell-sub">{Group.description}</span>}
                      </div>
                    </td>
                    <td className="mono">{Group.member_count}</td>
                    <td className="mono">{Group.access_count}</td>
                    <td>{Group.leader_user_id ? <Badge Tone="success" Dot>Assigned</Badge> : <span className="muted">—</span>}</td>
                    <td className="cell-actions">
                      <button className="btn btn-secondary btn-sm" type="button" onClick={() => SetSelectedID(Group.id === SelectedID ? null : Group.id)}>
                        {Group.id === SelectedID ? 'Close' : 'Manage'}
                      </button>
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
          Actions={(
            <>
              <button className="btn btn-ghost btn-sm" type="button" onClick={() => SetSelectedID(null)}>Close</button>
              <button className="btn btn-danger btn-sm" type="button" onClick={() => SetDeleteTarget(Selected)}>Delete group</button>
            </>
          )}
        >
          <div className="split">
            <div className="stack">
              <form className="stack" key={`${Selected.id}-${Selected.updated_at}`} onSubmit={HandleUpdate}>
                <div className="field">
                  <label htmlFor={`name-${Selected.id}`}>Name</label>
                  <input className="input" id={`name-${Selected.id}`} name="name" defaultValue={Selected.name} required maxLength={64} />
                </div>
                <div className="field">
                  <label htmlFor={`description-${Selected.id}`}>Description</label>
                  <input className="input" id={`description-${Selected.id}`} name="description" defaultValue={Selected.description} maxLength={500} />
                </div>
                <div className="form-actions">
                  <button className="btn" type="submit" disabled={Update.isPending}>{Update.isPending ? 'Saving…' : 'Save changes'}</button>
                </div>
              </form>
            </div>
            <div className="stack">
              <h3>Members</h3>
              <GroupMembers Group={Selected} key={Selected.id} />
            </div>
          </div>
        </Card>
      )}

      {DeleteTarget && (
        <ConfirmDialog
          Title="Delete group"
          Description={`Delete ${DeleteTarget.name}?`}
          ConfirmLabel="Delete group"
          Pending={Remove.isPending}
          OnConfirm={() => Remove.mutate(DeleteTarget.id)}
          OnCancel={() => SetDeleteTarget(null)}
        />
      )}
    </>
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
  const SetLeader = useMutation({
    mutationFn: (UserID: string) => SetGroupLeader(Group.id, UserID),
    onSuccess: () => Cache.invalidateQueries({ queryKey: AdminQueryKeys.groups }),
  })
  const MembershipBusy = Add.isPending || Remove.isPending || SetLeader.isPending

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
      <RequestError error={Members.error ?? Users.error ?? Add.error ?? Remove.error ?? SetLeader.error} />
      {Members.isPending && <Loading>Loading members…</Loading>}

      {Members.data && Members.data.length > 0 && (
        <ul className="list">
          {Members.data.map(Member => (
            <li key={Member.id} className="list-item">
              <div className="list-item-main">
                <span className="avatar" aria-hidden="true">{Member.username.slice(0, 1)}</span>
                <strong>{Member.username}</strong>
                {Member.is_platform_admin && <Badge Tone="ink">Platform admin</Badge>}
                {Member.id === Group.leader_user_id && <Badge Tone="success" Dot>{Member.is_active ? 'Leader' : 'Inactive leader'}</Badge>}
              </div>
              <div className="row-actions">
                {Member.is_active && Member.id !== Group.leader_user_id && (
                  <button className="btn btn-secondary btn-sm" type="button" disabled={MembershipBusy} onClick={() => SetLeader.mutate(Member.id)}>Set as leader</button>
                )}
                <button className="btn btn-ghost btn-sm" type="button" disabled={MembershipBusy} onClick={() => Remove.mutate(Member.id)}>Remove</button>
              </div>
            </li>
          ))}
        </ul>
      )}
      {Members.data?.length === 0 && <p className="muted">No members yet.</p>}

      {Available.length > 0 ? (
        <form className="toolbar" onSubmit={HandleAdd}>
          <div className="field">
            <label htmlFor={`member-${Group.id}`}>Add user</label>
            <select className="select" id={`member-${Group.id}`} value={Selected} onChange={Event => SetSelected(Event.target.value)}>
              <option value="">Select a user</option>
              {Available.map(User => <option key={User.id} value={User.id}>{User.username}</option>)}
            </select>
          </div>
          <button className="btn btn-secondary" type="submit" disabled={!Selected || MembershipBusy}>{Add.isPending ? 'Adding…' : 'Add member'}</button>
        </form>
      ) : null}
    </div>
  )
}
