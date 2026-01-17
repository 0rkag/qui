/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button"
import { api } from "@/lib/api"
import { cn } from "@/lib/utils"
import { useQuery } from "@tanstack/react-query"
import { Download, X } from "lucide-react"
import { useState } from "react"

export function UpdateBanner() {
  const [dismissed, setDismissed] = useState(false)

  const { data: updateInfo } = useQuery({
    queryKey: ["latest-version"],
    queryFn: () => api.getLatestVersion(),
    // Check for updates every 2 minutes
    refetchInterval: 2 * 60 * 1000,
    // Don't show loading state on mount, run in background
    refetchOnMount: false,
    refetchOnWindowFocus: false,
  })

  // Don't show banner if dismissed or no update available
  if (dismissed || !updateInfo) {
    return null
  }

  const handleViewUpdate = () => {
    window.open(updateInfo.html_url, "_blank", "noopener,noreferrer")
  }

  const handleDismiss = () => {
    setDismissed(true)
  }

  return (
    <div className="mb-3 rounded-md border border-success/30 bg-success/10 p-3">
      <div className="flex items-start gap-2">
        <Download className="h-4 w-4 text-success mt-0.5 flex-shrink-0" />
        <div className="flex-1 min-w-0">
          <p className="text-sm font-medium text-success">
            Update Available
          </p>
          <p className="text-xs text-success/80 mt-1">
            Version {updateInfo.tag_name} is now available
          </p>
          <Button
            size="sm"
            variant="outline"
            className="mt-2 h-6 text-xs border-success/50 text-success hover:bg-success/20"
            onClick={handleViewUpdate}
          >
            View Release
          </Button>
        </div>
        <Button
          size="icon"
          variant="ghost"
          className="h-4 w-4 text-success hover:text-success/80"
          onClick={handleDismiss}
        >
          <X className="h-3 w-3" />
          <span className="sr-only">Dismiss</span>
        </Button>
      </div>
    </div>
  )
}