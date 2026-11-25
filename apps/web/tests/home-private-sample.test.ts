import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

describe('home sample boundary', () => {
  it('does not import the stored multi-hit sample into the page', () => {
    const page = readFileSync(new URL('../app/pages/index.vue', import.meta.url), 'utf8')
    expect(page).not.toContain("import rawSample from '../data/sample-result.json'")
  })
})
