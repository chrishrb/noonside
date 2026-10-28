import { expect, test as setup, type Page } from '@playwright/test';
import { AUTH, AUTH_ADMIN, AUTH_DE } from '../playwright.config';
import { email, emailAdmin, emailDe, password } from './accounts';

async function signIn(page: Page, user: string, path: string) {
  await page.goto('/login');
  await page.getByLabel(/Email|E-Mail/).fill(user);
  await page.getByLabel(/Password|Passwort/).fill(password);
  await page.getByRole('button', { name: /Sign in|Anmelden/ }).click();
  await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
  await expect(page).not.toHaveURL(/\/login/);
  // The Supabase session lives in localStorage, which storageState keeps.
  await page.context().storageState({ path });
}

setup('sign in', ({ page }) => signIn(page, email, AUTH));
setup('sign in (superadmin)', ({ page }) => signIn(page, emailAdmin, AUTH_ADMIN));

setup.describe('German', () => {
  setup.use({ locale: 'de-DE' });
  setup('sign in (de)', ({ page }) => signIn(page, emailDe, AUTH_DE));
});
