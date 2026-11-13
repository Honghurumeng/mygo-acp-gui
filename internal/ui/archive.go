package ui

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/reed/acp-gui/internal/acp"
	"github.com/reed/acp-gui/internal/archive"
)

const archiveFileName = "sessions.json"

// loadSessionArchive opens the local session index and installs its rows in
// the sidebar. The archive is read before any agent process starts, so the
// saved list is available even when discovery is slow or an agent is missing.
func (a *App) loadSessionArchive(dataDir string) {
	dataDir = strings.TrimSpace(dataDir)
	if dataDir != "" {
		a.archive = archive.Open(filepath.Join(dataDir, archiveFileName))
	}
	a.loadArchive()
}

func (a *App) loadArchive() {
	if a.archive == nil {
		a.syncArchiveRows()
		return
	}
	file, err := a.archive.Load()
	if err != nil {
		a.SavedError = err.Error()
	}
	a.Archived = append([]archive.Session(nil), file.Sessions...)
	if a.sanitizeArchiveTitles() {
		a.saveArchive()
	}
	a.syncArchiveRows()
}

// sanitizeArchiveTitles rewrites titles saved by older versions that used the
// first user message verbatim. Those sessions are often just a clipboard
// image path, which would otherwise flash in the sidebar before session/list
// replaces it. It reports whether the archive changed.
func (a *App) sanitizeArchiveTitles() bool {
	changed := false
	for i := range a.Archived {
		cleaned := cleanSessionTitle(a.Archived[i].Title)
		if cleaned != strings.TrimSpace(a.Archived[i].Title) {
			a.Archived[i].Title = cleaned
			changed = true
		}
	}
	return changed
}

// syncArchiveRows rebuilds the sidebar rows from the local archive. It is the
// only place that turns archive records into savedSession values.
func (a *App) syncArchiveRows() {
	rows := make([]savedSession, 0, len(a.Archived))
	for _, record := range a.Archived {
		if strings.TrimSpace(record.SessionID) == "" {
			continue
		}
		cwd := strings.TrimSpace(record.Cwd)
		rows = append(rows, savedSession{
			SessionSummary: acp.SessionSummary{
				SessionID: record.SessionID,
				Cwd:       cwd,
				Title:     cleanSessionTitle(record.Title),
				UpdatedAt: strings.TrimSpace(record.UpdatedAt),
			},
			AgentID:   strings.TrimSpace(record.AgentID),
			ShowName:  strings.TrimSpace(record.ShowName),
			Show:      record.Show,
			archived:  true,
			workspace: canonicalWorkspace(cwd),
		})
	}
	a.SavedSessions = sortSavedSessions(rows)
}

func (a *App) archiveIndex(agentID, sessionID string) int {
	for i, record := range a.Archived {
		if record.AgentID == agentID && record.SessionID == sessionID {
			return i
		}
	}
	return -1
}

func (a *App) saveArchive() {
	if a.archive == nil {
		return
	}
	file := archive.File{
		Version:  archive.CurrentVersion,
		Sessions: append([]archive.Session(nil), a.Archived...),
	}
	if err := a.archive.Save(file); err != nil {
		a.SavedError = err.Error()
		return
	}
	// A successful write replaces a corrupt/missing archive, so an earlier
	// archive read/write warning no longer applies.
	if strings.HasPrefix(a.SavedError, "archive:") {
		a.SavedError = ""
	}
}

func archiveNow() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

// recordUserTurnSent stores the local send time of one user prompt. ACP
// history carries no timestamps, so this list is what lets Jump show times
// after a restart. number is the visible user-turn ordinal at send time.
func (a *App) recordUserTurnSent(chat *Chat, number int, sentAt time.Time) {
	if chat == nil || number <= 0 || sentAt.IsZero() {
		return
	}
	index := a.archiveIndex(strings.TrimSpace(chat.Agent.ID), strings.TrimSpace(chat.Info.SessionID))
	if index < 0 {
		return
	}
	record := &a.Archived[index]
	stamp := sentAt.UTC().Format(time.RFC3339Nano)
	for i := range record.UserTurns {
		if record.UserTurns[i].Number == number {
			record.UserTurns[i].SentAt = stamp
			a.saveArchive()
			return
		}
	}
	record.UserTurns = append(record.UserTurns, archive.UserTurn{Number: number, SentAt: stamp})
	a.saveArchive()
}

