import { expect, test } from '@playwright/test'
import { clientAddress } from './client-address'

test('edits restored text and submits a new first page', async ({ page }, info) => {
  await page.setExtraHTTPHeaders({ 'X-Forwarded-For': clientAddress(info) })
  await page.route('**/api/v1/me', route => route.fulfill({ json: { id: 1, email: 'test@example.invalid', csrf_token: 'fixture' } }))
  const bodies: Record<string, unknown>[] = []
  await page.route('**/api/v1/search', route => {
    bodies.push(route.request().postDataJSON())
    return route.fulfill({ json: { release: '20990101', hits: [], abstain: true, ...(bodies.length === 1 ? { restored_query: 'thuế thu nhập', next_cursor: 'cursor-a' } : {}) } })
  })
  await page.goto('/')
  await expect(page.getByRole('link', { name: 'Tài khoản' })).toBeVisible()
  await page.evaluate(() => (document.querySelector('#__nuxt') as unknown as { __vue_app__: { config: { globalProperties: { $router: { push: (path: string) => Promise<void> } } } } }).__vue_app__.config.globalProperties.$router.push('/vn/search'))
  const input = page.locator('#search-query')
  await input.fill('thue thu nhap')
  await page.locator('.search-box').evaluate(form => (form as HTMLFormElement).requestSubmit())
  await expect(page.getByText('Đã tìm theo: thuế thu nhập')).toBeVisible()
  await expect(input).toHaveValue('thue thu nhap')
  await page.getByRole('button', { name: 'Sửa câu tìm kiếm' }).click()
  await expect(input).toHaveValue('thuế thu nhập')
  await expect(input).toBeFocused()
  await input.fill('thuế thu nhập cá nhân')
  await page.locator('.search-box').evaluate(form => (form as HTMLFormElement).requestSubmit())
  await expect.poll(() => bodies.length).toBe(2)
  expect(bodies[1]).toMatchObject({ query: 'thuế thu nhập cá nhân' })
  expect(bodies[1]).not.toHaveProperty('cursor')
})
