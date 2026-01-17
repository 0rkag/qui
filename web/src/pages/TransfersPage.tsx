/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useState, useMemo } from "react"
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import {
  AlertCircle,
  ArrowRight,
  ArrowUpFromLine,
  CheckCircle2,
  Clock,
  Copy,
  HardDrive,
  Link2,
  Loader2,
  RefreshCw,
  Server,
  Trash2,
  XCircle,
} from "lucide-react"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import { useDateTimeFormatters } from "@/hooks/useDateTimeFormatters"
import { useInstances } from "@/hooks/useInstances"
import { api } from "@/lib/api"
import type { Transfer, TransferState } from "@/types"
import { toast } from "sonner"

// Base state config (for states that don't depend on linkMode)
const STATE_CONFIG: Record<TransferState, { label: string; color: string; icon: typeof Clock }> = {
  pending: { label: "Pending", color: "bg-slate-500", icon: Clock },
  preparing: { label: "Preparing", color: "bg-blue-500", icon: Loader2 },
  links_creating: { label: "Processing", color: "bg-blue-500", icon: HardDrive },
  links_created: { label: "Processed", color: "bg-blue-500", icon: HardDrive },
  adding_torrent: { label: "Adding Torrent", color: "bg-blue-500", icon: Server },
  torrent_added: { label: "Torrent Added", color: "bg-blue-500", icon: Server },
  deleting_source: { label: "Deleting Source", color: "bg-amber-500", icon: Trash2 },
  completed: { label: "Completed", color: "bg-green-500", icon: CheckCircle2 },
  failed: { label: "Failed", color: "bg-red-500", icon: XCircle },
  rolled_back: { label: "Rolled Back", color: "bg-amber-500", icon: RefreshCw },
  cancelled: { label: "Cancelled", color: "bg-slate-500", icon: XCircle },
}

// Get dynamic state label and icon based on linkMode
function getStateDisplay(state: TransferState, linkMode?: string): { label: string; icon: typeof Clock } {
  const baseConfig = STATE_CONFIG[state]

  // For file operation states, customize based on linkMode
  if (state === "links_creating") {
    switch (linkMode) {
      case "hardlink":
        return { label: "Creating Hardlinks", icon: Link2 }
      case "reflink":
        return { label: "Creating Reflinks", icon: Link2 }
      case "transfer":
        return { label: "Uploading Files", icon: ArrowUpFromLine }
      case "copy":
        return { label: "Copying Files", icon: Copy }
      case "direct":
        return { label: "Preparing", icon: Loader2 }
      default:
        return { label: baseConfig.label, icon: baseConfig.icon }
    }
  }

  if (state === "links_created") {
    switch (linkMode) {
      case "hardlink":
        return { label: "Hardlinks Created", icon: Link2 }
      case "reflink":
        return { label: "Reflinks Created", icon: Link2 }
      case "transfer":
        return { label: "Files Uploaded", icon: ArrowUpFromLine }
      case "copy":
        return { label: "Files Copied", icon: Copy }
      case "direct":
        return { label: "Ready", icon: CheckCircle2 }
      default:
        return { label: baseConfig.label, icon: baseConfig.icon }
    }
  }

  return { label: baseConfig.label, icon: baseConfig.icon }
}

const STATE_FILTER_OPTIONS: { value: string; label: string }[] = [
  { value: "all", label: "All States" },
  { value: "active", label: "Active" },
  { value: "completed", label: "Completed" },
  { value: "failed", label: "Failed" },
]

const ACTIVE_STATES: TransferState[] = ["pending", "preparing", "links_creating", "links_created", "adding_torrent", "deleting_source"]

// User-friendly labels for link modes
const LINK_MODE_LABELS: Record<string, { label: string; description: string }> = {
  hardlink: { label: "Hardlink", description: "Instant file linking (same filesystem)" },
  reflink: { label: "Reflink", description: "Copy-on-write links (BTRFS/XFS)" },
  direct: { label: "Direct", description: "Shared storage, no file copy needed" },
  transfer: { label: "SSH Transfer", description: "Remote file transfer via SSH/rsync" },
  copy: { label: "SSH Copy", description: "Remote file copy via SSH" },
}

// Format bytes to human-readable string
function formatBytes(bytes: number): string {
  if (bytes === 0) return "0 B"
  const k = 1024
  const sizes = ["B", "KB", "MB", "GB", "TB"]
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return `${(bytes / Math.pow(k, i)).toFixed(i > 0 ? 1 : 0)} ${sizes[i]}`
}

// Format speed to human-readable string
function formatSpeed(bytesPerSecond: number): string {
  if (bytesPerSecond === 0) return "-"
  return `${formatBytes(bytesPerSecond)}/s`
}

