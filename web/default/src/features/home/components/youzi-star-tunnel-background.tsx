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
import { useEffect, useRef, useState } from 'react'

import { useTheme } from '@/context/theme-provider'
import { cn } from '@/lib/utils'

/**
 * YouziStarTunnelBackground
 *
 * Atmospheric canvas star-tunnel backdrop for the Youziapi landing page.
 * Renders a soft radial gradient in both themes plus a lightweight particle
 * tunnel of stars and colored "model orbs" drifting toward the viewer. Honors
 * `prefers-reduced-motion` (static gradient only, no RAF).
 *
 * Theme is sourced from the existing ThemeProvider so the canvas tracks the
 * global light/dark switch with no separate state.
 */
interface YouziStarTunnelBackgroundProps {
  className?: string
}

const DESKTOP_STARS = 360
const MOBILE_STARS = 180
const DESKTOP_ORBS = 26
const MOBILE_ORBS = 12
const PERSPECTIVE = 700
const MAX_DEPTH = 4500
const FOG_START_Z = -800
const FOG_FULL_Z = -4200
const PARALLAX_X = 70
const PARALLAX_Y = 45
const PARALLAX_LERP = 0.05
const ENTRY_MS = 1500
const ENTRY_BOOST = 4

const ORB_PALETTE = [
  '#7c5cff',
  '#9b6bff',
  '#5ea8ff',
  '#4ad6c8',
  '#ff9db5',
  '#ffd166',
  '#a78bfa',
  '#f78ca0',
]

type EntityType = 'star' | 'orb'

interface Entity {
  type: EntityType
  angle: number
  radius: number
  z: number
  speed: number
  rotSpeed: number
  twPhase: number
  twFreq: number
  color?: string
  size?: number
}

function prefersReducedMotion(): boolean {
  if (typeof window === 'undefined') return false
  return window.matchMedia('(prefers-reduced-motion: reduce)').matches
}

