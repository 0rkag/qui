/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useState } from "react"
import {
  AlertCircle,
  CheckCircle2,
  HelpCircle,
  Info,
  Key,
  Loader2,
  Pencil,
  Plus,
  Server,
  Terminal,
  Trash2,
  Wifi,
  WifiOff,
} from "lucide-react"
import { toast } from "sonner"

import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
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
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import {
  Collapsible,
  CollapsibleContent,
} from "@/components/ui/collapsible"
import { useInstanceConnections } from "@/hooks/useInstanceConnections"
import { SSHTerminalButton } from "./SSHTerminal"
import type {
  ConnectionProtocol,
  InstanceConnection,
  InstanceConnectionCreate,
  SSHTestResult,
} from "@/types"

interface RemoteAccessEditorProps {
  instanceId: number
  instanceName: string
}

const PROTOCOL_INFO: Record<ConnectionProtocol, { label: string; icon: typeof Terminal; description: string; defaultPort: number }> = {
  ssh: {
    label: "SSH",
    icon: Terminal,
    description: "Secure shell for remote command execution and file transfers",
    defaultPort: 22,
  },
  sftp: {
    label: "SFTP",
    icon: Server,
    description: "SSH File Transfer Protocol for secure file operations",
    defaultPort: 22,
  },
  ftp: {
    label: "FTP",
    icon: Server,
    description: "File Transfer Protocol (not recommended for security)",
    defaultPort: 21,
  },
}

