// 由 internal/tools/tsgen 產生，不要手改

export interface Account {
  name?: string;
  engine?: string;
  config_dir?: string;
  env_file?: string;
  remote_access: boolean;
  usage_warmup: boolean;
  default: boolean;
}

export interface AccountUsage {
  account?: string;
  used_ratio: number;
  resets_at?: string;
  windows?: UsageWindow[];
  measured_at?: string;
  note?: string;
}

export type ActionClass = "read" | "write" | "commit" | "push" | "delete" | "send_external" | "spend" | "settings";

export type ActionVerdict = "allow" | "deny" | "ask";

export interface Actor {
  name?: string;
}

export interface AgentCard {
  id?: string;
  name?: string;
  aliases?: string[];
  duty?: string;
  body?: string;
}

export interface AgentCardFile {
  agent?: string;
  path?: string;
  action?: string;
  summary?: string;
}

export interface AgentDecl {
  id?: string;
  duty?: string;
  color?: string;
  color_dark?: string;
  optional?: boolean;
}

export interface AgentKnowledgeBlock {
  id?: string;
  title?: string;
  summary?: string;
  default: boolean;
  enabled: boolean;
}

export interface AgentLinkAccount {
  account?: string;
  note?: string;
  files?: AgentCardFile[];
}

export interface AgentPermission {
  agent?: string;
  level?: PermissionLevel;
  source?: string;
}

export interface AgentPref {
  agent?: string;
  categories?: Record<string, CategoryPref>;
}

export interface AgentSetting {
  id?: string;
  name?: string;
  duty?: string;
  persona?: string;
  account?: string;
  model?: string;
  effort?: string;
  enabled: boolean;
}

export interface AgentsAddRuleRequest {
  id?: string;
  rule?: string;
  section?: string;
  by?: Actor;
}

export interface AgentsAddRuleResponse {
  addendum?: string;
}

export interface AgentsAppendJournalRequest {
  id?: string;
  line?: string;
  by?: Actor;
}

export interface AgentsAppendJournalResponse {
}

export interface AgentsArchiveRequest {
  id?: string;
  by?: Actor;
}

export interface AgentsArchiveResponse {
  archived?: string[];
  restore?: string;
}

export interface AgentsAssignRequest {
  id?: string;
  persona?: string;
  by?: Actor;
}

export interface AgentsAssignResponse {
  agent?: SdkRecord;
}

export interface AgentsCreateRequest {
  id?: string;
  persona?: string;
  display_name?: string;
  duty?: string;
  knowledge?: string[];
  by?: Actor;
}

export interface AgentsCreateResponse {
  agent?: SdkRecord;
}

export interface AgentsCustomizationsRequest {
  id?: string;
}

export interface AgentsCustomizationsResponse {
  duties?: string;
  addendum?: string;
}

export interface AgentsKnowledgeRequest {
  agent?: string;
}

export interface AgentsKnowledgeResponse {
  agent?: string;
  blocks: AgentKnowledgeBlock[];
}

export interface AgentsLinkRequest {
  apply?: boolean;
  by?: Actor;
}

export interface AgentsLinkResponse {
  applied: boolean;
  accounts?: AgentLinkAccount[];
}

export interface AgentsRenameRequest {
  id?: string;
  new_id?: string;
  display_name?: string;
  by?: Actor;
}

export interface AgentsRenameResponse {
  agent?: SdkRecord;
  moved?: string[];
  mentions?: string[];
}

export interface AgentsSetAccountRequest {
  id?: string;
  account?: string;
  model?: string;
  effort?: string;
  by?: Actor;
}

export interface AgentsSetAccountResponse {
  agent?: SdkRecord;
}

export interface AgentsSetFieldRequest {
  id?: string;
  field?: string;
  value?: string;
  by?: Actor;
}

export interface AgentsSetFieldResponse {
  agent?: SdkRecord;
}

export interface AgentsSetKnowledgeRequest {
  agent?: string;
  enabled: string[];
  by?: Actor;
}

export interface AgentsSwapRequest {
  a?: string;
  b?: string;
  by?: Actor;
}

export interface AgentsSwapResponse {
  agents?: SdkRecord[];
}

export interface AgentsWriteMemoryRequest {
  id?: string;
  body?: string;
  by?: Actor;
}

export interface AgentsWriteMemoryResponse {
}

export interface AgentsWriteNoteRequest {
  id?: string;
  name?: string;
  body?: string;
  revision?: string;
  by?: Actor;
}

export interface AgentsWriteNoteResponse {
  name?: string;
  created: boolean;
  revision?: string;
}

export interface Answer {
  text?: string;
  at?: string;
  via?: AnswerVia;
  delivered_at?: string;
}

export type AnswerVia = "button" | "chat" | "api" | "auto";

export type ApprovalKind = "permission" | "question";

export interface BackupCreateRequest {
  by?: Actor;
}

export interface BackupCreateResponse {
  file: HomeBackupFile;
  sections?: HomeBackupSection[];
  skipped?: string[];
}

export interface BackupFile {
  path?: string;
  group?: string;
  size: number;
  mod_time?: string;
  prune?: boolean;
}

export interface BackupGetStatusRequest {
  local?: boolean;
}

export interface BackupGetStatusResponse {
  files?: HomeBackupFile[];
  last_at: string;
  last_kind?: string;
  auto_error?: string;
  auto_error_at: string;
  restoring: boolean;
  last_restore?: HomeRestoreResult;
  keep_count: number;
  keep_days: number;
  daily_keep: number;
  free_bytes: number;
  local: boolean;
}

export interface BackupPreviewRequest {
  upload?: string;
  kind?: string;
  name?: string;
  path?: string;
}

export interface BackupPreviewResponse {
  created_at: string;
  home_name?: string;
  owner?: string;
  machine?: string;
  app_version?: string;
  kind?: string;
  size: number;
  files: number;
  bytes: number;
  sections?: HomeBackupSection[];
  other_machine: boolean;
  problem?: string;
  installed?: string[];
  skipped?: string[];
  lost_links?: string[];
}

export interface BackupRestoreRequest {
  upload?: string;
  kind?: string;
  name?: string;
  path?: string;
  by?: Actor;
}

export interface BackupRestoreResponse {
  restarting: boolean;
  safety?: string;
}

export interface BackupSweepItem {
  path?: string;
  dir?: boolean;
  size: number;
  date?: string;
  expires?: string;
  mod_time: string;
  state?: string;
  checked?: string;
  used_by?: string;
  problem?: string;
  suggest?: string;
  reason?: string;
}

export interface BackupSweepReport {
  at: string;
  took_ms: number;
  by?: string;
  roots?: string[];
  checks?: string[];
  expired?: BackupSweepItem[];
  misnamed?: BackupSweepItem[];
  current: number;
  current_bytes: number;
  skipped?: BackupSweepItem[];
  pruned: number;
  pruned_bytes: number;
  notes?: string[];
  errors?: string[];
  file?: string;
}

export interface BootPrompt {
  id?: string;
  session?: string;
  title?: string;
  choices?: string[];
}

export interface BusGetRequest {
  id?: string;
}

export interface BusGetResponse {
  envelope?: Envelope;
}

export interface BusListRequest {
  to?: string;
  states?: Lifecycle[];
  page?: PageRequest;
}

export interface BusListResponse {
  envelopes?: Envelope[];
  page?: PageResponse;
}

export interface BusPublishRequest {
  topic?: string;
  payload?: Record<string, unknown>;
  by?: Actor;
}

export interface BusPublishResponse {
}

export interface BusSendRequest {
  from?: string;
  to?: string;
  kind?: EnvelopeKind;
  body?: string;
  reply_to?: string;
  notice?: string;
  idempotency_key?: string;
}

export interface BusSendResponse {
  envelope?: Envelope;
}

export interface BusSubscribeRequest {
  topics?: string[];
}

export interface BusSubscribeResponse {
  topic?: string;
  payload?: Record<string, unknown>;
  at?: string;
}

export interface BusWatchRequest {
  to?: string;
  id?: string;
}

export interface BusWatchResponse {
  envelope?: Envelope;
  step?: LifecycleStep;
}

export interface Capabilities {
  read?: string[];
  write?: string[];
  events?: string[];
}

export type Category = "approve" | "done" | "due" | "digest" | "opening";

export interface CategoryPref {
  mode?: DeliveryMode;
  digest_hours: number;
}

export interface ChangeEntry {
  at?: string;
  by?: string;
  kind?: string;
  id?: string;
  op?: string;
}

export type ChangeType = "created" | "updated" | "archived";

