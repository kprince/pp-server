package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/logic/common"
	"github.com/perfect-panel/server/internal/model/user"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/google"
	"github.com/perfect-panel/server/pkg/jwt"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/uuidx"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"gorm.io/gorm"
)

type UserRegisterLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewUserRegisterLogic User register
func NewUserRegisterLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UserRegisterLogic {
	return &UserRegisterLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UserRegisterLogic) UserRegister(req *types.UserRegisterRequest) (resp *types.LoginResponse, err error) {

	c := l.svcCtx.Config.Register
	email := l.svcCtx.Config.Email
	var referer *user.User
	// Check if the registration is stopped
	if c.StopRegister {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.StopRegister), "stop register")
	}

	if req.Invite == "" {
		if l.svcCtx.Config.Invite.ForcedInvite {
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.InviteCodeError), "invite code is required")
		}
	} else {
		// Check if the invite code is valid
		referer, err = l.svcCtx.UserModel.FindOneByReferCode(l.ctx, req.Invite)
		if err != nil {
			l.Errorw("FindOneByReferCode Error", logger.Field("error", err))
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.InviteCodeError), "invite code is invalid")
		}
	}

	// if the email verification is enabled, the verification code is required
	if email.EnableVerify {
		cacheKey := fmt.Sprintf("%s:%s:%s", config.AuthCodeCacheKey, constant.Register, req.Email)
		value, err := l.svcCtx.Redis.Get(l.ctx, cacheKey).Result()
		if err != nil {
			l.Errorw("Redis Error", logger.Field("error", err.Error()), logger.Field("cacheKey", cacheKey))
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.VerifyCodeError), "code error")
		}
		var payload common.CacheKeyPayload
		err = json.Unmarshal([]byte(value), &payload)
		if err != nil {
			l.Errorw("Unmarshal Error", logger.Field("error", err.Error()), logger.Field("value", value))
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.VerifyCodeError), "code error")
		}
		if payload.Code != req.Code {
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.VerifyCodeError), "code error")
		}
	}
	// Check if the user exists
	_, err = l.svcCtx.UserModel.FindOneByEmail(l.ctx, req.Email)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		l.Errorw("FindOneByEmail Error", logger.Field("error", err))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "query user info failed: %v", err.Error())
	} else if err == nil {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.UserExist), "user email exist: %v", req.Email)
	}
	// Generate password
	pwd := tool.EncodePassWord(req.Password)
	userInfo := &user.User{
		Password: pwd,
	}
	if referer != nil {
		userInfo.RefererId = referer.Id
	}
	// Save user source
	if req.Gclid != "" {
		userInfo.From = "google"
	}
	err = l.svcCtx.UserModel.Transaction(l.ctx, func(db *gorm.DB) error {
		// Save user information
		if err := db.Create(userInfo).Error; err != nil {
			return err
		}
		// Generate ReferCode
		userInfo.ReferCode = uuidx.UserInviteCode(userInfo.Id)
		// Update ReferCode
		if err := db.Model(&user.User{}).Where("id = ?", userInfo.Id).Update("refer_code", userInfo.ReferCode).Error; err != nil {
			return err
		}
		// create user auth info
		authInfo := &user.AuthMethods{
			UserId:         userInfo.Id,
			AuthType:       "email",
			AuthIdentifier: req.Email,
			Verified:       email.EnableVerify,
		}
		if err = db.Create(authInfo).Error; err != nil {
			return err
		}

		if l.svcCtx.Config.Register.EnableTrial {
			// Active trial
			if err = l.activeTrial(userInfo.Id); err != nil {
				return err
			}
		}
		return nil
	})
	// Generate session id
	sessionId := uuidx.NewUUID().String()
	// Generate token
	token, err := jwt.NewJwtToken(
		l.svcCtx.Config.JwtAuth.AccessSecret,
		time.Now().Unix(),
		l.svcCtx.Config.JwtAuth.AccessExpire,
		jwt.WithOption("UserId", userInfo.Id),
		jwt.WithOption("SessionId", sessionId),
	)
	if err != nil {
		l.Logger.Error("[UserLogin] token generate error", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "token generate error: %v", err.Error())
	}
	// Set session id
	sessionIdCacheKey := fmt.Sprintf("%v:%v", config.SessionIdKey, sessionId)
	if err := l.svcCtx.Redis.Set(l.ctx, sessionIdCacheKey, userInfo.Id, time.Duration(l.svcCtx.Config.JwtAuth.AccessExpire)*time.Second).Err(); err != nil {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "set session id error: %v", err.Error())
	}
	loginStatus := true
	defer func() {
		//Send register event to GA4
		if req.Gclid != "" {
			gac := google.NewGAClient()
			gac.SendEvent("sign_up", userInfo.Id, map[string]any{})
		}
		if token != "" && userInfo.Id != 0 {
			if err := l.svcCtx.UserModel.InsertLoginLog(l.ctx, &user.LoginLog{
				UserId:    userInfo.Id,
				LoginIP:   req.IP,
				UserAgent: req.UserAgent,
				Success:   &loginStatus,
			}); err != nil {
				l.Logger.Error("[UserRegister] insert login log error", logger.Field("error", err.Error()))
			}
		}
	}()
	return &types.LoginResponse{
		Token: token,
	}, nil
}

func (l *UserRegisterLogic) activeTrial(uid int64) error {
	sub, err := l.svcCtx.SubscribeModel.FindOne(l.ctx, l.svcCtx.Config.Register.TrialSubscribe)
	if err != nil {
		return err
	}
	userSub := &user.Subscribe{
		UserId:      uid,
		OrderId:     0,
		SubscribeId: sub.Id,
		StartTime:   time.Now(),
		ExpireTime:  tool.AddTime(l.svcCtx.Config.Register.TrialTimeUnit, l.svcCtx.Config.Register.TrialTime, time.Now()),
		Traffic:     sub.Traffic,
		Download:    0,
		Upload:      0,
		Token:       uuidx.SubscribeToken(fmt.Sprintf("Trial-%v", uid)),
		UUID:        uuidx.NewUUID().String(),
		Status:      1,
	}
	return l.svcCtx.UserModel.InsertSubscribe(l.ctx, userSub)
}
