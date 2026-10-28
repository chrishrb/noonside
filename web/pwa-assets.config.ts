import { defineConfig, minimal2023Preset } from '@vite-pwa/assets-generator/config';

// Icons for the manifest and iOS home screen, generated from the favicon by
// `mise run web:icons` and committed in public/.
export default defineConfig({
  headLinkOptions: { preset: '2023' },
  preset: {
    ...minimal2023Preset,
    // Full-bleed on the logo tile's colour (#c2410c, --brand-sun), so masks and
    // iOS corners cut into orange, not into a white frame.
    maskable: { ...minimal2023Preset.maskable, resizeOptions: { background: '#c2410c' } },
    apple: { ...minimal2023Preset.apple, resizeOptions: { background: '#c2410c' } },
  },
  images: ['public/favicon.svg'],
});
