package controller

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"github.com/smartwalle/alipay/v3"
	"github.com/thanhpk/randstr"
)

type AlipayDirectPayRequest struct {
	Amount int64 `json:"amount"`
}

func getAlipayDirectClient() (*alipay.Client, error) {
	appId := strings.TrimSpace(setting.AlipayDirectAppId)
	privateKey := strings.TrimSpace(setting.AlipayDirectPrivateKey)
	publicKey := strings.TrimSpace(setting.AlipayDirectPublicKey)

	if appId == "" || privateKey == "" || publicKey == "" {
		return nil, fmt.Errorf("支付宝配置不完整")
	}

	client, err := alipay.New(appId, privateKey, !setting.AlipayDirectSandbox)
	if err != nil {
		return nil, fmt.Errorf("支付宝客户端初始化失败: %w", err)
	}

	if err := client.LoadAliPayPublicKey(publicKey); err != nil {
		return nil, fmt.Errorf("加载支付宝公钥失败: %w", err)
	}

	return client, nil
}

func RequestAlipayDirectAmount(c *gin.Context) {
	var req AlipayDirectPayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "参数错误"})
		return
	}

	if req.Amount < int64(setting.AlipayDirectMinTopUp) {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": fmt.Sprintf("充值数量不能小于 %d", setting.AlipayDirectMinTopUp)})
		return
	}

	id := c.GetInt("id")
	group, err := model.GetUserGroup(id, true)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "获取用户分组失败"})
		return
	}

	payMoney := getAlipayDirectPayMoney(req.Amount, group)
	if payMoney <= 0.01 {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "充值金额过低"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "success", "data": fmt.Sprintf("%.2f", payMoney)})
}

func getAlipayDirectPayMoney(amount int64, group string) float64 {
	dAmount := decimal.NewFromInt(amount)
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		dAmount = dAmount.Div(decimal.NewFromFloat(common.QuotaPerUnit))
	}

	topupGroupRatio := common.GetTopupGroupRatio(group)
	if topupGroupRatio == 0 {
		topupGroupRatio = 1
	}

	dPrice := decimal.NewFromFloat(operation_setting.Price)
	dTopupGroupRatio := decimal.NewFromFloat(topupGroupRatio)

	discount := 1.0
	if ds, ok := operation_setting.GetPaymentSetting().AmountDiscount[int(amount)]; ok && ds > 0 {
		discount = ds
	}
	dDiscount := decimal.NewFromFloat(discount)

	payMoney := dAmount.Mul(dPrice).Mul(dTopupGroupRatio).Mul(dDiscount)
	return payMoney.InexactFloat64()
}

func normalizeAlipayDirectTopUpAmount(amount int64) int64 {
	if operation_setting.GetQuotaDisplayType() != operation_setting.QuotaDisplayTypeTokens {
		return amount
	}
	normalized := decimal.NewFromInt(amount).Div(decimal.NewFromFloat(common.QuotaPerUnit)).IntPart()
	if normalized < 1 {
		return 1
	}
	return normalized
}

func resolveAlipayDirectNotifyUrl() string {
	if u := strings.TrimSpace(setting.AlipayDirectNotifyUrl); u != "" {
		return u
	}
	callbackAddress := service.GetCallbackAddress()
	return callbackAddress + "/api/alipay-direct/webhook"
}

func resolveAlipayDirectReturnUrl() string {
	if u := strings.TrimSpace(setting.AlipayDirectReturnUrl); u != "" {
		return u
	}
	return paymentReturnPath("/console/log")
}

