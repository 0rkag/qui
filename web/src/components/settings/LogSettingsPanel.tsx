/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { LogViewer, type LogLevel } from "@/components/ui/log-viewer"
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from "@/components/ui/select"
import { usePersistedLogExclusions } from "@/hooks/usePersistedLogExclusions"
import { api } from "@/lib/api"
import { copyTextToClipboard } from "@/lib/utils"
import type { LogSettingsUpdate } from "@/types"
import { useForm } from "@tanstack/react-form"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { ChevronDown, Copy, FileText, Loader2, Lock, Settings, X } from "lucide-react"
import { useCallback, useMemo, useState } from "react"
import { toast } from "sonner"

// Log settings form types
const LOG_LEVELS = ["TRACE", "DEBUG", "INFO", "WARN", "ERROR"] as const

function normalizeLogLevel(level: string | undefined): (typeof LOG_LEVELS)[number] {
  const normalized = level?.trim().toUpperCase()
  if (normalized && (LOG_LEVELS as readonly string[]).includes(normalized)) {
    return normalized as (typeof LOG_LEVELS)[number]
  }
  return "INFO"
}

// Log entry types
type AppLogLevel = "trace" | "debug" | "info" | "warn" | "error"

interface ParsedLogEntry {
  id: number
  level: AppLogLevel
  time: string
  message: string
  extra: Record<string, unknown>
  raw: string
  isJson: boolean
}

const LOG_LEVEL_CONFIG: LogLevel[] = [
  { id: "trace", label: "trace", badgeClass: "bg-muted text-muted-foreground", textClass: "text-muted-foreground" },
  { id: "debug", label: "debug", badgeClass: "bg-blue-500/20 text-blue-400", textClass: "text-blue-400" },
  { id: "info", label: "info", badgeClass: "bg-green-500/20 text-green-400", textClass: "text-green-400" },
  { id: "warn", label: "warn", badgeClass: "bg-yellow-500/20 text-yellow-400", textClass: "text-yellow-400" },
  { id: "error", label: "error", badgeClass: "bg-red-500/20 text-red-400", textClass: "text-red-400" },
]

const LEVEL_MAP = new Map(LOG_LEVEL_CONFIG.map((l) => [l.id, l]))

const VALID_LEVELS = new Set<AppLogLevel>(["trace", "debug", "info", "warn", "error"])

function normalizeLevel(raw: string | undefined): AppLogLevel {
  const level = raw?.toLowerCase()
  if (level && VALID_LEVELS.has(level as AppLogLevel)) {
    return level as AppLogLevel
  }
  if (level === "fatal" || level === "panic") {
    return "error"
  }
  return "info"
}

function parseLogLine(data: string, index: number): ParsedLogEntry | null {
  try {
    const parsed = JSON.parse(data) as Record<string, unknown>
    const level = normalizeLevel(typeof parsed.level === "string" ? parsed.level : undefined)
    const time = typeof parsed.time === "string" ? parsed.time : ""
    const message = typeof parsed.message === "string" ? parsed.message : ""
    const { level: _l, time: _t, message: _m, ...extra } = parsed

    return { id: index, level, time, message, extra, raw: data, isJson: true }
  } catch {
    return { id: index, level: "info", time: "", message: data, extra: {}, raw: data, isJson: false }
  }
}

