/**
 * The backend's wire format, transcribed from the Go source. Every type below
 * mirrors a struct in backend/internal; no field exists here that the backend
 * does not send.
 *
 *   Artist, Release, Track, File   internal/music
 *   Job, Item, Summary, Event      internal/jobs
 *   Subscription, SyncResult       internal/subscriptions
 *   ProviderInfo                   internal/provider/registry.go
 *   Settings, SettingsUpdate       internal/settings
 *   Health                         internal/api/handlers/handlers.go
 *   ErrorCode                      internal/apperr/errors.go
 */

/* ------------------------------------------------------------------ envelope */

/** Every successful answer carries its payload under "data". */
export interface Envelope<T> {
  data: T
  meta?: ListMeta
}

/** List answers add a meta block. */
export interface ListMeta {
  count: number
  total?: number
  limit?: number
  offset?: number
}

/** The failure envelope. */
export interface ErrorEnvelope {
  error: {
    code: string
    message: string
    request_id?: string
  }
}

/* --------------------------------------------------------------- error codes */

/** The stable error codes from internal/apperr/errors.go. */
export const ERROR_CODES = [
  'PROVIDER_UNAVAILABLE',
  'PROVIDER_RATE_LIMITED',
  'PROVIDER_NOT_FOUND',
  'ARTIST_NOT_FOUND',
  'RELEASE_NOT_FOUND',
  'TRACK_NOT_FOUND',
  'JOB_NOT_FOUND',
  'SUBSCRIPTION_NOT_FOUND',
  'MATCH_FAILED',
  'DOWNLOAD_FAILED',
  'INVALID_AUDIO',
  'TAGGING_FAILED',
  'ALREADY_EXISTS',
  'JOB_CANCELLED',
  'INVALID_REQUEST',
  'UNSUPPORTED_MEDIA_TYPE',
  'TOOL_UNAVAILABLE',
  'UNAUTHENTICATED',
  'FORBIDDEN',
  'INVALID_CREDENTIALS',
  'USER_NOT_FOUND',
  'SESSION_NOT_FOUND',
  'LAST_ADMIN',
  'CSRF_INVALID',
  'RATE_LIMITED',
  'SETUP_REQUIRED',
  'SETUP_COMPLETED',
  'SHUTTING_DOWN',
  'INTERNAL_ERROR',
  'STORAGE_UNAVAILABLE',
  'STORAGE_GUARD_MISMATCH',
  'STORAGE_READ_ONLY',
  'STORAGE_LOW_SPACE',
  'STAGING_LOW_SPACE',
  'MEDIA_VERIFY_FAILED',
] as const

export type ErrorCode = (typeof ERROR_CODES)[number]

/* --------------------------------------------------------------------- music */

export interface Artist {
  id: string
  name: string
  provider: string
  source_id: string
  source_url: string
  image_url?: string
  genres?: string[]
  popularity?: number
}

export const RELEASE_TYPES = [
  'album',
  'single',
  'ep',
  'live',
  'compilation',
  'remix',
] as const

export type ReleaseType = (typeof RELEASE_TYPES)[number]

export interface Release {
  id: string
  title: string
  artists: string[]
  album_artist: string
  release_type: ReleaseType
  year: number
  /**
   * The YouTube Music discography listing reports 0 here; the real count is
   * only known once the release itself is read. Treat 0 as "unknown" and show
   * nothing rather than a made-up number.
   */
  track_count: number
  cover_url?: string
  provider: string
  source_id: string
  source_url: string
  release_date?: string
}

export const LYRICS_STATES = [
  'unknown',
  'available_synced',
  'available_plain',
  'instrumental',
  'not_found',
] as const

export type LyricsState = (typeof LYRICS_STATES)[number]

export interface Track {
  id: string
  title: string
  artists: string[]
  album: string
  album_artist: string
  track_number: number
  track_total: number
  disc_number: number
  disc_total: number
  duration_ms: number
  year: number
  isrc?: string
  cover_url?: string
  /** Empty on library tracks: the catalogue listing does not select them. */
  source_provider: string
  source_id: string
  source_url: string
  release_id?: string
  release_type?: ReleaseType
  lyrics_state?: LyricsState
  lyrics_provider?: string
  lyrics_checked_at?: string
  compilation?: boolean
}

/** GET /releases/{id} answers with the release and its track list. */
export interface ReleaseDetail {
  release: Release
  tracks: Track[]
}

