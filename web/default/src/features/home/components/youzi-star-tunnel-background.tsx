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
 * Canvas star-tunnel backdrop for the Youziapi landing page. Both stars and
 * model "chips" travel down the same hand-rolled perspective tunnel toward
 * the camera, with entry boost, mouse parallax, mouse repulsion on near
 * model chips, and click-driven shockwaves. Honors `prefers-reduced-motion`
 * (entity count halved, motion throttled, parallax/forces/shockwaves off).
 *
 * Mechanism ported from the nashiyard star-tunnel reference: single Canvas2D,
 * pinhole projection, pre-baked chip sprites drawn via drawImage.
 *
 * Theme is sourced from the existing ThemeProvider so the canvas tracks the
 * global light/dark switch with no separate state.
 */
interface YouziStarTunnelBackgroundProps {
  className?: string
}

// --- Model presets (deduplicated symbol/color pairs from the legacy
// FLOATING_NODES list). Each pair bakes one reusable chip sprite. ---
interface ModelPreset {
  key: string
  symbol: string
  color: string
}

const MODEL_PRESETS: ModelPreset[] = [
  { key: 'yuzu|#10a37f', symbol: '🍊', color: '#10a37f' },
  { key: 'yuzu|#d97757', symbol: '🍊', color: '#d97757' },
  { key: 'yuzu|#8ab4f8', symbol: '🍊', color: '#8ab4f8' },
  { key: 'yuzu|#1a73e8', symbol: '🍊', color: '#1a73e8' },
  { key: 'yuzu|#4d6bfe', symbol: '🍊', color: '#4d6bfe' },
  { key: 'yuzu|#6657fa', symbol: '🍊', color: '#6657fa' },
  { key: 'yuzu|#f26e24', symbol: '🍊', color: '#f26e24' },
  { key: 'yuzu|#00a4ef', symbol: '🍊', color: '#00a4ef' },
  { key: 'yuzu|#cbd5e1', symbol: '🍊', color: '#cbd5e1' },
  { key: 'yuzu|#a78bfa', symbol: '🍊', color: '#a78bfa' },
  { key: 'yuzu|#00cc99', symbol: '🍊', color: '#00cc99' },
  { key: 'yuzu|#ff9db5', symbol: '🍊', color: '#ff9db5' },
  { key: 'yuzu|#94a3b8', symbol: '🍊', color: '#94a3b8' },
  { key: 'yuzu|#2932e1', symbol: '🍊', color: '#2932e1' },
  { key: 'yuzu|#7a52ff', symbol: '🍊', color: '#7a52ff' },
]

// --- Chip baking (off-screen sprite per preset) ---
const CHIP_BAKE_SIZE = 160
const CHIP_RADIUS = 38
const CHIP_SYMBOL_PX = 48
const CHIP_DISPLAY_SIZE = 100

// --- Tunnel constants (aligned with nashiyard reference) ---
const PERSPECTIVE = 800
const MAX_DEPTH = 5000
const FOG_START_Z = -800
const FOG_FULL_Z = -4200

const DESKTOP_MODELS = 140
const DESKTOP_STARS = 500
const MOBILE_MODELS = 80
const MOBILE_STARS = 280

const ENTRY_MS = 1500
const ENTRY_BOOST = 6
const ENTRY_TAIL_BOOST = 4.5

const PARALLAX_X = 90
const PARALLAX_Y = 60
const PARALLAX_LERP = 0.06

const MOUSE_FORCE_RADIUS = 400
const MOUSE_FORCE_STRENGTH = 250
const MOUSE_FORCE_LERP = 0.12

const SHOCKWAVE_MS = 900
const SHOCKWAVE_SPEED = 1100
const SHOCKWAVE_FORCE = 320
const SHOCKWAVE_RING_WIDTH = 140
const MAX_SHOCKWAVES = 6

