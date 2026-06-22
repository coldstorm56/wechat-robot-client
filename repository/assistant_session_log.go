package repository

import (
	"context"
	"time"
	"wechat-robot-client/model"

	"gorm.io/gorm"
)

type AssistantSessionLog struct {
	Ctx context.Context
	DB  *gorm.DB
}

func NewAssistantSessionLogRepo(ctx context.Context, db *gorm.DB) *AssistantSessionLog {
	return &AssistantSessionLog{
		Ctx: ctx,
		DB:  db,
	}
}

func (repo *AssistantSessionLog) Create(data *model.AssistantSessionLog) error {
	now := time.Now().Unix()
	if data.CreatedAt == 0 {
		data.CreatedAt = now
	}
	data.UpdatedAt = now
	return repo.DB.WithContext(repo.Ctx).Create(data).Error
}

func (repo *AssistantSessionLog) Update(data *model.AssistantSessionLog) error {
	data.UpdatedAt = time.Now().Unix()
	return repo.DB.WithContext(repo.Ctx).Where("id = ?", data.ID).Updates(data).Error
}
