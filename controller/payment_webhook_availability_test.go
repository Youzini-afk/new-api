package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/require"
)

func confirmPaymentComplianceForTest(t *testing.T) {
	t.Helper()
	paymentSetting := operation_setting.GetPaymentSetting()
	originalConfirmed := paymentSetting.ComplianceConfirmed
	originalTermsVersion := paymentSetting.ComplianceTermsVersion
	t.Cleanup(func() {
		paymentSetting.ComplianceConfirmed = originalConfirmed
		paymentSetting.ComplianceTermsVersion = originalTermsVersion
	})
	paymentSetting.ComplianceConfirmed = true
	paymentSetting.ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
}

// revokePaymentComplianceForTest flips the compliance flags off for the
// duration of the test and restores them on cleanup. Used to prove the
// webhook gates stay true when compliance is revoked.
func revokePaymentComplianceForTest(t *testing.T) {
	t.Helper()
	paymentSetting := operation_setting.GetPaymentSetting()
	originalConfirmed := paymentSetting.ComplianceConfirmed
	originalTermsVersion := paymentSetting.ComplianceTermsVersion
	t.Cleanup(func() {
		paymentSetting.ComplianceConfirmed = originalConfirmed
		paymentSetting.ComplianceTermsVersion = originalTermsVersion
	})
	paymentSetting.ComplianceConfirmed = false
	paymentSetting.ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
}

// TestPaymentGateSplit_Stripe verifies the Phase 8B split for Stripe:
// the webhook gate only depends on StripeWebhookSecret, while the topup
// creation gate additionally requires compliance, a valid API secret, and
// the wallet StripePriceId.
func TestPaymentGateSplit_Stripe(t *testing.T) {
	originalAPISecret := setting.StripeApiSecret
	originalWebhookSecret := setting.StripeWebhookSecret
	originalPriceID := setting.StripePriceId
	t.Cleanup(func() {
		setting.StripeApiSecret = originalAPISecret
		setting.StripeWebhookSecret = originalWebhookSecret
		setting.StripePriceId = originalPriceID
	})

	// Compliance revoked + webhook secret set + no wallet StripePriceId.
	// Webhook must stay true (an already-pending order can still be fulfilled),
	// topup must be false (no new orders can be created).
	revokePaymentComplianceForTest(t)
	setting.StripeWebhookSecret = "whsec_test"
	setting.StripeApiSecret = "sk_test_123"
	setting.StripePriceId = ""

	require.True(t, isStripeWebhookEnabled(), "webhook gate must not depend on compliance or StripePriceId")
	require.False(t, isStripeTopUpEnabled(), "topup gate must require compliance + wallet StripePriceId")

	// Compliance confirmed but wallet StripePriceId missing: webhook still
	// true, topup still false.
	confirmPaymentComplianceForTest(t)
	require.True(t, isStripeWebhookEnabled())
	require.False(t, isStripeTopUpEnabled(), "topup gate must require wallet StripePriceId")

	// Filling the wallet StripePriceId flips topup to true.
	setting.StripePriceId = "price_123"
	require.True(t, isStripeTopUpEnabled())

	// Removing the webhook secret disables both gates.
	setting.StripeWebhookSecret = ""
	require.False(t, isStripeWebhookEnabled())
	require.False(t, isStripeTopUpEnabled())

	// API secret without sk_/rk_ prefix is rejected by the topup gate even
	// when everything else is in place; the webhook gate is unaffected.
	setting.StripeWebhookSecret = "whsec_test"
	setting.StripeApiSecret = "not_a_stripe_key"
	require.True(t, isStripeWebhookEnabled())
	require.False(t, isStripeTopUpEnabled(), "topup gate must reject malformed Stripe API secret")
}

// TestPaymentGateSplit_Creem verifies the Phase 8B split for Creem:
// the webhook gate requires a secret in production (but not in test mode),
// while the topup creation gate additionally requires compliance, an API key,
// and a non-empty wallet product list.
func TestPaymentGateSplit_Creem(t *testing.T) {
	originalAPIKey := setting.CreemApiKey
	originalProducts := setting.CreemProducts
	originalWebhookSecret := setting.CreemWebhookSecret
	originalTestMode := setting.CreemTestMode
	t.Cleanup(func() {
		setting.CreemApiKey = originalAPIKey
		setting.CreemProducts = originalProducts
		setting.CreemWebhookSecret = originalWebhookSecret
		setting.CreemTestMode = originalTestMode
	})

	// Compliance revoked + products empty + webhook secret set (production).
	// Webhook must stay true, topup must be false on both axes.
	revokePaymentComplianceForTest(t)
	setting.CreemTestMode = false
	setting.CreemWebhookSecret = "creem_secret"
	setting.CreemApiKey = "creem_api_key"
	setting.CreemProducts = "[]"

	require.True(t, isCreemWebhookEnabled(), "webhook gate must not depend on compliance or wallet products")
	require.False(t, isCreemTopUpEnabled(), "topup gate must require compliance + non-empty products")

	// Compliance confirmed but products still empty: webhook true, topup false.
	confirmPaymentComplianceForTest(t)
	require.True(t, isCreemWebhookEnabled())
	require.False(t, isCreemTopUpEnabled())

	// Products populated: topup now true.
	setting.CreemProducts = `[{"productId":"prod_123"}]`
	require.True(t, isCreemTopUpEnabled())

	// Removing the API key disables only the topup gate.
	setting.CreemApiKey = ""
	require.True(t, isCreemWebhookEnabled())
	require.False(t, isCreemTopUpEnabled(), "topup gate must require CreemApiKey")
	setting.CreemApiKey = "creem_api_key"

	// Production mode without a secret disables both gates.
	setting.CreemWebhookSecret = ""
	require.False(t, isCreemWebhookEnabled(), "production webhook gate must require a secret")
	require.False(t, isCreemTopUpEnabled())

	// Test mode allows the webhook gate without a secret (matches
	// verifyCreemSignature's existing test-mode behavior); the topup gate
	// still requires the secret-bearing product config, but with the webhook
	// gate now true we focus on the webhook assertion.
	setting.CreemTestMode = true
	require.True(t, isCreemWebhookEnabled(), "test-mode webhook gate may allow an empty secret")
}

