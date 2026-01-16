/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover"
import { Switch } from "@/components/ui/switch"
import { AlertCircle, ChevronDown, Filter, Search, X } from "lucide-react"
import { useCallback, useEffect, useMemo, useRef, useState } from "react"

// Buffer limits
const DEFAULT_SOFT_CAP = 1000
const DEFAULT_HARD_CAP = 10000

export interface LogLevel {
  id: string
  label: string
  badgeClass: string
  textClass: string
}

export interface BaseLogEntry {
  id: number | string
}

export interface LogViewerProps<T extends BaseLogEntry> {
  /** SSE stream URL */
  streamUrl: string
  /** Parse raw SSE data into a log entry */
  parseEntry: (data: string, index: number) => T | null
  /** Get the level ID from an entry for filtering */
  getEntryLevel: (entry: T) => string
  /** Get searchable text from an entry */
  getSearchableText: (entry: T) => string
  /** Render a single log entry row */
  renderEntry: (entry: T) => React.ReactNode
  /** Available log levels for filtering */
  levels: LogLevel[]
  /** Optional: Custom height class for the log container */
  heightClass?: string
  /** Optional: Soft cap for entries when auto-scrolling (default: 1000) */
  softCap?: number
  /** Optional: Hard cap for entries always (default: 10000) */
  hardCap?: number
  /** Optional: Callback when an entry is clicked */
  onEntryClick?: (entry: T) => void
  /** Optional: Footer content to render below the log viewer */
  footerContent?: React.ReactNode
  /** Optional: Additional toolbar content */
  toolbarContent?: React.ReactNode
  /** Optional: Entries to exclude by message/text */
  exclusions?: string[]
  /** Optional: Get the exclusion key from an entry (for muting) */
  getExclusionKey?: (entry: T) => string
}

interface ConnectionState {
  isConnected: boolean
  error: string | null
}

