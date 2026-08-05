package helper

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
)

func ModelMappedHelper(c *gin.Context, info *common.RelayInfo, request dto.Request) error {
	if info.ChannelMeta == nil {
		info.ChannelMeta = &common.ChannelMeta{}
	}

	// A RelayInfo can be reused while retrying another channel. Always clear
	// the previous channel's mapping state so an unmapped retry cannot inherit
	// either its upstream model or its public response alias.
	c.Set(string(constant.ContextKeyResponseModelName), "")
	info.IsModelMapped = false

	routingModelName := strings.TrimSpace(c.GetString(string(constant.ContextKeyOriginalModel)))
	if routingModelName == "" {
		routingModelName = strings.TrimSpace(info.ResponseModelAlias)
	}
	if routingModelName == "" {
		routingModelName = strings.TrimSpace(info.OriginModelName)
	}

	isResponsesCompact := info.RelayMode == relayconstant.RelayModeResponsesCompact
	publicModelName := routingModelName
	mappingModelName := routingModelName
	if isResponsesCompact && strings.HasSuffix(mappingModelName, ratio_setting.CompactModelSuffix) {
		mappingModelName = strings.TrimSuffix(mappingModelName, ratio_setting.CompactModelSuffix)
		// The distributor adds this suffix only for routing and billing. Restore
		// the model name that was actually present in the client's compact request.
		publicModelName = mappingModelName
	}
	info.ResponseModelAlias = publicModelName
	info.UpstreamModelName = mappingModelName

	// map model name
	modelMapping := c.GetString("model_mapping")
	if modelMapping != "" && modelMapping != "{}" {
		modelMap := make(map[string]string)
		err := json.Unmarshal([]byte(modelMapping), &modelMap)
		if err != nil {
			return fmt.Errorf("unmarshal_model_mapping_failed")
		}

		// 支持链式模型重定向，最终使用链尾的模型
		currentModel := mappingModelName
		visitedModels := map[string]bool{
			currentModel: true,
		}
		for {
			if mappedModel, exists := modelMap[currentModel]; exists && mappedModel != "" {
				// 模型重定向循环检测，避免无限循环
				if visitedModels[mappedModel] {
					if mappedModel == currentModel {
						if currentModel == mappingModelName {
							info.IsModelMapped = false
							break
						} else {
							info.IsModelMapped = true
							break
						}
					}
					return errors.New("model_mapping_contains_cycle")
				}
				visitedModels[mappedModel] = true
				currentModel = mappedModel
				info.IsModelMapped = true
			} else {
				break
			}
		}
		if info.IsModelMapped {
			info.UpstreamModelName = currentModel
		}
	}

	if isResponsesCompact {
		finalUpstreamModelName := mappingModelName
		if info.IsModelMapped && info.UpstreamModelName != "" {
			finalUpstreamModelName = info.UpstreamModelName
		}
		info.UpstreamModelName = finalUpstreamModelName
		info.OriginModelName = ratio_setting.WithCompactModelSuffix(finalUpstreamModelName)
	}
	// Responses compact always needs the public model restored on the response,
	// including unmapped and self-mapped requests. Both the upstream request and
	// client-visible response use the suffix-free model; only internal billing
	// keeps the compact suffix.
	if (info.IsModelMapped || isResponsesCompact) && publicModelName != "" {
		c.Set(string(constant.ContextKeyResponseModelName), publicModelName)
	}
	if request != nil {
		request.SetModelName(info.UpstreamModelName)
	}
	return nil
}
