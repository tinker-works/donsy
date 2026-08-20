// Package projectstore persists the local project registry and tracker stores.
package projectstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/tinker-works/donsy/internal/domain/agent"
	"github.com/tinker-works/donsy/internal/domain/epic"
)

// Project is the metadata stored in one project's local SQLite database.
type Project struct {
	Name         string
	Repositories []string
}

// Store is one project's local SQLite tracker store.
type Store struct {
	db *gorm.DB
}

// OpenStore creates or opens the SQLite database at databasePath. The caller
// owns creation of the database's parent directory.
func OpenStore(databasePath string) (*Store, error) {
	db, err := gorm.Open(sqlite.Open(databasePath), &gorm.Config{
		// The daemon owns stdout, so GORM diagnostics cannot write to it.
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, err
	}
	database, err := db.DB()
	if err != nil {
		return nil, err
	}
	database.SetMaxOpenConns(1)
	for _, pragma := range []string{
		"PRAGMA journal_mode=DELETE", "PRAGMA busy_timeout=5000", "PRAGMA foreign_keys=ON",
	} {
		if err := db.Exec(pragma).Error; err != nil {
			_ = database.Close()
			return nil, fmt.Errorf("%s: %w", pragma, err)
		}
	}
	for _, statement := range projectSchema {
		if err := db.Exec(statement).Error; err != nil {
			_ = database.Close()
			return nil, err
		}
	}
	if err := migrateSettingsSchema(db); err != nil {
		_ = database.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

var projectSchema = []string{
	`CREATE TABLE IF NOT EXISTS project_metadata (` +
		`id INTEGER PRIMARY KEY CHECK (id = 1), name TEXT NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS project_repositories (name TEXT PRIMARY KEY NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS agent_settings (` +
		`id INTEGER PRIMARY KEY CHECK (id = 1), setup_script TEXT NOT NULL, roles BLOB NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS repository_settings (` +
		`repository TEXT PRIMARY KEY NOT NULL, setup_script TEXT NOT NULL, roles BLOB NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS blobs (path TEXT PRIMARY KEY NOT NULL, contents BLOB NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS epics (` +
		`id TEXT PRIMARY KEY NOT NULL, title TEXT NOT NULL, assignee TEXT NOT NULL, ` +
		`repositories BLOB NOT NULL, body TEXT NOT NULL, state TEXT NOT NULL, ` +
		`branch_prefix TEXT NOT NULL, drafting_passes INTEGER NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS issues (` +
		`id TEXT PRIMARY KEY NOT NULL, epic_id TEXT NOT NULL REFERENCES epics(id) ON DELETE CASCADE, ` +
		`title TEXT NOT NULL, parent_id TEXT NOT NULL, repository TEXT NOT NULL, state TEXT NOT NULL, ` +
		`created_at DATETIME NOT NULL, body TEXT NOT NULL, blocked_by BLOB NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS pull_requests (` +
		`id TEXT PRIMARY KEY NOT NULL, epic_id TEXT NOT NULL REFERENCES epics(id) ON DELETE CASCADE, ` +
		`issue_id TEXT NOT NULL REFERENCES issues(id), title TEXT NOT NULL, status TEXT NOT NULL, ` +
		`repository TEXT NOT NULL, number INTEGER NOT NULL, url TEXT NOT NULL, head TEXT NOT NULL, ` +
		`base TEXT NOT NULL, flags BLOB NOT NULL, reviewed_head TEXT NOT NULL, ` +
		`reviewed_base TEXT NOT NULL, rounds INTEGER NOT NULL, reviews INTEGER NOT NULL, ` +
		`rounds_granted INTEGER NOT NULL, coding_rounds INTEGER NOT NULL, ` +
		`approved BOOLEAN NOT NULL, created_at DATETIME NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS issue_comments (` +
		`id TEXT PRIMARY KEY NOT NULL, issue_id TEXT NOT NULL REFERENCES issues(id) ON DELETE CASCADE, ` +
		`author TEXT NOT NULL, created_at DATETIME NOT NULL, body TEXT NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS pull_request_comments (` +
		`id TEXT PRIMARY KEY NOT NULL, pull_request_id TEXT NOT NULL REFERENCES pull_requests(id) ` +
		`ON DELETE CASCADE, author TEXT NOT NULL, created_at DATETIME NOT NULL, body TEXT NOT NULL)`,
}

const (
	readProjectSQL  = "SELECT name FROM project_metadata WHERE id = 1"
	writeProjectSQL = "INSERT INTO project_metadata (id, name) VALUES (1, ?) " +
		"ON CONFLICT(id) DO UPDATE SET name = excluded.name"
	insertRepositorySQL   = "INSERT INTO project_repositories (name) VALUES (?)"
	readRepositoriesSQL   = "SELECT name FROM project_repositories ORDER BY name"
	readAgentSettingsSQL  = "SELECT setup_script, roles FROM agent_settings WHERE id = 1"
	writeAgentSettingsSQL = "INSERT INTO agent_settings (id, setup_script, roles) " +
		"VALUES (1, ?, ?) ON CONFLICT(id) DO UPDATE SET " +
		"setup_script = excluded.setup_script, roles = excluded.roles"
	readRepositorySettingsSQL = "SELECT setup_script, roles FROM repository_settings " +
		"WHERE repository = ?"
	writeRepositorySettingsSQL = "INSERT INTO repository_settings " +
		"(repository, setup_script, roles) VALUES (?, ?, ?) " +
		"ON CONFLICT(repository) DO UPDATE SET " +
		"setup_script = excluded.setup_script, roles = excluded.roles"
	writeBlobSQL = "INSERT INTO blobs (path, contents) VALUES (?, ?) " +
		"ON CONFLICT(path) DO UPDATE SET contents = excluded.contents"
)

// migrateSettingsSchema rebuilds settings tables because SQLite cannot remove
// the legacy distro column with CREATE TABLE IF NOT EXISTS.
func migrateSettingsSchema(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		for _, table := range []string{"agent_settings", "repository_settings"} {
			columns, err := tx.Migrator().ColumnTypes(table)
			if err != nil {
				return fmt.Errorf("inspect %s: %w", table, err)
			}
			legacy := false
			for _, column := range columns {
				if column.Name() == "distro" {
					legacy = true
					break
				}
			}
			if !legacy {
				continue
			}
			if err := rebuildSettingsTable(tx, table); err != nil {
				return err
			}
		}
		return nil
	})
}

func rebuildSettingsTable(tx *gorm.DB, table string) error {
	temporary := table + "_without_distro"
	create := "CREATE TABLE " + temporary + " ("
	copy := "INSERT INTO " + temporary + " ("
	switch table {
	case "agent_settings":
		create += "id INTEGER PRIMARY KEY CHECK (id = 1), setup_script TEXT NOT NULL, roles BLOB NOT NULL)"
		copy += "id, setup_script, roles) SELECT id, setup_script, roles FROM agent_settings"
	case "repository_settings":
		create += "repository TEXT PRIMARY KEY NOT NULL, setup_script TEXT NOT NULL, roles BLOB NOT NULL)"
		copy += "repository, setup_script, roles) SELECT repository, setup_script, roles FROM repository_settings"
	default:
		return fmt.Errorf("unsupported settings table %q", table)
	}
	if err := tx.Exec(create).Error; err != nil {
		return fmt.Errorf("create migrated %s: %w", table, err)
	}
	if err := tx.Exec(copy).Error; err != nil {
		return fmt.Errorf("copy migrated %s: %w", table, err)
	}
	if err := tx.Exec("DROP TABLE " + table).Error; err != nil {
		return fmt.Errorf("drop legacy %s: %w", table, err)
	}
	if err := tx.Exec("ALTER TABLE " + temporary + " RENAME TO " + table).Error; err != nil {
		return fmt.Errorf("rename migrated %s: %w", table, err)
	}
	return nil
}

// Close releases the SQLite handle.
func (s *Store) Close() error {
	database, err := s.db.DB()
	if err != nil {
		return err
	}
	return database.Close()
}

// ReadProject returns stored project metadata and linked repositories.
func (s *Store) ReadProject() (Project, error) {
	var project Project
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var record struct{ Name string }
		result := tx.Raw(readProjectSQL).Scan(&record)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return os.ErrNotExist
		}
		repositories, err := readRepositories(tx)
		if err != nil {
			return err
		}
		project = Project{Name: record.Name, Repositories: repositories}
		return validateProject(project)
	})
	if err != nil {
		return Project{}, err
	}
	return project, nil
}

