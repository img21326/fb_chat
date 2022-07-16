package user

import (
	"context"

	"github.com/img21326/fb_chat/structure/user"
	"gorm.io/gorm"
)

type UserRepo struct {
	DB *gorm.DB
}

func NewUserRepo(db *gorm.DB) UserRepoInterFace {
	return &UserRepo{
		DB: db,
	}
}

func (repo *UserRepo) Create(ctx context.Context, u *user.User) error {
	return repo.DB.Create(&u).Error
}

func (repo *UserRepo) FindByFbID(ctx context.Context, FbId string) (u *user.User, err error) {
	err = repo.DB.Where("fb_id = ?", FbId).First(&u).Error
	return
}
