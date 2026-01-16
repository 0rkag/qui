/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { memo, useEffect, useMemo, useRef } from "react"

interface PieceBarProps {
  pieceStates: number[] | undefined
  isLoading?: boolean
  isComplete?: boolean // When true and no pieceStates, show solid green bar
}

// Piece states from qBittorrent API
const PIECE_STATE = {
  NOT_DOWNLOADED: 0,
  DOWNLOADING: 1,
  DOWNLOADED: 2,
} as const

// Helper to get computed CSS variable value and convert to usable color
function getThemeColor(cssVar: string, fallback: string): string {
  if (typeof document === "undefined") return fallback

  const value = getComputedStyle(document.documentElement).getPropertyValue(cssVar).trim()
  if (!value) return fallback

  // Create a temporary element to compute the final color value
  const temp = document.createElement("div")
  temp.style.color = value
  temp.style.display = "none"
  document.body.appendChild(temp)
  const computed = getComputedStyle(temp).color
  document.body.removeChild(temp)

  return computed || fallback
}

export const PieceBar = memo(function PieceBar({
  pieceStates,
  isLoading = false,
  isComplete = false,
}: PieceBarProps) {
  const canvasRef = useRef<HTMLCanvasElement>(null)
  const containerRef = useRef<HTMLDivElement>(null)

  // Calculate progress percentage for accessibility
  const progressInfo = useMemo(() => {
    if (!pieceStates || pieceStates.length === 0) {
      return { percentage: isComplete ? 100 : 0, downloaded: 0, total: 0 }
    }
    const downloaded = pieceStates.filter(s => Number(s) === PIECE_STATE.DOWNLOADED).length
    const downloading = pieceStates.filter(s => Number(s) === PIECE_STATE.DOWNLOADING).length
    const percentage = Math.round((downloaded / pieceStates.length) * 100)
    return { percentage, downloaded, downloading, total: pieceStates.length }
  }, [pieceStates, isComplete])

  useEffect(() => {
    if (!canvasRef.current || !containerRef.current) return
    if (!pieceStates || pieceStates.length === 0) return

    const canvas = canvasRef.current
    const container = containerRef.current
    const ctx = canvas.getContext("2d")
    if (!ctx) return

    // Get theme-aware colors (uses CSS variables)
    const colors = {
      downloaded: getThemeColor("--chart-3", "rgb(34, 197, 94)"),    // Green
      downloading: getThemeColor("--chart-4", "rgb(234, 179, 8)"),   // Yellow
      notDownloaded: getThemeColor("--muted", "rgb(63, 63, 70)"),    // Muted background
      background: getThemeColor("--border", "rgb(39, 39, 42)"),      // Border color
    }

    let rafId: number | undefined

    const draw = () => {
      // Get container width for responsive sizing
      const containerWidth = container.clientWidth
      if (containerWidth <= 0) return

      const barHeight = 12

      // Set canvas size (use device pixel ratio for sharp rendering)
      const dpr = window.devicePixelRatio || 1
      canvas.width = containerWidth * dpr
      canvas.height = barHeight * dpr
      canvas.style.width = `${containerWidth}px`
      canvas.style.height = `${barHeight}px`
      ctx.setTransform(1, 0, 0, 1, 0, 0)
      ctx.scale(dpr, dpr)

      // Clear canvas with background
      ctx.fillStyle = colors.background
      ctx.beginPath()
      ctx.roundRect(0, 0, containerWidth, barHeight, 4)
      ctx.fill()

      // Calculate bucket size - aggregate pieces when there are more pieces than pixels
      const bucketSize = Math.max(1, Math.ceil(pieceStates.length / containerWidth))
      const numBuckets = Math.ceil(pieceStates.length / bucketSize)
      if (numBuckets <= 0) return

      // Draw each bucket
      const pieceWidth = containerWidth / numBuckets

      for (let i = 0; i < numBuckets; i++) {
        const startIdx = i * bucketSize
        const endIdx = Math.min(startIdx + bucketSize, pieceStates.length)
        const bucket = pieceStates.slice(startIdx, endIdx)

        // Determine dominant state in bucket
        // Priority: downloading (show activity) > downloaded > not-downloaded
        let hasDownloading = false
        let hasNotDownloaded = false
        let hasDownloaded = false

        for (const state of bucket) {
          const stateNum = Number(state)
          if (stateNum === PIECE_STATE.DOWNLOADING) hasDownloading = true
          else if (stateNum === PIECE_STATE.NOT_DOWNLOADED) hasNotDownloaded = true
          else if (stateNum === PIECE_STATE.DOWNLOADED) hasDownloaded = true
        }

        // Choose color based on priority
        let color: string
        if (hasDownloading) {
          color = colors.downloading
        } else if (hasDownloaded && !hasNotDownloaded) {
          color = colors.downloaded
        } else if (hasNotDownloaded && !hasDownloaded) {
          color = colors.notDownloaded
        } else if (hasDownloaded) {
          // Mixed bucket with both downloaded and not-downloaded - show downloaded
          color = colors.downloaded
        } else {
          color = colors.notDownloaded
        }

        // Draw bucket segment
        const x = i * pieceWidth
        ctx.fillStyle = color
        ctx.fillRect(x, 0, pieceWidth + 0.5, barHeight) // +0.5 to avoid gaps
      }

      // Apply rounded corners mask by redrawing with clip
      ctx.globalCompositeOperation = "destination-in"
      ctx.beginPath()
      ctx.roundRect(0, 0, containerWidth, barHeight, 4)
      ctx.fill()
      ctx.globalCompositeOperation = "source-over"
    }

    const scheduleDraw = () => {
      if (rafId != null) cancelAnimationFrame(rafId)
      rafId = requestAnimationFrame(draw)
    }

    scheduleDraw()

    const view = container.ownerDocument.defaultView

    const ResizeObserverImpl = (globalThis as unknown as { ResizeObserver?: typeof ResizeObserver }).ResizeObserver
    let observer: ResizeObserver | undefined
    if (ResizeObserverImpl) {
      observer = new ResizeObserverImpl(() => scheduleDraw())
      observer.observe(container)
    } else {
      view?.addEventListener("resize", scheduleDraw)
    }

    // Listen for theme changes to redraw with new colors
    const handleThemeChange = () => scheduleDraw()
    window.addEventListener("themechange", handleThemeChange)

    // Also listen for class changes on documentElement (dark mode toggle)
    const classObserver = new MutationObserver(() => scheduleDraw())
    classObserver.observe(document.documentElement, { attributes: true, attributeFilter: ["class"] })

    return () => {
      if (rafId != null) cancelAnimationFrame(rafId)
      observer?.disconnect()
      classObserver.disconnect()
      view?.removeEventListener("resize", scheduleDraw)
      window.removeEventListener("themechange", handleThemeChange)
    }
  }, [pieceStates])

  // Show solid green bar for completed torrents (no need to fetch piece states)
  if (isComplete && (!pieceStates || pieceStates.length === 0)) {
    return (
      <div
        role="progressbar"
        aria-valuenow={100}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-label="Download complete: 100%"
        className="w-full h-3 rounded bg-chart-3"
      />
    )
  }

  // Show loading state or empty bar
  if (isLoading || !pieceStates || pieceStates.length === 0) {
    return (
      <div
        ref={containerRef}
        role="progressbar"
        aria-valuenow={0}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-label="Loading piece information..."
        aria-busy={isLoading}
        className="w-full h-3 rounded bg-muted motion-safe:animate-pulse"
      />
    )
  }

  const ariaLabel = progressInfo.downloading && progressInfo.downloading > 0
    ? `Download progress: ${progressInfo.percentage}% complete, ${progressInfo.downloading} pieces downloading`
    : `Download progress: ${progressInfo.percentage}% complete (${progressInfo.downloaded} of ${progressInfo.total} pieces)`

  return (
    <div
      ref={containerRef}
      className="w-full"
      role="progressbar"
      aria-valuenow={progressInfo.percentage}
      aria-valuemin={0}
      aria-valuemax={100}
      aria-label={ariaLabel}
    >
      <canvas
        ref={canvasRef}
        className="w-full rounded"
        aria-hidden="true"
      />
    </div>
  )
})
