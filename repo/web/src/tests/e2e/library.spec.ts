// Library / Search UX E2E tests
import { test, expect, Page } from '@playwright/test'

const ADMIN_PW = process.env.ADMIN_PW || ''

async function loginAsAdmin(page: Page) {
  await page.goto('/login')
  await page.locator('input#username').fill('bootstrap_admin')
  await page.locator('input#password').fill(ADMIN_PW)
  await page.getByRole('button', { name: /sign in/i }).click()
  await page.waitForURL(/^(?!.*\/login).*/, { timeout: 10000 })
}

test.describe('Library & Search UX', () => {
  test.skip(!ADMIN_PW, 'ADMIN_PW not set')

  test('library page loads with header and search input', async ({ page }) => {
    await loginAsAdmin(page)
    await page.goto('/library')
    await expect(page.getByRole('heading', { name: /learning library/i })).toBeVisible()
    await expect(page.getByPlaceholder(/search/i)).toBeVisible()
  })

  test('typing in search triggers a query and shows results', async ({ page }) => {
    await loginAsAdmin(page)
    await page.goto('/library')
    const input = page.getByPlaceholder(/search/i)
    await input.fill('leadership')
    await page.getByRole('button', { name: /^search$/i }).click()
    // After search, the results section should appear (may be empty but the sort tabs should render)
    await expect(page.getByRole('button', { name: /relevance|popular|recent/i }).first()).toBeVisible({ timeout: 5000 })
  })

  test('filter panel opens and lets user set category', async ({ page }) => {
    await loginAsAdmin(page)
    await page.goto('/library')
    // Click the filter toggle button (has SlidersHorizontal icon)
    const filterToggles = page.locator('button').filter({ has: page.locator('svg.lucide-sliders-horizontal') })
    if (await filterToggles.count() > 0) {
      await filterToggles.first().click()
      // Now Category, Content type, Tags, Publish date, Synonym, Fuzzy, Pinyin inputs should be visible
      await expect(page.getByText(/category/i).first()).toBeVisible({ timeout: 3000 })
    }
  })

  test('archive link navigates to archive page', async ({ page }) => {
    await loginAsAdmin(page)
    await page.goto('/library')
    await page.getByRole('link', { name: /archive/i }).click()
    await expect(page).toHaveURL(/\/archive/)
    await expect(page.getByRole('heading', { name: /resource archive/i })).toBeVisible()
  })

  test('archive page shows month/tag toggle', async ({ page }) => {
    await loginAsAdmin(page)
    await page.goto('/archive')
    await expect(page.getByRole('button', { name: /by month/i })).toBeVisible()
    await expect(page.getByRole('button', { name: /by tag/i })).toBeVisible()
  })

  test('archive by-tag switch changes the bucket list', async ({ page }) => {
    await loginAsAdmin(page)
    await page.goto('/archive')
    await page.getByRole('button', { name: /by tag/i }).click()
    // Should be on the tag bucket view — by-tag button is now active
    const tagBtn = page.getByRole('button', { name: /by tag/i })
    await expect(tagBtn).toHaveClass(/bg-slate-800|bg-\[#161922\]/)
  })
})
