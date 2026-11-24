package sdk

import "context"

type busClient struct{ c *Client }

func NewBusClient(c *Client) BusAPI { return &busClient{c: c} }
func (c *busClient) Send(ctx context.Context, req *BusSendRequest) (*BusSendResponse, error) {
	return Invoke[BusSendRequest, BusSendResponse](ctx, c.c, "/api/bus/send", req)
}
func (c *busClient) Get(ctx context.Context, req *BusGetRequest) (*BusGetResponse, error) {
	return Invoke[BusGetRequest, BusGetResponse](ctx, c.c, "/api/bus/get", req)
}
func (c *busClient) List(ctx context.Context, req *BusListRequest) (*BusListResponse, error) {
	return Invoke[BusListRequest, BusListResponse](ctx, c.c, "/api/bus/list", req)
}
func (c *busClient) Watch(ctx context.Context, req *BusWatchRequest, send func(*BusWatchResponse) error) error {
	return InvokeStream(ctx, c.c, "/api/bus/watch", req, send)
}
func (c *busClient) Publish(ctx context.Context, req *BusPublishRequest) (*BusPublishResponse, error) {
	return Invoke[BusPublishRequest, BusPublishResponse](ctx, c.c, "/api/bus/publish", req)
}
func (c *busClient) Subscribe(ctx context.Context, req *BusSubscribeRequest, send func(*BusSubscribeResponse) error) error {
	return InvokeStream(ctx, c.c, "/api/bus/subscribe", req, send)
}

type engineClient struct{ c *Client }

func NewEngineClient(c *Client) EngineAPI { return &engineClient{c: c} }
func (c *engineClient) Capabilities(ctx context.Context, req *EngineCapabilitiesRequest) (*EngineCapabilitiesResponse, error) {
	return Invoke[EngineCapabilitiesRequest, EngineCapabilitiesResponse](ctx, c.c, "/api/engine/capabilities", req)
}
func (c *engineClient) Start(ctx context.Context, req *EngineStartRequest) (*EngineStartResponse, error) {
	return Invoke[EngineStartRequest, EngineStartResponse](ctx, c.c, "/api/engine/start", req)
}
func (c *engineClient) Stop(ctx context.Context, req *EngineStopRequest) (*EngineStopResponse, error) {
	return Invoke[EngineStopRequest, EngineStopResponse](ctx, c.c, "/api/engine/stop", req)
}
func (c *engineClient) Deliver(ctx context.Context, req *EngineDeliverRequest) (*EngineDeliverResponse, error) {
	return Invoke[EngineDeliverRequest, EngineDeliverResponse](ctx, c.c, "/api/engine/deliver", req)
}
func (c *engineClient) Transcript(ctx context.Context, req *EngineTranscriptRequest, send func(*EngineTranscriptResponse) error) error {
	return InvokeStream(ctx, c.c, "/api/engine/transcript", req, send)
}
func (c *engineClient) SetPermission(ctx context.Context, req *EngineSetPermissionRequest) (*EngineSetPermissionResponse, error) {
	return Invoke[EngineSetPermissionRequest, EngineSetPermissionResponse](ctx, c.c, "/api/engine/set_permission", req)
}
func (c *engineClient) ListBootPrompts(ctx context.Context, req *EngineListBootPromptsRequest) (*EngineListBootPromptsResponse, error) {
	return Invoke[EngineListBootPromptsRequest, EngineListBootPromptsResponse](ctx, c.c, "/api/engine/list_boot_prompts", req)
}
func (c *engineClient) AnswerBootPrompt(ctx context.Context, req *EngineAnswerBootPromptRequest) (*EngineAnswerBootPromptResponse, error) {
	return Invoke[EngineAnswerBootPromptRequest, EngineAnswerBootPromptResponse](ctx, c.c, "/api/engine/answer_boot_prompt", req)
}
func (c *engineClient) AnswerApproval(ctx context.Context, req *EngineAnswerApprovalRequest) (*EngineAnswerApprovalResponse, error) {
	return Invoke[EngineAnswerApprovalRequest, EngineAnswerApprovalResponse](ctx, c.c, "/api/engine/answer_approval", req)
}
func (c *engineClient) Command(ctx context.Context, req *EngineCommandRequest) (*EngineCommandResponse, error) {
	return Invoke[EngineCommandRequest, EngineCommandResponse](ctx, c.c, "/api/engine/command", req)
}
func (c *engineClient) Usage(ctx context.Context, req *EngineUsageRequest) (*EngineUsageResponse, error) {
	return Invoke[EngineUsageRequest, EngineUsageResponse](ctx, c.c, "/api/engine/usage", req)
}
func (c *engineClient) WriteAgentCards(ctx context.Context, req *EngineWriteAgentCardsRequest) (*EngineWriteAgentCardsResponse, error) {
	return Invoke[EngineWriteAgentCardsRequest, EngineWriteAgentCardsResponse](ctx, c.c, "/api/engine/write_agent_cards", req)
}
func (c *engineClient) Preflight(ctx context.Context, req *EnginePreflightRequest) (*EnginePreflightResponse, error) {
	return Invoke[EnginePreflightRequest, EnginePreflightResponse](ctx, c.c, "/api/engine/preflight", req)
}

func (c *engineClient) Ask(ctx context.Context, req *EngineAskRequest) (*EngineAskResponse, error) {
	return Invoke[EngineAskRequest, EngineAskResponse](ctx, c.c, "/api/engine/ask", req)
}

type engineHostClient struct{ c *Client }

func NewEngineHostClient(c *Client) EngineHostAPI { return &engineHostClient{c: c} }
func (c *engineHostClient) ReportEvent(ctx context.Context, req *EngineHostReportEventRequest) (*EngineHostReportEventResponse, error) {
	return Invoke[EngineHostReportEventRequest, EngineHostReportEventResponse](ctx, c.c, "/api/engine_host/report_event", req)
}
func (c *engineHostClient) RequestApproval(ctx context.Context, req *EngineHostRequestApprovalRequest) (*EngineHostRequestApprovalResponse, error) {
	return Invoke[EngineHostRequestApprovalRequest, EngineHostRequestApprovalResponse](ctx, c.c, "/api/engine_host/request_approval", req)
}
func (c *engineHostClient) GetContext(ctx context.Context, req *EngineHostGetContextRequest) (*EngineHostGetContextResponse, error) {
	return Invoke[EngineHostGetContextRequest, EngineHostGetContextResponse](ctx, c.c, "/api/engine_host/get_context", req)
}

type sessionClient struct{ c *Client }

