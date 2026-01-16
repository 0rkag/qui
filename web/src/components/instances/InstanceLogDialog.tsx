/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle
} from "@/components/ui/dialog"
import { ScrollText } from "lucide-react"
import { InstanceLogViewer } from "./InstanceLogViewer"

interface InstanceLogDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  instanceId: number
  instanceName: string
}

export function InstanceLogDialog({
  open,
  onOpenChange,
  instanceId,
  instanceName,
}: InstanceLogDialogProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-5xl h-[80vh] flex flex-col">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <ScrollText className="h-5 w-5" />
            qBittorrent Logs
          </DialogTitle>
          <DialogDescription>
            Real-time logs from{" "}
            <strong className="truncate max-w-xs inline-block align-bottom" title={instanceName}>
              {instanceName}
            </strong>
          </DialogDescription>
        </DialogHeader>

        <div className="flex-1 min-h-0">
          <InstanceLogViewer instanceId={instanceId} />
        </div>
      </DialogContent>
    </Dialog>
  )
}
