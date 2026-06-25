package controller

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/shopspring/decimal"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/thanhpk/randstr"
)

const CreemSignatureHeader = "creem-signature"

var creemAdaptor = &CreemAdaptor{}

// 生成HMAC-SHA256签名
func generateCreemSignature(payload string, secret string) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(payload))
	return hex.EncodeToString(h.Sum(nil))
}

// 验证Creem webhook签名
func verifyCreemSignature(payload string, signature string, secret string) bool {
	if secret == "" {
		logger.LogWarn(context.Background(), fmt.Sprintf("Creem webhook secret 未配置 test_mode=%t signature=%q body=%q", setting.CreemTestMode, signature, payload))
		if setting.CreemTestMode {
			logger.LogInfo(context.Background(), fmt.Sprintf("Creem webhook 验签已跳过 reason=test_mode signature=%q body=%q", signature, payload))
			return true
		}
		return false
	}

	expectedSignature := generateCreemSignature(payload, secret)
	return hmac.Equal([]byte(signature), []byte(expectedSignature))
}

type CreemPayRequest struct {
	ProductId     string `json:"product_id"`
	PaymentMethod string `json:"payment_method"`
}

type CreemProduct struct {
	ProductId string  `json:"productId"`
	Name      string  `json:"name"`
	Price     float64 `json:"price"`
	Currency  string  `json:"currency"`
	Quota     int64   `json:"quota"`
}

type CreemAdaptor struct {
}

func (*CreemAdaptor) RequestPay(c *gin.Context, req *CreemPayRequest) {
	// Creation gate: refuse before product parse / order insert if compliance
	// is unconfirmed, the API key is missing, the wallet product list is
	// empty, or the webhook secret is missing in production. The webhook gate
	// (isCreemWebhookEnabled) is narrower so already-pending orders can
	// still be fulfilled after the fact.
	if !isCreemTopUpEnabled() {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "当前管理员未配置支付信息"})
		return
	}
	if req.PaymentMethod != model.PaymentMethodCreem {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "不支持的支付渠道"})
		return
	}

	if req.ProductId == "" {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "请选择产品"})
		return
	}
	req.ProductId = strings.TrimSpace(req.ProductId)

	// 解析产品列表
	var products []CreemProduct
	err := common.Unmarshal([]byte(setting.CreemProducts), &products)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Creem 产品配置解析失败 user_id=%d error=%q", c.GetInt("id"), err.Error()))
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "产品配置错误"})
		return
	}

	// 查找对应的产品，并在快照中修剪 product id / currency
	var selectedProduct *CreemProduct
	for i := range products {
		if strings.TrimSpace(products[i].ProductId) == req.ProductId {
			trimmed := products[i]
			trimmed.ProductId = strings.TrimSpace(trimmed.ProductId)
			trimmed.Currency = strings.TrimSpace(trimmed.Currency)
			selectedProduct = &trimmed
			break
		}
	}

	if selectedProduct == nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "产品不存在"})
		return
	}

	// 在插入待处理订单前验证所选产品快照。非正数的配额/价格、缺失的
	// 产品 ID 或币种会创建一个永远无法通过 webhook 校验的订单，因此
	// 必须在前面进行拦截，而不是让 genCreemLink 之后留下一个挂起的订单。
	if selectedProduct.ProductId == "" {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Creem 产品配置缺少 product_id user_id=%d", c.GetInt("id")))
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "产品配置错误"})
		return
	}
	if selectedProduct.Quota <= 0 {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Creem 产品配置 quota 非正数 product_id=%s quota=%d", selectedProduct.ProductId, selectedProduct.Quota))
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "产品配置错误"})
		return
	}
	if selectedProduct.Price <= 0 {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Creem 产品配置 price 非正数 product_id=%s price=%.2f", selectedProduct.ProductId, selectedProduct.Price))
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "产品配置错误"})
		return
	}
	if selectedProduct.Currency == "" {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Creem 产品配置缺少 currency product_id=%s", selectedProduct.ProductId))
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "产品配置错误"})
		return
	}

	id := c.GetInt("id")
	user, _ := model.GetUserById(id, false)

	// 生成唯一的订单引用ID
	reference := fmt.Sprintf("creem-api-ref-%d-%d-%s", user.Id, time.Now().UnixMilli(), randstr.String(4))
	referenceId := "ref_" + common.Sha1([]byte(reference))

	// 先创建订单记录，使用产品配置的金额和充值额度
	topUp := &model.TopUp{
		UserId:          id,
		Amount:          selectedProduct.Quota, // 充值额度
		Money:           selectedProduct.Price, // 支付金额
		TradeNo:         referenceId,
		PaymentMethod:   model.PaymentMethodCreem,
		PaymentProvider: model.PaymentProviderCreem,
		CreateTime:      time.Now().Unix(),
		Status:          common.TopUpStatusPending,
	}
	err = topUp.Insert()
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Creem 创建充值订单失败 user_id=%d trade_no=%s product_id=%s error=%q", id, referenceId, selectedProduct.ProductId, err.Error()))
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "创建订单失败"})
		return
	}

	// 创建支付链接，传入用户邮箱
	checkoutUrl, err := genCreemLink(c.Request.Context(), referenceId, selectedProduct, user.Email, user.Username)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Creem 创建支付链接失败 user_id=%d trade_no=%s product_id=%s error=%q", id, referenceId, selectedProduct.ProductId, err.Error()))
		// genCreemLink failed after the pending TopUp was inserted. Mark it
		// failed so a dangling pending order that can never be redeemed is
		// not stranded. UpdatePendingTopUpStatus only transitions pending ->
		// target and checks the payment provider, so it is safe even if the
		// row was concurrently completed/expired.
		if updateErr := model.UpdatePendingTopUpStatus(referenceId, model.PaymentProviderCreem, common.TopUpStatusFailed); updateErr != nil &&
			!errors.Is(updateErr, model.ErrTopUpNotFound) && !errors.Is(updateErr, model.ErrTopUpStatusInvalid) {
			logger.LogError(c.Request.Context(), fmt.Sprintf("Creem 创建支付链接失败后标记订单失败失败 user_id=%d trade_no=%s error=%q", id, referenceId, updateErr.Error()))
		}
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "拉起支付失败"})
		return
	}

	logger.LogInfo(c.Request.Context(), fmt.Sprintf("Creem 充值订单创建成功 user_id=%d trade_no=%s product_id=%s product_name=%q quota=%d money=%.2f", id, referenceId, selectedProduct.ProductId, selectedProduct.Name, selectedProduct.Quota, selectedProduct.Price))

	c.JSON(http.StatusOK, gin.H{
		"message": "success",
		"data": gin.H{
			"checkout_url": checkoutUrl,
			"order_id":     referenceId,
		},
	})
}

