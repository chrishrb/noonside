import { useMutation, useQuery } from '@tanstack/react-query';
import { useState } from 'react';
import { useForm } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { createShareLinkMutation, listShareLinksOptions, revokeShareLinkMutation } from '@/api/gen/@tanstack/react-query.gen';
import { useInvalidate } from '@/api/queries';
import type { CreatedShareLink, System } from '@/api/types';
import { SectionHead } from '@/components/layout/Page';
import { Button } from '@/components/ui/Button';
import { useConfirm } from '@/components/ui/Confirm';
import { FormDialog } from '@/components/ui/Dialog';
import { Icon } from '@/components/ui/Icon';
import { Checkbox, Field, Input } from '@/components/ui/form';
import { CopyButton, SecretReveal } from '@/components/ui/misc';
import { Badge, Banner, Card, EmptyState, Skeleton } from '@/components/ui/primitives';
import { Actions, ActionsTh, Table } from '@/components/ui/Table';
import { toast } from '@/components/ui/toast';
import { fmtDate } from '@/lib/format';
import { applyServerError, type ServerError } from '@/lib/forms';

/** The kiosk variant of a share link: large numbers, cycling views. */
export const kioskUrl = (url: string) => `${url}${url.includes('?') ? '&' : '?'}kiosk=1`;

export function SharingTab({ sys }: { sys: System }) {
  const { t } = useTranslation();
  const path = { path: { systemId: sys.id } };
  const q = useQuery(listShareLinksOptions(path));
  const revoke = useMutation(revokeShareLinkMutation());
  const confirm = useConfirm();
  const invalidate = useInvalidate();
  const [createOpen, setCreateOpen] = useState(false);

  if (q.isError) return <Banner tone="danger" title={t('sharing.loadFailed')}>{q.error.message}</Banner>;
  if (!q.data) return <Skeleton style={{ height: 240 }} />;

  return (
    <>
      <SectionHead title={t('settings.tabs.sharing')} sub={t('sharing.sub')}>
        <Button variant="primary" icon="plus" onClick={() => setCreateOpen(true)}>{t('sharing.create')}</Button>
      </SectionHead>
      <Card flush>
        {q.data.length ? (
          <Table caption={t('settings.tabs.sharing')}>
            <thead>
              <tr>
                <th scope="col">{t('sharing.label')}</th><th scope="col">{t('sharing.link')}</th><th scope="col">{t('sharing.shows')}</th>
                <th scope="col">{t('common.created')}</th><th scope="col">{t('sharing.expires')}</th><ActionsTh />
              </tr>
            </thead>
            <tbody>
              {q.data.map((l) => (
                <tr key={l.id}>
                  <td><span className="flex items-center gap-2"><Icon name="link" size="sm" /><span className="font-medium">{l.label}</span></span></td>
                  <td className="mono whitespace-nowrap">/p/{l.prefix}…</td>
                  <td>{l.show_financials ? <Badge tone="outline">{t('sharing.withFinancials')}</Badge> : <span className="sm muted">{t('sharing.yieldOnly')}</span>}</td>
                  <td className="sm muted whitespace-nowrap">{fmtDate(l.created_at)}{l.created_by ? ` · ${l.created_by}` : ''}</td>
                  <td className="sm whitespace-nowrap">{l.expires_at ? fmtDate(l.expires_at) : <span className="muted">{t('sharing.never')}</span>}</td>
                  <td>
                    <Actions>
                      <Button variant="danger-ghost" size="sm" onClick={async () => {
                        const ok = await confirm({ title: t('sharing.revoke.title', { name: l.label }), danger: true, confirmLabel: t('sharing.revoke.label'), message: t('sharing.revoke.message'),
                          onConfirm: () => revoke.mutateAsync({ path: { systemId: sys.id, shareLinkId: l.id } }) });
                        if (ok) { toast(t('sharing.revoke.done', { name: l.label })); await invalidate('listShareLinks'); }
                      }}>{t('members.revoke.label')}</Button>
                    </Actions>
                  </td>
                </tr>
              ))}
            </tbody>
          </Table>
        ) : (
          <EmptyState icon="link" title={t('sharing.none')} text={t('sharing.noneText')} />
        )}
      </Card>
      <section className="section card" aria-labelledby="kiosk-h">
        <div className="grid gap-2">
          <h2 id="kiosk-h" className="flex items-center gap-2"><Icon name="monitor" />{t('sharing.kiosk.title')}</h2>
          <p className="sm muted">{t('sharing.kiosk.text')}</p>
        </div>
      </section>
      <CreateLinkDialog sys={sys} open={createOpen} onOpenChange={setCreateOpen} />
    </>
  );
}