func NewSessionClient(c *Client) SessionAPI { return &sessionClient{c: c} }
func (c *sessionClient) List(ctx context.Context, req *SessionListRequest) (*SessionListResponse, error) {
	return Invoke[SessionListRequest, SessionListResponse](ctx, c.c, "/api/session/list", req)
}
func (c *sessionClient) Get(ctx context.Context, req *SessionGetRequest) (*SessionGetResponse, error) {
	return Invoke[SessionGetRequest, SessionGetResponse](ctx, c.c, "/api/session/get", req)
}
func (c *sessionClient) Open(ctx context.Context, req *SessionOpenRequest) (*SessionOpenResponse, error) {
	return Invoke[SessionOpenRequest, SessionOpenResponse](ctx, c.c, "/api/session/open", req)
}
func (c *sessionClient) Close(ctx context.Context, req *SessionCloseRequest) (*SessionCloseResponse, error) {
	return Invoke[SessionCloseRequest, SessionCloseResponse](ctx, c.c, "/api/session/close", req)
}
func (c *sessionClient) Rotate(ctx context.Context, req *SessionRotateRequest) (*SessionRotateResponse, error) {
	return Invoke[SessionRotateRequest, SessionRotateResponse](ctx, c.c, "/api/session/rotate", req)
}
func (c *sessionClient) Resolve(ctx context.Context, req *SessionResolveRequest) (*SessionResolveResponse, error) {
	return Invoke[SessionResolveRequest, SessionResolveResponse](ctx, c.c, "/api/session/resolve", req)
}
func (c *sessionClient) Chat(ctx context.Context, req *SessionChatRequest, send func(*SessionChatResponse) error) error {
	return InvokeStream(ctx, c.c, "/api/session/chat", req, send)
}
func (c *sessionClient) Watch(ctx context.Context, req *SessionWatchRequest, send func(*SessionWatchResponse) error) error {
	return InvokeStream(ctx, c.c, "/api/session/watch", req, send)
}
func (c *sessionClient) ListThreads(ctx context.Context, req *SessionListThreadsRequest) (*SessionListThreadsResponse, error) {
	return Invoke[SessionListThreadsRequest, SessionListThreadsResponse](ctx, c.c, "/api/session/list_threads", req)
}
func (c *sessionClient) WatchThreads(ctx context.Context, req *SessionWatchThreadsRequest, send func(*SessionWatchThreadsResponse) error) error {
	return InvokeStream(ctx, c.c, "/api/session/watch_threads", req, send)
}
func (c *sessionClient) MarkSeen(ctx context.Context, req *SessionMarkSeenRequest) (*SessionMarkSeenResponse, error) {
	return Invoke[SessionMarkSeenRequest, SessionMarkSeenResponse](ctx, c.c, "/api/session/mark_seen", req)
}
func (c *sessionClient) Command(ctx context.Context, req *SessionCommandRequest) (*SessionCommandResponse, error) {
	return Invoke[SessionCommandRequest, SessionCommandResponse](ctx, c.c, "/api/session/command", req)
}

type notifyClient struct{ c *Client }

func NewNotifyClient(c *Client) NotifyAPI { return &notifyClient{c: c} }
func (c *notifyClient) Send(ctx context.Context, req *NotifySendRequest) (*NotifySendResponse, error) {
	return Invoke[NotifySendRequest, NotifySendResponse](ctx, c.c, "/api/notify/send", req)
}
func (c *notifyClient) Get(ctx context.Context, req *NotifyGetRequest) (*NotifyGetResponse, error) {
	return Invoke[NotifyGetRequest, NotifyGetResponse](ctx, c.c, "/api/notify/get", req)
}
func (c *notifyClient) List(ctx context.Context, req *NotifyListRequest) (*NotifyListResponse, error) {
	return Invoke[NotifyListRequest, NotifyListResponse](ctx, c.c, "/api/notify/list", req)
}
func (c *notifyClient) MarkRead(ctx context.Context, req *NotifyMarkReadRequest) (*NotifyMarkReadResponse, error) {
	return Invoke[NotifyMarkReadRequest, NotifyMarkReadResponse](ctx, c.c, "/api/notify/mark_read", req)
}
func (c *notifyClient) Archive(ctx context.Context, req *NotifyArchiveRequest) (*NotifyArchiveResponse, error) {
	return Invoke[NotifyArchiveRequest, NotifyArchiveResponse](ctx, c.c, "/api/notify/archive", req)
}
func (c *notifyClient) Answer(ctx context.Context, req *NotifyAnswerRequest) (*NotifyAnswerResponse, error) {
	return Invoke[NotifyAnswerRequest, NotifyAnswerResponse](ctx, c.c, "/api/notify/answer", req)
}
func (c *notifyClient) GetPrefs(ctx context.Context, req *NotifyGetPrefsRequest) (*NotifyGetPrefsResponse, error) {
	return Invoke[NotifyGetPrefsRequest, NotifyGetPrefsResponse](ctx, c.c, "/api/notify/get_prefs", req)
}
func (c *notifyClient) SetPrefs(ctx context.Context, req *NotifySetPrefsRequest) (*NotifySetPrefsResponse, error) {
	return Invoke[NotifySetPrefsRequest, NotifySetPrefsResponse](ctx, c.c, "/api/notify/set_prefs", req)
}
func (c *notifyClient) Watch(ctx context.Context, req *NotifyWatchRequest, send func(*NotifyWatchResponse) error) error {
	return InvokeStream(ctx, c.c, "/api/notify/watch", req, send)
}
func (c *notifyClient) Share(ctx context.Context, req *ShareRequest) (*ShareResponse, error) {
	return Invoke[ShareRequest, ShareResponse](ctx, c.c, "/api/notify/share", req)
}
func (c *notifyClient) PushKey(ctx context.Context, req *NotifyPushKeyRequest) (*NotifyPushKeyResponse, error) {
	return Invoke[NotifyPushKeyRequest, NotifyPushKeyResponse](ctx, c.c, "/api/notify/push_key", req)
}
func (c *notifyClient) SubscribePush(ctx context.Context, req *NotifySubscribePushRequest) (*NotifySubscribePushResponse, error) {
	return Invoke[NotifySubscribePushRequest, NotifySubscribePushResponse](ctx, c.c, "/api/notify/subscribe_push", req)
}
func (c *notifyClient) UnsubscribePush(ctx context.Context, req *NotifyUnsubscribePushRequest) (*NotifyUnsubscribePushResponse, error) {
	return Invoke[NotifyUnsubscribePushRequest, NotifyUnsubscribePushResponse](ctx, c.c, "/api/notify/unsubscribe_push", req)
}
func (c *notifyClient) PushTest(ctx context.Context, req *NotifyPushTestRequest) (*NotifyPushTestResponse, error) {
	return Invoke[NotifyPushTestRequest, NotifyPushTestResponse](ctx, c.c, "/api/notify/push_test", req)
}
func (c *notifyClient) Nudge(ctx context.Context, req *NotifyNudgeRequest) (*NotifyNudgeResponse, error) {
	return Invoke[NotifyNudgeRequest, NotifyNudgeResponse](ctx, c.c, "/api/notify/nudge", req)
}
func (c *notifyClient) Presence(ctx context.Context, req *NotifyPresenceRequest) (*NotifyPresenceResponse, error) {
	return Invoke[NotifyPresenceRequest, NotifyPresenceResponse](ctx, c.c, "/api/notify/presence", req)
}
func (c *notifyClient) ResolveNudge(ctx context.Context, req *NotifyResolveNudgeRequest) (*NotifyResolveNudgeResponse, error) {
	return Invoke[NotifyResolveNudgeRequest, NotifyResolveNudgeResponse](ctx, c.c, "/api/notify/resolve_nudge", req)
}

type tasksClient struct{ c *Client }

