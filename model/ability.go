package model

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/samber/lo"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Ability struct {
	Group     string  `json:"group" gorm:"type:varchar(64);primaryKey;autoIncrement:false"`
	Model     string  `json:"model" gorm:"type:varchar(255);primaryKey;autoIncrement:false"`
	ChannelId int     `json:"channel_id" gorm:"primaryKey;autoIncrement:false;index"`
	Enabled   bool    `json:"enabled"`
	Priority  *int64  `json:"priority" gorm:"bigint;default:0;index"`
	Weight    uint    `json:"weight" gorm:"default:0;index"`
	Tag       *string `json:"tag" gorm:"index"`
}

type AbilityWithChannel struct {
	Ability
	ChannelType int `json:"channel_type"`
}

func GetAllEnableAbilityWithChannels() ([]AbilityWithChannel, error) {
	var abilities []AbilityWithChannel
	err := DB.Table("abilities").
		Select("abilities.*, channels.type as channel_type").
		Joins("left join channels on abilities.channel_id = channels.id").
		Where("abilities.enabled = ?", true).
		Scan(&abilities).Error
	if err != nil {
		return nil, err
	}
	baseAbilities := make([]Ability, 0, len(abilities))
	channelTypes := make(map[int]int, len(abilities))
	for _, ability := range abilities {
		baseAbilities = append(baseAbilities, ability.Ability)
		channelTypes[ability.ChannelId] = ability.ChannelType
	}
	filtered, err := filterAbilitiesByRequestPath(baseAbilities, "")
	if err != nil {
		return nil, err
	}
	result := make([]AbilityWithChannel, 0, len(filtered))
	for _, ability := range filtered {
		result = append(result, AbilityWithChannel{Ability: ability, ChannelType: channelTypes[ability.ChannelId]})
	}
	return result, nil
}

func GetGroupEnabledModels(group string) []string {
	if common.MemoryCacheEnabled {
		return getCachedAvailableModels(group, false)
	}
	var abilities []Ability
	if err := DB.Where(commonGroupCol+" = ? and enabled = ?", group, true).Find(&abilities).Error; err != nil {
		return []string{}
	}
	filtered, err := filterAbilitiesByRequestPath(abilities, "")
	if err != nil {
		return []string{}
	}
	return uniqueAbilityModels(filtered)
}

func GetEnabledModels() []string {
	if common.MemoryCacheEnabled {
		return getCachedAvailableModels("", true)
	}
	var abilities []Ability
	if err := DB.Where("enabled = ?", true).Find(&abilities).Error; err != nil {
		return []string{}
	}
	filtered, err := filterAbilitiesByRequestPath(abilities, "")
	if err != nil {
		return []string{}
	}
	return uniqueAbilityModels(filtered)
}

func GetAllEnableAbilities() []Ability {
	var abilities []Ability
	if err := DB.Find(&abilities, "enabled = ?", true).Error; err != nil {
		return []Ability{}
	}
	filtered, err := filterAbilitiesByRequestPath(abilities, "")
	if err != nil {
		return []Ability{}
	}
	return filtered
}

func uniqueAbilityModels(abilities []Ability) []string {
	seen := make(map[string]struct{}, len(abilities))
	models := make([]string, 0, len(abilities))
	for _, ability := range abilities {
		if _, ok := seen[ability.Model]; ok {
			continue
		}
		seen[ability.Model] = struct{}{}
		models = append(models, ability.Model)
	}
	sort.Strings(models)
	return models
}

func getCachedAvailableModels(group string, allGroups bool) []string {
	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()
	if group2model2channels == nil {
		return []string{}
	}

	now := time.Now()
	seen := make(map[string]struct{})
	models := make([]string, 0)
	appendAvailableModels := func(model2channels map[string][]int) {
		for modelName, channelIDs := range model2channels {
			if _, ok := seen[modelName]; ok {
				continue
			}
			for _, channelID := range channelIDs {
				if !isCachedChannelAvailableAtLocked(channelID, now) {
					continue
				}
				seen[modelName] = struct{}{}
				models = append(models, modelName)
				break
			}
		}
	}

	if allGroups {
		for _, model2channels := range group2model2channels {
			appendAvailableModels(model2channels)
		}
	} else {
		appendAvailableModels(group2model2channels[group])
	}
	sort.Strings(models)
	return models
}

