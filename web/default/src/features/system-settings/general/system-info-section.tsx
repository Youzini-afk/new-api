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
import { useRef } from 'react'
import * as z from 'zod'
import type { Resolver } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import { FormDirtyIndicator } from '../components/form-dirty-indicator'
import { FormNavigationGuard } from '../components/form-navigation-guard'
import {
  SettingsForm,
  SettingsFormGrid,
  SettingsFormGridItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useSettingsForm } from '../hooks/use-settings-form'
import { useUpdateOption } from '../hooks/use-update-option'

const LOGO_UPLOAD_ACCEPT = 'image/png,image/jpeg,image/webp,image/gif'
const LOGO_UPLOAD_TYPES = new Set([
  'image/png',
  'image/jpeg',
  'image/webp',
  'image/gif',
])
const MAX_LOGO_STORED_SIZE_BYTES = 128 * 1024
const MAX_LOGO_SOURCE_SIZE_BYTES = 5 * 1024 * 1024
const LOGO_MAX_DIMENSION_PX = 512
const LOGO_COMPRESSION_QUALITIES = [0.92, 0.84, 0.76, 0.68, 0.6, 0.52, 0.44, 0.36]
const LOGO_COMPRESSION_DIMENSIONS = [512, 384, 256, 192, 128]
const MAX_LOGO_VALUE_LENGTH =
  Math.ceil((MAX_LOGO_STORED_SIZE_BYTES * 4) / 3) + 128

function isValidLogoValue(value: string): boolean {
  if (!value) return true
  if (value.length > MAX_LOGO_VALUE_LENGTH) return false
  if (/^data:image\/(png|jpeg|webp|gif);base64,[A-Za-z0-9+/]+={0,2}$/.test(value)) {
    return true
  }
  try {
    const url = new URL(value)
    return url.protocol === 'http:' || url.protocol === 'https:'
  } catch {
    return false
  }
}

function readFileAsDataURL(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => {
      if (typeof reader.result === 'string') {
        resolve(reader.result)
        return
      }
      reject(new Error('invalid result'))
    }
    reader.onerror = () => reject(reader.error ?? new Error('read failed'))
    reader.readAsDataURL(file)
  })
}

function getDataURLPayloadSize(dataUrl: string): number {
  const commaIndex = dataUrl.indexOf(',')
  if (commaIndex === -1) return dataUrl.length
  const base64 = dataUrl.slice(commaIndex + 1)
  let padding = 0
  if (base64.endsWith('==')) {
    padding = 2
  } else if (base64.endsWith('=')) {
    padding = 1
  }
  return Math.floor((base64.length * 3) / 4) - padding
}

function loadImageFromDataURL(dataUrl: string): Promise<HTMLImageElement> {
  return new Promise((resolve, reject) => {
    const image = new Image()
    image.onload = () => resolve(image)
    image.onerror = () => reject(new Error('image load failed'))
    image.src = dataUrl
  })
}

function getLogoCanvasSize(image: HTMLImageElement, maxDimension: number) {
  const sourceWidth = image.naturalWidth || image.width
  const sourceHeight = image.naturalHeight || image.height
  const largestSide = Math.max(sourceWidth, sourceHeight)
  const scale = largestSide > maxDimension ? maxDimension / largestSide : 1
  return {
    width: Math.max(1, Math.round(sourceWidth * scale)),
    height: Math.max(1, Math.round(sourceHeight * scale)),
  }
}

function renderLogoToDataURL(
  image: HTMLImageElement,
  maxDimension: number,
  mimeType: 'image/webp' | 'image/jpeg' | 'image/png',
  quality: number,
): string | null {
  const canvas = document.createElement('canvas')
  const size = getLogoCanvasSize(image, maxDimension)
  canvas.width = size.width
  canvas.height = size.height

  const context = canvas.getContext('2d')
  if (!context) return null
  if (mimeType === 'image/jpeg') {
    context.fillStyle = '#ffffff'
    context.fillRect(0, 0, size.width, size.height)
  }
  context.drawImage(image, 0, 0, size.width, size.height)

  const dataUrl = canvas.toDataURL(mimeType, quality)
  return dataUrl.startsWith(`data:${mimeType}`) ? dataUrl : null
}

