import type { CreateClientConfig } from './gen/client.gen';

export const API_URL = import.meta.env.VITE_API_URL || '/api/v1';

// Arrays go as `?inverters=a,b` (form, explode: false), which is how api.yaml
// declares them. Interceptors (token, ApiError) are added in client.ts.
export const createClientConfig: CreateClientConfig = (config) => ({
  ...config,
  baseUrl: API_URL,
  throwOnError: true,
  querySerializer: { array: { style: 'form', explode: false } },
});