// TestPaymentGateSplit_Waffo verifies the Phase 8B split for the legacy
// Waffo gateway: the webhook gate depends only on the signing materials,
// while the topup creation gate additionally requires compliance and the
// explicit WaffoEnabled toggle.
func TestPaymentGateSplit_Waffo(t *testing.T) {
	originalEnabled := setting.WaffoEnabled
	originalSandbox := setting.WaffoSandbox
	originalAPIKey := setting.WaffoApiKey
	originalPrivateKey := setting.WaffoPrivateKey
	originalPublicCert := setting.WaffoPublicCert
	originalSandboxAPIKey := setting.WaffoSandboxApiKey
	originalSandboxPrivateKey := setting.WaffoSandboxPrivateKey
	originalSandboxPublicCert := setting.WaffoSandboxPublicCert
	t.Cleanup(func() {
		setting.WaffoEnabled = originalEnabled
		setting.WaffoSandbox = originalSandbox
		setting.WaffoApiKey = originalAPIKey
		setting.WaffoPrivateKey = originalPrivateKey
		setting.WaffoPublicCert = originalPublicCert
		setting.WaffoSandboxApiKey = originalSandboxAPIKey
		setting.WaffoSandboxPrivateKey = originalSandboxPrivateKey
		setting.WaffoSandboxPublicCert = originalSandboxPublicCert
	})

	// Compliance revoked + WaffoEnabled false + signing config set (prod).
	// Webhook must stay true, topup must be false.
	revokePaymentComplianceForTest(t)
	setting.WaffoEnabled = false
	setting.WaffoSandbox = false
	setting.WaffoApiKey = "api"
	setting.WaffoPrivateKey = "private"
	setting.WaffoPublicCert = "public"

	require.True(t, isWaffoWebhookEnabled(), "webhook gate must not depend on compliance or WaffoEnabled")
	require.False(t, isWaffoTopUpEnabled(), "topup gate must require compliance + WaffoEnabled")

	// Compliance confirmed but WaffoEnabled still false: webhook true, topup false.
	confirmPaymentComplianceForTest(t)
	require.True(t, isWaffoWebhookEnabled())
	require.False(t, isWaffoTopUpEnabled())

	// Toggling WaffoEnabled on flips topup to true.
	setting.WaffoEnabled = true
	require.True(t, isWaffoTopUpEnabled())

	// Removing the production API key disables both gates.
	setting.WaffoApiKey = ""
	require.False(t, isWaffoWebhookEnabled())
	require.False(t, isWaffoTopUpEnabled())

	// Sandbox mode with full sandbox signing config re-enables the webhook
	// gate; topup is gated by WaffoEnabled + compliance which are both set.
	setting.WaffoSandbox = true
	setting.WaffoSandboxApiKey = "sandbox_api"
	setting.WaffoSandboxPrivateKey = "sandbox_private"
	setting.WaffoSandboxPublicCert = "sandbox_public"
	require.True(t, isWaffoWebhookEnabled())
	require.True(t, isWaffoTopUpEnabled())

	// Missing sandbox API key disables both gates regardless of production keys.
	setting.WaffoSandboxApiKey = ""
	require.False(t, isWaffoWebhookEnabled())
	require.False(t, isWaffoTopUpEnabled())
}

