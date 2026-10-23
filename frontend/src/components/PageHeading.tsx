import type { ReactNode } from 'react'

export function PageHeading({ Title, Description, Actions }: {
  Title: string
  Description: string
  Actions?: ReactNode
}) {
  return (
    <header className="page-heading">
      <div>
        <h1>{Title}</h1>
        <p className="muted">{Description}</p>
      </div>
      {Actions && <div className="page-actions">{Actions}</div>}
    </header>
  )
}