/* ------------------------------------------------------------------ resolve */

/** GET /resolve?url= — a pasted address turned into a provider identity. */
export interface ResolvedRef {
  kind: 'artist' | 'release' | 'track'
  provider: string
  id: string
  /** Set for a track whose release the address also named. */
  release_id?: string
}

/* ---------------------------------------------------------------------- jobs */

export const JOB_TYPES = ['artist', 'release', 'track'] as const
export type JobType = (typeof JOB_TYPES)[number]

export const JOB_STATUSES = [
  'queued',
  'resolving_artist',
  'resolving_releases',
  'resolving_tracks',
  'deduplicating',
  'matching',
  'downloading',
  'tagging',
  'finalizing',
  'retry_wait',
  'waiting_for_storage',
  'waiting_for_space',
  'completed',
  'failed',
  'cancelled',
] as const

export type JobStatus = (typeof JOB_STATUSES)[number]

export const TERMINAL_JOB_STATUSES: readonly JobStatus[] = [
  'completed',
  'failed',
  'cancelled',
]

export type ItemStatus =
  | 'pending'
  | 'matching'
  | 'downloading'
  | 'tagging'
  | 'finalizing'
  | 'retry_wait'
  | 'waiting_for_storage'
  | 'waiting_for_space'
  | 'completed'
  | 'failed'
  | 'skipped'
  | 'cancelled'

export interface ReleaseFilter {
  albums: boolean
  singles: boolean
  eps: boolean
  live: boolean
  compilations: boolean
  remixes: boolean
}

export const JOB_PRIORITIES = ['low', 'normal', 'high', 'very_high'] as const
export type JobPriority = (typeof JOB_PRIORITIES)[number]

export interface JobOptions {
  release_filter: ReleaseFilter
  skip_existing: boolean
  release_id?: string
}

export interface Job {
  id: string
  type: JobType
  status: JobStatus
  priority: JobPriority
  paused: boolean
  label: string
  metadata_provider: string
  media_provider: string
  target_id: string
  options: JobOptions
  total: number
  completed: number
  failed: number
  skipped: number
  error_code?: string
  error_message?: string
  created_at: string
  updated_at: string
  started_at?: string
  finished_at?: string
}

export interface JobItem {
  id: string
  job_id: string
  position: number
  status: ItemStatus
  track_id?: string
  track: Track
  label: string
  media_provider?: string
  media_id?: string
  media_url?: string
  match_score: number
  file_id?: string
  attempts: number
  max_attempts?: number
  next_retry_at?: string
  staging_relpath?: string
  staged_size?: number
  staged_sha256?: string
  error_code?: string
  error_message?: string
  created_at: string
  updated_at: string
  started_at?: string
  finished_at?: string
}

export interface JobSummary {
  total: number
  completed: number
  failed: number
  skipped: number
}

/** GET /jobs/{id} answers with the job, its items and the summary. */
export interface JobDetail {
  job: Job
  items: JobItem[]
  summary: JobSummary
}

export interface ActiveWorkerPreview {
  job_id: string
  item_id: string
  artist: string
  release: string
  track: string
  track_number: number
  phase: ItemStatus
  progress_percent: number
  started_at: string
}

export interface NextUpJob {
  job_id: string
  artist: string
  release: string
  open_tracks: number
  total_tracks: number
  cover_url?: string
}

export interface QueueSummary {
  active_items: number
  remaining_items: number
  paused_jobs: number
  retry_wait_items: number
  completed_last_hour: number
  throughput_items_per_hour: number
  eta_seconds: number | null
  eta_confidence: string
  eta_text: string
  total_relevant: number
  completed_relevant: number
  storage_healthy: boolean
  current: ActiveWorkerPreview[]
  next: NextUpJob[]
}

/* -------------------------------------------------------------------- events */

export const JOB_EVENT_TYPES = [
  'job.created',
  'job.status',
  'job.progress',
  'job.completed',
  'job.cancelled',
  'job.failed',
  'job.priority_changed',
  'job.paused',
  'job.resumed',
  'job.retried',
  'job.item',
] as const

export type JobEventType = (typeof JOB_EVENT_TYPES)[number]

/**
 * The subscription sync events. They travel on the same stream as the job
 * events: the backend publishes both through one broker, so a client that
 * already listens for download progress does not open a second connection.
 */