func RequestCreemPay(c *gin.Context) {
	var req CreemPayRequest

	// 读取body内容用于打印，同时保留原始数据供后续使用
	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Creem 支付请求读取失败 error=%q", err.Error()))
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "read query error"})
		return
	}

	logger.LogInfo(c.Request.Context(), fmt.Sprintf("Creem 支付请求已收到 user_id=%d body=%q", c.GetInt("id"), string(bodyBytes)))

	// 重新设置body供后续的ShouldBindJSON使用
	c.Request.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	err = c.ShouldBindJSON(&req)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "参数错误"})
		return
	}
	creemAdaptor.RequestPay(c, &req)
}

// 新的Creem Webhook结构体，匹配实际的webhook数据格式
type CreemWebhookEvent struct {
	Id        string `json:"id"`
	EventType string `json:"eventType"`
	CreatedAt int64  `json:"created_at"`
	Object    struct {
		Id        string `json:"id"`
		Object    string `json:"object"`
		RequestId string `json:"request_id"`
		Order     struct {
			Object      string `json:"object"`
			Id          string `json:"id"`
			Customer    string `json:"customer"`
			Product     string `json:"product"`
			Amount      int    `json:"amount"`
			Currency    string `json:"currency"`
			SubTotal    int    `json:"sub_total"`
			TaxAmount   int    `json:"tax_amount"`
			AmountDue   int    `json:"amount_due"`
			AmountPaid  int    `json:"amount_paid"`
			Status      string `json:"status"`
			Type        string `json:"type"`
			Transaction string `json:"transaction"`
			CreatedAt   string `json:"created_at"`
			UpdatedAt   string `json:"updated_at"`
			Mode        string `json:"mode"`
		} `json:"order"`
		Product struct {
			Id                string  `json:"id"`
			Object            string  `json:"object"`
			Name              string  `json:"name"`
			Description       string  `json:"description"`
			Price             int     `json:"price"`
			Currency          string  `json:"currency"`
			BillingType       string  `json:"billing_type"`
			BillingPeriod     string  `json:"billing_period"`
			Status            string  `json:"status"`
			TaxMode           string  `json:"tax_mode"`
			TaxCategory       string  `json:"tax_category"`
			DefaultSuccessUrl *string `json:"default_success_url"`
			CreatedAt         string  `json:"created_at"`
			UpdatedAt         string  `json:"updated_at"`
			Mode              string  `json:"mode"`
		} `json:"product"`
		Units    int `json:"units"`
		Customer struct {
			Id        string `json:"id"`
			Object    string `json:"object"`
			Email     string `json:"email"`
			Name      string `json:"name"`
			Country   string `json:"country"`
			CreatedAt string `json:"created_at"`
			UpdatedAt string `json:"updated_at"`
			Mode      string `json:"mode"`
		} `json:"customer"`
		Status   string            `json:"status"`
		Metadata map[string]string `json:"metadata"`
		Mode     string            `json:"mode"`
	} `json:"object"`
}