export type Channel = "board" | "app" | "line";

export type ChatDisplay = "default" | "engineer";

export type ChatKind = "user" | "assistant" | "tool_call" | "tool_result" | "interrupt" | "notice";

export interface ChatMessage {
  uuid?: string;
  ts?: string;
  kind?: ChatKind;
  text?: string;
  tool?: string;
  tool_label?: string;
  call_id?: string;
  is_error: boolean;
  from_session?: string;
  draft?: boolean;
}

export type ClaimState = "asked" | "later" | "change" | "done" | "failed";

export interface Client {
}

export type Cycle = "once" | "daily" | "weekly" | "biweekly" | "monthly";

export interface DataFieldDecl {
  name?: string;
  type?: string;
  required: boolean;
  enum?: string[];
  description?: string;
}

export interface DataKindDecl {
  kind?: string;
  title?: string;
  ids?: string;
  prefix?: string;
  date_field?: string;
  fields?: DataFieldDecl[];
  export?: string;
  persona_field?: string;
  folder_field?: string;
  export_omit?: string[];
  pack_only?: Record<string, string[]>;
  pack_only_note?: string;
  pack_except?: Record<string, string[]>;
  pack_except_note?: string;
  persona_id?: string;
  persona_prefixed?: string[];
  persona_source?: string;
  file_field?: string;
  export_only?: Record<string, string[]>;
  pack_parent?: DataParentDecl;
}

export interface DataParentDecl {
  kind: string;
  field: string;
}

export interface Decision {
  allow: boolean;
  message?: string;
  answers?: Record<string, string>;
}

export interface Delivery {
  channel?: Channel;
  at?: string;
  result?: string;
}

export type DeliveryMode = "instant" | "digest" | "board" | "off";

export interface DeniedAction {
  at?: string;
  session?: string;
  agent?: string;
  from?: string;
  origin?: string;
  class?: ActionClass;
  tool?: string;
  verdict?: ActionVerdict;
  reason?: string;
}

export type Difficulty = "easy" | "normal" | "hard";

export interface DirectMessage {
  to?: string;
  text?: string;
  ok?: boolean;
}

export interface DoctorCheck {
  id?: string;
  group?: DoctorGroup;
  ok: boolean;
  severity?: DoctorSeverity;
  detail?: string;
  fix?: string;
}

export type DoctorGroup = "config" | "sessions" | "packages" | "permissions" | "schedule" | "data" | "push";

export type DoctorSeverity = "ok" | "warn" | "error";

export interface EngineAnswerApprovalRequest {
  session?: string;
  pending?: Pending;
  decision?: Decision;
  by?: Actor;
}

export interface EngineAnswerApprovalResponse {
  answered: boolean;
  reason?: string;
}

export interface EngineAnswerBootPromptRequest {
  id?: string;
  choice?: string;
  by?: Actor;
}

export interface EngineAnswerBootPromptResponse {
}

export interface EngineAskRequest {
  agent?: string;
  cwd?: string;
  account?: string;
  engine_config?: Record<string, unknown>;
  model?: string;
  effort?: string;
  instructions?: string;
  message?: string;
  timeout_seconds: number;
}

export interface EngineAskResponse {
  reply?: string;
  model?: string;
}

export interface EngineCapabilitiesRequest {
}

export interface EngineCapabilitiesResponse {
  engine?: string;
  version?: string;
  features?: Record<string, boolean>;
}

export interface EngineCommandRequest {
  session?: string;
  command?: SessionCommand;
  arg?: string;
  by?: Actor;
}

export interface EngineCommandResponse {
  done: boolean;
  reason?: string;
  engine_session_id?: string;
  model?: string;
  model_label?: string;
  effort?: string;
}

export interface EngineDeliverRequest {
  text?: string;
  session?: string;
  envelope_id?: string;
  force?: boolean;
  resend?: boolean;
}

export interface EngineDeliverResponse {
  delivered: boolean;
  reason?: string;
}

export type EngineErrorKind = "overloaded" | "auth" | "billing" | "rejected" | "unknown";

export interface EngineEvent {
  input?: string;
  source?: InputSource;
  from?: string;
  interrupted?: boolean;
  direct_message?: DirectMessage;
  session?: string;
  type?: EngineEventType;
  at?: string;
  activity?: string;
  background_tasks: number;
  last_message?: string;
  reason?: string;
  until?: string;
  error?: EngineErrorKind;
  model?: string;
  model_label?: string;
  effort?: string;
  engine_session_id?: string;
}

export type EngineEventType = "session_started" | "session_ended" | "turn_started" | "turn_ended" | "activity_started" | "activity_ended" | "idle_hint" | "rate_limited" | "turn_failed";

export interface EngineHostCheckActionRequest {
  session?: string;
  class?: ActionClass;
  tool?: string;
  summary?: string;
}

export interface EngineHostCheckActionResponse {
  verdict?: ActionVerdict;
  reason?: string;
}

export interface EngineHostGetContextRequest {
  input?: string;
  session?: string;
}

export interface EngineHostGetContextResponse {
  text?: string;
}

export interface EngineHostReportEventRequest {
  event?: EngineEvent;
}

export interface EngineHostReportEventResponse {
}

export interface EngineHostRequestApprovalRequest {
  session?: string;
  kind?: ApprovalKind;
  tool?: string;
  summary?: string;
  questions?: Question[];
  max_wait_seconds: number;
}

export interface EngineHostRequestApprovalResponse {
  decided: boolean;
  decision?: Decision;
}

export interface EngineListBootPromptsRequest {
  session?: string;
}

export interface EngineListBootPromptsResponse {
  prompts?: BootPrompt[];
}

export interface EnginePreflightRequest {
  account?: string;
  engine_config?: Record<string, unknown>;
}

export interface EnginePreflightResponse {
  checks?: PreflightCheck[];
}

export interface EngineSetPermissionRequest {
  policy?: PermissionPolicy;
  account?: string;
  dry_run?: boolean;
}

export interface EngineSetPermissionResponse {
  artifacts?: string[];
  generated_at?: string;
  drift?: string[];
}

export interface EngineStartRequest {
  name?: string;
  agent?: string;
  cwd?: string;
  account?: string;
  engine_config?: Record<string, unknown>;
  model?: string;
  permission_level?: PermissionLevel;
  rules?: PolicyRule[];
  effort?: string;
  remote_access: boolean;
  remote_access_name?: string;
  instructions?: string;
  resume_session_id?: string;
  first_message?: string;
  host_endpoint?: string;
}

export interface EngineStartResponse {
  engine_session_id?: string;
  remote_access_url?: string;
  attach?: string;
  model?: string;
  model_label?: string;
  effort?: string;
}

export interface EngineStopRequest {
  name?: string;
}

export interface EngineStopResponse {
}

export interface EngineTranscriptRequest {
  session?: string;
  since_uuid?: string;
  follow: boolean;
}

export interface EngineTranscriptResponse {
  message?: ChatMessage;
}

export interface EngineUsageRequest {
  account?: string;
  engine_config?: Record<string, unknown>;
  warmup?: boolean;
}

export interface EngineUsageResponse {
  account?: string;
  used_ratio: number;
  resets_at?: string;
  windows?: UsageWindow[];
  measured_at?: string;
  warmed?: boolean;
}

export interface EngineWriteAgentCardsRequest {
  account?: string;
  engine_config?: Record<string, unknown>;
  user_name?: string;
  cards?: AgentCard[];
  dry_run?: boolean;
}

export interface EngineWriteAgentCardsResponse {
  files?: AgentCardFile[];
  note?: string;
}

export interface Envelope {
  route?: EnvelopeRoute;
  reason?: string;
  last_message?: string;
  id?: string;
  from?: string;
  to?: string;
  kind?: EnvelopeKind;
  body?: string;
  lifecycle?: Lifecycle;
  history?: LifecycleStep[];
  attempts: number;
  reply_to?: string;
  notice?: string;
  created_at?: string;
  idempotency_key?: string;
}

export type EnvelopeKind = "echo" | "chat" | "say" | "delegate" | "report" | "answer" | "routine";

export type EnvelopeRoute = "core" | "direct";

export interface Error {
  code: ErrorCode;
  message: string;
  field?: string;
  details?: Record<string, string>;
}

export type ErrorCode = "invalid" | "not_found" | "conflict" | "forbidden" | "unavailable" | "internal";

export type ExtensionKind = "setting" | "prompt_append" | "page_slot";

