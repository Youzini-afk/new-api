package governance

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
)

type upstreamErrorEnvelope struct {
	Type       string          `json:"type"`
	Message    string          `json:"message"`
	Msg        string          `json:"msg"`
	Code       any             `json:"code"`
	Param      string          `json:"param"`
	Status     any             `json:"status"`
	StatusCode int             `json:"status_code"`
	Success    any             `json:"success"`
	Error      json.RawMessage `json:"error"`
	Response   json.RawMessage `json:"response"`
	Data       json.RawMessage `json:"data"`
}

// ParseUpstreamStreamError validates one SSE data payload and recognizes
// explicit upstream error envelopes. A non-JSON payload is malformed for the
// OpenAI-compatible JSON streams that use this entry point, so it is converted
// to an internal typed error instead of ever being forwarded verbatim.
func ParseUpstreamStreamError(data string) *types.NewAPIError {
	trimmed := strings.TrimSpace(data)
	if !json.Valid([]byte(trimmed)) || !strings.HasPrefix(trimmed, "{") {
		message := trimmed
		if message == "" {
			message = "upstream stream returned malformed data"
		}
		return types.NewOpenAIError(
			errors.New(message),
			types.ErrorCodeBadResponseBody,
			http.StatusBadGateway,
		)
	}
	return parseUpstreamErrorEnvelope([]byte(trimmed))
}

// ParseUpstreamStreamEvent adds the SSE event name to error detection. Some
// providers send `event: error` with a message-only data object, which is not
// self-describing once the event line is discarded.
func ParseUpstreamStreamEvent(event, data string) *types.NewAPIError {
	if err := ParseUpstreamStreamError(data); err != nil {
		return err
	}
	eventType := strings.ToLower(strings.TrimSpace(event))
	switch {
	case eventType == "error", eventType == "upstream_error", eventType == "response.error", eventType == "response.failed",
		strings.HasSuffix(eventType, ".error"), strings.HasSuffix(eventType, ".failed"):
		message := strings.TrimSpace(data)
		if message == "" {
			message = "upstream returned an error event"
		}
		return types.NewOpenAIError(errors.New(message), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	default:
		return nil
	}
}

// ParseUpstreamErrorEnvelope recognizes an error only when data is a valid
// JSON object with explicit error semantics. Invalid JSON and ordinary success
// payloads return nil, which makes this safe to use for optional JSON responses
// such as TTS audio bodies and legacy streams whose normal data can be text.
func ParseUpstreamErrorEnvelope(data []byte) *types.NewAPIError {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' || !json.Valid(trimmed) {
		return nil
	}
	return parseUpstreamErrorEnvelope(trimmed)
}

func parseUpstreamErrorEnvelope(data []byte) *types.NewAPIError {
	// Normal stream chunks vastly outnumber error frames. Avoid a second full JSON
	// decode for the common case; explicit error envelopes always carry one of
	// these markers (including legacy {"success": false, ...} providers).
	lower := bytes.ToLower(data)
	if !bytes.Contains(lower, []byte(`"error"`)) &&
		!bytes.Contains(lower, []byte("upstream_error")) &&
		!bytes.Contains(lower, []byte("response.failed")) &&
		!bytes.Contains(lower, []byte("response.error")) &&
		!bytes.Contains(lower, []byte(`"success"`)) &&
		!bytes.Contains(lower, []byte(`"status"`)) &&
		!bytes.Contains(lower, []byte(`"status_code"`)) &&
		!bytes.Contains(lower, []byte(`"code"`)) {
		return nil
	}

	var envelope upstreamErrorEnvelope
	if err := common.Unmarshal(data, &envelope); err != nil {
		return nil
	}

	eventType := strings.ToLower(strings.TrimSpace(envelope.Type))
	isErrorEvent := eventType == "error" || eventType == "upstream_error" ||
		eventType == "response.error" || eventType == "response.failed" ||
		strings.HasSuffix(eventType, ".error") || strings.HasSuffix(eventType, ".failed")
	isFailedResponse := upstreamSuccessIndicatesFailure(envelope.Success)
	errorRaw := firstMeaningfulUpstreamError(data, envelope.Error)

	topLevelStatus := envelope.StatusCode
	if topLevelStatus < 400 || topLevelStatus > 599 {
		topLevelStatus = upstreamHTTPStatus(envelope.Status)
	}
	if topLevelStatus < 400 || topLevelStatus > 599 {
		topLevelStatus = upstreamHTTPStatus(envelope.Code)
	}
	if topLevelStatus >= 400 && topLevelStatus <= 599 {
		isErrorEvent = true
	}
	if upstreamStatusIndicatesFailure(envelope.Status) {
		isErrorEvent = true
	}

	if len(errorRaw) == 0 && len(meaningfulUpstreamJSON(envelope.Response)) > 0 {
		var responseFailed bool
		errorRaw, responseFailed = nestedUpstreamError(envelope.Response)
		isErrorEvent = isErrorEvent || responseFailed
	}
	if len(errorRaw) == 0 && len(meaningfulUpstreamJSON(envelope.Data)) > 0 {
		var dataFailed bool
		errorRaw, dataFailed = nestedUpstreamError(envelope.Data)
		isErrorEvent = isErrorEvent || dataFailed
	}
	if !isErrorEvent && !isFailedResponse && len(errorRaw) == 0 {
		return nil
	}

	oaiError := types.OpenAIError{}
	if len(errorRaw) > 0 {
		if errorRaw[0] == '"' {
			_ = common.Unmarshal(errorRaw, &oaiError.Message)
		} else {
			_ = common.Unmarshal(errorRaw, &oaiError)
		}
	}
	if strings.TrimSpace(oaiError.Message) == "" {
		oaiError.Message = strings.TrimSpace(envelope.Message)
	}
	if strings.TrimSpace(oaiError.Message) == "" {
		oaiError.Message = strings.TrimSpace(envelope.Msg)
	}
	if strings.TrimSpace(oaiError.Message) == "" {
		oaiError.Message = "upstream returned an error event"
	}
	if strings.TrimSpace(oaiError.Type) == "" {
		oaiError.Type = "upstream_error"
	}
	if oaiError.Code == nil || strings.TrimSpace(common.Interface2String(oaiError.Code)) == "" {
		if envelope.Code != nil && strings.TrimSpace(common.Interface2String(envelope.Code)) != "" {
			oaiError.Code = envelope.Code
		} else {
			oaiError.Code = "upstream_stream_error"
		}
	}
	if oaiError.Param == "" {
		oaiError.Param = envelope.Param
	}

	statusCode := topLevelStatus
	if statusCode < 400 || statusCode > 599 {
		statusCode = upstreamHTTPStatus(oaiError.Code)
	}
	if statusCode < 400 || statusCode > 599 {
		statusCode = http.StatusBadGateway
	}
	return types.WithOpenAIError(oaiError, statusCode)
}

