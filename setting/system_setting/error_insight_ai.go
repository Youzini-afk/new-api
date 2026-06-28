package system_setting

import (
	"encoding/json"
	"strings"

	"github.com/QuantumNous/new-api/setting/config"
)

type ErrorInsightAISetting struct {
	Enabled              bool            `json:"enabled"`
	ChannelID            int             `json:"channel_id"`
	Model                string          `json:"model"`
	SampleSize           int             `json:"sample_size"`
	BatchLimit           int             `json:"batch_limit"`
	IncludeOriginalError bool            `json:"include_original_error"`
	RedactSensitive      bool            `json:"redact_sensitive"`
	PromptTemplate       string          `json:"prompt_template"`
	JSONOutputParams     json.RawMessage `json:"json_output_params"`
}

const DefaultErrorInsightAIPromptTemplate = `你是 API 网关错误治理规则生成助手。

你的任务是根据未匹配错误样本生成候选匹配规则，用于把上游原始错误归类为安全、稳定、可展示给用户的错误类型。

要求：
1. 只能输出 JSON，不要输出 Markdown。
2. 不要包含密钥、Token、Cookie、Authorization 等敏感内容。
3. 匹配规则不能过宽，必须能解释为什么能匹配这些样本。
4. 如果样本不足以生成可靠规则，返回 confidence 低并说明原因。
5. safe_error_message 必须适合展示给普通用户。
6. original_error_pattern 应尽量匹配稳定片段，不要依赖 request_id、时间戳、随机 ID。

签名：
{{signature}}

样本日志：
{{sample_logs}}

请输出：
{
  "rules": [
    {
      "rule_code": "string",
      "category": "string",
      "match_type": "contains|regex",
      "match_pattern": "string",
      "safe_error_code": "string",
      "safe_error_type": "string",
      "safe_error_message": "string",
      "confidence": 0.0,
      "reason": "string"
    }
  ]
}`

var defaultErrorInsightAISetting = ErrorInsightAISetting{
	Enabled:              false,
	SampleSize:           5,
	BatchLimit:           10,
	IncludeOriginalError: true,
	RedactSensitive:      true,
	PromptTemplate:       DefaultErrorInsightAIPromptTemplate,
	JSONOutputParams:     json.RawMessage(`{"response_format":{"type":"json_object"}}`),
}

func init() {
	config.GlobalConfig.Register("error_insight_ai", &defaultErrorInsightAISetting)
}

func GetErrorInsightAISetting() *ErrorInsightAISetting {
	normalizeErrorInsightAISetting(&defaultErrorInsightAISetting)
	return &defaultErrorInsightAISetting
}

func NormalizeErrorInsightAISetting(input ErrorInsightAISetting) ErrorInsightAISetting {
	normalizeErrorInsightAISetting(&input)
	return input
}

func normalizeErrorInsightAISetting(setting *ErrorInsightAISetting) {
	setting.Model = strings.TrimSpace(setting.Model)
	setting.PromptTemplate = strings.TrimSpace(setting.PromptTemplate)
	if setting.SampleSize <= 0 {
		setting.SampleSize = 5
	}
	if setting.SampleSize > 20 {
		setting.SampleSize = 20
	}
	if setting.BatchLimit <= 0 {
		setting.BatchLimit = 10
	}
	if setting.BatchLimit > 50 {
		setting.BatchLimit = 50
	}
	if setting.PromptTemplate == "" {
		setting.PromptTemplate = DefaultErrorInsightAIPromptTemplate
	}
	if len(setting.JSONOutputParams) == 0 || !json.Valid(setting.JSONOutputParams) {
		setting.JSONOutputParams = json.RawMessage(`{"response_format":{"type":"json_object"}}`)
	}
}
