package system_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDefaultThemeFrontendIsDefault(t *testing.T) {
	assert.Equal(t, "default", GetThemeSettings().Frontend)
}