export function RemoteAccessEditor({ instanceId, instanceName }: RemoteAccessEditorProps) {
  const {
    connections,
    isLoading,
    createConnectionAsync,
    updateConnectionAsync,
    deleteConnectionAsync,
    testConnection,
    testExistingConnection,
    isCreating,
    isUpdating,
    isDeleting,
    isTesting,
  } = useInstanceConnections(instanceId)

  const [editingId, setEditingId] = useState<number | null>(null)
  const [isAdding, setIsAdding] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState<InstanceConnection | null>(null)
  const [showHelp, setShowHelp] = useState(false)
  const [testResult, setTestResult] = useState<SSHTestResult | null>(null)
  const [testingId, setTestingId] = useState<number | null>(null)

  // Form state
  const [formData, setFormData] = useState<InstanceConnectionCreate>({
    protocol: "ssh",
    host: "",
    port: 22,
    username: "",
    privateKeyPath: "",
    enabled: true,
  })

  const resetForm = () => {
    setFormData({
      protocol: "ssh",
      host: "",
      port: 22,
      username: "",
      privateKeyPath: "",
      enabled: true,
    })
    setEditingId(null)
    setIsAdding(false)
    setTestResult(null)
  }

  const handleEdit = (conn: InstanceConnection) => {
    setFormData({
      protocol: conn.protocol,
      host: conn.host,
      port: conn.port,
      username: conn.username,
      privateKeyPath: conn.privateKeyPath ?? "",
      enabled: conn.enabled,
    })
    setEditingId(conn.id)
    setIsAdding(false)
    setTestResult(null)
  }

  const handleAdd = () => {
    resetForm()
    setIsAdding(true)
  }

  const handleSave = async () => {
    if (!formData.host.trim()) {
      toast.error("Host is required")
      return
    }
    if (!formData.username.trim()) {
      toast.error("Username is required")
      return
    }

    try {
      if (editingId !== null) {
        await updateConnectionAsync({
          id: editingId,
          data: {
            host: formData.host,
            port: formData.port ?? PROTOCOL_INFO[formData.protocol].defaultPort,
            username: formData.username,
            privateKeyPath: formData.privateKeyPath,
            enabled: formData.enabled,
          },
        })
        toast.success("Connection updated")
      } else {
        await createConnectionAsync(formData)
        toast.success("Connection created")
      }
      resetForm()
    } catch {
      // Error toast shown by useInstanceConnections hook
    }
  }

  const handleDelete = async () => {
    if (!deleteTarget) return
    try {
      await deleteConnectionAsync(deleteTarget.id)
      toast.success("Connection deleted")
      setDeleteTarget(null)
    } catch {
      // Error toast shown by useInstanceConnections hook
    }
  }

  const handleTestNew = async () => {
    if (!formData.host.trim() || !formData.username.trim()) {
      toast.error("Fill in host and username first")
      return
    }

    setTestResult(null)
    try {
      const result = await testConnection({
        protocol: formData.protocol,
        host: formData.host,
        port: formData.port,
        username: formData.username,
        privateKeyPath: formData.privateKeyPath,
      })
      setTestResult(result)
    } catch {
      // Error toast shown by useInstanceConnections hook
    }
  }

  const handleTestExisting = async (connId: number) => {
    setTestingId(connId)
    setTestResult(null)
    try {
      const result = await testExistingConnection(connId)
      setTestResult(result)
    } catch {
      // Error toast shown by useInstanceConnections hook
    } finally {
      setTestingId(null)
    }
  }

  // Available protocols (exclude already configured ones when adding)
  const availableProtocols = isAdding
    ? (["ssh", "sftp", "ftp"] as ConnectionProtocol[]).filter(
        (p) => !connections.some((c) => c.protocol === p)
      )
    : (["ssh", "sftp", "ftp"] as ConnectionProtocol[])

  if (isLoading) {
    return <div className="text-sm text-muted-foreground">Loading connections...</div>
  }

  return (
    <TooltipProvider>
      <div className="space-y-6">
        {/* Header with status */}
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <span className="text-sm text-muted-foreground">
              {connections.length === 0 ? (
                "No remote connections configured"
              ) : (
                <>
                  {connections.filter((c) => c.enabled).length} of {connections.length} connection
                  {connections.length !== 1 ? "s" : ""} enabled
                </>
              )}
            </span>
          </div>
          <Button variant="ghost" size="sm" onClick={() => setShowHelp(!showHelp)}>
            <HelpCircle className="h-4 w-4 mr-1" />
            {showHelp ? "Hide" : "Why SSH?"}
          </Button>
        </div>

        {/* Collapsible Help Section */}
        <Collapsible open={showHelp} onOpenChange={setShowHelp}>
          <CollapsibleContent>
            <div className="rounded-lg border bg-muted/30 p-4 space-y-3">
              <div className="flex items-start gap-3">
                <Info className="h-5 w-5 text-blue-500 mt-0.5 shrink-0" />
                <div className="space-y-2 text-sm">
                  <p>
                    <strong>SSH connections enable remote file operations</strong> when QUI runs on a
                    different machine than qBittorrent.
                  </p>
                  <div className="space-y-1 text-muted-foreground">
                    <p>With SSH configured, QUI can:</p>
                    <ul className="list-disc list-inside ml-2 space-y-0.5">
                      <li>Create hardlinks on the remote machine</li>
                      <li>Transfer files between instances using rsync</li>
                      <li>Verify file existence before adding torrents</li>
                    </ul>
                  </div>
                  <div className="pt-2 border-t text-xs text-muted-foreground">
                    <strong>Note:</strong> The private key must be accessible on the QUI server, not
                    your local machine.
                  </div>
                </div>
              </div>
            </div>
          </CollapsibleContent>
        </Collapsible>

        {/* Connections List */}
        <div className="space-y-3">
          {connections.length === 0 && !isAdding && (
            <div className="text-sm text-muted-foreground py-8 text-center border border-dashed rounded-md">
              <Terminal className="h-8 w-8 mx-auto mb-2 text-muted-foreground/50" />
              <p>No remote connections configured.</p>
              <p className="text-xs mt-1">
                Add an SSH connection to enable remote file operations.
              </p>
            </div>
          )}

          {connections.map((conn) => (
            <ConnectionCard
              key={conn.id}
              connection={conn}
              instanceId={instanceId}
              instanceName={instanceName}
              isEditing={editingId === conn.id}
              isTesting={testingId === conn.id && isTesting}
              testResult={editingId === conn.id || testingId === conn.id ? testResult : null}
              formData={editingId === conn.id ? formData : undefined}
              onFormChange={editingId === conn.id ? setFormData : undefined}
              onEdit={() => handleEdit(conn)}
              onDelete={() => setDeleteTarget(conn)}
              onTest={() => handleTestExisting(conn.id)}
              onTestNew={editingId === conn.id ? handleTestNew : undefined}
              onSave={handleSave}
              onCancel={resetForm}
              isSaving={isUpdating}
            />
          ))}

          {/* Add new connection form */}
          {isAdding && availableProtocols.length > 0 && (
            <div className="p-4 border border-primary ring-1 ring-primary rounded-md space-y-4">
              <div className="flex items-center gap-2 text-sm font-medium">
                <Plus className="h-4 w-4" />
                Add Connection
              </div>
              <ConnectionForm
                formData={formData}
                setFormData={setFormData}
                availableProtocols={availableProtocols}
                onSave={handleSave}
                onCancel={resetForm}
                onTest={handleTestNew}
                isSaving={isCreating}
                isTesting={isTesting}
                testResult={testResult}
                isNew
              />
            </div>
          )}
        </div>

        {/* Add button */}
        {!isAdding && editingId === null && availableProtocols.length > 0 && (
          <Button variant="outline" size="sm" onClick={handleAdd}>
            <Plus className="h-4 w-4 mr-1" />
            Add Connection
          </Button>
        )}

        {!isAdding && editingId === null && connections.length > 0 && availableProtocols.length === 0 && (
          <p className="text-xs text-muted-foreground">
            All connection types are configured.
          </p>
        )}

        {/* Delete confirmation dialog */}
        <AlertDialog open={!!deleteTarget} onOpenChange={(open) => !open && setDeleteTarget(null)}>
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>Delete Connection</AlertDialogTitle>
              <AlertDialogDescription>
                Are you sure you want to delete this {deleteTarget?.protocol.toUpperCase()} connection?
                <div className="mt-2 p-2 bg-muted rounded text-sm font-mono">
                  {deleteTarget?.username}@{deleteTarget?.host}:{deleteTarget?.port}
                </div>
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>Cancel</AlertDialogCancel>
              <AlertDialogAction onClick={handleDelete} disabled={isDeleting}>
                {isDeleting ? "Deleting..." : "Delete"}
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      </div>
    </TooltipProvider>
  )
}

