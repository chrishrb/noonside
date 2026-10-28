import { useQuery, useQueryClient, type Query } from '@tanstack/react-query';
import { useMemo } from 'react';
import type * as gen from './gen/@tanstack/react-query.gen';
import { getSystemOptions, listInvertersOptions, listSystemsOptions } from './gen/@tanstack/react-query.gen';

/** The operationId of a GET operation, e.g. 'listInverters'. */
export type QueryId = {
  [K in keyof typeof gen]: K extends `${infer Id}QueryKey` ? (Id extends `${string}Infinite` ? never : Id) : never
}[keyof typeof gen];

// Generated query keys are [{ _id: operationId, path, query, … }], with
// `_infinite: true` for infinite queries.
export const queryId = (q: Pick<Query, 'queryKey'>) => (q.queryKey[0] as { _id?: string } | undefined)?._id;

/** Invalidates every variant (any params, infinite or not) of the given GET operations. */
export function useInvalidate() {
  const qc = useQueryClient();
  return (...ids: QueryId[]) => qc.invalidateQueries({ predicate: (q) => ids.includes(queryId(q) as QueryId) });
}

export function useSystems() {
  const q = useQuery({ ...listSystemsOptions(), staleTime: 60_000 });
  const sorted = useMemo(() => q.data && [...q.data].sort((a, b) => a.name.localeCompare(b.name)), [q.data]);
  return { ...q, data: sorted };
}

export const useSystem = (systemId: string) => useQuery(getSystemOptions({ path: { systemId } }));
export const useInverters = (systemId: string) => useQuery(listInvertersOptions({ path: { systemId } }));
