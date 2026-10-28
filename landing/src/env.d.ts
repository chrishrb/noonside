interface ImportMetaEnv {
  readonly PUBLIC_APP_URL?: string;
  readonly PUBLIC_DEMO_URL?: string;
  /** Operator for the Impressum and the Datenschutzerklärung (src/lib/site.ts). */
  readonly PUBLIC_OPERATOR_NAME?: string;
  readonly PUBLIC_OPERATOR_STREET?: string;
  readonly PUBLIC_OPERATOR_CITY?: string;
  readonly PUBLIC_OPERATOR_EMAIL?: string;
  readonly PUBLIC_OPERATOR_PHONE?: string;
  readonly PUBLIC_HOSTING_NAME?: string;
  readonly PUBLIC_HOSTING_LOCATION?: string;
  readonly PUBLIC_MAIL_PROVIDER?: string;
  readonly PUBLIC_MAIL_PROVIDER_LOCATION?: string;
}
interface ImportMeta {
  readonly env: ImportMetaEnv;
}