func CreemWebhook(c *gin.Context) {
	if !isCreemWebhookEnabled() {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("Creem webhook 被拒绝 reason=webhook_disabled path=%q client_ip=%s", c.Request.RequestURI, c.ClientIP()))
		c.AbortWithStatus(http.StatusForbidden)
		return
	}

	// 读取body内容用于打印，同时保留原始数据供后续使用
	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Creem webhook 读取请求体失败 path=%q client_ip=%s error=%q", c.Request.RequestURI, c.ClientIP(), err.Error()))
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	// 获取签名头
	signature := c.GetHeader(CreemSignatureHeader)
	logger.LogInfo(c.Request.Context(), fmt.Sprintf("Creem webhook 收到请求 path=%q client_ip=%s signature=%q body=%q", c.Request.RequestURI, c.ClientIP(), signature, string(bodyBytes)))
	if signature == "" {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("Creem webhook 缺少签名 path=%q client_ip=%s body=%q", c.Request.RequestURI, c.ClientIP(), string(bodyBytes)))
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}

	// 验证签名
	if !verifyCreemSignature(string(bodyBytes), signature, setting.CreemWebhookSecret) {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("Creem webhook 验签失败 path=%q client_ip=%s signature=%q body=%q", c.Request.RequestURI, c.ClientIP(), signature, string(bodyBytes)))
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}

	logger.LogInfo(c.Request.Context(), fmt.Sprintf("Creem webhook 验签成功 path=%q client_ip=%s", c.Request.RequestURI, c.ClientIP()))

	// 重新设置body供后续的ShouldBindJSON使用
	c.Request.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	// 解析新格式的webhook数据
	var webhookEvent CreemWebhookEvent
	if err := c.ShouldBindJSON(&webhookEvent); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Creem webhook 解析失败 path=%q client_ip=%s error=%q body=%q", c.Request.RequestURI, c.ClientIP(), err.Error(), string(bodyBytes)))
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	logger.LogInfo(c.Request.Context(), fmt.Sprintf("Creem webhook 解析成功 event_type=%s event_id=%s request_id=%s order_id=%s order_status=%s", webhookEvent.EventType, webhookEvent.Id, webhookEvent.Object.RequestId, webhookEvent.Object.Order.Id, webhookEvent.Object.Order.Status))

	// 根据事件类型处理不同的webhook
	switch webhookEvent.EventType {
	case "checkout.completed":
		handleCheckoutCompleted(c, &webhookEvent)
	default:
		logger.LogInfo(c.Request.Context(), fmt.Sprintf("Creem webhook 忽略事件 event_type=%s event_id=%s", webhookEvent.EventType, webhookEvent.Id))
		c.Status(http.StatusOK)
	}
}

