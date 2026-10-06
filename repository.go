package goapp

import (
	"context"

	"gorm.io/gorm"
)

// BaseRepository provides generic CRUD so domains only write the queries that
// are actually specific to them. Embed it in a domain repository:
//
//	type Repository struct {
//	    *goapp.BaseRepository[Dokter]
//	}
type BaseRepository[T any] struct {
	DB *gorm.DB
}

func NewBaseRepository[T any](db *gorm.DB) *BaseRepository[T] {
	return &BaseRepository[T]{DB: db}
}

func (r *BaseRepository[T]) FindAll(ctx context.Context) ([]T, error) {
	var out []T
	return out, r.DB.WithContext(ctx).Find(&out).Error
}

func (r *BaseRepository[T]) FindByID(ctx context.Context, id any) (*T, error) {
	var out T
	if err := r.DB.WithContext(ctx).First(&out, id).Error; err != nil {
		return nil, err
	}
	return &out, nil
}

func (r *BaseRepository[T]) Create(ctx context.Context, data *T) error {
	return r.DB.WithContext(ctx).Create(data).Error
}

func (r *BaseRepository[T]) Update(ctx context.Context, data *T) error {
	return r.DB.WithContext(ctx).Save(data).Error
}

func (r *BaseRepository[T]) Delete(ctx context.Context, id any) error {
	var out T
	return r.DB.WithContext(ctx).Delete(&out, id).Error
}
