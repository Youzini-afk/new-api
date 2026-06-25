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
import { Link } from '@tanstack/react-router'
import type { CSSProperties } from 'react'
import { useTranslation } from 'react-i18next'

import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { useTheme } from '@/context/theme-provider'
import { useStatus } from '@/hooks/use-status'
import { INTERFACE_LANGUAGE_OPTIONS } from '@/i18n/languages'

import { YouziStarTunnelBackground } from './youzi-star-tunnel-background'

interface YouziLandingProps {
  isAuthenticated: boolean
}

interface BottomLink {
  label: string
  href: string
  external?: boolean
}

interface FloatingNode {
  id: string
  label: string
  x: string
  y: string
  size: number
  color: string
  delay: string
  duration: string
}

const FLOATING_NODES: FloatingNode[] = [
  {
    id: 'openai-a',
    label: '◎',
    x: '41%',
    y: '13%',
    size: 28,
    color: '#10a37f',
    delay: '0s',
    duration: '7.6s',
  },
  {
    id: 'claude-a',
    label: '✦',
    x: '16%',
    y: '18%',
    size: 34,
    color: '#d97757',
    delay: '-1.1s',
    duration: '8.2s',
  },
  {
    id: 'gemini-a',
    label: '✧',
    x: '58%',
    y: '15%',
    size: 26,
    color: '#8ab4f8',
    delay: '-2.2s',
    duration: '9s',
  },
  {
    id: 'meta-a',
    label: '∞',
    x: '68%',
    y: '23%',
    size: 36,
    color: '#1a73e8',
    delay: '-3.4s',
    duration: '8.8s',
  },
  {
    id: 'deepseek-a',
    label: '⌘',
    x: '31%',
    y: '31%',
    size: 22,
    color: '#4d6bfe',
    delay: '-1.8s',
    duration: '7.9s',
  },
  {
    id: 'qwen-a',
    label: '◇',
    x: '61%',
    y: '32%',
    size: 40,
    color: '#6657fa',
    delay: '-4.6s',
    duration: '9.4s',
  },
  {
    id: 'mistral-a',
    label: '✺',
    x: '76%',
    y: '39%',
    size: 24,
    color: '#f26e24',
    delay: '-2.9s',
    duration: '8.1s',
  },
  {
    id: 'azure-a',
    label: '▣',
    x: '12%',
    y: '41%',
    size: 30,
    color: '#00a4ef',
    delay: '-5s',
    duration: '8.7s',
  },
  {
    id: 'grok-a',
    label: '𝕏',
    x: '48%',
    y: '43%',
    size: 18,
    color: '#cbd5e1',
    delay: '-0.7s',
    duration: '7.4s',
  },
  {
    id: 'sora-a',
    label: '▻',
    x: '89%',
    y: '44%',
    size: 34,
    color: '#4d6bfe',
    delay: '-3.1s',
    duration: '9.1s',
  },
  {
    id: 'kimi-a',
    label: '☾',
    x: '23%',
    y: '55%',
    size: 20,
    color: '#a78bfa',
    delay: '-2.6s',
    duration: '8.5s',
  },
  {
    id: 'glm-a',
    label: '⌬',
    x: '39%',
    y: '62%',
    size: 32,
    color: '#00cc99',
    delay: '-1.6s',
    duration: '7.8s',
  },
  {
    id: 'cohere-a',
    label: '◆',
    x: '71%',
    y: '64%',
    size: 20,
    color: '#ff9db5',
    delay: '-3.8s',
    duration: '8.9s',
  },
  {
    id: 'midjourney-a',
    label: '⛵',
    x: '86%',
    y: '74%',
    size: 24,
    color: '#94a3b8',
    delay: '-4.4s',
    duration: '9.3s',
  },
  {
    id: 'baidu-a',
    label: '✣',
    x: '27%',
    y: '83%',
    size: 28,
    color: '#2932e1',
    delay: '-0.9s',
    duration: '8.4s',
  },
  {
    id: 'stability-a',
    label: '✹',
    x: '54%',
    y: '86%',
    size: 22,
    color: '#7a52ff',
    delay: '-2s',
    duration: '7.7s',
  },
  {
    id: 'openai-b',
    label: '◎',
    x: '81%',
    y: '90%',
    size: 31,
    color: '#10a37f',
    delay: '-3.5s',
    duration: '8.3s',
  },
]

function useBottomLinks(): BottomLink[] {
  const { t } = useTranslation()
  const { status } = useStatus()
  const docsUrl =
    (status?.docs_link as string | undefined) || 'https://docs.newapi.pro'

  return [
    { label: t('Docs'), href: docsUrl, external: docsUrl.startsWith('http') },
    { label: t('API Reference'), href: '/keys' },
    { label: t('Provider Guide'), href: '/dashboard/channel' },
    {
      label: 'GitHub',
      href: 'https://github.com/QuantumNous/new-api',
      external: true,
    },
    { label: t('Status'), href: '/dashboard' },
  ]
}

function SlashSeparator() {
  return (
    <span
      aria-hidden='true'
      className='text-foreground/25 select-none dark:text-pink-200/35'
    >
      /
    </span>
  )
}

function ThemeControl() {
  const { resolvedTheme, setTheme } = useTheme()
  const { t } = useTranslation()
  const isDark = resolvedTheme === 'dark'
  const nextLabel = isDark ? t('Light') : t('Dark')

  return (
    <button
      type='button'
      onClick={() => setTheme(isDark ? 'light' : 'dark')}
      className='text-foreground/60 hover:bg-foreground/5 hover:text-foreground rounded-full px-2.5 py-1 text-xs font-semibold tracking-wide transition-colors sm:text-sm dark:text-pink-100/80 dark:hover:bg-pink-100/10 dark:hover:text-pink-100'
    >
      {nextLabel}
    </button>
  )
}