const BLOOM_SCALE = 0.5
const BLOOM_BLUR_PX = 14
const BLOOM_STRENGTH = 0.65

// Clicks landing on these elements (or their ancestors) do not spawn
// shockwaves. The background's own canvas/fade layers are not interactive,
// so this is defensive against future interactive children.
const INTERACTIVE_SELECTOR =
  'button, a, input, textarea, select, [role="button"], [role="dialog"]'

type EntityType = 'model' | 'star'

interface TunnelEntity {
  type: EntityType
  chipKey?: string
  angle: number
  radius: number
  z: number
  speed: number
  rotSpeed: number
  offsetX: number
  offsetY: number
  twPhase: number
  twFreq: number
  // Per-frame cached projection state (not input).
  projX: number
  projY: number
  projScale: number
  screenX: number
  screenY: number
  fogF: number
  opacity: number
}

interface Shockwave {
  xRel: number
  yRel: number
  startTime: number
}

function prefersReducedMotion(): boolean {
  if (typeof window === 'undefined') return false
  return window.matchMedia('(prefers-reduced-motion: reduce)').matches
}

/**
 * Bake a 160×160 off-screen chip sprite: a colored disc with a soft glow
 * shadow, a 1.5px inner highlight ring, a thin outer dark ring, and the
 * unicode symbol centered in white. Runtime only calls drawImage on the
 * returned canvas — no per-frame gradient/text allocations.
 */