export interface ExtensionPoint {
  id?: string;
  kind?: ExtensionKind;
  description?: string;
  default_value?: string;
  owner_only?: boolean;
  package?: string;
  agent?: string;
}

export type Follow = "none" | "remind" | "confirm" | "check" | "discuss";

export interface ForkInfo {
  of?: string;
  base_version?: string;
  forked_at?: string;
}

export interface Goal {
  id?: string;
  title?: string;
  text?: string;
  criteria?: string;
  agent?: string;
  state?: GoalState;
  notice?: string;
  confirmed_at?: string;
  confirmed_by?: string;
  ended_at?: string;
  ended_by?: string;
  summary?: string;
  planbook?: string;
  planbook_log?: PlanbookEntry[];
  by?: string;
  created_at?: string;
  updated_at?: string;
}

export type GoalState = "draft" | "active" | "done" | "withdrawn";

export interface GoalsCreateRequest {
  title?: string;
  text?: string;
  criteria?: string;
  agent?: string;
  by?: Actor;
}

export interface GoalsCreateResponse {
  goal?: Goal;
}

export interface GoalsDoneRequest {
  id?: string;
  summary?: string;
  by?: Actor;
}

export interface GoalsDoneResponse {
  goal?: Goal;
}

export interface GoalsGetRequest {
  id?: string;
}

export interface GoalsGetResponse {
  goal?: Goal;
}

export interface GoalsListRequest {
  states?: GoalState[];
}

export interface GoalsListResponse {
  goals?: Goal[];
}

export interface GoalsProposeRequest {
  title?: string;
  text?: string;
  criteria?: string;
  agent?: string;
  by?: Actor;
}

export interface GoalsProposeResponse {
  goal?: Goal;
}

export interface GoalsUpdatePlanbookRequest {
  id?: string;
  body?: string;
  note?: string;
  by?: Actor;
}

export interface GoalsUpdatePlanbookResponse {
  goal?: Goal;
}

export interface GoalsWithdrawRequest {
  id?: string;
  by?: Actor;
}

export interface GoalsWithdrawResponse {
  goal?: Goal;
}

export interface HealthItem {
  name?: string;
  ok: boolean;
  detail?: string;
}

export interface HomeBackupFile {
  name?: string;
  kind?: string;
  size: number;
  created_at: string;
  download?: string;
}

export interface HomeBackupSection {
  id?: string;
  label?: string;
  files: number;
  bytes: number;
}

export type HomeEnv = "prod" | "dev";

export type HomeMode = "simple" | "full";

export interface HomeRestoreResult {
  ok: boolean;
  at: string;
  backup_created_at: string;
  backup_home?: string;
  message?: string;
  safety?: string;
  todo?: string[];
  from_other_machine?: boolean;
}

export type InputSource = "typed" | "peer" | "system";

export interface Job {
  id?: string;
  agent?: string;
  name?: string;
  cron?: string[];
  windows?: string[];
  weekend_windows?: string[];
  per_day?: number;
  idle_min?: number;
  enabled: boolean;
  catchup?: string;
  action?: string;
  silent?: boolean;
  source?: string;
  body?: string;
  archived: boolean;
  by?: string;
  created_at?: string;
  updated_at?: string;
  next_run?: string;
  last_run?: string;
  last_result?: JobResult;
  last_reason?: string;
  runs_today?: number;
}

export type JobResult = "ok" | "running" | "fired" | "failed" | "skipped";

export type Lifecycle = "unanswered" | "interrupted" | "queued" | "notified" | "received" | "working" | "replied" | "blocked" | "failed";

export interface LifecycleStep {
  state?: Lifecycle;
  at?: string;
  detail?: string;
}

export interface Menu {
  tree?: MenuEntry[];
  pool?: MenuItem[];
  tucked?: MenuItem[];
  renderer?: string;
  renderer_bundle_url?: string;
  renderer_error?: string;
}

export interface MenuEntry {
  id?: string;
  title?: string;
  icon?: string;
  route?: string;
  badge: number;
  children?: MenuEntry[];
  section?: string;
  section_title?: string;
  tier?: Tier;
}

export interface MenuItem {
  id?: string;
  title?: string;
  icon?: string;
  route?: string;
  badge: number;
  section?: string;
  section_title?: string;
  tier?: Tier;
}

export interface MenuItemDecl {
  id?: string;
  title?: string;
  icon?: string;
  group_hint?: string;
  order: number;
  front?: boolean;
  show_when?: string;
  page?: string;
  section?: string;
  section_title?: string;
}

export interface MenuPrefEntry {
  item?: string;
  group?: string;
  title?: string;
  icon?: string;
  children?: string[];
}

export interface MenuPrefs {
  tree?: MenuPrefEntry[];
  renderer?: string;
  hidden?: string[];
}

export type MessageWait = "answer" | "waking" | "busy" | "interject" | "limited" | "retrying";

export interface NotesAddCategoryRequest {
  name?: string;
  summary?: string;
  body?: string;
  by?: Actor;
}

export interface NotesAddCategoryResponse {
  category?: SdkRecord;
}

export interface NotesAddRequest {
  name?: string;
  line?: string;
  category?: string;
  summary?: string;
  aliases?: string[];
  follow?: string[];
  links?: string[];
  by?: Actor;
}

export interface NotesAddResponse {
  topic?: SdkRecord;
  created: boolean;
  line?: string;
}

export interface NotesArchiveRequest {
  name?: string;
  into?: string;
  by?: Actor;
}

export interface NotesArchiveResponse {
  name?: string;
  path?: string;
  into?: string;
  message?: string;
}

export interface NotesQuipUsedRequest {
  text?: string;
  by?: Actor;
}

export interface NotesQuipUsedResponse {
  quip?: string;
  until: string;
  message?: string;
}

export interface NotesQuipsRequest {
  by?: Actor;
}

export interface NotesQuipsResponse {
  text?: string;
  total: number;
  shown: number;
  resting?: string[];
}

export interface NotesSetRequest {
  name?: string;
  body?: string;
  revision?: string;
  keep_body?: boolean;
  summary?: string;
  aliases?: string[];
  follow?: string[];
  links?: string[];
  clear?: string[];
  by?: Actor;
}

export interface NotesSetResponse {
  topic?: SdkRecord;
}

export interface Notification {
  source?: string;
  id?: string;
  category?: Category;
  title?: string;
  context?: string;
  todo?: string;
  options?: string[];
  url?: string;
  image?: string;
  agent?: string;
  account?: string;
  room?: string;
  task?: string;
  reminder?: string;
  urgent: boolean;
  inline?: boolean;
  claim?: string;
  speaker?: string;
  link?: NotificationLink;
  typed?: boolean;
  lane?: string;
  badge: boolean;
  delivers_answer: boolean;
  delivery?: Delivery[];
  state?: NotificationState;
  answer?: Answer;
  body?: string;
  home?: string;
  by?: string;
  created_at?: string;
}

export interface NotificationLink {
  label?: string;
  url?: string;
}

export type NotificationState = "unread" | "read" | "answered" | "expired" | "failed";

export interface NotifyAnswerRequest {
  id?: string;
  text?: string;
  via?: AnswerVia;
  by?: Actor;
}

export interface NotifyAnswerResponse {
  notification?: Notification;
  delivered: boolean;
}

export interface NotifyArchiveRequest {
  ids?: string[];
  by?: Actor;
}

export interface NotifyArchiveResponse {
  archived?: string[];
  skipped?: string[];
}

export interface NotifyGetPrefsRequest {
}

export interface NotifyGetPrefsResponse {
  prefs?: NotifyPrefs;
}

export interface NotifyGetRequest {
  id?: string;
}

export interface NotifyGetResponse {
  notification?: Notification;
}

export interface NotifyListRequest {
  states?: NotificationState[];
  agent?: string;
  include_archived: boolean;
  page?: PageRequest;
}

export interface NotifyListResponse {
  notifications?: Notification[];
  unread: number;
  page?: PageResponse;
}

export interface NotifyMarkReadRequest {
  ids?: string[];
}

export interface NotifyMarkReadResponse {
}

export interface NotifyNudgeRequest {
  agent?: string;
  session?: string;
  task?: string;
  title?: string;
  context?: string;
  todo?: string;
  url?: string;
  key?: string;
  resolve?: boolean;
  by?: Actor;
}

export interface NotifyNudgeResponse {
  nudge?: Nudge;
}

export interface NotifyPrefs {
  preset?: string;
  quiet_hours?: string;
  channel_app: boolean;
  channel_line: boolean;
  reask_after_minutes: number;
  reask_max_asks: number;
  categories?: Record<string, CategoryPref>;
  agents?: AgentPref[];
  tasks?: TaskFollowPrefs;
  presets?: Record<string, Record<string, CategoryPref>>;
}