// WriteProject replaces project metadata and its complete set of repository links.
func (s *Store) WriteProject(project Project) error {
	if err := validateProject(project); err != nil {
		return err
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(writeProjectSQL, project.Name).Error; err != nil {
			return err
		}
		if err := tx.Exec("DELETE FROM project_repositories").Error; err != nil {
			return err
		}
		for _, repository := range sortedRepositories(project.Repositories) {
			if err := tx.Exec(insertRepositorySQL, repository).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// Repositories returns repository links in deterministic name order.
func (s *Store) Repositories() ([]string, error) {
	return readRepositories(s.db)
}

func readRepositories(db *gorm.DB) ([]string, error) {
	var records []struct{ Name string }
	if err := db.Raw(readRepositoriesSQL).Scan(&records).Error; err != nil {
		return nil, err
	}
	repositories := make([]string, 0, len(records))
	for _, record := range records {
		if err := validateRepository(record.Name); err != nil {
			return nil, err
		}
		repositories = append(repositories, record.Name)
	}
	return repositories, nil
}

// ReplaceRepositories replaces linked repositories while preserving metadata.
func (s *Store) ReplaceRepositories(repositories []string) error {
	if err := validateRepositories(repositories); err != nil {
		return err
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		var record struct{ Name string }
		result := tx.Raw(readProjectSQL).Scan(&record)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return os.ErrNotExist
		}
		if err := validateProject(Project{Name: record.Name}); err != nil {
			return err
		}
		if err := tx.Exec("DELETE FROM project_repositories").Error; err != nil {
			return err
		}
		for _, repository := range sortedRepositories(repositories) {
			if err := tx.Exec(insertRepositorySQL, repository).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// AgentSettings returns project-wide agent configuration. An unconfigured store
// returns the zero settings value.
func (s *Store) AgentSettings() (agent.AgentSettings, error) {
	var record settingsRecord
	result := s.db.Raw(readAgentSettingsSQL).Scan(&record)
	if result.Error != nil {
		return agent.AgentSettings{}, result.Error
	}
	if result.RowsAffected == 0 {
		return agent.AgentSettings{}, nil
	}
	return record.agentSettings()
}

// WriteAgentSettings validates and replaces project-wide agent configuration.
func (s *Store) WriteAgentSettings(settings agent.AgentSettings) error {
	if err := validateAgentSettings(settings); err != nil {
		return err
	}
	record, err := settingsRecordFrom(settings.SetupScript, settings.Roles)
	if err != nil {
		return err
	}
	return s.db.Exec(writeAgentSettingsSQL, record.SetupScript, record.Roles).Error
}

// RepositorySettings returns one repository override. A missing override is the
// zero value, which lets AgentSettings.Override supply project defaults.
func (s *Store) RepositorySettings(repository string) (agent.RepositorySettings, error) {
	if err := validateRepository(repository); err != nil {
		return agent.RepositorySettings{}, err
	}
	var record settingsRecord
	result := s.db.Raw(readRepositorySettingsSQL, repository).Scan(&record)
	if result.Error != nil {
		return agent.RepositorySettings{}, result.Error
	}
	if result.RowsAffected == 0 {
		return agent.RepositorySettings{}, nil
	}
	return record.repositorySettings()
}

// WriteRepositorySettings validates and replaces one repository override.
func (s *Store) WriteRepositorySettings(
	repository string, settings agent.RepositorySettings,
) error {
	if err := validateRepository(repository); err != nil {
		return err
	}
	if err := validateRepositorySettings(settings); err != nil {
		return err
	}
	record, err := settingsRecordFrom(settings.SetupScript, settings.Roles)
	if err != nil {
		return err
	}
	return s.db.Exec(
		writeRepositorySettingsSQL, repository, record.SetupScript, record.Roles,
	).Error
}

// ReadFile returns a path-keyed blob from the store.
func (s *Store) ReadFile(path string) (string, error) {
	if err := validatePath(path); err != nil {
		return "", err
	}
	var record struct{ Contents []byte }
	result := s.db.Raw("SELECT contents FROM blobs WHERE path = ?", path).Scan(&record)
	if result.Error != nil {
		return "", result.Error
	}
	if result.RowsAffected == 0 {
		return "", os.ErrNotExist
	}
	return string(record.Contents), nil
}

// WriteFile creates or replaces a path-keyed blob.
func (s *Store) WriteFile(path, contents string) error {
	if err := validatePath(path); err != nil {
		return err
	}
	return s.db.Exec(writeBlobSQL, path, []byte(contents)).Error
}

// ListEpics returns complete aggregates ordered by root issue creation time.
func (s *Store) ListEpics() ([]epic.Epic, error) {
	var aggregates []epic.Epic
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var records []struct{ ID string }
		if err := tx.Raw("SELECT id FROM epics").Scan(&records).Error; err != nil {
			return err
		}
		aggregates = make([]epic.Epic, 0, len(records))
		for _, record := range records {
			aggregate, err := readEpic(tx, record.ID)
			if err != nil {
				return err
			}
			aggregates = append(aggregates, aggregate)
		}
		sort.Slice(aggregates, func(i, j int) bool {
			left, _ := aggregates[i].RootIssue()
			right, _ := aggregates[j].RootIssue()
			if left.CreatedAt.Equal(right.CreatedAt) {
				return aggregates[i].ID < aggregates[j].ID
			}
			return left.CreatedAt.Before(right.CreatedAt)
		})
		return nil
	})
	return aggregates, err
}

// ReadEpic returns one complete aggregate.
func (s *Store) ReadEpic(id string) (epic.Epic, error) {
	var aggregate epic.Epic
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var err error
		aggregate, err = readEpic(tx, id)
		return err
	})
	if err != nil {
		return epic.Epic{}, err
	}
	return aggregate, nil
}

// WriteEpic validates and atomically replaces one complete aggregate.
func (s *Store) WriteEpic(aggregate epic.Epic) error {
	if err := aggregate.Validate(); err != nil {
		return fmt.Errorf("validate aggregate %s: %w", aggregate.ID, err)
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("DELETE FROM epics WHERE id = ?", aggregate.ID).Error; err != nil {
			return err
		}
		return writeEpic(tx, aggregate)
	})
}

