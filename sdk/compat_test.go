package sdk

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net/http"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
)

var publicTypes = map[string]reflect.Type{
	"InputSource":                   reflect.TypeOf(InputSource("")),
	"DirectMessage":                 reflect.TypeOf(DirectMessage{}),
	"EngineHostCheckActionRequest":  reflect.TypeOf(EngineHostCheckActionRequest{}),
	"EngineHostCheckActionResponse": reflect.TypeOf(EngineHostCheckActionResponse{}),
	"Goal":                          reflect.TypeOf(Goal{}),
	"PlanbookEntry":                 reflect.TypeOf(PlanbookEntry{}),
	"GoalsCreateRequest":            reflect.TypeOf(GoalsCreateRequest{}),
	"GoalsCreateResponse":           reflect.TypeOf(GoalsCreateResponse{}),
	"GoalsProposeRequest":           reflect.TypeOf(GoalsProposeRequest{}),
	"GoalsProposeResponse":          reflect.TypeOf(GoalsProposeResponse{}),
	"GoalsGetRequest":               reflect.TypeOf(GoalsGetRequest{}),
	"GoalsGetResponse":              reflect.TypeOf(GoalsGetResponse{}),
	"GoalsListRequest":              reflect.TypeOf(GoalsListRequest{}),
	"GoalsListResponse":             reflect.TypeOf(GoalsListResponse{}),
	"GoalsUpdatePlanbookRequest":    reflect.TypeOf(GoalsUpdatePlanbookRequest{}),
	"GoalsUpdatePlanbookResponse":   reflect.TypeOf(GoalsUpdatePlanbookResponse{}),
	"GoalsDoneRequest":              reflect.TypeOf(GoalsDoneRequest{}),
	"GoalsDoneResponse":             reflect.TypeOf(GoalsDoneResponse{}),
	"GoalsWithdrawRequest":          reflect.TypeOf(GoalsWithdrawRequest{}),
	"GoalsWithdrawResponse":         reflect.TypeOf(GoalsWithdrawResponse{}),
	"GoalState":                     reflect.TypeOf(GoalState("")),
	"ActionClass":                   reflect.TypeOf(ActionClass("")),
	"ActionVerdict":                 reflect.TypeOf(ActionVerdict("")),

	"EnvelopeRoute":                     reflect.TypeOf(EnvelopeRoute("")),
	"Client":                            reflect.TypeOf(Client{}),
	"TasksWaitingResponse":              reflect.TypeOf(TasksWaitingResponse{}),
	"TasksWaitingRequest":               reflect.TypeOf(TasksWaitingRequest{}),
	"TasksStaleResponse":                reflect.TypeOf(TasksStaleResponse{}),
	"TasksStaleRequest":                 reflect.TypeOf(TasksStaleRequest{}),
	"TasksRejectResponse":               reflect.TypeOf(TasksRejectResponse{}),
	"TasksRejectRequest":                reflect.TypeOf(TasksRejectRequest{}),
	"TasksAcceptResponse":               reflect.TypeOf(TasksAcceptResponse{}),
	"TasksAcceptRequest":                reflect.TypeOf(TasksAcceptRequest{}),
	"TasksArchiveRequest":               reflect.TypeOf(TasksArchiveRequest{}),
	"TasksArchiveResponse":              reflect.TypeOf(TasksArchiveResponse{}),
	"TasksSubmitResponse":               reflect.TypeOf(TasksSubmitResponse{}),
	"TasksSubmitRequest":                reflect.TypeOf(TasksSubmitRequest{}),
	"Proposed":                          reflect.TypeOf(Proposed("")),
	"WaitingTask":                       reflect.TypeOf(WaitingTask{}),
	"MenuItem":                          reflect.TypeOf(MenuItem{}),
	"MenuEntry":                         reflect.TypeOf(MenuEntry{}),
	"Menu":                              reflect.TypeOf(Menu{}),
	"WebGetMenuRequest":                 reflect.TypeOf(WebGetMenuRequest{}),
	"WebGetMenuResponse":                reflect.TypeOf(WebGetMenuResponse{}),
	"PageSlot":                          reflect.TypeOf(PageSlot{}),
	"PageInfo":                          reflect.TypeOf(PageInfo{}),
	"WebGetPagesRequest":                reflect.TypeOf(WebGetPagesRequest{}),
	"WebGetPagesResponse":               reflect.TypeOf(WebGetPagesResponse{}),
	"MenuPrefs":                         reflect.TypeOf(MenuPrefs{}),
	"MenuPrefEntry":                     reflect.TypeOf(MenuPrefEntry{}),
	"WebGetMenuPrefsRequest":            reflect.TypeOf(WebGetMenuPrefsRequest{}),
	"WebGetMenuPrefsResponse":           reflect.TypeOf(WebGetMenuPrefsResponse{}),
	"WebSetMenuPrefsRequest":            reflect.TypeOf(WebSetMenuPrefsRequest{}),
	"WebSetMenuPrefsResponse":           reflect.TypeOf(WebSetMenuPrefsResponse{}),
	"WebGetHealthRequest":               reflect.TypeOf(WebGetHealthRequest{}),
	"HealthItem":                        reflect.TypeOf(HealthItem{}),
	"WebGetHealthResponse":              reflect.TypeOf(WebGetHealthResponse{}),
	"AgentSetting":                      reflect.TypeOf(AgentSetting{}),
	"RosterAgent":                       reflect.TypeOf(RosterAgent{}),
	"WebGetRosterRequest":               reflect.TypeOf(WebGetRosterRequest{}),
	"WebGetRosterResponse":              reflect.TypeOf(WebGetRosterResponse{}),
	"WebGetOverviewRequest":             reflect.TypeOf(WebGetOverviewRequest{}),
	"WebGetOverviewResponse":            reflect.TypeOf(WebGetOverviewResponse{}),
	"Task":                              reflect.TypeOf(Task{}),
	"TasksCreateRequest":                reflect.TypeOf(TasksCreateRequest{}),
	"TasksCreateResponse":               reflect.TypeOf(TasksCreateResponse{}),
	"TasksGetRequest":                   reflect.TypeOf(TasksGetRequest{}),
	"TasksGetResponse":                  reflect.TypeOf(TasksGetResponse{}),
	"TasksListRequest":                  reflect.TypeOf(TasksListRequest{}),
	"TasksListResponse":                 reflect.TypeOf(TasksListResponse{}),
	"TasksUpdateRequest":                reflect.TypeOf(TasksUpdateRequest{}),
	"TasksUpdateResponse":               reflect.TypeOf(TasksUpdateResponse{}),
	"TasksCloseRequest":                 reflect.TypeOf(TasksCloseRequest{}),
	"TasksCloseResponse":                reflect.TypeOf(TasksCloseResponse{}),
	"TasksReconcileRequest":             reflect.TypeOf(TasksReconcileRequest{}),
	"TasksReconcileResponse":            reflect.TypeOf(TasksReconcileResponse{}),
	"Reminder":                          reflect.TypeOf(Reminder{}),
	"RemindersCreateRequest":            reflect.TypeOf(RemindersCreateRequest{}),
	"RemindersCreateResponse":           reflect.TypeOf(RemindersCreateResponse{}),
	"RemindersGetRequest":               reflect.TypeOf(RemindersGetRequest{}),
	"RemindersGetResponse":              reflect.TypeOf(RemindersGetResponse{}),
	"RemindersListRequest":              reflect.TypeOf(RemindersListRequest{}),
	"RemindersListResponse":             reflect.TypeOf(RemindersListResponse{}),
	"RemindersUpdateRequest":            reflect.TypeOf(RemindersUpdateRequest{}),
	"RemindersUpdateResponse":           reflect.TypeOf(RemindersUpdateResponse{}),
	"RemindersSnoozeRequest":            reflect.TypeOf(RemindersSnoozeRequest{}),
	"RemindersSnoozeResponse":           reflect.TypeOf(RemindersSnoozeResponse{}),
	"RemindersDoneRequest":              reflect.TypeOf(RemindersDoneRequest{}),
	"RemindersDoneResponse":             reflect.TypeOf(RemindersDoneResponse{}),
	"RemindersDoingRequest":             reflect.TypeOf(RemindersDoingRequest{}),
	"RemindersDoingResponse":            reflect.TypeOf(RemindersDoingResponse{}),
	"RemindersCancelRequest":            reflect.TypeOf(RemindersCancelRequest{}),
	"RemindersCancelResponse":           reflect.TypeOf(RemindersCancelResponse{}),
	"Delivery":                          reflect.TypeOf(Delivery{}),
	"Answer":                            reflect.TypeOf(Answer{}),
	"Notification":                      reflect.TypeOf(Notification{}),
	"NotificationLink":                  reflect.TypeOf(NotificationLink{}),
	"NotifySendRequest":                 reflect.TypeOf(NotifySendRequest{}),
	"NotifySendResponse":                reflect.TypeOf(NotifySendResponse{}),
	"NotifyGetRequest":                  reflect.TypeOf(NotifyGetRequest{}),
	"NotifyGetResponse":                 reflect.TypeOf(NotifyGetResponse{}),
	"NotifyListRequest":                 reflect.TypeOf(NotifyListRequest{}),
	"NotifyListResponse":                reflect.TypeOf(NotifyListResponse{}),
	"NotifyMarkReadRequest":             reflect.TypeOf(NotifyMarkReadRequest{}),
	"NotifyMarkReadResponse":            reflect.TypeOf(NotifyMarkReadResponse{}),
	"NotifyArchiveRequest":              reflect.TypeOf(NotifyArchiveRequest{}),
	"NotifyArchiveResponse":             reflect.TypeOf(NotifyArchiveResponse{}),
	"NotifyAnswerRequest":               reflect.TypeOf(NotifyAnswerRequest{}),
	"NotifyAnswerResponse":              reflect.TypeOf(NotifyAnswerResponse{}),
	"CategoryPref":                      reflect.TypeOf(CategoryPref{}),
	"AgentPref":                         reflect.TypeOf(AgentPref{}),
	"NotifyPrefs":                       reflect.TypeOf(NotifyPrefs{}),
	"TaskFollowPrefs":                   reflect.TypeOf(TaskFollowPrefs{}),
	"NotifyGetPrefsRequest":             reflect.TypeOf(NotifyGetPrefsRequest{}),
	"NotifyGetPrefsResponse":            reflect.TypeOf(NotifyGetPrefsResponse{}),
	"NotifySetPrefsRequest":             reflect.TypeOf(NotifySetPrefsRequest{}),
	"NotifySetPrefsResponse":            reflect.TypeOf(NotifySetPrefsResponse{}),
	"NotifyWatchRequest":                reflect.TypeOf(NotifyWatchRequest{}),
	"NotifyWatchResponse":               reflect.TypeOf(NotifyWatchResponse{}),
	"ShareRequest":                      reflect.TypeOf(ShareRequest{}),
	"ShareResponse":                     reflect.TypeOf(ShareResponse{}),
	"NotifyPushKeyRequest":              reflect.TypeOf(NotifyPushKeyRequest{}),
	"NotifyPushKeyResponse":             reflect.TypeOf(NotifyPushKeyResponse{}),
	"NotifySubscribePushRequest":        reflect.TypeOf(NotifySubscribePushRequest{}),
	"NotifySubscribePushResponse":       reflect.TypeOf(NotifySubscribePushResponse{}),
	"NotifyUnsubscribePushRequest":      reflect.TypeOf(NotifyUnsubscribePushRequest{}),
	"NotifyUnsubscribePushResponse":     reflect.TypeOf(NotifyUnsubscribePushResponse{}),
	"NotifyPushTestRequest":             reflect.TypeOf(NotifyPushTestRequest{}),
	"NotifyPushTestResponse":            reflect.TypeOf(NotifyPushTestResponse{}),
	"Nudge":                             reflect.TypeOf(Nudge{}),
	"NotifyNudgeRequest":                reflect.TypeOf(NotifyNudgeRequest{}),
	"NotifyNudgeResponse":               reflect.TypeOf(NotifyNudgeResponse{}),
	"NotifyPresenceRequest":             reflect.TypeOf(NotifyPresenceRequest{}),
	"NotifyPresenceResponse":            reflect.TypeOf(NotifyPresenceResponse{}),
	"NotifyResolveNudgeRequest":         reflect.TypeOf(NotifyResolveNudgeRequest{}),
	"NotifyResolveNudgeResponse":        reflect.TypeOf(NotifyResolveNudgeResponse{}),
	"DoctorGroup":                       reflect.TypeOf(DoctorGroup("")),
	"DoctorSeverity":                    reflect.TypeOf(DoctorSeverity("")),
	"DoctorCheck":                       reflect.TypeOf(DoctorCheck{}),
	"SystemDoctorRequest":               reflect.TypeOf(SystemDoctorRequest{}),
	"SystemDoctorResponse":              reflect.TypeOf(SystemDoctorResponse{}),
	"UsageWindow":                       reflect.TypeOf(UsageWindow{}),
	"EngineAskRequest":                  reflect.TypeOf(EngineAskRequest{}),
	"EngineAskResponse":                 reflect.TypeOf(EngineAskResponse{}),
	"EnginePreflightRequest":            reflect.TypeOf(EnginePreflightRequest{}),
	"EnginePreflightResponse":           reflect.TypeOf(EnginePreflightResponse{}),
	"PreflightCheck":                    reflect.TypeOf(PreflightCheck{}),
	"AccountUsage":                      reflect.TypeOf(AccountUsage{}),
	"SystemUsageRequest":                reflect.TypeOf(SystemUsageRequest{}),
	"SystemUsageResponse":               reflect.TypeOf(SystemUsageResponse{}),
	"SystemPreflightRequest":            reflect.TypeOf(SystemPreflightRequest{}),
	"SystemPreflightResponse":           reflect.TypeOf(SystemPreflightResponse{}),
	"BackupFile":                        reflect.TypeOf(BackupFile{}),
	"SystemBackupsRequest":              reflect.TypeOf(SystemBackupsRequest{}),
	"SystemBackupsResponse":             reflect.TypeOf(SystemBackupsResponse{}),
	"BackupSweepItem":                   reflect.TypeOf(BackupSweepItem{}),
	"BackupSweepReport":                 reflect.TypeOf(BackupSweepReport{}),
	"SystemBackupSweepRequest":          reflect.TypeOf(SystemBackupSweepRequest{}),
	"SystemBackupSweepResponse":         reflect.TypeOf(SystemBackupSweepResponse{}),
	"HomeBackupFile":                    reflect.TypeOf(HomeBackupFile{}),
	"HomeBackupSection":                 reflect.TypeOf(HomeBackupSection{}),
	"HomeRestoreResult":                 reflect.TypeOf(HomeRestoreResult{}),
	"BackupGetStatusRequest":            reflect.TypeOf(BackupGetStatusRequest{}),
	"BackupGetStatusResponse":           reflect.TypeOf(BackupGetStatusResponse{}),
	"BackupCreateRequest":               reflect.TypeOf(BackupCreateRequest{}),
	"BackupCreateResponse":              reflect.TypeOf(BackupCreateResponse{}),
	"BackupPreviewRequest":              reflect.TypeOf(BackupPreviewRequest{}),
	"BackupPreviewResponse":             reflect.TypeOf(BackupPreviewResponse{}),
	"BackupRestoreRequest":              reflect.TypeOf(BackupRestoreRequest{}),
	"BackupRestoreResponse":             reflect.TypeOf(BackupRestoreResponse{}),
	"UpdateState":                       reflect.TypeOf(UpdateState{}),
	"UpdateInfo":                        reflect.TypeOf(UpdateInfo{}),
	"UpdateStatusRequest":               reflect.TypeOf(UpdateStatusRequest{}),
	"UpdateCheckRequest":                reflect.TypeOf(UpdateCheckRequest{}),
	"UpdateApplyRequest":                reflect.TypeOf(UpdateApplyRequest{}),
	"UpdateDecideRequest":               reflect.TypeOf(UpdateDecideRequest{}),
	"UpdateRollbackRequest":             reflect.TypeOf(UpdateRollbackRequest{}),
	"SystemReportRequest":               reflect.TypeOf(SystemReportRequest{}),
	"SystemReportResponse":              reflect.TypeOf(SystemReportResponse{}),
	"Account":                           reflect.TypeOf(Account{}),
	"SystemAccountsRequest":             reflect.TypeOf(SystemAccountsRequest{}),
	"SystemAccountsResponse":            reflect.TypeOf(SystemAccountsResponse{}),
	"SystemSaveAccountRequest":          reflect.TypeOf(SystemSaveAccountRequest{}),
	"SystemSaveAccountResponse":         reflect.TypeOf(SystemSaveAccountResponse{}),
	"PolicyKind":                        reflect.TypeOf(PolicyKind("")),
	"PolicyStatus":                      reflect.TypeOf(PolicyStatus("")),
	"PolicyRule":                        reflect.TypeOf(PolicyRule{}),
	"PolicyRequest":                     reflect.TypeOf(PolicyRequest{}),
	"PolicyRequestRequest":              reflect.TypeOf(PolicyRequestRequest{}),
	"PolicyListRequest":                 reflect.TypeOf(PolicyListRequest{}),
	"PolicyGetRequest":                  reflect.TypeOf(PolicyGetRequest{}),
	"PolicyDecideRequest":               reflect.TypeOf(PolicyDecideRequest{}),
	"PolicyRevokeRequest":               reflect.TypeOf(PolicyRevokeRequest{}),
	"PolicyRequestResponse":             reflect.TypeOf(PolicyRequestResponse{}),
	"PolicyListResponse":                reflect.TypeOf(PolicyListResponse{}),
	"PolicyGetResponse":                 reflect.TypeOf(PolicyGetResponse{}),
	"PolicyDecideResponse":              reflect.TypeOf(PolicyDecideResponse{}),
	"PolicyRevokeResponse":              reflect.TypeOf(PolicyRevokeResponse{}),
	"RulesDoc":                          reflect.TypeOf(RulesDoc{}),
	"NotesAddRequest":                   reflect.TypeOf(NotesAddRequest{}),
	"NotesAddResponse":                  reflect.TypeOf(NotesAddResponse{}),
	"NotesSetRequest":                   reflect.TypeOf(NotesSetRequest{}),
	"NotesSetResponse":                  reflect.TypeOf(NotesSetResponse{}),
	"NotesArchiveRequest":               reflect.TypeOf(NotesArchiveRequest{}),
	"NotesArchiveResponse":              reflect.TypeOf(NotesArchiveResponse{}),
	"NotesAddCategoryRequest":           reflect.TypeOf(NotesAddCategoryRequest{}),
	"NotesAddCategoryResponse":          reflect.TypeOf(NotesAddCategoryResponse{}),
	"NotesQuipsRequest":                 reflect.TypeOf(NotesQuipsRequest{}),
	"NotesQuipsResponse":                reflect.TypeOf(NotesQuipsResponse{}),
	"NotesQuipUsedRequest":              reflect.TypeOf(NotesQuipUsedRequest{}),
	"NotesQuipUsedResponse":             reflect.TypeOf(NotesQuipUsedResponse{}),
	"RulesVersionSource":                reflect.TypeOf(RulesVersionSource("")),
	"RulesVersion":                      reflect.TypeOf(RulesVersion{}),
	"RulesDiffOp":                       reflect.TypeOf(RulesDiffOp("")),
	"RulesDiffLine":                     reflect.TypeOf(RulesDiffLine{}),
	"RulesProposalStatus":               reflect.TypeOf(RulesProposalStatus("")),
	"RulesProposal":                     reflect.TypeOf(RulesProposal{}),
	"RulesListRequest":                  reflect.TypeOf(RulesListRequest{}),
	"RulesListResponse":                 reflect.TypeOf(RulesListResponse{}),
	"RulesGetRequest":                   reflect.TypeOf(RulesGetRequest{}),
	"RulesGetResponse":                  reflect.TypeOf(RulesGetResponse{}),
	"RulesSaveRequest":                  reflect.TypeOf(RulesSaveRequest{}),
	"RulesSaveResponse":                 reflect.TypeOf(RulesSaveResponse{}),
	"RulesHistoryRequest":               reflect.TypeOf(RulesHistoryRequest{}),
	"RulesHistoryResponse":              reflect.TypeOf(RulesHistoryResponse{}),
	"RulesVersionRequest":               reflect.TypeOf(RulesVersionRequest{}),
	"RulesVersionResponse":              reflect.TypeOf(RulesVersionResponse{}),
	"RulesRestoreRequest":               reflect.TypeOf(RulesRestoreRequest{}),
	"RulesRestoreResponse":              reflect.TypeOf(RulesRestoreResponse{}),
	"RulesProposeRequest":               reflect.TypeOf(RulesProposeRequest{}),
	"RulesProposeResponse":              reflect.TypeOf(RulesProposeResponse{}),
	"RulesProposalsRequest":             reflect.TypeOf(RulesProposalsRequest{}),
	"RulesProposalsResponse":            reflect.TypeOf(RulesProposalsResponse{}),
	"RulesDecideRequest":                reflect.TypeOf(RulesDecideRequest{}),
	"RulesDecideResponse":               reflect.TypeOf(RulesDecideResponse{}),
	"SystemTrustRequest":                reflect.TypeOf(SystemTrustRequest{}),
	"SystemTrustResponse":               reflect.TypeOf(SystemTrustResponse{}),
	"SystemSetPermissionLevelsRequest":  reflect.TypeOf(SystemSetPermissionLevelsRequest{}),
	"SystemSetPermissionLevelsResponse": reflect.TypeOf(SystemSetPermissionLevelsResponse{}),
	"Job":                               reflect.TypeOf(Job{}),
	"JobResult":                         reflect.TypeOf(JobResult("")),
	"SchedulerListJobsRequest":          reflect.TypeOf(SchedulerListJobsRequest{}),
	"SchedulerListJobsResponse":         reflect.TypeOf(SchedulerListJobsResponse{}),
	"SchedulerSaveJobRequest":           reflect.TypeOf(SchedulerSaveJobRequest{}),
	"SchedulerSaveJobResponse":          reflect.TypeOf(SchedulerSaveJobResponse{}),
	"SchedulerSetJobRequest":            reflect.TypeOf(SchedulerSetJobRequest{}),
	"SchedulerSetJobResponse":           reflect.TypeOf(SchedulerSetJobResponse{}),
	"SchedulerRemoveJobRequest":         reflect.TypeOf(SchedulerRemoveJobRequest{}),
	"SchedulerRemoveJobResponse":        reflect.TypeOf(SchedulerRemoveJobResponse{}),
	"SchedulerRunJobRequest":            reflect.TypeOf(SchedulerRunJobRequest{}),
	"SchedulerRunJobResponse":           reflect.TypeOf(SchedulerRunJobResponse{}),
	"SystemGetConfigRequest":            reflect.TypeOf(SystemGetConfigRequest{}),
	"SystemGetConfigResponse":           reflect.TypeOf(SystemGetConfigResponse{}),
	"SystemGetModeRequest":              reflect.TypeOf(SystemGetModeRequest{}),
	"SystemGetModeResponse":             reflect.TypeOf(SystemGetModeResponse{}),
	"SystemSetModeRequest":              reflect.TypeOf(SystemSetModeRequest{}),
	"SystemSetChatDisplayRequest":       reflect.TypeOf(SystemSetChatDisplayRequest{}),
	"SystemSetChatDisplayResponse":      reflect.TypeOf(SystemSetChatDisplayResponse{}),
	"SystemGetWorkMaxRequest":           reflect.TypeOf(SystemGetWorkMaxRequest{}),
	"SystemGetWorkMaxResponse":          reflect.TypeOf(SystemGetWorkMaxResponse{}),
	"SystemSetWorkMaxRequest":           reflect.TypeOf(SystemSetWorkMaxRequest{}),
	"SystemSetWorkMaxResponse":          reflect.TypeOf(SystemSetWorkMaxResponse{}),
	"SystemSetModeResponse":             reflect.TypeOf(SystemSetModeResponse{}),
	"SystemSetConfigRequest":            reflect.TypeOf(SystemSetConfigRequest{}),
	"SystemSetConfigResponse":           reflect.TypeOf(SystemSetConfigResponse{}),
	"AgentsCreateRequest":               reflect.TypeOf(AgentsCreateRequest{}),
	"AgentsCreateResponse":              reflect.TypeOf(AgentsCreateResponse{}),
	"AgentsArchiveRequest":              reflect.TypeOf(AgentsArchiveRequest{}),
	"AgentsArchiveResponse":             reflect.TypeOf(AgentsArchiveResponse{}),
	"AgentsRenameRequest":               reflect.TypeOf(AgentsRenameRequest{}),
	"AgentsRenameResponse":              reflect.TypeOf(AgentsRenameResponse{}),
	"AgentsAssignRequest":               reflect.TypeOf(AgentsAssignRequest{}),
	"AgentsAssignResponse":              reflect.TypeOf(AgentsAssignResponse{}),
	"AgentsSwapRequest":                 reflect.TypeOf(AgentsSwapRequest{}),
	"AgentsSwapResponse":                reflect.TypeOf(AgentsSwapResponse{}),
	"AgentsSetAccountRequest":           reflect.TypeOf(AgentsSetAccountRequest{}),
	"AgentsSetAccountResponse":          reflect.TypeOf(AgentsSetAccountResponse{}),
	"AgentsSetFieldRequest":             reflect.TypeOf(AgentsSetFieldRequest{}),
	"AgentsSetFieldResponse":            reflect.TypeOf(AgentsSetFieldResponse{}),
	"AgentCard":                         reflect.TypeOf(AgentCard{}),
	"AgentCardFile":                     reflect.TypeOf(AgentCardFile{}),
	"AgentLinkAccount":                  reflect.TypeOf(AgentLinkAccount{}),
	"AgentsLinkRequest":                 reflect.TypeOf(AgentsLinkRequest{}),
	"AgentsLinkResponse":                reflect.TypeOf(AgentsLinkResponse{}),
	"AgentsAddRuleRequest":              reflect.TypeOf(AgentsAddRuleRequest{}),
	"AgentsAddRuleResponse":             reflect.TypeOf(AgentsAddRuleResponse{}),
	"AgentsWriteNoteRequest":            reflect.TypeOf(AgentsWriteNoteRequest{}),
	"AgentsWriteNoteResponse":           reflect.TypeOf(AgentsWriteNoteResponse{}),
	"AgentsWriteMemoryRequest":          reflect.TypeOf(AgentsWriteMemoryRequest{}),
	"AgentsWriteMemoryResponse":         reflect.TypeOf(AgentsWriteMemoryResponse{}),
	"AgentsAppendJournalRequest":        reflect.TypeOf(AgentsAppendJournalRequest{}),
	"AgentsAppendJournalResponse":       reflect.TypeOf(AgentsAppendJournalResponse{}),
	"AgentKnowledgeBlock":               reflect.TypeOf(AgentKnowledgeBlock{}),
	"AgentsKnowledgeRequest":            reflect.TypeOf(AgentsKnowledgeRequest{}),
	"AgentsKnowledgeResponse":           reflect.TypeOf(AgentsKnowledgeResponse{}),
	"AgentsSetKnowledgeRequest":         reflect.TypeOf(AgentsSetKnowledgeRequest{}),
	"AgentsCustomizationsRequest":       reflect.TypeOf(AgentsCustomizationsRequest{}),
	"AgentsCustomizationsResponse":      reflect.TypeOf(AgentsCustomizationsResponse{}),
	"EngineWriteAgentCardsRequest":      reflect.TypeOf(EngineWriteAgentCardsRequest{}),
	"EngineWriteAgentCardsResponse":     reflect.TypeOf(EngineWriteAgentCardsResponse{}),
	"AgentPermission":                   reflect.TypeOf(AgentPermission{}),
	"PermissionSource":                  reflect.TypeOf(PermissionSource{}),
	"DeniedAction":                      reflect.TypeOf(DeniedAction{}),
	"PersonaCard":                       reflect.TypeOf(PersonaCard{}),
	"PersonaStyleLevel":                 reflect.TypeOf(PersonaStyleLevel{}),
	"PersonasListRequest":               reflect.TypeOf(PersonasListRequest{}),
	"PersonasListResponse":              reflect.TypeOf(PersonasListResponse{}),
	"PersonasGetRequest":                reflect.TypeOf(PersonasGetRequest{}),
	"PersonasGetResponse":               reflect.TypeOf(PersonasGetResponse{}),
	"PersonasCreateRequest":             reflect.TypeOf(PersonasCreateRequest{}),
	"PersonasCreateResponse":            reflect.TypeOf(PersonasCreateResponse{}),
	"PersonasCopyRequest":               reflect.TypeOf(PersonasCopyRequest{}),
	"PersonasCopyResponse":              reflect.TypeOf(PersonasCopyResponse{}),
	"PersonasUpdateRequest":             reflect.TypeOf(PersonasUpdateRequest{}),
	"PersonasUpdateResponse":            reflect.TypeOf(PersonasUpdateResponse{}),
	"PersonasSetStatusRequest":          reflect.TypeOf(PersonasSetStatusRequest{}),
	"PersonasSetStatusResponse":         reflect.TypeOf(PersonasSetStatusResponse{}),
	"PersonasCheckRequest":              reflect.TypeOf(PersonasCheckRequest{}),
	"PersonasCheckResponse":             reflect.TypeOf(PersonasCheckResponse{}),
	"PersonasGetStyleRequest":           reflect.TypeOf(PersonasGetStyleRequest{}),
	"PersonasGetStyleResponse":          reflect.TypeOf(PersonasGetStyleResponse{}),
	"PersonasSetStyleRequest":           reflect.TypeOf(PersonasSetStyleRequest{}),
	"PersonasSetStyleResponse":          reflect.TypeOf(PersonasSetStyleResponse{}),
	"PersonasTestRequest":               reflect.TypeOf(PersonasTestRequest{}),
	"PersonasTestResponse":              reflect.TypeOf(PersonasTestResponse{}),
	"PersonasTestResultsRequest":        reflect.TypeOf(PersonasTestResultsRequest{}),
	"PersonasTestResultsResponse":       reflect.TypeOf(PersonasTestResultsResponse{}),
	"PersonaTestRun":                    reflect.TypeOf(PersonaTestRun{}),
	"WebGetAgentsRequest":               reflect.TypeOf(WebGetAgentsRequest{}),
	"WebGetAgentsResponse":              reflect.TypeOf(WebGetAgentsResponse{}),
	"WebSite":                           reflect.TypeOf(WebSite{}),
	"WebGetSitesRequest":                reflect.TypeOf(WebGetSitesRequest{}),
	"WebGetSitesResponse":               reflect.TypeOf(WebGetSitesResponse{}),
	"WebSetSitesRequest":                reflect.TypeOf(WebSetSitesRequest{}),
	"WebSetSitesResponse":               reflect.TypeOf(WebSetSitesResponse{}),
	"WebSetAllowBoardsRequest":          reflect.TypeOf(WebSetAllowBoardsRequest{}),
	"WebSetAllowBoardsResponse":         reflect.TypeOf(WebSetAllowBoardsResponse{}),
	"WebBootPrompt":                     reflect.TypeOf(WebBootPrompt{}),
	"WebGetBootPromptsRequest":          reflect.TypeOf(WebGetBootPromptsRequest{}),
	"WebGetBootPromptsResponse":         reflect.TypeOf(WebGetBootPromptsResponse{}),
	"WebAnswerBootPromptRequest":        reflect.TypeOf(WebAnswerBootPromptRequest{}),
	"WebAnswerBootPromptResponse":       reflect.TypeOf(WebAnswerBootPromptResponse{}),
	"WebVoiceSendRequest":               reflect.TypeOf(WebVoiceSendRequest{}),
	"WebVoiceSendResponse":              reflect.TypeOf(WebVoiceSendResponse{}),
	"WebVoiceAnswerRequest":             reflect.TypeOf(WebVoiceAnswerRequest{}),
	"WebVoiceAnswerResponse":            reflect.TypeOf(WebVoiceAnswerResponse{}),
	"WebSetOwnerLoginRequest":           reflect.TypeOf(WebSetOwnerLoginRequest{}),
	"WebSetOwnerLoginResponse":          reflect.TypeOf(WebSetOwnerLoginResponse{}),
	"WebPhone":                          reflect.TypeOf(WebPhone{}),
	"WebGetPhoneRequest":                reflect.TypeOf(WebGetPhoneRequest{}),
	"WebGetPhoneResponse":               reflect.TypeOf(WebGetPhoneResponse{}),
	"WebPhoneSetupRequest":              reflect.TypeOf(WebPhoneSetupRequest{}),
	"WebPhoneSetupResponse":             reflect.TypeOf(WebPhoneSetupResponse{}),
	"WebPhoneCloseFunnelRequest":        reflect.TypeOf(WebPhoneCloseFunnelRequest{}),
	"WebPhoneCloseFunnelResponse":       reflect.TypeOf(WebPhoneCloseFunnelResponse{}),
	"WebGetTrustRequest":                reflect.TypeOf(WebGetTrustRequest{}),
	"WebGetTrustResponse":               reflect.TypeOf(WebGetTrustResponse{}),
	"LifecycleStep":                     reflect.TypeOf(LifecycleStep{}),
	"Envelope":                          reflect.TypeOf(Envelope{}),
	"BusSendRequest":                    reflect.TypeOf(BusSendRequest{}),
	"BusSendResponse":                   reflect.TypeOf(BusSendResponse{}),
	"BusGetRequest":                     reflect.TypeOf(BusGetRequest{}),
	"BusGetResponse":                    reflect.TypeOf(BusGetResponse{}),
	"BusListRequest":                    reflect.TypeOf(BusListRequest{}),
	"BusListResponse":                   reflect.TypeOf(BusListResponse{}),
	"BusWatchRequest":                   reflect.TypeOf(BusWatchRequest{}),
	"BusWatchResponse":                  reflect.TypeOf(BusWatchResponse{}),
	"BusPublishRequest":                 reflect.TypeOf(BusPublishRequest{}),
	"BusPublishResponse":                reflect.TypeOf(BusPublishResponse{}),
	"BusSubscribeRequest":               reflect.TypeOf(BusSubscribeRequest{}),
	"BusSubscribeResponse":              reflect.TypeOf(BusSubscribeResponse{}),
	"EngineCapabilitiesRequest":         reflect.TypeOf(EngineCapabilitiesRequest{}),
	"EngineCapabilitiesResponse":        reflect.TypeOf(EngineCapabilitiesResponse{}),
	"EngineStartRequest":                reflect.TypeOf(EngineStartRequest{}),
	"EngineStartResponse":               reflect.TypeOf(EngineStartResponse{}),
	"EngineStopRequest":                 reflect.TypeOf(EngineStopRequest{}),
	"EngineStopResponse":                reflect.TypeOf(EngineStopResponse{}),
	"EngineDeliverRequest":              reflect.TypeOf(EngineDeliverRequest{}),
	"EngineDeliverResponse":             reflect.TypeOf(EngineDeliverResponse{}),
	"EngineTranscriptRequest":           reflect.TypeOf(EngineTranscriptRequest{}),
	"EngineTranscriptResponse":          reflect.TypeOf(EngineTranscriptResponse{}),
	"RepoPolicy":                        reflect.TypeOf(RepoPolicy{}),
	"PermissionPolicy":                  reflect.TypeOf(PermissionPolicy{}),
	"EngineSetPermissionRequest":        reflect.TypeOf(EngineSetPermissionRequest{}),
	"EngineSetPermissionResponse":       reflect.TypeOf(EngineSetPermissionResponse{}),
	"BootPrompt":                        reflect.TypeOf(BootPrompt{}),
	"EngineListBootPromptsRequest":      reflect.TypeOf(EngineListBootPromptsRequest{}),
	"EngineListBootPromptsResponse":     reflect.TypeOf(EngineListBootPromptsResponse{}),
	"EngineAnswerBootPromptRequest":     reflect.TypeOf(EngineAnswerBootPromptRequest{}),
	"EngineAnswerBootPromptResponse":    reflect.TypeOf(EngineAnswerBootPromptResponse{}),
	"EngineAnswerApprovalRequest":       reflect.TypeOf(EngineAnswerApprovalRequest{}),
	"EngineAnswerApprovalResponse":      reflect.TypeOf(EngineAnswerApprovalResponse{}),
	"EngineCommandRequest":              reflect.TypeOf(EngineCommandRequest{}),
	"EngineCommandResponse":             reflect.TypeOf(EngineCommandResponse{}),
	"EngineUsageRequest":                reflect.TypeOf(EngineUsageRequest{}),
	"EngineUsageResponse":               reflect.TypeOf(EngineUsageResponse{}),
	"EngineEvent":                       reflect.TypeOf(EngineEvent{}),
	"EngineHostReportEventRequest":      reflect.TypeOf(EngineHostReportEventRequest{}),
	"EngineHostReportEventResponse":     reflect.TypeOf(EngineHostReportEventResponse{}),
	"EngineHostRequestApprovalRequest":  reflect.TypeOf(EngineHostRequestApprovalRequest{}),
	"EngineHostRequestApprovalResponse": reflect.TypeOf(EngineHostRequestApprovalResponse{}),
	"EngineHostGetContextRequest":       reflect.TypeOf(EngineHostGetContextRequest{}),
	"EngineHostGetContextResponse":      reflect.TypeOf(EngineHostGetContextResponse{}),
	"QuestionOption":                    reflect.TypeOf(QuestionOption{}),
	"Question":                          reflect.TypeOf(Question{}),
	"Pending":                           reflect.TypeOf(Pending{}),
	"RemoteAccess":                      reflect.TypeOf(RemoteAccess{}),
	"Session":                           reflect.TypeOf(Session{}),
	"SessionListRequest":                reflect.TypeOf(SessionListRequest{}),
	"SessionListResponse":               reflect.TypeOf(SessionListResponse{}),
	"SessionGetRequest":                 reflect.TypeOf(SessionGetRequest{}),
	"SessionMessage":                    reflect.TypeOf(SessionMessage{}),
	"SessionGetResponse":                reflect.TypeOf(SessionGetResponse{}),
	"SessionOpenRequest":                reflect.TypeOf(SessionOpenRequest{}),
	"SessionOpenResponse":               reflect.TypeOf(SessionOpenResponse{}),
	"SessionCloseRequest":               reflect.TypeOf(SessionCloseRequest{}),
	"SessionCloseResponse":              reflect.TypeOf(SessionCloseResponse{}),
	"SessionRotateRequest":              reflect.TypeOf(SessionRotateRequest{}),
	"SessionRotateResponse":             reflect.TypeOf(SessionRotateResponse{}),
	"Decision":                          reflect.TypeOf(Decision{}),
	"SessionResolveRequest":             reflect.TypeOf(SessionResolveRequest{}),
	"SessionResolveResponse":            reflect.TypeOf(SessionResolveResponse{}),
	"ChatMessage":                       reflect.TypeOf(ChatMessage{}),
	"SessionChatRequest":                reflect.TypeOf(SessionChatRequest{}),
	"SessionChatResponse":               reflect.TypeOf(SessionChatResponse{}),
	"SessionWatchRequest":               reflect.TypeOf(SessionWatchRequest{}),
	"SessionWatchResponse":              reflect.TypeOf(SessionWatchResponse{}),
	"ThreadSummary":                     reflect.TypeOf(ThreadSummary{}),
	"SessionListThreadsRequest":         reflect.TypeOf(SessionListThreadsRequest{}),
	"SessionListThreadsResponse":        reflect.TypeOf(SessionListThreadsResponse{}),
	"SessionWatchThreadsRequest":        reflect.TypeOf(SessionWatchThreadsRequest{}),
	"SessionWatchThreadsResponse":       reflect.TypeOf(SessionWatchThreadsResponse{}),
	"SessionMarkSeenRequest":            reflect.TypeOf(SessionMarkSeenRequest{}),
	"SessionMarkSeenResponse":           reflect.TypeOf(SessionMarkSeenResponse{}),
	"SessionCommandRequest":             reflect.TypeOf(SessionCommandRequest{}),
	"SessionCommandResponse":            reflect.TypeOf(SessionCommandResponse{}),
	"SessionCommandNote":                reflect.TypeOf(SessionCommandNote{}),
	"MessageWait":                       reflect.TypeOf(MessageWait("")),
	"Record":                            reflect.TypeOf(Record{}),
	"VaultGetRequest":                   reflect.TypeOf(VaultGetRequest{}),
	"VaultGetResponse":                  reflect.TypeOf(VaultGetResponse{}),
	"VaultListRequest":                  reflect.TypeOf(VaultListRequest{}),
	"VaultListResponse":                 reflect.TypeOf(VaultListResponse{}),
	"VaultPutRequest":                   reflect.TypeOf(VaultPutRequest{}),
	"VaultPutResponse":                  reflect.TypeOf(VaultPutResponse{}),
	"VaultPatchRequest":                 reflect.TypeOf(VaultPatchRequest{}),
	"VaultPatchResponse":                reflect.TypeOf(VaultPatchResponse{}),
	"VaultAppendRequest":                reflect.TypeOf(VaultAppendRequest{}),
	"VaultAppendResponse":               reflect.TypeOf(VaultAppendResponse{}),
	"VaultArchiveRequest":               reflect.TypeOf(VaultArchiveRequest{}),
	"VaultArchiveResponse":              reflect.TypeOf(VaultArchiveResponse{}),
	"VaultNextIdRequest":                reflect.TypeOf(VaultNextIdRequest{}),
	"VaultNextIdResponse":               reflect.TypeOf(VaultNextIdResponse{}),
	"ChangeEntry":                       reflect.TypeOf(ChangeEntry{}),
	"VaultChangesRequest":               reflect.TypeOf(VaultChangesRequest{}),
	"VaultChangesResponse":              reflect.TypeOf(VaultChangesResponse{}),
	"VaultWatchRequest":                 reflect.TypeOf(VaultWatchRequest{}),
	"VaultWatchResponse":                reflect.TypeOf(VaultWatchResponse{}),
	"Requirement":                       reflect.TypeOf(Requirement{}),
	"Capabilities":                      reflect.TypeOf(Capabilities{}),
	"MenuItemDecl":                      reflect.TypeOf(MenuItemDecl{}),
	"PageDecl":                          reflect.TypeOf(PageDecl{}),
	"ExtensionPoint":                    reflect.TypeOf(ExtensionPoint{}),
	"ForkInfo":                          reflect.TypeOf(ForkInfo{}),
	"Package":                           reflect.TypeOf(Package{}),
	"PackageHelloRequest":               reflect.TypeOf(PackageHelloRequest{}),
	"PackageHelloResponse":              reflect.TypeOf(PackageHelloResponse{}),
	"PackageListRequest":                reflect.TypeOf(PackageListRequest{}),
	"PackageListResponse":               reflect.TypeOf(PackageListResponse{}),
	"PackageGetRequest":                 reflect.TypeOf(PackageGetRequest{}),
	"PackageGetResponse":                reflect.TypeOf(PackageGetResponse{}),
	"PackageInstallRequest":             reflect.TypeOf(PackageInstallRequest{}),
	"PackageInstallResponse":            reflect.TypeOf(PackageInstallResponse{}),
	"PackageUninstallRequest":           reflect.TypeOf(PackageUninstallRequest{}),
	"PackageUninstallResponse":          reflect.TypeOf(PackageUninstallResponse{}),
	"PackageReloadRequest":              reflect.TypeOf(PackageReloadRequest{}),
	"PackageReloadResponse":             reflect.TypeOf(PackageReloadResponse{}),
	"PackageSetEnabledRequest":          reflect.TypeOf(PackageSetEnabledRequest{}),
	"PackageSetEnabledResponse":         reflect.TypeOf(PackageSetEnabledResponse{}),
	"PackageForkRequest":                reflect.TypeOf(PackageForkRequest{}),
	"PackageForkResponse":               reflect.TypeOf(PackageForkResponse{}),
	"UpdateItem":                        reflect.TypeOf(UpdateItem{}),
	"PackageListUpdatesRequest":         reflect.TypeOf(PackageListUpdatesRequest{}),
	"PackageListUpdatesResponse":        reflect.TypeOf(PackageListUpdatesResponse{}),
	"PackageListExtensionsRequest":      reflect.TypeOf(PackageListExtensionsRequest{}),
	"PackageListExtensionsResponse":     reflect.TypeOf(PackageListExtensionsResponse{}),
	"PackageSetExtensionRequest":        reflect.TypeOf(PackageSetExtensionRequest{}),
	"PackageSetExtensionResponse":       reflect.TypeOf(PackageSetExtensionResponse{}),
	"PackageInstallGoRequest":           reflect.TypeOf(PackageInstallGoRequest{}),
	"PackageInstallGoResponse":          reflect.TypeOf(PackageInstallGoResponse{}),
	"PackageGoStatusRequest":            reflect.TypeOf(PackageGoStatusRequest{}),
	"PackageGoStatusResponse":           reflect.TypeOf(PackageGoStatusResponse{}),
	"DataFieldDecl":                     reflect.TypeOf(DataFieldDecl{}),
	"AgentDecl":                         reflect.TypeOf(AgentDecl{}),
	"DataKindDecl":                      reflect.TypeOf(DataKindDecl{}),
	"DataParentDecl":                    reflect.TypeOf(DataParentDecl{}),
	"PackageSyncRequest":                reflect.TypeOf(PackageSyncRequest{}),
	"SyncAgent":                         reflect.TypeOf(SyncAgent{}),
	"SyncJob":                           reflect.TypeOf(SyncJob{}),
	"PackageSyncResponse":               reflect.TypeOf(PackageSyncResponse{}),
	"PackageSource":                     reflect.TypeOf(PackageSource{}),
	"PackagePreviewRequest":             reflect.TypeOf(PackagePreviewRequest{}),
	"PackagePreviewResponse":            reflect.TypeOf(PackagePreviewResponse{}),
	"PackageNewRequest":                 reflect.TypeOf(PackageNewRequest{}),
	"PackageNewResponse":                reflect.TypeOf(PackageNewResponse{}),
	"PackageOutdatedRequest":            reflect.TypeOf(PackageOutdatedRequest{}),
	"PackageOutdated":                   reflect.TypeOf(PackageOutdated{}),
	"PackageOutdatedResponse":           reflect.TypeOf(PackageOutdatedResponse{}),
	"ClaimState":                        reflect.TypeOf(ClaimState("")),
	"PackageClaimNotifyRequest":         reflect.TypeOf(PackageClaimNotifyRequest{}),
	"PackageClaimNotifyResponse":        reflect.TypeOf(PackageClaimNotifyResponse{}),
	"PackageUpgradeRequest":             reflect.TypeOf(PackageUpgradeRequest{}),
	"PackageUpgradeResponse":            reflect.TypeOf(PackageUpgradeResponse{}),
	"PackagePreviewPersona":             reflect.TypeOf(PackagePreviewPersona{}),
	"PackageExportRequest":              reflect.TypeOf(PackageExportRequest{}),
	"PackageExportFinding":              reflect.TypeOf(PackageExportFinding{}),
	"PackageExportResponse":             reflect.TypeOf(PackageExportResponse{}),
	"PersonasPackStatusRequest":         reflect.TypeOf(PersonasPackStatusRequest{}),
	"PersonaPackPending":                reflect.TypeOf(PersonaPackPending{}),
	"PersonaPackLast":                   reflect.TypeOf(PersonaPackLast{}),
	"PersonasPackStatusResponse":        reflect.TypeOf(PersonasPackStatusResponse{}),
	"PersonasPackSyncRequest":           reflect.TypeOf(PersonasPackSyncRequest{}),
	"PersonasPackSyncResponse":          reflect.TypeOf(PersonasPackSyncResponse{}),
	"PersonasExportRequest":             reflect.TypeOf(PersonasExportRequest{}),
	"PersonasExportResponse":            reflect.TypeOf(PersonasExportResponse{}),
	"PersonasImportRequest":             reflect.TypeOf(PersonasImportRequest{}),
	"PersonasImportResponse":            reflect.TypeOf(PersonasImportResponse{}),
	"PackageHost":                       reflect.TypeOf(PackageHost{}),
	"Error":                             reflect.TypeOf(Error{}),
	"PageRequest":                       reflect.TypeOf(PageRequest{}),
	"PageResponse":                      reflect.TypeOf(PageResponse{}),
	"Actor":                             reflect.TypeOf(Actor{}),
	"TimeRange":                         reflect.TypeOf(TimeRange{}),
	"Route":                             reflect.TypeOf(Route{}),
	"TaskType":                          reflect.TypeOf(TaskType("")),
	"TaskStatus":                        reflect.TypeOf(TaskStatus("")),
	"Verify":                            reflect.TypeOf(Verify("")),
	"Follow":                            reflect.TypeOf(Follow("")),
	"Cycle":                             reflect.TypeOf(Cycle("")),
	"Difficulty":                        reflect.TypeOf(Difficulty("")),
	"ReminderStatus":                    reflect.TypeOf(ReminderStatus("")),
	"ReminderQuery":                     reflect.TypeOf(ReminderQuery("")),
	"Category":                          reflect.TypeOf(Category("")),
	"NudgeSurface":                      reflect.TypeOf(NudgeSurface("")),
	"DeliveryMode":                      reflect.TypeOf(DeliveryMode("")),
	"Channel":                           reflect.TypeOf(Channel("")),
	"NotificationState":                 reflect.TypeOf(NotificationState("")),
	"AnswerVia":                         reflect.TypeOf(AnswerVia("")),
	"EnvelopeKind":                      reflect.TypeOf(EnvelopeKind("")),
	"Lifecycle":                         reflect.TypeOf(Lifecycle("")),
	"PermissionMode":                    reflect.TypeOf(PermissionMode("")),
	"PermissionLevel":                   reflect.TypeOf(PermissionLevel("")),
	"UsageWindowKind":                   reflect.TypeOf(UsageWindowKind("")),
	"EngineEventType":                   reflect.TypeOf(EngineEventType("")),
	"EngineErrorKind":                   reflect.TypeOf(EngineErrorKind("")),
	"ApprovalKind":                      reflect.TypeOf(ApprovalKind("")),
	"SessionKind":                       reflect.TypeOf(SessionKind("")),
	"SessionState":                      reflect.TypeOf(SessionState("")),
	"SessionCommand":                    reflect.TypeOf(SessionCommand("")),
	"PendingKind":                       reflect.TypeOf(PendingKind("")),
	"ChatKind":                          reflect.TypeOf(ChatKind("")),
	"ThreadStatus":                      reflect.TypeOf(ThreadStatus("")),
	"WaitingReason":                     reflect.TypeOf(WaitingReason("")),
	"ChangeType":                        reflect.TypeOf(ChangeType("")),
	"HomeMode":                          reflect.TypeOf(HomeMode("")),
	"ChatDisplay":                       reflect.TypeOf(ChatDisplay("")),
	"HomeEnv":                           reflect.TypeOf(HomeEnv("")),
	"Tier":                              reflect.TypeOf(Tier("")),
	"PackageKind":                       reflect.TypeOf(PackageKind("")),
	"PackageState":                      reflect.TypeOf(PackageState("")),
	"PackageRemedy":                     reflect.TypeOf(PackageRemedy("")),
	"ExtensionKind":                     reflect.TypeOf(ExtensionKind("")),
	"ErrorCode":                         reflect.TypeOf(ErrorCode("")),
}
var publicEnums = map[string]string{
	"EnvPrefix":              EnvPrefix,
	"EnvState":               EnvState,
	"EnvMedia":               EnvMedia,
	"EnvOfficialPersonas":    EnvOfficialPersonas,
	"EnvBundledPersonas":     EnvBundledPersonas,
	"DownloadFixedDir":       DownloadFixedDir,
	"ProductName":            ProductName,
	"JobResultOk":            string(JobResultOk),
	"JobResultRunning":       string(JobResultRunning),
	"JobResultFired":         string(JobResultFired),
	"JobResultFailed":        string(JobResultFailed),
	"JobResultSkipped":       string(JobResultSkipped),
	"DoctorGroupConfig":      string(DoctorGroupConfig),
	"DoctorGroupSessions":    string(DoctorGroupSessions),
	"DoctorGroupPackages":    string(DoctorGroupPackages),
	"DoctorGroupPermissions": string(DoctorGroupPermissions),
	"DoctorGroupSchedule":    string(DoctorGroupSchedule),
	"DoctorGroupData":        string(DoctorGroupData),
	"DoctorGroupPush":        string(DoctorGroupPush),
	"DoctorSeverityOk":       string(DoctorSeverityOk),
	"DoctorSeverityWarn":     string(DoctorSeverityWarn),
	"DoctorSeverityError":    string(DoctorSeverityError),
	// 升級的種類與結果（DEV-155 S6）
	"UpdateKindUpgrade":       UpdateKindUpgrade,
	"UpdateKindRollback":      UpdateKindRollback,
	"UpdateKindRescue":        UpdateKindRescue,
	"UpdateOutcomeUntouched":  UpdateOutcomeUntouched,
	"UpdateOutcomeRolledBack": UpdateOutcomeRolledBack,
	"UpdateOutcomeStranded":   UpdateOutcomeStranded,
	"GoalStateDraft":          string(GoalStateDraft),
	"GoalStateActive":         string(GoalStateActive),
	"GoalStateDone":           string(GoalStateDone),
	"GoalStateWithdrawn":      string(GoalStateWithdrawn),
	"ActionClassRead":         string(ActionClassRead),
	"ActionClassWrite":        string(ActionClassWrite),
	"ActionClassCommit":       string(ActionClassCommit),
	"ActionClassPush":         string(ActionClassPush),
	"ActionClassDelete":       string(ActionClassDelete),
	"ActionClassSendExternal": string(ActionClassSendExternal),
	"ActionClassSpend":        string(ActionClassSpend),
	"ActionClassSettings":     string(ActionClassSettings),
	"ActionVerdictAllow":      string(ActionVerdictAllow),
	"ActionVerdictDeny":       string(ActionVerdictDeny),
	"ActionVerdictAsk":        string(ActionVerdictAsk),

	"EnvelopeRouteCore":              string(EnvelopeRouteCore),
	"EnvelopeRouteDirect":            string(EnvelopeRouteDirect),
	"EnvelopeKindEcho":               string(EnvelopeKindEcho),
	"LifecycleUnanswered":            string(LifecycleUnanswered),
	"LifecycleInterrupted":           string(LifecycleInterrupted),
	"TaskTypeTask":                   string(TaskTypeTask),
	"TaskTypeAsk":                    string(TaskTypeAsk),
	"TaskStatusIdea":                 string(TaskStatusIdea),
	"TaskStatusDoing":                string(TaskStatusDoing),
	"TaskStatusWaitingUser":          string(TaskStatusWaitingUser),
	"TaskStatusWaitingExternal":      string(TaskStatusWaitingExternal),
	"TaskStatusDone":                 string(TaskStatusDone),
	"TaskStatusCancelled":            string(TaskStatusCancelled),
	"VerifyVerified":                 string(VerifyVerified),
	"VerifyUnverifiable":             string(VerifyUnverifiable),
	"FollowNone":                     string(FollowNone),
	"FollowRemind":                   string(FollowRemind),
	"FollowConfirm":                  string(FollowConfirm),
	"FollowCheck":                    string(FollowCheck),
	"FollowDiscuss":                  string(FollowDiscuss),
	"CycleOnce":                      string(CycleOnce),
	"CycleDaily":                     string(CycleDaily),
	"CycleWeekly":                    string(CycleWeekly),
	"CycleBiweekly":                  string(CycleBiweekly),
	"CycleMonthly":                   string(CycleMonthly),
	"DifficultyEasy":                 string(DifficultyEasy),
	"DifficultyNormal":               string(DifficultyNormal),
	"DifficultyHard":                 string(DifficultyHard),
	"ReminderStatusTodo":             string(ReminderStatusTodo),
	"ReminderStatusDoing":            string(ReminderStatusDoing),
	"ReminderStatusAsking":           string(ReminderStatusAsking),
	"ReminderStatusShelved":          string(ReminderStatusShelved),
	"ReminderStatusDone":             string(ReminderStatusDone),
	"ReminderStatusCancelled":        string(ReminderStatusCancelled),
	"ReminderQueryToday":             string(ReminderQueryToday),
	"ReminderQueryOverdue":           string(ReminderQueryOverdue),
	"ReminderQueryThisWeek":          string(ReminderQueryThisWeek),
	"ReminderQueryWaiting":           string(ReminderQueryWaiting),
	"ReminderQueryNoTime":            string(ReminderQueryNoTime),
	"ReminderQueryDoneYesterday":     string(ReminderQueryDoneYesterday),
	"ReminderQueryUpcoming":          string(ReminderQueryUpcoming),
	"NotifySourceGoals":              NotifySourceGoals,
	"NotifySourcePolicy":             NotifySourcePolicy,
	"NotifySourcePackage":            NotifySourcePackage,
	"NotifyLaneAgent":                NotifyLaneAgent,
	"NotifyLaneSystem":               NotifyLaneSystem,
	"ClaimStateAsked":                string(ClaimStateAsked),
	"ClaimStateLater":                string(ClaimStateLater),
	"ClaimStateChange":               string(ClaimStateChange),
	"ClaimStateDone":                 string(ClaimStateDone),
	"ClaimStateFailed":               string(ClaimStateFailed),
	"NudgeSurfaceInline":             string(NudgeSurfaceInline),
	"NudgeSurfaceToast":              string(NudgeSurfaceToast),
	"NudgeSurfacePush":               string(NudgeSurfacePush),
	"NudgeSurfaceNone":               string(NudgeSurfaceNone),
	"CategoryApprove":                string(CategoryApprove),
	"CategoryDone":                   string(CategoryDone),
	"CategoryDue":                    string(CategoryDue),
	"CategoryDigest":                 string(CategoryDigest),
	"CategoryOpening":                string(CategoryOpening),
	"DeliveryModeInstant":            string(DeliveryModeInstant),
	"DeliveryModeDigest":             string(DeliveryModeDigest),
	"DeliveryModeBoard":              string(DeliveryModeBoard),
	"DeliveryModeOff":                string(DeliveryModeOff),
	"ChannelBoard":                   string(ChannelBoard),
	"ChannelApp":                     string(ChannelApp),
	"ChannelLine":                    string(ChannelLine),
	"NotificationStateUnread":        string(NotificationStateUnread),
	"NotificationStateRead":          string(NotificationStateRead),
	"NotificationStateAnswered":      string(NotificationStateAnswered),
	"NotificationStateExpired":       string(NotificationStateExpired),
	"NotificationStateFailed":        string(NotificationStateFailed),
	"AnswerViaButton":                string(AnswerViaButton),
	"AnswerViaChat":                  string(AnswerViaChat),
	"AnswerViaAPI":                   string(AnswerViaAPI),
	"AnswerViaAuto":                  string(AnswerViaAuto),
	"EnvelopeKindChat":               string(EnvelopeKindChat),
	"EnvelopeKindSay":                string(EnvelopeKindSay),
	"EnvelopeKindDelegate":           string(EnvelopeKindDelegate),
	"EnvelopeKindReport":             string(EnvelopeKindReport),
	"EnvelopeKindAnswer":             string(EnvelopeKindAnswer),
	"EnvelopeKindRoutine":            string(EnvelopeKindRoutine),
	"LifecycleQueued":                string(LifecycleQueued),
	"LifecycleNotified":              string(LifecycleNotified),
	"LifecycleReceived":              string(LifecycleReceived),
	"LifecycleWorking":               string(LifecycleWorking),
	"LifecycleReplied":               string(LifecycleReplied),
	"LifecycleBlocked":               string(LifecycleBlocked),
	"LifecycleFailed":                string(LifecycleFailed),
	"PermissionModeAsk":              string(PermissionModeAsk),
	"PermissionModeAuto":             string(PermissionModeAuto),
	"PolicyKindRule":                 string(PolicyKindRule),
	"PolicyKindGuidance":             string(PolicyKindGuidance),
	"PolicyStatusPending":            string(PolicyStatusPending),
	"PolicyStatusApplied":            string(PolicyStatusApplied),
	"PolicyStatusRejected":           string(PolicyStatusRejected),
	"PolicyStatusFailed":             string(PolicyStatusFailed),
	"PolicyStatusRevoked":            string(PolicyStatusRevoked),
	"PolicyStatusExpired":            string(PolicyStatusExpired),
	"RulesVersionSourceSave":         string(RulesVersionSourceSave),
	"RulesVersionSourceRestore":      string(RulesVersionSourceRestore),
	"RulesVersionSourceProposal":     string(RulesVersionSourceProposal),
	"RulesVersionSourceOutside":      string(RulesVersionSourceOutside),
	"RulesDiffOpSame":                string(RulesDiffOpSame),
	"RulesDiffOpAdd":                 string(RulesDiffOpAdd),
	"RulesDiffOpDel":                 string(RulesDiffOpDel),
	"RulesProposalStatusPending":     string(RulesProposalStatusPending),
	"RulesProposalStatusAccepted":    string(RulesProposalStatusAccepted),
	"RulesProposalStatusRejected":    string(RulesProposalStatusRejected),
	"UsageWindowKindShort":           string(UsageWindowKindShort),
	"UsageWindowKindWeekly":          string(UsageWindowKindWeekly),
	"PermissionLevelFull":            string(PermissionLevelFull),
	"PermissionLevelStandard":        string(PermissionLevelStandard),
	"PermissionLevelReadOnly":        string(PermissionLevelReadOnly),
	"EngineEventTypeSessionStarted":  string(EngineEventTypeSessionStarted),
	"EngineEventTypeSessionEnded":    string(EngineEventTypeSessionEnded),
	"InputSourceTyped":               string(InputSourceTyped),
	"InputSourcePeer":                string(InputSourcePeer),
	"InputSourceSystem":              string(InputSourceSystem),
	"EngineEventTypeTurnStarted":     string(EngineEventTypeTurnStarted),
	"EngineEventTypeTurnEnded":       string(EngineEventTypeTurnEnded),
	"EngineEventTypeActivityStarted": string(EngineEventTypeActivityStarted),
	"EngineEventTypeActivityEnded":   string(EngineEventTypeActivityEnded),
	"EngineEventTypeIdleHint":        string(EngineEventTypeIdleHint),
	"EngineEventTypeRateLimited":     string(EngineEventTypeRateLimited),
	"EngineEventTypeTurnFailed":      string(EngineEventTypeTurnFailed),
	"EngineErrorKindOverloaded":      string(EngineErrorKindOverloaded),
	"EngineErrorKindAuth":            string(EngineErrorKindAuth),
	"EngineErrorKindBilling":         string(EngineErrorKindBilling),
	"EngineErrorKindRejected":        string(EngineErrorKindRejected),
	"EngineErrorKindUnknown":         string(EngineErrorKindUnknown),
	"EngineNoPlatformMessage":        EngineNoPlatformMessage,
	"ApprovalKindPermission":         string(ApprovalKindPermission),
	"ApprovalKindQuestion":           string(ApprovalKindQuestion),
	"SessionKindResident":            string(SessionKindResident),
	"SessionKindRoom":                string(SessionKindRoom),
	"SessionKindClone":               string(SessionKindClone),
	"SessionKindWork":                string(SessionKindWork),
	"SessionKindTemp":                string(SessionKindTemp),
	"SessionStateStopped":            string(SessionStateStopped),
	"SessionStateStarting":           string(SessionStateStarting),
	"SessionStateIdle":               string(SessionStateIdle),
	"SessionStateBusy":               string(SessionStateBusy),
	"SessionStateWaiting":            string(SessionStateWaiting),
	"SessionStateLimited":            string(SessionStateLimited),
	"SessionCommandClear":            string(SessionCommandClear),
	"SessionCommandCompact":          string(SessionCommandCompact),
	"SessionCommandModel":            string(SessionCommandModel),
	"SessionCommandEffort":           string(SessionCommandEffort),
	"SessionCommandInterrupt":        string(SessionCommandInterrupt),
	"PendingKindPermission":          string(PendingKindPermission),
	"PendingKindQuestion":            string(PendingKindQuestion),
	"ChatKindUser":                   string(ChatKindUser),
	"ChatKindAssistant":              string(ChatKindAssistant),
	"ChatKindToolCall":               string(ChatKindToolCall),
	"ChatKindToolResult":             string(ChatKindToolResult),
	"ChatKindInterrupt":              string(ChatKindInterrupt),
	"ChatKindNotice":                 string(ChatKindNotice),
	"ThreadStatusStopped":            string(ThreadStatusStopped),
	"MessageWaitAnswer":              string(MessageWaitAnswer),
	"MessageWaitWaking":              string(MessageWaitWaking),
	"MessageWaitBusy":                string(MessageWaitBusy),
	"MessageWaitInterject":           string(MessageWaitInterject),
	"MessageWaitRetrying":            string(MessageWaitRetrying),
	"MessageWaitLimited":             string(MessageWaitLimited),
	"ReceiveWithin":                  ReceiveWithin.String(),
	"DeliverAlready":                 DeliverAlready,
	"SessionEndReasonLoggedOut":      SessionEndReasonLoggedOut,
	"AnswerNone":                     AnswerNone,
	"AnswerUnsupported":              AnswerUnsupported,
	"CommandUnsupported":             CommandUnsupported,
	"CommandDialog":                  CommandDialog,
	"CommandBadArg":                  CommandBadArg,
	"CommandTyping":                  CommandTyping,
	"ThreadStatusStarting":           string(ThreadStatusStarting),
	"ThreadStatusIdle":               string(ThreadStatusIdle),
	"ThreadStatusQueued":             string(ThreadStatusQueued),
	"ThreadStatusWorking":            string(ThreadStatusWorking),
	"ThreadStatusWaitingUser":        string(ThreadStatusWaitingUser),
	"ThreadStatusDone":               string(ThreadStatusDone),
	"ThreadStatusLimited":            string(ThreadStatusLimited),
	"WaitingReasonPermission":        string(WaitingReasonPermission),
	"WaitingReasonQuestion":          string(WaitingReasonQuestion),
	"WaitingReasonApproval":          string(WaitingReasonApproval),
	"WaitingReasonReminder":          string(WaitingReasonReminder),
	"WaitingReasonWorkQueue":         string(WaitingReasonWorkQueue),
	"ChangeTypeCreated":              string(ChangeTypeCreated),
	"ChangeTypeUpdated":              string(ChangeTypeUpdated),
	"ChangeTypeArchived":             string(ChangeTypeArchived),
	"HomeModeSimple":                 string(HomeModeSimple),
	"HomeModeFull":                   string(HomeModeFull),
	"ChatDisplayDefault":             string(ChatDisplayDefault),
	"ChatDisplayEngineer":            string(ChatDisplayEngineer),
	"HomeEnvProd":                    string(HomeEnvProd),
	"HomeEnvDev":                     string(HomeEnvDev),
	"TierOfficial":                   string(TierOfficial),
	"TierInstalled":                  string(TierInstalled),
	"TierMine":                       string(TierMine),
	"PackageKindData":                string(PackageKindData),
	"PackageKindProgram":             string(PackageKindProgram),
	"PackageStateStopped":            string(PackageStateStopped),
	"PackageStateBuilding":           string(PackageStateBuilding),
	"PackageStateRunning":            string(PackageStateRunning),
	"PackageStateFailed":             string(PackageStateFailed),
	"PackageStateRejected":           string(PackageStateRejected),
	"PackageRemedyInstallGo":         string(PackageRemedyInstallGo),
	"DataExportByPersona":            DataExportByPersona,
	"DataExportByFolder":             DataExportByFolder,
	"DataPersonaIDExact":             DataPersonaIDExact,
	"DataPersonaIDPrefix":            DataPersonaIDPrefix,
	"PersonaImportMaxBytes":          fmt.Sprint(PersonaImportMaxBytes),
	"HomeBackupUploadMaxBytes":       fmt.Sprint(HomeBackupUploadMaxBytes),
	"HomeBackupKindDaily":            HomeBackupKindDaily,
	"BackupSweepExpired":             BackupSweepExpired,
	"BackupSweepInUse":               BackupSweepInUse,
	"BackupSweepUnchecked":           BackupSweepUnchecked,
	"BackupSweepMisnamed":            BackupSweepMisnamed,
	"BackupSweepSkipped":             BackupSweepSkipped,
	"HomeBackupKindManual":           HomeBackupKindManual,
	"HomeBackupKindBeforeRestore":    HomeBackupKindBeforeRestore,
	"ExtensionKindSetting":           string(ExtensionKindSetting),
	"ExtensionKindPromptAppend":      string(ExtensionKindPromptAppend),
	"ExtensionKindPageSlot":          string(ExtensionKindPageSlot),
	"ErrorCodeInvalid":               string(ErrorCodeInvalid),
	"ErrorCodeNotFound":              string(ErrorCodeNotFound),
	"ErrorCodeConflict":              string(ErrorCodeConflict),
	"ErrorCodeForbidden":             string(ErrorCodeForbidden),
	"ErrorCodeUnavailable":           string(ErrorCodeUnavailable),
	"ErrorCodeInternal":              string(ErrorCodeInternal),

	// 看板介面等級（S3）：往上加時，舊的那一行登記進 api.removed。
	"BoardAPILevel": fmt.Sprint(BoardAPILevel),
}

