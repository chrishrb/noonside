import { useMutation, useQuery } from '@tanstack/react-query';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useForm, useWatch } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { z } from 'zod';
import { getNotificationTargetsOptions, sendTestToTargetMutation, setNotificationTargetsMutation } from '@/api/gen/@tanstack/react-query.gen';
import { useInvalidate } from '@/api/queries';
import type { NotificationTargets as Targets } from '@/api/types';
import { Button } from '@/components/ui/Button';
import { Field, Input } from '@/components/ui/form';
import { BackLink } from '@/components/ui/NavList';
import { Banner, Card, Skeleton } from '@/components/ui/primitives';
import { CopyButton, Disclosure } from '@/components/ui/misc';
import { toast, toastError } from '@/components/ui/toast';
import { applyServerError, type ServerError } from '@/lib/forms';

type Channel = 'ntfy' | 'webhook';

const url = z.string().trim().max(2000).refine((v) => v === '' || /^https?:\/\/[^/]/i.test(v), 'notifications.targets.urlInvalid');
const schema = z.object({
  ntfy_url: url,
  ntfy_token: z.string().trim().max(200, 'validation.max200'),
  webhook_url: url,
  webhook_secret: z.string().max(200, 'validation.max200'),
});
type Form = z.infer<typeof schema>;