type epicRecord struct {
	ID             string
	Title          string
	Assignee       string
	Repositories   []byte
	Body           string
	State          epic.EpicState
	BranchPrefix   string
	DraftingPasses int
}

type issueRecord struct {
	ID         string
	Title      string
	ParentID   string
	Repository string
	State      epic.IssueState
	CreatedAt  time.Time
	Body       string
	BlockedBy  []byte
}

type pullRequestRecord struct {
	ID            string
	IssueID       string
	Title         string
	Status        epic.PullRequestStatus
	Repository    string
	Number        int
	URL           string
	Head          string
	Base          string
	Flags         []byte
	ReviewedHead  string
	ReviewedBase  string
	Rounds        int
	Reviews       int
	RoundsGranted int
	CodingRounds  int
	Approved      bool
	CreatedAt     time.Time
}

type commentRecord struct {
	ID        string
	Author    string
	CreatedAt time.Time
	Body      string
}

func writeEpic(tx *gorm.DB, aggregate epic.Epic) error {
	repositories, err := json.Marshal(aggregate.Repositories)
	if err != nil {
		return err
	}
	if err := tx.Exec(
		"INSERT INTO epics (id, title, assignee, repositories, body, state, branch_prefix, "+
			"drafting_passes) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		aggregate.ID, aggregate.Title, aggregate.Assignee, repositories, aggregate.Body,
		aggregate.State, aggregate.BranchPrefix, aggregate.DraftingPasses,
	).Error; err != nil {
		return err
	}
	for _, issue := range aggregate.Issues {
		blockedBy, err := json.Marshal(issue.BlockedBy)
		if err != nil {
			return err
		}
		if err := tx.Exec(
			"INSERT INTO issues (id, epic_id, title, parent_id, repository, state, created_at, "+
				"body, blocked_by) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
			issue.ID, aggregate.ID, issue.Title, issue.ParentID, issue.Repository, issue.State,
			issue.CreatedAt.UTC(), issue.Body, blockedBy,
		).Error; err != nil {
			return err
		}
		if err := writeIssueComments(tx, issue.ID, issue.Comments); err != nil {
			return err
		}
	}
	for _, pullRequest := range aggregate.PullRequests {
		flags, err := json.Marshal(pullRequest.Flags)
		if err != nil {
			return err
		}
		if err := tx.Exec(
			"INSERT INTO pull_requests (id, epic_id, issue_id, title, status, repository, number, "+
				"url, head, base, flags, reviewed_head, reviewed_base, rounds, reviews, rounds_granted, "+
				"coding_rounds, approved, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, "+
				"?, ?, ?, ?, ?)",
			pullRequest.ID, aggregate.ID, pullRequest.IssueID, pullRequest.Title, pullRequest.Status,
			pullRequest.Repository, pullRequest.Number, pullRequest.URL, pullRequest.Head, pullRequest.Base,
			flags, pullRequest.ReviewedHead, pullRequest.ReviewedBase, pullRequest.Rounds,
			pullRequest.Reviews, pullRequest.RoundsGranted, pullRequest.CodingRounds, pullRequest.Approved,
			pullRequest.CreatedAt.UTC(),
		).Error; err != nil {
			return err
		}
		if err := writePullRequestComments(tx, pullRequest.ID, pullRequest.Comments); err != nil {
			return err
		}
	}
	return nil
}