export function YouziStarTunnelBackground(
  props: YouziStarTunnelBackgroundProps
) {
  const { resolvedTheme } = useTheme()
  const isDark = resolvedTheme === 'dark'

  const containerRef = useRef<HTMLDivElement>(null)
  const canvasRef = useRef<HTMLCanvasElement>(null)
  const mouseRef = useRef({ x: 0, y: 0 })
  const parallaxRef = useRef({ x: 0, y: 0 })
  const isDarkRef = useRef(isDark)
  const [reduced, setReduced] = useState<boolean>(() => prefersReducedMotion())

  useEffect(() => {
    isDarkRef.current = isDark
  }, [isDark])

  useEffect(() => {
    if (typeof window === 'undefined') return undefined
    const mq = window.matchMedia('(prefers-reduced-motion: reduce)')
    const handler = () => setReduced(mq.matches)
    mq.addEventListener('change', handler)
    return () => mq.removeEventListener('change', handler)
  }, [])

  useEffect(() => {
    if (reduced) return undefined
    const canvas = canvasRef.current
    const container = containerRef.current
    if (!canvas || !container) return undefined
    const ctx = canvas.getContext('2d')
    if (!ctx) return undefined

    const isMobile = window.matchMedia('(max-width: 600px)').matches
    const starCount = isMobile ? MOBILE_STARS : DESKTOP_STARS
    const orbCount = isMobile ? MOBILE_ORBS : DESKTOP_ORBS

    const entities: Entity[] = []
    for (let i = 0; i < starCount; i++) {
      entities.push({
        type: 'star',
        angle: Math.random() * Math.PI * 2,
        radius: 80 + Math.random() * 2800,
        z: -Math.random() * MAX_DEPTH,
        speed: 8 + Math.random() * 22,
        rotSpeed: (Math.random() - 0.5) * 0.001,
        twPhase: Math.random() * Math.PI * 2,
        twFreq: 0.0009 + Math.random() * 0.0022,
      })
    }
    for (let i = 0; i < orbCount; i++) {
      entities.push({
        type: 'orb',
        angle: Math.random() * Math.PI * 2,
        radius: 260 + Math.random() * 1500,
        z: -Math.random() * MAX_DEPTH,
        speed: 5 + Math.random() * 6,
        rotSpeed: (Math.random() - 0.5) * 0.0012,
        twPhase: Math.random() * Math.PI * 2,
        twFreq: 0.0004 + Math.random() * 0.0006,
        color: ORB_PALETTE[i % ORB_PALETTE.length],
        size: 4 + Math.random() * 5,
      })
    }

    let W = 0
    let H = 0
    let cx = 0
    let cy = 0
    let dpr = 1
    let raf = 0
    let pageVisible = document.visibilityState !== 'hidden'
    let inView = true
    const entryStart = performance.now()

    const resize = () => {
      W = container.clientWidth
      H = container.clientHeight
      cx = W / 2
      cy = H / 2
      const dprCap = W < 600 ? 1.5 : 2
      dpr = Math.min(window.devicePixelRatio || 1, dprCap)
      canvas.width = Math.max(1, Math.floor(W * dpr))
      canvas.height = Math.max(1, Math.floor(H * dpr))
      canvas.style.width = `${W}px`
      canvas.style.height = `${H}px`
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0)
    }

    const hexToRgb = (hex: string): [number, number, number] => {
      const m = hex.replace('#', '')
      const n = Number.parseInt(m, 16)
      return [(n >> 16) & 255, (n >> 8) & 255, n & 255]
    }

    const draw = () => {
      const now = performance.now()
      const isDarkNow = isDarkRef.current
      const p = PERSPECTIVE

      const entryElapsed = now - entryStart
      const entryT = Math.min(1, entryElapsed / ENTRY_MS)
      const entryInv = 1 - entryT
      const entryBoost = 1 + entryInv * entryInv * (ENTRY_BOOST - 1)
      const entryTailBoost = 1 + entryInv * entryInv * 3
      const entryFade = Math.min(1, entryT * 1.4)

      const mx = mouseRef.current.x
      const my = mouseRef.current.y
      const pr = parallaxRef.current
      const tgtX = W ? Math.max(-1, Math.min(1, mx / (W / 2))) * PARALLAX_X : 0
      const tgtY = H ? Math.max(-1, Math.min(1, my / (H / 2))) * PARALLAX_Y : 0
      pr.x += (tgtX - pr.x) * PARALLAX_LERP
      pr.y += (tgtY - pr.y) * PARALLAX_LERP

      const starBase = isDarkNow ? [255, 255, 255] : [40, 20, 70]
      const fogColor = isDarkNow ? [6, 10, 24] : [190, 150, 215]
      const starAlphaMul = isDarkNow ? 0.88 : 0.95

      ctx.clearRect(0, 0, W, H)

      for (let i = 0; i < entities.length; i++) {
        const item = entities[i]
        item.angle += item.rotSpeed
        const baseX = Math.cos(item.angle) * item.radius
        const baseY = Math.sin(item.angle) * item.radius

        item.z += item.speed * entryBoost
        if (item.z > p) {
          item.z -= MAX_DEPTH + p
          item.angle = Math.random() * Math.PI * 2
        }

        const scale = p / Math.max(1, p - item.z)
        const projX = baseX * scale
        const projY = baseY * scale
        const screenX = cx + pr.x + projX
        const screenY = cy + pr.y + projY

        let fogF = 0
        if (item.z < FOG_START_Z) {
          fogF = Math.min(
            1,
            (FOG_START_Z - item.z) / (FOG_START_Z - FOG_FULL_Z)
          )
        }

        let opacity = 1
        if (item.z < -4000) opacity = (item.z + 5000) / 1000
        if (item.z > p - 300) opacity = (p - item.z) / 300

        const tw =
          item.type === 'star'
            ? 0.62 + 0.38 * Math.sin(now * item.twFreq + item.twPhase)
            : 0.85 + 0.15 * Math.sin(now * item.twFreq + item.twPhase)

        opacity *= item.type === 'star' ? starAlphaMul * tw : tw
        opacity *= entryFade
        if (opacity <= 0) continue

        if (item.type === 'star') {
          const distFromCenter = Math.sqrt(projX * projX + projY * projY)
          let ux: number
          let uy: number
          if (distFromCenter < 0.001) {
            ux = Math.cos(item.angle)
            uy = Math.sin(item.angle)
          } else {
            ux = projX / distFromCenter
            uy = projY / distFromCenter
          }
          const tailLen = (1 + scale * 4) * entryTailBoost
          const fromX = screenX - ux * tailLen
          const fromY = screenY - uy * tailLen
          const r = Math.round(starBase[0] * (1 - fogF) + fogColor[0] * fogF)
          const g = Math.round(starBase[1] * (1 - fogF) + fogColor[1] * fogF)
          const b = Math.round(starBase[2] * (1 - fogF) + fogColor[2] * fogF)
          ctx.strokeStyle = `rgba(${r},${g},${b},${(opacity * 0.9).toFixed(3)})`
          ctx.lineWidth = Math.max(0.5, scale * 0.85)
          ctx.beginPath()
          ctx.moveTo(fromX, fromY)
          ctx.lineTo(screenX, screenY)
          ctx.stroke()
        } else if (item.color && item.size) {
          const ds = item.size * scale
          if (ds < 1.2) continue
          const [or, og, ob] = hexToRgb(item.color)
          const a = opacity * (1 - fogF * 0.5)
          // Soft glow without allocating a radial gradient for every orb on
          // every animation frame.
          ctx.save()
          ctx.shadowBlur = ds * 2.8
          ctx.shadowColor = `rgba(${or},${og},${ob},${(a * 0.65).toFixed(3)})`
          ctx.fillStyle = `rgba(${or},${og},${ob},${(a * 0.22).toFixed(3)})`
          ctx.beginPath()
          ctx.arc(screenX, screenY, ds * 1.25, 0, Math.PI * 2)
          ctx.fill()
          ctx.restore()
          // core
          ctx.fillStyle = `rgba(255,255,255,${(a * 0.6).toFixed(3)})`
          ctx.beginPath()
          ctx.arc(screenX, screenY, Math.max(1, ds * 0.5), 0, Math.PI * 2)
          ctx.fill()
        }
      }

      // vignette
      const vigCenterX = cx + pr.x * 0.4
      const vigCenterY = cy + pr.y * 0.4
      const vigInner = Math.min(W, H) * 0.22
      const vigOuter = Math.hypot(W, H) / 2
      const vig = ctx.createRadialGradient(
        vigCenterX,
        vigCenterY,
        vigInner,
        vigCenterX,
        vigCenterY,
        vigOuter
      )
      if (isDarkNow) {
        vig.addColorStop(0, 'rgba(0,0,0,0)')
        vig.addColorStop(0.55, 'rgba(2,4,14,0.28)')
        vig.addColorStop(1, 'rgba(0,0,0,0.7)')
      } else {
        vig.addColorStop(0, 'rgba(200,170,230,0)')
        vig.addColorStop(0.55, 'rgba(150,110,180,0.18)')
        vig.addColorStop(1, 'rgba(80,40,110,0.42)')
      }
      ctx.fillStyle = vig
      ctx.fillRect(0, 0, W, H)
    }

    const isActive = () => pageVisible && inView

    const stop = () => {
      if (!raf) return
      cancelAnimationFrame(raf)
      raf = 0
    }

    const tick = () => {
      raf = 0
      if (!isActive()) return
      draw()
      raf = requestAnimationFrame(tick)
    }

    const start = () => {
      if (raf || !isActive()) return
      raf = requestAnimationFrame(tick)
    }

    const handleVisibility = () => {
      pageVisible = document.visibilityState !== 'hidden'
      if (isActive()) start()
      else stop()
    }

    const handleMouseMove = (event: MouseEvent) => {
      const rect = container.getBoundingClientRect()
      if (!rect.width) return
      mouseRef.current = {
        x: event.clientX - rect.left - rect.width / 2,
        y: event.clientY - rect.top - rect.height / 2,
      }
    }

    resize()
    const observer = new IntersectionObserver(
      ([entry]) => {
        inView = !!entry?.isIntersecting
        if (isActive()) start()
        else stop()
      },
      { rootMargin: '160px 0px', threshold: 0.01 }
    )
    observer.observe(container)
    start()
    window.addEventListener('resize', resize)
    document.addEventListener('visibilitychange', handleVisibility)
    window.addEventListener('mousemove', handleMouseMove, { passive: true })

    return () => {
      stop()
      observer.disconnect()
      window.removeEventListener('resize', resize)
      document.removeEventListener('visibilitychange', handleVisibility)
      window.removeEventListener('mousemove', handleMouseMove)
    }
  }, [reduced])

  // Static gradient background (always rendered; canvas overlays it when motion is allowed)
  const gradient = isDark
    ? 'radial-gradient(circle at center, #0a0f1a 0%, #050811 55%, #020308 100%)'
    : 'radial-gradient(circle at center, #fff7fb 0%, #f7e7f1 48%, #e4d8f0 100%)'

  return (
    <div
      ref={containerRef}
      aria-hidden='true'
      className={cn(
        'pointer-events-none absolute inset-0 overflow-hidden',
        'transition-[background] duration-700 ease-out',
        props.className
      )}
      style={{ background: gradient }}
    >
      {!reduced && (
        <canvas
          ref={canvasRef}
          className='absolute inset-0 block h-full w-full'
        />
      )}
      {/* soft top/bottom fades to blend with PublicLayout bg */}
      <div
        className='absolute inset-x-0 top-0 h-24'
        style={{
          background: isDark
            ? 'linear-gradient(to bottom, rgba(2,3,8,0.6), transparent)'
            : 'linear-gradient(to bottom, rgba(255,247,251,0.5), transparent)',
        }}
      />
      <div
        className='absolute inset-x-0 bottom-0 h-40'
        style={{
          background: isDark
            ? 'linear-gradient(to top, var(--background) 0%, rgba(2,3,8,0) 100%)'
            : 'linear-gradient(to top, var(--background) 0%, rgba(255,247,251,0) 100%)',
        }}
      />
    </div>
  )
}
