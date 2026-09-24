import { useEffect, useMemo, useState } from 'react'
import CheckCircleOutlineRounded from '@mui/icons-material/CheckCircleOutlineRounded'
import ErrorOutlineRounded from '@mui/icons-material/ErrorOutlineRounded'
import PostAddRounded from '@mui/icons-material/PostAddRounded'
import {
  Alert,
  Button,
  Chip,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  Stack,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableRow,
  TextField,
  Typography
} from '@mui/material'
import type { BatchImportLine, BatchImportPreview } from '../../types/observation'
import { formatDecimal, formatFrequency } from '../../utils/format'

interface BatchImportDialogProps {
  open: boolean
  caseId: number
  caseCode: string
  onClose: () => void
  onPreview: (caseId: number, text: string) => Promise<BatchImportPreview>
  onCommit: (caseId: number, text: string) => Promise<{ imported: number }>
}

const FORMAT_HINT = '每行一条，制表符/逗号/分号分隔，可含表头，列顺序：站点编号、方位(度)、质量(good/fair/poor 或 良好/一般/差)、频率(Hz)、带宽(Hz)、信号(dBm)、观测时间(可留空，取当前时间)'

export function BatchImportDialog({ open, caseId, caseCode, onClose, onPreview, onCommit }: BatchImportDialogProps) {
  const [text, setText] = useState('')
  const [preview, setPreview] = useState<BatchImportPreview | null>(null)
  const [checking, setChecking] = useState(false)
  const [importing, setImporting] = useState(false)
  const [serverError, setServerError] = useState('')
  const [doneMessage, setDoneMessage] = useState('')

  useEffect(() => {
    if (open) {
      setText('')
      setPreview(null)
      setServerError('')
      setDoneMessage('')
      setChecking(false)
      setImporting(false)
    }
  }, [open])

  const rowCount = useMemo(() => text.split('\n').filter((line) => line.trim() !== '').length, [text])

  const runPreview = async () => {
    if (!text.trim()) return
    setChecking(true)
    setServerError('')
    setDoneMessage('')
    try {
      setPreview(await onPreview(caseId, text))
    } catch {
      setPreview(null)
    } finally {
      setChecking(false)
    }
  }

  const runCommit = async () => {
    if (!preview || preview.invalid > 0) return
    setImporting(true)
    setServerError('')
    try {
      const result = await onCommit(caseId, text)
      setDoneMessage(`已将 ${result.imported} 条观测一次性写入案例 ${caseCode}，并记录批量导入审计。`)
      setPreview(null)
      setText('')
    } catch {
      // 全局错误条已展示；保留粘贴内容与校验结果，方便修正后重试。
    } finally {
      setImporting(false)
    }
  }

  const busy = checking || importing

  return (
    <Dialog open={open} onClose={busy ? undefined : onClose} fullWidth maxWidth="lg" aria-labelledby="batch-import-title">
      <DialogTitle id="batch-import-title">批量导入方位观测 · {caseCode}</DialogTitle>
      <DialogContent>
        <Stack gap={2} sx={{ pt: 1 }}>
          <Alert severity="info">
            粘贴现场多站表格后先逐行校验；确认导入时采用整批事务——任意一行不合格，整批都不会写入。单条录入与人工排除不受影响。
          </Alert>
          <TextField
            label="多行观测记录"
            value={text}
            onChange={(event) => { setText(event.target.value); setPreview(null); setDoneMessage('') }}
            multiline
            minRows={5}
            maxRows={10}
            placeholder={'站点编号\t方位\t质量\t频率\t带宽\t信号\t观测时间\nRX-WEST\t89.6\tgood\t433920300\t12500\t-67\t2026-09-24 10:00:00'}
            slotProps={{ htmlInput: { style: { fontFamily: 'monospace', fontSize: 13 } } }}
            helperText={FORMAT_HINT}
          />
          {doneMessage && <Alert severity="success" icon={<CheckCircleOutlineRounded />}>{doneMessage}</Alert>}
          {serverError && <Alert severity="error">{serverError}</Alert>}

          {preview && (
            <>
              <Alert severity={preview.invalid > 0 ? 'warning' : 'success'} icon={preview.invalid > 0 ? <ErrorOutlineRounded /> : <CheckCircleOutlineRounded />}>
                共 {preview.total} 行：{preview.valid} 行可用，{preview.invalid} 行不可用
                {preview.invalid > 0 ? '。请修正或删除不可用行后重新校验，整批方可导入。' : '。全部可用，可以一次性写入案例。'}
              </Alert>
              <BatchImportTable items={preview.items} />
            </>
          )}
          {!preview && !doneMessage && rowCount > 0 && <Typography variant="body2" color="text.secondary">已粘贴 {rowCount} 行，点击“逐行校验”查看可用性与原因。</Typography>}
        </Stack>
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose} disabled={busy}>{doneMessage ? '关闭' : '继续查看'}</Button>
        <Button variant="outlined" startIcon={<ErrorOutlineRounded />} onClick={() => void runPreview()} disabled={busy || !text.trim() || checking}>
          {checking ? '校验中…' : '逐行校验'}
        </Button>
        <Button
          variant="contained"
          startIcon={<PostAddRounded />}
          onClick={() => void runCommit()}
          disabled={busy || !preview || preview.invalid > 0 || preview.valid === 0 || importing}
        >
          {importing ? '写入中…' : `确认导入 ${preview && preview.invalid === 0 ? preview.valid : ''} 条`}
        </Button>
      </DialogActions>
    </Dialog>
  )
}

function BatchImportTable({ items }: { items: BatchImportLine[] }) {
  return (
    <div style={{ maxHeight: 320, overflow: 'auto', border: '1px solid #d5dfd8', borderRadius: 6 }}>
      <Table size="small" stickyHeader aria-label="批量导入逐行校验结果">
        <TableHead>
          <TableRow>
            <TableCell>行</TableCell>
            <TableCell>站点</TableCell>
            <TableCell>方位</TableCell>
            <TableCell>质量</TableCell>
            <TableCell>频率 / 带宽</TableCell>
            <TableCell>信号</TableCell>
            <TableCell>观测时间</TableCell>
            <TableCell>结论 / 原因</TableCell>
          </TableRow>
        </TableHead>
        <TableBody>
          {items.map((item) => (
            <TableRow key={item.line} hover className={item.valid ? '' : 'row-warning'}>
              <TableCell>{item.line}</TableCell>
              <TableCell>{item.station_code || '—'}{item.station_status && item.station_status !== 'active' && <Typography variant="caption" display="block" color="warning.main">状态：{item.station_status}</Typography>}</TableCell>
              <TableCell className="numeric">{item.bearing_deg ? formatDecimal(item.bearing_deg, 1) : '—'}</TableCell>
              <TableCell>{item.quality || '—'}</TableCell>
              <TableCell className="numeric">{item.frequency_hz ? formatFrequency(item.frequency_hz) : '—'}<br /><span className="secondary-text">{item.bandwidth_hz ? formatFrequency(item.bandwidth_hz) : ''}</span></TableCell>
              <TableCell className="numeric">{item.signal_dbm ? formatDecimal(item.signal_dbm, 1) : '—'}</TableCell>
              <TableCell>{item.observed_at || '当前时间'}</TableCell>
              <TableCell>
                {item.valid
                  ? <Chip size="small" color="success" variant="outlined" icon={<CheckCircleOutlineRounded />} label="可用" />
                  : <Stack gap={0.5}>{item.issues.map((issue) => <Chip key={issue} size="small" color="error" variant="outlined" icon={<ErrorOutlineRounded />} label={issue} />)}</Stack>}
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}
