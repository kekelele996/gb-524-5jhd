import type { BatchImportRowInput } from '../types/observation'

export const BATCH_COLUMNS = ['站点编号', '方位°', '频率Hz', '带宽Hz', '信号dBm', '质量', '观测时间(可空)']

const HEADER_HINTS = ['站点', 'station', '方位', 'bearing', '频率', 'frequency']

/**
 * 将粘贴的多站表格文本拆成结构化行。
 * 支持制表符 / 逗号 / 分号 / 连续空白分隔；自动跳过表头和空行。
 * 不足 7 列时右侧补空（观测时间可空），多余列保留在最后一个字段里由后端判错。
 */
export function parseBatchPaste(text: string): BatchImportRowInput[] {
  const lines = text.split(/\r?\n/)
  const rows: BatchImportRowInput[] = []
  for (const line of lines) {
    const trimmed = line.trim()
    if (!trimmed) continue
    if (rows.length === 0 && looksLikeHeader(trimmed)) continue
    const cells = splitLine(trimmed)
    if (cells.length === 0) continue
    while (cells.length < 7) cells.push('')
    rows.push({
      station_code: cells[0] ?? '',
      bearing_deg: cells[1] ?? '',
      frequency_hz: cells[2] ?? '',
      bandwidth_hz: cells[3] ?? '',
      signal_dbm: cells[4] ?? '',
      quality: cells[5] ?? '',
      observed_at: cells[6] ?? '',
    })
  }
  return rows
}

function splitLine(line: string): string[] {
  let separator: RegExp = /\t+/
  if (!line.includes('\t')) {
    if (line.includes(',')) separator = /\s*,\s*/
    else if (line.includes(';')) separator = /\s*;\s*/
    else separator = /\s{2,}/
  }
  const cells = line.split(separator).map((cell) => cell.trim())
  while (cells.length && cells[0] === '') cells.shift()
  while (cells.length && cells[cells.length - 1] === '') cells.pop()
  return cells
}

function looksLikeHeader(line: string): boolean {
  const lowered = line.toLowerCase()
  return HEADER_HINTS.some((hint) => lowered.includes(hint)) && !/^[\s\d,.eE+-]+$/.test(line)
}
