import { expect, test } from '@playwright/test'
import { clientAddress } from './client-address'

test.use({ javaScriptEnabled: false })
test.beforeEach(async ({ page }, testInfo) => {
  await page.setExtraHTTPHeaders({ 'X-Forwarded-For': clientAddress(testInfo) })
})

const pages = [
  { path: '/login?next=%2Fvn%2Fchanges', inputs: 2 },
  { path: '/login/factor', inputs: 1 },
  { path: '/signup', inputs: 1 },
  { path: '/reset', inputs: 1 },
]
for (const { path, inputs } of pages) {
  test(`${path} is disabled before hydration`, async ({ page }) => {
    await page.goto(path)
    await expect(page.locator('form fieldset')).toHaveAttribute('disabled', '')
    const fields = page.locator('form input')
    expect(await fields.count()).toBe(inputs)
    for (let index = 0; index < inputs; index++) await expect(fields.nth(index)).toBeDisabled()
    await expect(page.locator('form button.btn-primary')).toBeDisabled()
  })
}
