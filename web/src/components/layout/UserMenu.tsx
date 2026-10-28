import { useEffect, useRef, useState, type ReactNode } from 'react';
import { Link } from 'react-router';
import { Popover } from 'radix-ui';
import { useTranslation } from 'react-i18next';
import clsx from 'clsx';
import type { Me } from '@/api/types';
import { useAuth } from '@/auth/AuthProvider';
import { Icon, type IconName } from '@/components/ui/Icon';
import { Select } from '@/components/ui/form';
import { Avatar, Badge } from '@/components/ui/primitives';
import { toast } from '@/components/ui/toast';
import { useFeedback } from '@/lib/feedback';
import { sentryEnabled } from '@/lib/sentry';
import { LANGUAGES, LANGUAGE_NAMES, currentLanguage, setLanguage, type Language } from '@/i18n';
import { setTheme, useTheme, type Theme } from '@/lib/theme';

const THEMES: [Theme, IconName][] = [['system', 'monitor'], ['light', 'sun'], ['dark', 'moon']];

/** Theme as three icon buttons (menu); the profile has the labelled version. */
export function ThemeToggle() {
  const { t } = useTranslation();
  const theme = useTheme();
  return (
    <div className="seg seg-icons" role="radiogroup" aria-label={t('profile.theme')}>
      {THEMES.map(([v, icon]) => (
        <button key={v} type="button" role="radio" aria-checked={v === theme} aria-label={t(`profile.themes.${v}`)} title={t(`profile.themes.${v}`)}
          onClick={() => setTheme(v)}><Icon name={icon} size="sm" /></button>
      ))}
    </div>
  );
}

// A language switch remounts the routes (main.tsx), maybe more than once,
// which closes the menu; the menu it came from opens again for a moment.
let reopen: string | null = null;
let reopenTimer: ReturnType<typeof setTimeout> | undefined;
const keepOpen = (id: string) => {
  reopen = id;
  clearTimeout(reopenTimer);
  reopenTimer = setTimeout(() => { reopen = null; }, 2000);
};

export function LanguageSelect({ id, onChange }: { id?: string; onChange?: () => void }) {
  const { t } = useTranslation();
  return (
    <Select id={id} selectSize="sm" aria-label={t('language.label')} value={currentLanguage()}
      onChange={(e) => { onChange?.(); setLanguage(e.target.value as Language); }} options={LANGUAGES.map((l) => ({ value: l, label: LANGUAGE_NAMES[l] }))} />
  );
}

/**
 * The account menu behind the avatar: language and theme at hand, the
 * profile, and signing out. `trigger` is the avatar button (sidebar chip or
 * top bar).
 */
export function UserMenu({ id, me, trigger, side = 'bottom', align = 'end' }: {
  /** Tells the sidebar's and the top bar's menu apart. */
  id: string; me: Me; trigger: ReactNode; side?: 'top' | 'bottom'; align?: 'start' | 'end';
}) {
  const { t } = useTranslation();
  const { signOut } = useAuth();
  const openFeedback = useFeedback();
  // The old menu also reports "closed" while unmounting, so only the timer
  // ends the reopening.
  const [open, setOpen] = useState(() => reopen === id);
  // Right after reopening, focus is still leaving the removed select.
  const reopened = useRef(open);
  useEffect(() => {
    if (!reopened.current) return;
    const t = setTimeout(() => { reopened.current = false; }, 500);
    return () => clearTimeout(t);
  }, []);
  const close = () => setOpen(false);
  const item = (to: string, icon: IconName, label: string, className?: string) => (
    <Link className={clsx('sys-action', className)} to={to} onClick={close}><Icon name={icon} size="sm" />{label}</Link>
  );
  return (
    <Popover.Root open={open} onOpenChange={setOpen}>
      <Popover.Trigger asChild>{trigger}</Popover.Trigger>
      <Popover.Portal>
        <Popover.Content className="sys-pop user-pop" side={side} align={align} sideOffset={8} collisionPadding={12} aria-label={t('nav.account')}
          tabIndex={-1}
          onOpenAutoFocus={(e) => {
            e.preventDefault();
            const el = e.currentTarget as HTMLElement;
            (reopened.current ? el.querySelector<HTMLElement>('select') ?? el : el).focus();
          }}
          onFocusOutside={(e) => { if (reopened.current) e.preventDefault(); }}>
          <div className="user-pop-head">
            <Avatar name={me.name || me.email} src={me.avatar_url} />
            <span className="grid min-w-0">
              <span className="font-medium truncate">{me.name || me.email}</span>
              <span className="sm muted truncate">{me.email}</span>
            </span>
            {me.is_superadmin && <Badge tone="accent" className="flex-none">{t('common.superadmin')}</Badge>}
          </div>
          <div className="user-pop-sep" />
          <div className="user-pop-row">
            <label htmlFor={`${id}-lang`} className="user-pop-label"><Icon name="globe" size="sm" />{t('language.label')}</label>
            <LanguageSelect id={`${id}-lang`} onChange={() => keepOpen(id)} />
          </div>
          <div className="user-pop-row">
            <span className="user-pop-label"><Icon name="sun" size="sm" />{t('profile.theme')}</span>
            <ThemeToggle />
          </div>
          <div className="user-pop-sep" />
          {item('/profile/account', 'user', t('nav.profile'))}
          {item('/profile/notifications', 'bell', t('profile.tabs.notifications'))}
          {/* From 1024px the sidebar has these. */}
          {me.is_superadmin && item('/admin/systems', 'home', t('nav.allSystems'), 'lg:hidden')}
          {me.is_superadmin && item('/admin/users', 'users', t('nav.users'), 'lg:hidden')}
          {sentryEnabled && (
            <button type="button" className="sys-action" onClick={() => { close(); openFeedback(); }}>
              <Icon name="bug" size="sm" />{t('feedback.menu')}
            </button>
          )}
          <div className="user-pop-sep" />
          <button type="button" className="sys-action danger" onClick={async () => {
            close();
            await signOut();
            toast(t('common.signedOut'));
          }}><Icon name="logout" size="sm" />{t('common.signOut')}</button>
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  );
}
