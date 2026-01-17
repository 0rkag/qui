/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useEffect, useRef, useState, useCallback } from "react"
import { Terminal as TerminalIcon } from "lucide-react"
import { Terminal } from "@xterm/xterm"
import { FitAddon } from "@xterm/addon-fit"
import { WebLinksAddon } from "@xterm/addon-web-links"
import "@xterm/xterm/css/xterm.css"

import { Dialog, DialogContent, DialogTitle } from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import type { InstanceConnection } from "@/types"

interface SSHTerminalProps {
  connection: InstanceConnection
  instanceId: number
  instanceName: string
  open: boolean
  onOpenChange: (open: boolean) => void
}

// Background color: light black with slight blue & green tone
// RGB: ~13, 17, 20 with a hint of teal
const BG_COLOR = "rgba(12, 18, 22, 0.80)"

export function SSHTerminal({ connection, instanceId, instanceName, open, onOpenChange }: SSHTerminalProps) {
  const terminalRef = useRef<HTMLDivElement>(null)
  const terminalInstance = useRef<Terminal | null>(null)
  const fitAddon = useRef<FitAddon | null>(null)
  const wsRef = useRef<WebSocket | null>(null)
  const [isConnected, setIsConnected] = useState(false)

  const sendResize = useCallback(() => {
    if (wsRef.current?.readyState === WebSocket.OPEN && terminalInstance.current) {
      const dims = { cols: terminalInstance.current.cols, rows: terminalInstance.current.rows }
      wsRef.current.send(JSON.stringify({ type: "resize", cols: dims.cols, rows: dims.rows }))
    }
  }, [])

  const connect = useCallback(() => {
    if (!terminalRef.current) return

    setIsConnected(false)

    // Initialize xterm with Pragmasevka font
    const term = new Terminal({
      cursorBlink: true,
      fontSize: 14,
      fontFamily: "'Pragmasevka', 'PragmataPro', 'JetBrains Mono', 'Fira Code', 'SF Mono', Menlo, Monaco, monospace",
      lineHeight: 1.2,
      letterSpacing: 0,
      theme: {
        // Transparent background - the container provides the styled background
        background: "transparent",
        foreground: "#b8c4ce",
        cursor: "#5fb3a1",
        cursorAccent: "transparent",
        selectionBackground: "rgba(95, 179, 161, 0.25)",
        selectionForeground: "#e0e6eb",
        black: "#3a4349",
        red: "#e06c75",
        green: "#7ec699",
        yellow: "#d4a656",
        blue: "#6eb4e0",
        magenta: "#c9a0dc",
        cyan: "#5fb3a1",
        white: "#b8c4ce",
        brightBlack: "#5c6670",
        brightRed: "#e88993",
        brightGreen: "#a3d9b1",
        brightYellow: "#e5c07b",
        brightBlue: "#8ed0f9",
        brightMagenta: "#ddb6eb",
        brightCyan: "#88cfbe",
        brightWhite: "#e0e6eb",
      },
      allowProposedApi: true,
    })

    const fit = new FitAddon()
    const webLinks = new WebLinksAddon()

    term.loadAddon(fit)
    term.loadAddon(webLinks)
    term.open(terminalRef.current)

    // Small delay to ensure proper sizing
    requestAnimationFrame(() => {
      fit.fit()
    })

    terminalInstance.current = term
    fitAddon.current = fit

    // Build WebSocket URL
    const protocol = window.location.protocol === "https:" ? "wss:" : "ws:"
    const wsUrl = `${protocol}//${window.location.host}/api/instances/${instanceId}/connections/${connection.id}/terminal`

    const ws = new WebSocket(wsUrl)
    wsRef.current = ws

    ws.onopen = () => {
      setIsConnected(true)
      sendResize()
    }

    ws.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data)
        if (data.type === "error") {
          term.writeln(`\x1b[38;5;203mError: ${data.error}\x1b[0m`)
          return
        }
      } catch {
        // Not JSON, treat as terminal output
      }
      term.write(event.data)
    }

    ws.onerror = () => {
      term.writeln("\x1b[38;5;203mConnection error\x1b[0m")
    }

    ws.onclose = () => {
      setIsConnected(false)
      term.writeln("")
      term.writeln("\x1b[38;5;243mConnection closed\x1b[0m")
    }

    // Handle terminal input
    term.onData((data) => {
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ type: "input", data }))
      }
    })

    // Handle resize
    const resizeObserver = new ResizeObserver(() => {
      requestAnimationFrame(() => {
        if (fitAddon.current) {
          fitAddon.current.fit()
          sendResize()
        }
      })
    })

    if (terminalRef.current) {
      resizeObserver.observe(terminalRef.current)
    }

    return () => {
      resizeObserver.disconnect()
      ws.close()
      term.dispose()
    }
  }, [connection, instanceId, sendResize])

  useEffect(() => {
    if (open) {
      const timer = setTimeout(connect, 50)
      return () => clearTimeout(timer)
    } else {
      if (wsRef.current) {
        wsRef.current.close()
        wsRef.current = null
      }
      if (terminalInstance.current) {
        terminalInstance.current.dispose()
        terminalInstance.current = null
      }
    }
  }, [open, connect])

  // Close on Escape key
  useEffect(() => {
    if (!open) return

    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape" && e.shiftKey) {
        onOpenChange(false)
      }
    }

    window.addEventListener("keydown", handleKeyDown)
    return () => window.removeEventListener("keydown", handleKeyDown)
  }, [open, onOpenChange])

  const hostLabel = `${connection.username}@${instanceName}`

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        className="!max-w-none !p-1 !border-none !rounded-lg w-[95vw] h-[85vh] md:w-[85vw] md:h-[80vh] lg:w-[70vw] lg:h-[70vh] data-[state=open]:animate-none data-[state=closed]:animate-none overflow-hidden"
        showCloseButton={false}
        style={{
          background: BG_COLOR,
        }}
      >
        {/* Hidden title for accessibility */}
        <DialogTitle className="sr-only">SSH Terminal - {hostLabel}</DialogTitle>

        {/* Subtle host label in top-right corner */}
        <div
          className="absolute top-0 right-0 z-10 select-none pointer-events-none"
          style={{
            padding: "0.5em 0.75em",
            color: isConnected ? "rgba(127, 176, 158, 0.5)" : "rgba(180, 140, 140, 0.4)",
            letterSpacing: "0.02em",
          }}
        >
          {hostLabel}
        </div>

        {/* Terminal container with padding - top for border spacing, others for edge margins */}
        <div
          ref={terminalRef}
          style={{
            fontFamily: "'Pragmasevka', 'PragmataPro', 'JetBrains Mono', monospace",
          }}
          className="w-full h-full"
        />
      </DialogContent>
    </Dialog>
  )
}

// Button to open terminal
interface SSHTerminalButtonProps {
  connection: InstanceConnection
  instanceId: number
  instanceName: string
}

export function SSHTerminalButton({ connection, instanceId, instanceName }: SSHTerminalButtonProps) {
  const [open, setOpen] = useState(false)

  // Only show for SSH/SFTP connections
  if (connection.protocol !== "ssh" && connection.protocol !== "sftp") {
    return null
  }

  return (
    <>
      <Button
        size="icon"
        variant="ghost"
        onClick={() => setOpen(true)}
        title="Open terminal"
        className="text-green-600 hover:text-green-500 hover:bg-green-500/10"
      >
        <TerminalIcon className="h-4 w-4" />
      </Button>
      <SSHTerminal
        connection={connection}
        instanceId={instanceId}
        instanceName={instanceName}
        open={open}
        onOpenChange={setOpen}
      />
    </>
  )
}