function formatTime(isoTime: string): string {
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

// Syntax highlight JSON string
function highlightJson(json: string): React.ReactNode[] {
  if (!json) return []

  const tokenRegex = /("(?:\\.|[^"\\])*")\s*:|("(?:\\.|[^"\\])*")|(-?\d+\.?\d*(?:[eE][+-]?\d+)?)|(\btrue\b|\bfalse\b)|(\bnull\b)|([{}[\],:])/g

  const result: React.ReactNode[] = []
  let lastIndex = 0
  let key = 0

  for (const match of json.matchAll(tokenRegex)) {
    const matchIndex = match.index
    if (matchIndex == null) continue

    if (matchIndex > lastIndex) {
      result.push(json.slice(lastIndex, matchIndex))
    }

    const [fullMatch, keyMatch, stringMatch, numberMatch, boolMatch, nullMatch, punctMatch] = match

    if (keyMatch) {
      result.push(<span key={key++} className="text-cyan-700 dark:text-cyan-400">{keyMatch}</span>, ":")
    } else if (stringMatch) {
      result.push(<span key={key++} className="text-green-700 dark:text-green-400">{stringMatch}</span>)
    } else if (numberMatch) {
      result.push(<span key={key++} className="text-amber-700 dark:text-amber-400">{numberMatch}</span>)
    } else if (boolMatch) {
      result.push(<span key={key++} className="text-purple-700 dark:text-purple-400">{boolMatch}</span>)
    } else if (nullMatch) {
      result.push(<span key={key++} className="text-muted-foreground">{nullMatch}</span>)
    } else if (punctMatch) {
      result.push(<span key={key++} className="text-muted-foreground/70">{punctMatch}</span>)
    } else {
      result.push(fullMatch)
    }

    lastIndex = matchIndex + fullMatch.length
  }

  if (lastIndex < json.length) {
    result.push(json.slice(lastIndex))
  }

  return result
}

function LogEntryDialog({
  entry,
  open,
  onOpenChange,
}: {
  entry: ParsedLogEntry | null
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const { prettyJson, highlightedJson } = useMemo(() => {
    if (!entry) return { prettyJson: "", highlightedJson: [] as React.ReactNode[] }
    const jsonObj = { level: entry.level, time: entry.time, message: entry.message, ...entry.extra }
    const pretty = JSON.stringify(jsonObj, null, 2)
    return { prettyJson: pretty, highlightedJson: highlightJson(pretty) }
  }, [entry])

  const handleCopyJson = useCallback(async () => {
    if (!prettyJson) return
    try {
      await copyTextToClipboard(prettyJson)
      toast.success("JSON copied to clipboard")
    } catch {
      toast.error("Failed to copy to clipboard")
    }
  }, [prettyJson])

  const handleCopyRaw = useCallback(async () => {
    if (!entry) return
    try {
      await copyTextToClipboard(entry.raw)
      toast.success("Raw line copied to clipboard")
    } catch {
      toast.error("Failed to copy to clipboard")
    }
  }, [entry])

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="!max-w-2xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            Log Entry
            {entry && (
              <span className={`text-xs font-medium uppercase px-1.5 py-0.5 rounded ${LEVEL_MAP.get(entry.level)?.badgeClass}`}>
                {entry.level}
              </span>
            )}
          </DialogTitle>
          <DialogDescription>
            {entry?.time ? new Date(entry.time).toLocaleString() : "Raw log line"}
          </DialogDescription>
        </DialogHeader>
        <div className="max-h-[400px] overflow-auto rounded-md border bg-muted/30 p-4">
          <pre className="font-mono text-sm whitespace-pre-wrap break-all">{highlightedJson}</pre>
        </div>
        <DialogFooter className="gap-2 sm:gap-0">
          <Button variant="outline" size="sm" onClick={handleCopyRaw}>
            <Copy className="mr-2 h-4 w-4" />
            Copy Raw
          </Button>
          <Button variant="outline" size="sm" onClick={handleCopyJson}>
            <Copy className="mr-2 h-4 w-4" />
            Copy JSON
          </Button>
          <Button size="sm" onClick={() => onOpenChange(false)}>
            Close
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function LogEntryRow({
  entry,
  onMute,
}: {
  entry: ParsedLogEntry
  onMute?: () => void
}) {
  const levelConfig = LEVEL_MAP.get(entry.level)
  const extraKeys = Object.keys(entry.extra)

  const handleMute = (e: React.MouseEvent) => {
    e.stopPropagation()
    onMute?.()
  }

  return (
    <div className="group flex gap-2 py-0.5 whitespace-nowrap hover:bg-muted/50">
      <span className="shrink-0 text-muted-foreground/60">{formatTime(entry.time)}</span>
      <button
        type="button"
        onClick={onMute ? handleMute : undefined}
        disabled={!onMute}
        title={onMute ? "Mute similar entries" : undefined}
        className={`group/mute shrink-0 w-12 h-4 inline-flex items-center justify-center text-[10px] font-medium uppercase rounded ${levelConfig?.badgeClass} ${onMute ? "cursor-pointer" : ""}`}
      >
        <span className={onMute ? "group-hover/mute:hidden" : ""}>{entry.level}</span>
        {onMute && <X className="hidden group-hover/mute:block size-2.5" />}
      </button>
      <span className={levelConfig?.textClass}>{entry.message}</span>
      {extraKeys.length > 0 && (
        <span className="text-muted-foreground/50">
          {extraKeys.map((key) => (
            <span key={key} className="ml-2">
              <span className="text-muted-foreground/70">{key}</span>
              <span className="text-muted-foreground/40">=</span>
              <span className="text-muted-foreground/60">
                {typeof entry.extra[key] === "string" ? entry.extra[key] as string : JSON.stringify(entry.extra[key])}
              </span>
            </span>
          ))}
        </span>
      )}
    </div>
  )
}

function LiveLogViewer({ configPath }: { configPath?: string }) {
  const [selectedEntry, setSelectedEntry] = useState<ParsedLogEntry | null>(null)
  const [logExclusions, setLogExclusions] = usePersistedLogExclusions()

  const handleMuteMessage = useCallback((message: string) => {
    if (!logExclusions.includes(message)) {
      setLogExclusions([...logExclusions, message])
    }
  }, [logExclusions, setLogExclusions])

  const handleUnmuteMessage = useCallback((message: string) => {
    setLogExclusions(logExclusions.filter((m) => m !== message))
  }, [logExclusions, setLogExclusions])

  const renderEntry = useCallback((entry: ParsedLogEntry) => (
    <LogEntryRow
      entry={entry}
      onMute={() => handleMuteMessage(entry.message)}
    />
  ), [handleMuteMessage])

  const toolbarContent = logExclusions.length > 0 ? (
    <Popover>
      <PopoverTrigger asChild>
        <Button variant="outline" size="sm" className="h-8 gap-1">
          <span className="text-xs">{logExclusions.length} Muted</span>
          <ChevronDown className="h-3.5 w-3.5 opacity-50" />
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-72 p-2" align="start">
        <div className="flex justify-between items-center mb-2">
          <span className="text-xs font-medium">Muted Messages</span>
          <Button variant="ghost" size="sm" className="h-6 px-2 text-xs" onClick={() => setLogExclusions([])}>
            Clear all
          </Button>
        </div>
        <div className="space-y-1 max-h-48 overflow-auto">
          {logExclusions.map((message) => (
            <div key={message} className="flex items-center justify-between gap-2 px-2 py-1 rounded hover:bg-muted group">
              <span className="text-xs truncate" title={message}>{message}</span>
              <button
                type="button"
                onClick={() => handleUnmuteMessage(message)}
                className="shrink-0 text-muted-foreground hover:text-foreground"
              >
                <X className="h-3 w-3" />
              </button>
            </div>
          ))}
        </div>
      </PopoverContent>
    </Popover>
  ) : null

  const footerContent = configPath ? (
    <span className="flex items-center gap-1.5 font-mono text-muted-foreground/70">
      <FileText className="h-3 w-3" />
      {configPath}
    </span>
  ) : null

  return (
    <>
      <LogViewer<ParsedLogEntry>
        streamUrl={api.getLogStreamUrl(1000)}
        parseEntry={parseLogLine}
        getEntryLevel={(e) => e.level}
        getSearchableText={(e) => {
          const extraText = Object.values(e.extra)
            .map((v) => (typeof v === "string" ? v : JSON.stringify(v) ?? ""))
            .join(" ")
          return `${e.message} ${extraText}`
        }}
        renderEntry={renderEntry}
        levels={LOG_LEVEL_CONFIG}
        heightClass="h-[clamp(300px,calc(100dvh-24rem),600px)]"
        onEntryClick={(entry) => entry.isJson && setSelectedEntry(entry)}
        toolbarContent={toolbarContent}
        footerContent={footerContent}
        exclusions={logExclusions}
        getExclusionKey={(e) => e.message}
      />
      <LogEntryDialog
        entry={selectedEntry}
        open={selectedEntry !== null}
        onOpenChange={(open) => !open && setSelectedEntry(null)}
      />
    </>
  )
}

function LogSettingsFormInner({ settings }: { settings: NonNullable<Awaited<ReturnType<typeof api.getLogSettings>>> }) {
  const queryClient = useQueryClient()

  const updateMutation = useMutation({
    mutationFn: (update: LogSettingsUpdate) => api.updateLogSettings(update),
    onSuccess: (data) => {
      queryClient.setQueryData(["log-settings"], data)
      toast.success("Log settings updated")
    },
    onError: (error) => {
      toast.error(error instanceof Error ? error.message : "Failed to update log settings")
    },
  })

  const initialLevel = normalizeLogLevel(settings.level)
  const form = useForm({
    defaultValues: {
      level: initialLevel,
      path: settings.path,
      maxSize: settings.maxSize,
      maxBackups: settings.maxBackups,
    },
    onSubmit: async ({ value }) => {
      const update: LogSettingsUpdate = {}
      if (value.level !== normalizeLogLevel(settings.level)) update.level = value.level
      if (value.path !== (settings.path ?? "")) update.path = value.path
      if (value.maxSize !== (settings.maxSize ?? 50)) update.maxSize = value.maxSize
      if (value.maxBackups !== (settings.maxBackups ?? 3)) update.maxBackups = value.maxBackups

      if (Object.keys(update).length > 0) {
        await updateMutation.mutateAsync(update)
      }
    },
  })

  const isLocked = (field: string) => settings.locked?.[field] !== undefined

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault()
        form.handleSubmit()
      }}
      className="space-y-3"
    >
      <form.Field name="level">
        {(field) => (
          <div className="space-y-1.5">
            <div className="flex items-center gap-2">
              <Label htmlFor="level" className="text-sm">Log Level</Label>
              {isLocked("level") && (
                <Badge variant="outline" className="gap-1 text-xs">
                  <Lock className="h-3 w-3" />
                  {settings?.locked?.level}
                </Badge>
              )}
            </div>
            <Select
              value={field.state.value}
              onValueChange={(value) => field.handleChange(normalizeLogLevel(value))}
              disabled={isLocked("level")}
            >
              <SelectTrigger id="level" className="h-9">
                <SelectValue placeholder="Select log level" />
              </SelectTrigger>
              <SelectContent>
                {LOG_LEVELS.map((level) => (
                  <SelectItem key={level} value={level}>{level}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        )}
      </form.Field>

      <form.Field name="path">
        {(field) => (
          <div className="space-y-1.5">
            <div className="flex items-center gap-2">
              <Label htmlFor="path" className="text-sm">Log File Path</Label>
              {isLocked("path") && (
                <Badge variant="outline" className="gap-1 text-xs">
                  <Lock className="h-3 w-3" />
                  {settings?.locked?.path}
                </Badge>
              )}
            </div>
            <Input
              id="path"
              className="h-9"
              placeholder="Leave empty for stdout only"
              value={field.state.value}
              onChange={(e) => field.handleChange(e.target.value)}
              disabled={isLocked("path")}
            />
          </div>
        )}
      </form.Field>

      <div className="grid grid-cols-2 gap-3">
        <form.Field name="maxSize">
          {(field) => (
            <div className="space-y-1.5">
              <div className="flex items-center gap-2">
                <Label htmlFor="maxSize" className="text-sm">Max Size (MB)</Label>
                {isLocked("maxSize") && (
                  <Badge variant="outline" className="gap-1 text-xs"><Lock className="h-3 w-3" /></Badge>
                )}
              </div>
              <Input
                id="maxSize"
                className="h-9"
                type="number"
                min={1}
                value={field.state.value}
                onChange={(e) => field.handleChange(parseInt(e.target.value) || 50)}
                disabled={isLocked("maxSize")}
              />
            </div>
          )}
        </form.Field>

        <form.Field name="maxBackups">
          {(field) => (
            <div className="space-y-1.5">
              <div className="flex items-center gap-2">
                <Label htmlFor="maxBackups" className="text-sm">Max Backups</Label>
                {isLocked("maxBackups") && (
                  <Badge variant="outline" className="gap-1 text-xs"><Lock className="h-3 w-3" /></Badge>
                )}
              </div>
              <Input
                id="maxBackups"
                className="h-9"
                type="number"
                min={0}
                value={field.state.value}
                onChange={(e) => field.handleChange(parseInt(e.target.value) || 0)}
                disabled={isLocked("maxBackups")}
              />
            </div>
          )}
        </form.Field>
      </div>

      <form.Subscribe selector={(state) => [state.canSubmit, state.isSubmitting]}>
        {([canSubmit, isSubmitting]) => (
          <Button type="submit" size="sm" className="w-full" disabled={!canSubmit || isSubmitting || updateMutation.isPending}>
            {isSubmitting || updateMutation.isPending ? (
              <>
                <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                Saving...
              </>
            ) : (
              "Save Settings"
            )}
          </Button>
        )}
      </form.Subscribe>
    </form>
  )
}

function LogSettingsForm() {
  const { data: settings, isLoading } = useQuery({
    queryKey: ["log-settings"],
    queryFn: () => api.getLogSettings(),
  })

  if (isLoading || !settings) {
    return (
      <div className="flex items-center justify-center py-8">
        <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
      </div>
    )
  }

  return <LogSettingsFormInner settings={settings} />
}

export function LogSettingsPanel() {
  const { data: settings } = useQuery({
    queryKey: ["log-settings"],
    queryFn: () => api.getLogSettings(),
  })

  return (
    <Card>
      <CardHeader>
        <div className="flex items-start justify-between gap-4">
          <div className="space-y-1">
            <CardTitle>Logs</CardTitle>
            <CardDescription>
              Real-time application logs. Click a level badge to mute similar entries.
            </CardDescription>
          </div>
          <Popover>
            <PopoverTrigger asChild>
              <Button variant="outline" size="icon" className="h-8 w-8 shrink-0">
                <Settings className="h-4 w-4" />
                <span className="sr-only">Log settings</span>
              </Button>
            </PopoverTrigger>
            <PopoverContent className="w-80" align="end">
              <div className="space-y-3">
                <div className="space-y-1">
                  <h4 className="font-medium text-sm">Log Configuration</h4>
                  <p className="text-xs text-muted-foreground">Changes are applied immediately.</p>
                </div>
                <LogSettingsForm />
              </div>
            </PopoverContent>
          </Popover>
        </div>
      </CardHeader>
      <CardContent>
        <LiveLogViewer configPath={settings?.configPath} />
      </CardContent>
    </Card>
  )
}
