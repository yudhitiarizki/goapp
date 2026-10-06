package goapp

import (
	"time"

	"github.com/google/uuid"
	"github.com/yudhitiarizki/goapp/reqctx"
	"gorm.io/gorm"
)

// BaseEntity is an embeddable model with audit columns. Embed it instead of
// gorm.Model to get id + created/updated/deleted timestamps AND the user who
// performed each action. The *_by fields are filled automatically from the
// caller's id in the request context (set from the JWT), so domains don't have
// to touch them.
//
//	type Pasien struct {
//	    goapp.BaseEntity
//	    Nama string
//	}
type BaseEntity struct {
	ID        uuid.UUID      `gorm:"type:uuid;primarykey" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	CreatedBy string         `json:"created_by,omitempty"`
	UpdatedAt time.Time      `json:"updated_at"`
	UpdatedBy string         `json:"updated_by,omitempty"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
	DeletedBy string         `json:"deleted_by,omitempty"`
}

// currentUser returns the caller id from the context carried by the GORM
// statement (empty for background/anonymous operations).
func currentUser(tx *gorm.DB) string {
	if tx.Statement == nil || tx.Statement.Context == nil {
		return ""
	}
	return reqctx.FromContext(tx.Statement.Context).UserID
}

// BeforeCreate generates a UUID (when unset) and stamps created_by/updated_by.
func (b *BaseEntity) BeforeCreate(tx *gorm.DB) error {
	if b.ID == uuid.Nil {
		b.ID = uuid.New()
	}
	if uid := currentUser(tx); uid != "" {
		if b.CreatedBy == "" {
			b.CreatedBy = uid
		}
		if b.UpdatedBy == "" {
			b.UpdatedBy = uid
		}
	}
	return nil
}

// BeforeUpdate stamps updated_by with the caller.
func (b *BaseEntity) BeforeUpdate(tx *gorm.DB) error {
	if uid := currentUser(tx); uid != "" {
		tx.Statement.SetColumn("updated_by", uid)
	}
	return nil
}

// BeforeDelete stamps deleted_by with the caller. For soft-deleted models this
// column is written alongside deleted_at in the same UPDATE.
func (b *BaseEntity) BeforeDelete(tx *gorm.DB) error {
	if uid := currentUser(tx); uid != "" {
		tx.Statement.SetColumn("deleted_by", uid)
	}
	return nil
}
