import { useEffect, useRef } from 'react'
import { AudioEngine } from '@/lib/audio/engine'
import type { VisualizerMode } from '@/lib/audio/types'

interface VisualizerCanvasProps {
  mode: VisualizerMode
  className?: string
  barCount?: number
  height?: number
  showPeakWarning?: boolean
}

export function VisualizerCanvas({
  mode,
  className = '',
  barCount = 36,
  height = 64,
  showPeakWarning = true,
}: VisualizerCanvasProps) {
  const canvasRef = useRef<HTMLCanvasElement | null>(null)
  const isNearPeakRef = useRef(false)
  const peakBadgeRef = useRef<HTMLSpanElement | null>(null)

  useEffect(() => {
    if (mode === 'off') return

    const canvas = canvasRef.current
    if (!canvas) return
    const ctx = canvas.getContext('2d')
    if (!ctx) return

    const engine = AudioEngine.getInstance()
    let animationFrameId: number

    // Data buffers
    const freqData = new Uint8Array(256)
    const timeData = new Uint8Array(256)

    const reducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches

    function render() {
      if (document.hidden || reducedMotion) {
        animationFrameId = requestAnimationFrame(render)
        return
      }

      const dpr = window.devicePixelRatio || 1
      const displayWidth = canvas!.clientWidth || (canvas!.parentElement?.clientWidth ?? 300)
      if (canvas!.width !== displayWidth * dpr || canvas!.height !== height * dpr) {
        canvas!.width = displayWidth * dpr
        canvas!.height = height * dpr
        ctx!.setTransform(1, 0, 0, 1, 0, 0)
        ctx!.scale(dpr, dpr)
      }

      const width = displayWidth
      const h = height
      ctx!.clearRect(0, 0, width, h)

      // Peak detection update
      const nearPeak = engine.isNearPeak()
      if (isNearPeakRef.current !== nearPeak) {
        isNearPeakRef.current = nearPeak
        if (peakBadgeRef.current) {
          peakBadgeRef.current.style.opacity = nearPeak ? '1' : '0'
        }
      }

      if (mode === 'spectrum') {
        engine.getFrequencyData(freqData)
        const bars = barCount
        const gap = 3
        const barWidth = Math.max(2, (width - (bars - 1) * gap) / bars)

        for (let i = 0; i < bars; i++) {
          const binIndex = Math.min(
            freqData.length - 1,
            Math.floor(Math.pow(i / bars, 1.5) * (freqData.length * 0.7)),
          )
          const value = freqData[binIndex] || 0
          const percent = value / 255
          const barHeight = Math.max(2, percent * (h - 6))
          const x = i * (barWidth + gap)
          const y = h - barHeight

          const gradient = ctx!.createLinearGradient(0, h, 0, 0)
          gradient.addColorStop(0, '#ce3463')
          gradient.addColorStop(0.7, '#e11d48')
          gradient.addColorStop(1, '#fda4af')

          ctx!.fillStyle = gradient
          ctx!.beginPath()
          ctx!.roundRect(x, y, barWidth, barHeight, [2, 2, 0, 0])
          ctx!.fill()
        }
      } else if (mode === 'waveform') {
        engine.getTimeDomainData(timeData)
        ctx!.lineWidth = 1.5
        ctx!.strokeStyle = '#ce3463'
        ctx!.shadowColor = 'rgba(206, 52, 99, 0.35)'
        ctx!.shadowBlur = 4
        ctx!.beginPath()

        const sliceWidth = width / timeData.length
        let x = 0

        for (let i = 0; i < timeData.length; i++) {
          const val = timeData[i] ?? 128
          const v = val / 128.0
          const y = (v * h) / 2

          if (i === 0) {
            ctx!.moveTo(x, y)
          } else {
            ctx!.lineTo(x, y)
          }

          x += sliceWidth
        }

        ctx!.lineTo(width, h / 2)
        ctx!.stroke()
        ctx!.shadowBlur = 0
      }

      animationFrameId = requestAnimationFrame(render)
    }

    animationFrameId = requestAnimationFrame(render)

    return () => {
      cancelAnimationFrame(animationFrameId)
    }
  }, [mode, barCount, height])

  if (mode === 'off') {
    return null
  }

  return (
    <div className={`relative w-full overflow-hidden ${className}`}>
      <canvas
        ref={canvasRef}
        className="w-full block"
        style={{ height: `${height}px` }}
      />
      {showPeakWarning && (
        <span
          ref={peakBadgeRef}
          style={{ opacity: 0 }}
          className="pointer-events-none absolute top-1 right-1 rounded bg-amber-500/80 px-1 py-0.2 text-[9px] font-medium text-black backdrop-blur-sm transition-opacity duration-200"
          title="Digital Peak / Headroom Warnung"
        >
          PEAK
        </span>
      )}
    </div>
  )
}
