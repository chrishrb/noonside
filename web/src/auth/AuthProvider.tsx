import { createContext, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import type { AuthChangeEvent, Session } from '@supabase/supabase-js';
import { useQueryClient } from '@tanstack/react-query';
import { persister } from '@/api/persist';
import { queryId } from '@/api/queries';
import i18n from '@/i18n';
import { setSentryUser } from '@/lib/sentry';
import { supabase } from './supabase';

interface AuthState {
  session: Session | null;
  /** False until the stored session has been read. */
  ready: boolean;
  /** Set after a password recovery link was opened. */
  recovery: boolean;
  signIn(email: string, password: string): Promise<void>;
  signOut(): Promise<void>;
  resetPassword(email: string): Promise<void>;
  updatePassword(password: string): Promise<void>;
}

const AuthContext = createContext<AuthState | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const qc = useQueryClient();
  const [session, setSession] = useState<Session | null>(null);
  const [ready, setReady] = useState(false);
  const [recovery, setRecovery] = useState(false);
  // Signed-in user the cache belongs to; undefined until the first auth event.
  const userId = useRef<string | null>(undefined);

  useEffect(() => {
    supabase.auth.getSession().then(({ data }) => {
      setSession(data.session);
      setReady(true);
    });
    const { data } = supabase.auth.onAuthStateChange((event: AuthChangeEvent, s) => {
      setSession(s);
      if (event === 'PASSWORD_RECOVERY') setRecovery(true);
      // Another user may sign in next: drop everything cached for this one.
      // supabase-js also emits SIGNED_IN for the same user whenever the
      // browser tab becomes visible again, so compare the user: clearing the
      // cache then would remount the app and lose open dialogs and forms.
      // The cache restored from IndexedDB may belong to a user whose session
      // is gone, so without a session it is dropped on the first event too.
      const uid = s?.user.id ?? null;
      if (uid !== userId.current) {
        setSentryUser(uid);
        if (userId.current !== undefined || uid === null) {
          qc.removeQueries({ predicate: (q) => queryId(q) !== 'getInvitation' });
          void persister.removeClient();
        }
        userId.current = uid;
      }
    });
    return () => data.subscription.unsubscribe();
  }, [qc]);

  const value = useMemo<AuthState>(() => ({
    session, ready, recovery,
    async signIn(email, password) {
      const { error } = await supabase.auth.signInWithPassword({ email, password });
      if (error) throw new Error(error.code === 'invalid_credentials' ? i18n.t('auth.wrongCredentials') : error.message);
    },
    async signOut() {
      // While the token is still valid: this device should stop getting pushes.
      await import('@/lib/push').then((m) => m.unsubscribePush()).catch(() => {});
      await supabase.auth.signOut();
    },
    async resetPassword(email) {
      const { error } = await supabase.auth.resetPasswordForEmail(email, { redirectTo: `${window.location.origin}/reset-password` });
      if (error) throw error;
    },
    async updatePassword(password) {
      const { error } = await supabase.auth.updateUser({ password });
      if (error) throw error;
      setRecovery(false);
    },
  }), [session, ready, recovery]);

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthState {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error('useAuth outside AuthProvider');
  return ctx;
}
