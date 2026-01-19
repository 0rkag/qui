/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useState } from "react"
import {
  AlertCircle,
  CheckCircle2,
  Eye,
  EyeOff,
  HelpCircle,
  Info,
  Key,
  Loader2,
  Lock,
  Pencil,
  Plus,
  Server,
  ShieldAlert,
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
import { Alert, AlertDescription } from "@/components/ui/alert"
import { useInstanceConnections } from "@/hooks/useInstanceConnections"
import { SSHTerminalButton } from "./SSHTerminal"
import type {
  ConnectionType,
  InstanceConnection,
  InstanceConnectionCreate,
  ConnectionTestResult,
} from "@/types"
import { isSSHType, isFTPType } from "@/types"

interface RemoteAccessEditorProps {
  instanceId: number
  instanceName: string
}

// Connection type info for UI display
const CONNECTION_TYPE_INFO: Record<ConnectionType, {
  label: string
  shortLabel: string
  icon: typeof Terminal
  description: string
  defaultPort: number
  category: "ssh" | "ftp"
}> = {
  ssh_auto: {
    label: "SSH (Auto)",
    shortLabel: "SSH Auto",
    icon: Terminal,
    description: "SSH with automatic transfer method selection (rsync preferred)",
    defaultPort: 22,
    category: "ssh",
  },
  ssh_rsync: {
    label: "SSH (rsync)",
    shortLabel: "rsync",
    icon: Terminal,
    description: "SSH using rsync for efficient delta transfers",
    defaultPort: 22,
    category: "ssh",
  },
  ssh_sftp: {
    label: "SSH (SFTP)",
    shortLabel: "SFTP",
    icon: Server,
    description: "SSH File Transfer Protocol for secure file operations",
    defaultPort: 22,
    category: "ssh",
  },
  ssh_scp: {
    label: "SSH (SCP)",
    shortLabel: "SCP",
    icon: Terminal,
    description: "Secure Copy Protocol for simple file transfers",
    defaultPort: 22,
    category: "ssh",
  },
  ftp_explicit: {
    label: "FTP (Explicit TLS)",
    shortLabel: "FTPES",
    icon: Server,
    description: "FTP with AUTH TLS encryption on port 21",
    defaultPort: 21,
    category: "ftp",
  },
  ftp_implicit: {
    label: "FTP (Implicit TLS)",
    shortLabel: "FTPS",
    icon: Lock,
    description: "FTP with implicit TLS encryption on port 990",
    defaultPort: 990,
    category: "ftp",
  },
  ftp_plain: {
    label: "FTP (Unencrypted)",
    shortLabel: "FTP",
    icon: Server,
    description: "Plain FTP without encryption (not recommended)",
    defaultPort: 21,
    category: "ftp",
  },
}

// All available connection types
const ALL_CONNECTION_TYPES: ConnectionType[] = [
  "ssh_auto",
  "ssh_rsync",
  "ssh_sftp",
  "ssh_scp",
  "ftp_explicit",
  "ftp_implicit",
  "ftp_plain",
]

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
  const [testResult, setTestResult] = useState<ConnectionTestResult | null>(null)
  const [testingId, setTestingId] = useState<number | null>(null)

  // Form state
  const [formData, setFormData] = useState<InstanceConnectionCreate>({
    type: "ssh_auto",
    host: "",
    port: 22,
    username: "",
    password: "",
    privateKeyPath: "",
    enabled: true,
  })

  const resetForm = () => {
    setFormData({
      type: "ssh_auto",
      host: "",
      port: 22,
      username: "",
      password: "",
      privateKeyPath: "",
      enabled: true,
    })
    setEditingId(null)
    setIsAdding(false)
    setTestResult(null)
  }

  const handleEdit = (conn: InstanceConnection) => {
    setFormData({
      type: conn.type,
      host: conn.host,
      port: conn.port,
      username: conn.username,
      password: "", // Don't populate password - user must re-enter if changing
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

    // For SSH types, require either password or private key
    const typeInfo = CONNECTION_TYPE_INFO[formData.type]
    if (typeInfo.category === "ssh") {
      if (!formData.password?.trim() && !formData.privateKeyPath?.trim()) {
        toast.error("Either password or private key path is required for SSH")
        return
      }
    } else {
      // For FTP types, require password
      if (!formData.password?.trim()) {
        toast.error("Password is required for FTP connections")
        return
      }
    }

    try {
      if (editingId !== null) {
        await updateConnectionAsync({
          id: editingId,
          data: {
            type: formData.type,
            host: formData.host,
            port: formData.port ?? typeInfo.defaultPort,
            username: formData.username,
            password: formData.password || undefined, // Only send if provided
            privateKeyPath: formData.privateKeyPath || undefined,
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
        type: formData.type,
        host: formData.host,
        port: formData.port,
        username: formData.username,
        password: formData.password,
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
            {showHelp ? "Hide" : "Why connect?"}
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
                    <strong>Remote connections enable file operations</strong> when QUI runs on a
                    different machine than qBittorrent.
                  </p>
                  <div className="space-y-1 text-muted-foreground">
                    <p>With a connection configured, QUI can:</p>
                    <ul className="list-disc list-inside ml-2 space-y-0.5">
                      <li>Create hardlinks on the remote machine</li>
                      <li>Transfer files between instances</li>
                      <li>Verify file existence before adding torrents</li>
                    </ul>
                  </div>
                  <div className="pt-2 border-t text-xs text-muted-foreground">
                    <strong>SSH vs FTP:</strong> SSH is preferred for security and features. FTP is
                    available for environments where SSH isn&apos;t possible.
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
                Add a connection to enable remote file operations.
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
          {isAdding && (
            <div className="p-4 border border-primary ring-1 ring-primary rounded-md space-y-4">
              <div className="flex items-center gap-2 text-sm font-medium">
                <Plus className="h-4 w-4" />
                Add Connection
              </div>
              <ConnectionForm
                formData={formData}
                setFormData={setFormData}
                availableTypes={ALL_CONNECTION_TYPES}
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
        {!isAdding && editingId === null && (
          <Button variant="outline" size="sm" onClick={handleAdd}>
            <Plus className="h-4 w-4 mr-1" />
            Add Connection
          </Button>
        )}

        {/* Delete confirmation dialog */}
        <AlertDialog open={!!deleteTarget} onOpenChange={(open) => !open && setDeleteTarget(null)}>
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>Delete Connection</AlertDialogTitle>
              <AlertDialogDescription>
                Are you sure you want to delete this {deleteTarget && CONNECTION_TYPE_INFO[deleteTarget.type].label} connection?
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
  testResult: ConnectionTestResult | null
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
  const typeInfo = CONNECTION_TYPE_INFO[connection.type]
  const Icon = typeInfo.icon

  if (isEditing && formData && onFormChange) {
    return (
      <div className="p-4 border border-primary ring-1 ring-primary rounded-md space-y-4">
        <div className="flex items-center gap-2 text-sm font-medium">
          <Icon className="h-4 w-4" />
          Edit {typeInfo.label} Connection
        </div>
        <ConnectionForm
          formData={formData}
          setFormData={onFormChange}
          availableTypes={ALL_CONNECTION_TYPES}
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
      {/* Type Icon & Info */}
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
            <span className="font-medium">{typeInfo.shortLabel}</span>
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
        {isSSHType(connection.type) && (
          <SSHTerminalButton connection={connection} instanceId={instanceId} instanceName={instanceName} />
        )}
        <Button
          size="sm"
          variant="ghost"
          onClick={onTest}
          disabled={isTesting}
          title="Test connection"
        >
          {isTesting ? <Loader2 className="h-4 w-4 animate-spin" /> : "Test"}
        </Button>
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
  availableTypes: ConnectionType[]
  onSave: () => void
  onCancel: () => void
  onTest: () => void
  isSaving: boolean
  isTesting: boolean
  testResult: ConnectionTestResult | null
  isNew: boolean
}

function ConnectionForm({
  formData,
  setFormData,
  availableTypes,
  onSave,
  onCancel,
  onTest,
  isSaving,
  isTesting,
  testResult,
  isNew,
}: ConnectionFormProps) {
  const typeInfo = CONNECTION_TYPE_INFO[formData.type]
  const isSSH = isSSHType(formData.type)
  const [showPassword, setShowPassword] = useState(false)

  return (
    <div className="space-y-4">
      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        {/* Connection Type */}
        <div className="space-y-1.5 md:col-span-2">
          <Label className="text-xs">Connection Type</Label>
          <Select
            value={formData.type}
            onValueChange={(v) => {
              const newType = v as ConnectionType
              const newInfo = CONNECTION_TYPE_INFO[newType]
              setFormData({
                ...formData,
                type: newType,
                port: newInfo.defaultPort,
                // Clear irrelevant fields when switching between SSH and FTP
                privateKeyPath: isSSHType(newType) ? formData.privateKeyPath : "",
              })
            }}
          >
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <div className="px-2 py-1.5 text-xs font-medium text-muted-foreground">SSH Connections</div>
              {availableTypes.filter(t => isSSHType(t)).map((t) => (
                <SelectItem key={t} value={t}>
                  <div className="flex items-center gap-2">
                    <span>{CONNECTION_TYPE_INFO[t].label}</span>
                  </div>
                </SelectItem>
              ))}
              <div className="px-2 py-1.5 text-xs font-medium text-muted-foreground border-t mt-1 pt-1">FTP Connections</div>
              {availableTypes.filter(t => isFTPType(t)).map((t) => (
                <SelectItem key={t} value={t}>
                  <div className="flex items-center gap-2">
                    <span>{CONNECTION_TYPE_INFO[t].label}</span>
                    {t === "ftp_plain" && (
                      <span className="text-xs text-amber-500">(insecure)</span>
                    )}
                  </div>
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <p className="text-xs text-muted-foreground">{typeInfo.description}</p>
          {formData.type === "ftp_plain" && (
            <Alert className="mt-2 border-red-200 bg-red-50 dark:border-red-900 dark:bg-red-950/50">
              <ShieldAlert className="h-4 w-4 text-red-600" />
              <AlertDescription className="text-xs text-red-800 dark:text-red-200">
                <strong>Security risk:</strong> Plain FTP transmits credentials and data without encryption.
                Anyone on the network can intercept your username and password. Only use on trusted local networks.
              </AlertDescription>
            </Alert>
          )}
        </div>

        {/* Host */}
        <div className="space-y-1.5">
          <div className="flex items-center gap-1">
            <Label className="text-xs">Host</Label>
            <Tooltip>
              <TooltipTrigger>
                <HelpCircle className="h-3 w-3 text-muted-foreground" />
              </TooltipTrigger>
              <TooltipContent className="max-w-xs">
                <p>IP address or hostname of the remote server where qBittorrent is running.
                   Use an IP for faster connections or a hostname for flexibility.</p>
              </TooltipContent>
            </Tooltip>
          </div>
          <Input
            value={formData.host}
            onChange={(e) => setFormData({ ...formData, host: e.target.value })}
            placeholder="192.168.1.100 or hostname"
          />
        </div>

        {/* Port */}
        <div className="space-y-1.5">
          <div className="flex items-center gap-1">
            <Label className="text-xs">Port</Label>
            <Tooltip>
              <TooltipTrigger>
                <HelpCircle className="h-3 w-3 text-muted-foreground" />
              </TooltipTrigger>
              <TooltipContent className="max-w-xs">
                <p>Standard ports: SSH uses 22, FTP uses 21 (explicit TLS) or 990 (implicit TLS).
                   Only change if your server uses a non-standard port.</p>
              </TooltipContent>
            </Tooltip>
          </div>
          <Input
            type="number"
            value={formData.port ?? typeInfo.defaultPort}
            onChange={(e) => setFormData({ ...formData, port: parseInt(e.target.value) || typeInfo.defaultPort })}
            placeholder={typeInfo.defaultPort.toString()}
          />
        </div>

        {/* Username */}
        <div className="space-y-1.5">
          <div className="flex items-center gap-1">
            <Label className="text-xs">Username</Label>
            <Tooltip>
              <TooltipTrigger>
                <HelpCircle className="h-3 w-3 text-muted-foreground" />
              </TooltipTrigger>
              <TooltipContent className="max-w-xs">
                <p>The user account on the remote server. For security, avoid using &quot;root&quot; unless necessary.
                   Create a dedicated user with only the permissions needed for file access.</p>
              </TooltipContent>
            </Tooltip>
          </div>
          <Input
            value={formData.username}
            onChange={(e) => setFormData({ ...formData, username: e.target.value })}
            placeholder="user"
          />
        </div>

        {/* Password */}
        <div className="space-y-1.5">
          <div className="flex items-center gap-1">
            <Label className="text-xs">Password</Label>
            {isSSH && (
              <span className="text-xs text-muted-foreground">(or use key below)</span>
            )}
            <Tooltip>
              <TooltipTrigger>
                <HelpCircle className="h-3 w-3 text-muted-foreground" />
              </TooltipTrigger>
              <TooltipContent className="max-w-xs">
                <p>Password is stored encrypted in the database. For SSH, using a private key is more secure
                   than password authentication.</p>
              </TooltipContent>
            </Tooltip>
          </div>
          <div className="relative">
            <Input
              type={showPassword ? "text" : "password"}
              value={formData.password ?? ""}
              onChange={(e) => setFormData({ ...formData, password: e.target.value })}
              placeholder={isNew ? "Enter password" : "Leave blank to keep existing"}
              className="pr-10"
            />
            <Button
              type="button"
              variant="ghost"
              size="sm"
              className="absolute right-0 top-0 h-full px-3 py-2 hover:bg-transparent"
              onClick={() => setShowPassword(!showPassword)}
            >
              {showPassword ? (
                <EyeOff className="h-4 w-4 text-muted-foreground" />
              ) : (
                <Eye className="h-4 w-4 text-muted-foreground" />
              )}
            </Button>
          </div>
        </div>

        {/* Private Key Path (SSH only) */}
        {isSSH && (
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
      {testResult && <TestResultDisplay result={testResult} isSSH={isSSH} />}

      {/* Actions */}
      <div className="flex items-center gap-2 pt-2">
        <Button size="sm" onClick={onSave} disabled={isSaving}>
          {isSaving ? "Saving..." : isNew ? "Create" : "Save"}
        </Button>
        <Button size="sm" variant="outline" onClick={onCancel}>
          Cancel
        </Button>
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
      </div>
    </div>
  )
}

// Test result badge (compact, for card view)
function TestResultBadge({ result }: { result: ConnectionTestResult }) {
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
function TestResultDisplay({ result, isSSH }: { result: ConnectionTestResult; isSSH: boolean }) {
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
        <div className="space-y-1 flex-1">
          <p className={result.success ? "text-green-700 dark:text-green-300" : "text-red-700 dark:text-red-300"}>
            {result.message}
          </p>
          {result.details && (
            <p className="text-xs text-muted-foreground font-mono whitespace-pre-wrap">
              {result.details}
            </p>
          )}

          {/* SSH Capabilities */}
          {result.success && isSSH && result.sshCapabilities && (
            <ul className="mt-1 space-y-0.5 text-xs text-muted-foreground">
              {result.sshCapabilities.rsyncAvailable && <li>✓ rsync available</li>}
              {result.sshCapabilities.sftpAvailable && <li>✓ SFTP available</li>}
              {result.sshCapabilities.hardlinksSupported && <li>✓ Hardlinks supported</li>}
              {result.sshCapabilities.reflinksSupported && <li>✓ Reflinks supported</li>}
            </ul>
          )}

          {/* FTP Capabilities */}
          {result.success && !isSSH && result.ftpCapabilities && (
            <ul className="mt-1 space-y-0.5 text-xs text-muted-foreground">
              {result.ftpCapabilities.tlsEnabled && <li>✓ TLS enabled</li>}
              {result.ftpCapabilities.passiveModeWorks && <li>✓ Passive mode works</li>}
              {result.ftpCapabilities.serverType && <li>Server: {result.ftpCapabilities.serverType}</li>}
            </ul>
          )}

          {!result.success && result.message.includes("Permission denied") && (
            <p className="text-xs text-muted-foreground mt-2">
              <strong>Tip:</strong> {isSSH
                ? "Ensure the SSH key is readable by QUI and the user is authorized on the remote server."
                : "Check that the username and password are correct."}
            </p>
          )}
        </div>
      </div>
    </div>
  )
}