// 处理支付完成事件
func handleCheckoutCompleted(c *gin.Context, event *CreemWebhookEvent) {
	// 获取引用ID（这是我们创建订单时传递的request_id）
	referenceId := event.Object.RequestId
	if referenceId == "" {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("Creem webhook 缺少 request_id event_id=%s order_id=%s", event.Id, event.Object.Order.Id))
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	// Payment status gate: a signed checkout.completed must additionally
	// carry checkout status "completed" AND order status "paid" before any
	// fulfillment runs. CompleteSubscriptionOrder does not understand the
	// Creem payload's amount/status semantics, so without this gate a
	// verified-but-not-paid event (e.g. an asynchronous checkout.completed
	// arriving while the order is still "pending"/"processing") would
	// directly complete a SubscriptionOrder and provision a UserSubscription
	// for an unpaid order. Leave the local pending order untouched and ack
	// 200 (mirrors the verified-mismatch behavior of the wallet branch /
	// Stripe). The wallet fallback's own validateCreemCallbackMatches gate
	// remains in place as a defense-in-depth re-check.
	if event.Object.Status != "completed" || event.Object.Order.Status != "paid" {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("Creem webhook 拒绝非已支付事件 trade_no=%s event_id=%s order_id=%s checkout_status=%s order_status=%s", referenceId, event.Id, event.Object.Order.Id, event.Object.Status, event.Object.Order.Status))
		c.Status(http.StatusOK)
		return
	}

	// Try complete subscription order first
	LockOrder(referenceId)
	defer UnlockOrder(referenceId)
	if err := model.CompleteSubscriptionOrder(referenceId, common.GetJsonString(event), model.PaymentProviderCreem, ""); err == nil {
		logger.LogInfo(c.Request.Context(), fmt.Sprintf("Creem 订阅订单处理成功 trade_no=%s creem_order_id=%s", referenceId, event.Object.Order.Id))
		c.Status(http.StatusOK)
		return
	} else if err != nil && !errors.Is(err, model.ErrSubscriptionOrderNotFound) {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Creem 订阅订单处理失败 trade_no=%s creem_order_id=%s error=%q", referenceId, event.Object.Order.Id, err.Error()))
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}

	// Subscription not found -> wallet topup branch. Pass the verified webhook
	// payload to CompleteCreemTopUp, which locks the row, validates the stored
	// snapshot (provider/method/checkout status/order status/order type/product
	// id/amount/currency/customer/metadata), and CAS-transitions pending ->
	// success. A mismatch leaves the order pending for reconciliation; the
	// webhook still acks 200 after a verified event (current Stripe behavior).
	// The controller no longer does its own TopUp lookup/status/pending
	// decision and never calls the unsafe legacy RechargeCreem.
	payload := model.CreemTopUpWebhookPayload{
		TradeNo:         referenceId,
		CheckoutStatus:  event.Object.Status,
		OrderStatus:     event.Object.Order.Status,
		OrderType:       event.Object.Order.Type,
		OrderProduct:    event.Object.Order.Product,
		OrderAmount:     event.Object.Order.Amount,
		OrderCurrency:   event.Object.Order.Currency,
		ProductId:       event.Object.Product.Id,
		ProductPrice:    event.Object.Product.Price,
		ProductCurrency: event.Object.Product.Currency,
		CustomerId:      event.Object.Customer.Id,
		OrderCustomer:   event.Object.Order.Customer,
		CustomerEmail:   event.Object.Customer.Email,
		CustomerName:    event.Object.Customer.Name,
		Metadata:        event.Object.Metadata,
	}
	completion, err := model.CompleteCreemTopUp(payload, c.ClientIP())
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Creem 充值处理失败 trade_no=%s creem_order_id=%s client_ip=%s checkout_status=%s order_status=%s order_type=%s order_amount=%d order_currency=%s product_id=%s product_price=%d product_currency=%s customer_id=%s error=%q", referenceId, event.Object.Order.Id, c.ClientIP(), event.Object.Status, event.Object.Order.Status, event.Object.Order.Type, event.Object.Order.Amount, event.Object.Order.Currency, event.Object.Product.Id, event.Object.Product.Price, event.Object.Product.Currency, event.Object.Customer.Id, err.Error()))
		c.Status(http.StatusOK)
		return
	}

	if completion.Completed {
		logger.LogInfo(c.Request.Context(), fmt.Sprintf("Creem 充值成功 trade_no=%s creem_order_id=%s user_id=%d quota=%d money=%.2f client_ip=%s", referenceId, event.Object.Order.Id, completion.UserId, completion.QuotaToAdd, completion.PayMoney, c.ClientIP()))
	} else {
		logger.LogInfo(c.Request.Context(), fmt.Sprintf("Creem 重复到账幂等忽略 trade_no=%s creem_order_id=%s user_id=%d client_ip=%s", referenceId, event.Object.Order.Id, completion.UserId, c.ClientIP()))
	}
	c.Status(http.StatusOK)
}

