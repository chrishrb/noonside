// The noonside mark, as in public/noonside-logo.svg: an "n" under the noon sun
// on the brand tile. White in both themes, like the app icon.
export const LogoMark = ({ size = 28 }: { size?: number }) => (
  <svg className="logo-mark" viewBox="0 0 32 32" width={size} height={size} aria-hidden="true" focusable="false">
    <rect width="32" height="32" rx="8" fill="var(--brand-sun)" />
    <path d="M11.5 24v-5.5a4.5 4.5 0 0 1 9 0v5.5" fill="none" stroke="#ffffff" strokeWidth="2.8" strokeLinecap="round" strokeLinejoin="round" />
    <circle cx="16" cy="8" r="2.5" fill="#ffffff" />
  </svg>
);