// TestPaymentGateSplit_WaffoPancake verifies the Phase 8B split for Waffo
// Pancake. The Pancake SDK carries the test/prod public signing keys inside
// the binary and selects the matching key from the payload's mode field (see
// service.VerifyConfiguredWaffoPancakeWebhook -> pancake.VerifyWebhookTyped
// with nil keys), so webhook verification does NOT depend on the operator's
// per-store WaffoPancakeMerchantID / WaffoPancakePrivateKey. The webhook
// gate is therefore always true — the actual signature verifier rejects bad
// payloads on its own — so already-pending orders stay fulfillable even after
// merchant credentials are cleared or rotated. The topup creation gate still
// requires compliance + MerchantID + PrivateKey + the wallet topup ProductID.
func TestPaymentGateSplit_WaffoPancake(t *testing.T) {
	originalMerchantID := setting.WaffoPancakeMerchantID
	originalPrivateKey := setting.WaffoPancakePrivateKey
	originalProductID := setting.WaffoPancakeProductID
	t.Cleanup(func() {
		setting.WaffoPancakeMerchantID = originalMerchantID
		setting.WaffoPancakePrivateKey = originalPrivateKey
		setting.WaffoPancakeProductID = originalProductID
	})

	// Compliance revoked + wallet ProductID empty + merchant/private empty.
	// Webhook must stay true (verifier uses SDK public keys, not merchant
	// creds), topup must be false (creation needs creds + product).
	revokePaymentComplianceForTest(t)
	setting.WaffoPancakeMerchantID = ""
	setting.WaffoPancakePrivateKey = ""
	setting.WaffoPancakeProductID = ""

	require.True(t, isWaffoPancakeWebhookEnabled(), "webhook gate uses SDK public keys, not merchant creds; must stay true")
	require.False(t, isWaffoPancakeTopUpEnabled(), "topup gate must require compliance + creds + ProductID")

	// Compliance confirmed but creds + ProductID still empty: webhook true,
	// topup false.
	confirmPaymentComplianceForTest(t)
	require.True(t, isWaffoPancakeWebhookEnabled())
	require.False(t, isWaffoPancakeTopUpEnabled())

	// Setting merchant + private creds + wallet ProductID flips topup to true.
	setting.WaffoPancakeMerchantID = "merchant"
	setting.WaffoPancakePrivateKey = "private"
	setting.WaffoPancakeProductID = "product"
	require.True(t, isWaffoPancakeTopUpEnabled())

	// Clearing the merchant ID strands the webhook gate nowhere (still true)
	// but disables the topup creation gate.
	setting.WaffoPancakeMerchantID = ""
	require.True(t, isWaffoPancakeWebhookEnabled(), "webhook gate must not depend on merchant creds")
	require.False(t, isWaffoPancakeTopUpEnabled(), "topup gate must require merchant creds")

	// Clearing the private key (merchant ID restored) likewise leaves the
	// webhook gate on but disables the topup creation gate.
	setting.WaffoPancakeMerchantID = "merchant"
	setting.WaffoPancakePrivateKey = ""
	require.True(t, isWaffoPancakeWebhookEnabled(), "webhook gate must not depend on private creds")
	require.False(t, isWaffoPancakeTopUpEnabled(), "topup gate must require private creds")

	// Removing the wallet ProductID (creds restored) leaves the webhook gate
	// on but disables the topup creation gate.
	setting.WaffoPancakePrivateKey = "private"
	setting.WaffoPancakeProductID = ""
	require.True(t, isWaffoPancakeWebhookEnabled())
	require.False(t, isWaffoPancakeTopUpEnabled(), "topup gate must require wallet ProductID")
}

// TestPaymentGateSplit_Epay verifies the Phase 8B split for Epay: the webhook
// gate depends only on the verification config (PayAddress/EpayId/EpayKey),
// while the topup creation gate additionally requires compliance and at least
// one wallet pay method.
func TestPaymentGateSplit_Epay(t *testing.T) {
	originalPayAddress := operation_setting.PayAddress
	originalEpayID := operation_setting.EpayId
	originalEpayKey := operation_setting.EpayKey
	originalPayMethods := operation_setting.PayMethods
	t.Cleanup(func() {
		operation_setting.PayAddress = originalPayAddress
		operation_setting.EpayId = originalEpayID
		operation_setting.EpayKey = originalEpayKey
		operation_setting.PayMethods = originalPayMethods
	})

	// Compliance revoked + PayMethods nil + verification config set.
	// Webhook must stay true, topup must be false.
	revokePaymentComplianceForTest(t)
	operation_setting.PayAddress = "https://pay.example.com"
	operation_setting.EpayId = "epay_id"
	operation_setting.EpayKey = "epay_key"
	operation_setting.PayMethods = nil

	require.True(t, isEpayWebhookEnabled(), "webhook gate must not depend on compliance or PayMethods")
	require.False(t, isEpayTopUpEnabled(), "topup gate must require compliance + non-empty PayMethods")

	// Compliance confirmed but PayMethods still nil: webhook true, topup false.
	confirmPaymentComplianceForTest(t)
	require.True(t, isEpayWebhookEnabled())
	require.False(t, isEpayTopUpEnabled())

	// Adding a wallet pay method flips topup to true.
	operation_setting.PayMethods = []map[string]string{{"type": "alipay"}}
	require.True(t, isEpayTopUpEnabled())

	// Removing the EpayKey disables both gates.
	operation_setting.EpayKey = ""
	require.False(t, isEpayWebhookEnabled())
	require.False(t, isEpayTopUpEnabled())
}
