import { test, expect } from '@playwright/test';

test('login page loads', async ({ page }) => {
  await page.goto('/login');
  await expect(page.getByRole('heading', { name: /welcome back/i })).toBeVisible();
});

test('unauthenticated access redirects to login', async ({ page }) => {
  await page.goto('/library');
  await expect(page).toHaveURL(/\/login/);
});
