import { describe, expect, it } from 'vitest'
import { parseBatchPaste } from './batchImport'

describe('parseBatchPaste', () => {
  it('parses tab separated rows and skips header and blank lines', () => {
    const text = [
      '站点编号\t方位°\t频率Hz\t带宽Hz\t信号dBm\t质量\t观测时间',
      'RX-WEST\t90.5\t433920000\t12500\t-70\tgood\t2026-09-24 10:05:00',
      '',
      'RX-SOUTH\t12\t433921000\t12500\t-75\tfair',
    ].join('\n')
    const rows = parseBatchPaste(text)
    expect(rows).toHaveLength(2)
    expect(rows[0]).toMatchObject({
      station_code: 'RX-WEST', bearing_deg: '90.5', frequency_hz: '433920000',
      bandwidth_hz: '12500', signal_dbm: '-70', quality: 'good', observed_at: '2026-09-24 10:05:00',
    })
    expect(rows[1].observed_at).toBe('')
    expect(rows[1].station_code).toBe('RX-SOUTH')
  })

  it('supports comma and semicolon separators', () => {
    expect(parseBatchPaste('RX-WEST,90.5,433920000,12500,-70,good')[0].station_code).toBe('RX-WEST')
    expect(parseBatchPaste('RX-SOUTH;12;433921000;12500;-75;fair')[0].signal_dbm).toBe('-75')
  })

  it('pads incomplete rows so backend reports missing columns', () => {
    const rows = parseBatchPaste('RX-WEST\t90.5')
    expect(rows).toHaveLength(1)
    expect(rows[0]).toEqual({
      station_code: 'RX-WEST', bearing_deg: '90.5', frequency_hz: '', bandwidth_hz: '',
      signal_dbm: '', quality: '', observed_at: '',
    })
  })

  it('does not treat numeric tabular content as a header', () => {
    const rows = parseBatchPaste('RX-WEST\t90\t433920000\t12500\t-70\tgood')
    expect(rows).toHaveLength(1)
  })
})