export const SUBSCRIPTION_EVENT_TYPES = [
  'subscription.sync.started',
  'subscription.sync.progress',
  'subscription.sync.completed',
  'subscription.sync.failed',
] as const

export type SubscriptionEventType = (typeof SUBSCRIPTION_EVENT_TYPES)[number]

/** Every message type the event stream can carry. */
export const STREAM_EVENT_TYPES = [
  ...JOB_EVENT_TYPES,
  ...SUBSCRIPTION_EVENT_TYPES,
] as const

export type StreamEventType = JobEventType | SubscriptionEventType

/**
 * One message on GET /events. Go omits zero values, so nearly every field is
 * optional — an event only carries what actually changed.
 */
export interface JobEvent {
  type: StreamEventType
  time: string
  job_id?: string
  /** Set on the subscription sync events; never on a job event. */
  subscription_id?: string
  status?: JobStatus
  label?: string
  priority?: JobPriority
  paused?: boolean
  item_id?: string
  item_status?: ItemStatus
  track?: string
  current?: number
  total?: number
  download_percent?: number
  match_score?: number
  error_code?: string
  error_message?: string
  summary?: JobSummary
}

/* ----------------------------------------------------------------- providers */

export interface ProviderInfo {
  name: string
  kind: 'metadata' | 'media'
  default: boolean
  available: boolean
  detail?: string
}

/* -------------------------------------------------------------------- health */

export interface HealthCheck {
  ok: boolean
  detail?: string
}

export interface Health {
  status: 'ok' | 'degraded' | 'unavailable'
  version: string
  uptime_seconds: number
  checks: Record<string, HealthCheck>
}

/* ------------------------------------------------------------------ settings */

export interface Settings {
  /* Changeable while the server runs. */
  skip_existing: boolean
  embed_cover: boolean
  write_cover_file: boolean
  lyrics_enabled: boolean
  lyrics_write_sidecar: boolean
  lyrics_genius_enabled: boolean
  genius_token_configured: boolean
  match_min_score: number

  max_workers: number
  rate_limit: string
  schedule_enabled: boolean
  schedule_start: string
  schedule_end: string
  schedule_timezone: string
  server_timezone: string

  subscription_default_auto_download: boolean
  subscription_default_priority: JobPriority
  subscription_default_release_filter: ReleaseFilter

  /* Fixed at start-up, reported so a client can explain the behaviour. */
  concurrent_downloads: number
  library_path: string
  allow_transcode: boolean
  match_duration_tolerance_ms: number
  default_metadata_provider: string
  default_media_provider: string
}

/** PUT /settings — an omitted field is left untouched. */
export interface SettingsUpdate {
  skip_existing?: boolean
  embed_cover?: boolean
  write_cover_file?: boolean
  lyrics_enabled?: boolean
  lyrics_write_sidecar?: boolean
  lyrics_genius_enabled?: boolean
  match_min_score?: number

  max_workers?: number
  rate_limit?: string
  schedule_enabled?: boolean
  schedule_start?: string
  schedule_end?: string
  schedule_timezone?: string

  subscription_default_auto_download?: boolean
  subscription_default_priority?: JobPriority
  subscription_default_release_filter?: ReleaseFilter
}

/* -------------------------------------------------------------- subscriptions */

/** The outcome of the last run. There is no "running": see `syncing`. */
export const SYNC_STATUSES = ['pending', 'success', 'partial', 'failed'] as const

export type SyncStatus = (typeof SYNC_STATUSES)[number]

export interface Subscription {
  id: string
  provider: string
  artist_source_id: string
  artist_name: string
  artist_image_url?: string
  enabled: boolean
  auto_download: boolean
  release_filter: ReleaseFilter
  download_priority: JobPriority
  last_sync_at?: string
  next_sync_at: string
  last_sync_status: SyncStatus
  last_error?: string
  created_at: string
  updated_at: string
  /**
   * Whether the backend is running a sync for this subscription right now.
   * It is process state, not a stored column, so it is false again after a
   * restart even if a run was interrupted.
   */
  syncing: boolean
}


/**
 * The report of one run. Counts only — a sync answer must not grow with the
 * size of a discography.
 */
