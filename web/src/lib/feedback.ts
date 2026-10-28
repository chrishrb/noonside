import { useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import { toast } from '@/components/ui/toast';
import { openFeedback, type FeedbackLabels } from './sentry';

/** Opens the "report a problem" form (Sentry) with labels in the current language. */
export function useFeedback() {
  const { t } = useTranslation();
  return useCallback((eventId?: string) => {
    const keys: (keyof FeedbackLabels)[] = [
      'formTitle', 'messageLabel', 'messagePlaceholder', 'submitButtonLabel', 'cancelButtonLabel', 'confirmButtonLabel',
      'successMessageText', 'isRequiredLabel', 'addScreenshotButtonLabel', 'removeScreenshotButtonLabel', 'highlightToolText',
      'hideToolText', 'removeHighlightText', 'errorEmptyMessageText', 'errorGenericText', 'errorTimeoutText',
      'errorForbiddenText', 'errorNoClientText',
    ];
    const labels = Object.fromEntries(keys.map((k) => [k, t(`feedback.form.${k}`)])) as unknown as FeedbackLabels;
    void openFeedback(labels, { eventId, onSent: () => toast(t('feedback.form.successMessageText')) });
  }, [t]);
}