// Calculate transfer speed from completed transfer
function calculateSpeed(transfer: Transfer): number | null {
  if (!transfer.completedAt || !transfer.createdAt || transfer.bytesTotal === 0) {
    return null
  }
  const startTime = new Date(transfer.createdAt).getTime()
  const endTime = new Date(transfer.completedAt).getTime()
  const durationSeconds = (endTime - startTime) / 1000
  if (durationSeconds <= 0) return null
  return transfer.bytesTotal / durationSeconds
}

export function TransfersPage() {
  const queryClient = useQueryClient()
  const { instances } = useInstances()
  const { formatISOTimestamp } = useDateTimeFormatters()

  const [stateFilter, setStateFilter] = useState<string>("all")
  const [instanceFilter, setInstanceFilter] = useState<string>("all")
  const [cancelTarget, setCancelTarget] = useState<Transfer | null>(null)

  // Build query params based on filters
  const queryParams = useMemo(() => {
    const params: { states?: TransferState[]; instanceId?: number; limit?: number } = {
      limit: 100,
    }

    if (stateFilter === "active") {
      params.states = ACTIVE_STATES
    } else if (stateFilter === "completed") {
      params.states = ["completed"]
    } else if (stateFilter === "failed") {
      params.states = ["failed", "rolled_back", "cancelled"]
    }

    if (instanceFilter !== "all") {
      params.instanceId = parseInt(instanceFilter, 10)
    }

    return params
  }, [stateFilter, instanceFilter])

  const { data: transfers = [], isLoading, refetch } = useQuery({
    queryKey: ["transfers", queryParams],
    queryFn: () => api.listTransfers(queryParams),
    refetchInterval: 5000, // Auto-refresh every 5 seconds for active transfers
  })

  const cancelMutation = useMutation({
    mutationFn: (id: number) => api.cancelTransfer(id),
    onSuccess: () => {
      toast.success("Transfer cancelled")
      queryClient.invalidateQueries({ queryKey: ["transfers"] })
    },
    onError: (error) => {
      toast.error(error instanceof Error ? error.message : "Failed to cancel transfer")
    },
  })

  const handleCancel = () => {
    if (cancelTarget) {
      cancelMutation.mutate(cancelTarget.id)
      setCancelTarget(null)
    }
  }

  // Get instance name by ID
  const getInstanceName = (id: number) => {
    return instances?.find((i) => i.id === id)?.name ?? `Instance #${id}`
  }

  // Count active transfers
  const activeCount = useMemo(() => {
    return transfers.filter((t) => ACTIVE_STATES.includes(t.state)).length
  }, [transfers])

  return (
    <TooltipProvider>
      <div className="container mx-auto p-6 space-y-6">
        <div className="flex items-center justify-between">
          <div>
            <h1 className="text-2xl font-bold">Transfers</h1>
            <p className="text-sm text-muted-foreground">
              View and manage torrent transfers between instances
            </p>
          </div>
          <div className="flex items-center gap-2">
            {activeCount > 0 && (
              <Badge variant="secondary" className="gap-1">
                <Loader2 className="h-3 w-3 animate-spin" />
                {activeCount} active
              </Badge>
            )}
            <Select value={stateFilter} onValueChange={setStateFilter}>
              <SelectTrigger className="w-[130px] h-8">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {STATE_FILTER_OPTIONS.map((opt) => (
                  <SelectItem key={opt.value} value={opt.value}>
                    {opt.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Select value={instanceFilter} onValueChange={setInstanceFilter}>
              <SelectTrigger className="w-[150px] h-8">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">All Instances</SelectItem>
                {instances?.map((inst) => (
                  <SelectItem key={inst.id} value={inst.id.toString()}>
                    {inst.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Button variant="outline" size="sm" className="h-8" onClick={() => refetch()}>
              <RefreshCw className="h-4 w-4" />
            </Button>
          </div>
        </div>

        {/* Transfers Table */}
        <Card>
          <CardHeader>
            <CardTitle>Transfer History</CardTitle>
            <CardDescription>
              {transfers.length === 0
                ? "No transfers found"
                : `${transfers.length} transfer${transfers.length !== 1 ? "s" : ""}`}
            </CardDescription>
          </CardHeader>
          <CardContent>
            {isLoading ? (
              <div className="flex items-center justify-center py-8">
                <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
              </div>
            ) : transfers.length === 0 ? (
              <div className="text-center py-8 text-muted-foreground">
                <Server className="h-12 w-12 mx-auto mb-3 opacity-50" />
                <p>No transfers found</p>
                <p className="text-sm mt-1">
                  Move torrents between instances from the{" "}
                  <Link to="/instances" className="text-primary underline">
                    torrents page
                  </Link>
                </p>
              </div>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Torrent</TableHead>
                    <TableHead>Route</TableHead>
                    <TableHead>State</TableHead>
                    <TableHead>Mode</TableHead>
                    <TableHead>Size / Speed</TableHead>
                    <TableHead>Options</TableHead>
                    <TableHead>Started</TableHead>
                    <TableHead className="w-[80px]"></TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {transfers.map((transfer) => (
                    <TransferRow
                      key={transfer.id}
                      transfer={transfer}
                      getInstanceName={getInstanceName}
                      formatISOTimestamp={formatISOTimestamp}
                      onCancel={() => setCancelTarget(transfer)}
                      isCancelling={cancelMutation.isPending}
                    />
                  ))}
                </TableBody>
              </Table>
            )}
          </CardContent>
        </Card>

        {/* Cancel Confirmation Dialog */}
        <AlertDialog open={!!cancelTarget} onOpenChange={(open) => !open && setCancelTarget(null)}>
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>Cancel Transfer</AlertDialogTitle>
              <AlertDialogDescription>
                Are you sure you want to cancel this transfer?
                {cancelTarget && (
                  <div className="mt-2 p-2 bg-muted rounded text-sm">
                    <div className="font-medium truncate">{cancelTarget.torrentName}</div>
                    <div className="text-xs text-muted-foreground mt-1">
                      {getInstanceName(cancelTarget.sourceInstanceId)} → {getInstanceName(cancelTarget.targetInstanceId)}
                    </div>
                  </div>
                )}
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>Keep Running</AlertDialogCancel>
              <AlertDialogAction
                onClick={handleCancel}
                className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
              >
                Cancel Transfer
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      </div>
    </TooltipProvider>
  )
}

interface TransferRowProps {
  transfer: Transfer
  getInstanceName: (id: number) => string
  formatISOTimestamp: (date: string) => string
  onCancel: () => void
  isCancelling: boolean
}

function TransferRow({
  transfer,
  getInstanceName,
  formatISOTimestamp,
  onCancel,
  isCancelling,
}: TransferRowProps) {
  const baseConfig = STATE_CONFIG[transfer.state]
  const stateDisplay = getStateDisplay(transfer.state, transfer.linkMode)
  const Icon = stateDisplay.icon
  const isActive = ACTIVE_STATES.includes(transfer.state)
  const canCancel = isActive

  return (
    <TableRow className={transfer.state === "failed" ? "bg-red-500/5" : undefined}>
      {/* Torrent Name */}
      <TableCell>
        <div className="max-w-[300px]">
          <Tooltip>
            <TooltipTrigger asChild>
              <span className="font-medium truncate block">{transfer.torrentName || transfer.torrentHash}</span>
            </TooltipTrigger>
            <TooltipContent>
              <p>{transfer.torrentName}</p>
              <p className="text-xs text-muted-foreground font-mono">{transfer.torrentHash}</p>
            </TooltipContent>
          </Tooltip>
        </div>
      </TableCell>

      {/* Route */}
      <TableCell>
        <Tooltip>
          <TooltipTrigger asChild>
            <div className="flex items-center gap-2 text-sm cursor-help">
              <span className="truncate max-w-[100px]">{getInstanceName(transfer.sourceInstanceId)}</span>
              <ArrowRight className="h-3 w-3 text-muted-foreground shrink-0" />
              <span className="truncate max-w-[100px]">{getInstanceName(transfer.targetInstanceId)}</span>
            </div>
          </TooltipTrigger>
          <TooltipContent className="max-w-md">
            <div className="space-y-2">
              <div>
                <p className="font-medium text-xs text-muted-foreground">Source</p>
                <p>{getInstanceName(transfer.sourceInstanceId)}</p>
                {transfer.sourceSavePath && (
                  <p className="text-xs font-mono text-muted-foreground break-all">{transfer.sourceSavePath}</p>
                )}
              </div>
              <div>
                <p className="font-medium text-xs text-muted-foreground">Destination</p>
                <p>{getInstanceName(transfer.targetInstanceId)}</p>
                {transfer.targetSavePath && (
                  <p className="text-xs font-mono text-muted-foreground break-all">{transfer.targetSavePath}</p>
                )}
              </div>
            </div>
          </TooltipContent>
        </Tooltip>
      </TableCell>

      {/* State */}
      <TableCell>
        <Tooltip>
          <TooltipTrigger asChild>
            <Badge variant="secondary" className={`gap-1 ${baseConfig.color} text-white`}>
              <Icon className={`h-3 w-3 ${isActive ? "animate-spin" : ""}`} />
              {stateDisplay.label}
            </Badge>
          </TooltipTrigger>
          {transfer.state === "failed" && transfer.error && (
            <TooltipContent className="max-w-xs">
              <div className="flex items-start gap-2">
                <AlertCircle className="h-4 w-4 text-red-500 shrink-0 mt-0.5" />
                <p>{transfer.error}</p>
              </div>
            </TooltipContent>
          )}
        </Tooltip>
      </TableCell>

      {/* Link Mode */}
      <TableCell>
        {transfer.linkMode ? (
          <Tooltip>
            <TooltipTrigger asChild>
              <Badge variant="outline" className="text-xs">
                {LINK_MODE_LABELS[transfer.linkMode]?.label ?? transfer.linkMode}
              </Badge>
            </TooltipTrigger>
            <TooltipContent>
              <p>{LINK_MODE_LABELS[transfer.linkMode]?.description ?? transfer.linkMode}</p>
            </TooltipContent>
          </Tooltip>
        ) : (
          <span className="text-muted-foreground">-</span>
        )}
      </TableCell>

      {/* Size / Speed */}
      <TableCell>
        <SizeSpeedCell transfer={transfer} />
      </TableCell>

      {/* Options */}
      <TableCell>
        <div className="flex items-center gap-1">
          {transfer.deleteFromSource && (
            <Tooltip>
              <TooltipTrigger>
                <Badge variant="outline" className="text-xs">
                  Del
                </Badge>
              </TooltipTrigger>
              <TooltipContent>Delete from source after transfer</TooltipContent>
            </Tooltip>
          )}
          {transfer.preserveCategory && (
            <Tooltip>
              <TooltipTrigger>
                <Badge variant="outline" className="text-xs">
                  Cat
                </Badge>
              </TooltipTrigger>
              <TooltipContent>Preserve category</TooltipContent>
            </Tooltip>
          )}
          {transfer.preserveTags && (
            <Tooltip>
              <TooltipTrigger>
                <Badge variant="outline" className="text-xs">
                  Tags
                </Badge>
              </TooltipTrigger>
              <TooltipContent>Preserve tags</TooltipContent>
            </Tooltip>
          )}
          {!transfer.deleteFromSource && !transfer.preserveCategory && !transfer.preserveTags && (
            <span className="text-muted-foreground">-</span>
          )}
        </div>
      </TableCell>

      {/* Started */}
      <TableCell>
        <span className="text-sm text-muted-foreground">
          {transfer.createdAt ? formatISOTimestamp(transfer.createdAt) : "-"}
        </span>
      </TableCell>

      {/* Actions */}
      <TableCell>
        {canCancel && (
          <Button
            size="sm"
            variant="ghost"
            onClick={onCancel}
            disabled={isCancelling}
            title="Cancel transfer"
          >
            <XCircle className="h-4 w-4 text-destructive" />
          </Button>
        )}
      </TableCell>
    </TableRow>
  )
}

// Size and Speed display component
function SizeSpeedCell({ transfer }: { transfer: Transfer }) {
  const isActive = ACTIVE_STATES.includes(transfer.state)
  const isCompleted = transfer.state === "completed"
  const speed = calculateSpeed(transfer)

  // No size info available
  if (transfer.bytesTotal === 0) {
    return <span className="text-muted-foreground text-sm">-</span>
  }

  // For active transfers, show progress
  if (isActive) {
    const progress = transfer.bytesTotal > 0
      ? Math.round((transfer.bytesTransferred / transfer.bytesTotal) * 100)
      : 0
    return (
      <Tooltip>
        <TooltipTrigger asChild>
          <div className="text-sm">
            <div className="font-medium">{formatBytes(transfer.bytesTotal)}</div>
            {transfer.bytesTransferred > 0 && transfer.bytesTransferred < transfer.bytesTotal && (
              <div className="text-xs text-muted-foreground">{progress}%</div>
            )}
          </div>
        </TooltipTrigger>
        <TooltipContent>
          <p>{formatBytes(transfer.bytesTransferred)} / {formatBytes(transfer.bytesTotal)}</p>
        </TooltipContent>
      </Tooltip>
    )
  }

  // For completed transfers, show size and speed
  if (isCompleted && speed !== null) {
    return (
      <Tooltip>
        <TooltipTrigger asChild>
          <div className="text-sm">
            <div>{formatBytes(transfer.bytesTotal)}</div>
            <div className="text-xs text-muted-foreground">{formatSpeed(speed)}</div>
          </div>
        </TooltipTrigger>
        <TooltipContent>
          <p>Transferred {formatBytes(transfer.bytesTotal)}</p>
          <p>Average speed: {formatSpeed(speed)}</p>
        </TooltipContent>
      </Tooltip>
    )
  }

  // For other states (failed, cancelled, etc.), just show size
  return (
    <span className="text-sm text-muted-foreground">{formatBytes(transfer.bytesTotal)}</span>
  )
}
