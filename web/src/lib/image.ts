/**
 * Scales and re-encodes a picture in the browser before upload, so uploads
 * stay small (the API takes at most 2 MB) and lose their EXIF data (GPS).
 * `square` crops the centre to a square (avatars); otherwise the long edge
 * is scaled down to `max`. WebP where the browser can encode it, else JPEG.
 */
export async function prepareImage(file: Blob, { max, square }: { max: number; square?: boolean }): Promise<Blob> {
  const bitmap = await createImageBitmap(file, { imageOrientation: 'from-image' });
  try {
    let sx = 0, sy = 0, sw = bitmap.width, sh = bitmap.height;
    if (square) {
      const side = Math.min(sw, sh);
      sx = (sw - side) / 2; sy = (sh - side) / 2; sw = sh = side;
    }
    const scale = Math.min(1, max / Math.max(sw, sh));
    const canvas = document.createElement('canvas');
    canvas.width = Math.max(1, Math.round(sw * scale));
    canvas.height = Math.max(1, Math.round(sh * scale));
    const ctx = canvas.getContext('2d');
    if (!ctx) throw new Error('canvas unavailable');
    ctx.imageSmoothingQuality = 'high';
    ctx.drawImage(bitmap, sx, sy, sw, sh, 0, 0, canvas.width, canvas.height);
    const webp = await toBlob(canvas, 'image/webp', 0.85);
    // Browsers without a WebP encoder (older Safari) fall back to PNG.
    if (webp?.type === 'image/webp') return webp;
    const jpeg = await toBlob(canvas, 'image/jpeg', 0.85);
    if (!jpeg) throw new Error('image encoding failed');
    return jpeg;
  } finally {
    bitmap.close();
  }
}

const toBlob = (canvas: HTMLCanvasElement, type: string, quality: number) =>
  new Promise<Blob | null>((resolve) => canvas.toBlob(resolve, type, quality));

/** Image types the file picker offers (HEIC is converted by iOS). */
export const IMAGE_ACCEPT = 'image/jpeg,image/png,image/webp';
