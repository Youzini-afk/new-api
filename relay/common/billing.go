package common

import "github.com/gin-gonic/gin"

// ShortMsgExtraBillingPreflight 是 short-message extra billing enforce
// 模式在预扣费阶段冻结的命中规则快照。它在 controller/relay.go 调用
// service.PreConsumeBilling 之前由 service.PrepareShortMsgExtraBillingPreConsume
// 设置到 RelayInfo 上，Post 阶段的 enforce 决策必须基于此快照，避免中途
// 配置变更引入账务竞态（响应成功但账务失败 / 透支）。
//
// 该结构体刻意只保留 Post 阶段 enforce 决策所需的纯标量字段，不引用
// operation_setting.ShortMsgExtraBillingRule，避免 relay/common 反向依赖
// setting/operation_setting。
type ShortMsgExtraBillingPreflight struct {
	// Mode 始终为 "enforce"（仅在 enforce 模式预检命中时构造）。
	Mode string
	// TextMode 是预检时刻的稳定文本模式标签 (chat_completions / responses /
	// claude / gemini / ...)。Post 阶段会重新计算并要求一致。
	TextMode string
	// RuleID / Model / Trigger / Threshold / FeeQuota / WaiveWhenCompletionTokensZero
	// 是命中规则的冻结拷贝。
	RuleID                        string
	Model                         string
	Trigger                       string
	Threshold                     int
	FeeQuota                      int
	WaiveWhenCompletionTokensZero bool
	// PotentialExtraQuota 是预检阶段决定预留的额外额度（= FeeQuota）。
	// 该值会被加进 priceData.QuotaToPreConsume 并由钱包原子预扣。Post 阶段
	// enforce 必须满足 frozen.FeeQuota <= frozen.PotentialExtraQuota 才允许实扣。
	PotentialExtraQuota int
	// Reason 是预检阶段的稳定机读结果标签 (matched / mode_disabled /
	// streaming / non_text_mode / tiered_expr / free_model / no_rule_matched /
	// threshold_not_met)。
	Reason string
}

// BillingSettler 抽象计费会话的生命周期操作。
// 由 service.BillingSession 实现，存储在 RelayInfo 上以避免循环引用。
type BillingSettler interface {
	// Settle 根据实际消耗额度进行结算，计算 delta = actualQuota - preConsumedQuota，
	// 同时调整资金来源（钱包/订阅）和令牌额度。
	Settle(actualQuota int) error

	// Refund 退还所有预扣费额度（资金来源 + 令牌），幂等安全。
	// 通过 gopool 异步执行。如果已经结算或退款则不做任何操作。
	Refund(c *gin.Context)

	// NeedsRefund 返回会话是否存在需要退还的预扣状态（未结算且未退款）。
	NeedsRefund() bool

	// GetPreConsumedQuota 返回实际预扣的额度值（信任用户可能为 0）。
	GetPreConsumedQuota() int

	// Reserve 将预扣额度补到目标值；若目标值不高于当前预扣额度则不做任何事。
	Reserve(targetQuota int) error
}
