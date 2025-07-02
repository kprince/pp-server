package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/google"

	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"

	"github.com/hibiken/asynq"
	"github.com/perfect-panel/server/internal/model/payment"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/payment/stripe"
	"github.com/perfect-panel/server/queue/types"
)

type StripeNotifyLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewStripeNotifyLogic Stripe notify
func NewStripeNotifyLogic(ctx context.Context, svcCtx *svc.ServiceContext) *StripeNotifyLogic {
	return &StripeNotifyLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *StripeNotifyLogic) StripeNotify(r *http.Request, w http.ResponseWriter) error {
	const MaxBodyBytes = int64(65536)
	r.Body = http.MaxBytesReader(w, r.Body, MaxBodyBytes)
	payload, err := io.ReadAll(r.Body)
	if err != nil {
		l.Errorw("[StripeNotify] error", logger.Field("errors", err.Error()))
		return err
	}
	signature := r.Header.Get("Stripe-Signature")
	stripeConfig, ok := l.ctx.Value(constant.CtxKeyPayment).(*payment.Payment)
	if !ok {
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "payment config not found")
	}
	config := payment.StripeConfig{}
	if err := json.Unmarshal([]byte(stripeConfig.Config), &config); err != nil {
		return err
	}
	client := stripe.NewClient(stripe.Config{
		PublicKey:     config.PublicKey,
		SecretKey:     config.SecretKey,
		WebhookSecret: config.WebhookSecret,
	})

	notify, err := client.ParseNotify(payload, signature)
	if err != nil {
		l.Errorw("[StripeNotify] error", logger.Field("errors", err.Error()))
		return err
	}

	orderInfo, err := l.svcCtx.OrderModel.FindOneByOrderNo(l.ctx, notify.OrderNo)

	if err != nil {
		l.Logger.Error("[StripeNotify] Find order failed", logger.Field("error", err.Error()), logger.Field("orderNo", notify.OrderNo))
		return errors.Wrapf(xerr.NewErrCode(xerr.OrderNotExist), "order not exist: %v", notify.OrderNo)
	}
	if notify.EventType == "payment_intent.succeeded" {
		if orderInfo.Status == 5 {
			return nil
		}
		//query first paid order
		firstOrder, _ := l.svcCtx.OrderModel.FindOneByUserId(l.ctx, orderInfo.Id, 2)
		// update order status
		err = l.svcCtx.OrderModel.UpdateOrderStatus(l.ctx, notify.OrderNo, 2)
		if err != nil {
			return err
		}
		if orderInfo.Gclid != "" && firstOrder == nil {
			gac := google.NewGAClient()
			gac.SendEvent("purchase", orderInfo.UserId, map[string]any{
				"trasaction_id": orderInfo.Id,
				"currency":      "USD",
				"value":         orderInfo.Amount,
				"items": []map[string]any{
					{
						"item_id":   orderInfo.SubscribeId,
						"item_name": orderInfo.SubscribeName,
					},
				},
			})
		}
		// create ActivateOrder task
		payload := types.ForthwithActivateOrderPayload{
			OrderNo: notify.OrderNo,
		}
		bytes, err := json.Marshal(payload)
		if err != nil {
			l.Errorw("[StripeNotify] Marshal error", logger.Field("errors", err.Error()), logger.Field("payload", payload))
			return err
		}
		task := asynq.NewTask(types.ForthwithActivateOrder, bytes)
		_, err = l.svcCtx.Queue.Enqueue(task)
		if err != nil {
			l.Errorw("[StripeNotify] Enqueue error", logger.Field("errors", err.Error()))
			return err
		}
		l.Infow("[StripeNotify] success", logger.Field("orderNo", notify.OrderNo))
	}
	return nil
}
