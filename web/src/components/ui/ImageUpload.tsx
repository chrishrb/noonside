import { useRef, useState, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { IMAGE_ACCEPT, prepareImage } from '@/lib/image';
import { Button } from './Button';
import { useConfirm } from './Confirm';
import { toast, toastError } from './toast';

/**
 * Picture with "Upload" / "Remove" buttons (profile and system pictures).
 * The file is scaled and re-encoded in the browser (`prepareImage`) before
 * `onUpload` sends it.
 */
export function ImageUpload({ preview, hasImage, square, max, onUpload, onRemove, removeTitle, disabled }: {
  /** The current picture (Avatar or SysTile, large). */
  preview: ReactNode;
  hasImage: boolean;
  /** Crop to a centred square (avatars). */
  square?: boolean;
  /** Longest edge in pixels after scaling. */
  max: number;
  onUpload: (image: Blob) => Promise<unknown>;
  onRemove: () => Promise<unknown>;
  removeTitle: string;
  disabled?: boolean;
}) {
  const { t } = useTranslation();
  const confirm = useConfirm();
  const input = useRef<HTMLInputElement>(null);
  const [busy, setBusy] = useState(false);

  const upload = async (file: File | undefined) => {
    if (!file) return;
    setBusy(true);
    try {
      let image: Blob;
      try {
        image = await prepareImage(file, { max, square });
      } catch {
        throw new Error(t('picture.unreadable'));
      }
      await onUpload(image);
      toast(t('picture.saved'));
    } catch (e) {
      toastError(e);
    } finally {
      setBusy(false);
      if (input.current) input.current.value = '';
    }
  };

  return (
    <div className="image-upload">
      {preview}
      <div className="grid gap-2">
        <div className="image-upload-actions">
          <input ref={input} type="file" accept={IMAGE_ACCEPT} className="sr-only" tabIndex={-1} aria-hidden="true"
            onChange={(e) => upload(e.target.files?.[0])} />
          <Button icon="upload" busy={busy} disabled={disabled} onClick={() => input.current?.click()}>
            {hasImage ? t('picture.change') : t('picture.upload')}
          </Button>
          {hasImage && (
            <Button variant="danger-ghost" icon="trash" disabled={disabled || busy} onClick={async () => {
              const ok = await confirm({ title: removeTitle, message: t('picture.removeMessage'), danger: true, confirmLabel: t('picture.remove'), onConfirm: onRemove });
              if (ok) toast(t('picture.removed'));
            }}>{t('picture.remove')}</Button>
          )}
        </div>
        <p className="xs muted">{t('picture.help')}</p>
      </div>
    </div>
  );
}
