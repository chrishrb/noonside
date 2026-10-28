import { z } from 'zod';

/** New password + repeat, matching Supabase's minimum length (config.toml). */
export const newPasswordFields = {
  password: z.string().min(10, 'validation.passwordMin').max(200, 'validation.passwordMax'),
  password2: z.string().min(1, 'validation.passwordRepeat'),
};

/** Use as `schema.refine(...passwordsMatch)`. Messages are i18n keys, translated by `Field`. */
export const passwordsMatch: [(v: { password: string; password2: string }) => boolean, { path: string[]; message: string }] = [
  (v) => v.password === v.password2,
  { path: ['password2'], message: 'validation.passwordMismatch' },
];
