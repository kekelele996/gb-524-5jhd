import { useState } from 'react'
import ErrorOutlineRounded from '@mui/icons-material/ErrorOutlineRounded'
import RuleRounded from '@mui/icons-material/RuleRounded'
import {
  Alert, Box, Button, Chip, Dialog, DialogActions, DialogContent, DialogTitle,
  Stack, Table, TableBody, TableCell, TableHead, TableRow, TextField, Typography,
} from '@mui/material'
import { ApiError } from '../../api/client'
import { useObservationStore } from '../../stores/observationStore'
import type { BatchImportPreview, BatchImportRowInput, ImportRowPreview, ObservationQuality } from '../../types/observation'
import { formatDateTime, formatDecimal, formatFrequency } from '../../utils/format'
import { BATCH_COLUMNS, parseBatchPaste } from '../../utils/batchImport'

const MAX_ROWS = 100

const qualityLabels: Partial<Record<ObservationQuality, string>> = { good: '良好', fair: '一般', poor: '偏低' }

interface BatchImportDialogProps {
  open: boolean
  caseId: number
  caseCode: string
  onClose: () => void
  onImported: (count: number) => void
}

export function BatchImportDialog({ open, caseId, caseCode, onClose, onImported }: BatchImportDialogProps) {
  const previewBatch = useObservationStore((state) => state.previewBatch)
  const importBatch = useObservationStore((state) => state.importBatch)
  const [text, setText] = useState('')
  const [rows, setRows] = useState<BatchImportRowInput[]>([])
  const [preview, setPreview] = useState<BatchImportPreview | null>(null)
  const [checking, setChecking] = useState(false)
  const [importing, setImporting] = useState(false)
  const [clientError, setClientError] = useState('')

  const busy = checking || importing
  const overLimit = rows.length > MAX_ROWS

  const updateText = (value: string) => {
    setText(value)
    setRows(parseBatchPaste(value))
    setPreview(null)
    setClientError('')
  }

  const check = async () => {
    if (rows.length === 0) {
      setClientError('没有可校验的数据行。每行至少包含站点编号、方位、频率、带宽、信号、质量 6 列。')
      return
    }
    if (overLimit) {
      setClientError(`单次最多导入 ${MAX_ROWS} 行，当前 ${rows.length} 行，请分批粘贴。`)
      return
    }
    setChecking(true)
    try {
      const result = await previewBatch(caseId, rows)
      setPreview(result)
    } catch {
      // 错误提示由全局 api:error 处理
    } finally {
      setChecking(false)
    }
  }

  const confirmImport = async () => {
    setImporting(true)
    try {
      const result = await importBatch(caseId, rows)
      onImported(result.imported_count)
      reset()
    } catch (error) {
      if (error instanceof ApiError && error.code === 'BATCH_IMPORT_REJECTED') {
        const details = error.details
        const items = Array.isArray(details?.items) ? (details?.items as ImportRowPreview[]) : undefined
        const valid = typeof details?.valid === 'number' ? details.valid : 0
        const invalid = typeof details?.invalid === 'number' ? details.invalid : rows.length
        if (items) {
          setPreview({ case_id: caseId, case_code: caseCode, center_frequency_hz: 0, total: items.length, valid, invalid, items })
        }
      }
    } finally {
      setImporting(false)
    }
  }

  const reset = () => {
    setText('')
    setRows([])
    setPreview(null)
    setClientError('')
  }

  const close = () => {
    if (busy) return
    reset()
    onClose()
  }

  return (
    <Dialog open={open} onClose={close} fullWidth maxWidth="lg">
      <DialogTitle>批量导入多站测向记录 · {caseCode}</DialogTitle>
      <DialogContent>
        <Stack gap={2} sx={{ pt: 1 }}>
          <Alert severity="info" variant="outlined">
            粘贴后先逐行校验，站点或频率等问题会在每行列出原因；存在任何不合格行时整批不会保存。
          </Alert>
          <TextField
            label="粘贴表格行"
            value={text}
            onChange={(event) => updateText(event.target.value)}
            multiline
            minRows={6}
            maxRows={12}
            fullWidth
            placeholder={`${BATCH_COLUMNS.join('\t')}\nRX-WEST\t90.5\t433920000\t12500\t-70\tgood\t2026-09-24 10:05:00\nRX-SOUTH\t12.0\t433921000\t12500\t-75\tfair`}
            helperText={`支持制表符 / 逗号 / 分号分隔，表头与空行自动跳过；每行格式：${BATCH_COLUMNS.join('、')}。当前 ${rows.length} 行${overLimit ? '（超过上限）' : ''}`}
            error={overLimit}
          />
          {clientError && <Alert severity="error">{clientError}</Alert>}
          {preview && (
            <Box>
              <Stack direction="row" alignItems="center" gap={1} mb={1}>
                <RuleRounded color={preview.invalid ? 'warning' : 'success'} />
                <Typography variant="subtitle1">
                  逐行校验结果：共 {preview.total} 行，
                  <Chip size="small" color="success" variant="outlined" label={`${preview.valid} 行可用`} sx={{ mx: 0.5 }} />
                  {preview.invalid > 0 && <Chip size="small" color="error" variant="outlined" label={`${preview.invalid} 行不合格`} sx={{ mx: 0.5 }} />}
                </Typography>
              </Stack>
              <Box className="table-scroll" sx={{ maxHeight: 360 }}>
                <Table size="small" aria-label="批量导入逐行校验结果">
                  <TableHead>
                    <TableRow>
                      <TableCell padding="checkbox">行</TableCell>
                      <TableCell>站点</TableCell>
                      <TableCell>原始 → 校正方位</TableCell>
                      <TableCell>频率 / 带宽</TableCell>
                      <TableCell>信号</TableCell>
                      <TableCell>质量</TableCell>
                      <TableCell>观测时间</TableCell>
                      <TableCell>校验结果 / 原因</TableCell>
                    </TableRow>
                  </TableHead>
                  <TableBody>
                    {preview.items.map((item) => (
                      <TableRow key={item.line} hover className={item.valid ? '' : 'row-invalid'}>
                        <TableCell padding="checkbox">{item.line}</TableCell>
                        <TableCell>{item.station_code || '—'}{item.station_id > 0 && <span className="secondary-text"> #{item.station_id}</span>}</TableCell>
                        <TableCell className="numeric">{formatDecimal(item.bearing_deg, 1)}°{item.station_id > 0 && <> → <strong>{formatDecimal(item.corrected_bearing_deg, 1)}°</strong></>}</TableCell>
                        <TableCell className="numeric">{item.frequency_hz > 0 ? formatFrequency(item.frequency_hz) : '—'}<br /><span className="secondary-text">BW {item.bandwidth_hz > 0 ? formatFrequency(item.bandwidth_hz) : '—'}</span></TableCell>
                        <TableCell className="numeric">{item.valid ? formatDecimal(item.signal_dbm, 1) : (rows[item.line - 1]?.signal_dbm || '—')}</TableCell>
                        <TableCell>{qualityLabels[item.quality as ObservationQuality] ?? '—'}</TableCell>
                        <TableCell>{item.observed_at ? formatDateTime(item.observed_at) : '导入时刻'}</TableCell>
                        <TableCell>
                          {item.valid
                            ? <Chip size="small" color="success" variant="outlined" label="可用" />
                            : <Stack gap={0.5}>{item.issues.map((issue) => (
                              <Typography key={issue} variant="caption" color="error" display="flex" alignItems="center" gap={0.5}>
                                <ErrorOutlineRounded fontSize="inherit" />{issue}
                              </Typography>
                            ))}</Stack>}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </Box>
              {preview.invalid > 0 && <Alert severity="warning" sx={{ mt: 1 }}>存在不合格行：请修正文本后重新校验，整批记录当前未保存。</Alert>}
            </Box>
          )}
        </Stack>
      </DialogContent>
      <DialogActions>
        <Button onClick={close} disabled={busy}>取消</Button>
        <Button variant="outlined" onClick={() => void check()} disabled={busy || rows.length === 0 || overLimit}>
          {checking ? '校验中…' : '逐行校验'}
        </Button>
        <Button
          variant="contained"
          color="success"
          onClick={() => void confirmImport()}
          disabled={busy || !preview || preview.invalid > 0 || preview.valid === 0}
        >
          {importing ? '写入中…' : `确认导入 ${preview && preview.invalid === 0 ? preview.valid : ''} 条`}
        </Button>
      </DialogActions>
    </Dialog>
  )
}