func GetChannel(group string, model string, retry int, requestPath string) (*Channel, error) {
	var abilities []Ability
	if err := DB.Where(commonGroupCol+" = ? and model = ? and enabled = ?", group, model, true).
		Order("priority DESC").
		Order("weight DESC").
		Find(&abilities).Error; err != nil {
		return nil, err
	}
	filtered, err := filterAbilitiesByRequestPath(abilities, requestPath)
	if err != nil {
		return nil, err
	}
	abilities = filtered
	if len(abilities) == 0 {
		normalizedModel := ratio_setting.FormatMatchingModelName(model)
		if normalizedModel != "" && normalizedModel != model {
			if err := DB.Where(commonGroupCol+" = ? and model = ? and enabled = ?", group, normalizedModel, true).
				Order("priority DESC").
				Order("weight DESC").
				Find(&abilities).Error; err != nil {
				return nil, err
			}
			abilities, err = filterAbilitiesByRequestPath(abilities, requestPath)
			if err != nil {
				return nil, err
			}
		}
	}
	if len(abilities) == 0 {
		return nil, nil
	}

	prioritySet := make(map[int64]struct{})
	for _, ability := range abilities {
		prioritySet[abilityPriority(ability)] = struct{}{}
	}
	priorities := make([]int64, 0, len(prioritySet))
	for priority := range prioritySet {
		priorities = append(priorities, priority)
	}
	sort.Slice(priorities, func(i, j int) bool { return priorities[i] > priorities[j] })
	if retry >= len(priorities) {
		retry = len(priorities) - 1
	}
	if retry < 0 {
		retry = 0
	}
	targetPriority := priorities[retry]
	targetAbilities := make([]Ability, 0, len(abilities))
	for _, ability := range abilities {
		if abilityPriority(ability) == targetPriority {
			targetAbilities = append(targetAbilities, ability)
		}
	}

	channel := Channel{}
	weightSum := uint(0)
	for _, ability := range targetAbilities {
		weightSum += ability.Weight + 10
	}
	weight := common.GetRandomInt(int(weightSum))
	for _, ability := range targetAbilities {
		weight -= int(ability.Weight) + 10
		if weight <= 0 {
			channel.Id = ability.ChannelId
			break
		}
	}
	err = DB.First(&channel, "id = ?", channel.Id).Error
	return &channel, err
}

func abilityPriority(ability Ability) int64 {
	if ability.Priority == nil {
		return 0
	}
	return *ability.Priority
}

// filterAbilitiesByRequestPath applies effective channel availability before
// optional Advanced Custom path matching for the DB (non-memory-cache) path.
func filterAbilitiesByRequestPath(abilities []Ability, requestPath string) ([]Ability, error) {
	if len(abilities) == 0 {
		return abilities, nil
	}

	channelIds := make([]int, 0, len(abilities))
	seen := make(map[int]struct{}, len(abilities))
	for _, ability := range abilities {
		if _, ok := seen[ability.ChannelId]; ok {
			continue
		}
		seen[ability.ChannelId] = struct{}{}
		channelIds = append(channelIds, ability.ChannelId)
	}

	var channels []*Channel
	if err := DB.Where("id IN ?", channelIds).Find(&channels).Error; err != nil {
		return nil, err
	}

	now := time.Now()
	availableChannels := make(map[int]bool, len(channels))
	advancedConfigs := make(map[int]*dto.AdvancedCustomConfig)
	for _, channel := range channels {
		availableChannels[channel.Id] = channel.IsAvailableAt(now)
		if requestPath != "" && channel.Type == constant.ChannelTypeAdvancedCustom {
			advancedConfigs[channel.Id] = channel.GetOtherSettings().AdvancedCustom
		}
	}

	filtered := make([]Ability, 0, len(abilities))
	for _, ability := range abilities {
		if !availableChannels[ability.ChannelId] {
			continue
		}
		if requestPath == "" {
			filtered = append(filtered, ability)
			continue
		}
		config, isAdvancedCustom := advancedConfigs[ability.ChannelId]
		if !isAdvancedCustom {
			filtered = append(filtered, ability)
			continue
		}
		if config != nil && config.SupportsPath(requestPath) {
			filtered = append(filtered, ability)
		}
	}
	return filtered, nil
}

func (channel *Channel) AddAbilities(tx *gorm.DB) error {
	models_ := strings.Split(channel.Models, ",")
	groups_ := strings.Split(channel.Group, ",")
	abilitySet := make(map[string]struct{})
	abilities := make([]Ability, 0, len(models_))
	for _, model := range models_ {
		for _, group := range groups_ {
			key := group + "|" + model
			if _, exists := abilitySet[key]; exists {
				continue
			}
			abilitySet[key] = struct{}{}
			ability := Ability{
				Group:     group,
				Model:     model,
				ChannelId: channel.Id,
				Enabled:   channel.Status == common.ChannelStatusEnabled,
				Priority:  channel.Priority,
				Weight:    uint(channel.GetWeight()),
				Tag:       channel.Tag,
			}
			abilities = append(abilities, ability)
		}
	}
	if len(abilities) == 0 {
		return nil
	}
	// choose DB or provided tx
	useDB := DB
	if tx != nil {
		useDB = tx
	}
	for _, chunk := range lo.Chunk(abilities, 50) {
		err := useDB.Clauses(clause.OnConflict{DoNothing: true}).Create(&chunk).Error
		if err != nil {
			return err
		}
	}
	return nil
}

