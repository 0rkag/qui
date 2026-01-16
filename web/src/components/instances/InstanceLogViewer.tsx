/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { LogViewer, LogEntryRow, type LogLevel } from "@/components/ui/log-viewer"
import { api } from "@/lib/api"
import { useCallback, useMemo } from "react"

type QBLogLevel = "normal" | "info" | "warning" | "critical"

interface QBLogEntry {
  id: number
  message: string
  timestamp: string
  type: number
  level: QBLogLevel
}

const LOG_LEVEL_CONFIG: LogLevel[] = [
  { id: "normal", label: "normal", badgeClass: "bg-muted text-muted-foreground", textClass: "text-foreground" },
  { id: "info", label: "info", badgeClass: "bg-blue-500/20 text-blue-400", textClass: "text-blue-400" },
  { id: "warning", label: "warning", badgeClass: "bg-yellow-500/20 text-yellow-400", textClass: "text-yellow-400" },
  { id: "critical", label: "critical", badgeClass: "bg-red-500/20 text-red-400", textClass: "text-red-400" },
]

const LEVEL_MAP = new Map(LOG_LEVEL_CONFIG.map((l) => [l.id, l]))

function formatTimestamp(isoTime: string): string {
  if (!isoTime) return ""
  try {
    const date = new Date(isoTime)
    return date.toLocaleTimeString("en-US", {
      hour12: false,
      hour: "2-digit",
      minute: "2-digit",
      second: "2-digit",
    })
  } catch {
    return ""
  }
}

function parseEntry(data: string, index: number): QBLogEntry | null {
  try {
    const parsed = JSON.parse(data) as QBLogEntry
    return { ...parsed, id: parsed.id ?? index }
  } catch {
    return null
  }
}

interface InstanceLogViewerProps {
  instanceId: number
}

export function InstanceLogViewer({ instanceId }: InstanceLogViewerProps) {
  const streamUrl = useMemo(
    () => api.getInstanceLogStreamUrl(instanceId, 500),
    [instanceId]
  )

  const renderEntry = useCallback((entry: QBLogEntry) => {
    const levelConfig = LEVEL_MAP.get(entry.level)
    return (
      <LogEntryRow
        time={formatTimestamp(entry.timestamp)}
        level={entry.level}
        levelBadgeClass={levelConfig?.badgeClass ?? ""}
        levelTextClass={levelConfig?.textClass ?? ""}
        message={entry.message}
      />
    )
  }, [])

  return (
    <LogViewer<QBLogEntry>
      streamUrl={streamUrl}
      parseEntry={parseEntry}
      getEntryLevel={(e) => e.level}
      getSearchableText={(e) => e.message}
      renderEntry={renderEntry}
      levels={LOG_LEVEL_CONFIG}
      softCap={500}
      hardCap={5000}
    />
  )
}
