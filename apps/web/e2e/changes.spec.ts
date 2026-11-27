import { expect, test, type Page } from '@playwright/test'
import { clientAddress } from './client-address'

const account = { id: 1, email: 'test@example.invalid', created_at: '2099-01-01T00:00:00Z', csrf_token: 'csrf' }
const counts = { document_added: 0, document_removed: 0, document_validity: 2, provision_text: 1, document_key: 1 }
const comparison = { country: 'vn', release: '20990101', previous_release: '20980101', state: 'available', counts }
const rows = [
  { kind: 'document_validity', key: 'vn:vbpl:2', handle: 'vn:vbpl:2', eff_from: { before: null, after: '2099-01-01' }, source_change: { before_key: 'vn:vbpl:1' } },
  { kind: 'document_validity', key: 'vn:vbpl:3', status_class: { before: 'unknown', after: 'in_force' }, source_change: { text_source: { before: 'ocr', after: 'pdf_text' } } },
]
async function signedIn(page: Page) {
  await page.context().addCookies([{ name: 'account-fixture', value: '1', url: 'http://127.0.0.1:3031' }])
  await page.route('**/api/v1/me', route => route.fulfill({ json: account }))
}

test.beforeEach(async ({ page }, info) => {
  await page.setExtraHTTPHeaders({ 'X-Forwarded-For': clientAddress(info) })
})

test('sources links to the page and signing in returns without fetching changes while signed out', async ({ page }) => {
  const requests: string[] = []
  page.on('request', request => { if (new URL(request.url()).pathname === '/api/v1/changes') requests.push(request.url()) })
  await page.route('**/api/v1/me', route => route.fulfill({ status: 204 }))
  await page.route('**/api/v1/changes', route => route.fulfill({ json: { ...comparison, items: rows } }))
  await page.goto('/sources')
  await page.waitForFunction(() => Boolean((document.querySelector('#__nuxt') as unknown as { __vue_app__?: unknown } | null)?.__vue_app__))
  const link = page.getByRole('link', { name: 'Thay đổi trong kho văn bản' })
  await expect(link).toHaveAttribute('href', '/vn/changes')
  await link.click()
  await expect(page).toHaveURL(/\/login\?/)
  await expect.poll(() => new URL(page.url()).searchParams.get('next')).toBe('/vn/changes')
  await expect(page.getByRole('heading', { name: 'Đăng nhập để tra cứu' })).toBeVisible()
  await page.waitForFunction(() => Boolean((document.querySelector('#__nuxt') as unknown as { __vue_app__?: unknown } | null)?.__vue_app__))
  expect(requests).toEqual([])
  await page.unroute('**/api/v1/me')
  await page.route('**/api/v1/me', route => route.fulfill({ json: account }))
  await page.route('**/api/v1/auth/password', async route => {
    expect(route.request().postDataJSON().next).toBe('/vn/changes')
    await page.context().addCookies([{ name: 'account-fixture', value: '1', url: 'http://127.0.0.1:3031' }])
    await route.fulfill({ json: { next: '/vn/changes' } })
  })
  await page.getByLabel('Email', { exact: true }).fill('test@example.invalid')
  await page.getByLabel('Mật khẩu', { exact: true }).fill('synthetic password')
  await page.getByRole('button', { name: 'Đăng nhập', exact: true }).click()
  await expect(page).toHaveURL(/\/vn\/changes$/)
  await expect.poll(() => requests.length).toBe(1)
  await expect(page.locator('article').first()).toContainText('vn:vbpl:2')
})

