import { supabase } from '@/auth/supabase';
import i18n from '@/i18n';
import { API_URL } from './config';
import { client } from './gen/client.gen';

export { API_URL };

/** An error response from the API (`{message}` with an HTTP status). */
export class ApiError extends Error {
  readonly status: number;
  constructor(status: number, message: string) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
  }
}

async function accessToken(): Promise<string | undefined> {
  const { data } = await supabase.auth.getSession();
  return data.session?.access_token;
}

// The generated SDK (./gen/sdk.gen) asks for a token for each security scheme
// of an operation; the app only sends the Supabase access token (bearer).
client.setConfig({ auth: (a) => (a.scheme === 'bearer' ? accessToken() : undefined) });

// Error responses and network failures become ApiError, so every call either
// returns data or throws. The client has already parsed a JSON body.
client.interceptors.error.use((error, response) => {
  if (error instanceof ApiError) return error;
  if (!response) return new ApiError(0, i18n.t('errors.noResponse'));
  const body = error as { message?: unknown } | null;
  const message = body && typeof body.message === 'string' ? body.message : response.statusText || 'request failed';
  return new ApiError(response.status, message);
});

/** Downloads text from an endpoint that doesn't return JSON (CSV export). */
export async function fetchText(path: string, query: Record<string, string>): Promise<{ text: string; filename: string | null }> {
  const token = await accessToken();
  const url = `${API_URL}${path}?${new URLSearchParams(query)}`;
  const resp = await fetch(url, { headers: token ? { Authorization: `Bearer ${token}` } : {} }).catch(() => {
    throw new ApiError(0, i18n.t('errors.noResponse'));
  });
  if (!resp.ok) {
    let message = resp.statusText;
    try { message = (await resp.json()).message ?? message; } catch { /* not JSON */ }
    throw new ApiError(resp.status, message);
  }
  const cd = resp.headers.get('Content-Disposition');
  const m = cd && /filename="?([^";]+)"?/.exec(cd);
  return { text: await resp.text(), filename: m ? m[1] : null };
}
