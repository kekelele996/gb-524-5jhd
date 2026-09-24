import { create } from 'zustand'
import { observationApi } from '../api/observations'
import type { BearingObservation, BatchImportPreview, BatchImportResult, BatchValidation, ObservationInput } from '../types/observation'

interface ObservationState {
  observations: BearingObservation[]
  validation: BatchValidation | null
  busy: boolean
  load: (caseId?: number, stationId?: number) => Promise<void>
  createObservation: (input: ObservationInput) => Promise<BearingObservation>
  excludeObservation: (id: number, reason: string) => Promise<void>
  validateCase: (caseId: number) => Promise<BatchValidation>
  previewImport: (caseId: number, text: string) => Promise<BatchImportPreview>
  commitImport: (caseId: number, text: string) => Promise<BatchImportResult>
}

function sortObservations(items: BearingObservation[]): BearingObservation[] {
  return [...items].sort((a, b) => {
    const byTime = new Date(b.observed_at).getTime() - new Date(a.observed_at).getTime()
    return byTime !== 0 ? byTime : b.id - a.id
  })
}

export const useObservationStore = create<ObservationState>((set, get) => ({
  observations: [],
  validation: null,
  busy: false,
  load: async (caseId, stationId) => {
    set({ busy: true })
    try {
      const response = await observationApi.list(caseId, stationId)
      set({ observations: sortObservations(response.data) })
    } finally {
      set({ busy: false })
    }
  },
  createObservation: async (input) => {
    const response = await observationApi.create(input)
    set({ observations: sortObservations([response.data, ...get().observations]) })
    return response.data
  },
  excludeObservation: async (id, reason) => {
    const response = await observationApi.exclude(id, reason)
    set({ observations: get().observations.map((item) => item.id === id ? { ...item, ...response.data } : item) })
  },
  validateCase: async (caseId) => {
    const response = await observationApi.validateCase(caseId)
    set({ validation: response.data })
    return response.data
  },
  previewImport: async (caseId, text) => observationApi.previewImport(caseId, text).then((response) => response.data),
  commitImport: async (caseId, text) => {
    const response = await observationApi.commitImport(caseId, text)
    const imported = new Map(response.data.observations.map((item) => [item.id, item]))
    const merged = get().observations.filter((item) => !imported.has(item.id))
    set({ observations: sortObservations([...response.data.observations, ...merged]) })
    return response.data
  }
}))