type CreemCheckoutRequest struct {
	ProductId string `json:"product_id"`
	RequestId string `json:"request_id"`
	Customer  struct {
		Email string `json:"email"`
	} `json:"customer"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type CreemCheckoutResponse struct {
	CheckoutUrl string `json:"checkout_url"`
	Id          string `json:"id"`
}

// genCreemLink builds a Creem checkout link. It is a function variable so the
// controller-level tests can deterministically simulate a checkout-link
// creation failure (the live function only fails after a network round-trip,
// which is non-deterministic in CI) and prove the pending local order is
// marked failed/expired instead of being stranded.
var genCreemLink = func(ctx context.Context, referenceId string, product *CreemProduct, email string, username string) (string, error) {
	if setting.CreemApiKey == "" {
		return "", fmt.Errorf("未配置Creem API密钥")
	}

	// 根据测试模式选择 API 端点
	apiUrl := "https://api.creem.io/v1/checkouts"
	if setting.CreemTestMode {
		apiUrl = "https://test-api.creem.io/v1/checkouts"
		logger.LogInfo(ctx, fmt.Sprintf("Creem 使用测试环境 api_url=%s", apiUrl))
	}

	// 构建请求数据，确保包含用户邮箱。元数据快照包含 product_id /
	// currency / price_minor，以便 webhook 可以交叉检查存储的 TopUp 快照
	// 是否与已验证的 payload 相匹配。
	requestData := CreemCheckoutRequest{
		ProductId: product.ProductId,
		RequestId: referenceId, // 这个作为订单ID传递给Creem
		Customer: struct {
			Email string `json:"email"`
		}{
			Email: email, // 用户邮箱会在支付页面预填充
		},
		Metadata: creemCheckoutMetadata(referenceId, product, username),
	}

	// 序列化请求数据
	jsonData, err := common.Marshal(requestData)
	if err != nil {
		return "", fmt.Errorf("序列化请求数据失败: %v", err)
	}

	// 创建 HTTP 请求
	req, err := http.NewRequest("POST", apiUrl, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("创建HTTP请求失败: %v", err)
	}

	// 设置请求头
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", setting.CreemApiKey)

	logger.LogInfo(ctx, fmt.Sprintf("Creem 支付请求已发送 api_url=%s product_id=%s email=%q trade_no=%s", apiUrl, product.ProductId, email, referenceId))

	// 发送请求
	client := &http.Client{
		Timeout: 30 * time.Second,
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("发送HTTP请求失败: %v", err)
	}
	defer resp.Body.Close()

	// 读取响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("读取响应失败: %v", err)
	}

	logger.LogInfo(ctx, fmt.Sprintf("Creem API 响应已收到 trade_no=%s status_code=%d body=%q", referenceId, resp.StatusCode, string(body)))

	// 检查响应状态
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("Creem API http status %d ", resp.StatusCode)
	}
	// 解析响应
	var checkoutResp CreemCheckoutResponse
	err = common.Unmarshal(body, &checkoutResp)
	if err != nil {
		return "", fmt.Errorf("解析响应失败: %v", err)
	}

	if checkoutResp.CheckoutUrl == "" {
		return "", fmt.Errorf("Creem API resp no checkout url ")
	}

	logger.LogInfo(ctx, fmt.Sprintf("Creem 支付链接创建成功 trade_no=%s response_id=%s checkout_url=%q", referenceId, checkoutResp.Id, checkoutResp.CheckoutUrl))
	return checkoutResp.CheckoutUrl, nil
}

// creemCheckoutMetadata builds the metadata map embedded in the Creem checkout
// request. Each field is cross-checked by model.CompleteCreemTopUp on webhook
// completion (when present), so the values must mirror the stored TopUp
// snapshot captured at creation: reference_id == TradeNo, product_id ==
// product.ProductId, quota == product.Quota (== TopUp.Amount), price_minor ==
// round(product.Price * 100) (== TopUp.Money * 100), currency ==
// product.Currency. Extracted so the controller-level test can verify the
// metadata contents deterministically without a network round-trip.
func creemCheckoutMetadata(referenceId string, product *CreemProduct, username string) map[string]string {
	priceMinor := decimal.NewFromFloat(product.Price).Mul(decimal.NewFromInt(100)).Round(0).String()
	return map[string]string{
		"username":     username,
		"reference_id": referenceId,
		"product_name": product.Name,
		"quota":        fmt.Sprintf("%d", product.Quota),
		"product_id":   product.ProductId,
		"currency":     product.Currency,
		"price_minor":  priceMinor,
	}
}