export interface SyncResult {
  subscription_id: string
  artist: string
  started_at: string
  finished_at: string
  status: SyncStatus
  releases_seen: number
  new_releases: number
  /** Distinct recordings after deduplication, which is what was compared. */
  tracks_seen: number
  new_tracks: number
  queued_tracks: number
  skipped_tracks: number
  warnings?: string[]
}

/** GET /subscriptions/{id} — the subscription plus the last report, if any. */
export interface SubscriptionDetail {
  subscription: Subscription
  /** Absent when this backend process has not run a sync for it yet. */
  last_result?: SyncResult
}

export interface SubscribeRequest {
  provider?: string
  artist_source_id: string
  artist_name?: string
  artist_image_url?: string
  auto_download?: boolean
  release_filter?: ReleaseFilter
  download_priority?: JobPriority
}

/** PATCH /subscriptions/{id} — an omitted field is left untouched. */
export interface SubscriptionUpdate {
  enabled?: boolean
  auto_download?: boolean
  release_filter?: ReleaseFilter
  download_priority?: JobPriority
}

/* --------------------------------------------------- subscription export/import */

export interface ExportSubscriptionItem {
  artist_name: string
  provider: string
  artist_source_id: string
  artist_image_url?: string
  enabled: boolean
  auto_download: boolean
  release_filter: ReleaseFilter
  download_priority: JobPriority
}

export interface SubscriptionExport {
  format: string
  version: number
  exported_at: string
  subscriptions: ExportSubscriptionItem[]
}

export type ImportItemStatus =
  | 'new'
  | 'would_update'
  | 'unchanged'
  | 'invalid'
  | 'duplicate'

export interface ImportPreviewItem {
  index: number
  artist_name: string
  provider: string
  artist_source_id: string
  artist_image_url?: string
  enabled: boolean
  auto_download: boolean
  release_filter: ReleaseFilter
  download_priority: JobPriority
  status: ImportItemStatus
  existing_id?: string
  changes?: string[]
  error?: string
}

export interface ImportPreview {
  total: number
  new: number
  existing: number
  would_update: number
  unchanged: number
  invalid: number
  duplicates: number
  warnings?: string[]
  items: ImportPreviewItem[]
}

export interface ImportError {
  index: number
  artist_name?: string
  provider?: string
  artist_source_id?: string
  error: string
}

export interface ImportResult {
  created: number
  updated: number
  unchanged: number
  failed: number
  errors?: ImportError[]
}


/* ---------------------------------------------------------- download requests */

export interface ArtistDownloadRequest {
  provider?: string
  media_provider?: string
  artist_id: string
  release_filter?: ReleaseFilter
  skip_existing?: boolean
}

export interface ReleaseDownloadRequest {
  provider?: string
  media_provider?: string
  release_id: string
  skip_existing?: boolean
}

export interface TrackDownloadRequest {
  provider?: string
  media_provider?: string
  track_id: string
  release_id?: string
  skip_existing?: boolean
}

/* ------------------------------------------------------------------- library */

export type HealthStatus =
  | 'healthy'
  | 'missing_file'
  | 'orphan_file'
  | 'invalid_file'
  | 'metadata_mismatch'
  | 'duplicate_file'

export interface ScanIssue {
  id: string
  status: HealthStatus
  track_id?: string
  track_title?: string
  artist_name?: string
  release_id?: string
  release_title?: string
  path: string
  expected?: string
  actual?: string
  details?: string
}

export interface ScanSummary {
  total_files_scanned: number
  healthy: number
  missing_files: number
  orphan_files: number
  invalid_files: number
  metadata_mismatches: number
  duplicate_files: number
}

export interface ScanResult {
  id: string
  status: 'running' | 'completed' | 'failed'
  started_at: string
  finished_at?: string
  duration_ms: number
  files_scanned: number
  summary: ScanSummary
  issues: ScanIssue[]
  warnings?: string[]
}

export interface LibraryStats {
  total_artists?: number
  total_releases?: number
  total_tracks?: number
  total_files: number
  total_bytes: number
  healthy_count: number
  issue_count: number
  codec_breakdown: Record<string, number>
  lyrics_coverage?: Record<LyricsState, number>
}

export interface MusicFile {
  id: string
  track_id: string
  path: string
  size_bytes: number
  format: string
  codec?: string
  container?: string
  bitrate_kbps?: number
  sample_rate?: number
  channels?: number
  duration_ms?: number
  health?: string
  created_at?: string
  updated_at?: string
}

