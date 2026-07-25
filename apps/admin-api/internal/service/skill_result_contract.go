package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const maxSkillResultSchemaBytes = 64 << 10

type skillResultContract struct {
	Version      int            `json:"version"`
	Renderer     string         `json:"renderer"`
	Schema       map[string]any `json:"schema"`
	ContractHash string         `json:"contract_hash"`
}

func normalizedSkillResultContractJSON(frontmatter json.RawMessage) (json.RawMessage, error) {
	if len(frontmatter) == 0 || string(frontmatter) == "{}" || string(frontmatter) == "null" {
		return nil, nil
	}
	var root map[string]any
	if err := json.Unmarshal(frontmatter, &root); err != nil {
		return nil, fmt.Errorf("frontmatter must be a JSON object")
	}
	cocola, _ := root["cocola"].(map[string]any)
	if cocola == nil {
		return nil, nil
	}
	rawResult, exists := cocola["result"]
	if !exists {
		return nil, nil
	}
	result, ok := rawResult.(map[string]any)
	if !ok {
		return nil, errors.New("frontmatter.cocola.result must be an object")
	}
	versionNumber, ok := result["version"].(float64)
	if !ok || int(versionNumber) != 1 || versionNumber != 1 {
		return nil, errors.New("frontmatter.cocola.result.version must be 1")
	}
	renderer, _ := result["renderer"].(string)
	renderer = strings.TrimSpace(renderer)
	switch renderer {
	case "summary", "table", "list", "metrics":
	default:
		return nil, errors.New("frontmatter.cocola.result.renderer is unsupported")
	}
	schema, ok := result["schema"].(map[string]any)
	if !ok || len(schema) == 0 {
		return nil, errors.New("frontmatter.cocola.result.schema must be a non-empty object")
	}
	if value, _ := schema["type"].(string); value != "object" {
		return nil, errors.New("frontmatter.cocola.result.schema.type must be object")
	}
	schemaJSON, err := json.Marshal(schema)
	if err != nil || len(schemaJSON) > maxSkillResultSchemaBytes {
		return nil, errors.New("frontmatter.cocola.result.schema exceeds 64 KiB")
	}
	if hasRemoteSchemaRef(schema) {
		return nil, errors.New("frontmatter.cocola.result.schema cannot use remote $ref")
	}
	hashInput, err := json.Marshal(map[string]any{
		"version": 1, "renderer": renderer, "schema": schema,
	})
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(hashInput)
	contract := skillResultContract{
		Version: 1, Renderer: renderer, Schema: schema,
		ContractHash: "sha256:" + hex.EncodeToString(digest[:]),
	}
	return json.Marshal(contract)
}

func hasRemoteSchemaRef(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			if key == "$ref" {
				ref, _ := item.(string)
				if ref != "" && !strings.HasPrefix(ref, "#") {
					return true
				}
			}
			if hasRemoteSchemaRef(item) {
				return true
			}
		}
	case []any:
		for _, item := range typed {
			if hasRemoteSchemaRef(item) {
				return true
			}
		}
	}
	return false
}
