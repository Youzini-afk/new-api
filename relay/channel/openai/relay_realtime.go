package openai

import (
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/governance"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

func OpenaiRealtimeHandler(c *gin.Context, info *relaycommon.RelayInfo) (*types.NewAPIError, *dto.RealtimeUsage) {
	if info == nil || info.ClientWs == nil || info.TargetWs == nil {
		return types.NewError(fmt.Errorf("invalid websocket connection"), types.ErrorCodeBadResponse), nil
	}

	info.IsStream = true
	clientConn := info.ClientWs
	targetConn := info.TargetWs
	broker := getRealtimeClientBroker(c, clientConn)
	subscription, replay := broker.subscribe()
	defer broker.unsubscribe(subscription)

	targetClosed := make(chan struct{})
	errChan := make(chan error, 4)
	upstreamErrChan := make(chan *types.NewAPIError, 1)

	usage := &dto.RealtimeUsage{}
	localUsage := &dto.RealtimeUsage{}
	sumUsage := &dto.RealtimeUsage{}
	var stateMu sync.Mutex
	var stopped atomic.Bool
	var readers sync.WaitGroup

	reportError := func(err error) {
		if err == nil {
			return
		}
		select {
		case errChan <- err:
		default:
		}
	}

	processClientMessage := func(message []byte) error {
		if stopped.Load() {
			return nil
		}
		realtimeEvent := &dto.RealtimeEvent{}
		if err := common.Unmarshal(message, realtimeEvent); err != nil {
			return fmt.Errorf("error unmarshalling realtime client message: %w", err)
		}

		stateMu.Lock()
		if stopped.Load() {
			stateMu.Unlock()
			return nil
		}
		if realtimeEvent.Type == dto.RealtimeEventTypeSessionUpdate && realtimeEvent.Session != nil && realtimeEvent.Session.Tools != nil {
			info.RealtimeTools = realtimeEvent.Session.Tools
		}
		textToken, audioToken, err := service.CountTokenRealtime(info, *realtimeEvent, info.UpstreamModelName)
		if err == nil {
			localUsage.TotalTokens += textToken + audioToken
			localUsage.InputTokens += textToken + audioToken
			localUsage.InputTokenDetails.TextTokens += textToken
			localUsage.InputTokenDetails.AudioTokens += audioToken
		}
		stateMu.Unlock()
		if err != nil {
			return fmt.Errorf("error counting realtime input token: %w", err)
		}
		logger.LogInfo(c, fmt.Sprintf("type: %s, textToken: %d, audioToken: %d", realtimeEvent.Type, textToken, audioToken))
		if stopped.Load() {
			return nil
		}
		if err := helper.WssString(c, targetConn, string(message)); err != nil {
			return fmt.Errorf("error writing to realtime target: %w", err)
		}
		return nil
	}

	readers.Add(1)
	gopool.Go(func() {
		defer readers.Done()
		defer func() {
			if r := recover(); r != nil {
				reportError(fmt.Errorf("panic in realtime client forwarder: %v", r))
			}
		}()
		for _, message := range replay {
			if err := processClientMessage(message); err != nil {
				reportError(err)
				return
			}
		}
		for {
			select {
			case <-c.Done():
				return
			case <-subscription.done:
				return
			case <-broker.Done():
				return
			case message := <-subscription.messages:
				if err := processClientMessage(message); err != nil {
					reportError(err)
					return
				}
			}
		}
	})

	readers.Add(1)
	gopool.Go(func() {
		defer readers.Done()
		defer func() {
			if r := recover(); r != nil {
				reportError(fmt.Errorf("panic in realtime target reader: %v", r))
			}
		}()
		for {
			if stopped.Load() {
				return
			}
			_, message, err := targetConn.ReadMessage()
			if err != nil {
				if !stopped.Load() && !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					reportError(fmt.Errorf("error reading from realtime target: %w", err))
				} else {
					select {
					case <-targetClosed:
					default:
						close(targetClosed)
					}
				}
				return
			}
			if stopped.Load() {
				return
			}
			info.SetFirstResponseTime()
			realtimeEvent := &dto.RealtimeEvent{}
			if err = common.Unmarshal(message, realtimeEvent); err != nil {
				reportError(fmt.Errorf("error unmarshalling realtime target message: %w", err))
				return
			}
			upstreamErr := governance.ParseUpstreamErrorEnvelope(message)
			if upstreamErr == nil && realtimeEvent.Type == dto.RealtimeEventTypeError {
				upstreamErr = types.NewOpenAIError(
					fmt.Errorf("upstream realtime returned an error event"),
					types.ErrorCodeBadResponse,
					http.StatusBadGateway,
				)
			}
			if upstreamErr != nil {
				select {
				case upstreamErrChan <- upstreamErr:
				default:
				}
				return
			}

			stateMu.Lock()
			if stopped.Load() {
				stateMu.Unlock()
				return
			}
			if realtimeEvent.Type == dto.RealtimeEventTypeResponseDone {
				var realtimeUsage *dto.RealtimeUsage
				if realtimeEvent.Response != nil {
					realtimeUsage = realtimeEvent.Response.Usage
				}
				if realtimeUsage != nil {
					usage.TotalTokens += realtimeUsage.TotalTokens
					usage.InputTokens += realtimeUsage.InputTokens
					usage.OutputTokens += realtimeUsage.OutputTokens
					usage.InputTokenDetails.AudioTokens += realtimeUsage.InputTokenDetails.AudioTokens
					usage.InputTokenDetails.CachedTokens += realtimeUsage.InputTokenDetails.CachedTokens
					usage.InputTokenDetails.TextTokens += realtimeUsage.InputTokenDetails.TextTokens
					usage.OutputTokenDetails.AudioTokens += realtimeUsage.OutputTokenDetails.AudioTokens
					usage.OutputTokenDetails.TextTokens += realtimeUsage.OutputTokenDetails.TextTokens
					if err := preConsumeUsage(c, info, usage, sumUsage); err != nil {
						stateMu.Unlock()
						reportError(fmt.Errorf("error consuming realtime usage: %w", err))
						return
					}
					usage = &dto.RealtimeUsage{}
					localUsage = &dto.RealtimeUsage{}
				} else {
					textToken, audioToken, countErr := service.CountTokenRealtime(info, *realtimeEvent, info.UpstreamModelName)
					if countErr != nil {
						stateMu.Unlock()
						reportError(fmt.Errorf("error counting realtime completion token: %w", countErr))
						return
					}
					logger.LogInfo(c, fmt.Sprintf("type: %s, textToken: %d, audioToken: %d", realtimeEvent.Type, textToken, audioToken))
					localUsage.TotalTokens += textToken + audioToken
					info.IsFirstRequest = false
					localUsage.InputTokens += textToken + audioToken
					localUsage.InputTokenDetails.TextTokens += textToken
					localUsage.InputTokenDetails.AudioTokens += audioToken
					if err := preConsumeUsage(c, info, localUsage, sumUsage); err != nil {
						stateMu.Unlock()
						reportError(fmt.Errorf("error consuming fallback realtime usage: %w", err))
						return
					}
					localUsage = &dto.RealtimeUsage{}
				}
			} else if realtimeEvent.Type == dto.RealtimeEventTypeSessionUpdated || realtimeEvent.Type == dto.RealtimeEventTypeSessionCreated {
				if realtimeEvent.Session != nil {
					info.InputAudioFormat = common.GetStringIfEmpty(realtimeEvent.Session.InputAudioFormat, info.InputAudioFormat)
					info.OutputAudioFormat = common.GetStringIfEmpty(realtimeEvent.Session.OutputAudioFormat, info.OutputAudioFormat)
				}
			} else {
				textToken, audioToken, countErr := service.CountTokenRealtime(info, *realtimeEvent, info.UpstreamModelName)
				if countErr != nil {
					stateMu.Unlock()
					reportError(fmt.Errorf("error counting realtime output token: %w", countErr))
					return
				}
				logger.LogInfo(c, fmt.Sprintf("type: %s, textToken: %d, audioToken: %d", realtimeEvent.Type, textToken, audioToken))
				localUsage.TotalTokens += textToken + audioToken
				localUsage.OutputTokens += textToken + audioToken
				localUsage.OutputTokenDetails.TextTokens += textToken
				localUsage.OutputTokenDetails.AudioTokens += audioToken
			}
			stateMu.Unlock()

			if stopped.Load() {
				return
			}
			if err = helper.WssString(c, clientConn, string(message)); err != nil {
				reportError(fmt.Errorf("error writing to realtime client: %w", err))
				return
			}
			helper.MarkStreamResponseStarted(c)
			broker.commit()
		}
	})

	var returnErr *types.NewAPIError
	select {
	case <-broker.Done():
		if brokerErr := broker.Err(); brokerErr != nil {
			logger.LogError(c, "realtime client closed: "+brokerErr.Error())
		}
	case <-targetClosed:
		if !helper.HasStreamResponseStarted(c) {
			returnErr = types.NewOpenAIError(
				fmt.Errorf("upstream realtime connection closed before the first response event"),
				types.ErrorCodeBadResponse,
				http.StatusBadGateway,
			)
		}
	case upstreamErr := <-upstreamErrChan:
		if helper.HasStreamResponseStarted(c) {
			safe := governance.SanitizeRelayErrorForClient(c, upstreamErr)
			helper.WssError(c, clientConn, safe.OpenAIError)
			governance.MarkHandledStreamError(c, upstreamErr)
		} else {
			// The request-scoped broker retains and replays client events, so the
			// controller can safely select another channel for a first-frame error.
			returnErr = upstreamErr
		}
	case err := <-errChan:
		upstreamErr := types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusBadGateway)
		if helper.HasStreamResponseStarted(c) {
			safe := governance.SanitizeRelayErrorForClient(c, upstreamErr)
			helper.WssError(c, clientConn, safe.OpenAIError)
			governance.MarkHandledStreamError(c, upstreamErr)
		} else {
			returnErr = upstreamErr
		}
	case <-c.Done():
	}

	stopped.Store(true)
	broker.unsubscribe(subscription)
	_ = targetConn.Close()
	readers.Wait()

	stateMu.Lock()
	if returnErr == nil {
		if usage.TotalTokens != 0 {
			_ = preConsumeUsage(c, info, usage, sumUsage)
		}
		if localUsage.TotalTokens != 0 {
			_ = preConsumeUsage(c, info, localUsage, sumUsage)
		}
	}
	stateMu.Unlock()

	return returnErr, sumUsage
}

func preConsumeUsage(ctx *gin.Context, info *relaycommon.RelayInfo, usage *dto.RealtimeUsage, totalUsage *dto.RealtimeUsage) error {
	if usage == nil || totalUsage == nil {
		return fmt.Errorf("invalid usage pointer")
	}

	totalUsage.TotalTokens += usage.TotalTokens
	totalUsage.InputTokens += usage.InputTokens
	totalUsage.OutputTokens += usage.OutputTokens
	totalUsage.InputTokenDetails.CachedTokens += usage.InputTokenDetails.CachedTokens
	totalUsage.InputTokenDetails.TextTokens += usage.InputTokenDetails.TextTokens
	totalUsage.InputTokenDetails.AudioTokens += usage.InputTokenDetails.AudioTokens
	totalUsage.OutputTokenDetails.TextTokens += usage.OutputTokenDetails.TextTokens
	totalUsage.OutputTokenDetails.AudioTokens += usage.OutputTokenDetails.AudioTokens
	// clear usage
	err := service.PreWssConsumeQuota(ctx, info, usage)
	return err
}