// recordUserTurnDuration stores how long one user prompt took. It updates the
// entry recordUserTurnSent created, so sessions.json keeps one row per turn.
func (a *App) recordUserTurnDuration(chat *Chat, number int, elapsed time.Duration) {
	if chat == nil || number <= 0 || elapsed <= 0 {
		return
	}
	index := a.archiveIndex(strings.TrimSpace(chat.Agent.ID), strings.TrimSpace(chat.Info.SessionID))
	if index < 0 {
		return
	}
	ms := elapsed.Milliseconds()
	if ms <= 0 {
		return
	}
	record := &a.Archived[index]
	for i := range record.UserTurns {
		if record.UserTurns[i].Number != number {
			continue
		}
		if record.UserTurns[i].DurationMs == ms {
			return
		}
		record.UserTurns[i].DurationMs = ms
		a.saveArchive()
		return
	}
}

// replayTurn is the locally known timing of one user turn, used to restore
// timestamps and runtime captions after session/load replays the history.
type replayTurn struct {
	sentAt  time.Time
	elapsed time.Duration
}

// applyReplayTimes attaches locally recorded send times and runtimes to
// replayed history. Timestamps are matched by user-turn number, so prompts
// sent outside this app stay unknown instead of borrowing a neighbour's
// metadata. The returned map is keyed by user-turn number for the runtime
// rows applyReplayDurations inserts at the same boundaries as live turns.
func (a *App) applyReplayTimes(chat *Chat) map[int]time.Duration {
	if chat == nil {
		return nil
	}
	index := a.archiveIndex(strings.TrimSpace(chat.Agent.ID), strings.TrimSpace(chat.Info.SessionID))
	if index < 0 {
		return nil
	}
	records := a.Archived[index].UserTurns
	if len(records) == 0 {
		return nil
	}
	turns := make(map[int]replayTurn, len(records))
	for _, turn := range records {
		if turn.Number <= 0 {
			continue
		}
		parsed := replayTurn{}
		if turn.SentAt != "" {
			if at, err := time.Parse(time.RFC3339Nano, turn.SentAt); err == nil {
				parsed.sentAt = at
			}
		}
		if turn.DurationMs > 0 {
			parsed.elapsed = time.Duration(turn.DurationMs) * time.Millisecond
		}
		if parsed.sentAt.IsZero() && parsed.elapsed <= 0 {
			continue
		}
		turns[turn.Number] = parsed
	}
	if len(turns) == 0 {
		return nil
	}
	durations := make(map[int]time.Duration, len(turns))
	number := 0
	for _, message := range chat.Messages {
		if message.Role != RoleUser {
			continue
		}
		number++
		turn, ok := turns[number]
		if !ok {
			continue
		}
		if message.SentAt.IsZero() && !turn.sentAt.IsZero() {
			message.SentAt = turn.sentAt
		}
		if turn.elapsed > 0 {
			durations[number] = turn.elapsed
		}
	}
	if len(durations) == 0 {
		return nil
	}
	return durations
}

// applyReplayDurations inserts the runtime caption after each replayed turn,
// just before the next user row and at the end of the history. Live turns add
// the same row from applyPromptResult.
func (a *App) applyReplayDurations(chat *Chat, durations map[int]time.Duration) {
	if chat == nil || len(durations) == 0 {
		return
	}
	out := make([]*Message, 0, len(chat.Messages)+len(durations))
	number := 0
	for _, message := range chat.Messages {
		if message.Role == RoleUser {
			if summary := a.newTurnSummary(durations[number]); summary != nil {
				out = append(out, summary)
			}
			number++
		}
		out = append(out, message)
	}
	if summary := a.newTurnSummary(durations[number]); summary != nil {
		out = append(out, summary)
	}
	chat.Messages = out
}

func cleanArchivePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return path
}