func writeIssueComments(tx *gorm.DB, issueID string, comments []epic.Comment) error {
	for _, comment := range comments {
		if err := tx.Exec(
			"INSERT INTO issue_comments (id, issue_id, author, created_at, body) VALUES (?, ?, ?, ?, ?)",
			comment.ID, issueID, comment.Author, comment.CreatedAt.UTC(), comment.Body,
		).Error; err != nil {
			return err
		}
	}
	return nil
}

func writePullRequestComments(tx *gorm.DB, pullRequestID string, comments []epic.Comment) error {
	for _, comment := range comments {
		if err := tx.Exec(
			"INSERT INTO pull_request_comments (id, pull_request_id, author, created_at, body) "+
				"VALUES (?, ?, ?, ?, ?)",
			comment.ID, pullRequestID, comment.Author, comment.CreatedAt.UTC(), comment.Body,
		).Error; err != nil {
			return err
		}
	}
	return nil
}

func readEpic(tx *gorm.DB, id string) (epic.Epic, error) {
	var record epicRecord
	result := tx.Raw(
		"SELECT id, title, assignee, repositories, body, state, branch_prefix, drafting_passes "+
			"FROM epics WHERE id = ?", id,
	).Scan(&record)
	if result.Error != nil {
		return epic.Epic{}, result.Error
	}
	if result.RowsAffected == 0 {
		return epic.Epic{}, os.ErrNotExist
	}
	var repositories []string
	if err := json.Unmarshal(record.Repositories, &repositories); err != nil {
		return epic.Epic{}, err
	}
	aggregate := epic.Epic{ID: record.ID, Title: record.Title, Assignee: record.Assignee,
		Repositories: repositories, Body: record.Body, State: record.State,
		BranchPrefix: record.BranchPrefix, DraftingPasses: record.DraftingPasses}
	var issues []issueRecord
	if err := tx.Raw(
		"SELECT id, title, parent_id, repository, state, created_at, body, blocked_by "+
			"FROM issues WHERE epic_id = ? ORDER BY created_at, id", id,
	).Scan(&issues).Error; err != nil {
		return epic.Epic{}, err
	}
	for _, issueRecord := range issues {
		var blockedBy []string
		if err := json.Unmarshal(issueRecord.BlockedBy, &blockedBy); err != nil {
			return epic.Epic{}, err
		}
		comments, err := readIssueComments(tx, issueRecord.ID)
		if err != nil {
			return epic.Epic{}, err
		}
		aggregate.Issues = append(aggregate.Issues, epic.Issue{
			ID: issueRecord.ID, Title: issueRecord.Title, ParentID: issueRecord.ParentID,
			Repository: issueRecord.Repository, State: issueRecord.State,
			CreatedAt: issueRecord.CreatedAt, Body: issueRecord.Body,
			BlockedBy: blockedBy, Comments: comments,
		})
	}
	var pullRequests []pullRequestRecord
	if err := tx.Raw(
		"SELECT id, issue_id, title, status, repository, number, url, head, base, flags, "+
			"reviewed_head, reviewed_base, rounds, reviews, rounds_granted, coding_rounds, approved, "+
			"created_at FROM pull_requests WHERE epic_id = ? ORDER BY created_at, id", id,
	).Scan(&pullRequests).Error; err != nil {
		return epic.Epic{}, err
	}
	for _, pullRequestRecord := range pullRequests {
		var flags []epic.PullRequestFlag
		if err := json.Unmarshal(pullRequestRecord.Flags, &flags); err != nil {
			return epic.Epic{}, err
		}
		comments, err := readPullRequestComments(tx, pullRequestRecord.ID)
		if err != nil {
			return epic.Epic{}, err
		}
		aggregate.PullRequests = append(aggregate.PullRequests, epic.PullRequest{
			ID: pullRequestRecord.ID, IssueID: pullRequestRecord.IssueID,
			Title: pullRequestRecord.Title, Status: pullRequestRecord.Status,
			Repository: pullRequestRecord.Repository, Number: pullRequestRecord.Number,
			URL: pullRequestRecord.URL, Head: pullRequestRecord.Head, Base: pullRequestRecord.Base,
			Flags: flags, ReviewedHead: pullRequestRecord.ReviewedHead,
			ReviewedBase: pullRequestRecord.ReviewedBase, Rounds: pullRequestRecord.Rounds,
			Reviews: pullRequestRecord.Reviews, RoundsGranted: pullRequestRecord.RoundsGranted,
			CodingRounds: pullRequestRecord.CodingRounds, Approved: pullRequestRecord.Approved,
			CreatedAt: pullRequestRecord.CreatedAt, Comments: comments,
		})
	}
	if err := aggregate.Validate(); err != nil {
		return epic.Epic{}, fmt.Errorf("validate aggregate %s: %w", aggregate.ID, err)
	}
	return aggregate, nil
}