export interface LibraryTrack extends Track {
  file_path?: string
  file_size_bytes?: number
  codec?: string
  bitrate_kbps?: number
  created_at: string
}

export interface LibraryTrackDetail {
  track: Track
  file?: MusicFile
  release?: Release
  artist?: Artist
  lyrics_path?: string
}

export interface LibraryRelease extends Release {
  track_count_in_library: number
  total_size_bytes: number
  created_at: string
}

export interface LibraryReleaseDetail {
  release: Release
  artist?: Artist
  tracks: LibraryTrack[]
  total_size_bytes: number
}

export interface LibraryArtist extends Artist {
  release_count: number
  track_count: number
  total_size_bytes: number
  created_at: string
}

export interface LibraryArtistDetail {
  artist: Artist
  releases: LibraryRelease[]
  tracks: LibraryTrack[]
  release_count: number
  track_count: number
  total_size_bytes: number
  subscribed: boolean
  subscription_id?: string
}

export interface LibrarySearchResults {
  artists: LibraryArtist[]
  releases: LibraryRelease[]
  tracks: LibraryTrack[]
}

/* ----------------------------------------------------------- lyrics & compat */

export interface TrackLyrics {
  track_id: string
  state: LyricsState
  provider?: string
  path?: string
  content?: string
  synced?: boolean
  checked_at?: string
}

export interface BackfillResult {
  status: 'running' | 'completed' | 'failed' | 'idle'
  started_at?: string
  finished_at?: string
  candidates: number
  processed: number
  written: number
  instrumental: number
  missing: number
  remaining: number
  warnings?: string[]
}

export const COMPAT_KINDS = [
  'artist_folder',
  'multidisc_name',
  'missing_totals',
  'missing_lyrics',
] as const

export type CompatKind = (typeof COMPAT_KINDS)[number]

export interface CompatIssue {
  id: string
  kind: CompatKind
  track_id: string
  title: string
  from: string
  to?: string
  detail?: string
}

export interface CompatReport {
  files_scanned: number
  issues: CompatIssue[]
  warnings?: string[]
}

export interface ReorganizeRequest {
  confirm: boolean
  issue_ids: string[]
}

export interface ReorganizeResult {
  requested: number
  moved: number
  skipped: number
  warnings?: string[]
}

/* ---------------------------------------------------------------------- auth */

export type Role = 'admin' | 'user'

export interface UserSummary {
  id: string
  username: string
  display_name: string
  role: Role
  enabled: boolean
  created_at: string
  updated_at: string
  last_login_at?: string | null
}

export interface AuthStatus {
  setup_required: boolean
  authenticated: boolean
  user?: UserSummary | null
}

export interface SessionSummary {
  id: string
  user_agent: string
  ip_address: string
  created_at: string
  expires_at: string
  last_seen_at: string
  is_current: boolean
}

export interface SetupRequest {
  username: string
  display_name?: string
  password: string
}

export interface LoginRequest {
  username: string
  password: string
}

export interface ChangePasswordRequest {
  current_password: string
  new_password: string
}

export interface UpdateProfileRequest {
  display_name: string
}

export interface CreateUserRequest {
  username: string
  display_name?: string
  password: string
  role: Role
}

export interface UpdateUserStatusRequest {
  display_name?: string
  role?: Role
  enabled?: boolean
}

/* ------------------------------------------------------------------- storage */

export type StorageHealthStatus =
  | 'healthy'
  | 'degraded'
  | 'unavailable'
  | 'guard_missing'
  | 'guard_mismatch'
  | 'read_only'
  | 'low_space'
  | 'unknown'

export type GuardStatus = 'disabled' | 'verified' | 'missing' | 'mismatch' | 'invalid'

export interface StorageLibraryStatus {
  path: string
  guard_configured: boolean
  guard_status: GuardStatus
  status: StorageHealthStatus
  status_detail?: string
  fs_type: string
  total_bytes: number
  free_bytes: number
  used_bytes: number
  free_percent: number
  min_free_bytes: number
  is_network_fs: boolean
  last_checked_at: string
}

export interface StorageStagingStatus {
  path: string
  total_bytes: number
  free_bytes: number
  used_bytes: number
  min_free_bytes: number
  max_bytes: number
  current_staged_bytes: number
  active_items: number
  active_partials: number
}

