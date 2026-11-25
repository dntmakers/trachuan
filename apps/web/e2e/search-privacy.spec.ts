import { expect, test } from '@playwright/test'
import { containsPrivateValue } from './privacy-state'

const account = { id: 1, email: 'test@example.invalid', created_at: '2026-01-01T00:00:00Z', csrf_token: 'token' }

for (const [mobile, label] of [[false, /^(Ban hành từ ngày|Issued from)/], [false, /^(Ban hành đến ngày|Issued to)/], [false, /^(Áp dụng vào ngày|Applies on)/], [true, /^(Ban hành từ ngày|Issued from)/], [true, /^(Ban hành đến ngày|Issued to)/], [true, /^(Áp dụng vào ngày|Applies on)/]] as const) {
  test(`rejects partial date ${label} on ${mobile ? 'mobile' : 'desktop'}`, async ({ page }) => {
    let searches = 0
    await page.route('**/api/v1/me', route => route.fulfill({ json: account }))
    await page.route('**/api/v1/search', route => { searches++; return route.fulfill({ json: {} }) })
    await page.goto('/')
    await expect(page.getByRole('link', { name: /account|tài khoản/i })).toBeVisible()
    if (mobile) await page.setViewportSize({ width: 390, height: 844 })
    await page.evaluate(() => (document.querySelector('#__nuxt') as unknown as { __vue_app__: { config: { globalProperties: { $router: { push: (path: string) => Promise<void> } } } } }).__vue_app__.config.globalProperties.$router.push('/vn/search'))
    await page.locator('#search-query').fill('query')
    if (mobile) await page.getByRole('button', { name: /bộ lọc ·|filters ·/i }).click()
    const field = page.locator(mobile ? 'dialog' : '.search-rail').getByLabel(label)
    await field.focus()
    await page.keyboard.press('1')
    expect(await field.evaluate((input: HTMLInputElement) => input.validity.badInput)).toBe(true)
    if (mobile) await page.getByRole('button', { name: /đóng bộ lọc|close filters/i }).click()
    await page.locator('.search-box').evaluate(form => (form as HTMLFormElement).requestSubmit())
    await expect(page.locator('[role="alert"]:visible').first()).toBeVisible()
    expect(searches).toBe(0)
    if (mobile) await page.getByRole('button', { name: /bộ lọc ·|filters ·/i }).click()
    await field.fill('')
    if (mobile) await page.getByRole('button', { name: /đóng bộ lọc|close filters/i }).click()
    await page.locator('.search-box').evaluate(form => (form as HTMLFormElement).requestSubmit())
    await expect.poll(() => searches).toBe(1)
  })
}

for (const action of ['resize', 'clear'] as const) {
  test(`partial date survives ${action} without hidden invalid state`, async ({ page }) => {
    let searches = 0
    await page.route('**/api/v1/me', route => route.fulfill({ json: account }))
    await page.route('**/api/v1/search', route => { searches++; return route.fulfill({ json: { release: 'r1', hits: [], abstain: false } }) })
    await page.goto('/')
    await expect(page.getByRole('link', { name: /account|tài khoản/i })).toBeVisible()
    await page.evaluate(() => (document.querySelector('#__nuxt') as unknown as { __vue_app__: { config: { globalProperties: { $router: { push: (path: string) => Promise<void> } } } } }).__vue_app__.config.globalProperties.$router.push('/vn/search'))
    await page.locator('#search-query').fill('query')
    if (action === 'clear') {
      await page.locator('.search-box').evaluate(form => (form as HTMLFormElement).requestSubmit())
      await expect.poll(() => searches).toBe(1)
      await expect(page.getByRole('button', { name: /bỏ bộ lọc|clear filters/i })).toBeVisible()
    }
    const field = page.locator('.search-rail').getByLabel(/^(Ban hành từ ngày|Issued from)/)
    await field.focus()
    await page.keyboard.press('1')
    expect(await field.evaluate((input: HTMLInputElement) => input.validity.badInput)).toBe(true)
    if (action === 'clear') {
      await page.getByRole('button', { name: /bỏ bộ lọc|clear filters/i }).click()
      await expect.poll(() => searches).toBe(2)
    } else {
      await page.setViewportSize({ width: 390, height: 844 })
      await page.getByRole('button', { name: /bộ lọc ·|filters ·/i }).click()
      const mobileField = page.locator('dialog').getByLabel(/^(Ban hành từ ngày|Issued from)/)
      expect(await mobileField.evaluate((input: HTMLInputElement) => input.validity.badInput)).toBe(true)
      await mobileField.fill('')
      await page.getByRole('button', { name: /đóng bộ lọc|close filters/i }).click()
      await page.locator('.search-box').evaluate(form => (form as HTMLFormElement).requestSubmit())
      await expect.poll(() => searches).toBe(1)
    }
  })
}

