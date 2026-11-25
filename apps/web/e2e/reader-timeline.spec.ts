import { expect, test } from '@playwright/test'
import { clientAddress } from './client-address'

test('timeline loads on opening without replacing reader text at phone width', async ({ page }, info) => {
  await page.setExtraHTTPHeaders({ 'X-Forwarded-For': clientAddress(info) })
  await page.setViewportSize({ width: 1280, height: 720 })
  await page.route('**/api/v1/me', route => route.fulfill({ json: { id: 1, email: 'test@example.invalid', created_at: '2099-01-01T00:00:00Z', csrf_token: 'token' } }))
  await page.route('**/api/v1/documents/*', route => route.fulfill({ json: { release: '20990101', item: { complete: true, document: { number: '1/2099', title: 'Test law', status: { class: 'in_force', current: true }, text_source: 'none', text_complete: false }, units: [{ kind: 'provision', anchor: 'dieu-1', label: 'Điều 1', text: 'Reader text' }] } } }))
  let calls = 0
  await page.route('**/api/v1/read', route => { calls++; return route.fulfill({ json: { release: '20990101', items: [{ complete: true, units: [{ kind: 'timeline', event: { kind: 'issued', date: '2099-01-01' } }] }] } }) })
  await page.goto('/')
  await expect(page.getByRole('link', { name: /account|tài khoản/i })).toBeVisible()
  await page.setViewportSize({ width: 390, height: 844 })
  await page.evaluate(() => (document.querySelector('#__nuxt') as unknown as { __vue_app__: { config: { globalProperties: { $router: { push: (path: string) => Promise<void> } } } } }).__vue_app__.config.globalProperties.$router.push('/vn/documents/vn:vbpl:1'))
  const panel = page.locator('details').filter({ has: page.getByText('Dòng thời gian', { exact: true }) })
  await expect(panel).toBeVisible()
  expect(calls).toBe(0)
  await panel.locator('summary').click()
  await expect(panel).toContainText('Ngày ban hành')
  expect(calls).toBe(1)
  await expect(page.getByText('Reader text')).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
})

test('timeline error leaves reader text visible', async ({ page }, info) => {
  await page.setExtraHTTPHeaders({ 'X-Forwarded-For': clientAddress(info) })
  await page.route('**/api/v1/me', route => route.fulfill({ json: { id: 1, email: 'test@example.invalid', created_at: '2099-01-01T00:00:00Z', csrf_token: 'token' } }))
  await page.route('**/api/v1/documents/*', route => route.fulfill({ json: { release: '20990101', item: { complete: true, document: { number: '1/2099', title: 'Test law', status: { class: 'in_force', current: true }, text_source: 'none', text_complete: false }, units: [{ kind: 'provision', anchor: 'dieu-1', label: 'Điều 1', text: 'Reader text' }] } } }))
  await page.route('**/api/v1/read', route => route.fulfill({ status: 503, json: { error: { code: 'unavailable', message: 'Unavailable' } } }))
  await page.goto('/')
  await expect(page.getByRole('link', { name: /account|tài khoản/i })).toBeVisible()
  await page.evaluate(() => (document.querySelector('#__nuxt') as unknown as { __vue_app__: { config: { globalProperties: { $router: { push: (path: string) => Promise<void> } } } } }).__vue_app__.config.globalProperties.$router.push('/vn/documents/vn:vbpl:1'))
  await page.getByText('Dòng thời gian', { exact: true }).click()
  await expect(page.getByRole('alert')).toContainText('Chưa tải được dòng thời gian')
  await expect(page.getByText('Reader text')).toBeVisible()
})
