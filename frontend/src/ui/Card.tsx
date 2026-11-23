import type { ReactNode } from 'react'
import { Link } from 'react-router-dom'

export function PageHeader({ Crumbs, Title, Description, Actions }: {
  Crumbs?: { Label: string; To: string }[]
  Title: string
  Description?: ReactNode
  Actions?: ReactNode
}) {
  return (
    <header className="page-header">
      <div className="page-title">
        {Crumbs && Crumbs.length > 0 && (
          <nav className="crumbs" aria-label="Breadcrumb">
            {Crumbs.map(Crumb => <Link key={Crumb.To} to={Crumb.To}>{Crumb.Label}</Link>)}
          </nav>
        )}
        <h1>{Title}</h1>
        {Description && <p>{Description}</p>}
      </div>
      {Actions && <div className="page-actions">{Actions}</div>}
    </header>
  )
}

export function Card({ Title, Description, Actions, Flush = false, Label, children, As = 'section' }: {
  Title?: ReactNode
  Description?: ReactNode
  Actions?: ReactNode
  Flush?: boolean
  Label?: string
  children?: ReactNode
  As?: 'section' | 'div'
}) {
  const Tag = As
  const HasHeader = Title !== undefined || Description !== undefined || Actions !== undefined
  return (
    <Tag className="card" aria-label={Label}>
      {HasHeader && (
        <header className="card-header">
          <div className="card-title">
            {Title !== undefined && <h2>{Title}</h2>}
            {Description !== undefined && <p>{Description}</p>}
          </div>
          {Actions && <div className="card-actions">{Actions}</div>}
        </header>
      )}
      {children !== undefined && children !== null && children !== false && (
        <div className={Flush ? 'card-flush' : 'card-body'}>{children}</div>
      )}
    </Tag>
  )
}

export function CodeBlock({ Label, Code, Actions }: { Label: string; Code: string; Actions?: ReactNode }) {
  return (
    <div className="code-block">
      <div className="code-block-header">
        <span className="label">{Label}</span>
        {Actions}
      </div>
      <pre>{Code}</pre>
    </div>
  )
}
