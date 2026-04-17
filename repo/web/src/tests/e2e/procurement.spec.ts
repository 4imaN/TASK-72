// Procurement / Approvals UX E2E tests
import { test, expect, Page } from '@playwright/test'

const PROCUREMENT_PW = process.env.PROCUREMENT_PW || ''
const APPROVER_PW    = process.env.APPROVER_PW    || ''

async function loginAs(page: Page, username: string, pw: string) {
  await page.goto('/login')
  await page.locator('input#username').fill(username)
  await page.locator('input#password').fill(pw)
  await page.getByRole('button', { name: /sign in/i }).click()
  await page.waitForURL(/^(?!.*\/login).*/, { timeout: 10000 })
}

test.describe('Procurement UX', () => {
  test('procurement user can access orders page', async ({ page }) => {
    test.skip(!PROCUREMENT_PW, 'PROCUREMENT_PW not set')
    await loginAs(page, 'bootstrap_procurement', PROCUREMENT_PW)
    await page.goto('/procurement/orders')
    await expect(page).toHaveURL(/\/procurement\/orders/)
  })

  test('procurement sees Disputes link', async ({ page }) => {
    test.skip(!PROCUREMENT_PW, 'PROCUREMENT_PW not set')
    await loginAs(page, 'bootstrap_procurement', PROCUREMENT_PW)
    await expect(page.getByRole('link', { name: /disputes/i })).toBeVisible()
  })

  test('approver sees Approvals link', async ({ page }) => {
    test.skip(!APPROVER_PW, 'APPROVER_PW not set')
    await loginAs(page, 'bootstrap_approver', APPROVER_PW)
    await expect(page.getByRole('link', { name: /approvals/i })).toBeVisible()
  })

  test('approver does NOT see Orders create button (no orders:write)', async ({ page }) => {
    test.skip(!APPROVER_PW, 'APPROVER_PW not set')
    await loginAs(page, 'bootstrap_approver', APPROVER_PW)
    await page.goto('/procurement/orders')
    // Approver has orders:read + orders:approve but NOT orders:write (cannot create)
    // Create button / new order button should not be visible
    const newBtns = await page.getByRole('button', { name: /new order|create order/i }).count()
    expect(newBtns).toBe(0)
  })
})
