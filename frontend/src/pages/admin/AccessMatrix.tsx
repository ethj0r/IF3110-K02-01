import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { AdminQueryKeys, GetMatrix, RemoveGroupAccess, SetGroupAccess } from '../../api/admin'
import type { AccessLevel, AdminMatrix } from '../../api/admin'
import { Card, PageHeader } from '../../ui/Card'
import { Badge, EmptyState, Loading, RequestError } from '../../ui/Feedback'
import { Icon } from '../../ui/Icon'

export function AccessMatrixPage() {
  const Cache = useQueryClient()
  const Matrix = useQuery({ queryKey: AdminQueryKeys.matrix, queryFn: GetMatrix })
  const Change = useMutation({
    mutationFn: ({ GroupID, DatabaseID, Level }: { GroupID: string; DatabaseID: string; Level: AccessLevel | '' }) => {
      if (!Level) return RemoveGroupAccess(GroupID, DatabaseID)
      return SetGroupAccess(GroupID, DatabaseID, Level)
    },
    onSuccess: () => {
      void Cache.invalidateQueries({ queryKey: AdminQueryKeys.matrix })
      void Cache.invalidateQueries({ queryKey: AdminQueryKeys.groups })
    },
  })

  return (
    <>
      <PageHeader
        Title="Database access"
        Description="Choose the highest permission each group receives. View and edit wins over view only. Platform administrators receive no access unless a group grants it."
      />

      <div className="legend">
        <span className="legend-item"><Badge>View only</Badge><span className="muted">can read schema, data, and history.</span></span>
        <span className="legend-item"><Badge Tone="ink">View and edit</Badge><span className="muted">can also create and run migrations.</span></span>
      </div>

      <RequestError error={Change.error} />

      <Card
        Title="Group grants"
        Description="Changes take effect immediately and may rotate or revoke member sessions."
        Actions={<Badge Tone="outline">{Matrix.data?.grants.length ?? 0} grants</Badge>}
        Flush
      >
        {Matrix.isPending && <div className="card-body"><Loading>Loading access matrix…</Loading></div>}
        {Matrix.error !== null && <div className="card-body"><RequestError error={Matrix.error} /></div>}

        {Matrix.data && (Matrix.data.groups.length === 0 || Matrix.data.databases.length === 0) && (
          <EmptyState IconName="shield" Title="Nothing to assign yet">
            <p>
              {Matrix.data.groups.length === 0
                ? 'Create a group before assigning database access.'
                : 'No database connections are configured. Add a connection before assigning access.'}
            </p>
            {Matrix.data.groups.length === 0 && <Link to="/admin/groups" className="btn btn-secondary btn-sm">Go to groups<Icon Name="arrow-right" Size={14} /></Link>}
          </EmptyState>
        )}

        {Matrix.data && Matrix.data.groups.length > 0 && Matrix.data.databases.length > 0 && (
          <MatrixTable Matrix={Matrix.data} Disabled={Change.isPending} OnChange={Input => Change.mutate(Input)} />
        )}
      </Card>
    </>
  )
}

function MatrixTable({ Matrix, Disabled, OnChange }: {
  Matrix: AdminMatrix
  Disabled: boolean
  OnChange: (Input: { GroupID: string; DatabaseID: string; Level: AccessLevel | '' }) => void
}) {
  return (
    <div className="table-scroll">
      <table className="table matrix">
        <thead>
          <tr>
            <th scope="col" className="sticky-col">Group</th>
            {Matrix.databases.map(Database => (
              <th key={Database.id} scope="col">
                <span className="cell-stack">
                  <span>{Database.name}</span>
                  <span className="cell-sub mono">{Database.dbname}</span>
                </span>
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {Matrix.groups.map(Group => (
            <tr key={Group.id}>
              <th scope="row" className="sticky-col">
                <span className="cell-stack">
                  <strong>{Group.name}</strong>
                  <span className="cell-sub">{Group.member_count} member{Group.member_count === 1 ? '' : 's'}</span>
                </span>
              </th>
              {Matrix.databases.map(Database => {
                const Current = CurrentLevel(Matrix, Group.id, Database.id)
                return (
                  <td key={Database.id}>
                    <select
                      className={`select level-select ${Current === 'view_edit' ? 'is-edit' : Current === 'view_only' ? 'is-view' : ''}`}
                      aria-label={`${Group.name} access to ${Database.name}`}
                      value={Current}
                      disabled={Disabled}
                      onChange={Event => OnChange({ GroupID: Group.id, DatabaseID: Database.id, Level: Event.target.value as AccessLevel | '' })}
                    >
                      <option value="">No access</option>
                      <option value="view_only">View only</option>
                      <option value="view_edit">View and edit</option>
                    </select>
                  </td>
                )
              })}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function CurrentLevel(Matrix: AdminMatrix, GroupID: string, DatabaseID: string): AccessLevel | '' {
  return Matrix.grants.find(Grant => Grant.group_id === GroupID && Grant.database_id === DatabaseID)?.level ?? ''
}