// Connection card component
interface ConnectionCardProps {
  connection: InstanceConnection
  instanceId: number
  instanceName: string
  isEditing: boolean
  isTesting: boolean
  testResult: SSHTestResult | null
  formData?: InstanceConnectionCreate
  onFormChange?: (data: InstanceConnectionCreate) => void
  onEdit: () => void
  onDelete: () => void
  onTest: () => void
  onTestNew?: () => void
  onSave: () => void
  onCancel: () => void
  isSaving: boolean
}

function ConnectionCard({
  connection,
  instanceId,
  instanceName,
  isEditing,
  isTesting,
  testResult,
  formData,
  onFormChange,
  onEdit,
  onDelete,
  onTest,
  onTestNew,
  onSave,
  onCancel,
  isSaving,
}: ConnectionCardProps) {
  const protocolInfo = PROTOCOL_INFO[connection.protocol]
  const Icon = protocolInfo.icon

  if (isEditing && formData && onFormChange) {
    return (
      <div className="p-4 border border-primary ring-1 ring-primary rounded-md space-y-4">
        <div className="flex items-center gap-2 text-sm font-medium">
          <Icon className="h-4 w-4" />
          Edit {protocolInfo.label} Connection
        </div>
        <ConnectionForm
          formData={formData}
          setFormData={onFormChange}
          availableProtocols={[connection.protocol]}
          onSave={onSave}
          onCancel={onCancel}
          onTest={onTestNew!}
          isSaving={isSaving}
          isTesting={isTesting}
          testResult={testResult}
          isNew={false}
        />
      </div>
    )
  }

  return (
    <div
      className={`flex items-center gap-4 p-4 border rounded-md transition-opacity ${
        !connection.enabled ? "opacity-50" : ""
      }`}
    >
      {/* Protocol Icon & Info */}
      <div className="flex items-center gap-3 flex-1 min-w-0">
        <div
          className={`p-2 rounded-md ${
            connection.enabled ? "bg-primary/10" : "bg-muted"
          }`}
        >
          <Icon className={`h-5 w-5 ${connection.enabled ? "text-primary" : "text-muted-foreground"}`} />
        </div>
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <span className="font-medium">{protocolInfo.label}</span>
            {connection.enabled ? (
              <Wifi className="h-3 w-3 text-green-500" />
            ) : (
              <WifiOff className="h-3 w-3 text-muted-foreground" />
            )}
          </div>
          <div className="text-sm text-muted-foreground font-mono truncate">
            {connection.username}@{connection.host}:{connection.port}
          </div>
          {connection.privateKeyPath && (
            <div className="text-xs text-muted-foreground flex items-center gap-1 mt-0.5">
              <Key className="h-3 w-3" />
              <span className="truncate max-w-[200px]" title={connection.privateKeyPath}>
                {connection.privateKeyPath.split("/").pop()}
              </span>
            </div>
          )}
        </div>
      </div>

      {/* Test Result (inline) */}
      {testResult && (
        <TestResultBadge result={testResult} />
      )}

      {/* Actions */}
      <div className="flex items-center gap-1 shrink-0">
        {(connection.protocol === "ssh" || connection.protocol === "sftp") && (
          <>
            <SSHTerminalButton connection={connection} instanceId={instanceId} instanceName={instanceName} />
            <Button
              size="sm"
              variant="ghost"
              onClick={onTest}
              disabled={isTesting}
              title="Test connection"
            >
              {isTesting ? (
                <Loader2 className="h-4 w-4 animate-spin" />
              ) : (
                "Test"
              )}
            </Button>
          </>
        )}
        <Button size="icon" variant="ghost" onClick={onEdit} title="Edit">
          <Pencil className="h-4 w-4" />
        </Button>
        <Button size="icon" variant="ghost" onClick={onDelete} title="Delete">
          <Trash2 className="h-4 w-4 text-destructive" />
        </Button>
      </div>
    </div>
  )
}

