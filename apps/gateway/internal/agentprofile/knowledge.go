package agentprofile

import (
	"net/url"
	"path"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	KnowledgeTypeFeishuDoc   = "feishu_doc"
	KnowledgeTypeFeishuWiki  = "feishu_wiki"
	KnowledgeTypeFeishuSheet = "feishu_sheet"
	KnowledgeTypeFeishuBase  = "feishu_base"
	KnowledgeTypeCocolaWiki  = "cocola_wiki"
)

var knowledgeTokenPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func NormalizeKnowledgeSource(source KnowledgeSource) (KnowledgeSource, bool) {
	source.Type = strings.TrimSpace(source.Type)
	source.Label = strings.TrimSpace(source.Label)
	source.URL = strings.TrimSpace(source.URL)
	source.NodeID = strings.TrimSpace(source.NodeID)
	if len(source.URL) > MaxKnowledgeURLChars ||
		utf8.RuneCountInString(source.Label) > MaxKnowledgeLabelChars ||
		strings.ContainsAny(source.Label, "\x00\r\n") {
		return KnowledgeSource{}, false
	}
	if source.Type == KnowledgeTypeCocolaWiki ||
		(source.Type == "" && source.URL == "" && source.NodeID != "") {
		parsedNodeID, err := uuid.Parse(source.NodeID)
		if err != nil || source.URL != "" {
			return KnowledgeSource{}, false
		}
		source.Type = KnowledgeTypeCocolaWiki
		source.NodeID = parsedNodeID.String()
		if source.Label == "" {
			source.Label = defaultKnowledgeLabel(source.Type)
		}
		return source, true
	}
	if source.NodeID != "" {
		return KnowledgeSource{}, false
	}
	parsed, err := url.Parse(source.URL)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil ||
		parsed.Port() != "" || !allowedKnowledgeHost(parsed.Hostname()) {
		return KnowledgeSource{}, false
	}
	segments := strings.Split(strings.Trim(path.Clean(parsed.Path), "/"), "/")
	if len(segments) < 2 {
		return KnowledgeSource{}, false
	}
	inferred, ok := knowledgeTypeForPath(segments[0])
	if !ok || (source.Type != "" && source.Type != inferred) {
		return KnowledgeSource{}, false
	}
	token := segments[1]
	if len(token) > 256 || !knowledgeTokenPattern.MatchString(token) {
		return KnowledgeSource{}, false
	}
	parsed.Scheme = "https"
	parsed.Host = strings.ToLower(parsed.Hostname())
	parsed.Path = "/" + segments[0] + "/" + token
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	source.Type = inferred
	source.URL = parsed.String()
	if source.Label == "" {
		source.Label = defaultKnowledgeLabel(inferred)
	}
	return source, true
}

func KnowledgeSourceKey(source KnowledgeSource) string {
	switch source.Type {
	case KnowledgeTypeCocolaWiki:
		return source.Type + ":" + source.NodeID
	default:
		return source.Type + ":" + source.URL
	}
}

func RequiredKnowledgeSkillIDs(sourceType string) []string {
	switch sourceType {
	case KnowledgeTypeFeishuDoc:
		return []string{"lark-doc"}
	case KnowledgeTypeFeishuWiki:
		return []string{"lark-wiki", "lark-doc"}
	case KnowledgeTypeFeishuSheet:
		return []string{"lark-sheets"}
	case KnowledgeTypeFeishuBase:
		return []string{"lark-base"}
	case KnowledgeTypeCocolaWiki:
		return []string{}
	default:
		return nil
	}
}

func allowedKnowledgeHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	for _, suffix := range []string{"feishu.cn", "larkoffice.com", "larksuite.com"} {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return true
		}
	}
	return false
}

func knowledgeTypeForPath(value string) (string, bool) {
	switch strings.ToLower(value) {
	case "docx":
		return KnowledgeTypeFeishuDoc, true
	case "wiki":
		return KnowledgeTypeFeishuWiki, true
	case "sheets":
		return KnowledgeTypeFeishuSheet, true
	case "base", "bitable":
		return KnowledgeTypeFeishuBase, true
	default:
		return "", false
	}
}

func defaultKnowledgeLabel(sourceType string) string {
	switch sourceType {
	case KnowledgeTypeFeishuDoc:
		return "Feishu document"
	case KnowledgeTypeFeishuWiki:
		return "Feishu Wiki"
	case KnowledgeTypeFeishuSheet:
		return "Feishu Sheet"
	case KnowledgeTypeFeishuBase:
		return "Feishu Base"
	case KnowledgeTypeCocolaWiki:
		return "Cocola Wiki file"
	default:
		return "Knowledge source"
	}
}