func (channel *Channel) DeleteAbilities() error {
	return DB.Where("channel_id = ?", channel.Id).Delete(&Ability{}).Error
}

// UpdateAbilities updates abilities of this channel.
// Make sure the channel is completed before calling this function.
func (channel *Channel) UpdateAbilities(tx *gorm.DB) error {
	isNewTx := false
	// 如果没有传入事务，创建新的事务
	if tx == nil {
		tx = DB.Begin()
		if tx.Error != nil {
			return tx.Error
		}
		isNewTx = true
		defer func() {
			if r := recover(); r != nil {
				tx.Rollback()
			}
		}()
	}

	// First delete all abilities of this channel
	err := tx.Where("channel_id = ?", channel.Id).Delete(&Ability{}).Error
	if err != nil {
		if isNewTx {
			tx.Rollback()
		}
		return err
	}

	// Then add new abilities
	models_ := strings.Split(channel.Models, ",")
	groups_ := strings.Split(channel.Group, ",")
	abilitySet := make(map[string]struct{})
	abilities := make([]Ability, 0, len(models_))
	for _, model := range models_ {
		for _, group := range groups_ {
			key := group + "|" + model
			if _, exists := abilitySet[key]; exists {
				continue
			}
			abilitySet[key] = struct{}{}
			ability := Ability{
				Group:     group,
				Model:     model,
				ChannelId: channel.Id,
				Enabled:   channel.Status == common.ChannelStatusEnabled,
				Priority:  channel.Priority,
				Weight:    uint(channel.GetWeight()),
				Tag:       channel.Tag,
			}
			abilities = append(abilities, ability)
		}
	}

	if len(abilities) > 0 {
		for _, chunk := range lo.Chunk(abilities, 50) {
			err = tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&chunk).Error
			if err != nil {
				if isNewTx {
					tx.Rollback()
				}
				return err
			}
		}
	}

	// 如果是新创建的事务，需要提交
	if isNewTx {
		return tx.Commit().Error
	}

	return nil
}

func UpdateAbilityStatus(channelId int, status bool) error {
	return DB.Model(&Ability{}).Where("channel_id = ?", channelId).Select("enabled").Update("enabled", status).Error
}

func UpdateAbilityStatusByTag(tag string, status bool) error {
	return DB.Model(&Ability{}).Where("tag = ?", tag).Select("enabled").Update("enabled", status).Error
}

func UpdateAbilityByTag(tag string, newTag *string, priority *int64, weight *uint) error {
	ability := Ability{}
	if newTag != nil {
		ability.Tag = newTag
	}
	if priority != nil {
		ability.Priority = priority
	}
	if weight != nil {
		ability.Weight = *weight
	}
	return DB.Model(&Ability{}).Where("tag = ?", tag).Updates(ability).Error
}

var fixLock = sync.Mutex{}

func FixAbility() (int, int, error) {
	lock := fixLock.TryLock()
	if !lock {
		return 0, 0, errors.New("已经有一个修复任务在运行中，请稍后再试")
	}
	defer fixLock.Unlock()

	// truncate abilities table
	if common.UsingMainDatabase(common.DatabaseTypeSQLite) {
		err := DB.Exec("DELETE FROM abilities").Error
		if err != nil {
			common.SysLog(fmt.Sprintf("Delete abilities failed: %s", err.Error()))
			return 0, 0, err
		}
	} else {
		err := DB.Exec("TRUNCATE TABLE abilities").Error
		if err != nil {
			common.SysLog(fmt.Sprintf("Truncate abilities failed: %s", err.Error()))
			return 0, 0, err
		}
	}
	var channels []*Channel
	// Find all channels
	err := DB.Model(&Channel{}).Find(&channels).Error
	if err != nil {
		return 0, 0, err
	}
	if len(channels) == 0 {
		return 0, 0, nil
	}
	successCount := 0
	failCount := 0
	for _, chunk := range lo.Chunk(channels, 50) {
		ids := lo.Map(chunk, func(c *Channel, _ int) int { return c.Id })
		// Delete all abilities of this channel
		err = DB.Where("channel_id IN ?", ids).Delete(&Ability{}).Error
		if err != nil {
			common.SysLog(fmt.Sprintf("Delete abilities failed: %s", err.Error()))
			failCount += len(chunk)
			continue
		}
		// Then add new abilities
		for _, channel := range chunk {
			err = channel.AddAbilities(nil)
			if err != nil {
				common.SysLog(fmt.Sprintf("Add abilities for channel %d failed: %s", channel.Id, err.Error()))
				failCount++
			} else {
				successCount++
			}
		}
	}
	InitChannelCache()
	return successCount, failCount, nil
}
