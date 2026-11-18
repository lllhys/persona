// Native JSONL exports can omit error steps that remain in the conversation service.
export function nativeErrorStep(step, index) {
  if (step?.type !== 'CORTEX_STEP_TYPE_ERROR_MESSAGE') return null
  if (['CORTEX_STEP_STATUS_CANCELLED', 'CORTEX_STEP_STATUS_CANCELED'].includes(step.status)) return null
  const error = step.errorMessage?.error
  if (!error) return null
  const text = error.userErrorMessage || error.modelErrorMessage || error.shortError
  if (typeof text !== 'string' || !text.trim()) return null
  const info = { turn_error: text.trim() }
  let details
  try { details = typeof error.details === 'string' ? JSON.parse(error.details) : error.details } catch {}
  const entries = Array.isArray(details?.error?.details) ? details.error.details : []
  const quota = entries.find((entry) => entry?.reason === 'QUOTA_EXHAUSTED')
  const reset = quota?.metadata?.quotaResetTimeStamp
  if (typeof reset === 'string' && Number.isFinite(Date.parse(reset))) {
    info.turn_error_kind = 'usage_limit'
    info.turn_error_resets_at = new Date(reset).toISOString()
  }
  if (Number(error.errorCode) === 429) info.turn_error_code = 'rate_limited'
  return {
    step_index: index,
    type: 'ERROR_MESSAGE',
    status: 'DONE',
    created_at: step.metadata?.createdAt,
    content: info.turn_error,
    turn_error: info,
  }
}
