import { describe, expect, it } from 'bun:test'
import { patchJob } from './useJobs'
import { getJobStatusLabel, isWaitingForProvider } from '@/lib/api/jobs'
import type { Job, JobEvent } from '@/types/api'

function mockJob(overrides: Partial<Job> = {}): Job {
  return {
    id: 'job-1',
    type: 'release',
    status: 'retry_wait',
    priority: 'normal',
    paused: false,
    label: 'Album Title',
    metadata_provider: 'musicbrainz',
    media_provider: 'youtube',
    target_id: 'rel-123',
    options: {},
    total: 10,
    completed: 2,
    failed: 0,
    skipped: 0,
    created_at: '2026-09-08T12:00:00Z',
    updated_at: '2026-09-08T12:00:00Z',
    ...overrides,
  }
}

describe('useJobs patchJob – SSE Stale Error Clearing', () => {
  // Test 5: provider-wait SSE sets SESSION_UNAVAILABLE
  it('5. provider-wait SSE sets SESSION_UNAVAILABLE', () => {
    const initialJob = mockJob({ status: 'matching' })
    const event: JobEvent = {
      type: 'job.status',
      time: '2026-09-08T12:01:00Z',
      job_id: 'job-1',
      status: 'retry_wait',
      error_code: 'SESSION_UNAVAILABLE',
      error_message: 'Provider vorübergehend nicht verfügbar',
    }

    const patched = patchJob(initialJob, event)
    expect(patched.status).toBe('retry_wait')
    expect(patched.error_code).toBe('SESSION_UNAVAILABLE')
    expect(patched.error_message).toBe('Provider vorübergehend nicht verfügbar')
    expect(isWaitingForProvider(patched)).toBe(true)
    expect(getJobStatusLabel(patched.status, patched.error_code)).toBe('Wartet auf Provider')
  })

  // Test 6: recovery SSE clears it
  it('6. recovery SSE clears error_code and error_message', () => {
    const waitingJob = mockJob({
      status: 'retry_wait',
      error_code: 'SESSION_UNAVAILABLE',
      error_message: 'Provider vorübergehend nicht verfügbar',
    })

    const recoveryEvent: JobEvent = {
      type: 'job.status',
      time: '2026-09-08T12:02:00Z',
      job_id: 'job-1',
      status: 'matching',
      error_code: '',
      error_message: '',
    }

    const recovered = patchJob(waitingJob, recoveryEvent)
    expect(recovered.status).toBe('matching')
    expect(recovered.error_code).toBeUndefined()
    expect(recovered.error_message).toBeUndefined()
    expect(isWaitingForProvider(recovered)).toBe(false)
  })

  // Test 7: waiting label disappears without reload
  it('7. waiting label disappears without reload', () => {
    const waitingJob = mockJob({
      status: 'retry_wait',
      error_code: 'SESSION_UNAVAILABLE',
      error_message: 'Provider vorübergehend nicht verfügbar',
    })
    expect(getJobStatusLabel(waitingJob.status, waitingJob.error_code)).toBe('Wartet auf Provider')

    const recoveryEvent: JobEvent = {
      type: 'job.status',
      time: '2026-09-08T12:02:00Z',
      job_id: 'job-1',
      status: 'downloading',
      error_code: '',
      error_message: '',
    }

    const recovered = patchJob(waitingJob, recoveryEvent)
    const label = getJobStatusLabel(recovered.status, recovered.error_code)
    expect(label).toBe('Wird heruntergeladen')
    expect(label).not.toContain('Provider')
  })

  // Test 8: REST and equivalent SSE sequence end with same state
  it('8. REST and equivalent SSE sequence end with same state', () => {
    // A job fetched via REST after recovery has no error_code or error_message
    const restJob: Job = {
      id: 'job-1',
      type: 'release',
      status: 'matching',
      priority: 'normal',
      paused: false,
      label: 'Album Title',
      metadata_provider: 'musicbrainz',
      media_provider: 'youtube',
      target_id: 'rel-123',
      options: {},
      total: 10,
      completed: 2,
      failed: 0,
      skipped: 0,
      created_at: '2026-09-08T12:00:00Z',
      updated_at: '2026-09-08T12:02:00Z',
    }

    // A job started in retry_wait + SESSION_UNAVAILABLE then receiving recovery SSE
    const waitingJob = mockJob({
      status: 'retry_wait',
      error_code: 'SESSION_UNAVAILABLE',
      error_message: 'Provider vorübergehend nicht verfügbar',
      created_at: '2026-09-08T12:00:00Z',
      updated_at: '2026-09-08T12:01:00Z',
    })

    const recoveryEvent: JobEvent = {
      type: 'job.status',
      time: '2026-09-08T12:02:00Z',
      job_id: 'job-1',
      status: 'matching',
      error_code: '',
      error_message: '',
    }

    const sseJob = patchJob(waitingJob, recoveryEvent)
    expect(sseJob).toEqual(restJob)
  })

  // Test 9: terminal replacement error works
  it('9. terminal replacement error works', () => {
    const waitingJob = mockJob({
      status: 'retry_wait',
      error_code: 'SESSION_UNAVAILABLE',
      error_message: 'Provider vorübergehend nicht verfügbar',
    })

    const failEvent: JobEvent = {
      type: 'job.failed',
      time: '2026-09-08T12:03:00Z',
      job_id: 'job-1',
      status: 'failed',
      error_code: 'MAX_RETRIES_EXCEEDED',
      error_message: 'Max retry attempts exceeded',
    }

    const failed = patchJob(waitingJob, failEvent)
    expect(failed.status).toBe('failed')
    expect(failed.error_code).toBe('MAX_RETRIES_EXCEEDED')
    expect(failed.error_message).toBe('Max retry attempts exceeded')
    expect(isWaitingForProvider(failed)).toBe(false)
  })

  // Test 10: generic retry error can be cleared
  it('10. generic retry error can be cleared', () => {
    const genericRetryJob = mockJob({
      status: 'retry_wait',
      error_code: 'RATE_LIMITED',
      error_message: 'Too many requests',
    })
    expect(getJobStatusLabel(genericRetryJob.status, genericRetryJob.error_code)).toBe('Wartet auf Wiederholung')

    const clearEvent: JobEvent = {
      type: 'job.status',
      time: '2026-09-08T12:04:00Z',
      job_id: 'job-1',
      status: 'downloading',
      error_code: '',
      error_message: '',
    }

    const cleared = patchJob(genericRetryJob, clearEvent)
    expect(cleared.status).toBe('downloading')
    expect(cleared.error_code).toBeUndefined()
    expect(cleared.error_message).toBeUndefined()
    expect(getJobStatusLabel(cleared.status, cleared.error_code)).toBe('Wird heruntergeladen')
  })
})