func NewTasksClient(c *Client) TasksAPI { return &tasksClient{c: c} }
func (c *tasksClient) Waiting(ctx context.Context, req *TasksWaitingRequest) (*TasksWaitingResponse, error) {
	return Invoke[TasksWaitingRequest, TasksWaitingResponse](ctx, c.c, "/api/tasks/waiting", req)
}
func (c *tasksClient) Stale(ctx context.Context, req *TasksStaleRequest) (*TasksStaleResponse, error) {
	return Invoke[TasksStaleRequest, TasksStaleResponse](ctx, c.c, "/api/tasks/stale", req)
}
func (c *tasksClient) Reject(ctx context.Context, req *TasksRejectRequest) (*TasksRejectResponse, error) {
	return Invoke[TasksRejectRequest, TasksRejectResponse](ctx, c.c, "/api/tasks/reject", req)
}
func (c *tasksClient) Accept(ctx context.Context, req *TasksAcceptRequest) (*TasksAcceptResponse, error) {
	return Invoke[TasksAcceptRequest, TasksAcceptResponse](ctx, c.c, "/api/tasks/accept", req)
}
func (c *tasksClient) Archive(ctx context.Context, req *TasksArchiveRequest) (*TasksArchiveResponse, error) {
	return Invoke[TasksArchiveRequest, TasksArchiveResponse](ctx, c.c, "/api/tasks/archive", req)
}
func (c *tasksClient) Submit(ctx context.Context, req *TasksSubmitRequest) (*TasksSubmitResponse, error) {
	return Invoke[TasksSubmitRequest, TasksSubmitResponse](ctx, c.c, "/api/tasks/submit", req)
}
func (c *tasksClient) Create(ctx context.Context, req *TasksCreateRequest) (*TasksCreateResponse, error) {
	return Invoke[TasksCreateRequest, TasksCreateResponse](ctx, c.c, "/api/tasks/create", req)
}
func (c *tasksClient) Get(ctx context.Context, req *TasksGetRequest) (*TasksGetResponse, error) {
	return Invoke[TasksGetRequest, TasksGetResponse](ctx, c.c, "/api/tasks/get", req)
}
func (c *tasksClient) List(ctx context.Context, req *TasksListRequest) (*TasksListResponse, error) {
	return Invoke[TasksListRequest, TasksListResponse](ctx, c.c, "/api/tasks/list", req)
}
func (c *tasksClient) Update(ctx context.Context, req *TasksUpdateRequest) (*TasksUpdateResponse, error) {
	return Invoke[TasksUpdateRequest, TasksUpdateResponse](ctx, c.c, "/api/tasks/update", req)
}
func (c *tasksClient) Close(ctx context.Context, req *TasksCloseRequest) (*TasksCloseResponse, error) {
	return Invoke[TasksCloseRequest, TasksCloseResponse](ctx, c.c, "/api/tasks/close", req)
}
func (c *tasksClient) Reconcile(ctx context.Context, req *TasksReconcileRequest) (*TasksReconcileResponse, error) {
	return Invoke[TasksReconcileRequest, TasksReconcileResponse](ctx, c.c, "/api/tasks/reconcile", req)
}

type remindersClient struct{ c *Client }

func NewRemindersClient(c *Client) RemindersAPI { return &remindersClient{c: c} }
func (c *remindersClient) Create(ctx context.Context, req *RemindersCreateRequest) (*RemindersCreateResponse, error) {
	return Invoke[RemindersCreateRequest, RemindersCreateResponse](ctx, c.c, "/api/reminders/create", req)
}
func (c *remindersClient) Get(ctx context.Context, req *RemindersGetRequest) (*RemindersGetResponse, error) {
	return Invoke[RemindersGetRequest, RemindersGetResponse](ctx, c.c, "/api/reminders/get", req)
}
func (c *remindersClient) List(ctx context.Context, req *RemindersListRequest) (*RemindersListResponse, error) {
	return Invoke[RemindersListRequest, RemindersListResponse](ctx, c.c, "/api/reminders/list", req)
}
func (c *remindersClient) Update(ctx context.Context, req *RemindersUpdateRequest) (*RemindersUpdateResponse, error) {
	return Invoke[RemindersUpdateRequest, RemindersUpdateResponse](ctx, c.c, "/api/reminders/update", req)
}
func (c *remindersClient) Snooze(ctx context.Context, req *RemindersSnoozeRequest) (*RemindersSnoozeResponse, error) {
	return Invoke[RemindersSnoozeRequest, RemindersSnoozeResponse](ctx, c.c, "/api/reminders/snooze", req)
}
func (c *remindersClient) Done(ctx context.Context, req *RemindersDoneRequest) (*RemindersDoneResponse, error) {
	return Invoke[RemindersDoneRequest, RemindersDoneResponse](ctx, c.c, "/api/reminders/done", req)
}
func (c *remindersClient) Doing(ctx context.Context, req *RemindersDoingRequest) (*RemindersDoingResponse, error) {
	return Invoke[RemindersDoingRequest, RemindersDoingResponse](ctx, c.c, "/api/reminders/doing", req)
}
func (c *remindersClient) Cancel(ctx context.Context, req *RemindersCancelRequest) (*RemindersCancelResponse, error) {
	return Invoke[RemindersCancelRequest, RemindersCancelResponse](ctx, c.c, "/api/reminders/cancel", req)
}

type vaultClient struct{ c *Client }

func NewVaultClient(c *Client) VaultAPI { return &vaultClient{c: c} }
func (c *vaultClient) Get(ctx context.Context, req *VaultGetRequest) (*VaultGetResponse, error) {
	return Invoke[VaultGetRequest, VaultGetResponse](ctx, c.c, "/api/vault/get", req)
}
func (c *vaultClient) List(ctx context.Context, req *VaultListRequest) (*VaultListResponse, error) {
	return Invoke[VaultListRequest, VaultListResponse](ctx, c.c, "/api/vault/list", req)
}
func (c *vaultClient) Put(ctx context.Context, req *VaultPutRequest) (*VaultPutResponse, error) {
	return Invoke[VaultPutRequest, VaultPutResponse](ctx, c.c, "/api/vault/put", req)
}
func (c *vaultClient) Patch(ctx context.Context, req *VaultPatchRequest) (*VaultPatchResponse, error) {
	return Invoke[VaultPatchRequest, VaultPatchResponse](ctx, c.c, "/api/vault/patch", req)
}
func (c *vaultClient) Append(ctx context.Context, req *VaultAppendRequest) (*VaultAppendResponse, error) {
	return Invoke[VaultAppendRequest, VaultAppendResponse](ctx, c.c, "/api/vault/append", req)
}
func (c *vaultClient) Archive(ctx context.Context, req *VaultArchiveRequest) (*VaultArchiveResponse, error) {
	return Invoke[VaultArchiveRequest, VaultArchiveResponse](ctx, c.c, "/api/vault/archive", req)
}
func (c *vaultClient) NextId(ctx context.Context, req *VaultNextIdRequest) (*VaultNextIdResponse, error) {
	return Invoke[VaultNextIdRequest, VaultNextIdResponse](ctx, c.c, "/api/vault/next_id", req)
}
func (c *vaultClient) Changes(ctx context.Context, req *VaultChangesRequest) (*VaultChangesResponse, error) {
	return Invoke[VaultChangesRequest, VaultChangesResponse](ctx, c.c, "/api/vault/changes", req)
}
func (c *vaultClient) Watch(ctx context.Context, req *VaultWatchRequest, send func(*VaultWatchResponse) error) error {
	return InvokeStream(ctx, c.c, "/api/vault/watch", req, send)
}

