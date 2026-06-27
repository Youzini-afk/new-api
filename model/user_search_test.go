package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSearchUsersIncludesDiscordFields(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Create(&User{
		Username:             "alpha",
		Password:             "password123",
		DisplayName:          "Alpha User",
		Email:                "alpha@example.com",
		Group:                "default",
		AffCode:              "aff_alpha",
		DiscordId:            "111222333444555666",
		DiscordUsername:      "alpha_dc",
		DiscordGlobalName:    "Alpha Global",
		DiscordDiscriminator: "0001",
		Status:               1,
		Role:                 1,
	}).Error)
	require.NoError(t, DB.Create(&User{
		Username:          "beta",
		Password:          "password123",
		DisplayName:       "Beta User",
		Email:             "beta@example.com",
		Group:             "default",
		AffCode:           "aff_beta",
		DiscordId:         "999888777666555444",
		DiscordUsername:   "beta_dc",
		DiscordGlobalName: "Beta Global",
		Status:            1,
		Role:              1,
	}).Error)

	users, total, err := SearchUsers("alpha_dc", "", nil, nil, 0, 20)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, users, 1)
	require.Equal(t, "alpha", users[0].Username)

	users, total, err = SearchUsers("Beta Global", "", nil, nil, 0, 20)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, users, 1)
	require.Equal(t, "beta", users[0].Username)

	users, total, err = SearchUsers("111222333444555666", "", nil, nil, 0, 20)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, users, 1)
	require.Equal(t, "alpha", users[0].Username)
}

func TestSearchUsersNumericKeywordMatchesUserIDAndDiscordID(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Create(&User{
		Id:              101,
		Username:        "by_id",
		Password:        "password123",
		DisplayName:     "By ID",
		Email:           "by-id@example.com",
		Group:           "default",
		AffCode:         "aff_by_id",
		DiscordId:       "555",
		DiscordUsername: "not_keyword",
		Status:          1,
		Role:            1,
	}).Error)
	require.NoError(t, DB.Create(&User{
		Id:              202,
		Username:        "by_discord_id",
		Password:        "password123",
		DisplayName:     "By Discord ID",
		Email:           "by-discord-id@example.com",
		Group:           "default",
		AffCode:         "aff_by_discord_id",
		DiscordId:       "101",
		DiscordUsername: "not_keyword_either",
		Status:          1,
		Role:            1,
	}).Error)

	users, total, err := SearchUsers("101", "", nil, nil, 0, 20)
	require.NoError(t, err)
	require.Equal(t, int64(2), total)
	require.Len(t, users, 2)
	require.Equal(t, "by_discord_id", users[0].Username)
	require.Equal(t, "by_id", users[1].Username)
}