test('shows source context with validity, separate text view and pinned links', async ({ page }) => {
  await signedIn(page)
  const requests: Record<string, unknown>[] = []
  await page.route('**/api/v1/changes', route => {
    const body = route.request().postDataJSON()
    requests.push(body)
    const text = body.kinds?.includes('provision_text')
    return route.fulfill({ json: { ...comparison, items: text ? [
      { kind: 'provision_text', key: 'vn:vbpl:4', anchor: 'dieu-1', change: 'changed', provenance: true, ambiguous: false, handle: 'vn:vbpl:4#dieu-1' },
      { kind: 'provision_text', key: 'vn:vbpl:5', anchor: 'dieu-2', change: 'removed', provenance: false, ambiguous: false },
    ] : rows } })
  })
  await page.goto('/vn/changes')
  const cards = page.locator('article')
  await expect(cards).toHaveCount(2)
  await expect(cards.nth(0)).toContainText('Hiệu lực ghi nhận trong kho văn bản')
  await expect(cards.nth(0)).toContainText('vn:vbpl:1')
  await expect(cards.nth(0)).toContainText('2099-01-01')
  await expect(cards.nth(1)).toContainText('Hiệu lực ghi nhận trong kho văn bản')
  await expect(cards.nth(1)).toContainText('Thay đổi nguồn')
  await expect(cards.nth(1)).toContainText('OCR')
  await expect(cards.nth(1)).toContainText('Còn hiệu lực')
  await expect(cards.nth(0).getByRole('link')).toHaveAttribute('href', '/vn/documents/vn:vbpl:2?release=20990101')
  await expect(cards.nth(1).getByRole('link')).toHaveCount(0)
  expect(requests[0]).toEqual({ country: 'vn' })
  await page.getByLabel('Thay đổi nội dung').check()
  await page.getByRole('button', { name: 'Áp dụng bộ lọc' }).click()
  await expect(cards.nth(0)).toContainText('vn:vbpl:4')
  expect(requests[1]).toEqual({ country: 'vn', kinds: ['provision_text'] })
  await expect(cards.nth(0).getByRole('link')).toHaveAttribute('href', '/vn/documents/vn:vbpl:4?release=20990101#dieu-1')
  await expect(cards.nth(1).getByRole('link')).toHaveCount(0)
})

test('clears old pages on release expiration and loads the active comparison', async ({ page }) => {
  await signedIn(page)
  const requests: Record<string, unknown>[] = []
  await page.route('**/api/v1/changes', route => {
    const body = route.request().postDataJSON()
    requests.push(body)
    if (body.cursor) return route.fulfill({ status: 409, json: { error: { code: 'release_expired', message: 'expired' } } })
    return route.fulfill({ json: requests.length < 3 ? { ...comparison, items: [rows[0]], next_cursor: 'cursor-a' } : { ...comparison, release: '20990102', items: [rows[1]] } })
  })
  await page.goto('/vn/changes')
  await expect(page.getByText('vn:vbpl:2')).toBeVisible()
  await page.getByLabel('Mã văn bản chính xác').fill('vn:vbpl:2')
  await page.getByRole('button', { name: 'Áp dụng bộ lọc' }).click()
  await expect.poll(() => requests.length).toBe(2)
  await page.getByLabel('Mã văn bản chính xác').fill('vn:vbpl:3')
  await page.getByRole('button', { name: 'Xem thêm' }).click()
  await expect(page.getByRole('alert')).toContainText('Bản so sánh này đã hết hạn')
  await expect(page.getByRole('button', { name: 'Tải bản so sánh hiện tại' })).toBeFocused()
  await expect(page.getByText('vn:vbpl:2')).toHaveCount(0)
  expect(requests[2]).toEqual({ country: 'vn', document: 'vn:vbpl:2', cursor: 'cursor-a', release: '20990101' })
  await page.getByRole('button', { name: 'Tải bản so sánh hiện tại' }).click()
  await expect(page.getByText('vn:vbpl:3')).toBeVisible()
  await expect(page.getByText('20990102')).toBeVisible()
  expect(requests[3]).toEqual({ country: 'vn', document: 'vn:vbpl:3' })
})

for (const [state, message] of [['baseline', 'Bản trước không có bản tóm tắt để so sánh.'], ['unavailable', 'Bản phát hành này chưa có dữ liệu so sánh.']] as const) {
  test(`shows the ${state} state`, async ({ page }) => {
    await signedIn(page)
    await page.route('**/api/v1/changes', route => route.fulfill({ json: { country: 'vn', release: '20990101', state, items: [] } }))
    await page.goto('/vn/changes')
    await expect(page.getByText(message)).toBeVisible()
  })
}

test('distinguishes zero served counts from an empty filter', async ({ page }) => {
  await signedIn(page)
  const zero = Object.fromEntries(Object.keys(counts).map(key => [key, 0]))
  await page.route('**/api/v1/changes', route => route.fulfill({ json: { ...comparison, counts: route.request().postDataJSON().document ? counts : zero, items: [] } }))
  await page.goto('/vn/changes')
  await expect(page.getByText('Không có thay đổi thuộc các loại được hiển thị')).toBeVisible()
  await page.getByLabel('Mã văn bản chính xác').fill('vn:vbpl:99')
  await page.getByRole('button', { name: 'Áp dụng bộ lọc' }).click()
  await expect(page.getByText('Không có thay đổi khớp bộ lọc.')).toBeVisible()
  await expect(page).toHaveURL(/\/vn\/changes$/)
})