export interface NotifyPresenceRequest {
  client?: string;
  path?: string;
  thread?: string;
  away: boolean;
  by?: Actor;
}

export interface NotifyPresenceResponse {
  nudges?: Nudge[];
}

export interface NotifyPushKeyRequest {
}

export interface NotifyPushKeyResponse {
  public_key?: string;
  subscriptions: number;
}

export interface NotifyPushTestRequest {
  endpoint?: string;
  by?: Actor;
}

export interface NotifyPushTestResponse {
}

export interface NotifyResolveNudgeRequest {
  id?: string;
  by?: Actor;
}

export interface NotifyResolveNudgeResponse {
}

export interface NotifySendRequest {
  agent?: string;
  category?: Category;
  title?: string;
  context?: string;
  todo?: string;
  options?: string[];
  url?: string;
  image?: string;
  room?: string;
  task?: string;
  reminder?: string;
  urgent: boolean;
  inline?: boolean;
  lane?: string;
  link?: NotificationLink;
  by?: Actor;
}

export interface NotifySendResponse {
  notification?: Notification;
}

export interface NotifySetPrefsRequest {
  prefs?: NotifyPrefs;
  expand?: boolean;
  by?: Actor;
}

export interface NotifySetPrefsResponse {
  prefs?: NotifyPrefs;
}

export interface NotifySubscribePushRequest {
  endpoint?: string;
  p256dh?: string;
  auth?: string;
  by?: Actor;
}

export interface NotifySubscribePushResponse {
  subscriptions: number;
}

export interface NotifyUnsubscribePushRequest {
  endpoint?: string;
  by?: Actor;
}

export interface NotifyUnsubscribePushResponse {
  subscriptions: number;
}

export interface NotifyWatchRequest {
}

export interface NotifyWatchResponse {
  notification?: Notification;
  unread: number;
  nudge?: Nudge;
}

export interface Nudge {
  id?: string;
  agent?: string;
  session?: string;
  task?: string;
  title?: string;
  context?: string;
  todo?: string;
  url?: string;
  key?: string;
  surface?: NudgeSurface;
  resolved: boolean;
  created_at?: string;
}

export type NudgeSurface = "inline" | "toast" | "push" | "none";

export interface Package {
  id?: string;
  name?: string;
  version?: string;
  description?: string;
  kind?: PackageKind;
  tier?: Tier;
  api?: string;
  state?: PackageState;
  enabled: boolean;
  optional?: boolean;
  dev_only?: boolean;
  avatars?: string;
  requires?: Requirement[];
  capabilities?: Capabilities;
  menu?: MenuItemDecl[];
  pages?: PageDecl[];
  data?: DataKindDecl[];
  agents?: AgentDecl[];
  extensions?: ExtensionPoint[];
  fork?: ForkInfo;
  source?: PackageSource;
  error?: string;
  remedy?: PackageRemedy;
}

export interface PackageClaimNotifyRequest {
  id?: string;
  agent?: string;
  persona?: string;
  by?: Actor;
}

export interface PackageClaimNotifyResponse {
  package?: string;
  agent?: string;
  persona?: string;
  persona_name?: string;
  reason?: string;
  notice?: string;
  state?: ClaimState;
  sent: boolean;
  detail?: string;
}

export interface PackageExportFinding {
  path?: string;
  line: number;
  kind?: string;
  what?: string;
}

export interface PackageExportRequest {
  id?: string;
  out?: string;
  git: boolean;
  allow_findings: boolean;
  by?: Actor;
}

export interface PackageExportResponse {
  path?: string;
  format?: string;
  files?: string[];
  skipped?: string[];
  findings?: PackageExportFinding[];
  blocked: boolean;
}

export interface PackageForkRequest {
  id?: string;
  by?: Actor;
}

export interface PackageForkResponse {
  package?: Package;
}

export interface PackageGetRequest {
  id?: string;
}

export interface PackageGetResponse {
  package?: Package;
}

export interface PackageGoStatusRequest {
}

export interface PackageGoStatusResponse {
  usable: boolean;
  source?: string;
  path?: string;
  version?: string;
  need?: string;
  installable: boolean;
  offer?: string;
  size_mb?: number;
  detail?: string;
  ask?: string;
}

export interface PackageHelloRequest {
  id?: string;
  api?: string;
  version?: string;
}

export interface PackageHelloResponse {
  token?: string;
  data_dir?: string;
  granted?: string[];
}

export interface PackageHost {
  id?: string;
  version?: string;
  dir?: string;
  data_dir?: string;
  token?: string;
  granted?: string[];
}

export interface PackageInstallGoRequest {
  confirmed: boolean;
  by?: Actor;
}

export interface PackageInstallGoResponse {
  command?: string;
  packages?: Package[];
  version?: string;
}

export interface PackageInstallRequest {
  source?: string;
  tier?: Tier;
  by?: Actor;
  ref?: string;
  confirmed?: boolean;
}

export interface PackageInstallResponse {
  package?: Package;
}

export type PackageKind = "data" | "program";

export interface PackageListExtensionsRequest {
  package?: string;
}

export interface PackageListExtensionsResponse {
  extensions?: ExtensionPoint[];
  values?: Record<string, string>;
  warnings?: string[];
}

export interface PackageListRequest {
  tier?: Tier;
}

export interface PackageListResponse {
  packages?: Package[];
  warnings?: string[];
}

export interface PackageListUpdatesRequest {
  id?: string;
}

export interface PackageListUpdatesResponse {
  base_version?: string;
  latest_version?: string;
  items?: UpdateItem[];
  report_path?: string;
}

export interface PackageNewRequest {
  id?: string;
  name?: string;
  by?: Actor;
}

export interface PackageNewResponse {
  package?: Package;
  dir?: string;
  files?: string[];
}

export interface PackageOutdated {
  id?: string;
  name?: string;
  current?: string;
  latest?: string;
  url?: string;
}

export interface PackageOutdatedRequest {
}

export interface PackageOutdatedResponse {
  packages?: PackageOutdated[];
  failed?: string[];
}

export interface PackagePreviewPersona {
  id?: string;
  slug?: string;
  name?: string;
  avatar: boolean;
}

export interface PackagePreviewRequest {
  source?: string;
  ref?: string;
}

export interface PackagePreviewResponse {
  id?: string;
  name?: string;
  version?: string;
  description?: string;
  kind?: string;
  runs_program: boolean;
  agents?: string[];
  pages?: string[];
  jobs?: string[];
  data_kinds?: string[];
  reads?: string[];
  writes?: string[];
  core?: string;
  ref?: string;
  commit?: string;
  problems?: string[];
  warnings?: string[];
  exists: boolean;
  personas?: PackagePreviewPersona[];
}

export interface PackageReloadRequest {
  id?: string;
}

export interface PackageReloadResponse {
  packages?: Package[];
}

export type PackageRemedy = "install_go";

export interface PackageSetEnabledRequest {
  id?: string;
  enabled: boolean;
  by?: Actor;
}

export interface PackageSetEnabledResponse {
  package?: Package;
}

export interface PackageSetExtensionRequest {
  package?: string;
  extension?: string;
  value?: Record<string, unknown>;
  by?: Actor;
}

export interface PackageSetExtensionResponse {
}

export interface PackageSource {
  url?: string;
  ref?: string;
  commit?: string;
  installed_at: string;
}

export type PackageState = "stopped" | "building" | "running" | "failed" | "rejected";

export interface PackageSyncRequest {
  id?: string;
  apply: boolean;
  agent?: string;
  persona?: string;
  knowledge?: string[];
  by?: Actor;
}

export interface PackageSyncResponse {
  agents?: SyncAgent[];
  jobs?: SyncJob[];
  applied: boolean;
  optional?: string[];
}

export interface PackageUninstallRequest {
  id?: string;
  by?: Actor;
}

export interface PackageUninstallResponse {
}

export interface PackageUpgradeRequest {
  id?: string;
  ref?: string;
  by?: Actor;
}

export interface PackageUpgradeResponse {
  package?: Package;
  from?: string;
  to?: string;
}

export interface PageDecl {
  id?: string;
  title?: string;
  route?: string;
  component?: string;
  slot?: string;
  slot_front?: boolean;
  chat: boolean;
  chat_agent?: string;
  merge?: boolean;
  show_when?: string;
}

