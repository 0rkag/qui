/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Outlet } from "@tanstack/react-router"
import { Header } from "@/components/layout/Header"
import { Sidebar } from "@/components/layout/Sidebar"
import { LayoutRouteProvider } from "@/contexts/LayoutRouteContext"
import { usePersistedSidebarState } from "@/hooks/usePersistedSidebarState"
import { Menu } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { cn } from "@/lib/utils"
import { MobileScrollProvider } from "@/contexts/MobileScrollContext"
import { TorrentSelectionProvider } from "@/contexts/TorrentSelectionContext"
import { ThemeValidator } from "@/components/themes/ThemeValidator"

function AppLayoutContent() {
  const [sidebarCollapsed, setSidebarCollapsed] = usePersistedSidebarState(false) // Desktop: persisted state

  return (
    <div className="flex h-[100dvh] bg-background">
      {/* Desktop Sidebar - Collapsible using transform for GPU acceleration */}
      <div
        className={cn(
          "hidden lg:block w-64 flex-shrink-0 transition-[transform,opacity,margin] duration-300 ease-out",
          sidebarCollapsed ? "-translate-x-full opacity-0 -mr-64" : "translate-x-0 opacity-100 mr-0"
        )}
        aria-hidden={sidebarCollapsed}
      >
        <Sidebar />
      </div>

      <div className="flex flex-1 flex-col min-w-0 relative">
        <Header
          sidebarCollapsed={sidebarCollapsed}
        >
          {/* Desktop toggle button */}
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="icon"
                onClick={() => setSidebarCollapsed(!sidebarCollapsed)}
                className="hidden lg:flex"
                aria-label={sidebarCollapsed ? "Show sidebar" : "Hide sidebar"}
                aria-expanded={!sidebarCollapsed}
              >
                <Menu
                  className={cn(
                    "h-5 w-5 transition-transform duration-300",
                    sidebarCollapsed && "rotate-90"
                  )}
                  aria-hidden="true"
                />
              </Button>
            </TooltipTrigger>
            <TooltipContent side="bottom">
              {sidebarCollapsed ? "Show sidebar" : "Hide sidebar"}
            </TooltipContent>
          </Tooltip>
        </Header>
        <main className="flex-1 overflow-y-auto">
          <Outlet />
        </main>
      </div>
    </div>
  )
}

export function AppLayout() {
  return (
    <LayoutRouteProvider>
      <ThemeValidator />
      <TorrentSelectionProvider>
        <MobileScrollProvider>
          <AppLayoutContent />
        </MobileScrollProvider>
      </TorrentSelectionProvider>
    </LayoutRouteProvider>
  )
}