interface FormValues { label: string; show_financials: boolean; expires: string }

function CreateLinkDialog({ sys, open, onOpenChange }: { sys: System; open: boolean; onOpenChange: (o: boolean) => void }) {
  const { t } = useTranslation();
  const create = useMutation(createShareLinkMutation());
  const invalidate = useInvalidate();
  const [created, setCreated] = useState<CreatedShareLink | null>(null);
  const [serverError, setServerError] = useState<ServerError | null>(null);
  const form = useForm<FormValues>({ defaultValues: { label: '', show_financials: false, expires: '' } });
  const close = (o: boolean) => {
    onOpenChange(o);
    if (!o) { setCreated(null); setServerError(null); form.reset(); void invalidate('listShareLinks'); }
  };

  if (created) {
    // Links point at this app, whatever public_url the server has.
    const url = `${window.location.origin}/p/${created.token}`;
    return (
      <FormDialog open={open} onOpenChange={close} title={t('sharing.copyTitle')} submitLabel={t('sharing.done')} hideCancel>
        <Banner tone="warn" title={t('sharing.onceTitle')}>{t('sharing.onceText')}</Banner>
        <SecretReveal label={created.share_link.label} value={url} />
        <div className="flex flex-wrap gap-2">
          <CopyButton text={kioskUrl(url)} label={t('sharing.copyKiosk')} size="sm" />
          <a className="btn btn-secondary btn-sm" href={url} target="_blank" rel="noreferrer"><Icon name="external" size="sm" />{t('sharing.open')}</a>
        </div>
      </FormDialog>
    );
  }
  return (
    <FormDialog open={open} onOpenChange={close} title={t('sharing.create')} subtitle={t('sharing.createSubtitle')} submitLabel={t('sharing.createLink')}
      busy={create.isPending} serverError={serverError}
      onSubmit={form.handleSubmit(async (v) => {
        setServerError(null);
        try {
          const r = await create.mutateAsync({ path: { systemId: sys.id }, body: {
            label: v.label.trim(), show_financials: v.show_financials,
            // Valid through the chosen day (in the browser's time zone).
            expires_at: v.expires ? new Date(`${v.expires}T23:59:59`).toISOString() : undefined,
          } });
          setCreated(r);
        } catch (err) { setServerError(applyServerError(err, form.setError, { label: 'label', expires_at: 'expires' })); }
      })}>
      <Field label={t('sharing.label')} required error={form.formState.errors.label?.message}>
        <Input placeholder={t('sharing.labelPlaceholder')} maxLength={200} autoFocus {...form.register('label', { validate: (v) => v.trim() !== '' || 'validation.nameRequired' })} />
      </Field>
      <Checkbox label={t('sharing.showFinancials')} help={t('sharing.showFinancialsHelp')} {...form.register('show_financials')} />
      <Field label={t('sharing.expires')} optional help={t('sharing.expiresHelp')} error={form.formState.errors.expires?.message}>
        <Input type="date" min={new Date().toISOString().slice(0, 10)} {...form.register('expires')} />
      </Field>
    </FormDialog>
  );
}
