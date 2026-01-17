/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useState, useMemo } from "react"
import { ArrowRight, Pencil, Play, Plus, Trash2 } from "lucide-react"
import { toast } from "sonner"

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
import { useInstancePathMappings } from "@/hooks/useInstancePathMappings"
import type { InstancePathMapping, InstancePathMappingCreate } from "@/types"

interface PathMappingsEditorProps {
  instanceId: number
}

export function PathMappingsEditor({ instanceId }: PathMappingsEditorProps) {
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

  const [editingId, setEditingId] = useState<number | null>(null)
  const [isAdding, setIsAdding] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState<InstancePathMapping | null>(null)

  // Form state for adding/editing
  const [formData, setFormData] = useState<InstancePathMappingCreate>({
    instancePath: "",
    canonicalPath: "",
    enabled: true,
    description: "",
  })

  // Test path state
  const [testInput, setTestInput] = useState("")
  const [testDirection, setTestDirection] = useState<"to_canonical" | "from_canonical">("to_canonical")
  const [testResult, setTestResult] = useState<{ input: string; output: string; noMatch: boolean } | null>(null)

  // Validation errors (computed from form data)
  const validationErrors = useMemo(() => ({
    instancePath: validatePath(formData.instancePath),
    canonicalPath: validatePath(formData.canonicalPath),
  }), [formData.instancePath, formData.canonicalPath])

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
    // Validate both paths
    const instanceError = validatePath(formData.instancePath)
    const canonicalError = validatePath(formData.canonicalPath)

    if (instanceError) {
      toast.error(`Instance path: ${instanceError}`)
      return
    }
    if (canonicalError) {
      toast.error(`Canonical path: ${canonicalError}`)
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

  const handleTest = async () => {
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

  if (isLoading) {
    return <div className="text-sm text-muted-foreground">Loading path mappings...</div>
  }

  return (
    <div className="space-y-6">
      {/* Description */}
      <div className="text-sm text-muted-foreground">
        Path mappings translate between how qBittorrent sees paths and how QUI (your server) sees them.
        This is useful when QUI and qBittorrent are on different machines or use different mount points.
      </div>

      {/* Path Mappings List */}
      <div className="space-y-2">
        {mappings.length === 0 && !isAdding && (
          <div className="text-sm text-muted-foreground py-4 text-center border border-dashed rounded-md">
            No path mappings configured. Add one to get started.
          </div>
        )}

        {mappings.map((mapping) => (
          <div
            key={mapping.id}
            className={`flex items-center gap-3 p-3 border rounded-md ${
              !mapping.enabled ? "opacity-50" : ""
            } ${editingId === mapping.id ? "border-primary" : ""}`}
          >
            {editingId === mapping.id ? (
              // Edit mode
              <div className="flex-1 space-y-3">
                <div className="grid grid-cols-2 gap-3">
                  <div className="space-y-1">
                    <Label className="text-xs">Instance Path</Label>
                    <Input
                      value={formData.instancePath}
                      onChange={(e) => setFormData({ ...formData, instancePath: e.target.value })}
                      placeholder="/downloads"
                      className={validationErrors.instancePath && formData.instancePath ? "border-destructive" : ""}
                    />
                    {validationErrors.instancePath && formData.instancePath && (
                      <p className="text-xs text-destructive">{validationErrors.instancePath}</p>
                    )}
                  </div>
                  <div className="space-y-1">
                    <Label className="text-xs">Canonical Path (QUI Server)</Label>
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
                <div className="flex items-center gap-4">
                  <div className="flex-1">
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
                  <Button size="sm" onClick={handleSave} disabled={isUpdating}>
                    {isUpdating ? "Saving..." : "Save"}
                  </Button>
                  <Button size="sm" variant="outline" onClick={resetForm}>
                    Cancel
                  </Button>
                </div>
              </div>
            ) : (
              // View mode
              <>
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2 text-sm">
                    <code className="bg-muted px-1.5 py-0.5 rounded text-xs truncate max-w-[200px]" title={mapping.instancePath}>
                      {mapping.instancePath}
                    </code>
                    <ArrowRight className="h-3 w-3 text-muted-foreground shrink-0" />
                    <code className="bg-muted px-1.5 py-0.5 rounded text-xs truncate max-w-[200px]" title={mapping.canonicalPath}>
                      {mapping.canonicalPath}
                    </code>
                  </div>
                  {mapping.description && (
                    <div className="text-xs text-muted-foreground mt-1">{mapping.description}</div>
                  )}
                </div>
                <div className="flex items-center gap-1">
                  <Button size="icon" variant="ghost" onClick={() => handleEdit(mapping)}>
                    <Pencil className="h-4 w-4" />
                  </Button>
                  <Button size="icon" variant="ghost" onClick={() => setDeleteTarget(mapping)}>
                    <Trash2 className="h-4 w-4 text-destructive" />
                  </Button>
                </div>
              </>
            )}
          </div>
        ))}

        {/* Add new mapping form */}
        {isAdding && (
          <div className="p-3 border border-primary rounded-md space-y-3">
            <div className="grid grid-cols-2 gap-3">
              <div className="space-y-1">
                <Label className="text-xs">Instance Path</Label>
                <Input
                  value={formData.instancePath}
                  onChange={(e) => setFormData({ ...formData, instancePath: e.target.value })}
                  placeholder="/downloads"
                  autoFocus
                  className={validationErrors.instancePath && formData.instancePath ? "border-destructive" : ""}
                />
                {validationErrors.instancePath && formData.instancePath && (
                  <p className="text-xs text-destructive">{validationErrors.instancePath}</p>
                )}
              </div>
              <div className="space-y-1">
                <Label className="text-xs">Canonical Path (QUI Server)</Label>
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
            <div className="flex items-center gap-4">
              <div className="flex-1">
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
              <Button size="sm" onClick={handleSave} disabled={isCreating}>
                {isCreating ? "Creating..." : "Create"}
              </Button>
              <Button size="sm" variant="outline" onClick={resetForm}>
                Cancel
              </Button>
            </div>
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

      {/* Path Tester */}
      <div className="border-t pt-4 space-y-3">
        <div className="text-sm font-medium">Test Path Translation</div>
        <div className="flex gap-2">
          <Input
            value={testInput}
            onChange={(e) => setTestInput(e.target.value)}
            placeholder="Enter a path to test..."
            className="flex-1"
          />
          <Select value={testDirection} onValueChange={(v) => setTestDirection(v as typeof testDirection)}>
            <SelectTrigger className="w-[180px]">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="to_canonical">To Canonical</SelectItem>
              <SelectItem value="from_canonical">From Canonical</SelectItem>
            </SelectContent>
          </Select>
          <Button onClick={handleTest} disabled={isTesting}>
            <Play className="h-4 w-4 mr-1" />
            {isTesting ? "Testing..." : "Test"}
          </Button>
        </div>
        {testResult && (
          <div className={`p-3 rounded-md text-sm ${testResult.noMatch ? "bg-amber-500/10 border border-amber-500/20" : "bg-green-500/10 border border-green-500/20"}`}>
            <div className="flex items-center gap-2">
              <code className="bg-muted px-1.5 py-0.5 rounded text-xs">{testResult.input}</code>
              <ArrowRight className="h-3 w-3 text-muted-foreground" />
              <code className="bg-muted px-1.5 py-0.5 rounded text-xs">{testResult.output}</code>
            </div>
            {testResult.noMatch && (
              <div className="text-xs text-amber-600 dark:text-amber-400 mt-1">
                No matching rule found - path returned unchanged
              </div>
            )}
          </div>
        )}
      </div>

      {/* Delete confirmation dialog */}
      <AlertDialog open={!!deleteTarget} onOpenChange={(open) => !open && setDeleteTarget(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete Path Mapping</AlertDialogTitle>
            <AlertDialogDescription>
              Are you sure you want to delete this path mapping?
              <div className="mt-2 p-2 bg-muted rounded text-sm">
                <code>{deleteTarget?.instancePath}</code> → <code>{deleteTarget?.canonicalPath}</code>
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
  )
}