var update = flag.Bool("update", false, "update the public API snapshot")

// Guards: REQ-144, REQ-147
func TestAPICompatibility(t *testing.T) {
	pkgs, err := parser.ParseDir(token.NewFileSet(), ".", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range pkgs["sdk"].Files {
		for _, d := range f.Decls {
			g, ok := d.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, spec := range g.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					if !s.Name.IsExported() {
						continue
					}
					_, structure := s.Type.(*ast.StructType)
					id, named := s.Type.(*ast.Ident)
					if structure || (named && id.Name == "string") {
						if _, ok := publicTypes[s.Name.Name]; !ok {
							t.Errorf("missing type registry entry: %s", s.Name)
						}
					}
				case *ast.ValueSpec:
					if g.Tok != token.CONST {
						continue
					}
					for _, n := range s.Names {
						if n.IsExported() {
							if _, ok := publicEnums[n.Name]; !ok {
								t.Errorf("missing enum registry entry: %s", n)
							}
						}
					}
				}
			}
		}
	}
	var lines []string
	for name, typ := range publicTypes {
		lines = append(lines, fmt.Sprintf("%s %s", name, typ.Kind()))
		if typ.Kind() != reflect.Struct {
			continue
		}
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			if !f.IsExported() {
				continue
			}
			lines = append(lines, fmt.Sprintf("%s.%s %s %s", name, f.Name, f.Type, f.Tag.Get("json")))
		}
	}
	for name, value := range publicEnums {
		lines = append(lines, fmt.Sprintf("enum.%s %s", name, value))
	}
	sort.Strings(lines)
	snapshot := strings.Join(lines, "\n") + "\n"
	if *update {
		if t.Failed() {
			return
		}
		if err := os.WriteFile("testdata/api.golden", []byte(snapshot), 0644); err != nil {
			t.Fatal(err)
		}
		return
	}
	golden, err := os.ReadFile("testdata/api.golden")
	if err != nil {
		t.Fatal(err)
	}
	// D-5 allows additions; every existing declaration must retain its exact shape.
	current := map[string]bool{}
	for _, line := range lines {
		current[line] = true
	}
	for _, line := range strings.Split(strings.TrimSpace(string(golden)), "\n") {
		if !current[line] {
			t.Errorf("API declaration removed or changed: %s", line)
		}
	}
}

