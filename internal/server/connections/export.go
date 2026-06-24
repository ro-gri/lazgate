package connections

import (
	"errors"
	"strings"

	"laz/internal/server/integrations/nativeawg"
	"laz/internal/server/model"
)

// AmneziaImportURL derives a second representation without creating another
// connection, changing its keys, or duplicating its secrets in storage.
func AmneziaImportURL(config model.IssuedConfig) (string, error) {
	switch config.Kind {
	case model.ConfigAmneziaConf:
		return nativeawg.RenderVPNURL(config.Name, config.Config)
	case model.ConfigAmneziaVPN:
		if strings.HasPrefix(config.Config, "vpn://") {
			return config.Config, nil
		}
	}
	return "", errors.New("AmneziaVPN export is unavailable")
}
