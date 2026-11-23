import { afterEach as AfterEach, expect as Expect, test as Test, vi as Vi } from 'vitest'
import { cleanup as Cleanup, render as Render, screen as Screen } from '@testing-library/react'
import UserEvent from '@testing-library/user-event'
import { ConfirmDialog } from '../src/ui/ConfirmDialog'

AfterEach(() => Cleanup())

function Dialog(Props: { OnCancel: () => void; OnConfirm?: () => void; Pending?: boolean }) {
  return (
    <ConfirmDialog
      Title="Delete draft migration"
      Description="Delete draft migration v2?"
      ConfirmLabel="Delete draft"
      Pending={Props.Pending ?? false}
      OnConfirm={Props.OnConfirm ?? (() => undefined)}
      OnCancel={Props.OnCancel}
    />
  )
}

Test('focuses cancel, keeps focus across re-renders, and traps Tab inside the dialog', async () => {
  const User = UserEvent.setup()
  const View = Render(<Dialog OnCancel={() => undefined} />)
  const Cancel = Screen.getByRole('button', { name: 'Cancel' })
  const Confirm = Screen.getByRole('button', { name: 'Delete draft' })
  Expect(document.activeElement).toBe(Cancel)

  await User.tab()
  Expect(document.activeElement).toBe(Confirm)
  View.rerender(<Dialog OnCancel={() => undefined} />)
  Expect(document.activeElement).toBe(Confirm)

  await User.tab()
  Expect(document.activeElement).toBe(Cancel)
  await User.tab({ shift: true })
  Expect(document.activeElement).toBe(Confirm)
})

Test('Escape uses the latest cancel handler and pending disables confirmation', async () => {
  const User = UserEvent.setup()
  const First = Vi.fn()
  const Latest = Vi.fn()
  const View = Render(<Dialog OnCancel={First} />)
  View.rerender(<Dialog OnCancel={Latest} Pending />)
  Expect((Screen.getByRole('button', { name: 'Working…' }) as HTMLButtonElement).disabled).toBe(true)
  await User.keyboard('{Escape}')
  Expect(First).not.toHaveBeenCalled()
  Expect(Latest).toHaveBeenCalledTimes(1)
})
