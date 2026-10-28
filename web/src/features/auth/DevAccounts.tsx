import { useState } from 'react';

import { Avatar, Spinner } from '@/components/ui/primitives';

// Local development only (LoginPage renders it under import.meta.env.DEV, so
// production builds drop it): the accounts from backend/db/seed.sql, one tap to
// sign in. Not translated, like the seed data.
const PASSWORD = 'noonside-dev-pw';
const ACCOUNTS = [
  { email: 'dev@noonside.local', name: 'Dev', role: 'Superadmin' },
  { email: 'demo@noonside.local', name: 'Demo', role: 'Owner' },
  { email: 'demo-de@noonside.local', name: 'Demo DE', role: 'Owner · DE' },
  { email: 'admin@noonside.local', name: 'Ada Admin', role: 'Admin' },
  { email: 'viewer@noonside.local', name: 'Vic Viewer', role: 'Viewer' },
];

export function DevAccounts({ onSignIn }: { onSignIn: (email: string, password: string) => Promise<void> }) {
  const [busy, setBusy] = useState<string | null>(null);
  return (
    <section className="dev-accounts" aria-labelledby="dev-accounts-title">
      <p className="dev-accounts-sep" id="dev-accounts-title">
        <span>Dev accounts · password <code>{PASSWORD}</code></span>
      </p>
      <ul>
        {ACCOUNTS.map((a) => (
          <li key={a.email}>
            <button
              type="button"
              className="dev-account"
              disabled={busy != null}
              onClick={async () => {
                setBusy(a.email);
                try { await onSignIn(a.email, PASSWORD); } finally { setBusy(null); }
              }}
            >
              <Avatar name={a.name} />
              <span className="grid min-w-0 text-start">
                <span className="truncate font-medium">{a.name}</span>
                <span className="sm muted truncate">{a.email}</span>
              </span>
              {busy === a.email
                ? <Spinner />
                : <span className={a.role === 'Superadmin' ? 'badge badge-accent' : 'badge badge-outline'}>{a.role}</span>}
            </button>
          </li>
        ))}
      </ul>
    </section>
  );
}