// rememberArchivedSession records a session that was just created or loaded.
// Existing ShowName and soft-delete state are preserved.
func (a *App) rememberArchivedSession(chat *Chat) {
	if chat == nil {
		return
	}
	agentID := strings.TrimSpace(chat.Agent.ID)
	sessionID := strings.TrimSpace(chat.Info.SessionID)
	title := strings.TrimSpace(chat.Info.Title)
	// A new record can take the UI title as a fallback. For an existing one,
	// chat.Title may be the local ShowName and must not replace the agent
	// title stored in the archive.
	if title == "" && agentID != "" && sessionID != "" && a.archiveIndex(agentID, sessionID) < 0 {
		title = strings.TrimSpace(chat.Title)
	}
	title = cleanSessionTitle(title)
	a.activateArchivedSession(agentID, sessionID, chat.Workspace, title)
}

func (a *App) activateArchivedSession(agentID, sessionID, cwd, title string) {
	agentID = strings.TrimSpace(agentID)
	sessionID = strings.TrimSpace(sessionID)
	if agentID == "" || sessionID == "" {
		return
	}
	now := archiveNow()
	cwd = cleanArchivePath(cwd)
	title = cleanSessionTitle(title)
	if index := a.archiveIndex(agentID, sessionID); index >= 0 {
		record := &a.Archived[index]
		if cwd != "" {
			record.Cwd = cwd
		}
		if title != "" {
			record.Title = title
		}
		if record.CreatedAt == "" {
			record.CreatedAt = now
		}
		record.UpdatedAt = now
		// Opening a hidden row restores it.
		record.Show = true
	} else {
		a.Archived = append(a.Archived, archive.Session{
			AgentID:   agentID,
			SessionID: sessionID,
			Cwd:       cwd,
			Title:     title,
			CreatedAt: now,
			UpdatedAt: now,
			Show:      true,
		})
	}
	a.saveArchive()
	a.syncArchiveRows()
}

// mergeArchivedSessions imports metadata from the agent's session/list. It
// never changes Show or ShowName, so a deleted or renamed session cannot be
// resurrected by a refresh.
func (a *App) mergeArchivedSessions(agentID string, sessions []acp.SessionSummary) {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return
	}
	now := archiveNow()
	for _, item := range sessions {
		sessionID := strings.TrimSpace(item.SessionID)
		if sessionID == "" {
			continue
		}
		cwd := cleanArchivePath(item.Cwd)
		// Only sessions from workspaces the user added belong in the sidebar.
		// Import must never create workspace groups on its own.
		if cwd != "" && !a.workspaceKnown(cwd) {
			continue
		}
		title := cleanSessionTitle(item.Title)
		updatedAt := strings.TrimSpace(item.UpdatedAt)
		if index := a.archiveIndex(agentID, sessionID); index >= 0 {
			record := &a.Archived[index]
			if cwd != "" {
				record.Cwd = cwd
			}
			// An attachment-only fallback must not replace a more specific
			// title an earlier sync or this app already stored.
			if title != "" && !(title == imageSessionTitle && record.Title != "" && record.Title != imageSessionTitle) {
				record.Title = title
			}
			if updatedAt != "" {
				incoming := savedUpdatedAt(acp.SessionSummary{UpdatedAt: updatedAt})
				current := savedUpdatedAt(acp.SessionSummary{UpdatedAt: record.UpdatedAt})
				if current.IsZero() || incoming.After(current) {
					record.UpdatedAt = updatedAt
				}
			}
			if record.CreatedAt == "" {
				record.CreatedAt = now
			}
			continue
		}
		if updatedAt == "" {
			updatedAt = now
		}
		a.Archived = append(a.Archived, archive.Session{
			AgentID:   agentID,
			SessionID: sessionID,
			Cwd:       cwd,
			Title:     title,
			CreatedAt: now,
			UpdatedAt: updatedAt,
			Show:      true,
		})
	}
	a.saveArchive()
	a.syncArchiveRows()
}

