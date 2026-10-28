import { defineConfig } from '@hey-api/openapi-ts';

// Generates the API client from the server's spec into src/api/gen
// (`mise run generate`): SDK functions, TanStack Query options and Zod schemas.
export default defineConfig({
  input: '../backend/internal/platform/httpapi/api.yaml',
  output: 'src/api/gen',
  plugins: [
    { name: '@hey-api/client-fetch', runtimeConfigPath: './src/api/config', baseUrl: false, throwOnError: true },
    '@hey-api/typescript',
    '@hey-api/sdk',
    { name: '@tanstack/react-query', queryOptions: true, infiniteQueryOptions: true, mutationOptions: true, queryKeys: true },
    'zod',
  ],
});
