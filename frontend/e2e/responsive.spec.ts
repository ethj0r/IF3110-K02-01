import { expect as Expect, test as Test } from '@playwright/test'
import type { Page } from '@playwright/test'

const Viewports = [
  { Name: 'small mobile', Width: 320, Height: 640 },
  { Name: 'mobile', Width: 360, Height: 780 },
  { Name: 'tablet', Width: 820, Height: 1180 },
  { Name: 'desktop', Width: 1440, Height: 900 },
]

const AdminPages = ['/admin/users', '/admin/groups', '/admin/access', '/admin/connections']

async function ExpectNoPageOverflow(Page: Page, Label: string) {
  const Overflow = await Page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
  Expect(Overflow, `${Label} scrolls horizontally`).toBeLessThanOrEqual(0)
}

for (const Viewport of Viewports) {
  Test(`layout stays within the ${Viewport.Name} viewport and navigation is reachable`, async ({ browser: Browser }) => {
    const Origin = process.env.APP_ORIGIN || 'http://127.0.0.1:5173'
    const Username = process.env.BOOTSTRAP_ADMIN_USERNAME
    const Password = process.env.BOOTSTRAP_ADMIN_PASSWORD
    if (!Username || !Password) throw new Error('Run make test-e2e with administrator credentials configured')
    if (!['127.0.0.1', 'localhost', '[::1]'].includes(new URL(Origin).hostname)) throw new Error('Responsive checks require a local development application')

    const Context = await Browser.newContext({ baseURL: Origin, viewport: { width: Viewport.Width, height: Viewport.Height } })
    const Page = await Context.newPage()
    const PageErrors: string[] = []
    Page.on('pageerror', Error => PageErrors.push(Error.message))
    try {
      await Page.goto('/login')
      await ExpectNoPageOverflow(Page, 'login')
      await Page.getByLabel('Username', { exact: true }).fill(Username)
      await Page.getByLabel('Password', { exact: true }).fill(Password)
      await Page.getByRole('button', { name: 'Sign in', exact: true }).click()
      await Expect(Page.getByRole('heading', { name: 'Database Workspace', exact: true })).toBeVisible()
      await ExpectNoPageOverflow(Page, 'workspace')

      const Menu = Page.getByRole('button', { name: 'Open menu', exact: true })
      const Users = Page.getByRole('link', { name: 'Users', exact: true })
      if (Viewport.Width <= 1024) {
        await Expect(Users).toBeHidden()
        await Menu.click()
        await Expect(Menu).toHaveAttribute('aria-expanded', 'true')
        await Expect(Users).toBeVisible()
        await Page.keyboard.press('Escape')
        await Expect(Users).toBeHidden()
        await Menu.click()
        await Users.click()
        await Expect(Users).toBeHidden()
      } else {
        await Expect(Menu).toBeHidden()
        await Users.click()
      }
      await Expect(Page.getByRole('heading', { name: 'Users', exact: true })).toBeVisible()

      for (const Path of ['/migrations', ...AdminPages]) {
        await Page.goto(Path)
        await Expect(Page.locator('h1')).toBeVisible()
        await ExpectNoPageOverflow(Page, Path)
      }

      await Page.goto('/')
      const FirstDatabase = Page.locator('.db-card-link').first()
      if (await FirstDatabase.count()) {
        await FirstDatabase.click()
        await Expect(Page.getByLabel('Schema', { exact: true })).toBeVisible()
        await ExpectNoPageOverflow(Page, 'database viewer')
        await Page.getByRole('link', { name: 'Migrations', exact: true }).click()
        await Expect(Page.getByRole('heading', { name: 'Database Migrations', exact: true })).toBeVisible()
        await ExpectNoPageOverflow(Page, 'database migrations')
      }
      Expect(PageErrors).toEqual([])
    } finally {
      await Context.close()
    }
  })
}
