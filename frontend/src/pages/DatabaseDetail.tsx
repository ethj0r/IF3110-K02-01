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
import { PageHeading } from '../components/PageHeading'
import { LevelBadge, RequestError } from '../components/Feedback'

function formatCell(Value: string | null) {
  if (Value === null) return <span className="muted">NULL</span>
  return <span>{Value}</span>
}

function isForbidden(Error: unknown) {
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

  const [Schema, setSchema] = useState('')
  const [Table, setTable] = useState('')
  const [Page, setPage] = useState(1)
  const [PageSize, setPageSize] = useState(25)

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
  const Forbidden = [Schemas.error, Tables.error, Columns.error, Rows.error].some(isForbidden)

  return (
    <div className="workspace-shell">
      <header className="app-header">
        <Link to="/" className="app-brand">
          <strong>Database Platform</strong>
          <small>Workspace</small>
        </Link>
        <div className="app-header-actions">
          <Link to="/" className="back-link">← Databases</Link>
        </div>
      </header>

      <main className="workspace-main">
        <PageHeading
          Title={Database?.name ?? 'Database'}
          Description={Database ? `Inspect schema and data. Access level below.` : 'Loading database…'}
          Actions={Database ? <LevelBadge level={Database.level} /> : undefined}
        />

        {Forbidden ? (
          <section className="panel">
            <div className="empty-state">
              <h3>No access to this database</h3>
              <p>Ask an administrator to grant your group access first.</p>
              <Link to="/" className="link-button">Back to databases →</Link>
            </div>
          </section>
        ) : (
          <>
            <RequestError error={Schemas.error} />
            {Schemas.isPending ? (
              <p className="state" role="status">Loading schema…</p>
            ) : (
              <div className="stack">
                <section className="panel">
                  <header className="panel-header">
                    <div>
                      <h2>Schema and tables</h2>
                      <p>Pick a schema, then a table to inspect.</p>
                    </div>
                  </header>
                  <div className="form-grid">
                    <label className="field">
                      <span>Schema</span>
                      <select value={ActiveSchema} onChange={Event => { setSchema(Event.target.value); setTable(''); setPage(1) }}>
                        {SchemaNames.map(Name => (
                          <option key={Name} value={Name}>{Name}</option>
                        ))}
                      </select>
                    </label>
                    <label className="field">
                      <span>Table</span>
                      <select value={Table} onChange={Event => { setTable(Event.target.value); setPage(1) }}>
                        <option value="">Select a table…</option>
                        {(Tables.data?.tables ?? []).map(Item => (
                          <option key={Item.name} value={Item.name}>{Item.name} ({Item.type === 'VIEW' ? 'view' : 'table'})</option>
                        ))}
                      </select>
                    </label>
                  </div>
                  <RequestError error={Tables.error} />
                </section>

                {Table !== '' && (
                  <>
                    <section className="panel">
                      <header className="panel-header">
                        <div>
                          <h2>Columns</h2>
                          <p><code>{Table}</code></p>
                        </div>
                      </header>
                      {Columns.isPending ? <p className="state" role="status">Loading columns…</p> : null}
                      <RequestError error={Columns.error} />
                      {Columns.data && (
                        <div className="table-wrap">
                          <table className="data-table">
                            <thead><tr><th>Name</th><th>Type</th><th>Nullable</th><th>Default</th></tr></thead>
                            <tbody>
                              {Columns.data.columns.map(Column => (
                                <tr key={Column.name}>
                                  <td><code>{Column.name}</code></td>
                                  <td><code>{Column.data_type}</code></td>
                                  <td>{Column.nullable ? 'yes' : 'no'}</td>
                                  <td>{Column.default === null ? <span className="muted">—</span> : <code>{Column.default}</code>}</td>
                                </tr>
                              ))}
                            </tbody>
                          </table>
                        </div>
                      )}
                    </section>

                    <section className="panel">
                      <header className="panel-header">
                        <div>
                          <h2>Constraints and indexes</h2>
                        </div>
                      </header>
                      <RequestError error={Constraints.error} />
                      <RequestError error={Indexes.error} />
                      <div className="table-wrap">
                        <table className="data-table">
                          <thead><tr><th>Constraint</th><th>Type</th><th>Columns</th></tr></thead>
                          <tbody>
                            {(Constraints.data?.constraints ?? []).map(Item => (
                              <tr key={Item.name}>
                                <td><code>{Item.name}</code></td>
                                <td>{Item.type}</td>
                                <td><code>{Item.columns === '' ? '—' : Item.columns}</code></td>
                              </tr>
                            ))}
                            {(Constraints.data?.constraints.length ?? 0) === 0 && (
                              <tr><td colSpan={3}><span className="muted">No constraints</span></td></tr>
                            )}
                          </tbody>
                        </table>
                      </div>
                      <div className="table-wrap">
                        <table className="data-table">
                          <thead><tr><th>Index</th><th>Definition</th></tr></thead>
                          <tbody>
                            {(Indexes.data?.indexes ?? []).map(Item => (
                              <tr key={Item.name}>
                                <td><code>{Item.name}</code></td>
                                <td><code>{Item.definition}</code></td>
                              </tr>
                            ))}
                            {(Indexes.data?.indexes.length ?? 0) === 0 && (
                              <tr><td colSpan={2}><span className="muted">No indexes</span></td></tr>
                            )}
                          </tbody>
                        </table>
                      </div>
                    </section>

                    <section className="panel">
                      <header className="panel-header">
                        <div>
                          <h2>Data</h2>
                          <p>{Rows.data ? `${Rows.data.total} rows` : ''}</p>
                        </div>
                        <span className="count-badge">Page {Page} of {TotalPages}</span>
                      </header>
                      {Rows.isPending ? <p className="state" role="status">Loading rows…</p> : null}
                      <RequestError error={Rows.error} />
                      {Rows.data && (
                        <>
                          <div className="table-wrap">
                            <table className="data-table">
                              <thead><tr>{Rows.data.columns.map(Name => <th key={Name}><code>{Name}</code></th>)}</tr></thead>
                              <tbody>
                                {Rows.data.rows.map((Row, Index) => (
                                  <tr key={Index}>
                                    {Row.map((Value, Cell) => <td key={Cell}>{formatCell(Value)}</td>)}
                                  </tr>
                                ))}
                                {Rows.data.rows.length === 0 && (
                                  <tr><td colSpan={Math.max(1, Rows.data.columns.length)}><span className="muted">No rows</span></td></tr>
                                )}
                              </tbody>
                            </table>
                          </div>
                          <div className="page-actions">
                            <label className="field">
                              <span>Rows per page</span>
                              <select value={PageSize} onChange={Event => { setPageSize(Number(Event.target.value)); setPage(1) }}>
                                {PageSizeOptions.map(Size => <option key={Size} value={Size}>{Size}</option>)}
                              </select>
                            </label>
                            <button className="secondary" disabled={Page <= 1} onClick={() => setPage(Value => Value - 1)}>← Previous</button>
                            <button className="secondary" disabled={Page >= TotalPages} onClick={() => setPage(Value => Value + 1)}>Next →</button>
                          </div>
                        </>
                      )}
                    </section>
                  </>
                )}
              </div>
            )}
          </>
        )}
      </main>
    </div>
  )
}
