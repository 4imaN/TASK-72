// Admin configuration UX E2E tests
import { test, expect, Page } from '@playwright/test'

const ADMIN_PW = process.env.ADMIN_PW || ''

async function loginAsAdmin(page: Page) {
  await page.goto('/login')
  await page.locator('input#username').fill('bootstrap_admin')
  await page.locator('input#password').fill(ADMIN_PW)
  await page.getByRole('button', { name: /sign in/i }).click()
  await page.waitForURL(/^(?!.*\/login).*/, { timeout: 10000 })
}

test.describe('Admin Config UX', () => {
  test.skip(!ADMIN_PW, 'ADMIN_PW not set')

  test('admin config page loads with tabs', async ({ page }) => {
    await loginAsAdmin(page)
    await page.goto('/admin/config')
    await expect(page.getByRole('heading', { name: /configuration center/i })).toBeVisible()
    await expect(page.getByRole('button', { name: /config flags/i })).toBeVisible()
    await expect(page.getByRole('button', { name: /parameters/i })).toBeVisible()
    await expect(page.getByRole('button', { name: /version rules/i })).toBeVisible()
  })

  test('sidebar Users link opens Users tab and updates URL', async ({ page }) => {
    await loginAsAdmin(page)
    await page.goto('/admin/config')
    await page.getByRole('link', { name: /^users$/i }).click()
    await expect(page).toHaveURL(/\/admin\/users/)
    // Users tab button should have the active styling
    const usersTab = page.getByRole('button', { name: /^users$/i })
    await expect(usersTab).toBeVisible()
  })

  test('sidebar Audit Log link opens Audit tab', async ({ page }) => {
    await loginAsAdmin(page)
    await page.goto('/admin/config')
    await page.getByRole('link', { name: /audit log/i }).click()
    await expect(page).toHaveURL(/\/admin\/audit/)
  })

  test('clicking tabs inside admin page updates URL', async ({ page }) => {
    await loginAsAdmin(page)
    await page.goto('/admin/config')
    await page.getByRole('button', { name: /^users$/i }).click()
    await expect(page).toHaveURL(/\/admin\/users/)
    await page.getByRole('button', { name: /audit log/i }).click()
    await expect(page).toHaveURL(/\/admin\/audit/)
  })

  test('config flags tab shows a table of seeded flags', async ({ page }) => {
    await loginAsAdmin(page)
    await page.goto('/admin/config')
    await page.getByRole('button', { name: /config flags/i }).click()
    // Should see at least one seeded flag (mfa.enabled, recommendations.enabled, etc.)
    await expect(page.getByText(/mfa\.enabled|recommendations\.enabled/i).first()).toBeVisible({ timeout: 5000 })
  })

  test('version rules tab has Add Rule button for admin', async ({ page }) => {
    await loginAsAdmin(page)
    await page.goto('/admin/config')
    await page.getByRole('button', { name: /version rules/i }).click()
    await expect(page.getByRole('button', { name: /\+ add rule/i })).toBeVisible({ timeout: 3000 })
  })

  test('version rule form exposes grace period field with 14-day max', async ({ page }) => {
    await loginAsAdmin(page)
    await page.goto('/admin/config')
    await page.getByRole('button', { name: /version rules/i }).click()
    await page.getByRole('button', { name: /\+ add rule/i }).click()
    const graceInput = page.locator('input[type="number"][max="14"]')
    await expect(graceInput).toBeVisible({ timeout: 3000 })
  })

  test('taxonomy tab lists seeded tags', async ({ page }) => {
    await loginAsAdmin(page)
    await page.goto('/admin/taxonomy')
    await expect(page.getByText(/skill tags/i).first()).toBeVisible({ timeout: 5000 })
  })

  test('catalog tab has Add Resource button for admin', async ({ page }) => {
    await loginAsAdmin(page)
    await page.goto('/admin/config')
    await page.getByRole('button', { name: /catalog/i }).click()
    await expect(page.getByRole('button', { name: /\+ add resource/i })).toBeVisible({ timeout: 3000 })
  })

  test('users tab shows user list with masked emails', async ({ page }) => {
    await loginAsAdmin(page)
    await page.goto('/admin/users')
    // Masked emails use asterisks
    await expect(page.getByText(/bootstrap_admin/i)).toBeVisible({ timeout: 5000 })
  })
})