export function LogViewer<T extends BaseLogEntry>({
  streamUrl,
  parseEntry,
  getEntryLevel,
  getSearchableText,
  renderEntry,
  levels,
  heightClass = "h-[400px]",
  softCap = DEFAULT_SOFT_CAP,
  hardCap = DEFAULT_HARD_CAP,
  onEntryClick,
  footerContent,
  toolbarContent,
  exclusions = [],
  getExclusionKey,
}: LogViewerProps<T>) {
  const [entries, setEntries] = useState<T[]>([])
  const [autoScroll, setAutoScroll] = useState(true)
  const [connection, setConnection] = useState<ConnectionState>({
    isConnected: false,
    error: null,
  })
  const [selectedLevels, setSelectedLevels] = useState<Set<string>>(
    new Set(levels.map((l) => l.id))
  )
  const [searchQuery, setSearchQuery] = useState("")
  const [droppedWhilePaused, setDroppedWhilePaused] = useState(false)

  const scrollRef = useRef<HTMLDivElement>(null)
  const eventSourceRef = useRef<EventSource | null>(null)
  const reconnectTimeoutRef = useRef<number | null>(null)
  const entryIndexRef = useRef(0)
  const autoScrollRef = useRef(autoScroll)

  const clearReconnectTimeout = useCallback(() => {
    if (reconnectTimeoutRef.current !== null) {
      window.clearTimeout(reconnectTimeoutRef.current)
      reconnectTimeoutRef.current = null
    }
  }, [])

  // Keep ref in sync for use in event handler
  useEffect(() => {
    autoScrollRef.current = autoScroll
    if (autoScroll) {
      setDroppedWhilePaused(false)
    }
  }, [autoScroll])

  const connect = useCallback(() => {
    if (eventSourceRef.current) {
      eventSourceRef.current.close()
    }
    clearReconnectTimeout()

    setConnection({ isConnected: false, error: null })
    setEntries([])
    entryIndexRef.current = 0

    const es = new EventSource(streamUrl, { withCredentials: true })
    eventSourceRef.current = es

    es.onopen = () => {
      setConnection({ isConnected: true, error: null })
    }

    es.onmessage = (event) => {
      const parsed = parseEntry(event.data as string, entryIndexRef.current++)
      if (!parsed) return

      setEntries((prev) => {
        const newEntries = [...prev, parsed]
        // Soft cap when auto-scroll ON
        if (autoScrollRef.current && newEntries.length > softCap) {
          return newEntries.slice(-softCap)
        }
        // Hard cap always
        if (newEntries.length > hardCap) {
          setDroppedWhilePaused(true)
          return newEntries.slice(-hardCap)
        }
        return newEntries
      })
    }

    es.onerror = () => {
      setConnection({ isConnected: false, error: "Connection lost. Reconnecting..." })
      es.close()
      reconnectTimeoutRef.current = window.setTimeout(connect, 3000)
    }
  }, [streamUrl, parseEntry, softCap, hardCap, clearReconnectTimeout])

  useEffect(() => {
    connect()
    return () => {
      if (eventSourceRef.current) {
        eventSourceRef.current.close()
      }
      clearReconnectTimeout()
    }
  }, [connect, clearReconnectTimeout])

  // Filter entries
  const filteredEntries = useMemo(() => {
    const query = searchQuery.toLowerCase().trim()

    return entries.filter((e) => {
      // Filter by level
      if (!selectedLevels.has(getEntryLevel(e))) return false

      // Filter by exclusions
      if (getExclusionKey && exclusions.includes(getExclusionKey(e))) return false

      // Filter by search query
      if (query && !getSearchableText(e).toLowerCase().includes(query)) return false

      return true
    })
  }, [entries, selectedLevels, searchQuery, exclusions, getEntryLevel, getSearchableText, getExclusionKey])

  const toggleLevel = (levelId: string) => {
    setSelectedLevels((prev) => {
      const next = new Set(prev)
      if (next.has(levelId)) {
        next.delete(levelId)
      } else {
        next.add(levelId)
      }
      return next
    })
  }

  const selectAll = () => setSelectedLevels(new Set(levels.map((l) => l.id)))
  const selectNone = () => setSelectedLevels(new Set())

  // Auto-scroll to bottom when enabled
  useEffect(() => {
    if (autoScroll && scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight
    }
  }, [filteredEntries, autoScroll])

  const handleClear = () => {
    setEntries([])
    entryIndexRef.current = 0
  }

  const getLevelFilterLabel = () => {
    if (selectedLevels.size === levels.length) return "All Levels"
    if (selectedLevels.size === 0) return "None"
    return `${selectedLevels.size} Level${selectedLevels.size > 1 ? "s" : ""}`
  }

  return (
    <div className="flex flex-col h-full space-y-3">
      {/* Toolbar */}
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex items-center gap-2">
          <div
            className={`h-2 w-2 rounded-full ${connection.isConnected ? "bg-green-500" : "bg-red-500"}`}
          />
          <span className="text-sm text-muted-foreground">
            {connection.isConnected ? "Connected" : "Disconnected"}
          </span>
          {connection.error && (
            <span className="flex items-center gap-1 text-sm text-yellow-500">
              <AlertCircle className="h-3 w-3" />
              {connection.error}
            </span>
          )}
        </div>
        <div className="flex items-center gap-2">
          {/* Search */}
          <div className="relative">
            <Search className="absolute left-2 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
            <Input
              type="text"
              placeholder="Search logs..."
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              className="h-8 w-40 pl-7 pr-7 text-xs"
            />
            {searchQuery && (
              <button
                type="button"
                onClick={() => setSearchQuery("")}
                className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
              >
                <X className="h-3.5 w-3.5" />
              </button>
            )}
          </div>

          {/* Level filter */}
          <Popover>
            <PopoverTrigger asChild>
              <Button variant="outline" size="sm" className="h-8 gap-1">
                <Filter className="h-3.5 w-3.5" />
                <span className="text-xs">{getLevelFilterLabel()}</span>
                <ChevronDown className="h-3.5 w-3.5 opacity-50" />
              </Button>
            </PopoverTrigger>
            <PopoverContent className="w-44 p-2" align="end">
              <div className="flex justify-between mb-2">
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-6 px-2 text-xs"
                  onClick={selectAll}
                >
                  All
                </Button>
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-6 px-2 text-xs"
                  onClick={selectNone}
                >
                  None
                </Button>
              </div>
              <div className="space-y-1">
                {levels.map((level) => (
                  <label
                    key={level.id}
                    className="flex items-center gap-2 px-2 py-1 rounded hover:bg-muted cursor-pointer"
                  >
                    <Checkbox
                      checked={selectedLevels.has(level.id)}
                      onCheckedChange={() => toggleLevel(level.id)}
                    />
                    <span
                      className={`text-xs font-medium uppercase ${level.badgeClass} px-1.5 py-0.5 rounded`}
                    >
                      {level.label}
                    </span>
                  </label>
                ))}
              </div>
            </PopoverContent>
          </Popover>

          {/* Additional toolbar content */}
          {toolbarContent}

          <Button variant="outline" size="sm" className="h-8" onClick={handleClear}>
            Clear
          </Button>

          <div className="flex items-center gap-2">
            <Switch
              id="log-viewer-autoscroll"
              checked={autoScroll}
              onCheckedChange={setAutoScroll}
            />
            <Label htmlFor="log-viewer-autoscroll" className="text-sm cursor-pointer">
              Auto-scroll
            </Label>
          </div>
        </div>
      </div>

      {/* Log content */}
      <div
        ref={scrollRef}
        className={`flex-1 min-h-0 overflow-auto rounded-md border bg-muted/30 p-2 font-mono text-sm ${heightClass}`}
        style={{ overflowAnchor: "none" }}
      >
        {filteredEntries.length > 0 ? (
          <div className="space-y-0.5">
            {filteredEntries.map((entry) => (
              <div
                key={entry.id}
                onClick={onEntryClick ? () => onEntryClick(entry) : undefined}
                className={onEntryClick ? "cursor-pointer" : undefined}
              >
                {renderEntry(entry)}
              </div>
            ))}
          </div>
        ) : (
          <div className="flex items-center justify-center h-full text-muted-foreground">
            {entries.length > 0 ? "No entries match the current filter" : "Waiting for log entries..."}
          </div>
        )}
      </div>

      {/* Footer */}
      <div className="flex items-center justify-between gap-4 text-xs text-muted-foreground">
        <span className="flex items-center gap-2">
          <span>
            Showing {filteredEntries.length} of {entries.length} entries
            {autoScroll
              ? ` (${softCap.toLocaleString()} max)`
              : ` (${hardCap.toLocaleString()} max while paused)`}
          </span>
          {droppedWhilePaused && (
            <Badge variant="outline" className="text-yellow-500 border-yellow-500/50">
              oldest entries dropped
            </Badge>
          )}
        </span>
        {footerContent}
      </div>
    </div>
  )
}

// Helper component for rendering a standard log entry row
export interface LogEntryRowProps {
  time: string
  level: string
  levelBadgeClass: string
  levelTextClass: string
  message: string
  extra?: React.ReactNode
}

export function LogEntryRow({
  time,
  level,
  levelBadgeClass,
  levelTextClass,
  message,
  extra,
}: LogEntryRowProps) {
  return (
    <div className="group flex gap-3 py-1 px-2 hover:bg-muted/50 rounded-sm">
      <span className="shrink-0 text-muted-foreground/60 font-mono text-xs tabular-nums">
        {time}
      </span>
      <span
        className={`shrink-0 w-16 h-5 inline-flex items-center justify-center text-[10px] font-medium uppercase rounded ${levelBadgeClass}`}
      >
        {level}
      </span>
      <span className={`${levelTextClass} break-words`}>
        {message}
      </span>
      {extra}
    </div>
  )
}