func readIssueComments(tx *gorm.DB, issueID string) ([]epic.Comment, error) {
	var records []commentRecord
	if err := tx.Raw(
		"SELECT id, author, created_at, body FROM issue_comments WHERE issue_id = ? "+
			"ORDER BY created_at, id", issueID,
	).Scan(&records).Error; err != nil {
		return nil, err
	}
	return commentsFrom(records), nil
}

func readPullRequestComments(tx *gorm.DB, pullRequestID string) ([]epic.Comment, error) {
	var records []commentRecord
	if err := tx.Raw(
		"SELECT id, author, created_at, body FROM pull_request_comments WHERE pull_request_id = ? "+
			"ORDER BY created_at, id", pullRequestID,
	).Scan(&records).Error; err != nil {
		return nil, err
	}
	return commentsFrom(records), nil
}

func commentsFrom(records []commentRecord) []epic.Comment {
	if len(records) == 0 {
		return nil
	}
	comments := make([]epic.Comment, 0, len(records))
	for _, record := range records {
		comments = append(comments, epic.Comment{
			ID: record.ID, Author: record.Author, CreatedAt: record.CreatedAt, Body: record.Body,
		})
	}
	return comments
}

type settingsRecord struct {
	SetupScript string
	Roles       []byte
}

func settingsRecordFrom(
	setupScript string, roles map[agent.AgentRole]agent.AgentProfile,
) (settingsRecord, error) {
	encoded, err := json.Marshal(roles)
	if err != nil {
		return settingsRecord{}, err
	}
	return settingsRecord{SetupScript: setupScript, Roles: encoded}, nil
}