/** A random signing secret (32 bytes, base64url). */
export function newSecret(): string {
  const b = crypto.getRandomValues(new Uint8Array(32));
  return btoa(String.fromCharCode(...b)).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

export function TargetCard({ channel, back }: { channel: Channel; back: string }) {
  const { t } = useTranslation();
  const q = useQuery(getNotificationTargetsOptions());
  if (q.isError) return <Banner tone="danger" title={t('notifications.targets.loadFailed')}>{q.error.message}</Banner>;
  if (!q.data) return <Skeleton style={{ height: 240 }} />;
  return <TargetForm key={channel} channel={channel} back={back} initial={q.data} />;
}

/** One target (ntfy or webhook); saves only its own fields. */
function TargetForm({ channel, back, initial }: { channel: Channel; back: string; initial: Targets }) {
  const { t } = useTranslation();
  const save = useMutation(setNotificationTargetsMutation());
  const test = useMutation(sendTestToTargetMutation());
  const invalidate = useInvalidate();
  const [saved, setSaved] = useState(initial);
  const [serverError, setServerError] = useState<ServerError | null>(null);
  const toForm = (v: Targets): Form => ({ ntfy_url: v.ntfy_url, ntfy_token: '', webhook_url: v.webhook_url, webhook_secret: '' });
  const form = useForm<Form>({ resolver: zodResolver(schema), defaultValues: toForm(initial) });
  const e = form.formState.errors;
  const msg = (k: keyof Form) => e[k]?.message as string | undefined;
  const secret = useWatch({ control: form.control, name: 'webhook_secret' });
  const ntfy = channel === 'ntfy';

  const submit = form.handleSubmit(async (v) => {
    setServerError(null);
    // Empty token/secret fields keep the stored ones; the other target is left alone.
    const body = ntfy
      ? { ntfy_url: v.ntfy_url, ...(v.ntfy_token && { ntfy_token: v.ntfy_token }) }
      : { webhook_url: v.webhook_url, ...(v.webhook_secret && { webhook_secret: v.webhook_secret }) };
    try {
      const r = await save.mutateAsync({ body });
      setSaved(r);
      form.reset(toForm(r));
      await invalidate('getNotificationTargets');
      toast(t('notifications.targets.saved'));
    } catch (err) {
      setServerError(applyServerError(err, form.setError, { ntfy_url: 'ntfy_url', ntfy_token: 'ntfy_token', webhook_url: 'webhook_url', webhook_secret: 'webhook_secret' }));
    }
  });
  const removeSecret = async () => {
    try {
      setSaved(await save.mutateAsync({ body: { [ntfy ? 'ntfy_token' : 'webhook_secret']: '' } }));
      await invalidate('getNotificationTargets');
      toast(t('notifications.targets.saved'));
    } catch (err) { toastError(err); }
  };
  const runTest = async () => {
    try {
      const r = await test.mutateAsync({ query: { channel } });
      if (r.ok) toast(t('notifications.targets.testSent'));
      else toastError(new Error(t('notifications.targets.testFailed', { error: r.error ?? '' })));
    } catch (err) { toastError(err); }
  };
  const dirty = form.formState.isDirty;
  const configured = ntfy ? saved.ntfy_url : saved.webhook_url;
  const hasSecret = ntfy ? saved.ntfy_has_token : saved.webhook_has_secret;

  return (
    <Card as="section" aria-labelledby="target-h">
      <form className="form" onSubmit={submit} noValidate>
        <BackLink to={back}>{t('notifications.prefs.title')}</BackLink>
        {serverError && <Banner tone="danger" role="alert" title={serverError.title}>{serverError.message}</Banner>}
        <div className="grid max-w-[60ch] gap-1">
          <h2 id="target-h">{ntfy ? 'ntfy' : 'Webhook'}</h2>
          <p className="sm muted">{t(`notifications.targets.${channel}Intro`)}</p>
        </div>

        {ntfy ? (<>
          <Field label={t('notifications.targets.ntfyUrl')} optional help={t('notifications.targets.ntfyUrlHelp')} error={msg('ntfy_url')}>
            <Input type="url" inputMode="url" autoComplete="off" spellCheck={false} placeholder="https://ntfy.sh/my-solar-topic" {...form.register('ntfy_url')} />
          </Field>
          <Field label={t('notifications.targets.ntfyToken')} optional error={msg('ntfy_token')}
            help={saved.ntfy_has_token ? t('notifications.targets.secretKeep') : t('notifications.targets.ntfyTokenHelp')}>
            <Input type="password" autoComplete="new-password" placeholder={saved.ntfy_has_token ? '••••••••' : 'tk_…'} {...form.register('ntfy_token')} />
          </Field>
        </>) : (<>
          <Field label={t('notifications.targets.webhookUrl')} optional help={t('notifications.targets.webhookUrlHelp')} error={msg('webhook_url')}>
            <Input type="url" inputMode="url" autoComplete="off" spellCheck={false} placeholder="https://example.com/noonside-hook" {...form.register('webhook_url')} />
          </Field>
          <Field label={t('notifications.targets.webhookSecret')} optional error={msg('webhook_secret')}
            help={secret ? t('notifications.targets.secretCopy') : saved.webhook_has_secret ? t('notifications.targets.secretKeep') : t('notifications.targets.webhookSecretHelp')}>
            <div className="flex flex-wrap gap-2">
              <Input className="mono min-w-0 flex-1 basis-56" autoComplete="off" spellCheck={false} placeholder={saved.webhook_has_secret ? '••••••••' : ''} {...form.register('webhook_secret')} />
              <Button icon="key" onClick={() => form.setValue('webhook_secret', newSecret(), { shouldDirty: true })}>{t('notifications.targets.generate')}</Button>
              {secret && <CopyButton text={secret} />}
            </div>
          </Field>
          <Disclosure summary={t('notifications.targets.verifyTitle')}>
            <div className="grid gap-2">
              <p className="sm">{t('notifications.targets.verifyText')}</p>
              <pre className="code" tabIndex={0} aria-label={t('notifications.targets.verifyTitle')}>{VERIFY_SNIPPET}</pre>
            </div>
          </Disclosure>
        </>)}

        <div className="flex flex-wrap items-center gap-2">
          <Button type="submit" variant="primary" busy={save.isPending} disabled={!dirty}>{t('notifications.targets.save')}</Button>
          <Button icon="play" disabled={!configured || dirty} busy={test.isPending} onClick={runTest}>{t('notifications.targets.test')}</Button>
          {hasSecret && <Button variant="ghost" disabled={save.isPending} onClick={removeSecret}>{t(ntfy ? 'notifications.targets.removeToken' : 'notifications.targets.removeSecret')}</Button>}
        </div>
        {dirty && <p className="sm muted">{t('notifications.targets.saveFirst')}</p>}
      </form>
    </Card>
  );
}

export const VERIFY_SNIPPET = `// Node.js
const sig = 'sha256=' + crypto.createHmac('sha256', SECRET)
  .update(rawBody).digest('hex');
const got = String(req.headers['x-noonside-signature'] ?? '');
const ok = got.length === sig.length &&
  crypto.timingSafeEqual(Buffer.from(got), Buffer.from(sig));`;