function bakeChip(symbol: string, color: string): HTMLCanvasElement {
  const canvas = document.createElement('canvas')
  canvas.width = CHIP_BAKE_SIZE
  canvas.height = CHIP_BAKE_SIZE
  const c = canvas.getContext('2d')
  if (!c) return canvas
  const mid = CHIP_BAKE_SIZE / 2

  // Glow disc: shadowBlur gives the halo, fill gives the body.
  c.shadowColor = color
  c.shadowBlur = 34
  c.fillStyle = color
  c.beginPath()
  c.arc(mid, mid, CHIP_RADIUS, 0, Math.PI * 2)
  c.fill()

  // Inner highlight ring.
  c.shadowBlur = 0
  c.strokeStyle = 'rgba(255,255,255,0.4)'
  c.lineWidth = 1.5
  c.beginPath()
  c.arc(mid, mid, CHIP_RADIUS, 0, Math.PI * 2)
  c.stroke()

  // Outer dark ring (separates chip from bright backgrounds).
  c.strokeStyle = 'rgba(0,0,0,0.18)'
  c.lineWidth = 1
  c.beginPath()
  c.arc(mid, mid, CHIP_RADIUS + 1, 0, Math.PI * 2)
  c.stroke()

  // Unicode symbol centered. Font stack covers symbol/math blocks; emoji
  // blocks are intentionally avoided to keep the white fill applied.
  c.fillStyle = '#ffffff'
  c.font = `${CHIP_SYMBOL_PX}px ui-sans-serif, system-ui, -apple-system, "Segoe UI Symbol", "DejaVu Sans", sans-serif`
  c.textAlign = 'center'
  c.textBaseline = 'middle'
  c.fillText(symbol, mid, mid + 2)

  return canvas
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
  const shockwavesRef = useRef<Shockwave[]>([])
  const isDarkRef = useRef(isDark)
  const reducedRef = useRef(false)
  const [reduced, setReduced] = useState<boolean>(() => prefersReducedMotion())

  useEffect(() => {
    isDarkRef.current = isDark
  }, [isDark])

  useEffect(() => {
    reducedRef.current = reduced
  }, [reduced])

  useEffect(() => {
    if (typeof window === 'undefined') return undefined
    const mq = window.matchMedia('(prefers-reduced-motion: reduce)')
    const handler = () => setReduced(mq.matches)
    mq.addEventListener('change', handler)
    return () => mq.removeEventListener('change', handler)
  }, [])

  useEffect(() => {
    const canvas = canvasRef.current
    const container = containerRef.current
    if (!canvas || !container) return undefined
    const ctx = canvas.getContext('2d')
    if (!ctx) return undefined

    // Off-screen bloom buffer (desktop only). Allocated once, reused per frame.
    const bloomCanvas = document.createElement('canvas')
    const bloomCtx = bloomCanvas.getContext('2d')
    if (!bloomCtx) return undefined

    // Bake chip sprites synchronously — unicode glyphs need no async loading.
    const chips: Record<string, HTMLCanvasElement> = {}
    for (const preset of MODEL_PRESETS) {
      chips[preset.key] = bakeChip(preset.symbol, preset.color)
    }

    const isMobile =
      typeof window !== 'undefined' &&
      window.matchMedia('(max-width: 600px)').matches
    // Reduced motion: halve entity counts (uses mobile/2 baseline like the
    // nashiyard reference) so the tunnel still drifts, just far slower.
    const modelCount = Math.floor(
      (isMobile ? MOBILE_MODELS : DESKTOP_MODELS) / (reduced ? 2 : 1)
    )
    const starCount = Math.floor(
      (isMobile ? MOBILE_STARS : DESKTOP_STARS) / (reduced ? 2 : 1)
    )

    const entities: TunnelEntity[] = []
    for (let i = 0; i < modelCount; i++) {
      const preset = MODEL_PRESETS[i % MODEL_PRESETS.length]
      entities.push({
        type: 'model',
        chipKey: preset.key,
        angle: Math.random() * Math.PI * 2,
        radius: 300 + Math.random() * 1800,
        z: -Math.random() * MAX_DEPTH,
        speed: 6 + Math.random() * 8,
        rotSpeed: (Math.random() - 0.5) * 0.0015,
        offsetX: 0,
        offsetY: 0,
        twPhase: Math.random() * Math.PI * 2,
        twFreq: 0.0003 + Math.random() * 0.0006,
        projX: 0,
        projY: 0,
        projScale: 0,
        screenX: 0,
        screenY: 0,
        fogF: 0,
        opacity: 0,
      })
    }
    for (let i = 0; i < starCount; i++) {
      entities.push({
        type: 'star',
        angle: Math.random() * Math.PI * 2,
        radius: 80 + Math.random() * 2800,
        z: -Math.random() * MAX_DEPTH,
        speed: 10 + Math.random() * 24,
        rotSpeed: (Math.random() - 0.5) * 0.001,
        offsetX: 0,
        offsetY: 0,
        twPhase: Math.random() * Math.PI * 2,
        twFreq: 0.0009 + Math.random() * 0.0022,
        projX: 0,
        projY: 0,
        projScale: 0,
        screenX: 0,
        screenY: 0,
        fogF: 0,
        opacity: 0,
      })
    }

    let W = 0
    let H = 0
    let cx = 0
    let cy = 0
    let dpr = 1
    let bloomW = 0
    let bloomH = 0
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

      bloomW = Math.max(2, Math.floor(W * BLOOM_SCALE))
      bloomH = Math.max(2, Math.floor(H * BLOOM_SCALE))
      bloomCanvas.width = bloomW
      bloomCanvas.height = bloomH
    }

    const draw = () => {
      const now = performance.now()
      const isDarkNow = isDarkRef.current
      const p = PERSPECTIVE

      const entryElapsed = now - entryStart
      const entryT = Math.min(1, entryElapsed / ENTRY_MS)
      const entryInv = 1 - entryT
      // Reduced motion: no entry boost (constant 1), so the tunnel eases in
      // at its already-throttled crawl speed instead of surging.
      const entryBoost = reduced
        ? 1
        : 1 + entryInv * entryInv * (ENTRY_BOOST - 1)
      const entryTailBoost = reduced
        ? 1
        : 1 + entryInv * entryInv * ENTRY_TAIL_BOOST
      const entryFade = Math.min(1, entryT * 1.4)

      const mx = mouseRef.current.x
      const my = mouseRef.current.y
      const pr = parallaxRef.current
      // Reduced motion: parallax disabled (target stays 0, lerp settles).
      const tgtX =
        reduced || !W ? 0 : Math.max(-1, Math.min(1, mx / (W / 2))) * PARALLAX_X
      const tgtY =
        reduced || !H ? 0 : Math.max(-1, Math.min(1, my / (H / 2))) * PARALLAX_Y
      pr.x += (tgtX - pr.x) * PARALLAX_LERP
      pr.y += (tgtY - pr.y) * PARALLAX_LERP

      // Reap expired shockwaves.
      const sws = shockwavesRef.current
      for (let i = sws.length - 1; i >= 0; i--) {
        if (now - sws[i].startTime > SHOCKWAVE_MS) sws.splice(i, 1)
      }

      const starColorBase = isDarkNow ? [255, 255, 255] : [40, 20, 70]
      const fogColor = isDarkNow ? [6, 10, 24] : [190, 150, 215]
      const starAlphaMul = isDarkNow ? 0.88 : 0.95

      // First pass: advance physics + projection, cache per-entity draw state.
      for (let i = 0; i < entities.length; i++) {
        const item = entities[i]
        item.angle += reduced ? item.rotSpeed * 0.15 : item.rotSpeed
        const baseX = Math.cos(item.angle) * item.radius
        const baseY = Math.sin(item.angle) * item.radius

        item.z += item.speed * entryBoost * (reduced ? 0.18 : 1)
        if (item.z > p) {
          item.z -= MAX_DEPTH + p
          item.angle = Math.random() * Math.PI * 2
        }

        const scale = p / Math.max(1, p - item.z)
        const projX = baseX * scale
        const projY = baseY * scale

        // Mouse repulsion + shockwave force (model chips near the camera only).
        // Disabled under reduced motion.
        let tox = 0
        let toy = 0
        if (!reduced && item.type === 'model' && item.z > -2500) {
          const dx = mx - projX
          const dy = my - projY
          const dist = Math.sqrt(dx * dx + dy * dy) || 1
          if (dist < MOUSE_FORCE_RADIUS) {
            const force =
              ((MOUSE_FORCE_RADIUS - dist) / MOUSE_FORCE_RADIUS) ** 2
            tox = -(dx / dist) * force * MOUSE_FORCE_STRENGTH
            toy = -(dy / dist) * force * MOUSE_FORCE_STRENGTH
          }
          for (let s = 0; s < sws.length; s++) {
            const sw = sws[s]
            const age = now - sw.startTime
            const ringR = (age / 1000) * SHOCKWAVE_SPEED
            const sdx = projX - sw.xRel
            const sdy = projY - sw.yRel
            const sDist = Math.sqrt(sdx * sdx + sdy * sdy) || 1
            const delta = Math.abs(sDist - ringR)
            if (delta < SHOCKWAVE_RING_WIDTH) {
              const fade = 1 - age / SHOCKWAVE_MS
              const strength = (1 - delta / SHOCKWAVE_RING_WIDTH) * fade
              tox += (sdx / sDist) * strength * SHOCKWAVE_FORCE
              toy += (sdy / sDist) * strength * SHOCKWAVE_FORCE
            }
          }
        }
        item.offsetX += (tox - item.offsetX) * MOUSE_FORCE_LERP
        item.offsetY += (toy - item.offsetY) * MOUSE_FORCE_LERP

        item.projX = projX + item.offsetX
        item.projY = projY + item.offsetY
        item.projScale = scale
        item.screenX = cx + pr.x + item.projX
        item.screenY = cy + pr.y + item.projY

        let fogF = 0
        if (item.z < FOG_START_Z) {
          fogF = Math.min(
            1,
            (FOG_START_Z - item.z) / (FOG_START_Z - FOG_FULL_Z)
          )
        }
        item.fogF = fogF

        let opacity = 1
        if (item.z < -4000) opacity = (item.z + 5000) / 1000
        if (item.z > p - 300) opacity = (p - item.z) / 300

        // Reduced motion: freeze twinkle drift at a constant mid-brightness.
        const twAmp = item.type === 'star' ? 0.38 : 0.12
        const twBase = item.type === 'star' ? 0.62 : 0.88
        const tw = reduced
          ? twBase + twAmp / 2
          : twBase + twAmp * Math.sin(now * item.twFreq + item.twPhase)

        opacity *= item.type === 'star' ? starAlphaMul * tw : tw
        opacity *= entryFade
        item.opacity = Math.max(0, Math.min(1, opacity))
      }

      // Painter's order: far entities first.
      entities.sort((a, b) => a.z - b.z)

      ctx.clearRect(0, 0, W, H)

      for (let i = 0; i < entities.length; i++) {
        const item = entities[i]
        if (item.opacity <= 0) continue
        const fogF = item.fogF

        if (item.type === 'star') {
          const distFromCenter = Math.sqrt(
            item.projX * item.projX + item.projY * item.projY
          )
          let ux: number
          let uy: number
          if (distFromCenter < 0.001) {
            ux = Math.cos(item.angle)
            uy = Math.sin(item.angle)
          } else {
            ux = item.projX / distFromCenter
            uy = item.projY / distFromCenter
          }
          const tailLen = (1 + item.projScale * 4) * entryTailBoost
          const fromX = item.screenX - ux * tailLen
          const fromY = item.screenY - uy * tailLen
          const r = Math.round(
            starColorBase[0] * (1 - fogF) + fogColor[0] * fogF
          )
          const g = Math.round(
            starColorBase[1] * (1 - fogF) + fogColor[1] * fogF
          )
          const b = Math.round(
            starColorBase[2] * (1 - fogF) + fogColor[2] * fogF
          )
          ctx.strokeStyle = `rgba(${r},${g},${b},${(item.opacity * 0.9).toFixed(3)})`
          ctx.lineWidth = Math.max(0.5, item.projScale * 0.9)
          ctx.beginPath()
          ctx.moveTo(fromX, fromY)
          ctx.lineTo(item.screenX, item.screenY)
          ctx.stroke()
        } else {
          const chip = item.chipKey ? chips[item.chipKey] : undefined
          const ds = CHIP_DISPLAY_SIZE * item.projScale
          if (ds < 3) continue
          if (chip) {
            ctx.globalAlpha = item.opacity * (1 - fogF * 0.55)
            ctx.drawImage(
              chip,
              item.screenX - ds / 2,
              item.screenY - ds / 2,
              ds,
              ds
            )

            // Lens flare for near chips: bell-shaped cross burst.
            if (item.z > 120 && item.z < p - 60) {
              const prog = (item.z - 120) / (p - 180)
              const bell = Math.max(0, Math.sin(prog * Math.PI))
              const flareI = bell * 0.55 * item.opacity
              if (flareI > 0.05) {
                ctx.save()
                ctx.globalCompositeOperation = 'lighter'
                const L = ds * 0.85
                ctx.strokeStyle = `rgba(255,255,255,${flareI.toFixed(3)})`
                ctx.lineWidth = 1.1
                ctx.beginPath()
                ctx.moveTo(item.screenX - L, item.screenY)
                ctx.lineTo(item.screenX + L, item.screenY)
                ctx.moveTo(item.screenX, item.screenY - L)
                ctx.lineTo(item.screenX, item.screenY + L)
                ctx.stroke()
                const L2 = L * 0.7
                ctx.strokeStyle = `rgba(255,255,255,${(flareI * 0.45).toFixed(3)})`
                ctx.lineWidth = 0.8
                ctx.beginPath()
                ctx.moveTo(item.screenX - L2, item.screenY - L2)
                ctx.lineTo(item.screenX + L2, item.screenY + L2)
                ctx.moveTo(item.screenX + L2, item.screenY - L2)
                ctx.lineTo(item.screenX - L2, item.screenY + L2)
                ctx.stroke()
                ctx.restore()
              }
            }
          }
          ctx.globalAlpha = 1
        }
      }

      // Shockwave rings (additive blend).
      if (sws.length) {
        ctx.save()
        ctx.globalCompositeOperation = 'lighter'
        for (let i = 0; i < sws.length; i++) {
          const sw = sws[i]
          const age = now - sw.startTime
          const tRing = age / SHOCKWAVE_MS
          const ringR = (age / 1000) * SHOCKWAVE_SPEED
          const alpha = (1 - tRing) * (1 - tRing)
          const sx = cx + pr.x + sw.xRel
          const sy = cy + pr.y + sw.yRel
          ctx.strokeStyle = `rgba(255,200,232,${(alpha * 0.5).toFixed(3)})`
          ctx.lineWidth = 10 * (1 - tRing)
          ctx.beginPath()
          ctx.arc(sx, sy, ringR, 0, Math.PI * 2)
          ctx.stroke()
          ctx.strokeStyle = `rgba(255,255,255,${(alpha * 0.9).toFixed(3)})`
          ctx.lineWidth = 2
          ctx.beginPath()
          ctx.arc(sx, sy, ringR, 0, Math.PI * 2)
          ctx.stroke()
        }
        ctx.restore()
      }

      // Bloom pass: downscale → blur → composite back additively. Desktop only,
      // and skipped under reduced motion (no decorative glow effects).
      if (!reduced && W >= 600) {
        bloomCtx.setTransform(1, 0, 0, 1, 0, 0)
        bloomCtx.clearRect(0, 0, bloomW, bloomH)
        bloomCtx.filter = `blur(${BLOOM_BLUR_PX}px)`
        bloomCtx.drawImage(canvas, 0, 0, bloomW, bloomH)
        bloomCtx.filter = 'none'
        ctx.save()
        ctx.globalCompositeOperation = 'lighter'
        ctx.globalAlpha = BLOOM_STRENGTH
        ctx.drawImage(bloomCanvas, 0, 0, W, H)
        ctx.restore()
      }

      // Vignette to blend tunnel edges with the page background.
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

  // Click on empty (non-interactive) areas spawns an expanding shockwave.
  // Disabled under reduced motion (no shockwaves).
  const handleClick = (event: React.MouseEvent<HTMLDivElement>) => {
    if (reducedRef.current) return
    const target = event.target as HTMLElement | null
    if (target?.closest?.(INTERACTIVE_SELECTOR)) return
    const rect = containerRef.current?.getBoundingClientRect()
    if (!rect) return
    const sws = shockwavesRef.current
    sws.push({
      xRel: event.clientX - rect.left - rect.width / 2,
      yRel: event.clientY - rect.top - rect.height / 2,
      startTime: performance.now(),
    })
    while (sws.length > MAX_SHOCKWAVES) sws.shift()
  }

  // Static gradient sits beneath the canvas; the canvas overlays it in both
  // full and reduced-motion modes (reduced just throttles the tunnel).
  const gradient = isDark
    ? 'radial-gradient(circle at center, #0a0f1a 0%, #050811 55%, #020308 100%)'
    : 'radial-gradient(circle at center, #fff7fb 0%, #f7e7f1 48%, #e4d8f0 100%)'

  return (
    <div
      ref={containerRef}
      aria-hidden='true'
      onClick={handleClick}
      className={cn(
        'absolute inset-0 cursor-crosshair overflow-hidden',
        'transition-[background] duration-700 ease-out',
        props.className
      )}
      style={{ background: gradient }}
    >
      <canvas
        ref={canvasRef}
        className='absolute inset-0 block h-full w-full'
      />
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