type goalsClient struct{ c *Client }

func NewGoalsClient(c *Client) GoalsAPI { return &goalsClient{c: c} }
func (c *goalsClient) Propose(ctx context.Context, req *GoalsProposeRequest) (*GoalsProposeResponse, error) {
	return Invoke[GoalsProposeRequest, GoalsProposeResponse](ctx, c.c, "/api/goals/propose", req)
}
func (c *goalsClient) Create(ctx context.Context, req *GoalsCreateRequest) (*GoalsCreateResponse, error) {
	return Invoke[GoalsCreateRequest, GoalsCreateResponse](ctx, c.c, "/api/goals/create", req)
}
func (c *goalsClient) Get(ctx context.Context, req *GoalsGetRequest) (*GoalsGetResponse, error) {
	return Invoke[GoalsGetRequest, GoalsGetResponse](ctx, c.c, "/api/goals/get", req)
}
func (c *goalsClient) List(ctx context.Context, req *GoalsListRequest) (*GoalsListResponse, error) {
	return Invoke[GoalsListRequest, GoalsListResponse](ctx, c.c, "/api/goals/list", req)
}
func (c *goalsClient) UpdatePlanbook(ctx context.Context, req *GoalsUpdatePlanbookRequest) (*GoalsUpdatePlanbookResponse, error) {
	return Invoke[GoalsUpdatePlanbookRequest, GoalsUpdatePlanbookResponse](ctx, c.c, "/api/goals/update_planbook", req)
}
func (c *goalsClient) Done(ctx context.Context, req *GoalsDoneRequest) (*GoalsDoneResponse, error) {
	return Invoke[GoalsDoneRequest, GoalsDoneResponse](ctx, c.c, "/api/goals/done", req)
}
func (c *goalsClient) Withdraw(ctx context.Context, req *GoalsWithdrawRequest) (*GoalsWithdrawResponse, error) {
	return Invoke[GoalsWithdrawRequest, GoalsWithdrawResponse](ctx, c.c, "/api/goals/withdraw", req)
}

func (c *engineHostClient) CheckAction(ctx context.Context, req *EngineHostCheckActionRequest) (*EngineHostCheckActionResponse, error) {
	return Invoke[EngineHostCheckActionRequest, EngineHostCheckActionResponse](ctx, c.c, "/api/engine_host/check_action", req)
}

type webClient struct{ c *Client }

func NewWebClient(c *Client) WebAPI { return &webClient{c: c} }
func (c *webClient) GetMenu(ctx context.Context, req *WebGetMenuRequest) (*WebGetMenuResponse, error) {
	return Invoke[WebGetMenuRequest, WebGetMenuResponse](ctx, c.c, "/api/web/get_menu", req)
}
func (c *webClient) GetPages(ctx context.Context, req *WebGetPagesRequest) (*WebGetPagesResponse, error) {
	return Invoke[WebGetPagesRequest, WebGetPagesResponse](ctx, c.c, "/api/web/get_pages", req)
}
func (c *webClient) GetMenuPrefs(ctx context.Context, req *WebGetMenuPrefsRequest) (*WebGetMenuPrefsResponse, error) {
	return Invoke[WebGetMenuPrefsRequest, WebGetMenuPrefsResponse](ctx, c.c, "/api/web/get_menu_prefs", req)
}
func (c *webClient) SetMenuPrefs(ctx context.Context, req *WebSetMenuPrefsRequest) (*WebSetMenuPrefsResponse, error) {
	return Invoke[WebSetMenuPrefsRequest, WebSetMenuPrefsResponse](ctx, c.c, "/api/web/set_menu_prefs", req)
}
func (c *webClient) GetHealth(ctx context.Context, req *WebGetHealthRequest) (*WebGetHealthResponse, error) {
	return Invoke[WebGetHealthRequest, WebGetHealthResponse](ctx, c.c, "/api/web/get_health", req)
}
func (c *webClient) GetRoster(ctx context.Context, req *WebGetRosterRequest) (*WebGetRosterResponse, error) {
	return Invoke[WebGetRosterRequest, WebGetRosterResponse](ctx, c.c, "/api/web/get_roster", req)
}
func (c *webClient) GetOverview(ctx context.Context, req *WebGetOverviewRequest) (*WebGetOverviewResponse, error) {
	return Invoke[WebGetOverviewRequest, WebGetOverviewResponse](ctx, c.c, "/api/web/get_overview", req)
}
func (c *webClient) GetTrust(ctx context.Context, req *WebGetTrustRequest) (*WebGetTrustResponse, error) {
	return Invoke[WebGetTrustRequest, WebGetTrustResponse](ctx, c.c, "/api/web/get_trust", req)
}
func (c *webClient) GetAgents(ctx context.Context, req *WebGetAgentsRequest) (*WebGetAgentsResponse, error) {
	return Invoke[WebGetAgentsRequest, WebGetAgentsResponse](ctx, c.c, "/api/web/get_agents", req)
}
func (c *webClient) GetSites(ctx context.Context, req *WebGetSitesRequest) (*WebGetSitesResponse, error) {
	return Invoke[WebGetSitesRequest, WebGetSitesResponse](ctx, c.c, "/api/web/get_sites", req)
}
func (c *webClient) SetSites(ctx context.Context, req *WebSetSitesRequest) (*WebSetSitesResponse, error) {
	return Invoke[WebSetSitesRequest, WebSetSitesResponse](ctx, c.c, "/api/web/set_sites", req)
}
func (c *webClient) SetAllowBoards(ctx context.Context, req *WebSetAllowBoardsRequest) (*WebSetAllowBoardsResponse, error) {
	return Invoke[WebSetAllowBoardsRequest, WebSetAllowBoardsResponse](ctx, c.c, "/api/web/set_allow_boards", req)
}
func (c *webClient) GetBootPrompts(ctx context.Context, req *WebGetBootPromptsRequest) (*WebGetBootPromptsResponse, error) {
	return Invoke[WebGetBootPromptsRequest, WebGetBootPromptsResponse](ctx, c.c, "/api/web/get_boot_prompts", req)
}
func (c *webClient) AnswerBootPrompt(ctx context.Context, req *WebAnswerBootPromptRequest) (*WebAnswerBootPromptResponse, error) {
	return Invoke[WebAnswerBootPromptRequest, WebAnswerBootPromptResponse](ctx, c.c, "/api/web/answer_boot_prompt", req)
}
func (c *webClient) VoiceAnswer(ctx context.Context, req *WebVoiceAnswerRequest) (*WebVoiceAnswerResponse, error) {
	return Invoke[WebVoiceAnswerRequest, WebVoiceAnswerResponse](ctx, c.c, "/api/web/voice_answer", req)
}