export interface PageInfo {
  id?: string;
  title?: string;
  route?: string;
  bundle_url?: string;
  package?: string;
  slots?: PageSlot[];
  slot?: string;
  slot_front?: boolean;
  error?: string;
  chat: boolean;
  chat_agent?: string;
  tier?: Tier;
  source?: string;
  alias?: string;
}

export interface PageRequest {
  limit: number;
  cursor?: string;
}

export interface PageResponse {
  next_cursor?: string;
}

export interface PageSlot {
  id?: string;
  page?: string;
}

export interface Pending {
  kind?: PendingKind;
  id?: string;
  tool?: string;
  summary?: string;
  questions?: Question[];
  since?: string;
  native?: boolean;
}

export type PendingKind = "permission" | "question";

export type PermissionLevel = "full" | "standard" | "readonly";

export type PermissionMode = "ask" | "auto";

export interface PermissionPolicy {
  mode?: PermissionMode;
  repos?: RepoPolicy[];
  allow?: string[];
  deny?: string[];
  guidance?: string[];
}

export interface PermissionSource {
  scope?: string;
  source?: string;
  kind?: string;
  value?: string;
}

export interface PersonaCard {
  slug?: string;
  name?: string;
  aka?: string[];
  source?: string;
  package?: string;
  unassignable?: string;
  status?: string;
  seats?: string[];
  shadowed: boolean;
  avatar_url?: string;
  tagline?: string;
  updated_at: string;
}

export interface PersonaPackLast {
  status?: string;
  version?: string;
  installed: number;
  unchanged: number;
  user_edited?: string[];
  reason?: string;
  at?: string;
  records_later?: boolean;
  message?: string;
}

export interface PersonaPackPending {
  version?: string;
  reason?: string;
  at?: string;
}

export interface PersonaStyleLevel {
  level?: string;
  text?: string;
  exists: boolean;
}

export interface PersonaTestRun {
  run?: string;
  status?: string;
  at: string;
  seat?: string;
  model?: string;
  style?: string;
  reason?: string;
  checks?: string;
  markdown?: string;
}

export interface PersonasCheckRequest {
  slug?: string;
}

export interface PersonasCheckResponse {
  problems?: string[];
}

export interface PersonasCopyRequest {
  from?: string;
  slug?: string;
  name?: string;
  aka?: string[];
  package?: string;
  by?: Actor;
}

export interface PersonasCopyResponse {
  persona?: PersonaCard;
}

export interface PersonasCreateRequest {
  slug?: string;
  name?: string;
  aka?: string[];
  package?: string;
  by?: Actor;
}

export interface PersonasCreateResponse {
  persona?: PersonaCard;
}

export interface PersonasExportRequest {
  personas?: string[];
  by?: Actor;
}

export interface PersonasExportResponse {
  personas?: string[];
  files: number;
  records: number;
  bytes: number;
  kinds?: string[];
  skipped?: string[];
  filename?: string;
  download?: string;
}

export interface PersonasGetRequest {
  slug?: string;
}

export interface PersonasGetResponse {
  persona?: PersonaCard;
  body?: string;
  raw?: string;
}

export interface PersonasGetStyleRequest {
}

export interface PersonasGetStyleResponse {
  level?: string;
  text?: string;
  custom_exists: boolean;
  levels?: PersonaStyleLevel[];
}

export interface PersonasImportRequest {
  upload?: string;
  dry_run?: boolean;
  force?: boolean;
  by?: Actor;
}

export interface PersonasImportResponse {
  dry_run: boolean;
  personas: string[];
  exported_at?: string;
  added?: string[];
  replaced?: string[];
  unchanged: number;
  kept?: string[];
  records_added?: string[];
  records_replaced?: string[];
  records_unchanged: number;
  records_kept?: string[];
  records_skipped?: string[];
  backup?: string;
}

export interface PersonasListRequest {
}

export interface PersonasListResponse {
  personas?: PersonaCard[];
}

export interface PersonasPackStatusRequest {
}

export interface PersonasPackStatusResponse {
  registered?: string;
  latest?: string;
  source: boolean;
  pending?: PersonaPackPending;
  last?: PersonaPackLast;
}

export interface PersonasPackSyncRequest {
  force?: boolean;
  pointer?: string;
  by?: Actor;
}

export interface PersonasPackSyncResponse {
  status: string;
  version?: string;
  registered?: string;
  installed?: string[];
  unchanged: number;
  user_edited?: string[];
  backup?: string;
  new_cards?: string[];
  records_added?: string[];
  records_replaced?: string[];
  records_unchanged?: number;
  records_kept?: string[];
  records_skipped?: string[];
  records_later?: boolean;
  reason?: string;
  message: string;
}

export interface PersonasSetStatusRequest {
  slug?: string;
  status?: string;
  by?: Actor;
}

export interface PersonasSetStatusResponse {
  persona?: PersonaCard;
}

export interface PersonasSetStyleRequest {
  level?: string;
  by?: Actor;
}

export interface PersonasSetStyleResponse {
  level?: string;
  text?: string;
  custom_exists: boolean;
  backup?: string;
}

export interface PersonasTestRequest {
  slug?: string;
  seat?: string;
  only?: string[];
  model?: string;
  card?: string;
  style?: string;
}

export interface PersonasTestResponse {
  run?: string;
  scenarios: number;
}

export interface PersonasTestResultsRequest {
  slug?: string;
  limit: number;
}

export interface PersonasTestResultsResponse {
  runs?: PersonaTestRun[];
}

export interface PersonasUpdateRequest {
  slug?: string;
  name?: string;
  aka?: string[];
  body?: string;
  base_updated_at: string;
  by?: Actor;
}

export interface PersonasUpdateResponse {
  persona?: PersonaCard;
  backup?: string;
}

export interface PlanbookEntry {
  at?: string;
  by?: string;
  note?: string;
}

export interface PolicyDecideRequest {
  id?: string;
  approve: boolean;
  permanent: boolean;
  note?: string;
  scope?: string;
  text?: string;
  fingerprint?: string;
  by?: Actor;
}

export interface PolicyDecideResponse {
  request?: PolicyRequest;
}

export interface PolicyGetRequest {
  id?: string;
}

export interface PolicyGetResponse {
  request?: PolicyRequest;
}

export type PolicyKind = "rule" | "guidance";

export interface PolicyListRequest {
  statuses?: PolicyStatus[];
  agent?: string;
}

export interface PolicyListResponse {
  requests?: PolicyRequest[];
}

export interface PolicyRequest {
  id?: string;
  from?: string;
  kind?: PolicyKind;
  agent?: string;
  action?: ActionClass;
  scope?: string;
  text?: string;
  why?: string;
  risk?: string;
  impact?: string;
  broad: boolean;
  status?: PolicyStatus;
  permanent: boolean;
  created_at?: string;
  expires_at?: string;
  decided_at?: string;
  decided_by?: string;
  note?: string;
  notice?: string;
  fingerprint?: string;
}

export interface PolicyRequestRequest {
  kind?: PolicyKind;
  agent?: string;
  action?: ActionClass;
  scope?: string;
  text?: string;
  why?: string;
  risk?: string;
  by?: Actor;
}

export interface PolicyRequestResponse {
  request?: PolicyRequest;
}

export interface PolicyRevokeRequest {
  id?: string;
  note?: string;
  by?: Actor;
}

export interface PolicyRevokeResponse {
  request?: PolicyRequest;
}

export interface PolicyRule {
  agent?: string;
  action?: ActionClass;
  scope?: string;
}

export type PolicyStatus = "pending" | "applied" | "rejected" | "failed" | "revoked" | "expired";

export interface PreflightCheck {
  id?: string;
  name?: string;
  ok: boolean;
  detail?: string;
  fix?: string;
  install?: string[];
  login?: string[];
  login_hint?: string[];
}

export type Proposed = TaskStatus;

export interface Question {
  header?: string;
  text?: string;
  options?: QuestionOption[];
  multi_select: boolean;
  allow_free_text: boolean;
}

export interface QuestionOption {
  label?: string;
  description?: string;
}

export interface Reminder {
  id?: string;
  content?: string;
  follow?: Follow;
  cycle?: Cycle;
  at?: string;
  owner?: string;
  room?: string;
  status?: ReminderStatus;
  check?: string;
  source?: string;
  asks: number;
  sent_at?: string;
  goal?: string;
  body?: string;
  difficulty?: Difficulty;
  done_points: number;
  done_at?: string;
  due_at?: string;
  points: number;
  attachments?: string[];
  by?: string;
  created_at?: string;
  updated_at?: string;
  closed_at?: string;
  archived: boolean;
}

