# Four-state synchronization verification

The implementation uses the original stable record, one workspaces table, and
GORM AutoMigrate. There are no action, phase, journal, or revision columns.

| Requirement | Implementation and verification |
| --- | --- |
| All eight present/absent transitions | `service.workspaceFSM`; `TestSyncFourStateMatrix` |
| Failed, missing, duplicate-ID scans make no changes | `TestSyncInvalidScanNeverChangesState`; Herdr client list decoding tests |
| Ambiguous identity rejected before writing | `TestSyncRebindsLostCreationWithoutLosingNote`; host-ID lookup precedes stable-ID temporary-name lookup |
| Names alone do not identify records | Sync only indexes host IDs and stable record IDs; ordinary labels are metadata |
| Notes, snapshot, manager identity survive disappearance | Matrix, lifecycle and manager tests; absence changes only status |
| Archive one conditional intent write before close | `ArchiveIntent`; `TestArchiveDoesNotCloseBeforeCommit`, `TestActionWriteLimits`, `TestArchiveConfirmedRejectsChanges` |
| Archive completion belongs to sync | Successful Archive returns without scanning; matrix covers persisted archiving intent with either host outcome |
| Lost close reply, delayed close, cancellation | `TestArchiveFailureAndDelayedClose`, `TestRepairUsesIndependentContext`, `TestSyncRepairsFailedArchiveReset` |
| Restore validates directories and snapshot before create | `Restore` calls `Node.Validate(true)` on every tab |
| Restore zero writes; original record binds to new host | `TestRestoreKeepsArchivedUntilSync`, `TestActionWriteLimits`, `TestSyncRepairsFailedNewIDCommit` |
| No creation, partial creation, lost reply, retry before event | `TestRestoreRetryBeforeEventAndNoCreationFailure`, `TestPartialRestoreRetries`, `TestLostCreationReplyRebindsInsteadOfDuplicating` |
| Bind before reconstruction; retry after failure | `TestSyncBindingBeforeRenameAndRetryCleanup` |
| Manager intent before create; registered before TUI | `OpenManager` has one CreateManager call; `TestOpenManager`, `TestManagerWriteFailureStopsBeforeHostAndSync` |
| Cross-process session lock, no SQL transaction across host call | `state.WaitLock`; action and sync lock scope; `TestArchiveEventWaitsUntilCloseCompletes`, `TestCreateEventWaitsForManagerRegistration` |
| Independent bounded repair; original error retained | `repairLocked`, Focus and OpenManager use background contexts with ten-second deadlines and joined errors; sync itself never invokes repair |
| Explicit repository operations | WorkspaceStore exposes NonClosed, FindHost, Get, List, CompleteRestore, ArchiveIntent, CreateManager, ApplySync and note operations; production service code has no transaction callback |
| Per-item sync writes; concurrent notes retained | Each FSM transition uses an upsert excluding note updates; `TestApplySyncAtomicAndPreservesLatestNote`, concurrent SQLite tests |
| Focus, notes, preview, width settings | Focus has no action write and repairs failures; Save updates only note; Preview only reads; SettingsService writes plugin config |
| Events, startup, manual sync; DB-only polling | Manifest retains created/closed/renamed; TUI Init/manual refresh sync; one-second polling reads StoredRows only |
| In-progress state and persistent errors | TUI renders `[archiving]`, guards archive/restore by exact status, retains note access; `TestFinishedReadsDBAndKeepsErrorOnPoll` |
| Real event dispatch does not deadlock action response | Isolated Linux smoke opens managers while create events contend for the same session lock and verifies all three event types |
| Real restoration and lost mutation replies | Linux smoke runs `TestHerdrIntegration` for successful restore and lost workspace/tab/pane/rename replies |

Validation commands:

- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- `go build -o bin/herdr-workspace-enhancer ./cmd/herdr-workspace-enhancer`
- `python3 scripts/linux_plugin_smoke.py .`
- `git diff --check`

Runtime integration is verified on Linux. Windows runtime smoke is not claimed.
Events remain non-durable; lost events require another event, startup, or manual
refresh. A successful empty scan assumes session restoration has completed.
An archive timeout followed by a late close can intentionally result in closed,
not archived. Archived live workspaces are rebuilt on the next sync after a failed restore. Active workspaces are never rebuilt.

## Manager initialization

Manager identity uses NONE, INIT and DONE instead of a boolean. Open persists
closed + INIT and creates a workspace; successful Open relies on the create event.
Refresh binds the workspace before initializing every active + INIT manager under
the session lock. Initialization retains one pane, closes the others, opens a
plugin pane without focus, closes the retained pane, focuses, then commits DONE.
Failures before DONE leave INIT for a full retry, including replacing a TUI from
a previous partial attempt. Focus failures also leave INIT for retry. DONE never rebuilds panes
or focuses on subsequent syncs. Missing closed + INIT workspaces remain unchanged.
No migration from the unreleased boolean schema is provided.

## Retryable restoration

Restore only ensures an archived record has a workspace, using its host ID or
reserved temporary name. Existing candidates return success without layout work.
A new workspace is created without focus; its create event triggers Refresh.
Success therefore means the workspace exists, not that its layout is complete.

Refresh resolves all identities (including closed records and managers) and
rejects ambiguous bindings before any host mutation. It processes each item with
a for/switch FSM, joins individual errors, and stops starting items on cancellation.
Manager initialization is a separate state machine under the same session lock.

Restoration validates all layouts and directories before binding the new host ID
while retaining archived status. It lists old tabs, creates the first replacement
tab before closing old tabs, expands pane trees, creates remaining tabs, and
renames the workspace. CompleteRestore conditionally commits active only after
all host operations succeed. Any failure retains archived, including lost CLI
replies and a failed final DB commit. A subsequent event, startup or manual refresh
rebuilds from scratch. No background retry scheduler or persistent phase is added.

Tests cover every host reconstruction call failing, final commit failure after
rename, note edits during reconstruction, invalid snapshots, independent items,
and concurrent Restore calls. The isolated Herdr integration verifies complete
layouts after lost workspace/tab/pane/rename replies, as well as event and TUI
startup without deadlocks.

## Delete archived workspaces

TUI `d` opens a confirmation for archived records; `y` deletes, other keys cancel.
Unsaved note handling runs first, and polling cannot replace the confirmation
target. The service rechecks archived/non-manager eligibility under the session
lock, completes identity resolution, closes any live remnant, then conditionally
deletes the record and its note/snapshot. It never validates the snapshot or invokes
repair sync. Close errors (including lost replies) retain the record for retry.
Tests cover missing snapshots, host and temporary-name matches, rejected states,
ambiguous identities, scan/close failures, retries, and TUI confirmation.