func (c *webClient) VoiceSend(ctx context.Context, req *WebVoiceSendRequest) (*WebVoiceSendResponse, error) {
	return Invoke[WebVoiceSendRequest, WebVoiceSendResponse](ctx, c.c, "/api/web/voice_send", req)
}
func (c *webClient) SetOwnerLogin(ctx context.Context, req *WebSetOwnerLoginRequest) (*WebSetOwnerLoginResponse, error) {
	return Invoke[WebSetOwnerLoginRequest, WebSetOwnerLoginResponse](ctx, c.c, "/api/web/set_owner_login", req)
}
func (c *webClient) GetPhone(ctx context.Context, req *WebGetPhoneRequest) (*WebGetPhoneResponse, error) {
	return Invoke[WebGetPhoneRequest, WebGetPhoneResponse](ctx, c.c, "/api/web/get_phone", req)
}
func (c *webClient) PhoneSetup(ctx context.Context, req *WebPhoneSetupRequest) (*WebPhoneSetupResponse, error) {
	return Invoke[WebPhoneSetupRequest, WebPhoneSetupResponse](ctx, c.c, "/api/web/phone_setup", req)
}
func (c *webClient) PhoneCloseFunnel(ctx context.Context, req *WebPhoneCloseFunnelRequest) (*WebPhoneCloseFunnelResponse, error) {
	return Invoke[WebPhoneCloseFunnelRequest, WebPhoneCloseFunnelResponse](ctx, c.c, "/api/web/phone_close_funnel", req)
}

type systemClient struct{ c *Client }

func NewSystemClient(c *Client) SystemAPI { return &systemClient{c: c} }
func (c *systemClient) Doctor(ctx context.Context, req *SystemDoctorRequest) (*SystemDoctorResponse, error) {
	return Invoke[SystemDoctorRequest, SystemDoctorResponse](ctx, c.c, "/api/system/doctor", req)
}
func (c *systemClient) Trust(ctx context.Context, req *SystemTrustRequest) (*SystemTrustResponse, error) {
	return Invoke[SystemTrustRequest, SystemTrustResponse](ctx, c.c, "/api/system/trust", req)
}
func (c *systemClient) SetPermissionLevels(ctx context.Context, req *SystemSetPermissionLevelsRequest) (*SystemSetPermissionLevelsResponse, error) {
	return Invoke[SystemSetPermissionLevelsRequest, SystemSetPermissionLevelsResponse](ctx, c.c, "/api/system/set_permission_levels", req)
}

type schedulerClient struct{ c *Client }

func NewSchedulerClient(c *Client) SchedulerAPI { return &schedulerClient{c: c} }
func (c *schedulerClient) ListJobs(ctx context.Context, req *SchedulerListJobsRequest) (*SchedulerListJobsResponse, error) {
	return Invoke[SchedulerListJobsRequest, SchedulerListJobsResponse](ctx, c.c, "/api/scheduler/list_jobs", req)
}
func (c *schedulerClient) SaveJob(ctx context.Context, req *SchedulerSaveJobRequest) (*SchedulerSaveJobResponse, error) {
	return Invoke[SchedulerSaveJobRequest, SchedulerSaveJobResponse](ctx, c.c, "/api/scheduler/save_job", req)
}
func (c *schedulerClient) SetJob(ctx context.Context, req *SchedulerSetJobRequest) (*SchedulerSetJobResponse, error) {
	return Invoke[SchedulerSetJobRequest, SchedulerSetJobResponse](ctx, c.c, "/api/scheduler/set_job", req)
}
func (c *schedulerClient) RemoveJob(ctx context.Context, req *SchedulerRemoveJobRequest) (*SchedulerRemoveJobResponse, error) {
	return Invoke[SchedulerRemoveJobRequest, SchedulerRemoveJobResponse](ctx, c.c, "/api/scheduler/remove_job", req)
}
func (c *schedulerClient) RunJob(ctx context.Context, req *SchedulerRunJobRequest) (*SchedulerRunJobResponse, error) {
	return Invoke[SchedulerRunJobRequest, SchedulerRunJobResponse](ctx, c.c, "/api/scheduler/run_job", req)
}
func (c *systemClient) GetConfig(ctx context.Context, req *SystemGetConfigRequest) (*SystemGetConfigResponse, error) {
	return Invoke[SystemGetConfigRequest, SystemGetConfigResponse](ctx, c.c, "/api/system/get_config", req)
}
func (c *systemClient) GetMode(ctx context.Context, req *SystemGetModeRequest) (*SystemGetModeResponse, error) {
	return Invoke[SystemGetModeRequest, SystemGetModeResponse](ctx, c.c, "/api/system/get_mode", req)
}
func (c *systemClient) SetMode(ctx context.Context, req *SystemSetModeRequest) (*SystemSetModeResponse, error) {
	return Invoke[SystemSetModeRequest, SystemSetModeResponse](ctx, c.c, "/api/system/set_mode", req)
}
func (c *systemClient) SetChatDisplay(ctx context.Context, req *SystemSetChatDisplayRequest) (*SystemSetChatDisplayResponse, error) {
	return Invoke[SystemSetChatDisplayRequest, SystemSetChatDisplayResponse](ctx, c.c, "/api/system/set_chat_display", req)
}
func (c *systemClient) GetWorkMax(ctx context.Context, req *SystemGetWorkMaxRequest) (*SystemGetWorkMaxResponse, error) {
	return Invoke[SystemGetWorkMaxRequest, SystemGetWorkMaxResponse](ctx, c.c, "/api/system/get_work_max", req)
}
func (c *systemClient) SetWorkMax(ctx context.Context, req *SystemSetWorkMaxRequest) (*SystemSetWorkMaxResponse, error) {
	return Invoke[SystemSetWorkMaxRequest, SystemSetWorkMaxResponse](ctx, c.c, "/api/system/set_work_max", req)
}
func (c *systemClient) Accounts(ctx context.Context, req *SystemAccountsRequest) (*SystemAccountsResponse, error) {
	return Invoke[SystemAccountsRequest, SystemAccountsResponse](ctx, c.c, "/api/system/accounts", req)
}
func (c *systemClient) SaveAccount(ctx context.Context, req *SystemSaveAccountRequest) (*SystemSaveAccountResponse, error) {
	return Invoke[SystemSaveAccountRequest, SystemSaveAccountResponse](ctx, c.c, "/api/system/save_account", req)
}

func (c *systemClient) SetConfig(ctx context.Context, req *SystemSetConfigRequest) (*SystemSetConfigResponse, error) {
	return Invoke[SystemSetConfigRequest, SystemSetConfigResponse](ctx, c.c, "/api/system/set_config", req)
}

type agentsClient struct{ c *Client }

