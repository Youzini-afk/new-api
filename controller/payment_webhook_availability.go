package controller

import (
	"strings"

	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// isPaymentComplianceConfirmed reports whether the operator has accepted the
// current payment-compliance terms. It is the foundation of every *creation*
// (topup / subscription purchase) gate; webhook processing gates MUST NOT
// depend on it so that already-pending orders can still be completed if the
// operator later un-confirms compliance.
func isPaymentComplianceConfirmed() bool {
	return operation_setting.IsPaymentComplianceConfirmed()
}

// ---------------------------------------------------------------------------
// Stripe
// ---------------------------------------------------------------------------

// isStripeCreateConfigured reports whether the wallet topup creation path has
// the materials it needs: an API secret with the documented sk_/rk_ prefix
// and a wallet StripePriceId to attach as the checkout line item.
func isStripeCreateConfigured() bool {
	apiSecret := strings.TrimSpace(setting.StripeApiSecret)
	if apiSecret == "" {
		return false
	}
	if !strings.HasPrefix(apiSecret, "sk_") && !strings.HasPrefix(apiSecret, "rk_") {
		return false
	}
	return strings.TrimSpace(setting.StripePriceId) != ""
}

// isStripeWebhookConfigured reports whether the Stripe webhook signature can
// be verified at all (webhook processing gate).
func isStripeWebhookConfigured() bool {
	return strings.TrimSpace(setting.StripeWebhookSecret) != ""
}

// isStripeTopUpEnabled is the wallet topup creation gate: compliance must be
// confirmed and both creation + webhook verification material must be present
// so a freshly-created order can actually be fulfilled later.
func isStripeTopUpEnabled() bool {
	if !isPaymentComplianceConfirmed() {
		return false
	}
	return isStripeCreateConfigured() && isStripeWebhookConfigured()
}

// isStripeWebhookEnabled is the webhook processing gate. It only checks
// signature verification material, so already-pending orders can still be
// completed even if compliance is later revoked, the wallet StripePriceId is
// unset, or the API secret has been rotated away.
func isStripeWebhookEnabled() bool {
	return isStripeWebhookConfigured()
}

// ---------------------------------------------------------------------------
// Creem
// ---------------------------------------------------------------------------

// isCreemWebhookConfigured reports whether a Creem webhook secret is set. This
// is the literal "secret present" predicate; the test-mode-aware gate is
// isCreemWebhookEnabled.
func isCreemWebhookConfigured() bool {
	return strings.TrimSpace(setting.CreemWebhookSecret) != ""
}

// isCreemWebhookEnabled is the webhook processing gate. In production a secret
// is mandatory; in test mode verifyCreemSignature already allows the webhook
// to be processed without one, so we mirror that semantics here.
func isCreemWebhookEnabled() bool {
	if setting.CreemTestMode {
		return true
	}
	return isCreemWebhookConfigured()
}

// isCreemTopUpEnabled is the wallet topup creation gate: compliance confirmed,
// API key present, wallet product list populated, and webhook verification
// material available for the configured mode.
func isCreemTopUpEnabled() bool {
	if !isPaymentComplianceConfirmed() {
		return false
	}
	products := strings.TrimSpace(setting.CreemProducts)
	if strings.TrimSpace(setting.CreemApiKey) == "" ||
		products == "" ||
		products == "[]" {
		return false
	}
	return isCreemWebhookEnabled()
}

// ---------------------------------------------------------------------------
// Waffo (legacy gateway)
// ---------------------------------------------------------------------------

// isWaffoWebhookConfigured reports whether the Waffo SDK can be initialised
// for the active mode (sandbox or production) and therefore whether the
// webhook signature can be verified. It checks the same triple the SDK build
// consumes (API key + private key + public cert).
func isWaffoWebhookConfigured() bool {
	if setting.WaffoSandbox {
		return strings.TrimSpace(setting.WaffoSandboxApiKey) != "" &&
			strings.TrimSpace(setting.WaffoSandboxPrivateKey) != "" &&
			strings.TrimSpace(setting.WaffoSandboxPublicCert) != ""
	}

	return strings.TrimSpace(setting.WaffoApiKey) != "" &&
		strings.TrimSpace(setting.WaffoPrivateKey) != "" &&
		strings.TrimSpace(setting.WaffoPublicCert) != ""
}

// isWaffoTopUpEnabled is the wallet topup creation gate: compliance confirmed,
// the operator has explicitly toggled WaffoEnabled, and the SDK signing
// materials are present so an order can be both created and later fulfilled.
func isWaffoTopUpEnabled() bool {
	if !isPaymentComplianceConfirmed() {
		return false
	}
	if !setting.WaffoEnabled {
		return false
	}
	return isWaffoWebhookConfigured()
}

// isWaffoWebhookEnabled is the webhook processing gate. It depends only on the
// SDK signing materials, so disabling WaffoEnabled or revoking compliance
// after the fact does not strand already-pending Waffo orders.
func isWaffoWebhookEnabled() bool {
	return isWaffoWebhookConfigured()
}

// ---------------------------------------------------------------------------
// Waffo Pancake
// ---------------------------------------------------------------------------

// isWaffoPancakeWebhookConfigured reports whether the local config carries the
// materials needed to verify a Pancake webhook. The Pancake SDK ships the test
// and production public signing keys inside the binary and selects the
// matching key from the payload's `mode` field (see
// service.VerifyConfiguredWaffoPancakeWebhook, which calls
// pancake.VerifyWebhookTyped(..., nil)), so verification does NOT depend on
// the operator's per-store WaffoPancakeMerchantID / WaffoPancakePrivateKey.
// The actual signature verifier rejects bad payloads on its own, so this
// predicate always returns true to keep already-pending orders fulfillable
// even after merchant credentials are cleared or rotated.
func isWaffoPancakeWebhookConfigured() bool {
	return true
}

// isWaffoPancakeTopUpEnabled is the wallet topup creation gate: compliance
// confirmed, per-store merchant credentials present (so the SDK can create a
// checkout session), and a wallet topup product id configured.
func isWaffoPancakeTopUpEnabled() bool {
	if !isPaymentComplianceConfirmed() {
		return false
	}
	if strings.TrimSpace(setting.WaffoPancakeMerchantID) == "" ||
		strings.TrimSpace(setting.WaffoPancakePrivateKey) == "" {
		return false
	}
	return strings.TrimSpace(setting.WaffoPancakeProductID) != ""
}

// isWaffoPancakeWebhookEnabled is the webhook processing gate. The Pancake SDK
// carries the public signing keys and selects them from the payload's mode
// field, so verification does not depend on the operator's per-store merchant
// / private credentials. Clearing/rotating the merchant credentials, removing
// the wallet product id, or revoking compliance therefore does not strand
// already-pending Pancake orders — the actual signature verifier rejects bad
// payloads on its own.
func isWaffoPancakeWebhookEnabled() bool {
	return isWaffoPancakeWebhookConfigured()
}

// ---------------------------------------------------------------------------
// Epay
// ---------------------------------------------------------------------------

// isEpayWebhookConfigured reports whether the Epay client can be constructed
// and signatures verified. These three fields are the verification material
// shared by both the wallet topup and the subscription notify/return flows.
func isEpayWebhookConfigured() bool {
	return strings.TrimSpace(operation_setting.PayAddress) != "" &&
		strings.TrimSpace(operation_setting.EpayId) != "" &&
		strings.TrimSpace(operation_setting.EpayKey) != ""
}

// isEpayTopUpEnabled is the wallet topup creation gate: compliance confirmed,
// verification material present, and at least one wallet pay method configured
// so the user can pick a payment instrument.
func isEpayTopUpEnabled() bool {
	if !isPaymentComplianceConfirmed() {
		return false
	}
	return isEpayWebhookConfigured() && len(operation_setting.PayMethods) > 0
}

// isEpayWebhookEnabled is the webhook processing gate. It depends only on the
// verification material, so compliance revocation, an empty PayMethods list,
// or a disabled UI does not strand already-pending Epay orders.
func isEpayWebhookEnabled() bool {
	return isEpayWebhookConfigured()
}
