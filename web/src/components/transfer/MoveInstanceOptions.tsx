// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

import { memo, useMemo } from "react"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Badge } from "@/components/ui/badge"
import { AlertTriangle } from "lucide-react"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import type { FileExistsAction, SourceAction } from "@/types"

// Helper to determine if transfer is possible between two instances
function canTransferBetween(
  source: { local: boolean; ssh: boolean },
  target: { local: boolean; ssh: boolean }
): boolean {
  // Direct local transfer (both have local access)
  if (source.local && target.local) return true
  // Push via SSH (source local, target has SSH)
  if (source.local && target.ssh) return true
  // Pull via SSH (source has SSH, target local)
  if (source.ssh && target.local) return true
  // Remote-to-remote via SSH
  if (source.ssh && target.ssh) return true
  return false
}

// Helper to determine the transfer method that will be used
function getTransferMethod(
  source: { local: boolean; ssh: boolean },
  target: { local: boolean; ssh: boolean }
): "local" | "ssh" | null {
  if (source.local && target.local) return "local"
  if (source.local && target.ssh) return "ssh"
  if (source.ssh && target.local) return "ssh"
  if (source.ssh && target.ssh) return "ssh"
  return null
}

export interface InstanceOption {
  id: number
  name: string
  connected: boolean
  hasLocalFilesystemAccess: boolean
  transferCapabilities?: {
    local: boolean
    ssh: boolean
  }
}

export interface MoveInstanceValue {
  targetInstanceId: number | null
  fileExistsAction: FileExistsAction
  sourceAction: SourceAction
  verifyTransfer: boolean
  preserveCategory: boolean
  preserveTags: boolean
}

export interface MoveInstanceOptionsProps {
  /** Current source instance ID */
  sourceInstanceId: number
  /** Source instance capabilities (optional - will lookup from instances if not provided) */
  sourceCapabilities?: {
    local: boolean
    ssh: boolean
  }
  /** All available instances */
  instances: InstanceOption[]
  /** Current value */
  value: MoveInstanceValue
  /** Callback when value changes */
  onChange: (value: MoveInstanceValue) => void
  /** Layout variant - 'compact' for inline use, 'full' for dialog use */
  variant?: "compact" | "full"
  /** Whether to show descriptions under options */
  showDescriptions?: boolean
  /** Custom disabled reason to show */
  disabledReason?: string
}

