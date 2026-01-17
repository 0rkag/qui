/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { api } from "@/lib/api"
import type {
  InstanceConnection,
  InstanceConnectionCreate,
  InstanceConnectionUpdate,
  SSHTestRequest,
  SSHTestResult,
} from "@/types"
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
    mutationFn: (data: SSHTestRequest) => api.testSSHConnection(instanceId, data),
    onError: (error: Error) => {
      toast.error("Failed to test connection", { description: error.message })
    },
  })

  const testExistingMutation = useMutation({
    mutationFn: (connectionId: number) => api.testSSHConnectionExisting(instanceId, connectionId),
    onError: (error: Error) => {
      toast.error("Failed to test connection", { description: error.message })
    },
  })

  // Helper to get connection by protocol
  const getConnectionByProtocol = (protocol: "ssh" | "sftp" | "ftp"): InstanceConnection | undefined => {
    return connections?.find((c) => c.protocol === protocol)
  }

  // Helper to check if a protocol is configured
  const hasProtocol = (protocol: "ssh" | "sftp" | "ftp"): boolean => {
    return connections?.some((c) => c.protocol === protocol) ?? false
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
    testResult: (testMutation.data ?? testExistingMutation.data) as SSHTestResult | undefined,
    // Helper functions
    getConnectionByProtocol,
    hasProtocol,
    // Convenience getters
    sshConnection: getConnectionByProtocol("ssh"),
    sftpConnection: getConnectionByProtocol("sftp"),
    ftpConnection: getConnectionByProtocol("ftp"),
  }
}