func NewAgentsClient(c *Client) AgentsAPI { return &agentsClient{c: c} }
func (c *agentsClient) Create(ctx context.Context, req *AgentsCreateRequest) (*AgentsCreateResponse, error) {
	return Invoke[AgentsCreateRequest, AgentsCreateResponse](ctx, c.c, "/api/agents/create", req)
}
func (c *agentsClient) Archive(ctx context.Context, req *AgentsArchiveRequest) (*AgentsArchiveResponse, error) {
	return Invoke[AgentsArchiveRequest, AgentsArchiveResponse](ctx, c.c, "/api/agents/archive", req)
}
func (c *agentsClient) Rename(ctx context.Context, req *AgentsRenameRequest) (*AgentsRenameResponse, error) {
	return Invoke[AgentsRenameRequest, AgentsRenameResponse](ctx, c.c, "/api/agents/rename", req)
}
func (c *agentsClient) Assign(ctx context.Context, req *AgentsAssignRequest) (*AgentsAssignResponse, error) {
	return Invoke[AgentsAssignRequest, AgentsAssignResponse](ctx, c.c, "/api/agents/assign", req)
}
func (c *agentsClient) Swap(ctx context.Context, req *AgentsSwapRequest) (*AgentsSwapResponse, error) {
	return Invoke[AgentsSwapRequest, AgentsSwapResponse](ctx, c.c, "/api/agents/swap", req)
}
func (c *agentsClient) SetAccount(ctx context.Context, req *AgentsSetAccountRequest) (*AgentsSetAccountResponse, error) {
	return Invoke[AgentsSetAccountRequest, AgentsSetAccountResponse](ctx, c.c, "/api/agents/set_account", req)
}
func (c *agentsClient) SetField(ctx context.Context, req *AgentsSetFieldRequest) (*AgentsSetFieldResponse, error) {
	return Invoke[AgentsSetFieldRequest, AgentsSetFieldResponse](ctx, c.c, "/api/agents/set_field", req)
}
func (c *agentsClient) Link(ctx context.Context, req *AgentsLinkRequest) (*AgentsLinkResponse, error) {
	return Invoke[AgentsLinkRequest, AgentsLinkResponse](ctx, c.c, "/api/agents/link", req)
}
func (c *agentsClient) AddRule(ctx context.Context, req *AgentsAddRuleRequest) (*AgentsAddRuleResponse, error) {
	return Invoke[AgentsAddRuleRequest, AgentsAddRuleResponse](ctx, c.c, "/api/agents/add_rule", req)
}
func (c *agentsClient) Customizations(ctx context.Context, req *AgentsCustomizationsRequest) (*AgentsCustomizationsResponse, error) {
	return Invoke[AgentsCustomizationsRequest, AgentsCustomizationsResponse](ctx, c.c, "/api/agents/customizations", req)
}
func (c *agentsClient) WriteNote(ctx context.Context, req *AgentsWriteNoteRequest) (*AgentsWriteNoteResponse, error) {
	return Invoke[AgentsWriteNoteRequest, AgentsWriteNoteResponse](ctx, c.c, "/api/agents/write_note", req)
}

func (c *agentsClient) WriteMemory(ctx context.Context, req *AgentsWriteMemoryRequest) (*AgentsWriteMemoryResponse, error) {
	return Invoke[AgentsWriteMemoryRequest, AgentsWriteMemoryResponse](ctx, c.c, "/api/agents/write_memory", req)
}
func (c *agentsClient) AppendJournal(ctx context.Context, req *AgentsAppendJournalRequest) (*AgentsAppendJournalResponse, error) {
	return Invoke[AgentsAppendJournalRequest, AgentsAppendJournalResponse](ctx, c.c, "/api/agents/append_journal", req)
}
func (c *agentsClient) Knowledge(ctx context.Context, req *AgentsKnowledgeRequest) (*AgentsKnowledgeResponse, error) {
	return Invoke[AgentsKnowledgeRequest, AgentsKnowledgeResponse](ctx, c.c, "/api/agents/knowledge", req)
}
func (c *agentsClient) SetKnowledge(ctx context.Context, req *AgentsSetKnowledgeRequest) (*AgentsKnowledgeResponse, error) {
	return Invoke[AgentsSetKnowledgeRequest, AgentsKnowledgeResponse](ctx, c.c, "/api/agents/set_knowledge", req)
}
func (c *systemClient) Usage(ctx context.Context, req *SystemUsageRequest) (*SystemUsageResponse, error) {
	return Invoke[SystemUsageRequest, SystemUsageResponse](ctx, c.c, "/api/system/usage", req)
}
func (c *systemClient) Preflight(ctx context.Context, req *SystemPreflightRequest) (*SystemPreflightResponse, error) {
	return Invoke[SystemPreflightRequest, SystemPreflightResponse](ctx, c.c, "/api/system/preflight", req)
}
func (c *systemClient) Backups(ctx context.Context, req *SystemBackupsRequest) (*SystemBackupsResponse, error) {
	return Invoke[SystemBackupsRequest, SystemBackupsResponse](ctx, c.c, "/api/system/backups", req)
}
func (c *systemClient) BackupSweep(ctx context.Context, req *SystemBackupSweepRequest) (*SystemBackupSweepResponse, error) {
	return Invoke[SystemBackupSweepRequest, SystemBackupSweepResponse](ctx, c.c, "/api/system/backup_sweep", req)
}

func (c *systemClient) UpdateStatus(ctx context.Context, req *UpdateStatusRequest) (*UpdateInfo, error) {
	return Invoke[UpdateStatusRequest, UpdateInfo](ctx, c.c, "/api/system/update_status", req)
}

func (c *systemClient) UpdateCheck(ctx context.Context, req *UpdateCheckRequest) (*UpdateInfo, error) {
	return Invoke[UpdateCheckRequest, UpdateInfo](ctx, c.c, "/api/system/update_check", req)
}

func (c *systemClient) UpdateApply(ctx context.Context, req *UpdateApplyRequest) (*UpdateInfo, error) {
	return Invoke[UpdateApplyRequest, UpdateInfo](ctx, c.c, "/api/system/update_apply", req)
}

func (c *systemClient) UpdateDecide(ctx context.Context, req *UpdateDecideRequest) (*UpdateInfo, error) {
	return Invoke[UpdateDecideRequest, UpdateInfo](ctx, c.c, "/api/system/update_decide", req)
}

func (c *systemClient) UpdateRollback(ctx context.Context, req *UpdateRollbackRequest) (*UpdateInfo, error) {
	return Invoke[UpdateRollbackRequest, UpdateInfo](ctx, c.c, "/api/system/update_rollback", req)
}
func (c *systemClient) Report(ctx context.Context, req *SystemReportRequest) (*SystemReportResponse, error) {
	return Invoke[SystemReportRequest, SystemReportResponse](ctx, c.c, "/api/system/report", req)
}

type policyClient struct{ c *Client }

func NewPolicyClient(c *Client) PolicyAPI { return &policyClient{c: c} }
func (c *policyClient) Request(ctx context.Context, req *PolicyRequestRequest) (*PolicyRequestResponse, error) {
	return Invoke[PolicyRequestRequest, PolicyRequestResponse](ctx, c.c, "/api/policy/request", req)
}
func (c *policyClient) List(ctx context.Context, req *PolicyListRequest) (*PolicyListResponse, error) {
	return Invoke[PolicyListRequest, PolicyListResponse](ctx, c.c, "/api/policy/list", req)
}
func (c *policyClient) Get(ctx context.Context, req *PolicyGetRequest) (*PolicyGetResponse, error) {
	return Invoke[PolicyGetRequest, PolicyGetResponse](ctx, c.c, "/api/policy/get", req)
}
func (c *policyClient) Decide(ctx context.Context, req *PolicyDecideRequest) (*PolicyDecideResponse, error) {
	return Invoke[PolicyDecideRequest, PolicyDecideResponse](ctx, c.c, "/api/policy/decide", req)
}
func (c *policyClient) Revoke(ctx context.Context, req *PolicyRevokeRequest) (*PolicyRevokeResponse, error) {
	return Invoke[PolicyRevokeRequest, PolicyRevokeResponse](ctx, c.c, "/api/policy/revoke", req)
}

