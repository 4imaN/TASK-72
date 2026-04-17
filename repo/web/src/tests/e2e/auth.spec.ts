// Auth flow E2E tests — real browser against real stack
import { test, expect } from '@playwright/test'

// Passwords are piped in from run_tests.sh via env vars
const ADMIN_PW     = process.env.ADMIN_PW     || ''
const LEARNER_PW   = process.env.LEARNER_PW   || ''
const FINANCE_PW   = process.env.FINANCE_PW   || ''

test.describe('Login UX', () => {
  test('login page renders required fields', async ({ page }) => {
    await page.goto('/login')
    await expect(page.getByRole('heading', { name: /welcome back/i })).toBeVisible()
    await expect(page.locator('input#username')).toBeVisible()
    await expect(page.locator('input#password')).toBeVisible()
    await expect(page.getByRole('button', { name: /sign in/i })).toBeVisible()
  })

  test('empty submit shows validation error', async ({ page }) => {
    await page.goto('/login')
    await page.getByRole('button', { name: /sign in/i }).click()
    // Zod validation triggers inline errors
    await expect(page.getByText(/required/i).first()).toBeVisible({ timeout: 3000 })
  })

  test('wrong password shows error and stays on login', async ({ page }) => {
    await page.goto('/login')
    await page.locator('input#username').fill('bootstrap_admin')
    await page.locator('input#password').fill('definitely-wrong-password-abc')
    await page.getByRole('button', { name: /sign in/i }).click()
    await expect(page.getByText(/invalid credentials|authentication/i)).toBeVisible({ timeout: 5000 })
    await expect(page).toHaveURL(/\/login/)
  })

  test.skip(!ADMIN_PW, 'admin can log in and reaches protected page')
  test('admin can log in and reaches protected page', async ({ page }) => {
    test.skip(!ADMIN_PW, 'ADMIN_PW not set')
    await page.goto('/login')
    await page.locator('input#username').fill('bootstrap_admin')
    await page.locator('input#password').fill(ADMIN_PW)
    await page.getByRole('button', { name: /sign in/i }).click()
    await page.waitForURL(/^(?!.*\/login).*/, { timeout: 10000 })
    await expect(page).not.toHaveURL(/\/login/)
  })

  test('learner can log in and sees learner-only nav', async ({ page }) => {
    test.skip(!LEARNER_PW, 'LEARNER_PW not set')
    await page.goto('/login')
    await page.locator('input#username').fill('bootstrap_learner')
    await page.locator('input#password').fill(LEARNER_PW)
    await page.getByRole('button', { name: /sign in/i }).click()
    await page.waitForURL(/^(?!.*\/login).*/, { timeout: 10000 })
    // Learner should see My Learning and My Progress
    await expect(page.getByRole('link', { name: /my learning/i })).toBeVisible()
    await expect(page.getByRole('link', { name: /my progress/i })).toBeVisible()
    // And should NOT see admin-only links
    await expect(page.getByRole('link', { name: /^users$/i })).not.toBeVisible()
    await expect(page.getByRole('link', { name: /audit log/i })).not.toBeVisible()
  })

  test('admin does NOT see My Learning / My Progress in sidebar', async ({ page }) => {
    test.skip(!ADMIN_PW, 'ADMIN_PW not set')
    await page.goto('/login')
    await page.locator('input#username').fill('bootstrap_admin')
    await page.locator('input#password').fill(ADMIN_PW)
    await page.getByRole('button', { name: /sign in/i }).click()
    await page.waitForURL(/^(?!.*\/login).*/, { timeout: 10000 })
    await expect(page.getByRole('link', { name: /my learning/i })).not.toBeVisible()
    await expect(page.getByRole('link', { name: /my progress/i })).not.toBeVisible()
  })

  test('logout returns to login', async ({ page }) => {
    test.skip(!ADMIN_PW, 'ADMIN_PW not set')
    await page.goto('/login')
    await page.locator('input#username').fill('bootstrap_admin')
    await page.locator('input#password').fill(ADMIN_PW)
    await page.getByRole('button', { name: /sign in/i }).click()
    await page.waitForURL(/^(?!.*\/login).*/, { timeout: 10000 })
    await page.getByRole('button', { name: /sign out/i }).click()
    await page.waitForURL(/\/login/, { timeout: 5000 })
    await expect(page).toHaveURL(/\/login/)
  })

  test('protected route redirects unauthenticated user to /login', async ({ page }) => {
    await page.goto('/admin/users')
    await expect(page).toHaveURL(/\/login/)
  })

  test('finance user can reach finance page', async ({ page }) => {
    test.skip(!FINANCE_PW, 'FINANCE_PW not set')
    await page.goto('/login')
    await page.locator('input#username').fill('bootstrap_finance')
    await page.locator('input#password').fill(FINANCE_PW)
    await page.getByRole('button', { name: /sign in/i }).click()
    await page.waitForURL(/^(?!.*\/login).*/, { timeout: 10000 })
    await page.getByRole('link', { name: /reconciliation/i }).first().click()
    await expect(page).toHaveURL(/\/finance\/reconciliation/)
  })
})
