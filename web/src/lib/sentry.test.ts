import { describe, expect, it } from 'vitest';
import { scrubRecordingEvent, scrubUrl, tokenPage } from './sentry';

describe('scrubUrl', () => {
  it('removes tokens, query strings and fragments', () => {
    expect(scrubUrl('https://noonside.example.com/p/s0p_secret?kiosk=1')).toBe('https://noonside.example.com/p/[token]');
    expect(scrubUrl('/api/v1/public/s0p_secret/overview?period=day')).toBe('/api/v1/public/[token]/overview');
    expect(scrubUrl('/invitations/abc123')).toBe('/invitations/[token]');
    expect(scrubUrl('/reset-password#access_token=xyz&type=recovery')).toBe('/reset-password');
    expect(scrubUrl('GET /api/v1/systems/42/live')).toBe('GET /api/v1/systems/42/live');
  });
});

describe('scrubRecordingEvent', () => {
  it('scrubs span descriptions and page URLs of replay recordings', () => {
    const span = { data: { tag: 'performanceSpan', payload: { op: 'navigation.navigate', description: 'http://x/p/s0p_secret?kiosk=1' } } };
    expect(scrubRecordingEvent(span).data.payload.description).toBe('http://x/p/[token]');
    expect(scrubRecordingEvent({ data: { href: 'http://x/invitations/abc#t=1' } }).data.href).toBe('http://x/invitations/[token]');
  });
});

describe('tokenPage', () => {
  it('detects pages opened with a secret in the URL', () => {
    expect(tokenPage('/p/s0p_x', '')).toBe(true);
    expect(tokenPage('/invitations/abc', '')).toBe(true);
    expect(tokenPage('/reset-password', '')).toBe(true);
    expect(tokenPage('/', '#access_token=x')).toBe(true);
    expect(tokenPage('/s/1/overview', '')).toBe(false);
    expect(tokenPage('/profile/account', '')).toBe(false);
  });
});
