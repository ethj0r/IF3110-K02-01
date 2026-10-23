import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { AdminQueryKeys, GetMatrix, RemoveGroupAccess, SetGroupAccess } from '../../api/admin'
import type { AccessLevel, AdminMatrix } from '../../api/admin'
import { RequestError } from '../../components/Feedback'
import { PageHeading } from '../../components/PageHeading'

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
    <section>
      <PageHeading
        Title="Database access"
        Description="Choose the highest permission each group receives. View and edit wins over view only. Platform administrators receive no access unless a group grants it."
      />

      <div className="legend">
        <span><span className="level-badge view">View only</span> can read database content.</span>
        <span><span className="level-badge edit">View and edit</span> can read and change content.</span>
      </div>

      <RequestError error={Change.error} />

      <div className="panel">
        <header className="panel-header">
          <div>
            <h3>Group grants</h3>
            <p>Changes take effect immediately and may rotate or revoke member sessions.</p>
          </div>
          <span className="count-badge">{Matrix.data?.grants.length ?? 0}</span>
        </header>

        {Matrix.isPending && <p className="state" role="status">Loading access matrix…</p>}
        <RequestError error={Matrix.error} />

        {Matrix.data && (Matrix.data.groups.length === 0 || Matrix.data.databases.length === 0) && (
          <div className="empty-state">
            <h3>Nothing to assign yet</h3>
            <p>
              {Matrix.data.groups.length === 0
                ? 'Create a group before assigning database access.'
                : 'No database connections are configured. Add a connection before assigning access.'}
            </p>
            {Matrix.data.groups.length === 0 && <Link to="/admin/groups" className="link-button">Go to groups →</Link>}
          </div>
        )}

        {Matrix.data && Matrix.data.groups.length > 0 && Matrix.data.databases.length > 0 && (
          <MatrixTable Matrix={Matrix.data} Disabled={Change.isPending} OnChange={Input => Change.mutate(Input)} />
        )}
      </div>
    </section>
  )
}

function MatrixTable({ Matrix, Disabled, OnChange }: {
  Matrix: AdminMatrix
  Disabled: boolean
  OnChange: (Input: { GroupID: string; DatabaseID: string; Level: AccessLevel | '' }) => void
}) {
  return (
    <div className="table-wrap matrix-wrap">
      <table className="data-table matrix">
        <thead>
          <tr>
            <th scope="col" className="sticky-column">Group</th>
            {Matrix.databases.map(Database => (
              <th key={Database.id} scope="col">
                {Database.name}
                <span className="table-subtext">{Database.host} · {Database.dbname}</span>
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {Matrix.groups.map(Group => (
            <tr key={Group.id}>
              <th scope="row" className="sticky-column">
                <strong>{Group.name}</strong>
                <span className="table-subtext">{Group.member_count} member{Group.member_count === 1 ? '' : 's'}</span>
              </th>
              {Matrix.databases.map(Database => {
                const Current = CurrentLevel(Matrix, Group.id, Database.id)
                return (
                  <td key={Database.id}>
                    <select
                      className={`level-select ${Current === 'view_edit' ? 'edit' : Current === 'view_only' ? 'view' : ''}`}
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