// Guards: REQ-144, REQ-147
func TestHTTPStatus(t *testing.T) {
	for code, want := range map[ErrorCode]int{ErrorCodeInvalid: 400, ErrorCodeNotFound: 404, ErrorCodeConflict: 409, ErrorCodeForbidden: 403, ErrorCodeUnavailable: 503, ErrorCodeInternal: 500, "": 500} {
		if got := HTTPStatus(code); got != want {
			t.Errorf("%q: got %d, want %d", code, got, want)
		}
	}
}

// Guards: REQ-144, REQ-147
func TestJSONFieldPresence(t *testing.T) {
	empty := ""
	b, err := json.Marshal(TasksUpdateRequest{Goal: &empty})
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"goal":""}` {
		t.Fatalf("explicit empty patch lost: %s", b)
	}
	b, err = json.Marshal(TasksUpdateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "{}" {
		t.Fatalf("absent patch gained fields: %s", b)
	}
	b, err = json.Marshal(PageRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"limit":0}` {
		t.Fatalf("numeric zero lost: %s", b)
	}
	b, err = json.Marshal(Record{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"archived":false`) {
		t.Fatalf("boolean false lost: %s", b)
	}
}

// Guards: REQ-144, REQ-147
func TestRoutesReferenceKnownTypes(t *testing.T) {
	seen := map[string]bool{}
	for _, r := range Routes {
		if seen[r.Path] {
			t.Errorf("duplicate route: %s", r.Path)
		}
		seen[r.Path] = true
		if r.Path != "/api/"+r.Service+"/"+r.Method {
			t.Errorf("bad path: %s", r.Path)
		}
		for _, n := range []string{r.Request, r.Response} {
			typ, ok := publicTypes[n]
			if !ok || typ.Kind() != reflect.Struct {
				t.Errorf("unknown route type: %s", n)
			}
		}
	}
}

// Guards: REQ-152, REQ-144
func TestGoalsClientRoutes(t *testing.T) {
	c := &Client{}
	client := NewGoalsClient(c)
	for _, route := range Routes {
		if route.Service != "goals" {
			continue
		}
		t.Run(route.Method, func(t *testing.T) {
			called := false
			c.http = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				called = true
				if r.Method != "POST" || r.URL.Path != route.Path {
					t.Errorf("unexpected route %s %s", r.Method, r.URL.Path)
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader("{}"))}, nil
			})}
			name := ""
			for _, part := range strings.Split(route.Method, "_") {
				name += strings.ToUpper(part[:1]) + part[1:]
			}
			method := reflect.ValueOf(client).MethodByName(name)
			if !method.IsValid() {
				t.Fatal("missing client method")
			}
			result := method.Call([]reflect.Value{reflect.ValueOf(context.Background()), reflect.New(method.Type().In(1).Elem())})
			if !called || !result[1].IsNil() {
				t.Fatal("client invocation failed")
			}
		})
	}
}

// Guards: REQ-144, REQ-153
func TestEngineHostCheckActionRoundTrip(t *testing.T) {
	request := &EngineHostCheckActionRequest{Session: "work", Class: ActionClassWrite, Tool: "write", Summary: "新增檔案"}
	c := &Client{http: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/api/engine_host/check_action" {
			t.Fatal(r.URL.Path)
		}
		var got EngineHostCheckActionRequest
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(&got, request) {
			t.Fatal(got)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"verdict":"deny","reason":"只允許查詢"}`))}, nil
	})}}
	response, err := NewEngineHostClient(c).CheckAction(context.Background(), request)
	if err != nil || response.Verdict != ActionVerdictDeny || response.Reason != "只允許查詢" {
		t.Fatal(response, err)
	}
}