function LanguageControl() {
  const { i18n, t } = useTranslation()
  const currentLanguage = i18n.language?.startsWith('zh') ? '中文' : 'EN'

  return (
    <DropdownMenu modal={false}>
      <DropdownMenuTrigger
        render={
          <button
            type='button'
            className='text-foreground/60 hover:bg-foreground/5 hover:text-foreground rounded-full px-2.5 py-1 text-xs font-semibold tracking-wide transition-colors sm:text-sm dark:text-pink-100/80 dark:hover:bg-pink-100/10 dark:hover:text-pink-100'
            aria-label={t('Language')}
          />
        }
      >
        {currentLanguage}
      </DropdownMenuTrigger>
      <DropdownMenuContent align='end' className='min-w-36'>
        {INTERFACE_LANGUAGE_OPTIONS.map((lang) => (
          <DropdownMenuItem
            key={lang.code}
            onClick={() => i18n.changeLanguage(lang.code)}
          >
            {lang.label}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

function FloatingModelNodes() {
  return (
    <div
      aria-hidden='true'
      className='pointer-events-none absolute inset-0 z-[2] hidden lg:block'
    >
      {FLOATING_NODES.map((node) => (
        <div
          key={node.id}
          className='landing-orb-float absolute grid place-items-center rounded-full border border-white/20 bg-white/75 font-semibold text-slate-800 shadow-[0_0_22px_var(--node-color)] backdrop-blur-sm dark:bg-slate-950/70 dark:text-white'
          style={
            {
              left: node.x,
              top: node.y,
              width: node.size,
              height: node.size,
              color: node.color,
              '--node-color': `${node.color}88`,
              animationDelay: node.delay,
              animationDuration: node.duration,
            } as CSSProperties
          }
        >
          <span className='text-[0.78em]'>{node.label}</span>
        </div>
      ))}
    </div>
  )
}

export function YouziLanding(props: YouziLandingProps) {
  const { t } = useTranslation()
  const bottomLinks = useBottomLinks()
  const authLink = props.isAuthenticated ? '/dashboard' : '/sign-in'
  const authLabel = props.isAuthenticated ? t('Console') : t('Sign In')

  return (
    <main className='bg-background text-foreground relative h-svh min-h-svh w-full overflow-hidden'>
      <YouziStarTunnelBackground />
      <FloatingModelNodes />

      <div className='absolute top-4 right-4 z-20 flex max-w-[calc(100%-2rem)] flex-wrap items-center justify-end gap-1 sm:top-6 sm:right-7'>
        <Link
          to={authLink}
          className='text-foreground/65 hover:bg-foreground/5 hover:text-foreground rounded-full px-2.5 py-1 text-xs font-semibold tracking-wide transition-colors sm:text-sm dark:text-pink-100/85 dark:hover:bg-pink-100/10 dark:hover:text-pink-100'
        >
          {authLabel}
        </Link>
        <SlashSeparator />
        <ThemeControl />
        <SlashSeparator />
        <LanguageControl />
      </div>

      <section className='relative z-10 flex h-svh min-h-svh flex-col items-center justify-center px-5 text-center'>
        <h1
          className='landing-animate-scale-in text-[clamp(4.5rem,13vw,10rem)] leading-none font-normal tracking-[-0.055em] text-[#d63384] select-none dark:text-[#ff85a2]'
          style={{
            fontFamily: 'Pacifico, "Segoe Script", "Marker Felt", cursive',
            textShadow:
              '0 0 28px rgba(255,133,162,0.48), 0 0 64px rgba(255,133,162,0.24), 0 2px 8px rgba(0,0,0,0.22)',
          }}
        >
          Youziapi
        </h1>

        <p className='landing-animate-fade-up text-foreground/70 mt-4 max-w-[min(86vw,760px)] text-base font-semibold tracking-[0.08em] [animation-delay:120ms] sm:text-xl md:text-2xl dark:text-pink-100/82'>
          {t('Where AI calls become Youzi trails')}
        </p>
      </section>

      <div className='absolute right-0 bottom-7 left-0 z-20 flex flex-col items-center gap-3 px-6 sm:bottom-8'>
        <nav
          className='text-foreground/45 flex flex-wrap items-center justify-center gap-x-3 gap-y-1.5 text-[11px] font-medium tracking-wide sm:text-xs dark:text-pink-100/45'
          aria-label={t('Footer')}
        >
          {bottomLinks.map((link, index) => (
            <span key={link.href} className='inline-flex items-center gap-x-3'>
              {link.external || !link.href.startsWith('/') ? (
                <a
                  href={link.href}
                  target={link.external ? '_blank' : undefined}
                  rel={link.external ? 'noopener noreferrer' : undefined}
                  className='transition-colors hover:text-[#d63384] dark:hover:text-[#ff85a2]'
                >
                  {link.label}
                </a>
              ) : (
                <Link
                  to={link.href}
                  className='transition-colors hover:text-[#d63384] dark:hover:text-[#ff85a2]'
                >
                  {link.label}
                </Link>
              )}
              {index < bottomLinks.length - 1 && (
                <span aria-hidden='true' className='opacity-50 select-none'>
                  ·
                </span>
              )}
            </span>
          ))}
        </nav>

        <p className='text-foreground/32 text-center text-[10px] tracking-[0.05em] sm:text-[11px] dark:text-pink-100/35'>
          {t('OpenAI-compatible · Multi-provider routing · Usage insights')}
        </p>
      </div>
    </main>
  )
}