export const MoveInstanceOptions = memo(function MoveInstanceOptions({
  sourceInstanceId,
  sourceCapabilities,
  instances,
  value,
  onChange,
  variant = "full",
  showDescriptions = true,
  disabledReason,
}: MoveInstanceOptionsProps) {
  // Source capabilities (fallback to hasLocalFilesystemAccess)
  const sourceCaps = useMemo(() => {
    if (sourceCapabilities) {
      return sourceCapabilities
    }
    const current = instances.find((i) => i.id === sourceInstanceId)
    // Use same fallback logic as target: transferCapabilities?.local ?? hasLocalFilesystemAccess
    return {
      local: current?.transferCapabilities?.local ?? current?.hasLocalFilesystemAccess ?? false,
      ssh: current?.transferCapabilities?.ssh ?? false,
    }
  }, [sourceCapabilities, instances, sourceInstanceId])

  // Filter instances to show only those that can receive transfers
  const availableInstances = useMemo(() => {
    return instances
      .filter((instance) => {
        if (instance.id === sourceInstanceId) return false
        if (!instance.connected) return false

        const targetCaps = {
          local: instance.transferCapabilities?.local ?? instance.hasLocalFilesystemAccess,
          ssh: instance.transferCapabilities?.ssh ?? false,
        }

        return canTransferBetween(sourceCaps, targetCaps)
      })
      .map((instance) => ({
        ...instance,
        transferMethod: getTransferMethod(sourceCaps, {
          local: instance.transferCapabilities?.local ?? instance.hasLocalFilesystemAccess,
          ssh: instance.transferCapabilities?.ssh ?? false,
        }),
      }))
  }, [instances, sourceInstanceId, sourceCaps])

  const handleTargetChange = (targetId: string) => {
    onChange({
      ...value,
      targetInstanceId: targetId ? parseInt(targetId, 10) : null,
    })
  }

  const handleFileExistsActionChange = (action: FileExistsAction) => {
    onChange({ ...value, fileExistsAction: action })
  }

  const handleSourceActionChange = (action: SourceAction) => {
    onChange({ ...value, sourceAction: action })
  }

  const handleVerifyTransferChange = (checked: boolean) => {
    onChange({ ...value, verifyTransfer: checked })
  }

  const handlePreserveCategoryChange = (checked: boolean) => {
    onChange({ ...value, preserveCategory: checked })
  }

  const handlePreserveTagsChange = (checked: boolean) => {
    onChange({ ...value, preserveTags: checked })
  }

  // No available instances warning
  if (availableInstances.length === 0) {
    return (
      <div className="p-4 border rounded-md bg-muted/50">
        <div className="flex items-start gap-2">
          <AlertTriangle className="h-5 w-5 text-warning mt-0.5 shrink-0" />
          <div className="text-sm">
            <p className="font-medium">No available instances</p>
            <p className="text-muted-foreground mt-1">
              {disabledReason || "Other instances must be connected and have either local filesystem access or SSH configured to receive transfers."}
            </p>
          </div>
        </div>
      </div>
    )
  }

  const isCompact = variant === "compact"

  return (
    <div className={isCompact ? "flex flex-col gap-3" : "space-y-4"}>
      {/* Target Instance */}
      <div className={isCompact ? "space-y-1" : "space-y-2"}>
        <Label className={isCompact ? "text-xs" : undefined}>Target Instance</Label>
        <Select
          value={value.targetInstanceId?.toString() ?? ""}
          onValueChange={handleTargetChange}
        >
          <SelectTrigger className={isCompact ? "w-fit min-w-[160px]" : undefined}>
            <SelectValue placeholder="Select instance" />
          </SelectTrigger>
          <SelectContent>
            {availableInstances.map((instance) => (
              <SelectItem key={instance.id} value={instance.id.toString()}>
                <div className="flex items-center gap-2">
                  <span>{instance.name}</span>
                  {instance.transferMethod === "ssh" && (
                    <Badge variant="outline" className="text-xs px-1.5 py-0">
                      SSH
                    </Badge>
                  )}
                </div>
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      {/* Options */}
      <div className={isCompact ? "flex flex-col gap-2" : "space-y-3"}>
        {/* File exists action */}
        <div className={isCompact ? "space-y-1" : "space-y-1.5"}>
          <Label className={isCompact ? "text-xs" : undefined}>If file exists</Label>
          {showDescriptions && !isCompact && (
            <p className="text-xs text-muted-foreground">
              What to do if a file already exists at the target
            </p>
          )}
          <Select
            value={value.fileExistsAction}
            onValueChange={(v) => handleFileExistsActionChange(v as FileExistsAction)}
          >
            <SelectTrigger className={isCompact ? "w-fit min-w-[140px]" : "w-[200px]"}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="abort">
                <div className="flex flex-col">
                  <span>Abort</span>
                  {!isCompact && <span className="text-xs text-muted-foreground">Fail if file exists</span>}
                </div>
              </SelectItem>
              <SelectItem value="skip">
                <div className="flex flex-col">
                  <span>Skip identical</span>
                  {!isCompact && <span className="text-xs text-muted-foreground">Skip if same size, error if different</span>}
                </div>
              </SelectItem>
              <SelectItem value="overwrite">
                <div className="flex flex-col">
                  <span>Overwrite</span>
                  {!isCompact && <span className="text-xs text-muted-foreground">Replace existing files</span>}
                </div>
              </SelectItem>
            </SelectContent>
          </Select>
        </div>

        {/* Source action */}
        <div className={isCompact ? "space-y-1" : "space-y-1.5"}>
          <Label className={isCompact ? "text-xs" : undefined}>After transfer</Label>
          {showDescriptions && !isCompact && (
            <p className="text-xs text-muted-foreground">
              What to do with the source torrent after transfer completes
            </p>
          )}
          <Select
            value={value.sourceAction}
            onValueChange={(v) => handleSourceActionChange(v as SourceAction)}
          >
            <SelectTrigger className={isCompact ? "w-fit min-w-[140px]" : "w-[200px]"}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="keep">
                <div className="flex flex-col">
                  <span>Keep seeding</span>
                  {!isCompact && <span className="text-xs text-muted-foreground">Leave source torrent running</span>}
                </div>
              </SelectItem>
              <SelectItem value="pause">
                <div className="flex flex-col">
                  <span>Pause source</span>
                  {!isCompact && <span className="text-xs text-muted-foreground">Pause torrent on source instance</span>}
                </div>
              </SelectItem>
              <SelectItem value="delete">
                <div className="flex flex-col">
                  <span>Delete source</span>
                  {!isCompact && <span className="text-xs text-muted-foreground">Remove torrent and files from source</span>}
                </div>
              </SelectItem>
            </SelectContent>
          </Select>
        </div>

        {/* Verify transfer */}
        <div className={isCompact ? "flex items-center gap-2" : "flex items-center justify-between"}>
          {isCompact ? (
            <>
              <Switch
                id="move-verify-transfer"
                checked={value.verifyTransfer}
                onCheckedChange={handleVerifyTransferChange}
              />
              <Label htmlFor="move-verify-transfer" className="text-sm cursor-pointer whitespace-nowrap">
                Verify transfer
              </Label>
            </>
          ) : (
            <>
              <div className="space-y-0.5">
                <Label htmlFor="move-verify-transfer">Verify transfer</Label>
                {showDescriptions && (
                  <p className="text-xs text-muted-foreground">
                    Verify checksums after transfer (slower but safer)
                  </p>
                )}
              </div>
              <Switch
                id="move-verify-transfer"
                checked={value.verifyTransfer}
                onCheckedChange={handleVerifyTransferChange}
              />
            </>
          )}
        </div>

        {/* Preserve category */}
        <div className={isCompact ? "flex items-center gap-2" : "flex items-center justify-between"}>
          {isCompact ? (
            <>
              <Switch
                id="move-preserve-category"
                checked={value.preserveCategory}
                onCheckedChange={handlePreserveCategoryChange}
              />
              <Label htmlFor="move-preserve-category" className="text-sm cursor-pointer whitespace-nowrap">
                Preserve category
              </Label>
            </>
          ) : (
            <>
              <div className="space-y-0.5">
                <Label htmlFor="move-preserve-category">Preserve category</Label>
                {showDescriptions && (
                  <p className="text-xs text-muted-foreground">
                    Keep the same category on the target instance
                  </p>
                )}
              </div>
              <Switch
                id="move-preserve-category"
                checked={value.preserveCategory}
                onCheckedChange={handlePreserveCategoryChange}
              />
            </>
          )}
        </div>

        {/* Preserve tags */}
        <div className={isCompact ? "flex items-center gap-2" : "flex items-center justify-between"}>
          {isCompact ? (
            <>
              <Switch
                id="move-preserve-tags"
                checked={value.preserveTags}
                onCheckedChange={handlePreserveTagsChange}
              />
              <Label htmlFor="move-preserve-tags" className="text-sm cursor-pointer whitespace-nowrap">
                Preserve tags
              </Label>
            </>
          ) : (
            <>
              <div className="space-y-0.5">
                <Label htmlFor="move-preserve-tags">Preserve tags</Label>
                {showDescriptions && (
                  <p className="text-xs text-muted-foreground">
                    Keep the same tags on the target instance
                  </p>
                )}
              </div>
              <Switch
                id="move-preserve-tags"
                checked={value.preserveTags}
                onCheckedChange={handlePreserveTagsChange}
              />
            </>
          )}
        </div>
      </div>
    </div>
  )
})

export default MoveInstanceOptions
