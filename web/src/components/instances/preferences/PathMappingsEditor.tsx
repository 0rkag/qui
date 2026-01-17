/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useState, useMemo } from "react"
import {
  ArrowRight,
  ArrowLeftRight,
  HelpCircle,
  Info,
  Pencil,
  Play,
  Plus,
  Trash2,
  Server,
  HardDrive,
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
import { useInstancePathMappings } from "@/hooks/useInstancePathMappings"
import { useInstances } from "@/hooks/useInstances"
import { api } from "@/lib/api"
import type { InstancePathMapping, InstancePathMappingCreate } from "@/types"

// Validate a path for path mapping
function validatePath(path: string): string | null {
  if (!path.trim()) {
    return "Path is required"
  }
  if (!path.startsWith("/")) {
    return "Path must be absolute (start with /)"
  }
  if (path.includes("..")) {
    return "Path cannot contain traversal elements (..)"
  }
  if (path.length > 4096) {
    return "Path is too long (max 4096 characters)"
  }
  return null
}

interface PathMappingsEditorProps {
  instanceId: number
  instanceName?: string
}

export function PathMappingsEditor({ instanceId, instanceName }: PathMappingsEditorProps) {
  const {
    mappings,
    isLoading,
    createMappingAsync,
    updateMappingAsync,
    deleteMappingAsync,
    testPath,
    isCreating,
    isUpdating,
    isDeleting,
    isTesting,
  } = useInstancePathMappings(instanceId)

  const { instances } = useInstances()

  const [editingId, setEditingId] = useState<number | null>(null)
  const [isAdding, setIsAdding] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState<InstancePathMapping | null>(null)
  const [showHelp, setShowHelp] = useState(false)

  // Form state for adding/editing
  const [formData, setFormData] = useState<InstancePathMappingCreate>({
    instancePath: "",
    canonicalPath: "",
    enabled: true,
    description: "",
  })

  // Simple path test state
  const [testInput, setTestInput] = useState("")
  const [testDirection, setTestDirection] = useState<"to_canonical" | "from_canonical">("to_canonical")
  const [testResult, setTestResult] = useState<{ input: string; output: string; noMatch: boolean } | null>(null)

  // Cross-instance test state
  const [crossTestPath, setCrossTestPath] = useState("")
  const [crossTestTargetId, setCrossTestTargetId] = useState<number | null>(null)
  const [crossTestResult, setCrossTestResult] = useState<{
    sourcePath: string
    canonicalPath: string
    targetPath: string
    sourceMatch: boolean
    targetMatch: boolean
  } | null>(null)
  const [isCrossTesting, setIsCrossTesting] = useState(false)

  // Other instances for cross-instance testing
  const otherInstances = useMemo(() => {
    return (instances ?? []).filter((i) => i.id !== instanceId)
  }, [instances, instanceId])

  // Validation errors (computed from form data)
  const validationErrors = useMemo(
    () => ({
      instancePath: validatePath(formData.instancePath),
      canonicalPath: validatePath(formData.canonicalPath),
    }),
    [formData.instancePath, formData.canonicalPath]
  )

  const resetForm = () => {
    setFormData({ instancePath: "", canonicalPath: "", enabled: true, description: "" })
    setEditingId(null)
    setIsAdding(false)
  }

  const handleEdit = (mapping: InstancePathMapping) => {
    setFormData({
      instancePath: mapping.instancePath,
      canonicalPath: mapping.canonicalPath,
      enabled: mapping.enabled,
      description: mapping.description ?? "",
    })
    setEditingId(mapping.id)
    setIsAdding(false)
  }

  const handleAdd = () => {
    setFormData({ instancePath: "", canonicalPath: "", enabled: true, description: "" })
    setEditingId(null)
    setIsAdding(true)
  }

  const handleSave = async () => {
    const instanceError = validatePath(formData.instancePath)
    const canonicalError = validatePath(formData.canonicalPath)

    if (instanceError) {
      toast.error(`Instance path: ${instanceError}`)
      return
    }
    if (canonicalError) {
      toast.error(`QUI server path: ${canonicalError}`)
      return
    }

    try {
      if (editingId !== null) {
        await updateMappingAsync({ id: editingId, data: formData })
        toast.success("Path mapping updated")
      } else {
        await createMappingAsync(formData)
        toast.success("Path mapping created")
      }
      resetForm()
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "Failed to save mapping")
    }
  }

  const handleDelete = async () => {
    if (!deleteTarget) return
    try {
      await deleteMappingAsync(deleteTarget.id)
      toast.success("Path mapping deleted")
      setDeleteTarget(null)
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "Failed to delete mapping")
    }
  }

  const handleSimpleTest = async () => {
    if (!testInput.trim()) {
      toast.error("Enter a path to test")
      return
    }

    try {
      const result = await testPath({ path: testInput, direction: testDirection })
      setTestResult({
        input: result.inputPath,
        output: result.outputPath,
        noMatch: result.noMatchFound,
      })
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "Failed to test path")
    }
  }

  const handleCrossInstanceTest = async () => {
    if (!crossTestPath.trim()) {
      toast.error("Enter a path to test")
      return
    }
    if (!crossTestTargetId) {
      toast.error("Select a target instance")
      return
    }

    setIsCrossTesting(true)
    try {
      // Step 1: Convert source instance path to canonical
      const toCanonical = await testPath({ path: crossTestPath, direction: "to_canonical" })

      // Step 2: Convert canonical to target instance path
      const toTarget = await api.testPathMapping(crossTestTargetId, {
        path: toCanonical.outputPath,
        direction: "from_canonical",
      })

      setCrossTestResult({
        sourcePath: crossTestPath,
        canonicalPath: toCanonical.outputPath,
        targetPath: toTarget.outputPath,
        sourceMatch: !toCanonical.noMatchFound,
        targetMatch: !toTarget.noMatchFound,
      })
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "Failed to test cross-instance path")
    } finally {
      setIsCrossTesting(false)
    }
  }

  const enabledCount = mappings.filter((m) => m.enabled).length

  if (isLoading) {
    return <div className="text-sm text-muted-foreground">Loading path mappings...</div>
  }

  return (
    <TooltipProvider>
      <div className="space-y-6">
        {/* Header with status */}
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <span className="text-sm text-muted-foreground">
              {mappings.length === 0 ? (
                "No mappings configured"
              ) : (
                <>
                  {enabledCount} of {mappings.length} mapping{mappings.length !== 1 ? "s" : ""} active
                </>
              )}
            </span>
          </div>
          <Button variant="ghost" size="sm" onClick={() => setShowHelp(!showHelp)}>
            <HelpCircle className="h-4 w-4 mr-1" />
            {showHelp ? "Hide" : "How it works"}
          </Button>
        </div>

        {/* Collapsible Help Section */}
        <Collapsible open={showHelp} onOpenChange={setShowHelp}>
          <CollapsibleContent>
            <div className="rounded-lg border bg-muted/30 p-4 space-y-4">
              <div className="flex items-start gap-3">
                <Info className="h-5 w-5 text-blue-500 mt-0.5 shrink-0" />
                <div className="space-y-3 text-sm">
                  <p>
                    <strong>Path mappings enable moving torrents between instances</strong> even when they see storage at different paths.
                  </p>

                  {/* Visual Diagram */}
                  <div className="bg-background rounded-md p-4 border">
                    <div className="flex items-center justify-center gap-2 text-xs">
                      <div className="flex flex-col items-center gap-1">
                        <Server className="h-6 w-6 text-blue-500" />
                        <span className="font-medium">Instance A</span>
                        <code className="bg-muted px-1.5 py-0.5 rounded text-[10px]">/downloads/</code>
                      </div>

                      <div className="flex flex-col items-center gap-1 px-4">
                        <ArrowRight className="h-4 w-4 text-muted-foreground" />
                      </div>

                      <div className="flex flex-col items-center gap-1 border-2 border-dashed border-primary/50 rounded-lg px-4 py-2">
                        <HardDrive className="h-6 w-6 text-primary" />
                        <span className="font-medium text-primary">QUI Server</span>
                        <code className="bg-primary/10 text-primary px-1.5 py-0.5 rounded text-[10px]">/data/media/</code>
                      </div>

                      <div className="flex flex-col items-center gap-1 px-4">
                        <ArrowRight className="h-4 w-4 text-muted-foreground" />
                      </div>

                      <div className="flex flex-col items-center gap-1">
                        <Server className="h-6 w-6 text-green-500" />
                        <span className="font-medium">Instance B</span>
                        <code className="bg-muted px-1.5 py-0.5 rounded text-[10px]">/mnt/storage/</code>
                      </div>
                    </div>
                  </div>

                  <div className="space-y-2 text-muted-foreground">
                    <p>
                      <strong className="text-foreground">How it works:</strong> Each instance defines how it sees storage compared to the QUI server. When moving a torrent:
                    </p>
                    <ol className="list-decimal list-inside space-y-1 ml-2">
                      <li>Source path is translated to the QUI server's view (canonical path)</li>
                      <li>Canonical path is translated to the target instance's view</li>
                    </ol>
                    <p className="text-xs italic">
                      This means you only need to configure mappings once per instance, not for every possible pair of instances.
                    </p>
                  </div>
                </div>
              </div>
            </div>
          </CollapsibleContent>
        </Collapsible>

        {/* Path Mappings List */}
        <div className="space-y-2">
          {mappings.length === 0 && !isAdding && (
            <div className="text-sm text-muted-foreground py-6 text-center border border-dashed rounded-md">
              <p>No path mappings configured.</p>
              <p className="text-xs mt-1">Add a mapping to enable path translation for this instance.</p>
            </div>
          )}

          {mappings.map((mapping) => (
            <div
              key={mapping.id}
              className={`flex items-center gap-3 p-3 border rounded-md transition-opacity ${
                !mapping.enabled ? "opacity-50" : ""
              } ${editingId === mapping.id ? "border-primary ring-1 ring-primary" : ""}`}
            >
              {editingId === mapping.id ? (
                <MappingForm
                  formData={formData}
                  setFormData={setFormData}
                  validationErrors={validationErrors}
                  onSave={handleSave}
                  onCancel={resetForm}
                  isLoading={isUpdating}
                  submitLabel="Save"
                />
              ) : (
                <>
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2 text-sm flex-wrap">
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <code className="bg-muted px-1.5 py-0.5 rounded text-xs truncate max-w-[180px]" title={mapping.instancePath}>
                            {mapping.instancePath}
                          </code>
                        </TooltipTrigger>
                        <TooltipContent>
                          <p>How this qBittorrent instance sees the path</p>
                        </TooltipContent>
                      </Tooltip>

                      <ArrowLeftRight className="h-3 w-3 text-muted-foreground shrink-0" />

                      <Tooltip>
                        <TooltipTrigger asChild>
                          <code className="bg-primary/10 text-primary px-1.5 py-0.5 rounded text-xs truncate max-w-[180px]" title={mapping.canonicalPath}>
                            {mapping.canonicalPath}
                          </code>
                        </TooltipTrigger>
                        <TooltipContent>
                          <p>How the QUI server sees the same path</p>
                        </TooltipContent>
                      </Tooltip>
                    </div>
                    {mapping.description && (
                      <div className="text-xs text-muted-foreground mt-1">{mapping.description}</div>
                    )}
                  </div>
                  <div className="flex items-center gap-1 shrink-0">
                    <Button size="icon" variant="ghost" onClick={() => handleEdit(mapping)} title="Edit">
                      <Pencil className="h-4 w-4" />
                    </Button>
                    <Button size="icon" variant="ghost" onClick={() => setDeleteTarget(mapping)} title="Delete">
                      <Trash2 className="h-4 w-4 text-destructive" />
                    </Button>
                  </div>
                </>
              )}
            </div>
          ))}

          {/* Add new mapping form */}
          {isAdding && (
            <div className="p-3 border border-primary ring-1 ring-primary rounded-md">
              <MappingForm
                formData={formData}
                setFormData={setFormData}
                validationErrors={validationErrors}
                onSave={handleSave}
                onCancel={resetForm}
                isLoading={isCreating}
                submitLabel="Create"
                autoFocus
              />
            </div>
          )}
        </div>

        {/* Add button */}
        {!isAdding && editingId === null && (
          <Button variant="outline" size="sm" onClick={handleAdd}>
            <Plus className="h-4 w-4 mr-1" />
            Add Path Mapping
          </Button>
        )}

        {/* Path Testers */}
        {mappings.length > 0 && (
          <div className="border-t pt-4 space-y-4">
            <div className="text-sm font-medium">Test Path Translation</div>

            {/* Simple Test (this instance only) */}
            <div className="space-y-3">
              <Label className="text-xs text-muted-foreground">Test within this instance</Label>
              <div className="flex gap-2">
                <Input
                  value={testInput}
                  onChange={(e) => setTestInput(e.target.value)}
                  placeholder="e.g., /downloads/movies/Avatar.mkv"
                  className="flex-1"
                />
                <Select value={testDirection} onValueChange={(v) => setTestDirection(v as typeof testDirection)}>
                  <SelectTrigger className="w-[180px]">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="to_canonical">Instance → QUI Server</SelectItem>
                    <SelectItem value="from_canonical">QUI Server → Instance</SelectItem>
                  </SelectContent>
                </Select>
                <Button onClick={handleSimpleTest} disabled={isTesting} size="sm">
                  <Play className="h-4 w-4 mr-1" />
                  Test
                </Button>
              </div>
              {testResult && (
                <TestResultDisplay
                  input={testResult.input}
                  output={testResult.output}
                  noMatch={testResult.noMatch}
                />
              )}
            </div>

            {/* Cross-Instance Test */}
            {otherInstances.length > 0 && (
              <div className="space-y-3 pt-3 border-t">
                <Label className="text-xs text-muted-foreground">
                  Test path translation to another instance
                </Label>
                <div className="flex gap-2 flex-wrap">
                  <Input
                    value={crossTestPath}
                    onChange={(e) => setCrossTestPath(e.target.value)}
                    placeholder="e.g., /downloads/movies/Avatar.mkv"
                    className="flex-1 min-w-[200px]"
                  />
                  <Select
                    value={crossTestTargetId?.toString() ?? ""}
                    onValueChange={(v) => setCrossTestTargetId(Number(v))}
                  >
                    <SelectTrigger className="w-[200px]">
                      <SelectValue placeholder="Select target instance" />
                    </SelectTrigger>
                    <SelectContent>
                      {otherInstances.map((inst) => (
                        <SelectItem key={inst.id} value={inst.id.toString()}>
                          {inst.name}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <Button onClick={handleCrossInstanceTest} disabled={isCrossTesting} size="sm">
                    <Play className="h-4 w-4 mr-1" />
                    {isCrossTesting ? "Testing..." : "Test Transfer"}
                  </Button>
                </div>
                {crossTestResult && (
                  <CrossInstanceTestResult
                    sourceName={instanceName ?? "This instance"}
                    targetName={otherInstances.find((i) => i.id === crossTestTargetId)?.name ?? "Target"}
                    result={crossTestResult}
                  />
                )}
              </div>
            )}
          </div>
        )}

        {/* Delete confirmation dialog */}
        <AlertDialog open={!!deleteTarget} onOpenChange={(open) => !open && setDeleteTarget(null)}>
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>Delete Path Mapping</AlertDialogTitle>
              <AlertDialogDescription>
                Are you sure you want to delete this path mapping?
                <div className="mt-2 p-2 bg-muted rounded text-sm">
                  <code>{deleteTarget?.instancePath}</code>
                  <ArrowLeftRight className="h-3 w-3 inline mx-2" />
                  <code>{deleteTarget?.canonicalPath}</code>
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

// Extracted form component to reduce duplication
interface MappingFormProps {
  formData: InstancePathMappingCreate
  setFormData: (data: InstancePathMappingCreate) => void
  validationErrors: { instancePath: string | null; canonicalPath: string | null }
  onSave: () => void
  onCancel: () => void
  isLoading: boolean
  submitLabel: string
  autoFocus?: boolean
}

function MappingForm({
  formData,
  setFormData,
  validationErrors,
  onSave,
  onCancel,
  isLoading,
  submitLabel,
  autoFocus,
}: MappingFormProps) {
  return (
    <div className="flex-1 space-y-3">
      <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
        <div className="space-y-1">
          <div className="flex items-center gap-1">
            <Label className="text-xs">Instance Path</Label>
            <Tooltip>
              <TooltipTrigger>
                <HelpCircle className="h-3 w-3 text-muted-foreground" />
              </TooltipTrigger>
              <TooltipContent className="max-w-xs">
                <p>The path as qBittorrent sees it. This is typically what you see in qBittorrent's download folder settings.</p>
              </TooltipContent>
            </Tooltip>
          </div>
          <Input
            value={formData.instancePath}
            onChange={(e) => setFormData({ ...formData, instancePath: e.target.value })}
            placeholder="/downloads"
            autoFocus={autoFocus}
            className={validationErrors.instancePath && formData.instancePath ? "border-destructive" : ""}
          />
          {validationErrors.instancePath && formData.instancePath && (
            <p className="text-xs text-destructive">{validationErrors.instancePath}</p>
          )}
        </div>
        <div className="space-y-1">
          <div className="flex items-center gap-1">
            <Label className="text-xs">QUI Server Path</Label>
            <Tooltip>
              <TooltipTrigger>
                <HelpCircle className="h-3 w-3 text-muted-foreground" />
              </TooltipTrigger>
              <TooltipContent className="max-w-xs">
                <p>The same location as seen by the QUI server. This is the "canonical" path used to translate between instances.</p>
              </TooltipContent>
            </Tooltip>
          </div>
          <Input
            value={formData.canonicalPath}
            onChange={(e) => setFormData({ ...formData, canonicalPath: e.target.value })}
            placeholder="/mnt/storage/downloads"
            className={validationErrors.canonicalPath && formData.canonicalPath ? "border-destructive" : ""}
          />
          {validationErrors.canonicalPath && formData.canonicalPath && (
            <p className="text-xs text-destructive">{validationErrors.canonicalPath}</p>
          )}
        </div>
      </div>
      <div className="flex items-center gap-4 flex-wrap">
        <div className="flex-1 min-w-[200px]">
          <Input
            value={formData.description}
            onChange={(e) => setFormData({ ...formData, description: e.target.value })}
            placeholder="Description (optional)"
          />
        </div>
        <div className="flex items-center gap-2">
          <Switch
            checked={formData.enabled}
            onCheckedChange={(checked) => setFormData({ ...formData, enabled: checked })}
          />
          <span className="text-sm">Enabled</span>
        </div>
      </div>
      <div className="flex gap-2">
        <Button size="sm" onClick={onSave} disabled={isLoading}>
          {isLoading ? "Saving..." : submitLabel}
        </Button>
        <Button size="sm" variant="outline" onClick={onCancel}>
          Cancel
        </Button>
      </div>
    </div>
  )
}

// Simple test result display
function TestResultDisplay({ input, output, noMatch }: { input: string; output: string; noMatch: boolean }) {
  return (
    <div
      className={`p-3 rounded-md text-sm ${
        noMatch ? "bg-amber-500/10 border border-amber-500/20" : "bg-green-500/10 border border-green-500/20"
      }`}
    >
      <div className="flex items-center gap-2 flex-wrap">
        <code className="bg-muted px-1.5 py-0.5 rounded text-xs">{input}</code>
        <ArrowRight className="h-3 w-3 text-muted-foreground shrink-0" />
        <code className="bg-muted px-1.5 py-0.5 rounded text-xs">{output}</code>
      </div>
      {noMatch && (
        <div className="text-xs text-amber-600 dark:text-amber-400 mt-1">
          No matching mapping found - path returned unchanged
        </div>
      )}
    </div>
  )
}

// Cross-instance test result display
function CrossInstanceTestResult({
  sourceName,
  targetName,
  result,
}: {
  sourceName: string
  targetName: string
  result: {
    sourcePath: string
    canonicalPath: string
    targetPath: string
    sourceMatch: boolean
    targetMatch: boolean
  }
}) {
  const hasWarning = !result.sourceMatch || !result.targetMatch

  return (
    <div
      className={`p-4 rounded-md text-sm space-y-3 ${
        hasWarning ? "bg-amber-500/10 border border-amber-500/20" : "bg-green-500/10 border border-green-500/20"
      }`}
    >
      {/* Step 1: Source to Canonical */}
      <div className="flex items-center gap-2 flex-wrap">
        <div className="flex items-center gap-1">
          <Server className="h-4 w-4 text-blue-500" />
          <span className="text-xs font-medium">{sourceName}</span>
        </div>
        <code className="bg-muted px-1.5 py-0.5 rounded text-xs">{result.sourcePath}</code>
        <ArrowRight className="h-3 w-3 text-muted-foreground shrink-0" />
        <div className="flex items-center gap-1">
          <HardDrive className="h-4 w-4 text-primary" />
          <span className="text-xs font-medium">QUI</span>
        </div>
        <code className="bg-primary/10 text-primary px-1.5 py-0.5 rounded text-xs">{result.canonicalPath}</code>
        {!result.sourceMatch && <span className="text-xs text-amber-600">(no match)</span>}
      </div>

      {/* Step 2: Canonical to Target */}
      <div className="flex items-center gap-2 flex-wrap">
        <div className="flex items-center gap-1">
          <HardDrive className="h-4 w-4 text-primary" />
          <span className="text-xs font-medium">QUI</span>
        </div>
        <code className="bg-primary/10 text-primary px-1.5 py-0.5 rounded text-xs">{result.canonicalPath}</code>
        <ArrowRight className="h-3 w-3 text-muted-foreground shrink-0" />
        <div className="flex items-center gap-1">
          <Server className="h-4 w-4 text-green-500" />
          <span className="text-xs font-medium">{targetName}</span>
        </div>
        <code className="bg-muted px-1.5 py-0.5 rounded text-xs">{result.targetPath}</code>
        {!result.targetMatch && <span className="text-xs text-amber-600">(no match)</span>}
      </div>

      {/* Warnings */}
      {hasWarning && (
        <div className="text-xs text-amber-600 dark:text-amber-400 pt-2 border-t border-amber-500/20">
          {!result.sourceMatch && !result.targetMatch ? (
            <p>Neither instance has a matching path mapping. Configure mappings on both instances.</p>
          ) : !result.sourceMatch ? (
            <p>No mapping found for the source path on this instance.</p>
          ) : (
            <p>No mapping found for the canonical path on the target instance.</p>
          )}
        </div>
      )}
    </div>
  )
}