for (const query of ['private-query-for-search', 'private-query-for-search\nsecond-line']) {
  test(`search sends ${query.includes('\n') ? 'multiline' : 'single-line'} private values only in POST bodies`, async ({ page }) => {
    const requests: { method: string, body: unknown }[] = []
    const urls: string[] = []
    page.on('request', request => urls.push(request.url()))
    await page.route('**/api/v1/me', route => route.fulfill({ json: account }))
    await page.route('**/api/v1/search', route => {
      requests.push({ method: route.request().method(), body: route.request().postDataJSON() })
      return route.fulfill({ json: { release: 'r1', next_cursor: requests.length === 1 ? 'cursor-1' : '', abstain: false, hits: [{ handle: 'vn:1#dieu-1', doc: 'vn:1', citation: 'Citation', text: 'Law text', complete: true }], documents: { 'vn:1': { status: { class: 'in_force', current: true }, text_source: 'source_tree', text_complete: true } } } })
    })
    await page.goto('/')
    await expect(page.getByRole('link', { name: /account|tài khoản/i })).toBeVisible()
    await page.evaluate(() => (document.querySelector('#__nuxt') as unknown as { __vue_app__: { config: { globalProperties: { $router: { push: (path: string) => Promise<void> } } } } }).__vue_app__.config.globalProperties.$router.push('/vn/search'))
    const input = page.locator('#search-query')
    await expect(input).toBeVisible()
    const before = await page.evaluate(() => history.length)
    await input.fill('   ')
    await page.locator('form').evaluate(form => (form as HTMLFormElement).requestSubmit())
    expect(requests).toHaveLength(0)
    await input.fill(query)
    await page.locator('.search-rail').getByLabel(/^(Áp dụng vào ngày|Applies on)/).fill('2026-01-02')
    await page.locator('.search-rail').getByLabel(/^(Ban hành từ ngày|Issued from)/).fill('2024-02-03')
    await page.locator('.search-rail').getByLabel(/^(Ban hành đến ngày|Issued to)/).fill('2025-04-05')
    await page.locator('input[type=checkbox]').uncheck()
    await page.locator('form').evaluate(form => (form as HTMLFormElement).requestSubmit())
    await expect(page.getByText('Citation')).toBeVisible()
    await page.getByRole('button', { name: /show more|xem thêm/i }).click()
    await expect.poll(() => requests.length).toBe(2)
    expect(requests).toEqual([
      { method: 'POST', body: { country: 'vn', query, detail: 'full', limit: 20, as_of: '2026-01-02', issued_from: '2024-02-03', issued_to: '2025-04-05', in_force_only: false } },
      { method: 'POST', body: { country: 'vn', query, detail: 'full', limit: 20, as_of: '2026-01-02', issued_from: '2024-02-03', issued_to: '2025-04-05', in_force_only: false, release: 'r1', cursor: 'cursor-1' } },
    ])
    await expect(page.getByRole('button', { name: /show more|xem thêm/i })).toHaveCount(0)
    expect(containsPrivateValue(urls, query)).toBe(false)
    for (const date of ['2026-01-02', '2024-02-03', '2025-04-05']) expect(containsPrivateValue(urls, date)).toBe(false)
    const browserState = await page.evaluate(() => ({ url: location.href, length: history.length, state: history.state, cookies: document.cookie, local: { ...localStorage }, session: { ...sessionStorage }, title: document.title }))
    const state = { browserState, databases: await page.evaluate(() => indexedDB.databases()), payload: await page.locator('#__NUXT_DATA__').textContent(), cookies: await page.context().cookies() }
    expect(containsPrivateValue(state, query)).toBe(false)
    expect(JSON.stringify(state)).not.toContain(JSON.stringify(query).slice(1, -1))
    for (const date of ['2026-01-02', '2024-02-03', '2025-04-05']) expect(containsPrivateValue(browserState, date)).toBe(false)
    expect(await page.evaluate(() => history.length)).toBe(before)
    const response = await page.reload()
    expect(containsPrivateValue(await response!.text(), query)).toBe(false)
    expect(await response!.text()).not.toContain('Law text')
  })
}