// touchArchivedSession moves a session to the top of its workspace list after
// a completed turn. It also records sessions created before this archive was
// introduced.
func (a *App) touchArchivedSession(chat *Chat) {
	if chat == nil {
		return
	}
	agentID := strings.TrimSpace(chat.Agent.ID)
	sessionID := strings.TrimSpace(chat.Info.SessionID)
	if agentID == "" || sessionID == "" {
		return
	}
	now := archiveNow()
	index := a.archiveIndex(agentID, sessionID)
	if index < 0 {
		a.Archived = append(a.Archived, archive.Session{
			AgentID:   agentID,
			SessionID: sessionID,
			Cwd:       cleanArchivePath(chat.Workspace),
			Title:     cleanSessionTitle(chat.Info.Title),
			CreatedAt: now,
			UpdatedAt: now,
			Show:      true,
		})
	} else {
		record := &a.Archived[index]
		if cwd := cleanArchivePath(chat.Workspace); cwd != "" {
			record.Cwd = cwd
		}
		record.UpdatedAt = now
	}
	a.saveArchive()
	a.syncArchiveRows()
}

func (a *App) setArchivedTitle(chat *Chat, title string) {
	if chat == nil {
		return
	}
	title = cleanSessionTitle(title)
	if title == "" {
		return
	}
	agentID := strings.TrimSpace(chat.Agent.ID)
	sessionID := strings.TrimSpace(chat.Info.SessionID)
	index := a.archiveIndex(agentID, sessionID)
	if index < 0 {
		return
	}
	a.Archived[index].Title = title
	a.saveArchive()
	a.syncArchiveRows()
}

func (a *App) archivedShowName(agentID, sessionID string) string {
	index := a.archiveIndex(strings.TrimSpace(agentID), strings.TrimSpace(sessionID))
	if index < 0 {
		return ""
	}
	return strings.TrimSpace(a.Archived[index].ShowName)
}

func (a *App) setArchivedShowName(agentID, sessionID, name string) {
	index := a.archiveIndex(strings.TrimSpace(agentID), strings.TrimSpace(sessionID))
	if index < 0 {
		return
	}
	a.Archived[index].ShowName = strings.TrimSpace(name)
	a.saveArchive()
	a.syncArchiveRows()
}

func (a *App) setArchivedShow(agentID, sessionID string, show bool) {
	index := a.archiveIndex(strings.TrimSpace(agentID), strings.TrimSpace(sessionID))
	if index < 0 {
		return
	}
	a.Archived[index].Show = show
	a.saveArchive()
	a.syncArchiveRows()
}

// hideArchivedSession soft-deletes a session. A missing record is created so
// the next session/list refresh cannot make the session visible again.
func (a *App) hideArchivedSession(chat *Chat) {
	if chat == nil {
		return
	}
	agentID := strings.TrimSpace(chat.Agent.ID)
	sessionID := strings.TrimSpace(chat.Info.SessionID)
	if sessionID == "" {
		sessionID = strings.TrimSpace(chat.pendingSessionID)
	}
	a.hideArchivedRecord(agentID, sessionID, chat.Workspace, chat.Title)
}

func (a *App) hideArchivedRecord(agentID, sessionID, cwd, title string) {
	agentID = strings.TrimSpace(agentID)
	sessionID = strings.TrimSpace(sessionID)
	if agentID == "" || sessionID == "" {
		return
	}
	index := a.archiveIndex(agentID, sessionID)
	if index >= 0 {
		a.Archived[index].Show = false
		if cwd := cleanArchivePath(cwd); cwd != "" {
			a.Archived[index].Cwd = cwd
		}
	} else {
		now := archiveNow()
		a.Archived = append(a.Archived, archive.Session{
			AgentID:   agentID,
			SessionID: sessionID,
			Cwd:       cleanArchivePath(cwd),
			Title:     strings.TrimSpace(title),
			CreatedAt: now,
			UpdatedAt: now,
			Show:      false,
		})
	}
	a.saveArchive()
	a.syncArchiveRows()
}

func (a *App) hiddenArchivedCount() int {
	count := 0
	for _, record := range a.Archived {
		if !record.Show {
			count++
		}
	}
	return count
}
