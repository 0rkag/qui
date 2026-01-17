/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { api } from "@/lib/api"
import type {
  InstancePathMapping,
  InstancePathMappingCreate,
  InstancePathMappingUpdate,
  PathTestRequest,
  PathTestResponse,
} from "@/types"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { toast } from "sonner"

export function useInstancePathMappings(instanceId: number) {
  const queryClient = useQueryClient()
  const queryKey = ["instances", instanceId, "path-mappings"]

  const { data: mappings, isLoading, error } = useQuery({
    queryKey,
    queryFn: () => api.getPathMappings(instanceId),
    enabled: instanceId > 0,
  })

  const createMutation = useMutation({
    mutationFn: (data: InstancePathMappingCreate) => api.createPathMapping(instanceId, data),
    onSuccess: (newMapping) => {
      queryClient.setQueryData<InstancePathMapping[]>(queryKey, (old) => {
        if (!old) return [newMapping]
        return [...old, newMapping]
      })
      queryClient.invalidateQueries({ queryKey })
    },
    onError: (error: Error) => {
      toast.error("Failed to create path mapping", { description: error.message })
    },
  })

  const updateMutation = useMutation({
    mutationFn: ({ id, data }: { id: number; data: InstancePathMappingUpdate }) =>
      api.updatePathMapping(instanceId, id, data),
    onSuccess: (updatedMapping) => {
      queryClient.setQueryData<InstancePathMapping[]>(queryKey, (old) => {
        if (!old) return [updatedMapping]
        return old.map((m) => (m.id === updatedMapping.id ? updatedMapping : m))
      })
      queryClient.invalidateQueries({ queryKey })
    },
    onError: (error: Error) => {
      toast.error("Failed to update path mapping", { description: error.message })
    },
  })

  const deleteMutation = useMutation({
    mutationFn: (mappingId: number) => api.deletePathMapping(instanceId, mappingId),
    onSuccess: (_data, mappingId) => {
      queryClient.setQueryData<InstancePathMapping[]>(queryKey, (old) => {
        if (!old) return []
        return old.filter((m) => m.id !== mappingId)
      })
      queryClient.invalidateQueries({ queryKey })
    },
    onError: (error: Error) => {
      toast.error("Failed to delete path mapping", { description: error.message })
    },
  })

  const reorderMutation = useMutation({
    mutationFn: (orders: Record<number, number>) => api.reorderPathMappings(instanceId, orders),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey })
    },
    onError: (error: Error) => {
      toast.error("Failed to reorder path mappings", { description: error.message })
    },
  })

  const testMutation = useMutation({
    mutationFn: (data: PathTestRequest) => api.testPathMapping(instanceId, data),
    onError: (error: Error) => {
      toast.error("Failed to test path mapping", { description: error.message })
    },
  })

  return {
    mappings: mappings ?? [],
    isLoading,
    error,
    createMapping: createMutation.mutate,
    createMappingAsync: createMutation.mutateAsync,
    updateMapping: updateMutation.mutate,
    updateMappingAsync: updateMutation.mutateAsync,
    deleteMapping: deleteMutation.mutate,
    deleteMappingAsync: deleteMutation.mutateAsync,
    reorderMappings: reorderMutation.mutate,
    testPath: testMutation.mutateAsync,
    isCreating: createMutation.isPending,
    isUpdating: updateMutation.isPending,
    isDeleting: deleteMutation.isPending,
    isReordering: reorderMutation.isPending,
    isTesting: testMutation.isPending,
    testResult: testMutation.data as PathTestResponse | undefined,
  }
}