export type ReminderQuery = "today" | "overdue" | "this_week" | "waiting" | "no_time" | "done_yesterday" | "upcoming";

export type ReminderStatus = "todo" | "doing" | "asking" | "shelved" | "done" | "cancelled";

export interface RemindersCancelRequest {
  id?: string;
  by?: Actor;
}

export interface RemindersCancelResponse {
  reminder?: Reminder;
}

export interface RemindersCreateRequest {
  content?: string;
  follow?: Follow;
  cycle?: Cycle;
  at?: string;
  owner?: string;
  room?: string;
  check?: string;
  source?: string;
  body?: string;
  difficulty?: Difficulty;
  attachments?: string[];
  by?: Actor;
}

export interface RemindersCreateResponse {
  reminder?: Reminder;
}

export interface RemindersDoingRequest {
  id?: string;
  by?: Actor;
}

export interface RemindersDoingResponse {
  reminder?: Reminder;
}

export interface RemindersDoneRequest {
  id?: string;
  by?: Actor;
}

export interface RemindersDoneResponse {
  reminder?: Reminder;
}

export interface RemindersGetRequest {
  id?: string;
}

export interface RemindersGetResponse {
  reminder?: Reminder;
}

export interface RemindersListRequest {
  query?: ReminderQuery;
  owner?: string;
  statuses?: ReminderStatus[];
  include_archived: boolean;
  page?: PageRequest;
}

export interface RemindersListResponse {
  reminders?: Reminder[];
  page?: PageResponse;
}

export interface RemindersSnoozeRequest {
  id?: string;
  until?: string;
  by?: Actor;
}

export interface RemindersSnoozeResponse {
  reminder?: Reminder;
}

export interface RemindersUpdateRequest {
  id?: string;
  content?: string;
  follow?: Follow;
  cycle?: Cycle;
  at?: string;
  owner?: string;
  room?: string;
  check?: string;
  goal?: string;
  body?: string;
  difficulty?: Difficulty;
  add_attachments?: string[];
  remove_attachments?: string[];
  by?: Actor;
}

export interface RemindersUpdateResponse {
  reminder?: Reminder;
}

export interface RemoteAccess {
  enabled: boolean;
  name?: string;
  url?: string;
}

export interface RepoPolicy {
  path?: string;
  remote?: string;
  push: boolean;
  writers?: string[];
}

export interface Requirement {
  id?: string;
  version?: string;
}

export interface RosterAgent {
  id?: string;
  name?: string;
  duty?: string;
  color?: string;
  color_dark?: string;
  avatar_url?: string;
  enabled: boolean;
  video_url?: string;
  video_urls?: string[];
  persona?: string;
}

export interface Route {
  service?: string;
  method?: string;
  path?: string;
  request?: string;
  response?: string;
  stream: boolean;
}

export interface RulesDecideRequest {
  id?: string;
  accept: boolean;
  note?: string;
  by?: Actor;
}

export interface RulesDecideResponse {
  proposal?: RulesProposal;
  version?: RulesVersion;
}

export interface RulesDiffLine {
  op?: RulesDiffOp;
  text: string;
}

export type RulesDiffOp = "same" | "add" | "del";

export interface RulesDoc {
  id?: string;
  title?: string;
  rank?: number;
  path?: string;
  section?: string;
  exists: boolean;
  revision?: string;
  pending?: number;
  problem?: string;
}

export interface RulesGetRequest {
  doc?: string;
}

export interface RulesGetResponse {
  doc?: RulesDoc;
  text?: string;
}

export interface RulesHistoryRequest {
  doc?: string;
}

export interface RulesHistoryResponse {
  versions?: RulesVersion[];
}

export interface RulesListRequest {
}

export interface RulesListResponse {
  docs?: RulesDoc[];
  problem?: string;
}

export interface RulesProposal {
  id?: string;
  doc?: string;
  section?: string;
  from?: string;
  why?: string;
  text?: string;
  status?: RulesProposalStatus;
  base?: string;
  stale?: boolean;
  created_at?: string;
  decided_at?: string;
  decided_by?: string;
  note?: string;
  notice?: string;
  version?: string;
  diff?: RulesDiffLine[];
}

export type RulesProposalStatus = "pending" | "accepted" | "rejected";

export interface RulesProposalsRequest {
  doc?: string;
  statuses?: RulesProposalStatus[];
}

export interface RulesProposalsResponse {
  proposals?: RulesProposal[];
}

export interface RulesProposeRequest {
  doc?: string;
  section?: string;
  text?: string;
  why?: string;
  by?: Actor;
}

export interface RulesProposeResponse {
  proposal?: RulesProposal;
}

export interface RulesRestoreRequest {
  id?: string;
  revision?: string;
  note?: string;
  by?: Actor;
}

export interface RulesRestoreResponse {
  doc?: RulesDoc;
  version?: RulesVersion;
}

export interface RulesSaveRequest {
  doc?: string;
  text?: string;
  revision?: string;
  note?: string;
  by?: Actor;
}

export interface RulesSaveResponse {
  doc?: RulesDoc;
  version?: RulesVersion;
}

export interface RulesVersion {
  id?: string;
  doc?: string;
  title?: string;
  source?: RulesVersionSource;
  note?: string;
  by?: string;
  proposal?: string;
  restored_from?: string;
  created_at?: string;
  text?: string;
}

export interface RulesVersionRequest {
  id?: string;
  against?: string;
}

export interface RulesVersionResponse {
  version?: RulesVersion;
  against?: string;
  diff?: RulesDiffLine[];
}

export type RulesVersionSource = "save" | "restore" | "proposal" | "outside";

export interface SchedulerListJobsRequest {
  agent?: string;
  include_archived?: boolean;
}

export interface SchedulerListJobsResponse {
  jobs?: Job[];
}

export interface SchedulerRemoveJobRequest {
  id?: string;
  by?: Actor;
}

export interface SchedulerRemoveJobResponse {
  job?: Job;
  archived: boolean;
}

export interface SchedulerRunJobRequest {
  id?: string;
  by?: Actor;
}

export interface SchedulerRunJobResponse {
  job?: Job;
  at?: string;
}

export interface SchedulerSaveJobRequest {
  job?: Job;
  by?: Actor;
}

export interface SchedulerSaveJobResponse {
  job?: Job;
}

export interface SchedulerSetJobRequest {
  id?: string;
  enabled?: boolean;
  cron?: string[];
  by?: Actor;
  name?: string;
  body?: string;
}

export interface SchedulerSetJobResponse {
  job?: Job;
}

export interface SdkRecord {
  kind?: string;
  id?: string;
  fields?: Record<string, unknown>;
  body?: string;
  revision?: string;
  archived: boolean;
  updated_at?: string;
}

export interface Session {
  name?: string;
  agent?: string;
  kind?: SessionKind;
  engine?: string;
  account?: string;
  model?: string;
  state?: SessionState;
  state_since?: string;
  pending?: Pending;
  engine_session_id?: string;
  remote_access?: RemoteAccess;
  last_event?: string;
  last_event_at?: string;
  last_turn_end_at?: string;
  last_user_at?: string;
  cwd?: string;
  attach?: string;
  task?: string;
  queued_at?: string;
  limited_until?: string;
  messages?: SessionMessage[];
  last_command?: SessionCommandNote;
  model_label?: string;
  effort?: string;
}

export interface SessionChatRequest {
  session?: string;
  since_uuid?: string;
  follow: boolean;
}

export interface SessionChatResponse {
  message?: ChatMessage;
}

export interface SessionCloseRequest {
  name?: string;
}

export interface SessionCloseResponse {
}

export type SessionCommand = "clear" | "compact" | "model" | "effort" | "interrupt";

export interface SessionCommandNote {
  command?: SessionCommand;
  arg?: string;
  at?: string;
  by?: string;
}

export interface SessionCommandRequest {
  session?: string;
  command?: SessionCommand;
  arg?: string;
  by?: Actor;
}

export interface SessionCommandResponse {
  note?: SessionCommandNote;
}

export interface SessionGetRequest {
  name?: string;
}

export interface SessionGetResponse {
  session?: Session;
}

export type SessionKind = "resident" | "room" | "temp" | "clone" | "work";

export interface SessionListRequest {
  agent?: string;
  states?: SessionState[];
}

export interface SessionListResponse {
  sessions?: Session[];
}

export interface SessionListThreadsRequest {
  agent?: string;
  include_stopped: boolean;
}

export interface SessionListThreadsResponse {
  threads?: ThreadSummary[];
  waiting_user: number;
}

