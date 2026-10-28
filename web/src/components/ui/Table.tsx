import type { ReactNode } from 'react';
import clsx from 'clsx';
import { useTranslation } from 'react-i18next';

/**
 * Table in the design's style. `scroll` caps the height with a sticky header;
 * `stickyFirst` keeps the first column in view when a phone scrolls sideways.
 */
export function Table({ caption, scroll, stickyFirst, children, className }: { caption?: string; scroll?: boolean; stickyFirst?: boolean; children: ReactNode; className?: string }) {
  return (
    <div className={scroll ? 'table-scroll' : 'table-wrap'}>
      <table className={clsx('table', stickyFirst && 'sticky-first', className)}>
        {caption && <caption className="sr-only">{caption}</caption>}
        {children}
      </table>
    </div>
  );
}

/** Header cell for an actions column (visually empty). */
export function ActionsTh() {
  const { t } = useTranslation();
  return <th scope="col"><span className="sr-only">{t('common.actions')}</span></th>;
}
export const Actions = ({ children }: { children: ReactNode }) => <div className="actions">{children}</div>;
