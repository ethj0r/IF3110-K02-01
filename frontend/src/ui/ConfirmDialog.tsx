import { useEffect, useRef } from 'react'

export function ConfirmDialog({ Title, Description, ConfirmLabel, Pending, OnConfirm, OnCancel }: {
  Title: string
  Description: string
  ConfirmLabel: string
  Pending: boolean
  OnConfirm: () => void
  OnCancel: () => void
}) {
  const Panel = useRef<HTMLDivElement>(null)
  const CancelButton = useRef<HTMLButtonElement>(null)
  const CancelHandler = useRef(OnCancel)

  useEffect(() => {
    CancelHandler.current = OnCancel
  })

  useEffect(() => {
    const Previous = document.activeElement as HTMLElement | null
    CancelButton.current?.focus()
    function HandleKey(Event: KeyboardEvent) {
      if (Event.key === 'Escape') {
        CancelHandler.current()
        return
      }
      if (Event.key !== 'Tab' || !Panel.current) return
      const Focusable = Array.from(Panel.current.querySelectorAll<HTMLElement>('button:not(:disabled), [href], input, select, textarea'))
      if (Focusable.length === 0) return
      const First = Focusable[0]
      const Last = Focusable[Focusable.length - 1]
      if (Event.shiftKey && document.activeElement === First) {
        Event.preventDefault()
        Last.focus()
      } else if (!Event.shiftKey && document.activeElement === Last) {
        Event.preventDefault()
        First.focus()
      }
    }
    window.addEventListener('keydown', HandleKey)
    return () => {
      window.removeEventListener('keydown', HandleKey)
      Previous?.focus?.()
    }
  }, [])

  return (
    <div className="dialog-backdrop" onMouseDown={Event => { if (Event.target === Event.currentTarget) OnCancel() }}>
      <div ref={Panel} className="dialog" role="dialog" aria-modal="true" aria-labelledby="dialog-title" aria-describedby="dialog-description">
        <h2 id="dialog-title">{Title}</h2>
        <p id="dialog-description">{Description}</p>
        <div className="dialog-actions">
          <button ref={CancelButton} className="btn btn-secondary" type="button" onClick={OnCancel}>Cancel</button>
          <button className="btn btn-danger-solid" type="button" onClick={OnConfirm} disabled={Pending}>
            {Pending ? 'Working…' : ConfirmLabel}
          </button>
        </div>
      </div>
    </div>
  )
}
