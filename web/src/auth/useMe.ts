import { useQuery } from '@tanstack/react-query';
import { getMeOptions } from '@/api/gen/@tanstack/react-query.gen';
import type { Me } from '@/api/types';
import { useAuth } from './AuthProvider';

/** The signed-in user with their memberships and scopes (only fetched with a session). */
export function useMe() {
  const { session } = useAuth();
  return useQuery({ ...getMeOptions(), staleTime: 60_000, enabled: !!session });
}

/** Inside the authenticated shell `me` is always loaded (RequireAuth waits for it). */
export function useMeData(): Me {
  const { data } = useMe();
  if (!data) throw new Error('useMeData before /me loaded');
  return data;
}
