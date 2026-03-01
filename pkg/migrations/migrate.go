package migrations

import (
	"context"

	"github.com/asciifaceman/lived/pkg/dal"
	"gorm.io/gorm"
)

func Run(ctx context.Context, database *gorm.DB) error {
	return database.WithContext(ctx).AutoMigrate(dal.Models()...)
}
