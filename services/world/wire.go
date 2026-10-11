package world

import (
	"github.com/xenomit/server/pkg/protocol/legacy"
	"github.com/xenomit/server/pkg/protocol/v10"
)

func legacyText(text string) string { return legacy.Std(text) }

func v10Encode(message string) ([]byte, error) { return v10.EncodeLegacy(message) }