// Connection form component
interface ConnectionFormProps {
  formData: InstanceConnectionCreate
  setFormData: (data: InstanceConnectionCreate) => void
  availableProtocols: ConnectionProtocol[]
  onSave: () => void
  onCancel: () => void
  onTest: () => void
  isSaving: boolean
  isTesting: boolean
  testResult: SSHTestResult | null
  isNew: boolean
}

function ConnectionForm({
  formData,
  setFormData,
  availableProtocols,
  onSave,
  onCancel,
  onTest,
  isSaving,
  isTesting,
  testResult,
  isNew,
}: ConnectionFormProps) {
  const protocolInfo = PROTOCOL_INFO[formData.protocol]
  const canTest = formData.protocol === "ssh" || formData.protocol === "sftp"

  return (
    <div className="space-y-4">
      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        {/* Protocol */}
        {isNew && availableProtocols.length > 1 && (
          <div className="space-y-1.5">
            <Label className="text-xs">Protocol</Label>
            <Select
              value={formData.protocol}
              onValueChange={(v) =>
                setFormData({
                  ...formData,
                  protocol: v as ConnectionProtocol,
                  port: PROTOCOL_INFO[v as ConnectionProtocol].defaultPort,
                })
              }
            >
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {availableProtocols.map((p) => (
                  <SelectItem key={p} value={p}>
                    {PROTOCOL_INFO[p].label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        )}

        {/* Host */}
        <div className="space-y-1.5">
          <Label className="text-xs">Host</Label>
          <Input
            value={formData.host}
            onChange={(e) => setFormData({ ...formData, host: e.target.value })}
            placeholder="192.168.1.100 or hostname"
          />
        </div>

        {/* Port */}
        <div className="space-y-1.5">
          <Label className="text-xs">Port</Label>
          <Input
            type="number"
            value={formData.port ?? protocolInfo.defaultPort}
            onChange={(e) => setFormData({ ...formData, port: parseInt(e.target.value) || protocolInfo.defaultPort })}
            placeholder={protocolInfo.defaultPort.toString()}
          />
        </div>

        {/* Username */}
        <div className="space-y-1.5">
          <Label className="text-xs">Username</Label>
          <Input
            value={formData.username}
            onChange={(e) => setFormData({ ...formData, username: e.target.value })}
            placeholder="root"
          />
        </div>

        {/* Private Key Path */}
        {(formData.protocol === "ssh" || formData.protocol === "sftp") && (
          <div className="space-y-1.5 md:col-span-2">
            <div className="flex items-center gap-1">
              <Label className="text-xs">Private Key Path</Label>
              <Tooltip>
                <TooltipTrigger>
                  <HelpCircle className="h-3 w-3 text-muted-foreground" />
                </TooltipTrigger>
                <TooltipContent className="max-w-xs">
                  <p>
                    Path to the SSH private key <strong>on the QUI server</strong>, not your local
                    machine. Example: /home/qui/.ssh/id_rsa
                  </p>
                </TooltipContent>
              </Tooltip>
            </div>
            <Input
              value={formData.privateKeyPath ?? ""}
              onChange={(e) => setFormData({ ...formData, privateKeyPath: e.target.value })}
              placeholder="/home/user/.ssh/id_rsa"
            />
          </div>
        )}
      </div>

      {/* Enabled toggle */}
      <div className="flex items-center gap-2">
        <Switch
          checked={formData.enabled ?? true}
          onCheckedChange={(checked) => setFormData({ ...formData, enabled: checked })}
        />
        <span className="text-sm">Enabled</span>
      </div>

      {/* Test Result */}
      {testResult && <TestResultDisplay result={testResult} />}

      {/* Actions */}
      <div className="flex items-center gap-2 pt-2">
        <Button size="sm" onClick={onSave} disabled={isSaving}>
          {isSaving ? "Saving..." : isNew ? "Create" : "Save"}
        </Button>
        <Button size="sm" variant="outline" onClick={onCancel}>
          Cancel
        </Button>
        {canTest && (
          <Button size="sm" variant="secondary" onClick={onTest} disabled={isTesting}>
            {isTesting ? (
              <>
                <Loader2 className="h-4 w-4 mr-1 animate-spin" />
                Testing...
              </>
            ) : (
              "Test Connection"
            )}
          </Button>
        )}
      </div>
    </div>
  )
}

// Test result badge (compact, for card view)
function TestResultBadge({ result }: { result: SSHTestResult }) {
  if (result.success) {
    return (
      <div className="flex items-center gap-1 text-xs text-green-600 dark:text-green-400">
        <CheckCircle2 className="h-4 w-4" />
        <span>OK</span>
      </div>
    )
  }
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <div className="flex items-center gap-1 text-xs text-red-600 dark:text-red-400 cursor-help">
          <AlertCircle className="h-4 w-4" />
          <span>Failed</span>
        </div>
      </TooltipTrigger>
      <TooltipContent className="max-w-xs">
        <p>{result.message}</p>
        {result.details && <p className="text-xs mt-1 opacity-75">{result.details}</p>}
      </TooltipContent>
    </Tooltip>
  )
}

// Test result display (full, for form view)
function TestResultDisplay({ result }: { result: SSHTestResult }) {
  return (
    <div
      className={`p-3 rounded-md text-sm ${
        result.success
          ? "bg-green-500/10 border border-green-500/20"
          : "bg-red-500/10 border border-red-500/20"
      }`}
    >
      <div className="flex items-start gap-2">
        {result.success ? (
          <CheckCircle2 className="h-5 w-5 text-green-600 dark:text-green-400 shrink-0 mt-0.5" />
        ) : (
          <AlertCircle className="h-5 w-5 text-red-600 dark:text-red-400 shrink-0 mt-0.5" />
        )}
        <div className="space-y-1">
          <p className={result.success ? "text-green-700 dark:text-green-300" : "text-red-700 dark:text-red-300"}>
            {result.message}
          </p>
          {result.details && (
            <p className="text-xs text-muted-foreground font-mono whitespace-pre-wrap">
              {result.details}
            </p>
          )}
          {!result.success && result.message.includes("Permission denied") && (
            <p className="text-xs text-muted-foreground mt-2">
              <strong>Tip:</strong> Ensure the SSH key is readable by QUI and the user is authorized
              on the remote server.
            </p>
          )}
        </div>
      </div>
    </div>
  )
}