func upstreamSuccessIndicatesFailure(value any) bool {
	switch success := value.(type) {
	case bool:
		return !success
	case float64:
		return success == 0
	case float32:
		return success == 0
	case int:
		return success == 0
	case int64:
		return success == 0
	case json.Number:
		return success.String() == "0"
	case string:
		switch strings.ToLower(strings.TrimSpace(success)) {
		case "false", "0", "failed", "failure", "error":
			return true
		}
	}
	return false
}

func upstreamStatusIndicatesFailure(value any) bool {
	status, ok := value.(string)
	if !ok {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "error", "failed", "failure", "cancelled", "canceled":
		return true
	default:
		return false
	}
}

// firstMeaningfulUpstreamError walks the top-level object with a Decoder so a
// meaningful earlier `error` value cannot be hidden by a duplicate trailing
// `error: null` key. The normal struct field remains the fast/common path.
func firstMeaningfulUpstreamError(data []byte, fallback json.RawMessage) json.RawMessage {
	result := meaningfulUpstreamJSON(fallback)
	_ = forEachJSONObjectField(data, func(name string, raw json.RawMessage) bool {
		if !strings.EqualFold(name, "error") {
			return true
		}
		if candidate := meaningfulUpstreamJSON(raw); len(candidate) > 0 {
			result = append(json.RawMessage(nil), candidate...)
			return false
		}
		return true
	})
	return result
}

func nestedUpstreamError(data json.RawMessage) (json.RawMessage, bool) {
	var errorRaw json.RawMessage
	failed := false
	_ = forEachJSONObjectField(data, func(name string, raw json.RawMessage) bool {
		switch strings.ToLower(name) {
		case "error":
			if candidate := meaningfulUpstreamJSON(raw); len(candidate) > 0 && len(errorRaw) == 0 {
				errorRaw = append(json.RawMessage(nil), candidate...)
				failed = true
			}
		case "status":
			var value any
			if common.Unmarshal(raw, &value) == nil && (upstreamStatusIndicatesFailure(value) || (upstreamHTTPStatus(value) >= 400 && upstreamHTTPStatus(value) <= 599)) {
				failed = true
			}
		case "status_code", "code":
			var value any
			if common.Unmarshal(raw, &value) == nil {
				status := upstreamHTTPStatus(value)
				if status >= 400 && status <= 599 {
					failed = true
				}
			}
		case "status_details":
			if nestedError, nestedFailed := nestedUpstreamError(raw); nestedFailed {
				failed = true
				if len(errorRaw) == 0 {
					errorRaw = nestedError
				}
			}
		}
		return true
	})
	return errorRaw, failed
}

func forEachJSONObjectField(data []byte, visit func(name string, raw json.RawMessage) bool) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	start, err := decoder.Token()
	if err != nil {
		return err
	}
	if delimiter, ok := start.(json.Delim); !ok || delimiter != '{' {
		return errors.New("JSON value is not an object")
	}
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return err
		}
		name, ok := key.(string)
		if !ok {
			return errors.New("JSON object key is not a string")
		}
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return err
		}
		if !visit(name, raw) {
			return nil
		}
	}
	_, err = decoder.Token()
	return err
}

func upstreamHTTPStatus(code any) int {
	switch value := code.(type) {
	case float64:
		return int(value)
	case float32:
		return int(value)
	case int:
		return value
	case int32:
		return int(value)
	case int64:
		return int(value)
	case json.Number:
		parsed, _ := strconv.Atoi(value.String())
		return parsed
	case string:
		parsed, _ := strconv.Atoi(strings.TrimSpace(value))
		return parsed
	default:
		return 0
	}
}

func meaningfulUpstreamJSON(raw json.RawMessage) json.RawMessage {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" || trimmed == "{}" || trimmed == "[]" || trimmed == `""` || trimmed == "false" || trimmed == "0" {
		return nil
	}
	return raw
}
