# Four-state synchronization verification

The implementation uses the original stable record, one workspaces table, and
GORM AutoMigrate. There are no action, phase, journal, or revision columns.

| Requirement | Implementation and verification |
| --- | --- |
| All eight present/absent transitions | `service.refreshLocked`; `TestSyncFourStateMatrix` |
| Failed, missing, duplicate-ID scans make no changes | `TestSyncInvalidScanNeverChangesState`; Herdr client list decoding tests |
| Ambiguous identity rejected before writing | `TestSyncRebindsLostCreationWithoutLosingNote`; host-ID lookup precedes stable-ID temporary-name lookup |
| Names alone do not identify records | Sync only indexes host IDs and stable record IDs; ordinary labels are metadata |
| Notes, snapshot, manager identity survive disappearance | Matrix, lifecycle and manager tests; absence changes only status |
| Archive one conditional intent write before close | `ArchiveIntent`; `TestArchiveDoesNotCloseBeforeCommit`, `TestActionWriteLimits`, `TestArchiveConfirmedRejectsChanges` |
| Archive completion belongs to sync | Successful Archive returns without scanning; matrix covers persisted archiving intent with either host outcome |
| Lost close reply, delayed close, cancellation | `TestArchiveFailureAndDelayedClose`, `TestRepairUsesIndependentContext`, `TestSyncRepairsFailedArchiveReset` |
| Restore validates directories and snapshot before create | `Restore` calls `Node.Validate(true)` on every tab |
| Restore zero writes; original record binds to new host | `TestRestoreKeepsArchivedUntilSync`, `TestActionWriteLimits`, `TestSyncRepairsFailedNewIDCommit` |
| No creation, partial creation, lost reply, retry before event | `TestRestoreRetryBeforeEventAndNoCreationFailure`, `TestPartialRestoreRemainsActiveWithoutAutomaticReplay`, `TestLostCreationReplyRebindsInsteadOfDuplicating` |
| Bind before rename; retry name cleanup without replay | `TestSyncBindingBeforeRenameAndRetryCleanup` |
| Manager intent before create; registered before TUI | `OpenManager` has one CreateManager call; `TestOpenManager`, `TestManagerWriteFailureStopsBeforeHostAndSync` |
| Cross-process session lock, no SQL transaction across host call | `state.WaitLock`; action and sync lock scope; `TestArchiveEventWaitsUntilCloseCompletes`, `TestCreateEventWaitsForManagerRegistration` |
| Independent bounded repair; original error retained | `repairLocked`, Focus and OpenManager use background contexts with ten-second deadlines and joined errors; sync itself never invokes repair |
| Explicit repository operations | WorkspaceStore exposes All, Get, List, ArchiveIntent, CreateManager, ApplySync and note operations; production service code has no transaction callback |
| One atomic sync SQL statement; concurrent notes retained | `ApplySync` performs one bulk upsert excluding note updates; `TestApplySyncAtomicAndPreservesLatestNote`, concurrent SQLite tests |
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
not archived. Sync never replays layout creation or retries closing a workspace.
