import { expect, type Page, type TestInfo } from '@playwright/test';

/** Scroll boxes that may be wider than the screen (they scroll sideways). */
const SCROLLERS = ['auto', 'scroll'];

/**
 * Layout problems on the current page: the page scrolling sideways, or
 * visible elements past the screen's edges that no scroll box contains
 * (clipped by an `overflow: hidden` parent means cut off, so that counts).
 */
export async function layoutProblems(page: Page): Promise<string[]> {
  return page.evaluate((scrollers) => {
    const out: string[] = [];
    const vw = document.documentElement.clientWidth;
    const root = document.scrollingElement!;
    if (root.scrollWidth > vw + 1) out.push(`page scrolls sideways: ${root.scrollWidth}px > ${vw}px`);
    // The mobile shell scrolls inside .main instead of the page.
    const inner = document.querySelector<HTMLElement>('.app > .main > .main');
    if (inner && inner.scrollWidth > inner.clientWidth + 1) out.push(`.main is wider than the screen: ${inner.scrollWidth}px > ${inner.clientWidth}px`);

    const describe = (el: Element) => {
      const id = el.id ? `#${el.id}` : '';
      const cls = typeof el.className === 'string' && el.className ? `.${el.className.trim().split(/\s+/).slice(0, 2).join('.')}` : '';
      const text = (el.textContent ?? '').trim().replace(/\s+/g, ' ').slice(0, 40);
      return `<${el.tagName.toLowerCase()}${id}${cls}>${text ? ` "${text}"` : ''}`;
    };
    const inScroller = (el: Element) => {
      for (let p = el.parentElement; p && p !== document.body; p = p.parentElement) {
        if (scrollers.includes(getComputedStyle(p).overflowX)) return true;
      }
      return false;
    };
    for (const el of document.querySelectorAll('body *')) {
      // Map tiles and pins extend past the map by design; the map clips them.
      if (el.closest('[data-sonner-toaster], .sr-only, [aria-hidden="true"], .leaflet-pane')) continue;
      const r = el.getBoundingClientRect();
      if (r.width <= 1 || r.height <= 1) continue;
      const cs = getComputedStyle(el);
      if (cs.visibility === 'hidden' || cs.position === 'fixed' && cs.opacity === '0') continue;
      if ((r.right > vw + 1 || r.left < -1) && !inScroller(el)) {
        out.push(`${describe(el)} spans ${Math.round(r.left)}–${Math.round(r.right)}px of ${vw}px`);
        if (out.length > 12) break;
      }
    }
    return out;
  }, SCROLLERS);
}

/** Fails on console errors, uncaught exceptions and API 5xx responses. */
export function watchErrors(page: Page) {
  const errors: string[] = [];
  page.on('console', (m) => { if (m.type() === 'error') errors.push(`console: ${m.text()}`); });
  page.on('pageerror', (e) => errors.push(`exception: ${e.message}`));
  page.on('response', (r) => { if (r.url().includes('/api/') && r.status() >= 500) errors.push(`${r.status()} ${r.url()}`); });
  return errors;
}

/** Waits until queries have settled: no skeletons, spinners or stale content. */
export async function settle(page: Page) {
  await page.waitForLoadState('networkidle');
  await expect(page.locator('.skel, [aria-busy="true"]')).toHaveCount(0, { timeout: 15_000 });
  // Charts measure their width after the first paint.
  await page.waitForTimeout(300);
}

/**
 * Full-page screenshot to e2e/screenshots/<project>/<name>.png. The mobile
 * shell scrolls inside .main, so it is unpinned for the capture; toasts and
 * the install bar are hidden.
 */
export async function shoot(page: Page, info: TestInfo, name: string) {
  await page.addStyleTag({ content: `
    .app { height: auto !important; overflow: visible !important; }
    .app > .main > .main { overflow: visible !important; }
    *, *::before, *::after { animation: none !important; transition: none !important; caret-color: transparent !important; }
    /* Transient overlays that hide the page in a review. */
    [data-sonner-toaster], .install-bar { display: none !important; }` });
  await page.screenshot({ path: `e2e/screenshots/${info.project.name}/${name}.png`, fullPage: true });
}
