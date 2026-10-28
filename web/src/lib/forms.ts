import type { FieldValues, Path, UseFormSetError } from 'react-hook-form';
import { ApiError } from '@/api/client';
import i18n from '@/i18n';
import { cap } from './format';

export interface ServerError { title: string; message: string }

export function serverErrorOf(err: unknown): ServerError {
  const status = err instanceof ApiError ? err.status : 0;
  const message = err instanceof Error && err.message ? `${cap(err.message)}.` : i18n.t('common.somethingWrongDot');
  const title = status === 409 ? i18n.t('errors.conflict') : status === 403 ? i18n.t('errors.notAllowed') : i18n.t('errors.couldNotSave');
  return { title, message: message.replace(/\.\.$/, '.') };
}

/**
 * Maps a 400/409 message like "email must be a valid address" onto the field
 * whose key it starts with. Returns the error to show in a banner otherwise.
 */
export function applyServerError<T extends FieldValues>(
  err: unknown,
  setError: UseFormSetError<T>,
  map: Record<string, Path<T>> = {},
): ServerError | null {
  const msg = err instanceof Error ? err.message : '';
  const key = Object.keys(map).find((k) => msg.startsWith(k));
  if (key && err instanceof ApiError && (err.status === 400 || err.status === 409)) {
    setError(map[key], { type: 'server', message: `${cap(msg)}.` }, { shouldFocus: true });
    return null;
  }
  return serverErrorOf(err);
}

/** Empty strings from number inputs become undefined. */
export const numOrUndef = (v: unknown) => (v === '' || v == null || Number.isNaN(v) ? undefined : Number(v));
