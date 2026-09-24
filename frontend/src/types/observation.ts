import type { ReceiverStation } from './station'

export type ObservationQuality = 'good' | 'fair' | 'poor' | 'excluded'

export interface BearingObservation {
  id: number
  station_id: number
  case_id: number
  bearing_deg: number
  corrected_bearing_deg: number
  signal_dbm: number
  frequency_hz: number
  bandwidth_hz: number
  observed_at: string
  quality: ObservationQuality
  excluded_reason: string
  created_by: number
  created_at: string
  station?: ReceiverStation
}

export interface ObservationInput {
  station_id: number
  case_id: number
  bearing_deg: number
  signal_dbm: number
  frequency_hz: number
  bandwidth_hz: number
  observed_at?: string
  quality: Exclude<ObservationQuality, 'excluded'>
}

export interface ObservationValidation {
  observation_id: number
  valid: boolean
  issues: string[]
  frequency_delta_hz: number
}

export interface BatchValidation {
  case_id: number
  valid: number
  invalid: number
  items: ObservationValidation[]
}

export interface BatchImportLine {
  line: number
  station_code: string
  station_id: number
  station_status: string
  bearing_deg: number
  signal_dbm: number
  frequency_hz: number
  bandwidth_hz: number
  quality: string
  observed_at?: string
  frequency_delta_hz: number
  valid: boolean
  issues: string[]
}

export interface BatchImportPreview {
  case_id: number
  total: number
  valid: number
  invalid: number
  items: BatchImportLine[]
}

export interface BatchImportResult {
  case_id: number
  imported: number
  audit_id: number
  observations: BearingObservation[]
}

