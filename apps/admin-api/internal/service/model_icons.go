package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"strings"

	_ "golang.org/x/image/webp"
)

const (
	MaxModelIconBytes      = 1 << 20
	maxModelIconDimension  = 2048
	modelIconObjectPrefix  = "model-icons/"
	managedModelIconPrefix = "/api/model-icons/"
)

type ModelIconAsset struct {
	ID          string `json:"id"`
	Source      string `json:"src"`
	ContentType string `json:"content_type"`
}

func (a *Admin) SaveModelIcon(ctx context.Context, data []byte) (ModelIconAsset, error) {
	if a.modelIcons == nil || len(data) == 0 || len(data) > MaxModelIconBytes {
		return ModelIconAsset{}, ErrInvalidArg
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width <= 0 || config.Height <= 0 ||
		config.Width > maxModelIconDimension || config.Height > maxModelIconDimension {
		return ModelIconAsset{}, ErrInvalidArg
	}
	contentType := modelIconContentType(format)
	if contentType == "" {
		return ModelIconAsset{}, ErrInvalidArg
	}
	sum := sha256.Sum256(data)
	id := hex.EncodeToString(sum[:])
	if err := a.modelIcons.PutBytes(ctx, modelIconObjectPrefix+id, data, contentType); err != nil {
		return ModelIconAsset{}, err
	}
	return ModelIconAsset{
		ID:          id,
		Source:      managedModelIconPrefix + id,
		ContentType: contentType,
	}, nil
}

func (a *Admin) GetModelIcon(ctx context.Context, id string) ([]byte, string, error) {
	if a.modelIcons == nil || !validModelIconID(id) {
		return nil, "", ErrInvalidArg
	}
	return a.modelIcons.GetBytes(ctx, modelIconObjectPrefix+id)
}

func validManagedModelIconURL(value string) bool {
	if !strings.HasPrefix(value, managedModelIconPrefix) {
		return false
	}
	return validModelIconID(strings.TrimPrefix(value, managedModelIconPrefix))
}

func validModelIconID(id string) bool {
	if len(id) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}

func modelIconContentType(format string) string {
	switch format {
	case "jpeg":
		return "image/jpeg"
	case "png":
		return "image/png"
	case "webp":
		return "image/webp"
	default:
		return ""
	}
}
