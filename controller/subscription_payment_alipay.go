package controller

import (
	"fmt"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/smartwalle/alipay/v3"
	"github.com/thanhpk/randstr"
)

type SubscriptionAlipayDirectPayRequest struct {
	PlanId int `json:"plan_id"`
}

func SubscriptionRequestAlipayDirect(c *gin.Context) {
	if !requirePaymentCompliance(c) {
		return
	}

	if !requireTosAcceptance(c) {
		return
	}

	var req SubscriptionAlipayDirectPayRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.PlanId <= 0 {
		common.ApiErrorMsg(c, "参数错误")
		return
	}

	plan, err := model.GetSubscriptionPlanById(req.PlanId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !plan.Enabled {
		common.ApiErrorMsg(c, "套餐未启用")
		return
	}

	userId := c.GetInt("id")
	user, err := model.GetUserById(userId, false)
	if err != nil || user == nil {
		common.ApiErrorMsg(c, "用户不存在")
		return
	}

	if plan.MaxPurchasePerUser > 0 {
		count, err := model.CountUserSubscriptionsByPlan(userId, plan.Id)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		if count >= int64(plan.MaxPurchasePerUser) {
			common.ApiErrorMsg(c, "已达到该套餐购买上限")
			return
		}
	}

	tradeNo := fmt.Sprintf("ALIPAY_DIRECT_SUB-%d-%d-%s", userId, time.Now().UnixMilli(), randstr.String(6))

	order := &model.SubscriptionOrder{
		UserId:          userId,
		PlanId:          plan.Id,
		Money:           plan.PriceAmount,
		TradeNo:         tradeNo,
		PaymentMethod:   model.PaymentMethodAlipayDirect,
		PaymentProvider: model.PaymentProviderAlipayDirect,
		CreateTime:      time.Now().Unix(),
		Status:          common.TopUpStatusPending,
	}
	if err := order.Insert(); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("支付宝 订阅订单创建失败 user_id=%d plan_id=%d trade_no=%s error=%q", userId, plan.Id, tradeNo, err.Error()))
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "创建订单失败"})
		return
	}

	client, err := getAlipayDirectClient()
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("支付宝 订阅客户端初始化失败 user_id=%d plan_id=%d trade_no=%s error=%q", userId, plan.Id, tradeNo, err.Error()))
		order.Status = common.TopUpStatusFailed
		_ = order.Update()
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "支付宝配置错误"})
		return
	}

	var p = alipay.TradePagePay{}
	p.Subject = fmt.Sprintf("订阅-%s", plan.Title)
	p.OutTradeNo = tradeNo
	p.TotalAmount = fmt.Sprintf("%.2f", plan.PriceAmount)
	p.ProductCode = "FAST_INSTANT_TRADE_PAY"
	p.NotifyURL = resolveAlipayDirectNotifyUrl()
	p.ReturnURL = resolveAlipayDirectReturnUrl()

	payUrl, err := client.TradePagePay(p)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("支付宝 订阅 TradePagePay 失败 user_id=%d plan_id=%d trade_no=%s error=%q", userId, plan.Id, tradeNo, err.Error()))
		order.Status = common.TopUpStatusFailed
		_ = order.Update()
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "拉起支付失败"})
		return
	}

	paymentUrl := payUrl.String()
	logger.LogInfo(c.Request.Context(), fmt.Sprintf("支付宝 订阅订单创建成功 user_id=%d plan_id=%d trade_no=%s money=%.2f", userId, plan.Id, tradeNo, plan.PriceAmount))

	c.JSON(http.StatusOK, gin.H{
		"message": "success",
		"data": gin.H{
			"payment_url": paymentUrl,
			"order_id":    tradeNo,
		},
	})
}

// SubscriptionAlipayDirectNotify 处理订阅异步通知
func SubscriptionAlipayDirectNotify(c *gin.Context) {
	if !isAlipayDirectWebhookEnabled() {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("支付宝 订阅 webhook 被拒绝 path=%q client_ip=%s", c.Request.RequestURI, c.ClientIP()))
		c.String(http.StatusOK, "fail")
		return
	}

	client, err := getAlipayDirectClient()
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("支付宝 订阅 webhook 客户端初始化失败 error=%q", err.Error()))
		c.String(http.StatusOK, "fail")
		return
	}

	if err := c.Request.ParseForm(); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("支付宝 订阅 webhook 表单解析失败 error=%q", err.Error()))
		c.String(http.StatusOK, "fail")
		return
	}

	notification, err := client.DecodeNotification(c.Request.Context(), c.Request.Form)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("支付宝 订阅 webhook 验签/解码失败 error=%q", err.Error()))
		c.String(http.StatusOK, "fail")
		return
	}

	if notification.TradeStatus != alipay.TradeStatusSuccess && notification.TradeStatus != alipay.TradeStatusFinished {
		alipay.AckNotification(c.Writer)
		return
	}

	tradeNo := notification.OutTradeNo
	LockOrder(tradeNo)
	defer UnlockOrder(tradeNo)

	payload, _ := common.Marshal(notification)
	if err := model.CompleteSubscriptionOrder(tradeNo, string(payload), model.PaymentProviderAlipayDirect, model.PaymentMethodAlipayDirect); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("支付宝 订阅 webhook 完成失败 trade_no=%s error=%q", tradeNo, err.Error()))
		c.String(http.StatusOK, "fail")
		return
	}

	logger.LogInfo(c.Request.Context(), fmt.Sprintf("支付宝 订阅 webhook 完成成功 trade_no=%s client_ip=%s", tradeNo, c.ClientIP()))
	alipay.AckNotification(c.Writer)
}

// SubscriptionAlipayDirectReturn 处理订阅同步回调（前端跳转）
func SubscriptionAlipayDirectReturn(c *gin.Context) {
	returnUrl := resolveAlipayDirectReturnUrl()
	c.Redirect(http.StatusFound, returnUrl)
}