func (r settingsRecord) agentSettings() (agent.AgentSettings, error) {
	roles := map[agent.AgentRole]agent.AgentProfile(nil)
	if err := json.Unmarshal(r.Roles, &roles); err != nil {
		return agent.AgentSettings{}, err
	}
	settings := agent.AgentSettings{SetupScript: r.SetupScript, Roles: roles}
	if err := validateAgentSettings(settings); err != nil {
		return agent.AgentSettings{}, err
	}
	return settings, nil
}

func (r settingsRecord) repositorySettings() (agent.RepositorySettings, error) {
	roles := map[agent.AgentRole]agent.AgentProfile(nil)
	if err := json.Unmarshal(r.Roles, &roles); err != nil {
		return agent.RepositorySettings{}, err
	}
	settings := agent.RepositorySettings{SetupScript: r.SetupScript, Roles: roles}
	if err := validateRepositorySettings(settings); err != nil {
		return agent.RepositorySettings{}, err
	}
	return settings, nil
}

func validateProject(project Project) error {
	if strings.TrimSpace(project.Name) == "" {
		return errors.New("project name is required")
	}
	return validateRepositories(project.Repositories)
}

func validateRepositories(repositories []string) error {
	seen := make(map[string]struct{}, len(repositories))
	for _, repository := range repositories {
		if err := validateRepository(repository); err != nil {
			return err
		}
		if _, exists := seen[repository]; exists {
			return fmt.Errorf("duplicate repository %q", repository)
		}
		seen[repository] = struct{}{}
	}
	return nil
}

