/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { TorrentManagementBar } from "@/components/torrents/TorrentManagementBar"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu"
import { Input } from "@/components/ui/input"
import { Logo } from "@/components/ui/Logo"
import { NapsterLogo } from "@/components/ui/NapsterLogo"
import { SwizzinLogo } from "@/components/ui/SwizzinLogo"
import { ThemeToggle } from "@/components/ui/ThemeToggle"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger
} from "@/components/ui/tooltip"
import { useLayoutRoute } from "@/contexts/LayoutRouteContext"
import { useTorrentSelection } from "@/contexts/TorrentSelectionContext"
import { useAuth } from "@/hooks/useAuth"
import { useCrossSeedInstanceState } from "@/hooks/useCrossSeedInstanceState"
import { useDebounce } from "@/hooks/useDebounce"
import { useInstances } from "@/hooks/useInstances"
import { usePersistedCompactViewState } from "@/hooks/usePersistedCompactViewState"
import { useTheme } from "@/hooks/useTheme"
import { api } from "@/lib/api"
import { cn } from "@/lib/utils"
import { useQuery } from "@tanstack/react-query"
import { Link, useLocation, useNavigate, useSearch } from "@tanstack/react-router"
import { Archive, ChevronsUpDown, Download, GitBranch, HardDrive, Home, Info, Loader2, LogOut, Menu, Palette, Rss, Search, SearchCode, Server, Settings, X, Zap } from "lucide-react"
import { type ReactNode, useCallback, useEffect, useMemo, useRef, useState } from "react"
import { useHotkeys } from "react-hotkeys-hook"

interface HeaderProps {
  children?: ReactNode
  sidebarCollapsed?: boolean
}

