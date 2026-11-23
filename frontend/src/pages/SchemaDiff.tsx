import { useMutation, useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { AccessQueryKeys, ListAccessibleDatabases } from '../api/access'
import { ListSchemas } from '../api/databases'
import { CompareSchemas } from '../api/schemaDiff'
import { DatabaseTabs } from '../layout/DatabaseTabs'
import { Card, PageHeader } from '../ui/Card'
import { Badge, Loading, Notice, RequestError } from '../ui/Feedback'
import { Icon } from '../ui/Icon'

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
  const Result = Compare.data && !Compare.isPending ? Compare.data : null

  function UseInEditor() {
    if (!Result) return
    void NavigateTo(`/databases/${DatabaseID}/migrations`, {
      state: {
        GeneratedMigration: {
          DatabaseID,
          Title: `${Result.requires_recomparison ? 'Prepare enums in' : 'Sync schema'} ${Result.schema}`,
          SQLText: Result.sql_text,
        },
      },
    })
  }

  return (
    <>
      <PageHeader
        Eyebrow={Database?.name ?? 'Database'}
        Title="Schema comparison"
        Description="Compare this database with a reference to generate reviewable SQL for the same named schema. Neither database is changed."
      />
      {Database && <DatabaseTabs Database={Database} />}

      <RequestError error={Databases.error} />
      {Databases.isPending && <Loading>Loading databases…</Loading>}
      {!Databases.isPending && !Databases.isError && !Database && <Notice Tone="danger" Role="alert">No access to this database.</Notice>}
      {Database && Database.level !== 'view_edit' && <Notice Tone="warning" Role="alert">View and edit access is required to generate a migration.</Notice>}

      {Database?.level === 'view_edit' && (
        <div className="stack-lg">
          <Card
            Title={<>Compare <strong>{Database.name}</strong> with a reference</>}
            Description="Generation reads both databases. Review the SQL in the migration editor before saving or executing it."
          >
            <form className="stack" onSubmit={Event => { Event.preventDefault(); Compare.mutate() }}>
              <div className="form-grid">
                <div className="field">
                  <label htmlFor="reference-database">Reference database</label>
                  <select id="reference-database" className="select" value={ReferenceID} disabled={Compare.isPending} onChange={Event => { SetReferenceID(Event.target.value); SetSchema(''); Compare.reset() }} required>
                    <option value="">Choose a reference…</option>
                    {Databases.data?.map(Item => <option key={Item.id} value={Item.id}>{Item.name}</option>)}
                  </select>
                </div>
                <div className="field">
                  <label htmlFor="comparison-schema">Schema</label>
                  <select id="comparison-schema" className="select" value={ActiveSchema} disabled={!ReferenceID || Schemas.isPending || Schemas.isError || Compare.isPending} onChange={Event => { SetSchema(Event.target.value); Compare.reset() }} required>
                    <option value="">Choose a schema…</option>
                    {Names.map(Name => <option key={Name} value={Name}>{Name}</option>)}
                  </select>
                </div>
              </div>
              <RequestError error={Schemas.error} />
              <div className="form-actions">
                <button className="btn" type="submit" disabled={!ReferenceID || !ActiveSchema || Schemas.isPending || Schemas.isError || Compare.isPending}>
                  <Icon Name="diff" Size={14} />
                  {Compare.isPending ? 'Comparing…' : 'Compare schemas'}
                </button>
                <span className="field-hint">Direction: current → reference</span>
              </div>
            </form>
          </Card>

          <RequestError error={Compare.error} />

          {Result && (
            <Card
              Label="Schema comparison results"
              Title="Schema differences"
              Description={Result.statements.length === 0 ? undefined : `${Result.statements.length} schema change statements found. Neither database has been changed.`}
              Actions={Result.statements.length > 0 ? <Badge Tone="info">{Result.statements.length} statements</Badge> : undefined}
            >
              {Result.statements.length === 0 ? (
                <Notice Tone="success" Role="status">No schema differences found.</Notice>
              ) : (
                <>
                  {Result.requires_recomparison && (
                    <Notice Tone="warning" Role="status">This migration only adds enum values. Save and execute it first, then compare again to generate the remaining changes. New enum values must be committed before they can be used.</Notice>
                  )}
                  <ol className="list">
                    {Result.statements.map((Change, Index) => (
                      <li key={Index} className="list-item statement">
                        <span className="eyebrow">Statement {Index + 1}</span>
                        <pre className="code-pre">{Change.sql}</pre>
                        {Change.hazards.length > 0 && (
                          <ul className="hazards">
                            {Change.hazards.map((Risk, RiskIndex) => (
                              <li key={RiskIndex}><Notice Tone="warning"><strong>{Risk.type}</strong>: {Risk.message}</Notice></li>
                            ))}
                          </ul>
                        )}
                      </li>
                    ))}
                  </ol>
                  <div className="field">
                    <label htmlFor="generated-sql">Generated SQL</label>
                    <textarea id="generated-sql" className="textarea code-input" value={Result.sql_text} readOnly rows={12} />
                  </div>
                  <div className="form-actions">
                    <button className="btn" type="button" onClick={UseInEditor}>Use SQL in migration editor<Icon Name="arrow-right" Size={14} /></button>
                    <span className="field-hint">Run comparison again if either schema changes before execution.</span>
                  </div>
                </>
              )}
            </Card>
          )}

        </div>
      )}
    </>
  )
}