// 法律第 7 條之前寫的資料帶 site（還有已拿掉的 offline 狀態）：解得開、其他欄位照讀，site 直接略過。
func TestLegacySiteFieldDecodes(t *testing.T) {
	cases := map[string]any{
		"Session":       &Session{},
		"ThreadSummary": &ThreadSummary{},
		"Notification":  &Notification{},
		"Nudge":         &Nudge{},
		"Reminder":      &Reminder{},
		"Envelope":      &Envelope{},
	}
	for name, v := range cases {
		if err := json.Unmarshal([]byte(`{"agent":"code","site":"office"}`), v); err != nil {
			t.Fatalf("%s 解不開帶 site 的舊資料：%v", name, err)
		}
	}
	var th ThreadSummary
	if err := json.Unmarshal([]byte(`{"name":"code","status":"offline","site":"office"}`), &th); err != nil || th.Name != "code" {
		t.Fatalf("舊的 offline 狀態也要解得開：%+v %v", th, err)
	}
}

// 2026-11-01 之前的封包帶 goal（傳話掛 Goal 已拿掉）：解得開、其他欄位照讀，goal 直接略過。
func TestLegacyEnvelopeGoalDecodes(t *testing.T) {
	var e Envelope
	if err := json.Unmarshal([]byte(`{"id":"x","from":"work","to":"code","goal":"GOAL-1","body":"做事"}`), &e); err != nil || e.From != "work" || e.Body != "做事" {
		t.Fatalf("解不開帶 goal 的舊封包：%+v %v", e, err)
	}
}