export interface SessionMarkSeenRequest {
  session?: string;
  uuid?: string;
  by?: Actor;
}

export interface SessionMarkSeenResponse {
  thread?: ThreadSummary;
}

export interface SessionMessage {
  id?: string;
  from?: string;
  body?: string;
  lifecycle?: Lifecycle;
  reason?: string;
  reason_detail?: string;
  reason_kind?: EngineErrorKind;
  interject?: boolean;
  created_at?: string;
  wait?: MessageWait;
  wait_until?: string;
}

export interface SessionOpenRequest {
  name?: string;
  agent?: string;
  kind?: SessionKind;
  cwd?: string;
  topic?: string;
  first_message?: string;
  by?: Actor;
  wake?: boolean;
}

export interface SessionOpenResponse {
  session?: Session;
}

export interface SessionResolveRequest {
  session?: string;
  pending_id?: string;
  decision?: Decision;
  by?: Actor;
}

export interface SessionResolveResponse {
  applied: boolean;
}

export interface SessionRotateRequest {
  name?: string;
}

export interface SessionRotateResponse {
  session?: Session;
}

export type SessionState = "stopped" | "starting" | "idle" | "busy" | "waiting" | "limited";

export interface SessionWatchRequest {
  agent?: string;
}

export interface SessionWatchResponse {
  session?: Session;
}

export interface SessionWatchThreadsRequest {
  agent?: string;
}

export interface SessionWatchThreadsResponse {
  thread?: ThreadSummary;
  waiting_user: number;
}

export interface ShareRequest {
  path?: string;
  agent?: string;
}

export interface ShareResponse {
  url?: string;
  mime?: string;
  public_base?: string;
}

export interface SyncAgent {
  id?: string;
  action?: string;
  added: number;
  removed: number;
  persona?: string;
  persona_from?: string;
}

export interface SyncJob {
  agent?: string;
  id?: string;
  action?: string;
}

export interface SystemAccountsRequest {
}

export interface SystemAccountsResponse {
  accounts?: Account[];
  engines?: string[];
}

export interface SystemBackupSweepRequest {
  run?: boolean;
  by?: Actor;
}

export interface SystemBackupSweepResponse {
  report?: BackupSweepReport;
  running: boolean;
}

export interface SystemBackupsRequest {
  prune?: boolean;
  apply?: boolean;
  keep?: number;
  days?: number;
  by?: Actor;
}

export interface SystemBackupsResponse {
  files?: BackupFile[];
  total: number;
  pruned: number;
  freed: number;
  applied: boolean;
}

export interface SystemDoctorRequest {
  groups?: DoctorGroup[];
}

export interface SystemDoctorResponse {
  checks?: DoctorCheck[];
}

export interface SystemGetConfigRequest {
  key?: string;
}

export interface SystemGetConfigResponse {
  key?: string;
  value?: string;
  text?: string;
}

export interface SystemGetModeRequest {
}

export interface SystemGetModeResponse {
  mode?: HomeMode;
  chat_display?: ChatDisplay;
  env?: HomeEnv;
  label?: string;
  machine?: string;
  user_name?: string;
  version?: string;
  board_api?: number;
  public_url?: string;
}

export interface SystemGetWorkMaxRequest {
}

export interface SystemGetWorkMaxResponse {
  work_max?: number;
  set?: boolean;
  default?: number;
  min?: number;
  max?: number;
}

export interface SystemPreflightRequest {
}

export interface SystemPreflightResponse {
  checks?: PreflightCheck[];
}

export interface SystemReportRequest {
  detail?: boolean;
}

export interface SystemReportResponse {
  title?: string;
  body?: string;
  issue_link?: string;
}

export interface SystemSaveAccountRequest {
  account: Account;
  make_default: boolean;
  by?: Actor;
}

export interface SystemSaveAccountResponse {
  backup?: string;
}

export interface SystemSetChatDisplayRequest {
  chat_display?: ChatDisplay;
  by?: Actor;
}

export interface SystemSetChatDisplayResponse {
  chat_display?: ChatDisplay;
  backup?: string;
}

export interface SystemSetConfigRequest {
  key?: string;
  value?: string;
  by?: Actor;
}

export interface SystemSetConfigResponse {
  key?: string;
  value?: string;
  backup?: string;
}

export interface SystemSetModeRequest {
  mode?: HomeMode;
  by?: Actor;
}

export interface SystemSetModeResponse {
  mode?: HomeMode;
  backup?: string;
}

export interface SystemSetPermissionLevelsRequest {
  default_level?: PermissionLevel;
  levels?: AgentPermission[];
  by?: Actor;
}

export interface SystemSetPermissionLevelsResponse {
  agents?: AgentPermission[];
  backup?: string;
}

export interface SystemSetWorkMaxRequest {
  work_max?: number;
  by?: Actor;
}

export interface SystemSetWorkMaxResponse {
  work_max?: number;
  backup?: string;
}

export interface SystemTrustRequest {
  denied_days?: number;
  snapshot?: boolean;
}

export interface SystemTrustResponse {
  policy?: PermissionPolicy;
  default_level?: PermissionLevel;
  agents?: AgentPermission[];
  sources?: PermissionSource[];
  denied?: DeniedAction[];
  snapshot_path?: string;
  changes?: string[];
}

export interface SystemUsageRequest {
  account?: string;
}

export interface SystemUsageResponse {
  accounts?: AccountUsage[];
}

export interface Task {
  ask?: string;
  waiting_since?: string;
  nudges: number;
  last_nudged_at?: string;
  proposed?: TaskStatus;
  review_note?: string;
  accepted_by?: string;
  accepted_at?: string;
  id?: string;
  title?: string;
  type?: TaskType;
  agent?: string;
  status?: TaskStatus;
  result?: string;
  verify?: Verify;
  verify_how?: string;
  links?: string[];
  attachments?: string[];
  goal?: string;
  criteria?: string;
  criteria_owner?: boolean;
  engine_session?: string;
  body?: string;
  by?: string;
  created_at?: string;
  updated_at?: string;
  closed_at?: string;
  archived: boolean;
}

export interface TaskFollowPrefs {
  nudge_at?: string;
  stale_days: number;
  nudge_every_days: number;
  nudge_max: number;
}

export type TaskStatus = "idea" | "doing" | "waiting_user" | "waiting_external" | "done" | "cancelled";

export type TaskType = "task" | "ask";

export interface TasksAcceptRequest {
  id?: string;
  by?: Actor;
}

export interface TasksAcceptResponse {
  task?: Task;
}

export interface TasksArchiveRequest {
  ids?: string[];
  by?: Actor;
}

export interface TasksArchiveResponse {
  archived?: string[];
}

export interface TasksCloseRequest {
  id?: string;
  status?: TaskStatus;
  result?: string;
  verify?: Verify;
  verify_how?: string;
  add_links?: string[];
  by?: Actor;
}

export interface TasksCloseResponse {
  task?: Task;
}

export interface TasksCreateRequest {
  ask?: string;
  goal?: string;
  title?: string;
  type?: TaskType;
  agent?: string;
  status?: TaskStatus;
  result?: string;
  verify?: Verify;
  verify_how?: string;
  body?: string;
  attachments?: string[];
  criteria?: string;
  criteria_from_owner?: boolean;
  by?: Actor;
}

export interface TasksCreateResponse {
  task?: Task;
}

export interface TasksGetRequest {
  id?: string;
}

export interface TasksGetResponse {
  task?: Task;
}

export interface TasksListRequest {
  agent?: string;
  statuses?: TaskStatus[];
  ideas_only: boolean;
  include_archived: boolean;
  goal?: string;
  page?: PageRequest;
}

export interface TasksListResponse {
  tasks?: Task[];
  page?: PageResponse;
}

export interface TasksReconcileRequest {
  agent?: string;
}

export interface TasksReconcileResponse {
  tasks?: Task[];
  shelved_ids?: string[];
}

export interface TasksRejectRequest {
  id?: string;
  note?: string;
  by?: Actor;
}

export interface TasksRejectResponse {
  task?: Task;
}

export interface TasksStaleRequest {
}

export interface TasksStaleResponse {
  tasks?: Task[];
}

export interface TasksSubmitRequest {
  id?: string;
  result?: string;
  verify?: Verify;
  verify_how?: string;
  proposed?: TaskStatus;
  add_links?: string[];
  by?: Actor;
}

export interface TasksSubmitResponse {
  task?: Task;
}

