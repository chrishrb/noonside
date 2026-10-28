import { expect, test } from '@playwright/test';
import { email, password } from './accounts';

// Signs in itself instead of reusing the stored session.
test.use({ storageState: { cookies: [], origins: [] } });

test('sign in, dashboard and yield', async ({ page }) => {
  await page.goto('/');
  await expect(page).toHaveURL(/\/login$/);
  await page.getByLabel('Email', { exact: true }).fill(email);
  await page.getByLabel('Password', { exact: true }).fill(password);
  await page.getByRole('button', { name: 'Sign in' }).click();

  await expect(page.getByRole('heading', { level: 1 })).toContainText(/Good (morning|afternoon|evening)/);
  const card = page.locator('article.sys-card').filter({ hasNot: page.getByText('No inverters') }).first();
  await card.click();

  await expect(page.getByText('Yield', { exact: true }).first()).toBeVisible();
  await expect(page.getByRole('img', { name: /chart/ })).toBeVisible();
  await page.getByRole('navigation', { name: 'Period' }).getByRole('link', { name: 'Month' }).click();
  await expect(page).toHaveURL(/period=month/);
  await expect(page.getByRole('heading', { name: 'Energy per day' })).toBeVisible();

  // Sign out lives in the account menu.
  await page.locator('.user-chip:visible, .topbar button:has(.avatar):visible').first().click();
  await page.getByRole('button', { name: 'Sign out' }).click();
  await expect(page).toHaveURL(/\/login$/);
});