// 2026-11-01 之前的 Goal 帶 grants（授權範圍，DEV-95 拿掉）：解得開、其他欄位照讀，grants 直接略過。
func TestLegacyGoalGrantsDecode(t *testing.T) {
	for name, v := range map[string]any{"Goal": &Goal{}, "GoalsProposeRequest": &GoalsProposeRequest{}, "GoalsCreateRequest": &GoalsCreateRequest{}} {
		if err := json.Unmarshal([]byte(`{"title":"舊目標","text":"原話","grants":["push","delete"]}`), v); err != nil {
			t.Fatalf("%s 解不開帶 grants 的舊資料：%v", name, err)
		}
	}
	var g Goal
	if err := json.Unmarshal([]byte(`{"id":"GOAL-1","title":"舊目標","grants":["push"]}`), &g); err != nil || g.ID != "GOAL-1" || g.Title != "舊目標" {
		t.Fatalf("舊 Goal：%+v %v", g, err)
	}
}

// 2026-11-02 之前（S5）的那台送來的 stuck 狀態、lost 旗標、late 原因：解得開、其他欄位照讀；
// 狀態與原因原樣留著（看板不認得就畫灰），lost 直接略過。
func TestLegacySessionS5FieldsDecode(t *testing.T) {
	var s Session
	raw := `{"name":"code","state":"stuck","messages":[{"id":"m1","body":"在嗎","lifecycle":"interrupted","lost":true,"wait":"late"}]}`
	if err := json.Unmarshal([]byte(raw), &s); err != nil || s.Name != "code" || len(s.Messages) != 1 {
		t.Fatalf("解不開舊版的 session：%+v %v", s, err)
	}
	if s.State != "stuck" || s.Messages[0].Wait != "late" || s.Messages[0].Body != "在嗎" {
		t.Fatalf("舊的狀態與原因要原樣留著：%+v", s)
	}
	var th ThreadSummary
	if err := json.Unmarshal([]byte(`{"name":"code","status":"stuck"}`), &th); err != nil || th.Status != "stuck" {
		t.Fatalf("舊的 stuck 討論串：%+v %v", th, err)
	}
}
