package system_setting

import (
	"encoding/json"
	"strings"

	"github.com/QuantumNous/new-api/setting/config"
)

type ErrorGovernanceAISetting struct {
	Enabled          bool            `json:"enabled"`
	ChannelID        int             `json:"channel_id"`
	Model            string          `json:"model"`
	RedactSensitive  bool            `json:"redact_sensitive"`
	PromptTemplate   string          `json:"prompt_template"`
	JSONOutputParams json.RawMessage `json:"json_output_params"`
}

const DefaultErrorGovernanceAIPromptTemplate = `你是 API 网关中继错误治理规则整理助手。

你的任务是综合整理当前 custom_rules，输出一份更稳定、更少冲突、更易维护的候选规则列表。你不是逐条新增规则，而是做整体治理：合并重复、调整优先级、禁用被覆盖规则、修复明显错误的 regex、统一安全错误文案。

要求：
1. 只能输出 JSON，不要输出 Markdown。
2. 不要包含密钥、Token、Cookie、Authorization 等敏感内容。
3. 不要创建过宽规则，contains/regex 必须尽量匹配稳定错误片段。
4. 自定义规则按数组顺序匹配，越靠前优先级越高。
5. 保留必要规则，删除或禁用明显重复、被覆盖、冲突严重的规则。
6. safe_error_message 必须适合展示给普通用户。
7. 如果无法判断，保守保留原规则并在 summary 中说明。
8. status_code 只能是 400-599，并应保留原规则中明确合理的状态码：参数/格式/内容政策通常为 400，认证为 401，额度不足为 402，权限或封禁为 403，不存在为 404，站内限流为 429，上游响应异常为 502，上游不可用或上游限流为 503，上游超时为 504。
9. 不得为了方便把所有规则统一写成 503；无法可靠判断时可以省略 status_code，由服务端根据规则语义推断。
10. rule_code 必须唯一，且不得使用系统内置规则码；rule_code、safe_error_code、safe_error_type 仅允许字母、数字、点、下划线和短横线，长度不超过 64。

当前治理配置：
{{governance_config}}

当前冲突摘要：
{{conflicts}}

请输出：
{
  "summary": "string",
  "rules": [
    {
      "enabled": true,
      "rule_code": "string",
      "category": "string",
      "match_type": "contains|regex",
      "match_pattern": "string",
      "safe_error_code": "string",
      "safe_error_type": "string",
      "safe_error_message": "string",
      "status_code": 400
    }
  ]
}`

var defaultErrorGovernanceAISetting = ErrorGovernanceAISetting{
	Enabled:          false,
	RedactSensitive:  true,
	PromptTemplate:   DefaultErrorGovernanceAIPromptTemplate,
	JSONOutputParams: json.RawMessage(`{"response_format":{"type":"json_object"}}`),
}

func init() {
	config.GlobalConfig.Register("error_governance_ai", &defaultErrorGovernanceAISetting)
}

func GetErrorGovernanceAISetting() *ErrorGovernanceAISetting {
	normalizeErrorGovernanceAISetting(&defaultErrorGovernanceAISetting)
	return &defaultErrorGovernanceAISetting
}

func NormalizeErrorGovernanceAISetting(input ErrorGovernanceAISetting) ErrorGovernanceAISetting {
	normalizeErrorGovernanceAISetting(&input)
	return input
}

func normalizeErrorGovernanceAISetting(setting *ErrorGovernanceAISetting) {
	setting.Model = strings.TrimSpace(setting.Model)
	setting.PromptTemplate = strings.TrimSpace(setting.PromptTemplate)
	if setting.PromptTemplate == "" {
		setting.PromptTemplate = DefaultErrorGovernanceAIPromptTemplate
	}
	if len(setting.JSONOutputParams) == 0 || !json.Valid(setting.JSONOutputParams) {
		setting.JSONOutputParams = json.RawMessage(`{"response_format":{"type":"json_object"}}`)
	}
}