export interface StorageQueueStatus {
  paused: boolean
  waiting_storage_items: number
  waiting_space_items: number
  retry_wait_items: number
}

export interface StorageStatusResponse {
  library: StorageLibraryStatus
  staging: StorageStagingStatus
  queue: StorageQueueStatus
}

/* -------------------------------------------------------- library audit & repair */

export type AuditMode = 'quick' | 'deep'
export type AuditRunStatus = 'running' | 'completed' | 'failed' | 'cancelled'
export type FindingSeverity = 'info' | 'warning' | 'error'

export type FindingCode =
  | 'FILE_MISSING'
  | 'FILE_UNTRACKED'
  | 'LEGACY_DUPLICATE'
  | 'FILE_DUPLICATE'
  | 'AUDIO_INVALID'
  | 'TAG_MISMATCH'
  | 'PATH_MISMATCH'
  | 'COVER_MISSING'
  | 'COVER_INVALID'
  | 'LYRICS_MISSING'
  | 'LYRICS_ORPHANED'
  | 'RELEASE_INCOMPLETE'

export type EvidenceLevel =
  | 'EXACT_CONTENT'
  | 'EXACT_CATALOG_ID'
  | 'STRONG_METADATA'
  | 'WEAK_METADATA'
  | 'UNKNOWN'

export type RepairAction =
  | 'MOVE_CANONICAL'
  | 'RESTORE_TAGS'
  | 'ADOPT_FILE'
  | 'QUARANTINE_FILE'

export interface FindingEvidence {
  level?: EvidenceLevel
  expected_path?: string
  actual_path?: string
  canonical_path?: string
  size_bytes?: number
  duration_ms?: number
  sha256?: string
  mismatched_tags?: string[]
  details?: string
}

export interface AuditFinding {
  id: string
  run_id: string
  finding_code: FindingCode
  severity: FindingSeverity
  relative_path: string
  artist_id?: string
  release_id?: string
  track_id?: string
  artist_name?: string
  release_title?: string
  track_title?: string
  suggested_action?: RepairAction
  evidence: FindingEvidence
  created_at: string
}

export interface AuditRun {
  id: string
  mode: AuditMode
  status: AuditRunStatus
  started_at: string
  finished_at?: string
  scanned: number
  total: number
  findings_count: number
  error_summary?: string
  created_by?: string
  created_at: string
}

export interface RepairPreview {
  finding_id: string
  finding_code: FindingCode
  action: RepairAction
  source_path: string
  destination_path?: string
  allowed: boolean
  message?: string
  db_changes: string[]
  file_changes: string[]
  warnings?: string[]
}

export interface RepairItemAction {
  finding_id: string
  action: RepairAction
}

export interface RepairApplyRequest {
  confirm: boolean
  actions: RepairItemAction[]
}

export interface RepairApplyResult {
  requested: number
  applied: number
  quarantined: number
  failed: number
  warnings?: string[]
}

export type UpdateState =
  | 'up_to_date'
  | 'update_available'
  | 'no_public_release'
  | 'disabled'
  | 'unavailable'
  | 'invalid_release'
  | 'development_version'

export interface UpdateStatus {
  current_version: string
  latest_version?: string
  state: UpdateState
  release_name?: string
  published_at?: string
  release_url?: string
  release_notes?: string
  checked_at: string
  cached: boolean
}

/* ----------------------------------------------------------- media sessions */

export type MediaSessionHealthStatus =
  | 'unknown'
  | 'healthy'
  | 'cooldown'
  | 'rate_limited'
  | 'bot_challenge'
  | 'auth_failed'

export interface MediaSession {
  id: string
  name: string
  provider_family: string
  enabled: boolean
  health_status: MediaSessionHealthStatus
  has_credentials: boolean
  in_use: boolean
  cooldown_until?: string | null
  last_used_at?: string | null
  last_success_at?: string | null
  last_failure_at?: string | null
  last_failure_reason?: string | null
  created_at: string
  updated_at: string
}

export interface MediaSessionProbeResult {
  status: MediaSessionHealthStatus
  tested_at: string
  metadata_ok: boolean
  usable_audio_formats: boolean
  failure_category?: string | null
  cooldown_until?: string | null
}

export interface CreateMediaSessionPayload {
  name: string
  provider_family?: string
  enabled?: boolean
}

export interface UpdateMediaSessionPayload {
  name?: string
  enabled?: boolean
}
