import { createAsyncStoragePersister } from '@tanstack/query-async-storage-persister';
import type { Query } from '@tanstack/react-query';
import { del, get, set } from 'idb-keyval';
import { queryId } from './queries';

// The query cache survives reloads and offline starts in IndexedDB
// (minute curves outgrow localStorage). It holds the signed-in user's data,
// so AuthProvider drops it whenever the user changes or signs out.
export const persister = createAsyncStoragePersister({
  key: 'noonside.query-cache',
  storage: { getItem: get, setItem: set, removeItem: del },
  throttleTime: 2000,
});

// The cache under the app's former name; it is never read again.
del('solar0.query-cache').catch(() => {});

export const PERSIST_MAX_AGE = 7 * 24 * 60 * 60 * 1000;

export const persistOptions = {
  persister,
  maxAge: PERSIST_MAX_AGE,
  buster: __APP_VERSION__,
  dehydrateOptions: {
    shouldDehydrateQuery: (q: Query) =>
      q.state.status === 'success' && !!queryId(q) && queryId(q) !== 'getInvitation',
  },
};
