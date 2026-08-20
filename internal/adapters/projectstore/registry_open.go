package projectstore

import (
	"fmt"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/tinker-works/donsy/internal/domain/agent"
)

// Open creates or opens the daemon registry at path.
func Open(path string) (*Registry, error) {
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{
		// The daemon owns stdout, so GORM diagnostics cannot write to it.
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, err
	}
	if err := serializeWrites(db); err != nil {
		return nil, err
	}
	if db.Migrator().HasTable("tracker_records") && !db.Migrator().HasTable("project_records") {
		if err := db.Migrator().RenameTable("tracker_records", "project_records"); err != nil {
			return nil, err
		}
	}
	if err := migrateProjectSchema(db); err != nil {
		return nil, err
	}
	if err := db.AutoMigrate(
		&projectRecord{}, &organisationRecord{}, &repositoryRecord{},
		&sandboxRecord{}, &agentRunRecord{},
	); err != nil {
		return nil, err
	}
	return &Registry{db: db}, nil
}

// OpenRegistry is an explicit spelling for callers wiring several local stores.
func OpenRegistry(path string) (*Registry, error) {
	return Open(path)
}

// migrateProjectSchema removes tracker checkout metadata from existing local
// registries. SQLite does not infer destructive changes from AutoMigrate.
func migrateProjectSchema(db *gorm.DB) error {
	if !db.Migrator().HasTable("project_records") {
		return nil
	}
	columns, err := db.Migrator().ColumnTypes("project_records")
	if err != nil {
		return err
	}
	for _, column := range columns {
		name := column.Name()
		if name != "remote_url" && name != "local_path" && name != "branch" {
			continue
		}
		if err := db.Exec("ALTER TABLE project_records DROP COLUMN " + name).Error; err != nil {
			return fmt.Errorf("remove project column %s: %w", name, err)
		}
	}
	return nil
}

// serializeWrites pins the pool to one connection and puts SQLite in WAL mode.
// Concurrent rounds write runtime records while the daemon polls them, so this
// queues writers in-process and keeps reads from waiting behind the journal.
func serializeWrites(db *gorm.DB) error {
	database, err := db.DB()
	if err != nil {
		return err
	}
	database.SetMaxOpenConns(1)
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL", "PRAGMA busy_timeout=5000",
	} {
		if err := db.Exec(pragma).Error; err != nil {
			return fmt.Errorf("%s: %w", pragma, err)
		}
	}
	return nil
}

func (r *Registry) SaveSandbox(sandbox agent.Sandbox) error {
	if err := sandbox.Validate(); err != nil {
		return err
	}
	record := sandboxToRecord(sandbox)
	return r.db.Save(&record).Error
}

func (r *Registry) ListSandboxes(projectID uint) ([]agent.Sandbox, error) {
	var records []sandboxRecord
	if err := r.db.Where("project_id = ?", projectID).Order("name").Find(&records).Error; err != nil {
		return nil, err
	}
	sandboxes := make([]agent.Sandbox, 0, len(records))
	for _, record := range records {
		sandboxes = append(sandboxes, recordToSandbox(record))
	}
	return sandboxes, nil
}

// DeleteProjectRuntime drops machine-local records for one project. Runtime
// rows are intentionally not foreign-keyed to project records.
func (r *Registry) DeleteProjectRuntime(projectID uint) error {
	if err := r.db.Delete(&sandboxRecord{}, "project_id = ?", projectID).Error; err != nil {
		return err
	}
	return r.db.Delete(&agentRunRecord{}, "project_id = ?", projectID).Error
}

func (r *Registry) DeleteSubjectRuntime(projectID uint, subject agent.AgentSubject) error {
	if err := subject.Validate(); err != nil {
		return err
	}
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&sandboxRecord{},
			"project_id = ? AND subject_kind = ? AND subject_id = ?",
			projectID, subject.Kind, subject.ID,
		).Error; err != nil {
			return err
		}
		return tx.Delete(&agentRunRecord{},
			"project_id = ? AND subject_kind = ? AND subject_id = ?",
			projectID, subject.Kind, subject.ID,
		).Error
	})
}

func (r *Registry) SaveAgentRun(run agent.AgentRun) error {
	if err := run.Validate(); err != nil {
		return err
	}
	record := runToRecord(run)
	return r.db.Save(&record).Error
}

func (r *Registry) ListAgentRuns(
	projectID uint,
	subject agent.AgentSubject,
) ([]agent.AgentRun, error) {
	if err := subject.Validate(); err != nil {
		return nil, err
	}
	var records []agentRunRecord
	if err := r.db.Where(
		"project_id = ? AND subject_kind = ? AND subject_id = ?",
		projectID, subject.Kind, subject.ID,
	).Order("round desc").Find(&records).Error; err != nil {
		return nil, err
	}
	runs := make([]agent.AgentRun, 0, len(records))
	for _, record := range records {
		runs = append(runs, recordToRun(record))
	}
	return runs, nil
}

func (r *Registry) ListProjectAgentRuns(projectID uint) ([]agent.AgentRun, error) {
	var records []agentRunRecord
	if err := r.db.Where("project_id = ?", projectID).
		Order("created_at desc").Find(&records).Error; err != nil {
		return nil, err
	}
	runs := make([]agent.AgentRun, 0, len(records))
	for _, record := range records {
		runs = append(runs, recordToRun(record))
	}
	return runs, nil
}

func (r *Registry) GetAgentRun(runID string) (agent.AgentRun, error) {
	var record agentRunRecord
	if err := r.db.First(&record, "id = ?", runID).Error; err != nil {
		return agent.AgentRun{}, err
	}
	return recordToRun(record), nil
}
