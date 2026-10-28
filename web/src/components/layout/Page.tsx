import { useEffect, type ReactNode } from 'react';
import clsx from 'clsx';
import { useTranslation } from 'react-i18next';
import { ApiError } from '@/api/client';
import { Button, ButtonLink } from '@/components/ui/Button';
import { Card, Skeleton } from '@/components/ui/primitives';
import { Icon } from '@/components/ui/Icon';
import { cap } from '@/lib/format';

/** Sets the document title and the mobile top bar title. */
export function usePageTitle(title: string | undefined) {
  useEffect(() => {
    if (!title) return;
    document.title = `${title} · noonside`;
    window.dispatchEvent(new CustomEvent('noonside:title', { detail: title }));
  }, [title]);
}

export function Content({ children }: { children: ReactNode }) {
  return <div className="content">{children}</div>;
}

export function PageHeader({ eyebrow, title, sub, actions, actionsClassName }: { eyebrow?: ReactNode; title: string; sub?: ReactNode; actions?: ReactNode; actionsClassName?: string }) {
  usePageTitle(title);
  return (
    <div className="page-head">
      <div className="titles">
        {eyebrow && <span className="eyebrow">{eyebrow}</span>}
        <h1 tabIndex={-1} data-page-title>{title}</h1>
        {sub && <p className="muted sm">{sub}</p>}
      </div>
      {actions && <div className={clsx('actions', actionsClassName)}>{actions}</div>}
    </div>
  );
}

export function SectionHead({ title, id, sub, children }: { title: ReactNode; id?: string; sub?: ReactNode; children?: ReactNode }) {
  return (
    <div className="section-head">
      {sub ? (
        <div className="grid gap-0.5"><h2 id={id}>{title}</h2><span className="sm muted">{sub}</span></div>
      ) : (
        <h2 id={id}>{title}</h2>
      )}
      {children}
    </div>
  );
}

/** Loading placeholder; `inline` skips the page header and padding. */
export function PageSkeleton({ inline }: { inline?: boolean }) {
  const { t } = useTranslation();
  const body = (
    <div aria-busy="true">
      <span className="sr-only">{t('common.loading')}</span>
      {!inline && (
        <>
          <Skeleton style={{ height: 28, width: 240, marginBottom: 12 }} />
          <Skeleton style={{ height: 16, width: 320, marginBottom: 32 }} />
        </>
      )}
      <div className="kpis">{[0, 1, 2, 3].map((i) => <Skeleton key={i} style={{ height: 96 }} />)}</div>
      <Skeleton style={{ height: 300, marginTop: 24 }} />
    </div>
  );
  return inline ? body : <Content>{body}</Content>;
}

export function ErrorPanel({ title, text, action, inline }: { title: string; text: ReactNode; action?: ReactNode; inline?: boolean }) {
  usePageTitle(inline ? undefined : title);
  const card = (
    <Card className="max-w-[560px]">
      <div className="grid gap-3">
        <span className="status err"><Icon name="alert" /><strong>{title}</strong></span>
        <p className="muted">{text}</p>
        {action && <div>{action}</div>}
      </div>
    </Card>
  );
  return inline ? card : <Content>{card}</Content>;
}

export function ApiErrorPanel({ error, retry, inline }: { error: unknown; retry?: () => void; inline?: boolean }) {
  const { t } = useTranslation();
  const status = error instanceof ApiError ? error.status : 0;
  const message = error instanceof Error ? error.message : '';
  const back = <ButtonLink to="/">{t('common.backToDashboard')}</ButtonLink>;
  if (status === 404) return <ErrorPanel inline={inline} title={t('errors.notFound')} text={t('errors.notFoundText', { message: cap(message) })} action={back} />;
  if (status === 403) return <ErrorPanel inline={inline} title={t('errors.noAccess')} text={t('errors.noAccessText', { message })} action={back} />;
  return (
    <ErrorPanel
      inline={inline}
      title={t('common.somethingWrong')}
      text={message || t('errors.serverNoResponse')}
      action={retry && <Button onClick={retry}>{t('common.tryAgain')}</Button>}
    />
  );
}

interface QueryLike { data: unknown; isError: boolean; error: unknown; refetch: () => unknown }
type Queries = QueryLike[];

/**
 * Renders a skeleton until every query has data, or an error panel. `children`
 * only runs once all data is there.
 */
export function QueryView({ queries, inline, children }: { queries: Queries; inline?: boolean; children: () => ReactNode }) {
  const failed = queries.find((q) => q.isError);
  if (failed) return <ApiErrorPanel inline={inline} error={failed.error} retry={() => queries.forEach((q) => q.isError && q.refetch())} />;
  if (queries.some((q) => q.data === undefined)) return <PageSkeleton inline={inline} />;
  return <>{children()}</>;
}