func validateAgentSettings(settings agent.AgentSettings) error {
	if err := settings.Validate(); err != nil {
		return err
	}
	if settings.SetupScript != "" {
		return validatePath(settings.SetupScript)
	}
	return nil
}

func validateRepositorySettings(settings agent.RepositorySettings) error {
	if err := settings.Validate(); err != nil {
		return err
	}
	if settings.SetupScript != "" {
		return validatePath(settings.SetupScript)
	}
	return nil
}

func validateRepository(repository string) error {
	parts := strings.Split(repository, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" ||
		strings.Contains(repository, "..") || strings.Contains(repository, "\\") {
		return fmt.Errorf("repository must use owner/name form, got %q", repository)
	}
	return nil
}

func validatePath(path string) error {
	if path == "" {
		return errors.New("path is required")
	}
	if strings.ContainsAny(path, "\x00\r\n") {
		return fmt.Errorf("path %q contains control characters", path)
	}
	if filepath.IsAbs(path) {
		return fmt.Errorf("path %q must be relative", path)
	}
	cleaned := filepath.Clean(path)
	if cleaned == "." || cleaned == ".." ||
		strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path %q escapes the store", path)
	}
	return nil
}

func sortedRepositories(repositories []string) []string {
	sorted := append([]string(nil), repositories...)
	sort.Strings(sorted)
	return sorted
}