func RequestAlipayDirectPay(c *gin.Context) {
	if !requireTosAcceptance(c) {
		return
	}

	if !isAlipayDirectTopUpEnabled() {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "支付宝支付未启用"})
		return
	}

	var req AlipayDirectPayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "参数错误"})
		return
	}
	if req.Amount < int64(setting.AlipayDirectMinTopUp) {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": fmt.Sprintf("充值数量不能小于 %d", setting.AlipayDirectMinTopUp)})
		return
	}

	id := c.GetInt("id")
	group, err := model.GetUserGroup(id, true)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "获取用户分组失败"})
		return
	}

	payMoney := getAlipayDirectPayMoney(req.Amount, group)
	if payMoney < 0.01 {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "充值金额过低"})
		return
	}

	tradeNo := fmt.Sprintf("ALIPAY_DIRECT-%d-%d-%s", id, time.Now().UnixMilli(), randstr.String(6))
	topUp := &model.TopUp{
		UserId:          id,
		Amount:          normalizeAlipayDirectTopUpAmount(req.Amount),
		Money:           payMoney,
		TradeNo:         tradeNo,
		PaymentMethod:   model.PaymentMethodAlipayDirect,
		PaymentProvider: model.PaymentProviderAlipayDirect,
		CreateTime:      time.Now().Unix(),
		Status:          common.TopUpStatusPending,
	}
	if err := topUp.Insert(); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("支付宝 创建充值订单失败 user_id=%d trade_no=%s amount=%d error=%q", id, tradeNo, req.Amount, err.Error()))
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "创建订单失败"})
		return
	}

	client, err := getAlipayDirectClient()
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("支付宝 客户端初始化失败 user_id=%d trade_no=%s error=%q", id, tradeNo, err.Error()))
		topUp.Status = common.TopUpStatusFailed
		_ = topUp.Update()
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "支付宝配置错误"})
		return
	}

	var p = alipay.TradePagePay{}
	p.Subject = fmt.Sprintf("TUC%d", req.Amount)
	p.OutTradeNo = tradeNo
	p.TotalAmount = fmt.Sprintf("%.2f", payMoney)
	p.ProductCode = "FAST_INSTANT_TRADE_PAY"
	p.NotifyURL = resolveAlipayDirectNotifyUrl()
	p.ReturnURL = resolveAlipayDirectReturnUrl()

	payUrl, err := client.TradePagePay(p)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("支付宝 TradePagePay 失败 user_id=%d trade_no=%s error=%q", id, tradeNo, err.Error()))
		topUp.Status = common.TopUpStatusFailed
		_ = topUp.Update()
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "拉起支付失败"})
		return
	}

	paymentUrl := payUrl.String()
	logger.LogInfo(c.Request.Context(), fmt.Sprintf("支付宝 充值订单创建成功 user_id=%d trade_no=%s amount=%d money=%.2f", id, tradeNo, req.Amount, payMoney))

	c.JSON(http.StatusOK, gin.H{
		"message": "success",
		"data": gin.H{
			"payment_url": paymentUrl,
			"order_id":    tradeNo,
		},
	})
}

func AlipayDirectWebhook(c *gin.Context) {
	if !isAlipayDirectWebhookEnabled() {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("支付宝 webhook 被拒绝 reason=webhook_disabled path=%q client_ip=%s", c.Request.RequestURI, c.ClientIP()))
		c.String(http.StatusOK, "fail")
		return
	}

	client, err := getAlipayDirectClient()
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("支付宝 webhook 客户端初始化失败 path=%q client_ip=%s error=%q", c.Request.RequestURI, c.ClientIP(), err.Error()))
		c.String(http.StatusOK, "fail")
		return
	}

	if err := c.Request.ParseForm(); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("支付宝 webhook 表单解析失败 path=%q client_ip=%s error=%q", c.Request.RequestURI, c.ClientIP(), err.Error()))
		c.String(http.StatusOK, "fail")
		return
	}

	// DecodeNotification 内部会进行验签
	notification, err := client.DecodeNotification(c.Request.Context(), c.Request.Form)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("支付宝 webhook 验签/解码失败 path=%q client_ip=%s error=%q", c.Request.RequestURI, c.ClientIP(), err.Error()))
		c.String(http.StatusOK, "fail")
		return
	}

	logger.LogInfo(c.Request.Context(), fmt.Sprintf("支付宝 webhook 收到通知 trade_no=%s trade_status=%s out_trade_no=%s client_ip=%s",
		notification.TradeNo, notification.TradeStatus, notification.OutTradeNo, c.ClientIP()))

	if notification.TradeStatus != alipay.TradeStatusSuccess && notification.TradeStatus != alipay.TradeStatusFinished {
		logger.LogInfo(c.Request.Context(), fmt.Sprintf("支付宝 webhook 忽略非成功状态 trade_no=%s trade_status=%s client_ip=%s", notification.OutTradeNo, notification.TradeStatus, c.ClientIP()))
		alipay.AckNotification(c.Writer)
		return
	}

	tradeNo := notification.OutTradeNo

	if strings.HasPrefix(tradeNo, "ALIPAY_DIRECT_SUB-") {
		LockOrder(tradeNo)
		defer UnlockOrder(tradeNo)

		payload, _ := common.Marshal(notification)
		if err := model.CompleteSubscriptionOrder(tradeNo, string(payload), model.PaymentProviderAlipayDirect, model.PaymentMethodAlipayDirect); err != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("支付宝 webhook 订阅完成失败 trade_no=%s client_ip=%s error=%q", tradeNo, c.ClientIP(), err.Error()))
			c.String(http.StatusOK, "fail")
			return
		}
		logger.LogInfo(c.Request.Context(), fmt.Sprintf("支付宝 webhook 订阅完成成功 trade_no=%s client_ip=%s", tradeNo, c.ClientIP()))
	} else {
		LockOrder(tradeNo)
		defer UnlockOrder(tradeNo)

		if err := model.RechargeAlipayDirect(tradeNo, c.ClientIP()); err != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("支付宝 webhook 充值完成失败 trade_no=%s client_ip=%s error=%q", tradeNo, c.ClientIP(), err.Error()))
			c.String(http.StatusOK, "fail")
			return
		}
		logger.LogInfo(c.Request.Context(), fmt.Sprintf("支付宝 webhook 充值完成成功 trade_no=%s client_ip=%s", tradeNo, c.ClientIP()))
	}

	alipay.AckNotification(c.Writer)
}
