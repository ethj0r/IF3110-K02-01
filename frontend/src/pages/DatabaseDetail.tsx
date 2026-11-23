import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { AccessQueryKeys, ListAccessibleDatabases } from '../api/access'
import { ApiError } from '../api/client'
import {
  DatabaseQueryKeys,
  ListColumns,
  ListConstraints,
  ListIndexes,
  ListRows,
  ListSchemas,
  ListTables,
} from '../api/databases'
import { DatabaseTabs } from '../layout/DatabaseTabs'
import { Card, PageHeader } from '../ui/Card'
import { Badge, EmptyState, LevelBadge, Loading, RequestError } from '../ui/Feedback'
import { Icon } from '../ui/Icon'

function FormatCell(Value: string | null) {
  if (Value === null) return <span className="muted">NULL</span>
  return <span>{Value}</span>
}

function IsForbidden(Error: unknown) {
  return Error instanceof ApiError && Error.Status === 403
}

const PageSizeOptions = [10, 25, 50, 100]

export function DatabaseDetailPage() {
  const { id } = useParams()
  const DatabaseId = id ?? ''
  const Access = useQuery({ queryKey: AccessQueryKeys.databases, queryFn: ListAccessibleDatabases })
  const Schemas = useQuery({
    queryKey: DatabaseQueryKeys.schemas(DatabaseId),
    queryFn: () => ListSchemas(DatabaseId),
    enabled: DatabaseId !== '',
  })

  const [Schema, SetSchema] = useState('')
  const [Table, SetTable] = useState('')
  const [Page, SetPage] = useState(1)
  const [PageSize, SetPageSize] = useState(25)

  const SchemaNames = Schemas.data?.schemas ?? []
  const ActiveSchema = Schema !== '' ? Schema : SchemaNames.includes('public') ? 'public' : (SchemaNames[0] ?? '')

  const Tables = useQuery({
    queryKey: DatabaseQueryKeys.tables(DatabaseId, ActiveSchema),
    queryFn: () => ListTables(DatabaseId, ActiveSchema),
    enabled: DatabaseId !== '' && ActiveSchema !== '',
  })
  const Columns = useQuery({
    queryKey: DatabaseQueryKeys.columns(DatabaseId, ActiveSchema, Table),
    queryFn: () => ListColumns(DatabaseId, ActiveSchema, Table),
    enabled: Table !== '',
  })
  const Constraints = useQuery({
    queryKey: DatabaseQueryKeys.constraints(DatabaseId, ActiveSchema, Table),
    queryFn: () => ListConstraints(DatabaseId, ActiveSchema, Table),
    enabled: Table !== '',
  })
  const Indexes = useQuery({
    queryKey: DatabaseQueryKeys.indexes(DatabaseId, ActiveSchema, Table),
    queryFn: () => ListIndexes(DatabaseId, ActiveSchema, Table),
    enabled: Table !== '',
  })
  const Rows = useQuery({
    queryKey: DatabaseQueryKeys.rows(DatabaseId, ActiveSchema, Table, Page, PageSize),
    queryFn: () => ListRows(DatabaseId, ActiveSchema, Table, Page, PageSize),
    enabled: Table !== '',
  })

  const Database = Access.data?.find(Item => Item.id === DatabaseId)
  const TotalPages = Rows.data ? Math.max(1, Math.ceil(Rows.data.total / Rows.data.page_size)) : 1
  const Forbidden = [Schemas.error, Tables.error, Columns.error, Rows.error].some(IsForbidden)
  const TableCount = Tables.data?.tables.length ?? 0

  return (
    <>
      <PageHeader
        Eyebrow="Database"
        Title={Database?.name ?? 'Database'}
        Description={Database ? <><span className="mono">{Database.dbname}</span> on {Database.host}</> : 'Loading database…'}
        Actions={Database ? <LevelBadge level={Database.level} /> : undefined}
      />

      {Database && <DatabaseTabs Database={Database} />}

      {Forbidden ? (
        <div className="card">
          <EmptyState IconName="lock" Title="No access to this database">
            <p>Ask an administrator to grant your group access first.</p>
            <Link to="/" className="btn btn-secondary btn-sm">Back to databases<Icon Name="arrow-right" Size={14} /></Link>
          </EmptyState>
        </div>
      ) : (
        <>
          <RequestError error={Schemas.error} />
          {Schemas.isPending ? (
            <Loading>Loading schema…</Loading>
          ) : (
            <div className="stack-lg">
              <Card
                Title="Schema and tables"
                Description="Pick a schema, then a table to inspect its structure and data."
                Actions={Tables.data ? <Badge Tone="outline">{TableCount} object{TableCount === 1 ? '' : 's'}</Badge> : undefined}
              >
                <div className="form-grid">
                  <div className="field">
                    <label htmlFor="schema-select">Schema</label>
                    <select id="schema-select" className="select" value={ActiveSchema} onChange={Event => { SetSchema(Event.target.value); SetTable(''); SetPage(1) }}>
                      {SchemaNames.map(Name => <option key={Name} value={Name}>{Name}</option>)}
                    </select>
                  </div>
                  <div className="field">
                    <label htmlFor="table-select">Table</label>
                    <select id="table-select" className="select" value={Table} onChange={Event => { SetTable(Event.target.value); SetPage(1) }}>
                      <option value="">Select a table…</option>
                      {(Tables.data?.tables ?? []).map(Item => (
                        <option key={Item.name} value={Item.name}>{Item.name} ({Item.type === 'VIEW' ? 'view' : 'table'})</option>
                      ))}
                    </select>
                  </div>
                </div>
                <RequestError error={Tables.error} />
              </Card>

              {Table === '' && (
                <div className="card">
                  <EmptyState IconName="table" Title="Choose a table">
                    <p>Columns, constraints, indexes, and paginated rows appear here once a table is selected.</p>
                  </EmptyState>
                </div>
              )}

              {Table !== '' && (
                <>
                  <Card Title="Columns" Description={<code className="inline-code">{ActiveSchema}.{Table}</code>} Flush>
                    {Columns.isPending && <div className="card-body"><Loading>Loading columns…</Loading></div>}
                    {Columns.error !== null && <div className="card-body"><RequestError error={Columns.error} /></div>}
                    {Columns.data && (
                      <div className="table-scroll">
                        <table className="table">
                          <thead><tr><th>Name</th><th>Type</th><th>Nullable</th><th>Default</th></tr></thead>
                          <tbody>
                            {Columns.data.columns.map(Column => (
                              <tr key={Column.name}>
                                <td><code>{Column.name}</code></td>
                                <td><code className="inline-code">{Column.data_type}</code></td>
                                <td>{Column.nullable ? 'yes' : 'no'}</td>
                                <td>{Column.default === null ? <span className="muted">—</span> : <code>{Column.default}</code>}</td>
                              </tr>
                            ))}
                          </tbody>
                        </table>
                      </div>
                    )}
                  </Card>

                  <div className="split">
                    <Card Title="Constraints" Flush>
                      {Constraints.error !== null && <div className="card-body"><RequestError error={Constraints.error} /></div>}
                      <div className="table-scroll">
                        <table className="table">
                          <thead><tr><th>Constraint</th><th>Type</th><th>Columns</th><th>Definition</th></tr></thead>
                          <tbody>
                            {(Constraints.data?.constraints ?? []).map(Item => (
                              <tr key={Item.name}>
                                <td><code>{Item.name}</code></td>
                                <td>{Item.type}</td>
                                <td><code>{Item.columns === '' ? '—' : Item.columns}</code></td>
                                <td><code>{Item.definition}</code></td>
                              </tr>
                            ))}
                            {(Constraints.data?.constraints.length ?? 0) === 0 && (
                              <tr><td colSpan={4} className="cell-empty">No constraints</td></tr>
                            )}
                          </tbody>
                        </table>
                      </div>
                    </Card>

                    <Card Title="Indexes" Flush>
                      {Indexes.error !== null && <div className="card-body"><RequestError error={Indexes.error} /></div>}
                      <div className="table-scroll">
                        <table className="table">
                          <thead><tr><th>Index</th><th>Definition</th></tr></thead>
                          <tbody>
                            {(Indexes.data?.indexes ?? []).map(Item => (
                              <tr key={Item.name}>
                                <td><code>{Item.name}</code></td>
                                <td><code>{Item.definition}</code></td>
                              </tr>
                            ))}
                            {(Indexes.data?.indexes.length ?? 0) === 0 && (
                              <tr><td colSpan={2} className="cell-empty">No indexes</td></tr>
                            )}
                          </tbody>
                        </table>
                      </div>
                    </Card>
                  </div>

                  <Card
                    Title="Data"
                    Description={Rows.data ? `${Rows.data.total} rows` : 'Rows ordered by primary key.'}
                    Actions={<Badge Tone="outline">Page {Page} of {TotalPages}</Badge>}
                    Flush
                  >
                    {Rows.isPending && <div className="card-body"><Loading>Loading rows…</Loading></div>}
                    {Rows.error !== null && <div className="card-body"><RequestError error={Rows.error} /></div>}
                    {Rows.data && (
                      <>
                        <div className="table-scroll">
                          <table className="table table-data">
                            <thead><tr>{Rows.data.columns.map(Name => <th key={Name}>{Name}</th>)}</tr></thead>
                            <tbody>
                              {Rows.data.rows.map((Row, Index) => (
                                <tr key={Index}>
                                  {Row.map((Value, Cell) => <td key={Cell}>{FormatCell(Value)}</td>)}
                                </tr>
                              ))}
                              {Rows.data.rows.length === 0 && (
                                <tr><td colSpan={Math.max(1, Rows.data.columns.length)} className="cell-empty">No rows</td></tr>
                              )}
                            </tbody>
                          </table>
                        </div>
                        <div className="card-body">
                          <div className="toolbar">
                            <div className="field field-compact">
                              <label htmlFor="page-size">Rows per page</label>
                              <select id="page-size" className="select" value={PageSize} onChange={Event => { SetPageSize(Number(Event.target.value)); SetPage(1) }}>
                                {PageSizeOptions.map(Size => <option key={Size} value={Size}>{Size}</option>)}
                              </select>
                            </div>
                            <div className="form-actions">
                              <button className="btn btn-secondary btn-sm" type="button" disabled={Page <= 1} onClick={() => SetPage(Value => Value - 1)}>
                                <Icon Name="chevron-left" Size={14} />Previous
                              </button>
                              <button className="btn btn-secondary btn-sm" type="button" disabled={Page >= TotalPages} onClick={() => SetPage(Value => Value + 1)}>
                                Next<Icon Name="chevron-right" Size={14} />
                              </button>
                            </div>
                          </div>
                        </div>
                      </>
                    )}
                  </Card>
                </>
              )}
            </div>
          )}
        </>
      )}
    </>
  )
}
