package projectstore

import (
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/tinker-works/donsy/internal/domain"
)

func (r *Registry) ListOrganisations() ([]domain.Organisation, error) {
	var records []organisationRecord
	if err := r.db.Order("name").Find(&records).Error; err != nil {
		return nil, err
	}
	organisations := make([]domain.Organisation, 0, len(records))
	for _, record := range records {
		organisations = append(organisations, domain.Organisation{Name: record.Name})
	}
	return organisations, nil
}

func (r *Registry) CreateOrganisation(organisation *domain.Organisation) error {
	return r.db.FirstOrCreate(&organisationRecord{Name: organisation.Name}).Error
}

func (r *Registry) DeleteOrganisation(name string) error {
	if err := r.db.Delete(&organisationRecord{}, "name = ?", name).Error; err != nil {
		return err
	}
	return r.db.Delete(&repositoryRecord{}, "organisation = ?", name).Error
}

func (r *Registry) ListRepositories() ([]domain.Repository, error) {
	var records []repositoryRecord
	if err := r.db.Order("organisation, full_name").Find(&records).Error; err != nil {
		return nil, err
	}
	repositories := make([]domain.Repository, 0, len(records))
	for _, record := range records {
		repositories = append(repositories, domain.Repository{
			Name: record.Name, FullName: record.FullName, HTTPURL: record.HTTPURL,
			SSHURL: record.SSHURL, Organisation: record.Organisation,
		})
	}
	return repositories, nil
}

// SaveRepository upserts by full name, so re-adding a repository refreshes it
// rather than failing on the primary key.
func (r *Registry) SaveRepository(repository domain.Repository) error {
	record := repositoryRecord{
		FullName: repository.FullName, Name: repository.Name,
		HTTPURL: repository.HTTPURL, SSHURL: repository.SSHURL,
		Organisation: repository.Organisation,
	}
	return r.db.Save(&record).Error
}

func (r *Registry) ReplaceRepositories(
	organisation string, repositories []domain.Repository,
) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&repositoryRecord{}, "organisation = ?", organisation).Error; err != nil {
			return err
		}
		for _, repository := range repositories {
			if err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "full_name"}},
				UpdateAll: true,
			}).Create(&repositoryRecord{
				FullName: repository.FullName, Name: repository.Name,
				HTTPURL: repository.HTTPURL, SSHURL: repository.SSHURL,
				Organisation: organisation,
			}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *Registry) List() ([]domain.Project, error) {
	var records []projectRecord
	if err := r.db.Order("last_opened_at desc").Find(&records).Error; err != nil {
		return nil, err
	}
	projects := make([]domain.Project, 0, len(records))
	for _, record := range records {
		projects = append(projects, domain.Project{
			ID: record.ID, Name: record.Name, LastOpenedAt: record.LastOpenedAt,
		})
	}
	return projects, nil
}

func (r *Registry) Create(project *domain.Project) error {
	record := projectRecord{
		ID: project.ID, Name: project.Name, LastOpenedAt: project.LastOpenedAt,
	}
	if err := r.db.Create(&record).Error; err != nil {
		return err
	}
	project.ID = record.ID
	return nil
}

func (r *Registry) Touch(projectID uint) error {
	return r.db.Model(&projectRecord{}).
		Where("id = ?", projectID).
		Update("last_opened_at", time.Now().UTC()).Error
}

func (r *Registry) Delete(projectID uint) error {
	return r.db.Delete(&projectRecord{}, "id = ?", projectID).Error
}

// Close releases the registry's SQLite handle.
func (r *Registry) Close() error {
	database, err := r.db.DB()
	if err != nil {
		return err
	}
	return database.Close()
}
