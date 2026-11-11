export const en = {
  updatesTitle: 'Software updates',
  updatesDescription:
    'Check official SubLane releases and update this instance. Only the platform owner can install updates.',
  updateCurrent: 'Current version',
  updateLatest: 'Latest stable release',
  updateCheck: 'Check for updates',
  updateInstall: 'Update to {{version}}',
  updateConfirmTitle: 'Update SubLane',
  updateConfirmDescription:
    'Install {{version}} and restart SubLane? Active requests may be interrupted. Export and verify a backup first. Reinstalling an older version cannot undo database migrations.',
  updateBackupConfirm: 'I have exported and verified a current backup.',
  updateConfirm: 'Install update',
  updateDisabled: 'Enable the host updater to install updates from this page.',
  updateSetup: 'Set up web updates',
  updateQueued: 'Update queued. Waiting for the host updater.',
  updateRunning: 'Installing the update. SubLane will restart.',
  updateSucceeded: 'Update completed. Reload to use the new version.',
  updateFailed:
    'The update failed. Check the host updater log before retrying. A migrated database may require restoring a verified backup.',
  updateInterrupted:
    'The update was interrupted. Check the installed version and host updater log before continuing.',
  updateReconnecting: 'Reconnecting to SubLane…',
  updateReload: 'Reload page',
  updateNoNew: 'No newer stable release found.',
  updateCheckFailed:
    'Could not check stable releases. Check network access to GitHub and try again later.',
  updateRequestFailed:
    'Could not confirm the update request. Refresh to check its status before retrying.',
  updateCooldown: 'Please wait one minute before checking again.',
  updateReleaseNotes: 'Release notes',
  updateBuildUnsupported:
    'This build requires a manual update. Web installation supports stable release builds.',
  updateWorkerLost:
    'The host updater is not responding. Check its systemd service on the server.',
  auditUpdateRequest: 'Requested a software update',
  allocationInitialEditHint:
    'Before the first activation, edit the initial member plan here. Saving keeps its reviewed start time and accounting anchor.',
  allocationReplacePendingHint:
    'To change members in this scheduled new period, confirm the replacement below. Budget and price edits can keep the existing plan.',
  allocationSharedBudgetBlocked:
    'The shared group budget is temporarily unavailable. Retry later or after renewal.',
  allocationChanged:
    'This allowance changed or the confirmed time expired. Refresh and review before saving.',
  allocationMemberConflict:
    'The scheduled rule has a separate member plan or incompatible limits. Review or cancel it explicitly.',
  allocationFundingGap:
    'The remaining shared budget cannot fund these shares. Reduce them or retry after renewal.',
  allocationReplacementRequired:
    'Changing the unit or reset schedule requires confirming a new accounting period.',
  allocationInitialScheduleRequired:
    'The initial schedule cannot be cancelled. Pause the allowance or move its start time.',
  allocationConfirmReplacement:
    'Confirm a new accounting period at the displayed time. Existing requests keep their original accounting.',
  allocationReschedule: 'Move to next boundary',
  allocationCancelSchedule: 'Cancel scheduled change',
  allocationWindowSharesHint:
    'Each condition is a shared group budget. The same member percentages apply independently to every window.',
  allocationConfirmStart: 'Confirm start time',
  allocationReviewLatest: 'Refresh and review',

  turnstileRetry: 'Retry',
  turnstileTitle: 'Turnstile',
  turnstileDescription:
    'Require Cloudflare verification for password and passkey sign-in across this instance.',
  turnstileEnabled: 'Require verification at sign-in',
  turnstileSiteKey: 'Site Key',
  turnstileSecretKey: 'Secret Key',
  turnstileSecretSaved: 'Secret Key is configured. Leave blank to keep it.',
  turnstileSecretHint:
    'Add your login hostname to the widget in Cloudflare. The secret is encrypted and never displayed.',
  turnstileSave: 'Save Turnstile',
  turnstileSaved: 'Turnstile settings saved.',
  turnstileSettingsFailed:
    'Could not load or save Turnstile settings. Please retry.',
  turnstileSettingsInvalid:
    'Enter both a valid Site Key and Secret Key before enabling verification.',
  turnstileFailed:
    'Verification failed or expired. Please complete the challenge again.',
  turnstileUnavailable: 'Verification is unavailable. Please retry.',
  turnstileVerification: 'Human verification',

  memberRemovedStatus: 'Removed',
  memberRemove: 'Remove from workspace',
  memberRemoveTitle: 'Remove member from workspace',
  memberRemoveDescription:
    'Remove “{{username}}” from this workspace? Their access and API keys in this workspace will be revoked. Historical requests and usage are retained. Rejoining requires new keys and resource-group access.',
  memberRemoveConfirm: 'Remove member',
  memberRemoveFailed:
    'Could not remove this member. Refresh the list and try again.',
  memberRemoved: '{{username}} was removed from this workspace.',
  auditMemberRemove: 'Removed workspace member',
  exportCSV: 'Export CSV',
  exportCSVLoading: 'Exporting…',
  exportCSVFailed: 'Could not export CSV. Refresh and try again.',

  totpTitle: 'Two-factor authentication',
  totpDescription:
    'Add a verification code from your authenticator app to password sign-in. Applies to your account in every workspace.',
  totpEnabled: 'Enabled',
  totpDisabled: 'Not enabled',
  totpEnable: 'Enable 2FA',
  totpEnableTitle: 'Enable two-factor authentication',
  totpDisable: 'Disable 2FA',
  totpScan:
    'Scan this QR code with your authenticator app, or enter the setup key. Then enter a fresh code to finish.',
  totpSecret: 'Setup key',
  totpCode: 'Verification code',
  totpQR: 'Authenticator setup QR code',
  totpConfirm: 'Verify and enable',
  factorCode: 'Verification code or recovery code',
  factorRequired:
    'Enter a fresh code from your authenticator app, or an unused recovery code.',
  factorInvalid:
    'This code is invalid, expired or already used. Try a fresh code or an unused recovery code.',
  recoveryRegenerate: 'Replace recovery codes',
  recoveryCodesTitle: 'Save your recovery codes',
  recoveryDescription:
    'Keep these codes somewhere safe. Each code works once instead of an authenticator code. They will not be shown again; replacing them invalidates all previous codes.',
  recoveryRemaining: 'Recovery codes remaining: {{remaining}}',
  recoveryDownload: 'Download codes',
  recoverySaved: 'I saved these codes',
  passkeysTitle: 'Passkeys',
  passkeysDescription:
    'Sign in without a password using your fingerprint, face or device PIN. A verified passkey also satisfies two-factor authentication. You can add up to 10.',
  passkeysEmpty: 'No passkeys added.',
  passkeyAdd: 'Add passkey',
  passkeyDelete: 'Delete passkey',
  passkeyDeleteNamed: 'Delete {{name}}',
  passkeyName: 'Passkey name',
  signInPasskey: 'Sign in with a passkey',
  passkeyUnavailable:
    'Passkeys require HTTPS or localhost and a browser with WebAuthn JSON support.',
  passkeyFailed:
    'Could not verify the passkey. Try again or use your password.',
  passkeyCancelled:
    'Passkey verification was cancelled or timed out. Try again when ready.',
  securityProofHint:
    'Confirm your current password and, if enabled, a fresh verification or recovery code. Other browser sessions will be signed out after a change.',
  securityExpired:
    'This setup has expired or the account changed. Close this dialog and start again.',
  securityInvalid:
    'Check your inputs. Passkey names must contain 1–64 characters, with at most 10 passkeys per account.',
  securityContinue: 'Continue',
  securitySaved: 'Security settings updated.',
  auditTotpEnable: 'Enabled two-factor authentication',
  auditTotpDisable: 'Disabled two-factor authentication',
  auditRecoveryCodes: 'Replaced recovery codes',
  auditPasskeyAdd: 'Added passkey',
  auditPasskeyDelete: 'Deleted passkey',
  contentDetails: 'Scope and limits',
  contentScanLimit: 'Scan limit',
  contentScanSummary: '8 MiB JSON · 64 nesting levels',
  contentRulesLabel: 'Rules',
  contentEnabledCount: '{{enabled}} of {{count}} enabled',
  contentRuleCount: '{{count}} / 50 rules',
  contentRuleStatus: 'Status',
  contentEmptyHint:
    'Add a specific term or expression to check outgoing request text.',
  contentTitle: 'Content rules',
  contentDescription:
    'Check outgoing request text against workspace terms and regular expressions. Matching content is encrypted and stays hidden.',
  contentWorkspacePolicy: 'Workspace policy',
  contentOff: 'Off',
  contentObserve: 'Observe',
  contentBlock: 'Block',
  contentMode: 'Handling mode',
  contentText: 'Text contains',
  contentRegex: 'Regular expression',
  contentKind: 'Match type',
  contentRuleName: 'Rule name',
  contentPattern: 'Matching content',
  contentRuleEnabled: 'Enabled',
  contentRuleDisabled: 'Disabled',
  contentEnableRule: 'Enable this rule',
  contentOffHint:
    'Checks are off. Requests are forwarded without content checks.',
  contentObserveHint:
    'Matches appear in request diagnostics. Requests still go upstream, including matching content.',
  contentBlockHint:
    'Matching requests are rejected before dispatch. An incomplete check also prevents forwarding.',
  contentScope:
    'Checks cover JSON text, including conversation history and tool outputs. Images, binary attachments and encoded secrets are not decoded. Rule matches and policy versions are retained with request diagnostics; request bodies are never saved.',
  contentEmpty: 'No rules configured.',
  contentConfigure: 'Configure rules',
  contentSaved: 'Content rules saved.',
  contentAdd: 'Add rule',
  contentRemove: 'Remove rule',
  contentNewRule: 'New rule',
  contentKeepPattern: 'Leave blank to keep the saved content',
  contentTextHint:
    'Case-sensitive text matching. Use a specific value; broad terms such as api_key can match ordinary code. Keep secrets out of the rule name.',
  contentRegexHint:
    'Uses Go RE2 syntax; lookaround and backreferences are unsupported. Use (?i) for case-insensitive matching. Empty matches are rejected. Your current input is visible with syntax colors; saved patterns stay hidden.',
  contentLimits:
    'Up to 50 rules, 64 bytes per name and 1,024 bytes per expression. Checks cover up to 8 MiB of JSON and 64 nesting levels. Larger or deeper requests have an incomplete check. Changes apply to subsequent checks, including existing WebSocket conversations.',
  contentTestTitle: 'Test this rule',
  contentSample: 'Test sample',
  contentTest: 'Test rule',
  contentSampleHint:
    'Test one rule without forwarding to a model. Samples are not stored and clear after testing.',
  contentMatched: 'Sample matched.',
  contentNotMatched: 'Sample did not match.',
  contentInvalid:
    'Check the rule types, names, patterns and limits. Invalid rules were not saved.',
  contentConflict:
    'Rules changed in another session. Refresh before editing again.',
  contentUnavailable: 'Could not load or check content rules. Try again.',
  reasonContentBlocked: 'Blocked by content rules',
  reasonContentUnavailable: 'Content check unavailable',
  requestContent: 'Content check',
  requestContentRules: 'Matched rule IDs',
  requestContentRevision: 'Policy version',
  requestContentPassed: 'No matches',
  requestContentFailed: 'Check incomplete',
  recoveryContent:
    'Remove sensitive text from the request and its conversation history, or ask your workspace administrator to review the rules. This request was not sent upstream.',
  recoveryContentUnavailable:
    'Content checks could not finish. Retry or ask your workspace administrator to check the policy.',

  availabilityTitle: 'Group model availability',
  availabilityModel: 'Model ID to inspect',
  availabilityInspect: 'Inspect {{model}}',
  availabilityCheck: 'Check status',
  availabilityChecking: 'Checking status…',
  availabilityHint:
    'A local snapshot of eligibility for a new request. No generation is sent. Key, member and allocation limits still apply, and existing conversations keep their original resource.',
  availabilityFailed: 'Could not update this snapshot. Check status again.',
  availabilityEligible: 'Eligible for a new request',
  availabilityUnknown: 'Availability unknown',
  availabilityUnavailable: 'No eligible account',
  availabilitySummary:
    '{{available}} eligible · {{unknown}} with unknown model support',
  availabilityRetryAt: 'Known retry time: {{date}}',
  availabilityObserved: 'Observed: {{date}}',
  availabilityAccounts: 'Account eligibility',
  availabilityConcurrency: '{{active}} / {{limit}} concurrent requests',
  availabilityQuotaUsed: '{{percent}}% quota used',
  availabilityQuotaUnknown: 'Quota unavailable or stale',
  availabilityDisabled: 'Account disabled',
  availabilityProviderDisabled: 'Provider disabled',
  availabilityReauthorize: 'Reauthorization required',
  availabilityNoAccounts: 'Group has no accounts',
  alertsTest: 'Send test notification',
  alertsTesting: 'Sending test…',
  alertsTestSuccess: 'The saved destination accepted the test notification.',
  alertsTestFailed:
    'Test delivery failed. Check the saved destination and try again after one minute.',
  alertsTestCooling: 'Wait one minute between test notifications.',
  alertsModelEnable: 'Notify when a group model has no eligible account',
  alertsQuotaThreshold: 'Notify when quota used reaches (%)',
  alertsQuotaHint:
    'Enter 1–99; leave blank to disable. Only fresh, model-relevant quota observations are used. Unknown data does not prove recovery.',
  alertModelUnavailable: 'Group model unavailable',
  alertQuotaLow: 'Account quota threshold reached',
  alertAllowanceLow: 'Member allowance threshold reached',
  alertsAllowanceDescription:
    'Enabled webhooks also notify at 80% and 95% of a member’s settled allowance, once per threshold and period.',
  auditSettingsTest: 'Requested a webhook test',
  modelPrices: 'Model prices',
  pricingDescription:
    'Current reference prices used for request cost estimates and new allowance revisions. These are not subscription charges. Existing allowance revisions use their saved prices. A listed price does not guarantee model access; check your key’s model catalog.',
  pricingUnit: 'USD per 1 million tokens',
  pricingSearch: 'Search model prices',
  pricingModel: 'Model',
  pricingInput: 'Input',
  pricingCached: 'Cached input',
  pricingOutput: 'Output',
  pricingSource: 'Source',
  pricingSourceRemote: 'Remote catalog',
  pricingSourceCache: 'Local cache',
  pricingSourceFallback: 'Fallback prices',
  pricingSourceOverride: 'Custom prices',
  pricingSourceUnknown: 'Price catalog',
  pricingLoading: 'Loading model prices…',
  pricingLoadFailed: 'Could not load model prices. Refresh to try again.',
  pricingReloadFailed:
    'Could not reload prices. Showing the last loaded catalog.',
  pricingEmpty:
    'No model prices are available yet. Reload after the price catalog has synced.',
  pricingNoMatch: 'No matching model prices.',
  pricingRange: '{{start}}–{{end}} of {{count}} models',
  demoTitle: 'Read-only demo',
  demoDescription:
    'Explore sample accounts, groups, and usage. Changes, credential actions, and live API calls are disabled. Data resets when the demo restarts.',
  demoCredentials: 'Username: {{username}} · Password: {{password}}',
  demoExplore: 'Explore demo',
  demoReadOnly:
    'Changes are unavailable in this demo. You can still browse and filter the sample data.',
  tourStart: 'Start guided setup',
  tourOpen: 'Guided setup',
  tourAccessWaiting:
    'After your administrator grants access, check progress to continue.',
  tourTitle: 'Guided setup',
  tourStep: 'Step {{current}} of {{total}}',
  tourSkip: 'Skip guide',
  tourPrevious: 'Previous step',
  tourNext: 'Next step',
  tourFinish: 'Finish guide',
  tourCheck: 'Check progress',
  tourStepDone: 'This step is complete. Continue when you are ready.',
  tourWaiting:
    'Complete the highlighted action. The next step unlocks after confirmation.',
  tourUnavailable:
    'Progress could not be confirmed. Check again before continuing.',
  tourOpenStep: 'Go to this step',

  recoveryModel:
    'Open this key’s client guide and choose a model from its available catalog. Ask your administrator if the required model is missing.',
  recoveryUpstream:
    'Check the affected account’s connection, proxy, and cooldown status. Retry after recovery; an existing conversation stays on its original account.',
  alertsSaving: 'Saving…',
  recoveryAllocations: 'Review allocations',
  alertsTitle: 'Workspace alerts',
  alertsEnabled: 'Notifications enabled',
  alertsDisabled: 'Notifications off',
  alertsConfigure: 'Configure alerts',
  alertsDescription:
    'Receive webhook notifications when an account needs authorization, a group has no verified available account, or generation failures increase.',
  alertsUnavailable: 'Could not load or save alert settings. Try again.',
  alertsDeliveryFailed:
    'The webhook could not be delivered. Check the receiver; delivery will retry automatically.',
  alertsNeverDelivered:
    'No notification has been delivered yet. The destination is not verified until an event is accepted.',
  alertsLastDelivered: 'Last delivered: {{date}}',
  alertsPayload:
    'Checks run every minute. Only event IDs, workspace/resource IDs, event types, state changes, and timestamps are sent. Unchanged incidents are deduplicated; recovery sends another event.',
  alertsEnable: 'Enable notifications',
  alertsURL: 'Webhook URL',
  alertsURLHint:
    'Use a public HTTPS endpoint accepting JSON POST requests. The URL is stored encrypted.',
  alertsKeepURL:
    'Leave blank to keep the encrypted destination. Enter a new URL to replace it.',
  alertsClear: 'Remove the saved destination and disable notifications',
  alertsInvalid:
    'Enter a public HTTPS URL without credentials or a fragment. Enabling notifications requires a destination.',
  alertReauthorization: 'Account needs authorization',
  alertPoolUnavailable: 'Group has no verified available account',
  alertRequestFailures: 'Generation failures increased',

  clientConfigureKey: 'Configure {{name}}',
  clientCatalogUnavailable:
    'No model catalog is available for this key. Refresh it or ask your administrator to check its group.',
  clientCheckRequest: 'Check first request',
  recoveryTitle: 'What to do next',
  recoveryWait:
    'Wait before retrying. Repeated immediate retries will not free capacity or end a cooldown.',
  recoveryContext:
    'Reduce the conversation context or start a new conversation in your client.',
  recoveryQuota:
    'The subscription quota is exhausted. Check its reset time with your administrator. An existing conversation stays on its original account.',
  recoveryAdmin:
    'Open the relevant settings, check this request’s group and account, then ask the caller to retry after resolving the cause.',
  recoveryContact:
    'Copy the request ID above and send it to your workspace administrator. They need to repair the account, group permissions, or allocation.',
  recoveryUnknown:
    'Keep the request ID above. If a retry also fails, share it with your workspace administrator for diagnosis.',

  activationTitle: 'Complete your first request',
  activationAccount: 'Connect a resource',
  activationAccountHint:
    'Connect and enable a subscription account or API channel in this workspace.',
  activationVerify: 'Verify the resource connection',
  activationVerifyHint:
    'Verify the subscription account or API channel before adding it to a resource group.',
  activationPool: 'Add a resource to an enabled group',
  activationPoolHint:
    'Your resource is connected. Create or enable a resource group and select the verified account or channel.',
  activationPoolAction: 'Set up a resource group',
  activationAccess: 'Receive resource group access',
  activationAccessHint:
    'Ask your workspace administrator to grant you access to a ready resource group.',
  activationKey: 'Create your personal API key',
  activationKeyHint: 'Create or enable a personal key for your resource group.',
  activationClient: 'Configure a client and send a message',
  activationClientHint:
    'Open a key’s client guide, choose an available model, and send a message. This step completes after the gateway records your successful generation.',
  activationComplete: 'Your first request succeeded',
  activationCompleteHint:
    'A successful generation has been recorded for you in this workspace.',
  activationRequests: 'View my requests',

  workspaceChooseTitle: 'Choose a workspace',
  workspaceChooseDescription: 'Select an active workspace to continue.',
  workspaceNoneActive: 'You do not have access to an active workspace.',
  workspaceCreate: 'Create workspace',
  workspaceCreateDescription:
    'Set up a separate space for members, accounts, and usage.',
  workspaceName: 'Workspace name',
  workspaceNameInvalid: 'Enter a workspace name with up to 64 characters.',
  workspaceCreateFailed: 'Could not create the workspace. Try again.',
  workspaceLimitReached: 'You can own up to 10 workspaces.',
  workspaceListFailed: 'Could not load workspaces.',
  workspaceSave: 'Save workspace',
  allocationPause: 'Pause resource allowance',
  allocationResume: 'Enable resource allowance',
  allocationAccountLocked:
    'This account is reserved by a resource allowance and cannot be deleted. Disable it to stop routing requests.',
  allocationUnavailableShort: 'Unavailable',
  allocationWaiting: 'Scheduled',
  allocationPrerequisite:
    'Create an enabled resource group with subscription accounts, then grant group access to members. Each group can have one resource allowance.',
  allocationRosterFailed: 'Could not load members with access to this group.',
  allocationNoGrantedMembers:
    'Grant members access to this resource group before assigning an allowance.',
  allocationSchemes: 'Resource allowances',
  allocationsDescription:
    'Optional limits by share of a token or internal USD budget, or by direct member allowance.',
  allocationCreate: 'Add resource allowance',
  allocationEdit: 'Edit resource allowance',
  allocationEditNamed: 'Edit resource allowance {{name}}',
  allocationName: 'Resource allowance name',
  allocationMode: 'Allowance type',
  allocationRatio: 'By share',
  allocationRatioUnit: 'Share basis',
  allocationRatioTokens: 'By token share',
  allocationRatioAmount: 'By amount share',
  allocationShares: 'Member shares',
  allocationSplitEqually: 'Split equally',
  allocationSectionDetails: 'Rule details',
  allocationSectionPolicy: 'Allowance and reset',
  allocationSectionActivation: 'Activation',
  allocationModeRatioSummary: 'Split one budget by member percentage',
  allocationModeAmountSummary: 'Give each member a USD limit',
  allocationModeTokensSummary: 'Give each member a Token limit',
  allocationModeWindowsSummary: 'Set multiple USD limits by duration',
  allocationAmount: 'By amount',
  allocationTokens: 'By tokens',
  allocationWindows: 'By time window',
  allocationWindowConditions: 'Time-window conditions',
  allocationWindowConditionsHint:
    'Add up to 8 windows. Each has its own duration and shared USD limit; blank means unlimited.',
  allocationAddCondition: 'Add condition',
  allocationCondition: 'Condition {{index}}',
  allocationConditionDuration: 'Duration for condition {{index}}',
  allocationConditionUnit: 'Unit for condition {{index}}',
  allocationConditionLimit: 'Shared limit for condition {{index}}',
  allocationRemoveCondition: 'Remove condition {{index}}',
  allocationHours: 'Hours',
  allocationDays: 'Days',
  allocationDurationHours: '{{value}} hours',
  allocationDurationDays: '{{value}} days',
  allocationOverrideWindow: 'Override {{duration}} for {{name}}',
  allocationWindowFor: '{{duration}} limit for {{name}}',
  allocationWindowDurationHint:
    'Each window starts when the rule takes effect and renews after its own duration. Times are shown in {{zone}} and can differ from upstream resets.',
  allocationFutureEditDelay:
    'Future edits may wait up to {{duration}} before taking effect, based on the longest current window.',
  allocationAutoPricingHint:
    'Model prices are filled automatically from the models supported by the selected resource group when you save.',
  allocationSave: 'Save resource allowance',
  allocationView: 'View usage',
  allocationBack: 'Back to resource allowances',
  allocationsEmpty:
    'Create a dedicated resource group and grant members access to add a resource allowance.',
  allocationTotalBudget: 'Total budget',
  allocationSelectAllMembers: 'Select all members',
  allocationIncludeMember: 'Include {{name}}',
  allocationOverrideMember: 'Override limits for {{name}}',
  allocationOverrideLimits: 'Set personal limits',
  allocationTotalAmountHint:
    'This is an internal USD budget calculated from saved model prices, not an upstream cash balance.',
  allocationTotalTokensHint:
    'This is a budget you choose, not the subscription’s actual token capacity. 1 M = 1,000,000 tokens.',
  allocationRatioTokenRule:
    'Each member receives their percentage of the total token budget for the selected period.',
  allocationRatioAmountRule:
    'Each member receives their percentage of the total internal USD budget for the selected period.',
  allocationExclusiveHint:
    'Use a dedicated group and remove its accounts from every other group first. After adding an allowance, the group and its resource membership stay fixed.',
  allocationMembers: 'Member allowances',
  allocationBlankHint:
    'Select members who should receive this allowance, then enter a limit or percentage for each selected member.',
  allocationFor: 'Allowance for {{name}}',
  allocationTotal: 'Allocated {{total}}% · Unallocated {{remaining}}%',
  allocationPeriod: 'Reset period',
  allocationWindowSchedule: '{{windows}} time windows · {{zone}}',
  allocationWindowReached: 'Limit reached',
  allocationWindowRisk: 'New requests paused',
  allocationUnlimited: 'Unlimited',
  allocationUnmeteredExposure:
    'This window: {{inFlight}} in flight · {{pending}} awaiting usage',
  allocationWindowPendingHint:
    'Pending records across this allowance: {{pending}}. Each window counts only records within its own duration.',
  allocationWindowBlankHint:
    'Only selected members may use this allowance. A blank amount in a shared or personal condition means unlimited use for that condition.',
  allocationResetDay: 'Day of month',
  allocationResetTime: 'Reset time',
  allocationResetZoneHint: 'Times use the instance time zone ({{zone}}).',
  allocationResetShortMonthHint:
    'If a month has fewer days, it resets on the last day.',
  allocationDailySchedule: 'Daily at {{time}} · {{zone}}',
  allocationMonthlySchedule: 'Monthly on day {{day}} at {{time}} · {{zone}}',
  allocationRemove: 'Remove',
  allocationEnabled: 'Enable this resource allowance',
  allocationNextHint:
    'Budget and price edits remain scheduled and retain usage. Edit current members separately. A new unit or reset schedule requires confirming a new accounting period. Pause access with the separate control.',
  allocationEditEffectivePreview:
    'With the current schedule, changes saved now are expected to take effect on {{date}}. Pausing or enabling access takes effect immediately.',
  allocationStartEffectivePreview:
    'If saved now, this rule is expected to start on {{date}}.',
  allocationMissingPrices:
    'Saved model prices are in use because current catalog prices are missing ({{count}}): {{models}}. Refresh the price catalog to update future revisions.',
  allocationUncoveredModels:
    'Supported models absent from this rule’s saved prices ({{count}}): {{models}}. Save the allowance to include its price once available; requests remain blocked until that revision takes effect.',
  allocationUncoveredModelsScheduled:
    'Supported models without prices in the saved upcoming version ({{count}}): {{models}}. Save a corrected version before it takes effect; requests to these models will remain blocked after this version takes effect.',
  allocationUncoveredModelsTiming:
    'If saved now, this change is expected to take effect on {{date}}.',
  allocationUncoveredModelsScheduledTiming:
    'This saved version is scheduled to take effect on {{date}}.',
  allocationCatalogUnavailable:
    'Some account model catalogs are unavailable. Saved prices are retained when available; refresh account models to update coverage.',
  allocationCurrentPricing: 'Current price coverage',
  allocationScheduledPricing: 'Scheduled price coverage',
  allocationStartNext: 'Start next period',
  allocationApplyNow: 'Edit current members',
  allocationApplyNowWarning:
    'Member edits retain usage and ongoing requests; an independent scheduled member plan must be resolved first.',
  allocationScheduledHint:
    'Scheduled budget and price changes retain period boundaries and recorded usage.',
  allocationImmediateHint:
    'Start immediately: the budget counts new requests from activation. Earlier usage is not backfilled.',
  allocationScheduled: '{{mode}} takes effect {{date}}',
  allocationLoading: 'Loading allocations…',
  allocationFailed: 'Unable to load or save allocations. Please retry.',
  allocationInvalid:
    'Check the name, selected members, reset date/time, and window conditions. Shares must total at most 100%; percentages allow two decimal places and M / USD allow six.',
  allocationPoolConflict:
    'These accounts are reserved by a resource allowance or shared with another group. Use a separate group; a managed group’s account list is fixed.',
  allocationSettlementConflict:
    'Usage changed or the correction conflicts with a previous settlement. Refresh before retrying.',
  allocationPending: 'Usage pending reconciliation',
  allocationRiskLimit:
    'Too many requests are in flight or admission headroom is low. Retry when an ongoing request finishes. Pending usage does not pause admission.',
  allocationRiskPaused: 'New requests temporarily paused',
  allocationRiskExposure:
    'This cycle: {{inFlight}} in flight · {{reserved}} {{unit}} temporarily reserved',
  allocationPendingCurrent:
    '{{pending}} awaiting usage this cycle; unconfirmed usage is not charged or reserved.',
  allocationAdmissionRoom:
    'Admission headroom: {{room}} {{unit}} (includes a temporary buffer, not added allowance).',
  allocationOlderPending:
    'Older pending requests: {{older}}. They do not count toward this cycle’s admission.',
  allocationExhausted: 'Allowance exhausted',
  allocationUnavailable:
    'This resource allowance is paused, scheduled, or no longer available to you.',
  allocationUnpriced:
    'No usable model price is available for this group. Refresh account models or restrict the group to priced models, then retry.',
  allocationUnknownWindowOverride:
    'A personal limit refers to a time-window condition that no longer exists. Refresh this rule and retry.',
  allocationRatioTokensBalanceHint:
    'Each token limit equals its window’s total token budget × assigned percentage. This is an internal allowance; upstream quota may run out first.',
  allocationRatioAmountBalanceHint:
    'Each amount limit equals its window’s total internal USD budget × assigned percentage. This is not an upstream cash balance.',
  allocationNoBalances:
    'No balances yet. Usage begins with requests made after activation.',
  allocationLimit: 'Allowance',
  allocationUsed: 'Used',
  allocationRemaining: 'Remaining',
  allocationResetAt: 'Resets {{date}}',
  allocationActualTokens: '{{tokens}} M tokens',
  allocationCorrect: 'Correct usage',
  allocationSettle: 'Confirm settlement',
  allocationSettlementHint:
    'Correct only with evidence: enter the request’s total input, output and cached-input tokens. Corrections are audited and cannot be undone here.',
  allocationInputM: 'Input tokens · M',
  allocationOutputM: 'Output tokens · M',
  allocationCachedM: 'Cached input · M',
  allocationMoreMembers: 'Load more members',
  allocationMine: 'My resource allowances',
  allocationRefreshMine: 'Refresh my resource allowances',
  allocationMineEmpty: 'No resource allowances assigned.',
  allocationKeyChoice: 'Resource allowance: {{scheme}} · {{pool}}',
  auditAllocationSave: 'Saved resource allowance',
  auditAllocationSettle: 'Settled resource allowance usage',
  auditAllocationReconcile: 'Confirmed external quota usage',
  auditAllocationDelete: 'Deleted resource allowance',

  periodDaily: 'Daily · {{zone}}',
  periodMonthly: 'Monthly · {{zone}}',
  auditMemberBudget: 'Updated token budget',
  auditMemberBudgetSettle: 'Settled token usage',
  reasonTokenQuota:
    'Your token budget is exhausted. Wait for the reset or contact an administrator.',
  reasonTokenPending:
    'A previous request needs token settlement. Contact an administrator.',
  reasonTokenAccounting:
    'Token accounting is unavailable. Contact an administrator.',
  reasonAllocationAccounting:
    'Resource allowance accounting is unavailable. Contact an administrator.',

  requestFilters: 'Filters',
  requestClearFilters: 'Clear filters',
  requestApplyFilters: 'Apply filters',
  requestModelFilter: 'Model name',
  requestExactModel: 'Exact model name',
  requestIDMissing: 'No request ID was recorded for this call.',
  requestID: 'Request ID',
  requestKeyFilter: 'API key filter',
  requestAllKeys: 'All API keys',
  requestMember: 'Member',
  requestMemberFilter: 'Member filter',
  requestAllMembers: 'All members',
  requestFrom: 'From (local time)',
  requestUntil: 'Until (local time)',
  requestInvalidRange: 'The end time must be later than the start time.',
  requestFiltersFailed: 'Could not load member and key options.',
  requestFilterHint:
    'Search retained requests from the last 7 days. Names match exactly.',
  requestFirstTokenValue: 'First output: {{value}}',
  requestReasoningEffort: 'Reasoning effort',
  requestReasoningEffortValue: 'Reasoning effort: {{value}}',
  requestFirstToken: 'First output latency',
  requestFirstTokenHint:
    'Time from gateway processing to the first text, reasoning, or tool argument delta. Not measured when no output delta is observed.',
  requestViewDetails: 'View request details',
  requestDetails: 'Request details',
  requestDetailsHint:
    'Use the request ID to locate this call. Prompt and response content are not recorded.',
  requestTransport: 'Connection',
  requestReason: 'Reason',
  requestUpstreamStatus: 'Upstream HTTP status',
  requestCopyID: 'Copy request ID',
  requestCopyFailed: 'Copy failed. Select the ID to copy it manually.',
  accountQuotaExhausted: 'Quota exhausted',
  reasonQuotaExhausted:
    'Subscription quota exhausted; retry after the next quota check.',
  accountQuotaRoutingHint:
    'Fresh quota confirms exhaustion. New sessions use another available account; existing sessions keep their account.',

  auditSettingsUpdate: 'Updated instance settings',
  auditSettings: 'Instance settings',
  backupExportPassword: 'Export password',
  backupConfirmPassword: 'Confirm backup password',
  backupPassword: 'Backup password',
  backupPasswordHint:
    'Use 12–128 characters. Keep the password separately; it cannot be reset if lost.',
  backupPasswordMismatch: 'The backup passwords do not match.',
  backupPasswordOptional:
    'Required for encrypted backups. Leave blank for historical unencrypted archives.',
  backupPasswordRequired: 'Enter the backup password.',
  backupDecryptFailed:
    'The password is incorrect, or the encrypted backup is damaged or unsupported.',
  backupEncrypted: 'Password-encrypted',
  backupLegacy: 'Unencrypted legacy backup',
  backupLegacyWarning:
    'This legacy archive contains readable credential keys. Keep it private and create a new encrypted backup after restoring.',
  backupRestartKey:
    'Move the recovered credentials.key to a new private path outside the data directory and update SUBLANE_CREDENTIALS_KEY_FILE before restarting.',
  backupKeySeparated:
    'The credential key is stored separately from the data directory.',
  backupKeyLegacy:
    'The credential key uses the legacy data-directory location. Independent storage can be configured on the server.',
  backupTitle: 'Backup and restore',
  backupExportTitle: 'Create a backup',
  backupExportDescription:
    'Export a password-encrypted database snapshot and its credential key while SubLane is running. Keep the password separately from the backup.',
  backupDownload: 'Download backup',
  backupExporting: 'Creating backup…',
  backupRestoreTitle: 'Restore from a backup',
  backupRestoreDescription:
    'Verify your archive, then prepare a new data directory. Restart SubLane with that directory to activate the restored data.',
  backupFile: 'Backup file',
  backupFileLimit:
    'SubLane encrypted .age or legacy .tar.gz archive, up to {{size}} MiB. Use the CLI for larger files.',
  backupVerify: 'Verify backup',
  backupVerifying: 'Verifying…',
  backupRestore: 'Prepare restore',
  backupRestoring: 'Preparing restore…',
  backupVerified: 'Backup verified',
  backupCreatedAt: 'Created',
  backupVersion: 'SubLane version',
  backupDatabaseSize: 'Database size',
  backupRestored: 'Restored data is ready',
  backupRestartStop: 'Stop the current SubLane service.',
  backupRestartDirectory: 'Set SUBLANE_DATA_DIR to the directory shown above.',
  backupRestartStart: 'Restart SubLane and sign in again.',
  backupSessionNotice:
    'The original data is retained. Restored browser sessions are cleared; accounts and API keys return to their state at backup time.',
  backupDestinationExists:
    'The restore directory already exists and cannot be overwritten. Inspect it on the server or use the CLI to choose another new directory.',
  backupInvalid:
    'This archive could not be verified. Check its integrity, encryption key and SubLane version compatibility.',
  backupTooLarge:
    'This file exceeds the browser backup limit. Use the command-line backup tools.',
  backupBusy:
    'Another backup operation is running. Try again when it finishes.',
  backupFailed:
    'The backup operation failed. Check server storage and permissions, then try again.',
  backupAuditFailed:
    'Recovery data was prepared, but its audit record could not be saved. Inspect the restore directory on the server before retrying.',
  backupLoadFailed: 'Could not load backup settings.',
  auditBackupExport: 'Exported backup',
  auditBackupPrepare: 'Prepared restored data',
  auditBackupVerify: 'Verified backup',
  systemSettings: 'System settings',
  systemSettingsDescription: 'Applies to every workspace.',
  workspaceSettingsDescription: 'Applies only to the current workspace.',
  timeZoneTitle: 'Time zone',
  timeZoneDescription:
    'Controls displayed timestamps, reporting dates, and daily/monthly resource allowance resets across this instance.',
  timeZoneLabel: 'Instance time zone',
  timeZoneHint:
    'Enter an IANA time zone, such as Asia/Shanghai. Changes take effect immediately.',
  timeZonePreview: 'Current time: {{time}} ({{zone}})',
  timeZoneInvalid: 'Enter a valid IANA time zone.',
  timeZoneSaveFailed:
    'Could not save the time zone. The current setting is unchanged.',
  timeZoneSaved: 'Time zone saved.',
  timeZoneSave: 'Save time zone',
  clientVersionsTitle: 'Client versions',
  claudeVersionTitle: 'Claude client version',
  claudeVersionDescription:
    'Instance-wide settings for Claude subscription requests. New requests use the selected version; requests already running retain their original version.',
  claudeVersionAutoHint:
    'Check the official Claude Code npm package’s stable channel every 6 hours. Failed checks keep the last known version.',
  grokVersionTitle: 'Grok client version',
  grokVersionDescription:
    'Instance-wide settings for Grok subscription requests and quota queries. Version checks read official package metadata.',
  grokVersionAutoHint:
    'Check the official Grok npm package’s latest stable release every 6 hours. Failed checks keep the last known version.',
  codexVersionTitle: 'Codex client version',
  codexVersionDescription:
    'Instance-wide settings for Codex model discovery and requests. Version checks read official release metadata; they do not install software.',
  clientVersionManual: 'Manual version',
  clientVersionManualHint:
    'Leave blank to use the last synchronized stable version or the built-in default. A manual version always takes priority.',
  clientVersionAuto: 'Automatically follow stable releases',
  codexVersionAutoHint:
    'Check official Codex releases on GitHub every 6 hours. Failed checks keep the last known version.',
  clientVersionAutoOff:
    'Automatic checks are off. The last synchronized version is kept.',
  clientVersionEffective: 'Effective version',
  clientVersionLatest: 'Last synchronized stable version',
  clientVersionAutomatic: 'Automatic · built-in {{version}}',
  clientVersionManualSource: 'Manual',
  clientVersionSyncedSource: 'Synchronized',
  clientVersionBuiltinSource: 'Built-in',
  clientVersionSave: 'Save settings',
  clientVersionCheck: 'Check now',
  clientVersionChecking: 'Checking official releases…',
  clientVersionChecked: 'Last check: {{time}}',
  clientVersionNextCheck: 'Next automatic check: {{time}}',
  clientVersionNeverChecked: 'No version check has completed yet.',
  clientVersionInvalid: 'Enter a stable version in major.minor.patch format.',
  clientVersionLoadFailed: 'Could not load client version settings.',
  clientVersionSaveFailed:
    'Could not save version settings. The current configuration is unchanged.',
  clientVersionSyncFailed:
    'The version check failed. The last known version is still active.',
  clientVersionCooldown: 'Please wait before checking for another release.',
  clientVersionSaved: 'Settings saved.',

  reasonModelUnavailable: 'No account supports this model',
  reasonCatalogUnavailable: 'Model catalog unavailable',

  catalogModels: 'Models',
  modelTest: 'Test',
  modelTestFor: 'Test {{model}}',
  modelTesting: 'Testing…',
  modelTestingFor: 'Testing {{model}}…',
  modelTestHint:
    'Each test sends a real request and uses a small amount of upstream quota. It times out after 30 seconds. Results apply to this resource at the time of testing.',
  modelTestSuccess: 'Responded successfully',
  modelTestResult: '{{result}} · {{duration}} ms',
  catalogFor: 'Models for {{name}}',
  catalogHiddenModel: 'Hidden model',
  catalogAvailableModels: 'Available models',
  catalogAccountDescription:
    'Models discovered from this account. This catalog describes reported support, not remaining quota or a successful generation test.',
  catalogGroupDescription:
    'Automatically combined from enabled resources and filtered by the group’s model policy.',
  catalogCount_one: '{{count}} model',
  catalogCount_other: '{{count}} models',
  catalogUnknown: 'Unknown',
  catalogUnknownHint:
    'Model support is unknown. Refresh to retrieve the account catalog.',
  catalogRefresh: 'Refresh models',
  catalogReload: 'Refresh list',
  catalogLoading: 'Loading models…',
  catalogRefreshing: 'Synchronizing account catalogs…',
  catalogRefreshFailed: 'Refresh failed. Showing the last saved catalog.',
  catalogLoadFailed: 'Could not retrieve the model catalog. Try again later.',
  catalogPartial:
    'Some account catalogs are unavailable. This list may be incomplete.',
  catalogStale: 'Showing a saved catalog while checking for updates.',
  catalogExpired:
    'This saved catalog is too old to use for routing. Refresh to restore model availability.',
  catalogEmptyAccount: 'This account reported no visible models.',
  catalogEmptyGroup: 'No models are currently available to this group.',
  catalogSearch: 'Search models',
  catalogNoMatch: 'No matching models.',
  catalogNarrowSearch:
    'Showing the first 200 matches. Refine your search to find a model.',
  catalogUpdated: 'Updated {{time}}',
  catalogPickerHint: 'Search and select a model available to this key’s group.',
  catalogSelectionRequired: 'Choose a model from the available catalog.',

  ccSwitchImport: 'Import into CC Switch',
  ccSwitchImportNamed: 'Import {{name}} into CC Switch',
  ccSwitchDescription:
    'Choose a client and model for this API key, then confirm the import in CC Switch.',
  ccSwitchClient: 'Client',
  ccSwitchApp_codex: 'Codex',
  ccSwitchApp_opencode: 'OpenCode',
  ccSwitchApp_claude: 'Claude Code',
  ccSwitchApp_openclaw: 'OpenClaw',
  ccSwitchApp_hermes: 'Hermes',
  ccSwitchApp_gemini: 'Gemini CLI',
  ccSwitchApp_grokbuild: 'Grok Build',
  ccSwitchClaudeHint:
    'Haiku, Sonnet, and Opus will all use the selected group model.',
  ccSwitchGeminiHint:
    'Gemini CLI uses API key authentication. Token counting and native Gemini model discovery are not available; test a conversation after import.',
  ccSwitchGrokHint:
    'After import, check that the context window in CC Switch matches the selected model.',
  ccSwitchName: 'Configuration name',
  ccSwitchModelHint:
    'Enter an exact model ID allowed by this key’s group. Use the provider prefix when required.',
  ccSwitchInputInvalid:
    'Use a configuration name of 1–128 characters without control characters and a valid model ID.',
  ccSwitchPrivacy:
    'The selected key and connection details will be passed to CC Switch on this device. The imported provider will not be activated automatically.',
  ccSwitchReady:
    'Ready to open CC Switch. Confirm the configuration there, then enable it when needed.',
  ccSwitchOpened:
    'Confirm the import in CC Switch. If it did not open, check that the app is installed and try again.',
  ccSwitchOpenFailed:
    'Could not open CC Switch. Check that it is installed, then try again.',
  ccSwitchKeyUnavailable:
    'Use an enabled, unexpired key with access to its group.',
  ccSwitchPrepare: 'Prepare import',
  ccSwitchOpen: 'Open CC Switch',
  ccSwitchEdit: 'Edit configuration',

  magpieImport: 'Import into Magpie',
  magpieImportNamed: 'Import {{name}} into Magpie',
  magpieDescription:
    'Add this API key and gateway to Magpie. Models are discovered automatically from this key’s group.',
  magpiePrivacy:
    'This key and connection details will be passed to Magpie on this device. Confirm the import in Magpie.',
  magpieReady: 'Ready to open Magpie and confirm the import.',
  magpieOpened:
    'Confirm the import in Magpie. If it did not open, check that Magpie 0.1.8 or later is installed and try again.',
  magpieOpenFailed:
    'Could not open Magpie. Check that Magpie 0.1.8 or later is installed, then try again.',
  magpieKeyUnavailable:
    'Use an enabled, unexpired key with access to its group.',
  magpiePrepare: 'Prepare import',
  magpieOpen: 'Open Magpie',

  copyKeyNamed: 'Copy {{name}}',
  keyLegacyCopy:
    'This older key cannot be copied again. Create a new key if you need its full value.',
  retrievingKey: 'Retrieving key…',
  keyRetrieveFailed: 'Could not retrieve this key. Try again.',
  copyKeyTitle: 'Copy API key',
  keyManualCopy:
    'Clipboard access is unavailable. Select and copy the key below.',
  auditKeyReveal: 'Retrieved API key',

  keyPaused: 'Paused',
  editKey: 'Edit API key',
  edit: 'Edit',
  editKeyNamed: 'Edit {{name}}',
  editKeyDescription:
    'Update access without changing the key or its resource group.',
  keyEnabled: 'Key enabled',
  keyPauseHint:
    'Pausing blocks new requests. You can enable this key again later.',
  keyExpiry: 'Expires',
  keyNeverExpires: 'Never expires',
  keyKeepExpiry: 'Keep current expiry',
  keyExpiryDays: 'In {{count}} days',
  keyExpiryDays_one: 'In {{count}} day',
  keyExpiryDays_other: 'In {{count}} days',
  keyExpiresOn: 'Expires {{date}}',
  keyExpired: 'Expired',
  keyUpdateFailed: 'Could not update the key. Check the values and try again.',
  keyAlreadyRevoked:
    'This key has already been revoked. Refresh the list to see its latest state.',
  groupModelPolicy: 'Model access',
  groupRestrictModels: 'Limit allowed models',
  groupAllowedModels: 'Allowed model IDs',
  groupModelsHint:
    'One exact model ID per line, up to 100. Use IDs from /v1/models without a provider prefix. Existing provider-specific rules retain their scope; wildcards are not supported.',
  groupNoModels: 'No models are allowed while this list is empty.',
  groupAllModelsHint:
    'All models available through this group can be used, including models added later.',
  groupAllModels: 'All models',
  groupRestrictedModels: 'Model allowlist',
  reasonModelNotAllowed: 'Model not allowed by this group',
  auditLog: 'Audit log',
  auditResourceFilter: 'Resource type',
  auditAllResources: 'All resources',
  auditMember: 'Member',
  auditUser: 'User',
  auditAccount: 'Upstream account',
  auditActor: 'Performed by',
  auditAction: 'Action',
  auditTarget: 'Target',
  auditSuccess: 'Succeeded',
  auditFailure: 'Failed',
  auditLocal: 'Local maintenance',
  auditLoading: 'Loading audit records…',
  auditLoadFailed: 'Could not load audit records. Try again.',
  auditEmpty: 'No audit records',
  auditEmptyHint: 'Management actions matching these filters will appear here.',
  auditRetention:
    'Retained for 90 days, up to 10,000 records. Passwords, credentials and request content are never recorded.',
  auditKeyCreate: 'Created API key',
  auditKeyUpdate: 'Updated API key',
  auditKeyRevoke: 'Revoked API key',
  auditGroupCreate: 'Created resource group',
  auditGroupUpdate: 'Updated resource group',
  auditMemberCreate: 'Created member',
  auditMemberUpdate: 'Changed member status',
  auditMemberGroups: 'Changed group access',
  auditMemberLimits: 'Changed member limits',
  auditMemberPassword: 'Reset member password',
  auditInvitationCreate: 'Created invitation link',
  auditUserPassword: 'Changed password',
  auditUserRecover: 'Recovered administrator',
  auditAccountCreate: 'Added upstream account',
  auditAccountAuthorize: 'Updated account credentials',
  auditAccountUpdate: 'Changed account status',
  auditAccountDelete: 'Deleted upstream account',
  auditAccountConcurrency: 'Changed account concurrency',
  auditAccountResume: 'Cleared account cooldown',
  auditAccountProxy: 'Changed account network proxy',
  auditProxyCreate: 'Created network proxy',
  auditProxyUpdate: 'Updated network proxy',
  auditProxyDelete: 'Deleted network proxy',
  auditProxyImport: 'Imported network proxies',
  auditProxyPrune: 'Deleted failed network proxies',

  activityTitle: 'Activity by time',
  activityGrid: 'Weekday and hour activity',
  activityNotCollected: 'Not collected',
  activityCoverage:
    'Hourly totals collected since {{time}} ({{zone}}); earlier hours are not included.',
  activityKeys:
    'Use arrow keys to move between hours and weekdays. Home and End move within a row; Control plus Home or End moves to the first or last cell.',
  activitySamples: 'Matching hourly windows: {{count}}',
  activityHint: 'Hover, tap or use arrow keys to inspect a time slot.',
  activityLess: 'Less',
  activityMore: 'More',
  activityUnknown: 'Unknown',
  usageInputTokens: 'Input tokens',
  usageOutputTokens: 'Output tokens',
  usageMetric: 'Chart metric',
  usageChart: 'Usage chart',
  usageChartHint: 'Hover or tap to inspect a day.',
  usageChartKeys:
    'Use Left and Right arrow keys to inspect days. Home and End select the first and last day.',
  usageChartGaps: 'Missing token reports appear as gaps.',
  usageNoTokenReports:
    'No values were reported for this metric in this period.',
  usageTokenNotReported: 'Not reported',
  usageRankHint: 'The 20 most active entries, ordered by request count.',
  usageViewDetails: 'View details',
  usageHideDetails: 'Hide details',
  changePassword: 'Change password',
  currentPassword: 'Current password',
  newPassword: 'New password',
  resetPassword: 'Reset password',
  resetPasswordFor: 'Reset password for {{username}}',
  savePassword: 'Save password',
  passwordSessionNotice:
    'Saving signs this user out of all browser sessions. API keys stay valid.',
  currentPasswordInvalid: 'The current password is incorrect.',
  passwordChangedRetry:
    'The password or account status changed. Sign in again and retry.',
  passwordRateLimited:
    'Too many password attempts. Wait a minute and try again.',
  passwordSaveFailed: 'Could not save the password. Please try again.',
  memberPasswordReset: 'Password reset for {{username}}.',
  memberActionsFor: 'More actions for {{username}}',
  memberLimits: 'Request limits',
  memberLimitsFor: 'Request limits for {{username}}',
  memberLimitsDescription:
    'All API keys and groups used by this member share these limits. Changes apply to new model requests.',
  memberLimitsHint:
    '0 means unlimited. Requests per minute: 0–6,000; concurrency: 0–1,000 (default: 10). Active calls are allowed to finish.',
  memberLimitsInvalid:
    'Enter whole numbers: 0–6,000 requests per minute and 0–1,000 concurrent requests.',
  memberLimitsFailed:
    'Could not load or save request limits. Please try again.',
  loadingMemberLimits: 'Loading request limits…',
  requestsPerMinute: 'Requests per minute',
  memberConcurrency: 'Concurrent requests',
  saveChanges: 'Save changes',
  unlimited: 'Unlimited',
  yourRequestLimits: 'Your request limits',
  personalLimitsNotice:
    'Minute limits reset at each minute boundary (UTC) and count admitted model attempts. Shared gateway and account limits still apply.',
  reasonMemberBusy: 'Member concurrency limit reached',
  reasonMemberRate: 'Member request rate limit reached',
  usageSummary: 'Usage',
  yourUsage: 'Your usage',
  teamUsage: 'Team usage',
  usageKey: 'Usage key',
  usageAllKeys: 'All keys',
  usagePeriod: 'Usage period',
  usageToday: 'Today',
  usageLastDays: 'Last {{days}} days',
  usageBreakdown: 'Breakdown',
  usageDaily: 'Daily',
  usageModels: 'Models',
  usageMembers: 'Members',
  loadingUsageSummary: 'Loading usage summary…',
  usageSummaryFailed: 'Could not load usage summary. Please try again.',
  usageTrackingSince:
    'Collecting since {{time}} ({{zone}}). Earlier calls are not included.',
  usageRequests: 'Requests',
  usageCompletionRate: 'Completion rate',
  usageAverageDuration: 'Average duration',
  usageOutcomes:
    'Completed {{completed}} · Incomplete {{incomplete}} · Failed {{errors}} · Canceled {{canceled}} · Rejected {{rejected}}',
  usagePartialTokens:
    'Some calls did not report token usage; totals include reported values only.',
  usageReportedOnly: 'Reported values only',
  statisticsUnknown: 'Unavailable',
  usageOtherModels: 'Other models',
  noUsageSummary: 'No usage in this period',
  noUsageSummaryHint:
    'Completed model attempts will appear here after usage collection begins.',
  usageRetentionNote:
    'Daily summaries use {{zone}} calendar dates and are retained for 90 days, independently of recent request records. Breakdowns show the top 20 entries. These are operational statistics, not billing totals.',
  gatewayAttentionDescription:
    'Verify or reauthorize a subscription account to make the gateway available.',
  gatewayChecking: 'Checking gateway…',
  needsConnection: 'Not connected',
  gatewayStatusUnknown: 'Connection status unknown',
  clientConfigLocation:
    'Set these values in your user-level ~/.codex/config.toml, replacing existing values with the same names.',
  connectionStatusError: 'Could not check gateway availability.',
  addAccount: 'Add account',
  accountName: 'Account name',
  connectAccountTitle: 'Connect subscription',
  channelsTitle: 'API channels',
  addChannel: 'Add API channel',
  channelsDescription:
    'Manage API endpoints, keys and models. Add channels to resource groups to grant client access.',
  channelPlatformOpenAI: 'OpenAI',
  channelPlatformAnthropic: 'Anthropic',
  channelPlatformGemini: 'Gemini',
  channelPlatformDeepSeek: 'DeepSeek',
  channelPlatformOpenRouter: 'OpenRouter',
  channelPlatformOpenCode: 'OpenCode Zen',
  channelPlatformOpenCodeGo: 'OpenCode Go',
  channelProtocolChat: 'Chat Completions',
  channelProtocolResponses: 'OpenAI Responses',
  channelProtocolAnthropic: 'Anthropic Messages',
  channelProtocolGemini: 'Gemini',
  channelPlatform: 'Platform',
  channelProtocol: 'Upstream protocol',
  channelPlatformCustom: 'Custom',
  channelPlatformHint:
    'Choose a platform preset, or choose Custom to set the upstream protocol. You can adjust the base URL.',
  channelPlatformXAI: 'xAI',
  channelPlatformKimi: 'Kimi',
  channelPlatformKimiCN: 'Kimi (CN)',
  channelPlatformZhipu: 'Zhipu GLM',
  channelPlatformZAI: 'Z.ai',
  channelPlatformMiniMax: 'MiniMax',
  channelPlatformMiniMaxCN: 'MiniMax (CN)',
  channelPlatformQwen: 'Qwen',
  channelPlatformQwenCN: 'Qwen (CN)',
  channelPlatformMistral: 'Mistral',
  channelPlatformGroq: 'Groq',
  channelPlatformTogether: 'Together AI',
  channelPlatformFireworks: 'Fireworks',
  channelPlatformSiliconFlow: 'SiliconFlow (CN)',
  channelPlatformSiliconFlowGlobal: 'SiliconFlow (Global)',
  channelPlatformNvidia: 'NVIDIA NIM',
  channelPlatformModelScope: 'ModelScope',
  channelPlatformAzure: 'Azure OpenAI',
  channelPlatformVolcengine: 'Volcengine Ark',
  channelPlatformBaidu: 'Baidu Qianfan',
  channelPlatformTencent: 'Tencent TokenHub',
  channelPlatformStepFun: 'StepFun',
  channelPlatformXiaomi: 'Xiaomi MiMo',
  channelBack: 'Back to API channels',
  channelPlatformSearch: 'Search platforms',
  channelPlatformNoMatch: 'No matching platforms.',
  channelProtocolSearch: 'Search protocols',
  channelProtocolNoMatch: 'No matching protocols.',
  channelName: 'Channel name',
  channelNameInvalid:
    'Enter a channel name of 1–64 characters without control characters.',
  channelsEmpty: 'No API channels',
  channelsEmptyHint:
    'Connect an OpenAI-compatible service with its API key and base URL. Subscription accounts are managed separately.',
  loadingChannels: 'Loading API channels…',
  channelsLoadFailed: 'Could not load API channels.',
  channelBackendMissing:
    'This backend does not support API channels. Start an updated backend and reconnect.',
  channelProtocolOpenAI: 'OpenAI Chat Completions compatible',
  channelNeedsKey: 'Replace key',
  channelKeyHint:
    'The upstream key is encrypted and never returned. Verify the channel, then add it to a resource group.',
  channelExists:
    'This API channel is already configured. Replace the key on the existing channel.',
  channelNotFound:
    'This API channel is no longer available. Reload the channel list.',
  verifyChannelConnection: 'Verify connection for {{name}}',
  channelActions: 'Actions for {{name}}',
  channelVerifiedModels:
    '“{{name}}” is connected. Available models: {{count}}.',
  channelRuntimeFailed: 'Could not read channel activity.',
  channelRuntimeUnknown: 'Channel activity is not available yet.',
  channelAssignGroup: 'Assign to a resource group',
  resourceAssigned: 'Assigned to {{count}} groups',
  resourceUnassigned: 'Unassigned',
  deleteChannelTitle: 'Remove API channel',
  deleteChannelDescription:
    'Remove “{{name}}” and its stored key. Existing conversations on this channel will stop.',
  deleteChannel: 'Remove channel',
  disableChannel: 'Disable channel',
  enableChannel: 'Enable channel',
  catalogChannelDescription:
    'Models reported by this API channel. Refresh to check its current catalog.',
  groupChannelsHint:
    'Select the API channels available to this resource group.',
  groupRouting: 'Routing preference',
  routingAPIOnly: 'Use selected API channels',
  routingSubscriptionsOnly: 'Use selected subscriptions',
  routingSubscriptionFirst: 'Subscriptions first',
  routingAPIFirst: 'API channels first',
  routingProtocol: 'Prefer the client protocol',
  allowAPIFallback: 'Allow API fallback when subscriptions are unavailable',
  apiFallbackHint:
    'Mixed groups use API channels only when fallback is enabled and no eligible subscription is available. API-only groups use their selected channels directly. Existing conversations keep their source.',
  apiPriorityHint:
    'New conversations prefer an eligible API channel, then a subscription. API calls may consume the upstream service’s balance.',
  protocolRoutingHint:
    'Keep the existing policy: prefer the provider matching the client protocol, then other eligible resources.',
  apiFallbackEnabled: 'API fallback enabled',
  apiFallbackDisabled: 'API fallback off',
  apiUpstream: 'API upstream',
  upstreamKeyExpiry: 'Key expiry',
  upstreamKeyExpiryUnknown: 'Not reported by upstream',
  apiUpstreamProxyHint:
    'Model discovery and generation use the selected server network route.',
  connectAPI: 'Connect API',
  upstreamBaseURL: 'Base URL',
  upstreamAPIKey: 'Upstream API key',
  apiUpstreamDescription:
    'Connect a platform API or a custom service using its API key and base URL.',
  upstreamBaseURLHint:
    'Enter the API base URL, including /v1 or /v1beta when required by the upstream.',
  upstreamEndpointLocked:
    'Key replacement preserves this platform, protocol, endpoint and existing conversations. Create another channel to change them.',
  upstreamAPIKeyHint:
    'The upstream key is encrypted and never returned. After connecting, verify the connection and add the account to a group.',
  upstreamAPIKeyInvalid: 'Enter a valid API key without whitespace.',
  upstreamBaseURLInvalid:
    'Enter an HTTP or HTTPS base URL without login credentials, query parameters or a fragment.',
  apiUpstreamUsageHint:
    'Request and token usage is tracked by SubLane. Upstream quota is shown for OpenCode Go channels; other API channels do not report subscription quota or balances.',
  apiKeyReplacementHint:
    'The upstream rejected this key. Replace the API key, then verify the connection.',
  connectionMethod: 'Connection method',
  accountOAuth: 'Browser authorization',
  accountImport: 'Import auth.json',
  startAuthorization: 'Start authorization',
  openAuthorization: 'Open provider authorization',
  copyAuthorization: 'Copy authorization link',
  callbackURL: 'Callback URL',
  callbackHint:
    'After signing in, the localhost page may not load. Copy its complete address from the browser address bar and paste it here.',
  completeAuthorization: 'Complete authorization',
  deviceAuthorizationInstructions:
    'Open the Grok authorization page and approve the device code. This window will connect your account automatically.',
  deviceCode: 'Device code',
  deviceAuthorizationWaiting: 'Waiting for approval in your browser…',
  authorizationInstructions:
    'Open the provider authorization page, sign in, then return here with the complete callback URL.',
  startAgain: 'Start again',
  accountImportDescription:
    'Choose an existing Codex auth.json file or paste its contents. Stored credentials are encrypted on this instance.',
  authJSONContents: 'auth.json contents',
  authJSONFile: 'auth.json file',
  chooseAuthFile: 'Choose auth.json',
  fileTooLarge: 'The file must be 64 KiB or smaller.',
  fileReadFailed:
    'Could not read this file. Choose it again or paste its contents.',
  importAccount: 'Import account',
  loadingAccounts: 'Loading subscription accounts…',
  accountsLoadFailed: 'Could not load subscription accounts.',
  accountActions: 'Actions for {{name}}',
  checkAccount: 'Verify connection',
  checkingAccount: 'Verifying…',
  reauthorizeAccount: 'Reauthorize',
  disableAccount: 'Disable account',
  enableAccount: 'Enable account',
  deleteAccount: 'Remove account',
  deleteAccountTitle: 'Remove subscription account',
  deleteAccountDescription:
    'Remove “{{name}}” and its stored credentials? Conversations using this account will need a new session. This does not revoke your provider login.',
  accountVerified: 'Verified',
  accountUnverified: 'Not verified',
  accountNeedsAuth: 'Reauthorization needed',
  accountExpires: 'Token expiry',
  accountUnknownExpiry: 'Checked on first use',
  accountNameInvalid: 'Use 1–64 characters without control characters.',
  accountJSONInvalid:
    'Paste valid credential JSON for the selected provider. Official API keys are not supported here.',
  accountInvalidInput:
    'Check the account name, provider, and upstream credentials.',
  accountExists:
    'This upstream is already added. Update credentials on the existing account.',
  accountIdentityMismatch:
    'Reauthorize with the same provider and account. Add a separate account for a different subscription.',
  accountLimitReached: 'This workspace supports up to 100 resources.',
  accountDisabledHint: 'Enable this account before verifying or using it.',
  accountReauthorizeHint: 'This account needs new credentials.',
  accountRefreshFailed:
    'Could not refresh this account. Check network access or reauthorize it.',
  oauthStateInvalid:
    'This authorization expired or was already used. Start a new authorization.',
  oauthCallbackInvalid:
    'Paste the full localhost callback URL from this authorization attempt.',
  oauthAccessDenied:
    'Authorization was declined. Start again when you are ready.',
  accountBusy: 'The gateway is busy. Wait a moment and try again.',
  accountUpstreamUnavailable:
    'Could not reach the provider. Check network access and try again.',
  accountCheckFailed:
    'The provider could not complete the request. Try again or reauthorize this account.',
  accountRateLimited:
    'The provider is rate limiting this account. Try again later.',
  accountActionFailed: 'Could not complete this action. Please try again.',
  accountVerificationResult:
    '“{{name}}” is connected. Available models: {{count}}.',
  accountReadyDescription:
    'A verified subscription account is available for gateway requests.',
  gatewayNeedsAttention: 'Needs attention',
  gatewayReadyDescription:
    'Your gateway is ready. Connect a client using a personal API key.',
  gatewayKeyNeeded: 'Key needed',
  gatewayKeyNeededDescription:
    'Create or reactivate a key for a ready resource group.',
  gatewayNoKey: 'No usable key',
  gatewayKeySetupDescription:
    'Create a personal key for a ready resource group before your first request.',
  clientGuideTitle: 'Connect your client',
  clientGuideDescription:
    'Choose your client or API format and connect with a personal SubLane API key.',
  clientProtocol: 'Client protocol',
  clientProtocolCodex: 'Codex (Responses)',
  clientProtocolOpenAI: 'Cline / OpenAI compatible',
  clientClineInstructions:
    'In Cline settings, select OpenAI Compatible. Enter the API base URL and model ID below, then paste your personal SubLane key into API Key. If Plan and Act use separate configurations, configure both.',
  clientProtocolClaude: 'Claude Messages',
  clientProtocolGemini: 'Gemini API',
  clientRequestExample: 'Request example',
  clientRequestInstructions:
    'Set SUBLANE_API_KEY in your shell, then run this request with a model from your key’s group.',
  clientNativeGuideNote:
    'Tool, thinking and cache features depend on the model and provider. These examples use the native API; they do not configure the provider’s desktop app.',
  clientEndpoint: 'API base URL',
  clientConfig: 'Codex configuration',
  clientGuideNote:
    'Set SUBLANE_API_KEY in the client process environment, and choose a model available to your connected subscriptions. The desktop app must inherit the same environment variable.',
  copyClientConfig: 'Copy configuration',
  clientModel: 'Model ID',
  clientModelHint:
    'Use the model ID returned by GET /v1/models directly. SubLane selects an available account in your group.',
  connectionNotConfigured:
    'Ask an administrator to connect and verify a subscription account first.',
  checkingSession: 'Checking your session…',
  setupWelcome: 'Welcome to SubLane',
  setupWelcomeDescription: 'Set up your workspace in two steps.',
  setupAccountStep: 'Create your administrator',
  setupAccountDetail: 'Set your login for this instance.',
  setupWorkspaceStep: 'Create your first workspace',
  setupWorkspaceDetail: 'Name the space for accounts, members, keys and usage.',
  startSetup: 'Start setup',
  backToWelcome: 'Back to welcome',
  setupBackToAdmin: 'Back to administrator',
  setupProgress: 'Step {{current}} of 2',
  setupContinue: 'Continue',
  setupFirstWorkspace: 'Create your first workspace',
  setupFirstWorkspaceDescription:
    'This name appears in the switcher. Your account and workspace are created together.',
  setupFinish: 'Create workspace',
  setupFinishing: 'Creating workspace…',
  createAdministrator: 'Create administrator',
  creatingAdministrator: 'Creating administrator…',
  setupDescription:
    'Set your login. Your account and workspace are created after the next step.',
  username: 'Username',
  usernameHint:
    'Use 3–32 letters, numbers, underscores, or hyphens. Start with a letter or number.',
  password: 'Password',
  confirmPassword: 'Confirm password',
  passwordHint: 'Use a password with 8–20 characters.',
  passwordRequired: 'Enter your password.',
  passwordMismatch: 'Passwords do not match.',
  signInTitle: 'Sign in to SubLane',
  signIn: 'Sign in',
  signingIn: 'Signing in…',
  signOut: 'Sign out',
  accountMenu: 'Account menu',
  administrator: 'Administrator',
  owner: 'Owner',
  member: 'Member',
  members: 'Members',
  apiKeys: 'API keys',
  apiKey: 'API key',
  gatewayKeysNotice:
    'Personal keys access models; administrators manage subscription credentials.',
  createKey: 'Create key',
  createKeyTitle: 'Create API key',
  createKeyDescription:
    'Give this key a name so you can recognize where it is used.',
  keyName: 'Name',
  keyNameHint: 'Use 1–64 characters without control characters.',
  keyLimitReached:
    'You can keep up to 20 non-revoked keys, including paused or expired keys. Revoke one before creating another.',
  keyCreateFailed: 'Could not create the key. Please try again.',
  saveKeyTitle: 'Save your API key',
  saveKeyDescription:
    'Copy this key now, or use the copy button in your key list later.',
  copyKey: 'Copy key',
  copied: 'Copied',
  copyKeyFailed:
    'Could not copy automatically. Select the key and copy it manually.',
  done: 'Done',
  loadingKeys: 'Loading API keys…',
  keysLoadFailed: 'Could not load your API keys. Please try again.',
  noKeys: 'No API keys yet',
  noKeysDescription: 'Create a personal key for your client or application.',
  noUnrevokedKeys: 'No unrevoked API keys',
  noUnrevokedKeysDescription:
    'Create a personal key, or turn on Show revoked to view past keys.',
  showRevokedKeys: 'Show revoked',
  keyLastUsed: 'Last used',
  neverUsed: 'Never',
  revoked: 'Revoked',
  revoke: 'Revoke',
  revokeKey: 'Revoke key',
  revokeKeyTitle: 'Revoke API key',
  revokeKeyNamed: 'Revoke {{name}}',
  revokeKeyDescription:
    'Revoke “{{name}}”? Clients using this key will lose access. This cannot be undone.',
  keyRevokeFailed: 'Could not revoke this key. Please try again.',
  keyRevokedNotice: '{{name}} was revoked.',
  manageKeys: 'Manage API keys',
  membersDescription:
    'Disabling a member immediately blocks their keys in this workspace only.',
  addMember: 'Add member',
  createInviteLink: 'Create invite link',
  invitationLink: 'Invitation link',
  inviteLinkHint:
    'This link is valid for 7 days and can be used once. Share it privately; the new member will create their own login.',
  copyInviteLink: 'Copy link',
  inviteCopied: 'Invitation link copied.',
  inviteCopyFailed: 'Could not copy the link. Select and copy it manually.',
  inviteCreateFailed: 'Could not create an invitation. Please try again.',
  inviteJoinTitle: 'Join workspace',
  inviteJoinDescription:
    'Create your account to join the workspace that invited you.',
  inviteInvalid:
    'This invitation is invalid, expired, or already used. Ask an administrator for a new link.',
  inviteRegisterFailed: 'Could not create your account. Please try again.',
  inviteCreateAccount: 'Create account',
  inviteRegistering: 'Creating account…',
  inviteBackToLogin: 'Already have an account? Sign in',
  memberExistingAdd: 'Add existing account',
  memberExistingDescription:
    'Enter an existing login and choose its role in this workspace. If the account is already here, change its role from the member list.',
  memberExistingNameRequired: 'Enter a username.',
  memberExistingNotFound: 'This account does not exist or is disabled.',
  memberExistingAlreadyHere:
    'This account already belongs to this workspace. Use Change workspace role on its row.',
  memberRoleInvalid:
    'Enable this member before assigning the administrator role.',
  memberExistingSaveFailed: 'Could not save workspace access. Try again.',
  memberAccessSaved: 'Workspace access saved for {{username}}.',
  memberRoleChange: 'Change workspace role',
  memberRoleChangeNamed: 'Change role for {{username}}',
  memberRoleChangeDescription: 'Choose the workspace role for {{username}}.',
  memberBasicRoleHint:
    'Can use granted resource groups and manage personal keys and usage.',
  memberAdminRoleHint:
    'Can manage accounts, groups, members, allowances and audit in this workspace.',
  addMemberDescription:
    'Create a member account and share its credentials privately with that person.',
  createMember: 'Create member',
  creatingMember: 'Creating member…',
  memberCreated:
    'Member {{username}} was created. Grant access to a resource group so they can use the gateway.',
  memberUsernameTaken: 'This username is already in use.',
  memberCreateFailed: 'Could not create the member. Please try again.',
  memberUpdateFailed: 'Could not update this member. Please try again.',
  loadingMembers: 'Loading members…',
  membersLoadFailed: 'Could not load members. Please try again.',
  noMembers: 'No members yet',
  noMembersDescription:
    'Create a login or add an existing account, then grant members access to the groups they can use.',
  memberStatus: 'Status',
  memberCreatedAt: 'Created',
  actions: 'Actions',
  disableMember: 'Disable',
  enableMember: 'Enable',
  disableMemberNamed: 'Disable {{username}}',
  enableMemberNamed: 'Enable {{username}}',
  previousPage: 'Previous',
  nextPage: 'Next',
  pagination: 'Pagination',
  paginationRange: 'Showing {{start}}–{{end}} of {{total}}',
  paginationSize: 'Rows per page',
  paginationPage: 'Page {{page}} of {{pages}}',
  paginationNumber: 'Page {{page}}',
  paginationJump: 'Go to page',
  paginationGo: 'Go',
  cancel: 'Cancel',
  role: 'Role',
  active: 'Active',
  disabled: 'Disabled',
  yourAccount: 'Your account',
  memberOverviewTitle: 'Your workspace',
  memberAccessDescription:
    'Create a personal API key and configure your client to use this gateway.',
  accessDenied: 'Access denied',
  adminAccessRequired:
    'This page is available to administrators only. Return to your workspace to continue.',
  signingOut: 'Signing out…',
  signOutError: 'Could not sign out. Please try again.',
  invalidCredentials: 'Username or password is incorrect.',
  authRateLimited: 'Too many attempts. Try again in {{seconds}} seconds.',
  authBusy: 'Other sign-in requests are being processed. Try again shortly.',
  authOriginError:
    'This address is not allowed to sign in. Check the instance public URL.',
  authInvalidInput: 'Check the account details and try again.',
  setupClosed: 'This instance has already been set up. Sign in to continue.',
  overview: 'Overview',
  accounts: 'Subscription accounts',
  preferences: 'Preferences',
  workspace: 'Workspace',
  workspaces: 'Workspaces',
  general: 'General',
  administration: 'Administration',
  foundation: 'Foundation',
  internalGateway: 'Team subscription gateway',
  overviewTitle: 'Workspace overview',
  instanceStatus: 'Instance status',
  gatewaySetup: 'Gateway setup',
  gatewaySetupDescription: 'Connect your team’s first subscription account.',
  adminReadyDescription:
    'Sign-in and management access are set up for this instance.',
  codexSetupDescription:
    'Add a subscription, then verify its connection before inviting your team to use it.',
  teamAccess: 'Gateway keys',
  teamAccessDescription:
    'An active personal key can use a ready resource group.',
  comingNext: 'Coming next',
  planned: 'Planned',
  clientAccess: 'Client access',
  codexCLI: 'Codex CLI',
  codexDesktop: 'Codex desktop',
  clientAccessDescription:
    'Use a personal API key and the client configuration on the API keys page.',
  lastChecked: 'Last checked',
  statusUnknown: 'Status unknown',
  refreshFailed:
    'Showing the last successful check. Current service health could not be verified; refresh to try again.',
  service: 'Service',
  running: 'Running',
  ready: 'Ready',
  notConfigured: 'Not connected',
  version: 'Version',
  uptime: 'Uptime',
  storage: 'Storage',
  refresh: 'Refresh',
  refreshing: 'Refreshing',
  loading: 'Connecting to your instance…',
  connectionError: 'Unable to connect',
  reconnect: 'Reconnect',
  unavailable:
    'The service is unavailable. Check that the backend is running, then try again.',
  invalidResponse:
    'The service returned an unexpected response. Check that the frontend and backend versions match.',
  codexTitle: 'Subscription access',
  viewAccounts: 'View accounts',
  accountsTitle: 'Subscription accounts',
  accountEmptyTitle: 'No subscription accounts',
  accountEmptyDescription:
    'Connect Codex, Claude, Antigravity or Grok with OAuth or credential JSON. Manage API keys and endpoints under API channels.',
  providerDisabled: 'This provider is temporarily disabled.',
  backToOverview: 'Back to overview',
  appearance: 'Appearance',
  theme: 'Theme',
  themeLabel: 'Interface theme',
  light: 'Light',
  dark: 'Dark',
  system: 'System',
  language: 'Language',
  navigation: 'Navigation',
  navigationDescription: 'Navigate between workspace pages.',
  toggleNavigation: 'Toggle navigation',
  close: 'Close',
  skipContent: 'Skip to content',
  notFoundTitle: 'Page not found',
  notFoundDescription: 'This page does not exist or has moved.',
  unexpectedError: 'Something went wrong',
  unexpectedErrorDescription: 'Try opening the workspace again.',
  reload: 'Reload workspace',
  sourceCode: 'Source code',
  sourceDescription:
    'This instance is distributed under AGPL-3.0. Source availability is described in the project README.',
  accountUsage: 'Usage limits',
  accountUsageLabel: 'Usage for {{name}}',
  refreshAccountUsage: 'Refresh usage for {{name}}',
  refreshUsage: 'Refresh usage',
  usageLoading: 'Loading usage…',
  usageUpdated: 'Updated {{time}}',
  usageStale: 'Refresh failed: {{reason}}. Showing the last known usage.',
  usageLoadFailed: 'Unable to load usage: {{reason}}. Try refreshing later.',
  usageReasonRateLimited: 'upstream rate limited',
  usageReasonReauthorize: 'credentials need reauthorization',
  usageReasonCredentialRefresh: 'credential refresh failed',
  usageReasonUnsupported: 'usage is not supported by this provider',
  usageReasonInvalidResponse: 'upstream returned invalid data',
  usageReasonUpstreamRejected: 'upstream rejected the request',
  usageReasonTimeout: 'the request timed out',
  usageReasonUpstreamUnavailable: 'upstream service is unavailable',
  usageReasonBusy: 'the system is busy',
  usageReasonUnknown: 'unknown reason',
  usageNotReported: 'No usage limits reported',
  usageResetCredits: 'Reset cards: {{count}}',
  usageResetCreditsUnknown: 'Unknown',
  usageLimited: 'Currently limited',
  usagePrimary: 'Primary limit',
  usageSecondary: 'Secondary limit',
  usageModel: 'Model quota',
  usageModelsExpand: 'Show all {{count}} models',
  usageModelsCollapse: 'Show fewer models',
  usageDays: '{{count}}-day limit',
  usageHours: '{{count}}-hour limit',
  usageMinutes: '{{count}}-minute limit',
  usageSeconds: '{{count}}-second limit',
  usageUnknown: 'Usage unavailable',
  usageRemaining: '{{percent}}% remaining',
  usageResetUnknown: 'Reset time unavailable',
  usageResetPassed: 'Reset time passed · refresh usage',
  usageResetDays: 'Resets in {{days}}d {{hours}}h',
  usageResetHours: 'Resets in {{hours}}h {{minutes}}m',
  usageResetMinutes: 'Resets in {{minutes}}m',
  usageRetryIn: 'Retry in {{seconds}}s',
  usageRefreshingCached: 'Cached usage is outdated. Refreshing…',
  usageExpired: 'Cached usage is outdated. Refresh to update.',
  verifyAccountConnection: 'Verify connection for {{name}}',
  serviceProvider: 'Service provider',
  providerImportDescription:
    'Import credential JSON exported by CLIProxyAPI for the selected provider. Credentials are encrypted on this instance.',
  accountGroups: 'Resource groups',
  groupsAssignmentHint:
    'New accounts and channels start unassigned. Select resources for a group, then grant member access.',
  accountUnassigned: 'Unassigned',
  accountAssignGroup: 'Assign to a resource group',
  groupsEmpty:
    'No resource groups yet. Create one and select subscription accounts or API channels.',
  createGroup: 'Create resource group',
  editGroup: 'Edit resource group',
  editGroupNamed: 'Edit {{name}}',
  groupName: 'Group name',
  groupEnabled: 'Group enabled',
  groupCounts: 'Resources: {{accounts}} · Members: {{members}}',
  loadingGroups: 'Loading groups…',
  groupsLoadFailed: 'Could not load groups. Try again.',
  groupEditorDescription:
    'Choose subscription accounts and API channels available to keys bound to this resource group.',
  groupAccounts: 'Subscription accounts',
  groupAccountsHint:
    'Accounts stay in any other groups. For exclusive access, keep them in only one group. Removing an account from this group stops conversations using it through this group.',
  groupNoAccounts:
    'Add a subscription account first, or save an empty group and configure it later.',
  saveGroup: 'Save group',
  groupNameTaken: 'A group with this name already exists.',
  groupLimitReached: 'Each workspace supports up to 32 resource groups.',
  groupInputInvalid:
    'Check the group name (1–64 characters), selected accounts and exact model IDs (up to 100).',
  groupSaveFailed: 'Could not save group changes. Try again.',
  groupAccess: 'Group access',
  memberGroupsNamed: 'Manage groups for {{username}}',
  memberGroupsDescription:
    'Choose which groups {{username}} can use when creating API keys.',
  groupGrantRevocationHint:
    'Removing access blocks existing keys for that group on their next request, including new WebSocket turns.',
  memberNoGroupsHint:
    'This member will not be able to create or use gateway keys until a group is granted.',
  saveGroupAccess: 'Save access',
  keyGroup: 'Resource group',
  poolAccountCount: '{{count}} resources',
  keyEmptyPoolWarning:
    'This group has no resources. A new key cannot serve requests until a resource is added.',
  keyGroupHint:
    'This key can only use the selected group. You can change its group later without replacing the key.',
  noAvailableGroups:
    'No resource groups are available. Create one or ask an administrator for access.',
  groupAccessChanged:
    'Group access has changed. Select an available group and try again.',
  keyGroupUnavailable: 'Group unavailable',
  keyAllocationRequired: 'Link allowance',
  keyAllocationHint:
    'This group now uses an allowance. Link this key to continue using it.',
  keyRestoreUnavailable:
    'Ask a workspace administrator to include you in an active allowance, or choose another resource group.',
  restoreKey: 'Restore access',
  restoreKeyNamed: 'Restore access for {{name}}',
  restoreKeyDescription:
    'Link this key to the current allowance for “{{name}}”. Your key and client configuration stay the same.',
  keyRestoredNotice: 'Access restored for “{{name}}”.',
  changeKeyGroup: 'Change group',
  changeKeyGroupNamed: 'Change resource group for {{name}}',
  changeKeyGroupDescription:
    'Choose a resource group you can use in this workspace. Your key stays the same. Subsequent requests use the new group and its allowance; existing usage stays in its original group. Start a new conversation after switching.',
  keyGroupChangedNotice: 'Resource group updated for “{{name}}”.',
  keyGroupChangeFailed: 'Could not update the resource group. Try again.',
  keyGroupUnavailableHint:
    'The selected group or allowance is no longer available. Refresh the choices or contact an administrator.',
  allocationExistingKeysHint:
    'Saving links the selected members’ existing, unrevoked keys in this resource group to this allowance. Key values and client settings stay the same. Existing keys of members outside the allowance will lose access to this group.',
  keyGroupName: 'Group: {{name}}',
  chooseGroup: 'Choose a resource group',
  clientGuideAction: 'Setup guide',
  accountScheduling: 'Scheduling settings',
  accountConcurrencyLimit: 'Concurrent model requests',
  accountConcurrencyHint:
    'Allow 1–1,000 active model requests for this account across all groups. New accounts default to 30. Workspace capacity and queue settings also apply. Lowering the limit lets existing requests finish.',
  accountConcurrencyInvalid: 'Enter a whole number from 1 to 1,000.',
  reasonModelCooling: 'Model cooling down',
  lastAccountFailure: 'Last scheduling failure',
  resumeAccountHint:
    'Clear the cooldown to allow requests again. This does not enable a disabled account or renew its authorization.',
  resumeAccount: 'Clear cooldown',
  accountResumed: 'Cooldown cleared.',
  accountSchedulingFailed: 'Could not update scheduling settings. Try again.',
  saveAccountScheduling: 'Save settings',
  runtimeLoadFailed: 'Scheduling status is temporarily unavailable.',
  accountInFlight: 'Concurrent: {{active}} / {{limit}}',
  accountCooling: 'Scheduling paused',
  accountCoolingReason: 'Reason: {{reason}}',
  accountCoolingHint:
    'Active requests continue. New requests use another account when possible and retry this account after the safety window.',
  accountProbing: 'Recovering',
  accountRetryReady: 'Ready to retry',
  accountRetryAt:
    'Retry after {{time}} (about {{seconds}} seconds); this is a local safety window, not a provider recovery guarantee.',
  requests: 'Requests',
  allRequests: 'All requests',
  yourRequests: 'Your requests',
  personalRequestsDescription:
    'Last 7 days · up to 5,000 records instance-wide. Request and response bodies are not stored.',
  requestKey: 'API key',
  requestGroup: 'Group',
  requestsDescription:
    'Last 7 days · up to 5,000 records. Request and response bodies are not stored.',
  requestAccountFilter: 'Account filter',
  requestResultFilter: 'Result filter',
  allRequestAccounts: 'All accounts',
  allRequestOutcomes: 'All results',
  loadingRequests: 'Loading request records…',
  requestsLoadFailed: 'Could not load request records.',
  noRequests: 'No matching requests',
  noRequestsHint:
    'Records appear after model calls finish. Try another filter or refresh later.',
  requestTime: 'Started',
  requestCaller: 'Member / key',
  requestAccount: 'Account / group',
  requestModel: 'Model',
  requestResult: 'Result',
  requestDuration: 'Duration',
  requestTokens: 'Tokens (in / out)',
  requestEstimatedCost: 'Estimated cost (USD)',
  requestEstimatedCostHint:
    'Calculated from reported tokens and current model prices, including cached input pricing. This is a reference estimate, not a billed amount or an allowance debit. Missing usage or prices show —.',
  requestCachedTokens: 'Cached: {{count}}',
  requestCacheHitRate: 'Hit rate: {{rate}}',
  requestCacheHitRateHint:
    'Cached input tokens divided by total input tokens. — means usage is missing, inconsistent, or the input count is zero.',
  requestDeletedAccount: 'Deleted account',
  requestSuccess: 'Completed',
  requestIncomplete: 'Incomplete',
  requestFailed: 'Failed',
  requestCanceled: 'Canceled',
  requestRejected: 'Rejected',
  reasonRateLimited: 'Rate limited',
  reasonModelRateLimited: 'This model is temporarily rate limited',
  accountModelLimits_one: '{{count}} model limited',
  accountModelLimits_other: '{{count}} models limited',
  reasonTimeout: 'Timed out',
  reasonUpstream: 'Upstream unavailable',
  reasonInterrupted: 'Stream interrupted',
  reasonCanceled: 'Client disconnected',
  reasonAccountBusy: 'Account at capacity',
  reasonAccountQueueFull: 'Account waiting queue is full',
  reasonAccountWaitTimeout: 'Timed out waiting for account capacity',
  reasonCooling: 'Account cooling down',
  reasonAccountUnavailable: 'Account unavailable',
  reasonModelConflict: 'Conversation model conflict',
  recoveryModelConflict:
    'Switch back to the previous model or start a new conversation to use the requested model. The current conversation keeps its account binding.',
  reasonGroupUnavailable: 'Group access unavailable',
  reasonAuthRequired: 'Authorization required',
  reasonForbidden: 'Upstream access denied',
  reasonRejected: 'Upstream rejected request',
  reasonUploadTimeout: 'Request upload timed out',
  reasonUploadFailed: 'Request upload failed',
  reasonRequestTooLarge: 'Request body too large',
  reasonGatewayBusy: 'Gateway at capacity',
  recoveryUpload:
    'Check your upload connection, retry, or reduce the request size. This request did not reach a model.',
  reasonInvalid: 'Invalid request',
  reasonRefresh: 'Credential refresh failed',
  reasonUnknown: 'Request failed',
  reasonContextLimit: 'Conversation context limit',
  proxiesTitle: 'Network proxies',
  proxiesUsageHint:
    'Bind a proxy to route an account’s server requests through that exit.',
  addProxy: 'Add proxy',
  editProxy: 'Edit proxy',
  deleteProxy: 'Delete proxy',
  saveProxy: 'Save proxy',
  proxyName: 'Proxy name',
  proxyURL: 'Proxy URL',
  proxyFormHint:
    'The address and any password are encrypted and never shown again.',
  proxyKeepURL: 'Leave blank to keep the saved URL',
  proxyURLExample: 'http://user:password@host:port',
  proxyURLHint: 'Use http://, https://, or socks5:// with a host and port.',
  proxyInvalidInput: 'Enter a name and a valid proxy URL with a host and port.',
  proxyNotFound: 'This proxy no longer exists. Refresh the list and try again.',
  proxyInUse: 'Unbind all accounts from this proxy before deleting it.',
  proxyExists: 'A proxy with this name already exists.',
  proxyLimit: 'This workspace has reached its proxy limit.',
  proxyActionFailed: 'Could not save this proxy. Please try again.',
  proxyAccountCount: 'Bound accounts: {{count}}',
  loadingProxies: 'Loading proxies…',
  proxiesLoadFailed: 'Could not load proxies. Please retry.',
  proxiesEmptyTitle: 'No network proxies',
  proxiesEmptyDescription:
    'Add a proxy, then bind a subscription account to it.',
  deleteProxyConfirm: 'Delete this proxy from the workspace?',
  accountProxy: 'Network proxy',
  accountProxyHint:
    'Server requests use the selected exit. Browser authorization still opens in your browser.',
  directConnection: 'Default network route',
  proxySearch: 'Search proxies',
  proxyNoMatch: 'No matching proxies.',
  saveProxyBinding: 'Save proxy binding',
  proxyAssigned: 'Assigned',
  importProxies: 'Import proxies',
  proxyList: 'Proxy list',
  proxyImportHint:
    'Paste up to 32 proxies. The entire batch is saved only if every line is valid.',
  proxyImportFormat:
    'One URL per line. Use “Name | URL” for a custom name; host:port without a scheme uses HTTP.',
  proxyImportEmpty: 'Enter one or more proxy lines within the size limit.',
  proxyImportInvalid:
    'One or more proxy lines are invalid. Nothing was imported; check the URL format and try again.',
  proxyImportUnavailable:
    'Bulk import is unavailable on the running backend. Update or restart SubLane, then try again.',
  testProxy: 'Test proxy',
  proxyProbeHint:
    'Manual tests contact IPinfo through the selected proxy. No background checks run.',
  proxyNotTested: 'Not tested',
  proxyConnected: 'Connected',
  proxyConnectionFailed: 'Connection failed',
  proxyLookupUnavailable: 'Location lookup unavailable',
  proxyLookupRateLimited: 'Location lookup rate limited',
  proxyLocationUnknown: 'Unknown',
  proxyLatency: '{{latency}} ms',
  proxyChanged: 'The proxy changed during the test. Run it again.',
  testProxyNamed: 'Test {{name}}',
  editProxyNamed: 'Edit {{name}}',
  deleteProxyNamed: 'Delete {{name}}',
  testAllProxies: 'Test all proxies',
  proxyBulkProgress: 'Testing proxies {{done}}/{{total}}…',
  proxyBulkSummary_one: '{{count}} proxy checked',
  proxyBulkSummary_other: '{{count}} proxies checked',
  proxyBulkDetail:
    '{{connected}} connected · {{failed}} connection failed · {{errors}} check errors',
  deleteFailedProxies: 'Delete failed proxies ({{count}})',
  proxyPruneConfirm:
    'Delete recently failed proxies that are not bound to any account? Untested proxies, location lookup failures and bound proxies stay in place.',
  proxyPruneAction_one: 'Delete {{count}} failed proxy',
  proxyPruneAction_other: 'Delete {{count}} failed proxies',
  proxyPruned: 'Deleted {{count}} failed proxies.',
  proxyPruneFailed: 'Could not delete failed proxies. Refresh and try again.',
  resetSettings: 'Reset card settings',
  resetCardsTitle: 'Reset cards',
  resetCardsPolicy:
    'Uses the available card expiring soonest; non-expiring cards go last. Ties use the oldest granted card.',
  resetCardsRefresh: 'Refresh cards',
  resetCardsLoading: 'Loading reset cards…',
  resetCardsDisabled:
    'Enable or reauthorize this account to view its reset cards.',
  resetCardsLoadFailed: 'Could not refresh reset cards. Try again.',
  resetCardsAvailable: 'Available: {{count}}',
  resetCardsEmpty: 'No reset card details reported.',
  resetCardsUpdated: 'Observed: {{date}}',
  resetCardAvailable: 'Available',
  resetCardRedeeming: 'Redeeming',
  resetCardRedeemed: 'Redeemed',
  resetCardExpired: 'Expired',
  resetCardUnknown: 'Unknown',
  resetCardUnsupported: 'Unsupported card type',
  resetCardNoExpiry: 'No expiry',
  resetCardExpires: 'Expires: {{date}}',
  resetCardGranted: 'Granted: {{date}}',
  resetCardNext: 'Next to use',
  resetCardDefaultTitle: 'Usage reset',
  resetCardsUnavailable: 'Skipped: reset card details unavailable',
  resetCardsNoEligible: 'Skipped: no eligible reset cards',
  resetDescription:
    'Allow this account to spend reset cards using either or both triggers. Each attempt uses at most one card; the provider decides which quota windows can be reset.',
  resetScheduled: 'Use at a scheduled time',
  resetExhausted: 'Use when quota is exhausted',
  resetExhaustedHint:
    'Check in the background about once a minute. Only explicit quota exhaustion triggers a card; network errors and unknown data do not.',
  resetFrequency: 'Frequency',
  resetOnce: 'Once',
  resetDaily: 'Every day',
  resetWeekly: 'Every week',
  resetWeekday: 'Day of the week',
  resetDate: 'Date',
  resetTime: 'Time',
  resetZone: 'Time zone: {{zone}}.',
  resetMissedHint:
    'Runs missed by more than five minutes are skipped. Nonexistent daylight-saving times are skipped too.',
  resetDailyLimit: 'Daily card limit',
  resetLimitHint:
    'Today: {{used}} / {{limit}} cards, including pending attempts. Scheduled, exhausted and manual uses share this limit in the instance time zone.',
  resetNext: 'Next scheduled run: {{date}}',
  resetAutoSummary: 'Automatic use on quota exhaustion is on',
  resetSave: 'Save settings',
  resetSaved: 'Reset card settings saved.',
  resetInvalid:
    'Enter a valid time, a future date for a one-time run, and a daily limit from 1 to 100.',
  resetBusy: 'A reset operation is running. Wait, then refresh and try again.',
  resetNoPending:
    'There is no pending result. Refresh to see the latest status.',
  resetFailed: 'Could not complete the reset operation. Refresh and try again.',
  resetLoadFailed: 'Could not load reset card settings. Try again.',
  resetRetry: 'Retry',
  resetActions: 'Use a reset card',
  resetUseNow: 'Use one card now',
  resetManualHint:
    'Uses one card if the provider allows a reset. A reset does not guarantee that every quota or restriction will be cleared.',
  resetSaveFirst:
    'Save your changes before using a card or checking a pending result.',
  resetCheckPending: 'Check pending result',
  resetPendingHint:
    'The last attempt may have used a card. Automatic use is paused. Check the result using the same attempt so another card is not spent.',
  resetHistory: 'Recent activity',
  resetHistoryEmpty: 'No reset attempts yet.',
  resetTriggerScheduled: 'Scheduled',
  resetTriggerExhausted: 'Quota exhausted',
  resetTriggerManual: 'Manual',
  resetResultSuccess: 'Used one reset card',
  resetResultConfirmed: 'Previous reset confirmed; no additional card used',
  resetResultUnneeded: 'Skipped: no quota window eligible for reset',
  resetResultNoCards: 'Skipped: no reset cards available',
  resetResultPending: 'Reset result awaiting confirmation',
  resetResultDisabled: 'Skipped: account is disabled',
  resetResultReauthorize: 'Skipped: account needs reauthorization',
  resetResultChanged: 'Stopped: account changed during the attempt',
  resetResultMissed: 'Skipped: scheduled time was missed',
  resetResultLimit: 'Skipped: daily card limit reached',
  resetResultUsage: 'Skipped: could not refresh quota',
  resetResultUnknown: 'Skipped: quota or card availability is unknown',
  resetResultAvailable: 'Skipped: quota is not confirmed exhausted',
  auditAccountResetSettings: 'Updated reset card settings',
  auditAccountResetUse: 'Used a reset card',
  catalogContextWindow: 'Context window',
  catalogMaxOutput: 'Maximum output',
  catalogReasoningLevels: 'Reasoning levels',
  catalogImages: 'Image input',
  catalogTools: 'Tool calls',
  catalogSupported: 'Supported',
  catalogUnsupported: 'Not supported',
  catalogCapabilitiesHint:
    'Only upstream-reported capabilities are shown. Unreported details remain unknown.',
  catalogCapabilitiesMerge:
    'Shared models show the lowest reported limits and capabilities confirmed by every eligible resource.',
  usagePaceAhead: '{{points}} percentage points ahead of even pace',
  usagePaceBehind: '{{points}} percentage points behind even pace',
  usagePaceOnTrack: 'On even pace',
  usagePaceDetails: 'About usage pace',
  usagePaceExplanation:
    'Compares quota used with elapsed time at the last usage observation. An even pace spreads usage across the reported window. Provider windows may roll or reset; this is a reference, not an exhaustion forecast.',
  requestRouting: 'Routing explanation',
  requestRoutingHint: 'Recorded at the final routing attempt for this request.',
  requestRoutingUnavailable:
    'No historical routing explanation is available for this request.',
  requestRouteAffinity: 'Kept the conversation’s bound resource.',
  requestRouteParent: 'Preferred the parent conversation’s eligible resource.',
  requestRouteRotation: 'Used rotation within the preferred eligible tier.',
  requestRouteWeighted:
    'Used quota-weighted rotation within the preferred eligible tier.',
  requestRouteNone: 'No resource was selected.',
  requestRouteRejected: 'Resource admission did not succeed.',
  requestRouteParentFallback:
    'The parent preference was unavailable; normal scheduling applied.',
  requestRouteCandidates: 'Resources in the final selection tier',
  requestRouteExclusions: 'Excluded resources',
  requestRouteModel: 'Model support unknown or unsupported',
  requestRoutePolicy: 'Model or resource policy',
  requestRouteQuota: 'Upstream quota or member allowance',
  requestRouteCooldown: 'Resource or model cooldown',
  requestRouteConcurrency: 'Concurrency capacity',
  requestRouteUnavailable: 'Disabled or unavailable resource',
  auditGroupDelete: 'Deleted resource group',
  deleteGroup: 'Delete group',
  deleteGroupNamed: 'Delete {{name}}',
  deleteGroupTitle: 'Delete resource group?',
  deleteGroupDescription:
    '“{{name}}” will stop routing requests and lose its resources, model policy and member grants. Request history and usage are kept.',
  groupDeleteInUse:
    'Active API keys still use this group. Move them to another group or revoke them first.',
  groupDeleteAllocated:
    'This group has a resource allowance. Delete the allowance before deleting the group.',
  groupDeleteMissing: 'This group no longer exists.',
  groupDeleteFailed: 'The group could not be deleted. Try again.',
  sessionSticky: 'Session stickiness',
  sessionStickyHint:
    'Keeps conversations without a session ID on one resource, using a hash of the opening prompt, and lets subagents prefer their parent’s resource. Unavailable resources still fail over. Improves upstream prompt cache hits.',
  sessionStickyEnabled: 'Session stickiness on',
  editChannel: 'Edit channel',
  channelKeyKeepHint:
    'Leave blank to keep the stored key. The stored key is never shown.',
  saveChannel: 'Save channel',
  usageMonthly: 'Monthly limit',
  allocationDelete: 'Delete resource allowance',
  allocationDeleteNamed: 'Delete resource allowance {{name}}',
  allocationDeleteTitle: 'Delete resource allowance?',
  allocationDeleteDescription:
    '“{{name}}” and its budget rules, price snapshots and allowance ledger will be deleted. Keys bound to it keep working in their resource group without a budget limit. Request history and usage statistics are kept.',
  allocationDeleteInFlight:
    'Requests using this allowance are still running. Try again when they finish.',
  allocationDeleteMissing: 'This resource allowance no longer exists.',
  allocationDeleteFailed:
    'The resource allowance could not be deleted. Try again.',
  proxyExitIPLabel: 'Exit IP',
  proxyLocationLabel: 'Location',
  ipReportLabel: 'IP report for {{name}}',
  ipReportOpen: 'Open the full report',
  ipReportRefresh: 'Refresh IP report',
  ipReportRefreshNamed: 'Refresh IP report for {{name}}',
  ipReportNeedsCheck: 'Test the connection to see the exit IP report.',
  ipReportLoading: 'Loading IP report…',
  ipReportRateLimited:
    'The IP report service is rate limited. Try again later.',
  ipReportUnsupported: 'No public report is available for this exit IP.',
  ipReportFailed: 'The IP report could not be loaded.',
  ipReportMobile: 'Mobile network',
  ipReportResidential: 'Residential',
  ipReportDatacenter: 'Data center',
  ipReportBusiness: 'Business',
  ipReportISP: 'ISP',
  ipReportUnknownType: 'Unknown type',
  ipReportVPN: 'VPN',
  ipReportProxy: 'Proxy',
  ipReportTor: 'Tor',
  ipReportAbuser: 'Abuse reports',
  ipReportCrawler: 'Crawler',
  ipReportScoreUnknown: 'No score',
  ipReportScore: 'IP purity score',
  ipReportNoRisk: 'No risk flags',
  ipReportVerdictLabel: 'Verdict',
  ipReportPristine: 'Pristine',
  ipReportClean: 'Clean',
  ipReportGood: 'Good',
  ipReportNeutral: 'Neutral',
  ipReportSuspicious: 'Suspicious',
  proxyNetworkLabel: 'Network',
  proxyBoundAccounts: 'Bound accounts',
  proxyCheckedAt: 'Checked {{time}}',
  channelEditDescription:
    'Rename the channel or replace its key. To use another endpoint or protocol, add a new channel so the stored key never reaches a different upstream.',
  ipReportLookup: 'Check IP purity',
  ipReportLookupNamed: 'Check IP purity for {{name}}',
  ipReportLookupHint: 'Checking sends this exit IP to ip.net.coffee.',
  ipReportPageHint:
    'IP purity is looked up only when you request it; ip.net.coffee then receives that exit IP, and results are cached for six hours.',
  allocationImageInputM: 'Image input · M',
  allocationImageInputHint:
    'For image model requests: the image share of input tokens, charged at the image input rate. Leave empty otherwise.',
} satisfies Record<string, string>
