import { expect, test, type Page } from '@playwright/test'
import { clientAddress } from './client-address'
import { hydrated } from './hydrated'

test.beforeEach(async ({ page }, testInfo) => {
  await page.setExtraHTTPHeaders({ 'X-Forwarded-For': clientAddress(testInfo) })
  await page.route('**/api/v1/me', route => route.fulfill({ status: 204 }))
})

async function holdScripts(page: Page) {
  let release!: () => void
  const gate = new Promise<void>(resolve => { release = resolve })
  await page.route('**/_nuxt/**/*.js', async route => { await gate; await route.continue() })
  return release
}
async function expectLocked(page: Page, count: number) {
  await expect(page.locator('form fieldset')).toHaveAttribute('disabled', '')
  const fields = page.locator('form input')
  expect(await fields.count()).toBe(count)
  for (let index = 0; index < count; index++) await expect(fields.nth(index)).toBeDisabled()
  await expect(page.locator('form button.btn-primary')).toBeDisabled()
}

test('login is locked until hydration, then signs in with next', async ({ page }) => {
  let body: unknown
  await page.route('**/api/v1/auth/password', async route => { body = route.request().postDataJSON(); await route.fulfill({ json: { next: '/connect' } }) })
  const release = await holdScripts(page)
  await page.goto('/login?next=%2Fvn%2Fchanges', { waitUntil: 'commit' })
  await expectLocked(page, 2)
  release()
  await hydrated(page)
  await expect(page.locator('form fieldset')).not.toHaveAttribute('disabled')
  await page.locator('input[type=email]').fill('person@example.invalid')
  await page.locator('input[type=password]').fill('synthetic long password')
  await page.locator('form button.btn-primary').click()
  await expect(page).toHaveURL(/\/connect$/)
  expect(body).toEqual({ email: 'person@example.invalid', password: 'synthetic long password', next: '/vn/changes' })
})

for (const path of ['/signup', '/reset']) {
  test(`${path} is locked until hydration`, async ({ page }) => {
    const release = await holdScripts(page)
    await page.goto(path, { waitUntil: 'commit' })
    await expectLocked(page, 1)
    release()
    await hydrated(page)
    await expect(page.locator('form fieldset')).not.toHaveAttribute('disabled')
    await page.locator('input[type=email]').fill('person@example.invalid')
    await expect(page.locator('input[type=email]')).toHaveValue('person@example.invalid')
  })
}
