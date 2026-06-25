/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import i18next from 'i18next'

// ============================================================================
// Avatar image compression helper
// ============================================================================
// Reads an uploaded PNG/JPEG file, crops it to a centered square, downscales
// it to AVATAR_OUTPUT_SIZE x AVATAR_OUTPUT_SIZE, and re-encodes as JPEG while
// stepping quality down until the result fits under AVATAR_TARGET_BYTES, with
// AVATAR_HARD_LIMIT_BYTES as the final backend-compatible fallback.
//
// Notes:
// - Uses an object URL for the source image (never a data URL stored in auth
//   state) and revokes it once the bitmap is loaded.
// - PNG transparency is flattened onto a solid background because avatars are
//   rendered on circular/rounded surfaces; JPEG keeps the payload tiny.
// - Validation messages are translated via i18next so users see localized
//   text in the toast that surfaces them.
// ============================================================================

/** Maximum source file size accepted by the picker (5 MB). */
export const AVATAR_MAX_SOURCE_BYTES = 5 * 1024 * 1024

/** Output square side length in pixels. */
export const AVATAR_OUTPUT_SIZE = 256

/** Browser-side decode guard, aligned with backend dimensions. */
export const AVATAR_MAX_DIMENSION = 2048

/** Browser-side pixel guard, aligned with backend max pixel count. */
export const AVATAR_MAX_PIXELS = 4 * 1024 * 1024

/**
 * Hard backend limit. We never emit a blob larger than this.
 * The compressor targets AVATAR_TARGET_BYTES first and only falls back to this.
 */
export const AVATAR_HARD_LIMIT_BYTES = 512 * 1024

/**
 * Preferred maximum payload size. 128 KB is comfortably below the backend
 * hard limit while preserving enough quality for avatar-sized renders.
 */
export const AVATAR_TARGET_BYTES = 128 * 1024

/** MIME types accepted by the hidden file input. */
export const AVATAR_ACCEPT =
  'image/png,image/jpeg' as const

export class AvatarValidationError extends Error {
  constructor(message: string) {
    super(message)
    this.name = 'AvatarValidationError'
  }
}

function isSupportedImageType(file: File): boolean {
  return file.type === 'image/png' || file.type === 'image/jpeg'
}

function loadImage(source: string): Promise<HTMLImageElement> {
  return new Promise((resolve, reject) => {
    const image = new Image()
    image.onload = () => resolve(image)
    image.onerror = () =>
      reject(new AvatarValidationError(i18next.t('Failed to decode image')))
    image.src = source
  })
}

function canvasToBlob(
  canvas: HTMLCanvasElement,
  quality: number
): Promise<Blob> {
  return new Promise((resolve, reject) => {
    canvas.toBlob(
      (blob) => {
        if (blob) resolve(blob)
        else
          reject(
            new AvatarValidationError(i18next.t('Failed to encode image'))
          )
      },
      'image/jpeg',
      quality
    )
  })
}

/**
 * Compress an uploaded image file into a centered-square JPEG avatar blob.
 *
 * @throws {AvatarValidationError} when the file is rejected before encoding,
 *         or when the encoded result cannot be made small enough.
 */
export async function compressAvatarImage(file: File): Promise<Blob> {
  if (!isSupportedImageType(file)) {
    throw new AvatarValidationError(
      i18next.t('Only PNG or JPEG images are supported')
    )
  }
  if (file.size > AVATAR_MAX_SOURCE_BYTES) {
    throw new AvatarValidationError(
      i18next.t('Image must be 5 MB or smaller')
    )
  }

  // Use an object URL just for decoding the source; revoke it immediately
  // so nothing long-lived is retained.
  const objectUrl = URL.createObjectURL(file)
  let image: HTMLImageElement
  try {
    image = await loadImage(objectUrl)
  } finally {
    URL.revokeObjectURL(objectUrl)
  }

  const sourceSize = Math.min(image.naturalWidth, image.naturalHeight)
  if (sourceSize === 0) {
    throw new AvatarValidationError(i18next.t('Image has no usable dimensions'))
  }
  if (
    image.naturalWidth > AVATAR_MAX_DIMENSION ||
    image.naturalHeight > AVATAR_MAX_DIMENSION ||
    image.naturalWidth * image.naturalHeight > AVATAR_MAX_PIXELS
  ) {
    throw new AvatarValidationError(
      i18next.t('Image dimensions are too large')
    )
  }

  // Centered square crop coordinates on the source bitmap.
  const offsetX = (image.naturalWidth - sourceSize) / 2
  const offsetY = (image.naturalHeight - sourceSize) / 2

  const canvas = document.createElement('canvas')
  canvas.width = AVATAR_OUTPUT_SIZE
  canvas.height = AVATAR_OUTPUT_SIZE
  const context = canvas.getContext('2d')
  if (!context) {
    throw new AvatarValidationError(
      i18next.t('Canvas 2D context unavailable')
    )
  }

  // Flatten PNG transparency onto a neutral background so JPEG output looks
  // clean on circular avatars instead of turning black.
  context.fillStyle = '#ffffff'
  context.fillRect(0, 0, AVATAR_OUTPUT_SIZE, AVATAR_OUTPUT_SIZE)
  context.imageSmoothingEnabled = true
  context.imageSmoothingQuality = 'high'
  context.drawImage(
    image,
    offsetX,
    offsetY,
    sourceSize,
    sourceSize,
    0,
    0,
    AVATAR_OUTPUT_SIZE,
    AVATAR_OUTPUT_SIZE
  )

  // Step JPEG quality down until we fit under the target. Keep the best
  // backend-compatible fallback in case a highly detailed image cannot reach
  // the preferred target without excessive quality loss.
  const qualities = [0.92, 0.85, 0.78, 0.7, 0.62, 0.55, 0.45]
  let hardLimitFallback: Blob | null = null
  for (const quality of qualities) {
    const candidate = await canvasToBlob(canvas, quality)
    if (candidate.size <= AVATAR_TARGET_BYTES) {
      return candidate
    }
    if (candidate.size <= AVATAR_HARD_LIMIT_BYTES) {
      hardLimitFallback = candidate
    }
  }

  if (hardLimitFallback) {
    return hardLimitFallback
  }

  throw new AvatarValidationError(i18next.t('Failed to compress avatar'))
}
