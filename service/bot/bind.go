package bot

import (
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/hi2shark/santaizi-dashboard/model"
	"github.com/hi2shark/santaizi-dashboard/pkg/utils"
	"github.com/hi2shark/santaizi-dashboard/service/singleton"
)

func IssueBindCode(role uint8, ttl time.Duration, createdBy string) (*model.BotBindCode, error) {
	if role < model.BotRoleViewer {
		role = model.BotRoleViewer
	}
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	code, err := utils.GenerateRandomString(16)
	if err != nil {
		return nil, err
	}
	row := &model.BotBindCode{
		Code:      strings.ToUpper(code),
		Role:      role,
		ExpiresAt: time.Now().Add(ttl),
		CreatedBy: createdBy,
	}
	if err := singleton.DB.Create(row).Error; err != nil {
		return nil, err
	}
	return row, nil
}

func ConsumeBindCode(code string, chat *model.BotChat) (*model.BotBindCode, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" || chat == nil {
		return nil, errBindInvalid
	}
	var row model.BotBindCode
	now := time.Now()
	tx := singleton.DB.Begin()
	if err := tx.Where("code = ? AND used_at IS NULL AND expires_at > ?", code, now).First(&row).Error; err != nil {
		tx.Rollback()
		return nil, errBindInvalid
	}
	used := now
	row.UsedAt = &used
	row.UsedByChatID = chat.ChatID
	if err := tx.Save(&row).Error; err != nil {
		tx.Rollback()
		return nil, err
	}
	var existing model.BotChat
	err := tx.Where("chat_id = ?", chat.ChatID).First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		chat.Role = row.Role
		chat.Enabled = model.BoolPtr(true)
		chat.LastSeenAt = now
		if err := tx.Create(chat).Error; err != nil {
			tx.Rollback()
			return nil, err
		}
	} else if err != nil {
		tx.Rollback()
		return nil, err
	} else {
		existing.Role = row.Role
		existing.Kind = chat.Kind
		existing.Title = chat.Title
		existing.BoundBy = chat.BoundBy
		existing.Enabled = model.BoolPtr(true)
		existing.LastSeenAt = now
		if err := tx.Save(&existing).Error; err != nil {
			tx.Rollback()
			return nil, err
		}
		*chat = existing
	}
	if err := tx.Commit().Error; err != nil {
		return nil, err
	}
	return &row, nil
}

var errBindInvalid = bindError("绑定码无效或已过期")

type bindError string

func (e bindError) Error() string { return string(e) }
