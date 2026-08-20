package projectstore

import (
	"time"

	"github.com/tinker-works/donsy/internal/domain/agent"
)

type projectRecord struct {
	ID           uint `gorm:"primaryKey"`
	Name         string
	LastOpenedAt time.Time
}

type organisationRecord struct {
	Name string `gorm:"primaryKey"`
}

type repositoryRecord struct {
	FullName     string `gorm:"primaryKey"`
	Name         string
	HTTPURL      string
	SSHURL       string
	Organisation string `gorm:"index"`
}

type sandboxRecord struct {
	ID          string `gorm:"primaryKey"`
	ProjectID   uint   `gorm:"index;not null"`
	Name        string `gorm:"uniqueIndex;not null"`
	Role        string `gorm:"not null"`
	SubjectKind string `gorm:"not null"`
	SubjectID   string `gorm:"not null"`
	Status      string `gorm:"not null"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type agentRunRecord struct {
	ID        string `gorm:"primaryKey"`
	ProjectID uint   `gorm:"index;not null"`
	// Defaults keep rows written before these fields existed readable during
	// SQLite's table migration.
	SandboxID   string `gorm:"index;not null;default:''"`
	Role        string `gorm:"not null"`
	SubjectKind string `gorm:"not null"`
	SubjectID   string `gorm:"index;not null"`
	Engine      string `gorm:"not null"`
	Agent       string `gorm:"not null;default:''"`
	Variant     string `gorm:"not null;default:''"`
	SessionMode string `gorm:"not null"`
	Status      string `gorm:"not null"`
	Round       int    `gorm:"not null"`
	Error       string
	// Usage fields were added after the first run records. Their zero defaults
	// preserve the distinction made by agent.RunUsage.Reported.
	TokensIn   int     `gorm:"not null;default:0"`
	TokensOut  int     `gorm:"not null;default:0"`
	CostUSD    float64 `gorm:"not null;default:0"`
	CreatedAt  time.Time
	StartedAt  *time.Time
	FinishedAt *time.Time
}

func sandboxToRecord(sandbox agent.Sandbox) sandboxRecord {
	return sandboxRecord{
		ID: sandbox.ID, ProjectID: sandbox.ProjectID, Name: sandbox.Name,
		Role: string(sandbox.Role), SubjectKind: string(sandbox.Subject.Kind),
		SubjectID: sandbox.Subject.ID, Status: string(sandbox.Status),
		CreatedAt: sandbox.CreatedAt, UpdatedAt: sandbox.UpdatedAt,
	}
}

func recordToSandbox(record sandboxRecord) agent.Sandbox {
	return agent.Sandbox{
		ID: record.ID, ProjectID: record.ProjectID, Name: record.Name,
		Role: agent.AgentRole(record.Role),
		Subject: agent.AgentSubject{
			Kind: agent.AgentSubjectKind(record.SubjectKind), ID: record.SubjectID,
		},
		Status:    agent.SandboxStatus(record.Status),
		CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt,
	}
}

func runToRecord(run agent.AgentRun) agentRunRecord {
	return agentRunRecord{
		ID: run.ID, ProjectID: run.ProjectID, SandboxID: run.SandboxID,
		Role: string(run.Role), SubjectKind: string(run.Subject.Kind),
		SubjectID: run.Subject.ID, Engine: string(run.Engine), Agent: run.Agent,
		Variant: run.Variant, SessionMode: string(run.SessionMode),
		Status: string(run.Status), Round: run.Round, Error: run.Error,
		TokensIn: run.Usage.TokensIn, TokensOut: run.Usage.TokensOut,
		CostUSD: run.Usage.CostUSD, CreatedAt: run.CreatedAt,
		StartedAt: run.StartedAt, FinishedAt: run.FinishedAt,
	}
}

func recordToRun(record agentRunRecord) agent.AgentRun {
	return agent.AgentRun{
		ID: record.ID, ProjectID: record.ProjectID, SandboxID: record.SandboxID,
		Role: agent.AgentRole(record.Role),
		Subject: agent.AgentSubject{
			Kind: agent.AgentSubjectKind(record.SubjectKind), ID: record.SubjectID,
		},
		Engine: agent.AgentEngine(record.Engine), Agent: record.Agent,
		Variant: record.Variant, SessionMode: agent.SessionMode(record.SessionMode),
		Status: agent.AgentRunStatus(record.Status), Round: record.Round,
		Error: record.Error,
		Usage: agent.RunUsage{
			TokensIn: record.TokensIn, TokensOut: record.TokensOut, CostUSD: record.CostUSD,
		},
		CreatedAt: record.CreatedAt, StartedAt: record.StartedAt,
		FinishedAt: record.FinishedAt,
	}
}
