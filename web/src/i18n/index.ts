import i18n from 'i18next';
import { initReactI18next } from 'react-i18next';
import { LANGUAGE, store } from '@/lib/storage';
import { en } from './en';
import { de } from './de';
import { es } from './es';

export const LANGUAGES = ['en', 'de', 'es'] as const;
export type Language = (typeof LANGUAGES)[number];
/** Language names in their own language, for the switcher. */
export const LANGUAGE_NAMES: Record<Language, string> = { en: 'English', de: 'Deutsch', es: 'Español' };

export const resources = { en: { translation: en }, de: { translation: de }, es: { translation: es } } as const;

const isLanguage = (v: string | null | undefined): v is Language => LANGUAGES.includes(v as Language);

/** The choice stored in this browser, else the first supported browser language, else English. */
export function detectLanguage(): Language {
  const saved = store.get(LANGUAGE);
  if (isLanguage(saved)) return saved;
  const langs = typeof navigator === 'undefined' ? [] : navigator.languages?.length ? navigator.languages : [navigator.language];
  for (const l of langs) {
    const base = l?.toLowerCase().split('-')[0];
    if (isLanguage(base)) return base;
  }
  return 'en';
}

void i18n.use(initReactI18next).init({
  resources,
  lng: detectLanguage(),
  fallbackLng: 'en',
  supportedLngs: LANGUAGES,
  interpolation: { escapeValue: false }, // React escapes
});

const syncHtmlLang = (l: string) => { if (typeof document !== 'undefined') document.documentElement.lang = l; };
syncHtmlLang(i18n.language);
i18n.on('languageChanged', syncHtmlLang);

export const currentLanguage = (): Language => (isLanguage(i18n.resolvedLanguage) ? i18n.resolvedLanguage : 'en');

/** BCP 47 locale for Intl formatting in the active language. */
const INTL_LOCALES: Record<Language, string> = { en: 'en-GB', de: 'de-DE', es: 'es-ES' };
export const currentLocale = () => INTL_LOCALES[currentLanguage()];

/** Switches the UI language and remembers it in this browser. */
export function setLanguage(l: Language) {
  store.set(LANGUAGE, l);
  void i18n.changeLanguage(l);
}

/** Translates `s` if it is a key (e.g. a zod message like "validation.emailRequired"), else returns it as is. */
export const translateMaybe = (s: string) => (i18n.exists(s) ? (i18n.t as unknown as (k: string) => string)(s) : s);

export default i18n;