export function Header({
  children,
  sidebarCollapsed = false,
}: HeaderProps) {
  const { logout } = useAuth()
  const navigate = useNavigate()
  const location = useLocation()
  const routeSearch = useSearch({ strict: false }) as { q?: string; modal?: string;[key: string]: unknown }
  const { state: layoutRouteState } = useLayoutRoute()

  // Get selection state from context
  const {
    selectedHashes,
    selectedTorrents,
    isAllSelected,
    totalSelectionCount,
    selectedTotalSize,
    excludeHashes,
    filters,
    clearSelection,
  } = useTorrentSelection()

  const selectedInstanceId = layoutRouteState.instanceId
  const isInstanceRoute = selectedInstanceId !== null
  const shouldShowInstanceControls = layoutRouteState.showInstanceControls && isInstanceRoute

  const [searchValue, setSearchValue] = useState<string>(routeSearch?.q || "")
  const debouncedSearch = useDebounce(searchValue, 500)
  const { instances } = useInstances()
  const activeInstances = useMemo(
    () => (instances ?? []).filter(instance => instance.isActive),
    [instances]
  )

  const instanceName = useMemo(() => {
    if (!isInstanceRoute || !instances || selectedInstanceId === null) return null
    return instances.find(i => i.id === selectedInstanceId)?.name ?? null
  }, [isInstanceRoute, instances, selectedInstanceId])
  const hasMultipleActiveInstances = activeInstances.length > 1

  // Keep local state in sync with URL when navigating between instances/routes
  useEffect(() => {
    setSearchValue(routeSearch?.q || "")
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedInstanceId])

  // Update URL search param after debounce
  useEffect(() => {
    if (!shouldShowInstanceControls) return
    const trimmedSearch = debouncedSearch.trim()
    const next = { ...(routeSearch || {}) }
    if (trimmedSearch) next.q = trimmedSearch
    else delete next.q
    navigate({ search: next as any, replace: true }) // eslint-disable-line @typescript-eslint/no-explicit-any
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [debouncedSearch, shouldShowInstanceControls])

  const isGlobSearch = !!searchValue && /[*?[\]]/.test(searchValue)
  const searchInputRef = useRef<HTMLInputElement>(null)

  // Detect platform for appropriate key display
  const isMac = typeof navigator !== "undefined" && /Mac|iPhone|iPad|iPod/.test(navigator.userAgent)
  const shortcutKey = isMac ? "⌘K" : "Ctrl+K"

  // Global keyboard shortcut to focus search
  useHotkeys(
    "meta+k, ctrl+k",
    (event) => {
      event.preventDefault()
      searchInputRef.current?.focus()
    },
    {
      preventDefault: true,
      enableOnFormTags: ["input", "textarea", "select"],
      enabled: shouldShowInstanceControls,
    },
    [shouldShowInstanceControls]
  )
  const { theme } = useTheme()
  const { viewMode } = usePersistedCompactViewState("normal")

  // Query for available updates
  const { data: updateInfo } = useQuery({
    queryKey: ["latest-version"],
    queryFn: () => api.getLatestVersion(),
    refetchInterval: 2 * 60 * 1000,
    refetchOnMount: false,
    refetchOnWindowFocus: false,
  })

  const { state: crossSeedInstanceState } = useCrossSeedInstanceState()

  // Dense mode uses reduced header height
  const headerHeight = viewMode === "dense" ? "lg:h-12" : "lg:h-16"
  const innerHeight = viewMode === "dense" ? "h-10 lg:h-auto" : "h-12 lg:h-auto"
  const smInnerHeight = viewMode === "dense" ? "sm:h-10 lg:h-auto" : "sm:h-12 lg:h-auto"

  return (
    <header className={cn("sticky top-0 z-50 flex flex-wrap lg:flex-nowrap items-center justify-between border-b bg-background px-4 md:pl-4 md:pr-4 lg:pl-0 lg:static py-2 lg:py-0", headerHeight)}>
      <div className={cn("flex items-center gap-2 mr-2 order-first sm:order-1 lg:order-none", innerHeight)}>
        {children}
        {instanceName && hasMultipleActiveInstances ? (
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <button
                className={cn(
                  "group flex items-center gap-2 text-xl font-semibold transition-opacity duration-300 hover:opacity-90 rounded-sm px-1 -mx-1 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2",
                  "lg:hidden", // Hidden on desktop by default
                  sidebarCollapsed && "lg:flex" // Visible on desktop when sidebar collapsed
                )}
                aria-label={`Current instance: ${instanceName}. Click to switch instances.`}
                aria-haspopup="menu"
              >
                {theme === "swizzin" ? (
                  <SwizzinLogo className="h-5 w-5" />
                ) : theme === "napster" ? (
                  <NapsterLogo className="h-5 w-5" />
                ) : (
                  <Logo className="h-5 w-5" />
                )}
                <span className="flex items-center max-w-32">
                  <span className="truncate">{instanceName}</span>
                  <ChevronsUpDown className="h-3 w-3 text-muted-foreground ml-1 mt-0.5 opacity-60 flex-shrink-0" />
                </span>
              </button>
            </DropdownMenuTrigger>
            <DropdownMenuContent className="w-64 mt-2" side="bottom" align="start">
              <DropdownMenuLabel className="text-xs font-semibold text-muted-foreground uppercase tracking-wide">
                Switch Instance
              </DropdownMenuLabel>
              <DropdownMenuSeparator />
              <div className="max-h-64 overflow-y-auto space-y-1">
                {activeInstances.length > 0 ? (
                  activeInstances.map((instance) => (
                    <DropdownMenuItem key={instance.id} asChild>
                      <Link
                        to="/instances/$instanceId"
                        params={{ instanceId: instance.id.toString() }}
                        className={cn(
                          "flex items-center gap-2 cursor-pointer rounded-sm px-2 py-1.5 text-sm focus-visible:outline-none",
                          instance.id === selectedInstanceId ? "bg-accent text-accent-foreground font-medium" : "hover:bg-accent/80 data-[highlighted]:bg-accent/80 text-foreground"
                        )}
                      >
                        <HardDrive className="h-4 w-4 flex-shrink-0" aria-hidden="true" />
                        <span className="flex-1 truncate">{instance.name}</span>
                        <span
                          className={cn(
                            "h-2 w-2 rounded-full flex-shrink-0",
                            instance.connected ? "bg-success" : "bg-destructive"
                          )}
                          aria-label={instance.connected ? "Connected" : "Disconnected"}
                        />
                      </Link>
                    </DropdownMenuItem>
                  ))
                ) : (
                  <p className="px-2 py-1.5 text-xs text-muted-foreground">No active instances</p>
                )}
              </div>
            </DropdownMenuContent>
          </DropdownMenu>
        ) : (
          <h1 className={cn(
            "flex items-center gap-2 text-xl font-semibold",
            "lg:hidden", // Hidden on desktop by default
            sidebarCollapsed && "lg:flex" // Visible on desktop when sidebar collapsed
          )}>
            {theme === "swizzin" ? (
              <SwizzinLogo className="h-5 w-5" />
            ) : theme === "napster" ? (
              <NapsterLogo className="h-5 w-5" />
            ) : (
              <Logo className="h-5 w-5" />
            )}
            {instanceName ? (
              <span className="truncate max-w-32">{instanceName}</span>
            ) : "qui"}
          </h1>
        )}
      </div>

      {/* Navigation buttons - hidden on mobile, visible from sm up when sidebar is hidden */}
      <nav
        className={cn(
          "hidden sm:flex items-center gap-1 order-2 lg:order-none",
          innerHeight,
          sidebarCollapsed ? "lg:flex lg:ml-2" : "lg:hidden"
        )}
        aria-label="Main navigation"
      >
        {/* Main navigation items - 44px minimum touch targets for accessibility */}
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              variant={location.pathname === "/dashboard" ? "secondary" : "ghost"}
              size="icon"
              className="h-8 w-8 min-h-[44px] min-w-[44px]"
              aria-label="Dashboard"
              aria-current={location.pathname === "/dashboard" ? "page" : undefined}
              asChild
            >
              <Link to="/dashboard">
                <Home className="h-4 w-4" aria-hidden="true" />
              </Link>
            </Button>
          </TooltipTrigger>
          <TooltipContent>Dashboard</TooltipContent>
        </Tooltip>
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              variant={location.pathname === "/search" ? "secondary" : "ghost"}
              size="icon"
              className="h-8 w-8 min-h-[44px] min-w-[44px]"
              aria-label="Search"
              aria-current={location.pathname === "/search" ? "page" : undefined}
              asChild
            >
              <Link to="/search">
                <Search className="h-4 w-4" aria-hidden="true" />
              </Link>
            </Button>
          </TooltipTrigger>
          <TooltipContent>Search</TooltipContent>
        </Tooltip>
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              variant={location.pathname === "/cross-seed" ? "secondary" : "ghost"}
              size="icon"
              className="h-8 w-8 min-h-[44px] min-w-[44px]"
              aria-label="Cross-Seed"
              aria-current={location.pathname === "/cross-seed" ? "page" : undefined}
              asChild
            >
              <Link to="/cross-seed">
                <GitBranch className="h-4 w-4" aria-hidden="true" />
              </Link>
            </Button>
          </TooltipTrigger>
          <TooltipContent>Cross-Seed</TooltipContent>
        </Tooltip>
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              variant={location.pathname === "/automations" ? "secondary" : "ghost"}
              size="icon"
              className="h-8 w-8 min-h-[44px] min-w-[44px]"
              aria-label="Automations"
              aria-current={location.pathname === "/automations" ? "page" : undefined}
              asChild
            >
              <Link to="/automations">
                <Zap className="h-4 w-4" aria-hidden="true" />
              </Link>
            </Button>
          </TooltipTrigger>
          <TooltipContent>Automations</TooltipContent>
        </Tooltip>
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              variant={location.pathname === "/backups" ? "secondary" : "ghost"}
              size="icon"
              className="h-8 w-8 min-h-[44px] min-w-[44px]"
              aria-label="Backups"
              aria-current={location.pathname === "/backups" ? "page" : undefined}
              asChild
            >
              <Link to="/backups">
                <Archive className="h-4 w-4" aria-hidden="true" />
              </Link>
            </Button>
          </TooltipTrigger>
          <TooltipContent>Backups</TooltipContent>
        </Tooltip>
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              variant={location.pathname.startsWith("/settings") ? "secondary" : "ghost"}
              size="icon"
              className="h-8 w-8 min-h-[44px] min-w-[44px]"
              aria-label="Settings"
              aria-current={location.pathname.startsWith("/settings") ? "page" : undefined}
              asChild
            >
              <Link to="/settings">
                <Settings className="h-4 w-4" aria-hidden="true" />
              </Link>
            </Button>
          </TooltipTrigger>
          <TooltipContent>Settings</TooltipContent>
        </Tooltip>
      </nav>
      {/* Management Bar - only shows when torrents selected on instance routes */}
      {shouldShowInstanceControls && (selectedHashes.length > 0 || isAllSelected) && (
        <div className="sm:w-full sm:basis-full lg:basis-auto lg:w-auto sm:order-5 lg:order-none flex justify-center lg:justify-start lg:ml-2 animate-in fade-in duration-400 ease-out motion-reduce:animate-none motion-reduce:duration-0">
          <TorrentManagementBar
            instanceId={selectedInstanceId || undefined}
            selectedHashes={selectedHashes}
            selectedTorrents={selectedTorrents}
            isAllSelected={isAllSelected}
            totalSelectionCount={totalSelectionCount}
            totalSelectionSize={selectedTotalSize}
            filters={filters}
            search={routeSearch?.q}
            excludeHashes={excludeHashes}
            onComplete={clearSelection}
          />
        </div>
      )}
      {/* Instance route - search on right */}
      {shouldShowInstanceControls && (
        <div className={cn("flex items-center flex-1 gap-2 sm:order-3 lg:order-none", smInnerHeight)}>
          {/* Search bar */}
          <div className="flex items-center gap-2 flex-1 justify-end mr-2">
            {/* Search bar - will-change applied via focus-within to avoid permanent compositor layer */}
            <div className="relative flex-1 min-w-0 md:w-62 md:flex-initial md:focus-within:w-full md:focus-within:will-change-[width] max-w-md transition-[width] duration-100 ease-out">
              <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground pointer-events-none" aria-hidden="true" />
              <Input
                ref={searchInputRef}
                placeholder={isGlobSearch ? "Glob pattern..." : `Search torrents... (${shortcutKey})`}
                value={searchValue}
                onChange={(e) => setSearchValue(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter") {
                    const next = { ...(routeSearch || {}) }
                    const trimmedValue = searchValue.trim()
                    if (trimmedValue) next.q = trimmedValue
                    else delete next.q
                    navigate({ search: next as any, replace: true }) // eslint-disable-line @typescript-eslint/no-explicit-any
                  } else if (e.key === "Escape") {
                    // Clear search and blur the input
                    e.preventDefault()
                    e.stopPropagation() // Prevent event from bubbling to table selection handler
                    if (searchValue) {
                      setSearchValue("")
                    }
                    // Delay blur to match animation duration
                    setTimeout(() => {
                      searchInputRef.current?.blur()
                    }, 100)
                  }
                }}
                className={cn(
                  "w-full pl-9 pr-16 transition-[box-shadow,border-color] duration-200 text-xs",
                  searchValue && "ring-1 ring-primary/50",
                  isGlobSearch && "ring-1 ring-primary"
                )}
              />
              <div className="absolute right-2 top-1/2 -translate-y-1/2 flex items-center gap-0.5">
                {/* Clear search button */}
                {searchValue && (
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <button
                        type="button"
                        className="p-2 min-h-[44px] min-w-[44px] flex items-center justify-center hover:bg-muted rounded-sm transition-colors hidden sm:flex"
                        aria-label="Clear search"
                        onClick={() => {
                          setSearchValue("")
                          const next = { ...(routeSearch || {}) }
                          delete next.q
                          navigate({ search: next as any, replace: true }) // eslint-disable-line @typescript-eslint/no-explicit-any
                        }}
                      >
                        <X className="h-3.5 w-3.5 text-muted-foreground" aria-hidden="true" />
                      </button>
                    </TooltipTrigger>
                    <TooltipContent>Clear search</TooltipContent>
                  </Tooltip>
                )}
                {/* Info tooltip */}
                <Tooltip>
                  <TooltipTrigger asChild>
                    <button
                      type="button"
                      className="p-2 min-h-[44px] min-w-[44px] flex items-center justify-center hover:bg-muted rounded-sm transition-colors hidden sm:flex"
                      aria-label="Search help"
                      onClick={(e) => e.preventDefault()}
                    >
                      <Info className="h-3.5 w-3.5 text-muted-foreground" aria-hidden="true" />
                    </button>
                  </TooltipTrigger>
                  <TooltipContent className="max-w-xs">
                    <div className="space-y-2 text-xs">
                      <p className="font-semibold">Search tips</p>
                      <ul className="space-y-1 ml-2">
                        <li>• Use * as wildcard: <strong>*.mkv</strong> or <strong>*1080p*</strong></li>
                        <li>• Spaces work: "breaking bad" finds "Breaking.Bad"</li>
                        <li>• Searches name, category, and tags</li>
                        <li>• Press Enter or wait to search</li>
                      </ul>
                    </div>
                  </TooltipContent>
                </Tooltip>
              </div>
            </div>
            <span id="header-search-actions" className="flex items-center gap-1" />
          </div>
        </div>
      )}

      <div className={cn("flex items-center gap-1 order-last sm:order-4 lg:order-none ml-auto", smInnerHeight)}>
        {/* Theme toggle - hidden on mobile, shown in hamburger menu instead */}
        <div className="hidden sm:block">
          <ThemeToggle />
        </div>
        {/* Hamburger menu - visible on mobile (<sm), hidden sm-lg, visible on lg+ only when sidebar collapsed */}
        <div className={cn(
          "sm:hidden", // Hide from sm breakpoint up by default
          "lg:block lg:transition-[width,opacity] lg:duration-300 lg:ease-out lg:overflow-hidden", // Show on lg+ with animation
          sidebarCollapsed ? "lg:w-10 lg:opacity-100" : "lg:w-0 lg:opacity-0"
        )}>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button
                variant="ghost"
                size="icon"
                className="h-12 w-12 hover:bg-muted hover:text-foreground transition-colors relative"
                aria-label={updateInfo ? "Open menu (update available)" : "Open menu"}
              >
                <Menu className="h-6 w-6" aria-hidden="true" />
                {updateInfo && (
                  <span
                    className="absolute top-1 right-1 h-2 w-2 bg-success rounded-full"
                    aria-hidden="true"
                  />
                )}
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-52">
              {updateInfo && (
                <>
                  <DropdownMenuItem asChild>
                    <a
                      href={updateInfo.html_url}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="flex items-center gap-2 text-success cursor-pointer"
                    >
                      <Download className="mr-2 h-4 w-4" aria-hidden="true" />
                      <div className="flex flex-col">
                        <span className="font-medium">Update Available</span>
                        <span className="text-[10px] text-muted-foreground">Version {updateInfo.tag_name}</span>
                      </div>
                    </a>
                  </DropdownMenuItem>
                  <DropdownMenuSeparator />
                </>
              )}
              <DropdownMenuItem asChild>
                <Link
                  to="/dashboard"
                  className="flex cursor-pointer"
                >
                  <Home className="mr-2 h-4 w-4" aria-hidden="true" />
                  Dashboard
                </Link>
              </DropdownMenuItem>
              <DropdownMenuItem asChild>
                <Link
                  to="/search"
                  className="flex cursor-pointer"
                >
                  <Search className="mr-2 h-4 w-4" aria-hidden="true" />
                  Search
                </Link>
              </DropdownMenuItem>
              <DropdownMenuItem asChild>
                <Link
                  to="/cross-seed"
                  className="flex cursor-pointer"
                >
                  <GitBranch className="mr-2 h-4 w-4" aria-hidden="true" />
                  Cross-Seed
                </Link>
              </DropdownMenuItem>
              <DropdownMenuItem asChild>
                <Link
                  to="/automations"
                  className="flex cursor-pointer"
                >
                  <Zap className="mr-2 h-4 w-4" aria-hidden="true" />
                  Automations
                </Link>
              </DropdownMenuItem>
              <DropdownMenuItem asChild>
                <Link
                  to="/backups"
                  className="flex cursor-pointer"
                >
                  <Archive className="mr-2 h-4 w-4" aria-hidden="true" />
                  Backups
                </Link>
              </DropdownMenuItem>
              <DropdownMenuItem asChild>
                <Link
                  to="/settings"
                  search={{ tab: "instances" }}
                  className="flex cursor-pointer"
                >
                  <Server className="mr-2 h-4 w-4" aria-hidden="true" />
                  Instances
                </Link>
              </DropdownMenuItem>
              {activeInstances.length > 0 && (
                <>
                  {activeInstances.map((instance) => {
                    const csState = crossSeedInstanceState[instance.id]
                    const hasRss = csState?.rssEnabled || csState?.rssRunning
                    const hasSearch = csState?.searchRunning

                    return (
                      <DropdownMenuItem key={instance.id} asChild>
                        <Link
                          to="/instances/$instanceId"
                          params={{ instanceId: instance.id.toString() }}
                          className="flex cursor-pointer pl-6"
                        >
                          <HardDrive className="mr-2 h-4 w-4" aria-hidden="true" />
                          <span className="truncate">{instance.name}</span>
                          <span className="ml-auto flex items-center gap-1.5">
                            {hasRss && (
                              <Tooltip>
                                <TooltipTrigger asChild>
                                  <span className="flex items-center">
                                    {csState?.rssRunning ? (
                                      <Loader2 className="h-3 w-3 animate-spin text-muted-foreground" />
                                    ) : (
                                      <Rss className="h-3 w-3 text-muted-foreground" />
                                    )}
                                  </span>
                                </TooltipTrigger>
                                <TooltipContent side="left" className="text-xs">
                                  {csState?.rssRunning ? "Cross-seed RSS scanning..." : "Cross-seed RSS active"}
                                </TooltipContent>
                              </Tooltip>
                            )}
                            {hasSearch && (
                              <Tooltip>
                                <TooltipTrigger asChild>
                                  <span className="flex items-center">
                                    <SearchCode className="h-3 w-3 text-muted-foreground" />
                                  </span>
                                </TooltipTrigger>
                                <TooltipContent side="left" className="text-xs">
                                  Cross-seed scan in progress...
                                </TooltipContent>
                              </Tooltip>
                            )}
                            <span
                              className={cn(
                                "h-2 w-2 rounded-full flex-shrink-0",
                                instance.connected ? "bg-success" : "bg-destructive"
                              )}
                              role="status"
                              aria-label={instance.connected ? "Connected" : "Disconnected"}
                            />
                          </span>
                        </Link>
                      </DropdownMenuItem>
                    )
                  })}
                </>
              )}
              <DropdownMenuSeparator />
              {/* Appearance - only shown on mobile where ThemeToggle is hidden */}
              <DropdownMenuItem asChild className="sm:hidden">
                <Link
                  to="/settings"
                  search={{ tab: "themes" }}
                  className="flex cursor-pointer"
                >
                  <Palette className="mr-2 h-4 w-4" aria-hidden="true" />
                  Appearance
                </Link>
              </DropdownMenuItem>
              <DropdownMenuItem asChild>
                <Link
                  to="/settings"
                  className="flex cursor-pointer"
                >
                  <Settings className="mr-2 h-4 w-4" aria-hidden="true" />
                  Settings
                </Link>
              </DropdownMenuItem>
              <DropdownMenuSeparator />
              <DropdownMenuItem onClick={() => logout()}>
                <LogOut className="mr-2 h-4 w-4" aria-hidden="true" />
                Logout
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </div>
    </header>
  )
}
