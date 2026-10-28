import type { APIRoute } from 'astro';

export const GET: APIRoute = ({ site }) =>
  new Response(`User-agent: *\nAllow: /\nDisallow: /warteliste/\nDisallow: /bald/\n\nSitemap: ${new URL('sitemap-index.xml', site)}\n`);