export interface TasksUpdateRequest {
  ask?: string;
  id?: string;
  title?: string;
  body?: string;
  status?: TaskStatus;
  result?: string;
  agent?: string;
  type?: TaskType;
  goal?: string;
  engine_session?: string;
  add_links?: string[];
  add_attachments?: string[];
  remove_attachments?: string[];
  criteria?: string;
  criteria_from_owner?: boolean;
  by?: Actor;
}

export interface TasksUpdateResponse {
  task?: Task;
}

export interface TasksWaitingRequest {
}

export interface TasksWaitingResponse {
  tasks?: WaitingTask[];
}

export type ThreadStatus = "stopped" | "starting" | "idle" | "queued" | "working" | "waiting_user" | "done" | "limited";

export interface ThreadSummary {
  name?: string;
  agent?: string;
  title?: string;
  status?: ThreadStatus;
  waiting?: WaitingReason[];
  activity?: string;
  status_since?: string;
  unread: number;
  queued: number;
  background_tasks: number;
  last_preview?: string;
  last_assistant_at?: string;
  seen_uuid?: string;
  kind?: SessionKind;
  task?: string;
  last_turn_end_at?: string;
  limited_until?: string;
}

export type Tier = "official" | "installed" | "mine";

export interface TimeRange {
  from?: string;
  to?: string;
}

export interface UpdateApplyRequest {
  version?: string;
  by?: Actor;
}

export interface UpdateCheckRequest {
  by?: Actor;
}

export interface UpdateDecideRequest {
  version?: string;
  decision?: string;
  by?: Actor;
}

export interface UpdateInfo {
  current?: string;
  target?: string;
  tag?: string;
  notes?: string;
  channel?: string;
  detail?: string;
  core_found: boolean;
  skipped?: string[];
  decision?: string;
  decision_at?: string;
  state?: UpdateState;
  checked_at?: string;
  tonight_at?: string;
  rescue?: boolean;
}

export interface UpdateItem {
  path?: string;
  summary?: string;
  diff?: string;
  conflicts_with_fork: boolean;
}

export interface UpdateRollbackRequest {
  by?: Actor;
}

export interface UpdateState {
  phase?: string;
  from?: string;
  to?: string;
  reason?: string;
  ok: boolean;
  started_at?: string;
  finished_at?: string;
  kind?: string;
  outcome?: string;
  restored?: boolean;
  next?: string;
  log?: string;
}

export interface UpdateStatusRequest {
}

export interface UsageWindow {
  kind?: UsageWindowKind;
  hours: number;
  used_ratio: number;
  resets_at?: string;
  expired?: boolean;
  rolled?: boolean;
}

export type UsageWindowKind = "short" | "weekly";

export interface VaultAppendRequest {
  kind?: string;
  id?: string;
  line?: string;
  by?: Actor;
}

export interface VaultAppendResponse {
  revision?: string;
}

export interface VaultArchiveRequest {
  kind?: string;
  id?: string;
  by?: Actor;
}

export interface VaultArchiveResponse {
}

export interface VaultChangesRequest {
  since?: string;
  until?: string;
  by?: string;
  kind?: string;
  limit?: number;
  before?: string;
}

export interface VaultChangesResponse {
  items?: ChangeEntry[];
  next_before?: string;
}

export interface VaultGetRequest {
  kind?: string;
  id?: string;
}

export interface VaultGetResponse {
  record?: SdkRecord;
}

export interface VaultListRequest {
  kind?: string;
  where?: Record<string, string>;
  include_archived: boolean;
  page?: PageRequest;
}

export interface VaultListResponse {
  records?: SdkRecord[];
  page?: PageResponse;
}

export interface VaultNextIdRequest {
  kind?: string;
}

export interface VaultNextIdResponse {
  id?: string;
}

export interface VaultPatchRequest {
  kind?: string;
  id?: string;
  set?: Record<string, unknown>;
  unset?: string[];
  by?: Actor;
  expected_revision?: string;
}

export interface VaultPatchResponse {
  record?: SdkRecord;
}

export interface VaultPutRequest {
  record?: SdkRecord;
  by?: Actor;
  expected_revision?: string;
}

export interface VaultPutResponse {
  record?: SdkRecord;
}

export interface VaultWatchRequest {
  kinds?: string[];
}

export interface VaultWatchResponse {
  type?: ChangeType;
  record?: SdkRecord;
  by?: Actor;
  at?: string;
}

export type Verify = "verified" | "unverifiable";

export type WaitingReason = "permission" | "question" | "approval" | "reminder" | "work_queue";

export interface WaitingTask {
  task?: Task;
  waiting_days: number;
}

export interface WebAnswerBootPromptRequest {
  session?: string;
  id?: string;
  choice?: string;
  by?: Actor;
}

export interface WebAnswerBootPromptResponse {
}

export interface WebBootPrompt {
  id?: string;
  session?: string;
  agent_name?: string;
  title?: string;
  choices?: string[];
}

export interface WebGetAgentsRequest {
}

export interface WebGetAgentsResponse {
  agents?: AgentSetting[];
  accounts?: string[];
}

export interface WebGetBootPromptsRequest {
}

export interface WebGetBootPromptsResponse {
  prompts?: WebBootPrompt[];
}

export interface WebGetHealthRequest {
}

export interface WebGetHealthResponse {
  items?: HealthItem[];
}

export interface WebGetMenuPrefsRequest {
}

export interface WebGetMenuPrefsResponse {
  prefs?: MenuPrefs;
}

export interface WebGetMenuRequest {
}

export interface WebGetMenuResponse {
  menu?: Menu;
}

export interface WebGetOverviewRequest {
}

export interface WebGetOverviewResponse {
  waiting_total: number;
  approvals?: Notification[];
  waiting_tasks?: WaitingTask[];
  waiting_threads?: ThreadSummary[];
  unread: number;
  today_reminders?: Reminder[];
}

export interface WebGetPagesRequest {
}

export interface WebGetPagesResponse {
  pages?: PageInfo[];
}

export interface WebGetPhoneRequest {
  local: boolean;
  by?: Actor;
}

export interface WebGetPhoneResponse {
  phone: WebPhone;
}

export interface WebGetRosterRequest {
}

export interface WebGetRosterResponse {
  agents?: RosterAgent[];
}

export interface WebGetSitesRequest {
}

export interface WebGetSitesResponse {
  owner_login?: string;
  allow_boards?: string[];
  sites?: WebSite[];
}

export interface WebGetTrustRequest {
}

export interface WebGetTrustResponse {
  policy?: PermissionPolicy;
}

export interface WebPhone {
  local: boolean;
  tailscale?: string;
  account?: string;
  mounted: boolean;
  url?: string;
  push_ready: boolean;
  funnel: boolean;
  funnel_url?: string;
  problem?: string;
  download_url?: string;
}

export interface WebPhoneCloseFunnelRequest {
  local: boolean;
  by?: Actor;
}

export interface WebPhoneCloseFunnelResponse {
  phone: WebPhone;
}

export interface WebPhoneSetupRequest {
  local: boolean;
  by?: Actor;
}

export interface WebPhoneSetupResponse {
  ok: boolean;
  reason?: string;
  message?: string;
  help_url?: string;
  phone: WebPhone;
}

export interface WebSetAllowBoardsRequest {
  allow_boards?: string[];
  by?: Actor;
}

export interface WebSetAllowBoardsResponse {
  allow_boards?: string[];
  backup?: string;
}

export interface WebSetMenuPrefsRequest {
  prefs?: MenuPrefs;
  by?: Actor;
}

export interface WebSetMenuPrefsResponse {
  prefs?: MenuPrefs;
}

export interface WebSetOwnerLoginRequest {
  owner_login?: string;
  by?: Actor;
}

export interface WebSetOwnerLoginResponse {
  owner_login?: string;
  backup?: string;
}

export interface WebSetSitesRequest {
  sites?: WebSite[];
  by?: Actor;
}

export interface WebSetSitesResponse {
  sites?: WebSite[];
  backup?: string;
}

export interface WebSite {
  url?: string;
  color?: string;
}

export interface WebVoiceAnswerRequest {
  ask?: string;
  text?: string;
  by?: Actor;
}

export interface WebVoiceAnswerResponse {
  delivered: boolean;
  reply?: string;
}

export interface WebVoiceSendRequest {
  text?: string;
  to?: string;
  no_wait?: boolean;
  by?: Actor;
}

export interface WebVoiceSendResponse {
  agent?: string;
  agent_name?: string;
  text?: string;
  guessed: boolean;
  sure: boolean;
  envelope?: string;
  reply?: string;
  speak?: string;
  question: boolean;
  answered: boolean;
}