type packageClient struct{ c *Client }

func NewPackageClient(c *Client) PackageAPI { return &packageClient{c: c} }
func (c *packageClient) Hello(ctx context.Context, req *PackageHelloRequest) (*PackageHelloResponse, error) {
	return Invoke[PackageHelloRequest, PackageHelloResponse](ctx, c.c, "/api/package/hello", req)
}
func (c *packageClient) List(ctx context.Context, req *PackageListRequest) (*PackageListResponse, error) {
	return Invoke[PackageListRequest, PackageListResponse](ctx, c.c, "/api/package/list", req)
}
func (c *packageClient) Get(ctx context.Context, req *PackageGetRequest) (*PackageGetResponse, error) {
	return Invoke[PackageGetRequest, PackageGetResponse](ctx, c.c, "/api/package/get", req)
}
func (c *packageClient) Install(ctx context.Context, req *PackageInstallRequest) (*PackageInstallResponse, error) {
	return Invoke[PackageInstallRequest, PackageInstallResponse](ctx, c.c, "/api/package/install", req)
}
func (c *packageClient) Uninstall(ctx context.Context, req *PackageUninstallRequest) (*PackageUninstallResponse, error) {
	return Invoke[PackageUninstallRequest, PackageUninstallResponse](ctx, c.c, "/api/package/uninstall", req)
}
func (c *packageClient) Reload(ctx context.Context, req *PackageReloadRequest) (*PackageReloadResponse, error) {
	return Invoke[PackageReloadRequest, PackageReloadResponse](ctx, c.c, "/api/package/reload", req)
}
func (c *packageClient) SetEnabled(ctx context.Context, req *PackageSetEnabledRequest) (*PackageSetEnabledResponse, error) {
	return Invoke[PackageSetEnabledRequest, PackageSetEnabledResponse](ctx, c.c, "/api/package/set_enabled", req)
}
func (c *packageClient) New(ctx context.Context, req *PackageNewRequest) (*PackageNewResponse, error) {
	return Invoke[PackageNewRequest, PackageNewResponse](ctx, c.c, "/api/package/new", req)
}
func (c *packageClient) Fork(ctx context.Context, req *PackageForkRequest) (*PackageForkResponse, error) {
	return Invoke[PackageForkRequest, PackageForkResponse](ctx, c.c, "/api/package/fork", req)
}
func (c *packageClient) ListUpdates(ctx context.Context, req *PackageListUpdatesRequest) (*PackageListUpdatesResponse, error) {
	return Invoke[PackageListUpdatesRequest, PackageListUpdatesResponse](ctx, c.c, "/api/package/list_updates", req)
}
func (c *packageClient) ListExtensions(ctx context.Context, req *PackageListExtensionsRequest) (*PackageListExtensionsResponse, error) {
	return Invoke[PackageListExtensionsRequest, PackageListExtensionsResponse](ctx, c.c, "/api/package/list_extensions", req)
}
func (c *packageClient) SetExtension(ctx context.Context, req *PackageSetExtensionRequest) (*PackageSetExtensionResponse, error) {
	return Invoke[PackageSetExtensionRequest, PackageSetExtensionResponse](ctx, c.c, "/api/package/set_extension", req)
}
func (c *packageClient) InstallGo(ctx context.Context, req *PackageInstallGoRequest) (*PackageInstallGoResponse, error) {
	return Invoke[PackageInstallGoRequest, PackageInstallGoResponse](ctx, c.c, "/api/package/install_go", req)
}
func (c *packageClient) GoStatus(ctx context.Context, req *PackageGoStatusRequest) (*PackageGoStatusResponse, error) {
	return Invoke[PackageGoStatusRequest, PackageGoStatusResponse](ctx, c.c, "/api/package/go_status", req)
}
func (c *packageClient) Sync(ctx context.Context, req *PackageSyncRequest) (*PackageSyncResponse, error) {
	return Invoke[PackageSyncRequest, PackageSyncResponse](ctx, c.c, "/api/package/sync", req)
}
func (c *packageClient) Preview(ctx context.Context, req *PackagePreviewRequest) (*PackagePreviewResponse, error) {
	return Invoke[PackagePreviewRequest, PackagePreviewResponse](ctx, c.c, "/api/package/preview", req)
}
func (c *packageClient) Outdated(ctx context.Context, req *PackageOutdatedRequest) (*PackageOutdatedResponse, error) {
	return Invoke[PackageOutdatedRequest, PackageOutdatedResponse](ctx, c.c, "/api/package/outdated", req)
}
func (c *packageClient) Upgrade(ctx context.Context, req *PackageUpgradeRequest) (*PackageUpgradeResponse, error) {
	return Invoke[PackageUpgradeRequest, PackageUpgradeResponse](ctx, c.c, "/api/package/upgrade", req)
}
func (c *packageClient) Export(ctx context.Context, req *PackageExportRequest) (*PackageExportResponse, error) {
	return Invoke[PackageExportRequest, PackageExportResponse](ctx, c.c, "/api/package/export", req)
}

type personasClient struct{ c *Client }

func NewPersonasClient(c *Client) PersonasAPI { return &personasClient{c: c} }
func (c *personasClient) List(ctx context.Context, req *PersonasListRequest) (*PersonasListResponse, error) {
	return Invoke[PersonasListRequest, PersonasListResponse](ctx, c.c, "/api/personas/list", req)
}
func (c *personasClient) Get(ctx context.Context, req *PersonasGetRequest) (*PersonasGetResponse, error) {
	return Invoke[PersonasGetRequest, PersonasGetResponse](ctx, c.c, "/api/personas/get", req)
}
func (c *personasClient) Create(ctx context.Context, req *PersonasCreateRequest) (*PersonasCreateResponse, error) {
	return Invoke[PersonasCreateRequest, PersonasCreateResponse](ctx, c.c, "/api/personas/create", req)
}
func (c *personasClient) Copy(ctx context.Context, req *PersonasCopyRequest) (*PersonasCopyResponse, error) {
	return Invoke[PersonasCopyRequest, PersonasCopyResponse](ctx, c.c, "/api/personas/copy", req)
}
func (c *personasClient) Update(ctx context.Context, req *PersonasUpdateRequest) (*PersonasUpdateResponse, error) {
	return Invoke[PersonasUpdateRequest, PersonasUpdateResponse](ctx, c.c, "/api/personas/update", req)
}
func (c *personasClient) SetStatus(ctx context.Context, req *PersonasSetStatusRequest) (*PersonasSetStatusResponse, error) {
	return Invoke[PersonasSetStatusRequest, PersonasSetStatusResponse](ctx, c.c, "/api/personas/set_status", req)
}
func (c *personasClient) Check(ctx context.Context, req *PersonasCheckRequest) (*PersonasCheckResponse, error) {
	return Invoke[PersonasCheckRequest, PersonasCheckResponse](ctx, c.c, "/api/personas/check", req)
}
func (c *personasClient) GetStyle(ctx context.Context, req *PersonasGetStyleRequest) (*PersonasGetStyleResponse, error) {
	return Invoke[PersonasGetStyleRequest, PersonasGetStyleResponse](ctx, c.c, "/api/personas/get_style", req)
}
func (c *personasClient) SetStyle(ctx context.Context, req *PersonasSetStyleRequest) (*PersonasSetStyleResponse, error) {
	return Invoke[PersonasSetStyleRequest, PersonasSetStyleResponse](ctx, c.c, "/api/personas/set_style", req)
}
func (c *personasClient) Test(ctx context.Context, req *PersonasTestRequest) (*PersonasTestResponse, error) {
	return Invoke[PersonasTestRequest, PersonasTestResponse](ctx, c.c, "/api/personas/test", req)
}
func (c *personasClient) TestResults(ctx context.Context, req *PersonasTestResultsRequest) (*PersonasTestResultsResponse, error) {
	return Invoke[PersonasTestResultsRequest, PersonasTestResultsResponse](ctx, c.c, "/api/personas/test_results", req)
}
func (c *personasClient) PackStatus(ctx context.Context, req *PersonasPackStatusRequest) (*PersonasPackStatusResponse, error) {
	return Invoke[PersonasPackStatusRequest, PersonasPackStatusResponse](ctx, c.c, "/api/personas/pack_status", req)
}
func (c *personasClient) PackSync(ctx context.Context, req *PersonasPackSyncRequest) (*PersonasPackSyncResponse, error) {
	return Invoke[PersonasPackSyncRequest, PersonasPackSyncResponse](ctx, c.c, "/api/personas/pack_sync", req)
}
func (c *personasClient) Export(ctx context.Context, req *PersonasExportRequest) (*PersonasExportResponse, error) {
	return Invoke[PersonasExportRequest, PersonasExportResponse](ctx, c.c, "/api/personas/export", req)
}
func (c *personasClient) Import(ctx context.Context, req *PersonasImportRequest) (*PersonasImportResponse, error) {
	return Invoke[PersonasImportRequest, PersonasImportResponse](ctx, c.c, "/api/personas/import", req)
}