async function prepareLogoDataURL(file: File): Promise<{
  dataUrl: string
  compressed: boolean
}> {
  const originalDataUrl = await readFileAsDataURL(file)
  if (
    file.size <= MAX_LOGO_STORED_SIZE_BYTES &&
    getDataURLPayloadSize(originalDataUrl) <= MAX_LOGO_STORED_SIZE_BYTES
  ) {
    return { dataUrl: originalDataUrl, compressed: false }
  }

  const image = await loadImageFromDataURL(originalDataUrl)
  const sourceLargestSide = Math.max(image.naturalWidth || 0, image.naturalHeight || 0)
  const candidateDimensions = LOGO_COMPRESSION_DIMENSIONS.filter(
    (dimension) => dimension <= Math.min(LOGO_MAX_DIMENSION_PX, sourceLargestSide),
  )
  const dimensions = candidateDimensions.length > 0 ? candidateDimensions : [LOGO_MAX_DIMENSION_PX]
  const mimeTypes: Array<'image/webp' | 'image/jpeg' | 'image/png'> = [
    'image/webp',
    'image/jpeg',
    'image/png',
  ]

  for (const maxDimension of dimensions) {
    for (const mimeType of mimeTypes) {
      for (const quality of LOGO_COMPRESSION_QUALITIES) {
        const dataUrl = renderLogoToDataURL(image, maxDimension, mimeType, quality)
        if (!dataUrl) continue
        if (getDataURLPayloadSize(dataUrl) <= MAX_LOGO_STORED_SIZE_BYTES) {
          return { dataUrl, compressed: true }
        }
        if (mimeType === 'image/png') break
      }
    }
  }

  throw new Error('compression failed')
}

const _systemInfoSchema = z.object({
  theme: z.object({
    frontend: z.enum(['default', 'classic']),
  }),
  SystemName: z.string().min(1),
  ServerAddress: z.string().optional(),
  Logo: z.string().refine(isValidLogoValue),
  Footer: z.string().optional(),
  About: z.string().optional(),
  HomePageContent: z.string().optional(),
  legal: z.object({
    user_agreement: z.string().optional(),
    privacy_policy: z.string().optional(),
  }),
})

type SystemInfoFormValues = z.infer<typeof _systemInfoSchema>

type SystemInfoSectionProps = {
  defaultValues: SystemInfoFormValues
}

function normalizeValue(value: unknown): string {
  if (value === undefined || value === null) return ''
  return typeof value === 'string' ? value : String(value)
}

