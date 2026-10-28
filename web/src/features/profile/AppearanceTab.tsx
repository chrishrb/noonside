import { useTranslation } from 'react-i18next';
import { LANGUAGES, LANGUAGE_NAMES, currentLanguage, setLanguage } from '@/i18n';
import { useInstallApp } from '@/components/layout/InstallBar';
import { Button } from '@/components/ui/Button';
import { FormSection, FormSections, Segmented } from '@/components/ui/form';
import { Card } from '@/components/ui/primitives';
import { useInstall } from '@/lib/install';
import { setTheme, useTheme } from '@/lib/theme';

export function AppearanceTab() {
  const { t } = useTranslation();
  const theme = useTheme();
  const install = useInstall();
  const installApp = useInstallApp();
  return (
    <div className="settings-grid">
      <Card as="section" aria-label={t('profile.tabs.appearance')}>
        <FormSections>
          <FormSection title={t('profile.theme')} description={t('profile.storedInBrowser')}>
            <Segmented label={t('profile.theme')} value={theme} onChange={setTheme}
              options={[['system', t('profile.themes.system'), 'monitor'], ['light', t('profile.themes.light'), 'sun'], ['dark', t('profile.themes.dark'), 'moon']]} />
          </FormSection>
          <FormSection title={t('language.label')} description={t('profile.languageHelp')}>
            <Segmented label={t('language.label')} value={currentLanguage()} onChange={setLanguage} options={LANGUAGES.map((l) => [l, LANGUAGE_NAMES[l]])} />
          </FormSection>
        </FormSections>
      </Card>

      {!install.standalone && (install.canPrompt || install.ios) && (
        <Card as="section" aria-labelledby="app-h">
          <div className="flex flex-wrap items-center justify-between gap-4">
            <div className="grid max-w-[60ch] gap-1">
              <h2 id="app-h">{t('pwa.app')}</h2>
              <p className="sm muted">{t('pwa.installText')}</p>
            </div>
            <Button icon="phone" busy={installApp.busy} onClick={installApp.start}>{t('pwa.installApp')}</Button>
            {installApp.dialog}
          </div>
        </Card>
      )}
    </div>
  );
}