type rulesClient struct{ c *Client }

func NewRulesClient(c *Client) RulesAPI { return &rulesClient{c: c} }
func (c *rulesClient) List(ctx context.Context, req *RulesListRequest) (*RulesListResponse, error) {
	return Invoke[RulesListRequest, RulesListResponse](ctx, c.c, "/api/rules/list", req)
}
func (c *rulesClient) Get(ctx context.Context, req *RulesGetRequest) (*RulesGetResponse, error) {
	return Invoke[RulesGetRequest, RulesGetResponse](ctx, c.c, "/api/rules/get", req)
}
func (c *rulesClient) Save(ctx context.Context, req *RulesSaveRequest) (*RulesSaveResponse, error) {
	return Invoke[RulesSaveRequest, RulesSaveResponse](ctx, c.c, "/api/rules/save", req)
}
func (c *rulesClient) History(ctx context.Context, req *RulesHistoryRequest) (*RulesHistoryResponse, error) {
	return Invoke[RulesHistoryRequest, RulesHistoryResponse](ctx, c.c, "/api/rules/history", req)
}
func (c *rulesClient) Version(ctx context.Context, req *RulesVersionRequest) (*RulesVersionResponse, error) {
	return Invoke[RulesVersionRequest, RulesVersionResponse](ctx, c.c, "/api/rules/version", req)
}
func (c *rulesClient) Restore(ctx context.Context, req *RulesRestoreRequest) (*RulesRestoreResponse, error) {
	return Invoke[RulesRestoreRequest, RulesRestoreResponse](ctx, c.c, "/api/rules/restore", req)
}
func (c *rulesClient) Propose(ctx context.Context, req *RulesProposeRequest) (*RulesProposeResponse, error) {
	return Invoke[RulesProposeRequest, RulesProposeResponse](ctx, c.c, "/api/rules/propose", req)
}
func (c *rulesClient) Proposals(ctx context.Context, req *RulesProposalsRequest) (*RulesProposalsResponse, error) {
	return Invoke[RulesProposalsRequest, RulesProposalsResponse](ctx, c.c, "/api/rules/proposals", req)
}
func (c *rulesClient) Decide(ctx context.Context, req *RulesDecideRequest) (*RulesDecideResponse, error) {
	return Invoke[RulesDecideRequest, RulesDecideResponse](ctx, c.c, "/api/rules/decide", req)
}

type notesClient struct{ c *Client }

func NewNotesClient(c *Client) NotesAPI { return &notesClient{c: c} }
func (c *notesClient) Add(ctx context.Context, req *NotesAddRequest) (*NotesAddResponse, error) {
	return Invoke[NotesAddRequest, NotesAddResponse](ctx, c.c, "/api/notes/add", req)
}
func (c *notesClient) Set(ctx context.Context, req *NotesSetRequest) (*NotesSetResponse, error) {
	return Invoke[NotesSetRequest, NotesSetResponse](ctx, c.c, "/api/notes/set", req)
}
func (c *notesClient) Archive(ctx context.Context, req *NotesArchiveRequest) (*NotesArchiveResponse, error) {
	return Invoke[NotesArchiveRequest, NotesArchiveResponse](ctx, c.c, "/api/notes/archive", req)
}
func (c *notesClient) AddCategory(ctx context.Context, req *NotesAddCategoryRequest) (*NotesAddCategoryResponse, error) {
	return Invoke[NotesAddCategoryRequest, NotesAddCategoryResponse](ctx, c.c, "/api/notes/add_category", req)
}
func (c *notesClient) Quips(ctx context.Context, req *NotesQuipsRequest) (*NotesQuipsResponse, error) {
	return Invoke[NotesQuipsRequest, NotesQuipsResponse](ctx, c.c, "/api/notes/quips", req)
}
func (c *notesClient) QuipUsed(ctx context.Context, req *NotesQuipUsedRequest) (*NotesQuipUsedResponse, error) {
	return Invoke[NotesQuipUsedRequest, NotesQuipUsedResponse](ctx, c.c, "/api/notes/quip_used", req)
}

type backupClient struct{ c *Client }

// NewBackupClient：整個家的備份與還原（DEV-153）。
func NewBackupClient(c *Client) BackupAPI { return &backupClient{c: c} }
func (c *backupClient) GetStatus(ctx context.Context, req *BackupGetStatusRequest) (*BackupGetStatusResponse, error) {
	return Invoke[BackupGetStatusRequest, BackupGetStatusResponse](ctx, c.c, "/api/backup/get_status", req)
}
func (c *backupClient) Create(ctx context.Context, req *BackupCreateRequest) (*BackupCreateResponse, error) {
	return Invoke[BackupCreateRequest, BackupCreateResponse](ctx, c.c, "/api/backup/create", req)
}
func (c *backupClient) Preview(ctx context.Context, req *BackupPreviewRequest) (*BackupPreviewResponse, error) {
	return Invoke[BackupPreviewRequest, BackupPreviewResponse](ctx, c.c, "/api/backup/preview", req)
}
func (c *backupClient) Restore(ctx context.Context, req *BackupRestoreRequest) (*BackupRestoreResponse, error) {
	return Invoke[BackupRestoreRequest, BackupRestoreResponse](ctx, c.c, "/api/backup/restore", req)
}
