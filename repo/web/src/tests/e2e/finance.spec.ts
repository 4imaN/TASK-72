// Finance / Settlement UX E2E tests
import { test, expect, Page } from '@playwright/test'

const FINANCE_PW  = process.env.FINANCE_PW  || ''
const APPROVER_PW = process.env.APPROVER_PW || ''

async function loginAs(page: Page, username: string, pw: string) {
  await page.goto('/login')
  await page.locator('input#username').fill(username)
  await page.locator('input#password').fill(pw)
  await page.getByRole('button', { name: /sign in/i }).click()
  await page.waitForURL(/^(?!.*\/login).*/, { timeout: 10000 })
}

test.describe('Finance Settlement UX', () => {
  test('finance user can access reconciliation page', async ({ page }) => {
    test.skip(!FINANCE_PW, 'FINANCE_PW not set')
    await loginAs(page, 'bootstrap_finance', FINANCE_PW)
    await page.goto('/finance/reconciliation')
    await expect(page.getByRole('heading', { name: /finance.*reconciliation|finance/i })).toBeVisible()
  })

  test('reconciliation page shows tab bar', async ({ page }) => {
    test.skip(!FINANCE_PW, 'FINANCE_PW not set')
    await loginAs(page, 'bootstrap_finance', FINANCE_PW)
    await page.goto('/finance/reconciliation')
    await expect(page.getByRole('button', { name: /statement imports/i })).toBeVisible()
    await expect(page.getByRole('button', { name: /reconciliation runs/i })).toBeVisible()
    await expect(page.getByRole('button', { name: /settlement batches/i })).toBeVisible()
  })

  test('sidebar Reconciliation link opens Runs tab', async ({ page }) => {
    test.skip(!FINANCE_PW, 'FINANCE_PW not set')
    await loginAs(page, 'bootstrap_finance', FINANCE_PW)
    await page.getByRole('link', { name: /reconciliation/i }).first().click()
    await expect(page).toHaveURL(/\/finance\/reconciliation/)
  })

  test('sidebar Settlements link opens Batches tab', async ({ page }) => {
    test.skip(!FINANCE_PW, 'FINANCE_PW not set')
    await loginAs(page, 'bootstrap_finance', FINANCE_PW)
    await page.getByRole('link', { name: /settlements/i }).click()
    await expect(page).toHaveURL(/\/finance\/settlements/)
  })

  test('switching tabs inside finance updates URL', async ({ page }) => {
    test.skip(!FINANCE_PW, 'FINANCE_PW not set')
    await loginAs(page, 'bootstrap_finance', FINANCE_PW)
    await page.goto('/finance/reconciliation')
    await page.getByRole('button', { name: /settlement batches/i }).click()
    await expect(page).toHaveURL(/\/finance\/settlements/)
  })

  test('finance user sees New Run button on runs tab', async ({ page }) => {
    test.skip(!FINANCE_PW, 'FINANCE_PW not set')
    await loginAs(page, 'bootstrap_finance', FINANCE_PW)
    await page.goto('/finance/reconciliation')
    await page.getByRole('button', { name: /reconciliation runs/i }).click()
    await expect(page.getByRole('button', { name: /new run/i })).toBeVisible({ timeout: 3000 })
  })

  test('approver (read-only) does NOT see New Run button', async ({ page }) => {
    test.skip(!APPROVER_PW, 'APPROVER_PW not set')
    await loginAs(page, 'bootstrap_approver', APPROVER_PW)
    await page.goto('/finance/reconciliation')
    await page.getByRole('button', { name: /reconciliation runs/i }).click()
    // Approver has reconciliation:read but NOT :write
    await expect(page.getByRole('button', { name: /new run/i })).not.toBeVisible({ timeout: 3000 })
  })

  test('statement imports tab has Import Statements button for finance', async ({ page }) => {
    test.skip(!FINANCE_PW, 'FINANCE_PW not set')
    await loginAs(page, 'bootstrap_finance', FINANCE_PW)
    await page.goto('/finance/reconciliation')
    await page.getByRole('button', { name: /statement imports/i }).click()
    await expect(page.getByRole('button', { name: /import statements/i })).toBeVisible({ timeout: 3000 })
  })

  test('settlement batches tab has Create Batch button for finance', async ({ page }) => {
    test.skip(!FINANCE_PW, 'FINANCE_PW not set')
    await loginAs(page, 'bootstrap_finance', FINANCE_PW)
    await page.goto('/finance/settlements')
    await expect(page.getByRole('button', { name: /create batch/i })).toBeVisible({ timeout: 3000 })
  })
})