export function SystemInfoSection({ defaultValues }: SystemInfoSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const logoFileInputRef = useRef<HTMLInputElement | null>(null)

  const normalizedDefaults: SystemInfoFormValues = {
    theme: {
      frontend:
        defaultValues.theme?.frontend === 'classic' ? 'classic' : 'default',
    },
    SystemName: normalizeValue(defaultValues.SystemName),
    ServerAddress: normalizeValue(defaultValues.ServerAddress),
    Logo: normalizeValue(defaultValues.Logo),
    Footer: normalizeValue(defaultValues.Footer),
    About: normalizeValue(defaultValues.About),
    HomePageContent: normalizeValue(defaultValues.HomePageContent),
    legal: {
      user_agreement: normalizeValue(defaultValues.legal?.user_agreement),
      privacy_policy: normalizeValue(defaultValues.legal?.privacy_policy),
    },
  }

  const systemInfoSchemaWithI18n = z.object({
    theme: z.object({
      frontend: z.enum(['default', 'classic']),
    }),
    SystemName: z.string().min(1, {
      error: () => t('System name is required'),
    }),
    ServerAddress: z.string().optional(),
    Logo: z.string().refine(isValidLogoValue, {
      error: () => t('Logo must be a valid URL or uploaded image'),
    }),
    Footer: z.string().optional(),
    About: z.string().optional(),
    HomePageContent: z.string().optional(),
    legal: z.object({
      user_agreement: z.string().optional(),
      privacy_policy: z.string().optional(),
    }),
  })

  const { form, handleSubmit, handleReset, isDirty, isSubmitting } =
    useSettingsForm<SystemInfoFormValues>({
      resolver: zodResolver(systemInfoSchemaWithI18n) as Resolver<
        SystemInfoFormValues,
        unknown,
        SystemInfoFormValues
      >,
      defaultValues: normalizedDefaults,
      onSubmit: async (_data, changedFields) => {
        for (const [key, value] of Object.entries(changedFields)) {
          let v = normalizeValue(value)
          if (key === 'ServerAddress') {
            v = v.replace(/\/+$/, '')
          }
          await updateOption.mutateAsync({
            key,
            value: v,
          })
        }
      },
    })

  return (
    <>
      <FormNavigationGuard when={isDirty} />

      <SettingsSection title={t('System Information')}>
        <Form {...form}>
          <SettingsForm onSubmit={handleSubmit}>
            <SettingsPageFormActions
              onSave={handleSubmit}
              onReset={handleReset}
              isSaving={isSubmitting || updateOption.isPending}
              isResetDisabled={!isDirty}
            />
            <FormDirtyIndicator isDirty={isDirty} />
            <SettingsFormGrid>
              <FormField
                control={form.control}
                name='theme.frontend'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Frontend Theme')}</FormLabel>
                    <Select
                      items={[
                        {
                          value: 'default',
                          label: t('Default (New Frontend)'),
                        },
                        {
                          value: 'classic',
                          label: t('Classic (Legacy Frontend)'),
                        },
                      ]}
                      onValueChange={field.onChange}
                      value={field.value}
                    >
                      <FormControl>
                        <SelectTrigger className='w-full'>
                          <SelectValue />
                        </SelectTrigger>
                      </FormControl>
                      <SelectContent alignItemWithTrigger={false}>
                        <SelectGroup>
                          <SelectItem value='default'>
                            {t('Default (New Frontend)')}
                          </SelectItem>
                          <SelectItem value='classic'>
                            {t('Classic (Legacy Frontend)')}
                          </SelectItem>
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                    <FormDescription>
                      {t(
                        'Switch between the new frontend and the classic frontend. Changes take effect after page reload.'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='SystemName'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('System Name')}</FormLabel>
                    <FormControl>
                      <Input placeholder={t('New API')} {...field} />
                    </FormControl>
                    <FormDescription>
                      {t('The name displayed across the application')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='ServerAddress'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Server Address')}</FormLabel>
                    <FormControl>
                      <Input placeholder='https://yourdomain.com' {...field} />
                    </FormControl>
                    <FormDescription>
                      {t(
                        'The public URL of your server, used for OAuth callbacks, webhooks, and other external integrations'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='Logo'
                render={({ field }) => {
                  const logoValue = field.value ?? ''
                  const isUploadedLogo = logoValue.startsWith('data:image/')
                  return (
                    <FormItem>
                      <FormLabel>{t('Logo')}</FormLabel>
                      <FormControl>
                        <div className='grid gap-2'>
                          <Input
                            ref={field.ref}
                            name={field.name}
                            onBlur={field.onBlur}
                            placeholder={t('https://example.com/logo.png')}
                            readOnly={isUploadedLogo}
                            value={
                              isUploadedLogo ? t('Uploaded image data') : logoValue
                            }
                            onChange={(event) => {
                              field.onChange(event.target.value)
                              form.clearErrors('Logo')
                            }}
                          />
                          <div className='flex flex-wrap items-center gap-2'>
                            <Input
                              ref={logoFileInputRef}
                              type='file'
                              accept={LOGO_UPLOAD_ACCEPT}
                              className='hidden'
                              onChange={async (event) => {
                                const file = event.currentTarget.files?.[0]
                                event.currentTarget.value = ''
                                if (!file) return
                                if (!LOGO_UPLOAD_TYPES.has(file.type)) {
                                  form.setError('Logo', {
                                    type: 'manual',
                                    message: t(
                                      'Logo file must be PNG, JPEG, WebP, or GIF'
                                    ),
                                  })
                                  return
                                }
                                if (file.size > MAX_LOGO_SOURCE_SIZE_BYTES) {
                                  form.setError('Logo', {
                                    type: 'manual',
                                    message: t(
                                      'Logo source image must be smaller than {{size}} MB',
                                      {
                                        size: Math.floor(
                                          MAX_LOGO_SOURCE_SIZE_BYTES /
                                            1024 /
                                            1024
                                        ),
                                      }
                                    ),
                                  })
                                  return
                                }
                                try {
                                  const result = await prepareLogoDataURL(file)
                                  field.onChange(result.dataUrl)
                                  form.clearErrors('Logo')
                                  toast.success(
                                    result.compressed
                                      ? t(
                                          'Logo image compressed and uploaded. Save changes to apply it.'
                                        )
                                      : t(
                                          'Logo image uploaded. Save changes to apply it.'
                                        )
                                  )
                                } catch {
                                  form.setError('Logo', {
                                    type: 'manual',
                                    message: t(
                                      'Failed to compress logo image below {{size}} KB. Please choose a smaller image.',
                                      {
                                        size: Math.floor(
                                          MAX_LOGO_STORED_SIZE_BYTES / 1024
                                        ),
                                      }
                                    )
                                  })
                                }
                              }}
                            />
                            <Button
                              type='button'
                              variant='outline'
                              size='sm'
                              onClick={() => logoFileInputRef.current?.click()}
                            >
                              {t('Upload logo image')}
                            </Button>
                            {logoValue ? (
                              <Button
                                type='button'
                                variant='ghost'
                                size='sm'
                                onClick={() => {
                                  field.onChange('')
                                  form.clearErrors('Logo')
                                }}
                              >
                                {t('Clear logo')}
                              </Button>
                            ) : null}
                          </div>
                          {logoValue ? (
                            <div className='border-border/70 bg-muted/20 flex items-center gap-3 rounded-lg border px-3 py-2'>
                              <img
                                src={logoValue}
                                alt={t('Logo preview')}
                                className='bg-background size-10 rounded-md object-contain'
                              />
                              <span className='text-muted-foreground text-xs'>
                                {t('Logo preview')}
                              </span>
                            </div>
                          ) : null}
                        </div>
                      </FormControl>
                      <FormDescription>
                        {t(
                          'Use a remote image URL, or upload a local PNG/JPEG/WebP/GIF up to {{sourceSize}} MB. Large images are compressed to {{storedSize}} KB before saving.',
                          {
                            sourceSize: Math.floor(
                              MAX_LOGO_SOURCE_SIZE_BYTES / 1024 / 1024
                            ),
                            storedSize: Math.floor(
                              MAX_LOGO_STORED_SIZE_BYTES / 1024
                            ),
                          }
                        )}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )
                }}
              />

              <FormField
                control={form.control}
                name='Footer'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Footer')}</FormLabel>
                    <FormControl>
                      <Textarea
                        placeholder={t(
                          '© 2025 Your Company. All rights reserved.'
                        )}
                        rows={4}
                        {...field}
                      />
                    </FormControl>
                    <FormDescription>
                      {t('Footer text displayed at the bottom of pages')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='About'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('About')}</FormLabel>
                    <FormControl>
                      <Textarea
                        placeholder={t(
                          'Enter HTML code (e.g., <p>About us...</p>) or a URL (e.g., https://example.com) to embed as iframe'
                        )}
                        rows={4}
                        {...field}
                      />
                    </FormControl>
                    <FormDescription>
                      {t(
                        'Supports HTML markup or iframe embedding. Enter HTML code directly, or provide a complete URL to automatically embed it as an iframe.'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <SettingsFormGridItem span='full'>
                <FormField
                  control={form.control}
                  name='HomePageContent'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Home Page Content')}</FormLabel>
                      <FormControl>
                        <Textarea
                          placeholder={t('Welcome to our New API...')}
                          rows={6}
                          {...field}
                        />
                      </FormControl>
                      <FormDescription>
                        {t(
                          'Content displayed on the home page (supports Markdown)'
                        )}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </SettingsFormGridItem>

              <FormField
                control={form.control}
                name='legal.user_agreement'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('User Agreement')}</FormLabel>
                    <FormControl>
                      <Textarea
                        placeholder={t(
                          'Provide Markdown, HTML, or an external URL for the user agreement'
                        )}
                        rows={6}
                        {...field}
                      />
                    </FormControl>
                    <FormDescription>
                      {t(
                        'Leave empty to disable the agreement requirement. Supports Markdown, HTML, or a full URL to redirect users.'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='legal.privacy_policy'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Privacy Policy')}</FormLabel>
                    <FormControl>
                      <Textarea
                        placeholder={t(
                          'Provide Markdown, HTML, or an external URL for the privacy policy'
                        )}
                        rows={6}
                        {...field}
                      />
                    </FormControl>
                    <FormDescription>
                      {t(
                        'Leave empty to disable the privacy policy requirement. Supports Markdown, HTML, or a full URL to redirect users.'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </SettingsFormGrid>
          </SettingsForm>
        </Form>
      </SettingsSection>
    </>
  )
}
