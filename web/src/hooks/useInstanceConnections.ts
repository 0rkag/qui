/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { api } from "@/lib/api"
import type {
  ConnectionType,
  InstanceConnection,
  InstanceConnectionCreate,
  InstanceConnectionUpdate,
  ConnectionTestRequest,
  ConnectionTestResult,
} from "@/types"
import { isSSHType, isFTPType } from "@/types"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { toast } from "sonner"

export function useInstanceConnections(instanceId: number) {
  const queryClient = useQueryClient()
  const queryKey = ["instances", instanceId, "connections"]

  const { data: connections, isLoading, error } = useQuery({
    queryKey,
    queryFn: () => api.getConnections(instanceId),
    enabled: instanceId > 0,
  })

  const createMutation = useMutation({
    mutationFn: (data: InstanceConnectionCreate) => api.createConnection(instanceId, data),
    onSuccess: (newConnection) => {
      queryClient.setQueryData<InstanceConnection[]>(queryKey, (old) => {
        if (!old) return [newConnection]
        return [...old, newConnection]
      })
      queryClient.invalidateQueries({ queryKey })
    },
    onError: (error: Error) => {
      toast.error("Failed to create connection", { description: error.message })
    },
  })

  const updateMutation = useMutation({
    mutationFn: ({ id, data }: { id: number; data: InstanceConnectionUpdate }) =>
      api.updateConnection(instanceId, id, data),
    onSuccess: (updatedConnection) => {
      queryClient.setQueryData<InstanceConnection[]>(queryKey, (old) => {
        if (!old) return [updatedConnection]
        return old.map((c) => (c.id === updatedConnection.id ? updatedConnection : c))
      })
      queryClient.invalidateQueries({ queryKey })
    },
    onError: (error: Error) => {
      toast.error("Failed to update connection", { description: error.message })
    },
  })

  const deleteMutation = useMutation({
    mutationFn: (connectionId: number) => api.deleteConnection(instanceId, connectionId),
    onSuccess: (_data, connectionId) => {
      queryClient.setQueryData<InstanceConnection[]>(queryKey, (old) => {
        if (!old) return []
        return old.filter((c) => c.id !== connectionId)
      })
      queryClient.invalidateQueries({ queryKey })
    },
    onError: (error: Error) => {
      toast.error("Failed to delete connection", { description: error.message })
    },
  })

  const testMutation = useMutation({
    mutationFn: (data: ConnectionTestRequest) => api.testRemoteConnection(instanceId, data),
    onError: (error: Error) => {
      toast.error("Failed to test connection", { description: error.message })
    },
  })

  const testExistingMutation = useMutation({
    mutationFn: (connectionId: number) => api.testRemoteConnectionExisting(instanceId, connectionId),
    onError: (error: Error) => {
      toast.error("Failed to test connection", { description: error.message })
    },
  })

  // Helper to get connection by type
  const getConnectionByType = (type: ConnectionType): InstanceConnection | undefined => {
    return connections?.find((c) => c.type === type)
  }

  // Helper to check if a type is configured
  const hasType = (type: ConnectionType): boolean => {
    return connections?.some((c) => c.type === type) ?? false
  }

  // Helper to get any SSH connection
  const getSSHConnection = (): InstanceConnection | undefined => {
    return connections?.find((c) => isSSHType(c.type))
  }

  // Helper to get any FTP connection
  const getFTPConnection = (): InstanceConnection | undefined => {
    return connections?.find((c) => isFTPType(c.type))
  }

  // Helper to check if any SSH is configured
  const hasSSH = (): boolean => {
    return connections?.some((c) => isSSHType(c.type)) ?? false
  }

  // Helper to check if any FTP is configured
  const hasFTP = (): boolean => {
    return connections?.some((c) => isFTPType(c.type)) ?? false
  }

  return {
    connections: connections ?? [],
    isLoading,
    error,
    // CRUD operations
    createConnection: createMutation.mutate,
    createConnectionAsync: createMutation.mutateAsync,
    updateConnection: updateMutation.mutate,
    updateConnectionAsync: updateMutation.mutateAsync,
    deleteConnection: deleteMutation.mutate,
    deleteConnectionAsync: deleteMutation.mutateAsync,
    // Test operations
    testConnection: testMutation.mutateAsync,
    testExistingConnection: testExistingMutation.mutateAsync,
    // Loading states
    isCreating: createMutation.isPending,
    isUpdating: updateMutation.isPending,
    isDeleting: deleteMutation.isPending,
    isTesting: testMutation.isPending || testExistingMutation.isPending,
    testResult: (testMutation.data ?? testExistingMutation.data) as ConnectionTestResult | undefined,
    // Helper functions
    getConnectionByType,
    hasType,
    getSSHConnection,
    getFTPConnection,
    hasSSH,
    hasFTP,
    // Convenience getters (for common types)
    sshConnection: getSSHConnection(),
    ftpConnection: getFTPConnection(),
  }
}
