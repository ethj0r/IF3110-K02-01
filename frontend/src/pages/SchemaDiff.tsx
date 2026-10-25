import { useMutation, useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { AccessQueryKeys, ListAccessibleDatabases } from '../api/access'
import { ListSchemas } from '../api/databases'
import { CompareSchemas } from '../api/schemaDiff'
import { RequestError } from '../components/Feedback'

export function SchemaDiffPage() {
  const { dbId: DatabaseID = '' } = useParams<{ dbId: string }>()
  return <Comparison key={DatabaseID} DatabaseID={DatabaseID} />
}

function Comparison({ DatabaseID }: { DatabaseID: string }) {
  const NavigateTo = useNavigate()
  const Databases = useQuery({ queryKey: AccessQueryKeys.databases, queryFn: ListAccessibleDatabases })
  const Database = Databases.data?.find(Item => Item.id === DatabaseID)
  const [ReferenceID, SetReferenceID] = useState('')
  const [Schema, SetSchema] = useState('')
  const Schemas = useQuery({
    queryKey: ['schema-diff', 'schemas', ReferenceID],
    queryFn: () => ListSchemas(ReferenceID),
    enabled: ReferenceID !== '' && Database?.level === 'view_edit',
  })
  const Names = Schemas.data?.schemas ?? []
  const ActiveSchema = Names.includes(Schema) ? Schema : Names.includes('public') ? 'public' : Names[0] ?? ''
  const Compare = useMutation({ mutationFn: () => CompareSchemas(DatabaseID, ReferenceID, ActiveSchema) })

  return (
    <main className="workspace-main">
      <nav aria-label="Main navigation">
        <Link to={`/databases/${DatabaseID}`}>Browse database</Link>{' '}
        <Link to={`/databases/${DatabaseID}/migrations`}>Migrations</Link>
      </nav>
      <h1>Schema comparison</h1>
      <RequestError error={Databases.error} />
      {Databases.isPending && <p role="status">Loading databases…</p>}
      {!Databases.isPending && !Databases.isError && !Database && <p role="alert">No access to this database.</p>}
      {Database && Database.level !== 'view_edit' && <p role="alert">View and edit access is required to generate a migration.</p>}
      {Database?.level === 'view_edit' && (
        <>
          <p>Compare <strong>{Database.name}</strong> with a reference database to generate SQL for the same named schema.</p>
          <p>Generation reads both databases. Review the SQL in the migration editor before saving or executing it.</p>
          <form className="panel stack" onSubmit={Event => { Event.preventDefault(); Compare.mutate() }}>
            <label htmlFor="reference-database">Reference database</label>
            <select id="reference-database" value={ReferenceID} disabled={Compare.isPending} onChange={Event => { SetReferenceID(Event.target.value); SetSchema(''); Compare.reset() }} required>
              <option value="">Choose a reference…</option>
              {Databases.data?.map(Item => <option key={Item.id} value={Item.id}>{Item.name}</option>)}
            </select>
            <label htmlFor="comparison-schema">Schema</label>
            <select id="comparison-schema" value={ActiveSchema} disabled={!ReferenceID || Schemas.isPending || Schemas.isError || Compare.isPending} onChange={Event => { SetSchema(Event.target.value); Compare.reset() }} required>
              <option value="">Choose a schema…</option>
              {Names.map(Name => <option key={Name} value={Name}>{Name}</option>)}
            </select>
            <RequestError error={Schemas.error} />
            <button type="submit" disabled={!ReferenceID || !ActiveSchema || Schemas.isPending || Schemas.isError || Compare.isPending}>{Compare.isPending ? 'Comparing…' : 'Compare schemas'}</button>
          </form>
          <RequestError error={Compare.error} />
          {Compare.data && !Compare.isPending && (
            <section className="panel stack" aria-label="Schema comparison results">
              <h2>Schema differences</h2>
              {Compare.data.statements.length === 0 ? <p role="status">No schema differences found.</p> : (
                <>
                  <p>{Compare.data.statements.length} SQL statements proposed. Neither database has been changed.</p>
                  <ol>
                    {Compare.data.statements.map((Change, Index) => (
                      <li key={Index}>
                        <pre>{Change.sql}</pre>
                        {Change.hazards.length > 0 && <ul>{Change.hazards.map((Risk, RiskIndex) => <li key={RiskIndex}><strong>{Risk.type}</strong>: {Risk.message}</li>)}</ul>}
                      </li>
                    ))}
                  </ol>
                  <div className="field">
                    <label htmlFor="generated-sql">Generated SQL</label>
                    <textarea id="generated-sql" value={Compare.data.sql_text} readOnly rows={12} />
                  </div>
                  <button type="button" onClick={() => NavigateTo(`/databases/${DatabaseID}/migrations`, { state: { GeneratedMigration: { DatabaseID, Title: `Sync schema ${Compare.data.schema}`, SQLText: Compare.data.sql_text } } })}>Use SQL in migration editor</button>
                  <p>Run comparison again if either schema changes before execution.</p>
                </>
              )}
            </section>
          )}
        </>
      )}
    </main>
  )
}
