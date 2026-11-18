package assistant

import (
	"cmp"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// What a gateway says of a model beyond its id, name and window (#8778, docs/integrations/launchers.md "Connection
// tests and model discovery"): its thinking levels, or that it takes none; its output limit; whether it takes images
// and tools; and whether it is a chat model at all. Everything is read by the shape of the fields, never by which
// gateway answered, in the spellings the common model lists use (cindy's parseModelsListResponse reads the same).

// capabilitiesTimeout bounds GET /v1/model-capabilities, which only adds to a list already read (a var for the tests).
var capabilitiesTimeout = 5 * time.Second

// Fields naming a model's kind, task or mode, and the values that say it is no chat model: an image, video, audio,
// embedding, rerank or moderation model (magpie's kind, Vercel's type, LiteLLM's mode).
var (
	modelKindFields = []string{"kind", "type", "mode", "model_type", "task"}
	nonChatKinds    = []string{"image", "images", "image_generation", "image-generation", "video", "video_generation",
		"video-generation", "embedding", "embeddings", "rerank", "reranker", "reranking", "moderation", "moderations",
		"audio", "audio_speech", "audio_transcription", "speech", "tts", "stt", "transcription"}
	// new-api's supported_endpoint_types that are no chat API: a model with only these is no chat model.
	nonChatEndpoints = []string{"image-generation", "image-edit", "embeddings", "embedding", "jina-rerank", "rerank",
		"openai-video", "video", "audio", "tts", "moderation"}
	// supported_parameters entries that say a model takes a thinking level (OpenRouter's, magpie's).
	reasoningParameters = []string{"reasoning", "reasoning_effort", "reasoning_budget", "include_reasoning", "thinking"}
	// supported_parameters entries every chat model takes: a list naming one is the model's whole parameter list
	// (OpenRouter's), so what it leaves out it doesn't take; a list naming none may be a few capabilities picked out.
	samplingParameters = []string{"temperature", "top_p", "top_k", "max_tokens", "max_completion_tokens", "stop", "seed"}
)

// readModelMeta reads what one entry of a model list or of a capability report says beyond id, name and window into
// m: the thinking levels in the gateway's words (m.AllEfforts, the default in m.DefaultEffort, an explicit none as
// m.NoEffort; settleDiscoveredEfforts projects them), the output limit, image input and tool calls. chat is false when
// the entry marks a model that is no chat model; guessed is true when its parameter list only suggests it takes no
// level (reportedEfforts), which discovery applies last and only to a model nothing else said levels for
// (guessNoEffort).
// A null, or a list with an entry of a shape it can't read, says nothing (rawBool, rawStrings).
func readModelMeta(fields map[string]json.RawMessage, m *proto.ProviderModel) (chat, guessed bool) {
	for _, k := range modelKindFields {
		if v := strings.ToLower(rawString(fields[k])); v != "" && slices.Contains(nonChatKinds, v) {
			return false, false
		}
	}
	modalities := rawObject(fields["modalities"])
	architecture := rawObject(fields["architecture"])
	if out, ok := firstStrings(modalities["output"], fields["output_modalities"], architecture["output_modalities"]); ok &&
		!slices.Contains(out, "text") {
		return false, false
	}
	if ends, ok := rawStrings(fields["supported_endpoint_types"]); ok && len(ends) > 0 &&
		!slices.ContainsFunc(ends, func(e string) bool { return !slices.Contains(nonChatEndpoints, e) }) {
		return false, false
	}
	m.AllEfforts, m.NoEffort, guessed, m.DefaultEffort = reportedEfforts(fields)
	limit := rawObject(fields["limit"])
	top := rawObject(fields["top_provider"])
	m.MaxOutputTokens = firstTokens(fields["max_output_tokens"], fields["max_completion_tokens"], fields["output_token_limit"],
		fields["outputTokenLimit"], fields["maxOutputTokens"], limit["output"], top["max_completion_tokens"])
	capabilities := rawObject(fields["capabilities"])
	m.ImageInput = firstBool(fields["supports_image_input"], fields["supports_vision"], fields["vision"],
		capabilities["vision"], capabilities["image_input"], rawObject(capabilities["image_input"])["supported"])
	if m.ImageInput == nil {
		if in, ok := firstStrings(modalities["input"], fields["input_modalities"], architecture["input_modalities"]); ok {
			m.ImageInput = boolPtr(slices.Contains(in, "image"))
		}
	}
	params, _ := rawStrings(fields["supported_parameters"])
	m.ToolCalls = firstBool(fields["tool_call"], fields["tool_calls"], fields["supports_tool_calls"], fields["supports_tools"],
		fields["supports_function_calling"], fields["function_calling"], capabilities["tool_use"], capabilities["function_calling"])
	if m.ToolCalls == nil && (slices.Contains(params, "tools") || fullParameters(params)) {
		m.ToolCalls = boolPtr(slices.Contains(params, "tools"))
	}
	return true, guessed
}

// fullParameters: a supported_parameters list is the model's whole parameter list (samplingParameters), so an entry
// it leaves out is one the model doesn't take.
func fullParameters(params []string) bool {
	return slices.ContainsFunc(params, func(p string) bool { return slices.Contains(samplingParameters, p) })
}

// reportedEfforts is what an entry says of its thinking levels: the levels in EffortWords' words (anything else
// dropped; a list naming none of them says nothing), or none (a top-level list that is empty or names only none, a
// reasoning object whose status is unsupported, reasoning false); and the default level it names, if any. A top-level
// list wins over the signals of none, the first list in the order below over the others. A reasoning object's levels
// count only when it names some and its status is neither unknown nor unsupported (a capability report's
// {"status","allowed","default"}). Last, a whole parameter list (fullParameters) without a reasoning parameter
// suggests none (guessed, not none: guessNoEffort decides once everything else is read), unless reasoning is said to
// be true.
func reportedEfforts(fields map[string]json.RawMessage) (levels []string, none, guessed bool, def string) {
	reasoning := rawObject(fields["reasoning"])
	def = effortWord(cmp.Or(rawString(fields["default_reasoning_level"]), rawString(fields["default_reasoning_effort"]),
		rawString(fields["default_effort"]), rawString(reasoning["default"]), rawString(reasoning["default_effort"])))
	if def == "none" {
		def = ""
	}
	status := strings.ToLower(rawString(reasoning["status"]))
	for _, raw := range []json.RawMessage{fields["reasoning_levels"], fields["supported_reasoning_levels"],
		fields["supported_efforts"], fields["reasoning_efforts"], fields["efforts"]} {
		if words, ok := rawStrings(raw); ok {
			if levels, none, said := effortList(words); said {
				return levels, none, false, def
			}
		}
	}
	if status == "unsupported" {
		return nil, true, false, ""
	}
	if status != "unknown" {
		for _, raw := range []json.RawMessage{reasoning["allowed"], reasoning["supported_efforts"], reasoning["efforts"], reasoning["levels"]} {
			if words, ok := rawStrings(raw); ok && len(words) > 0 {
				if levels, none, said := effortList(words); said {
					return levels, none, false, def
				}
			}
		}
	}
	thinks := false // said to reason: supported_parameters then says nothing of levels
	for _, raw := range []json.RawMessage{fields["reasoning"], fields["supports_reasoning"], fields["supports_reasoning_effort"]} {
		if b, ok := rawBool(raw); ok && !b {
			return nil, true, false, ""
		} else if ok {
			thinks = true
		}
	}
	if params, _ := rawStrings(fields["supported_parameters"]); !thinks && fullParameters(params) &&
		!slices.ContainsFunc(params, func(p string) bool { return slices.Contains(reasoningParameters, p) }) {
		return nil, false, true, def
	}
	return nil, false, false, def
}

// effortList reads one list of levels: said is false when it names only words outside EffortWords (a vendor's own),
// none when it is empty or names none alone. Beside other levels none stays: a direct API can send it.
func effortList(words []string) (levels []string, none, said bool) {
	known := knownEfforts(words)
	if len(words) > 0 && len(known) == 0 {
		return nil, false, false
	}
	if !slices.ContainsFunc(known, func(e string) bool { return e != "none" }) {
		return nil, true, true
	}
	return known, false, true
}

// knownEfforts is words lowered, those in EffortWords only, each once, weakest first.
func knownEfforts(words []string) []string {
	var out []string
	for _, w := range proto.EffortWords {
		if slices.ContainsFunc(words, func(x string) bool { return effortWord(x) == w }) {
			out = append(out, w)
		}
	}
	return out
}

// effortWord is s as an EffortWords level, "" when it is none of them.
func effortWord(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if slices.Contains(proto.EffortWords, s) {
		return s
	}
	return ""
}

// guessNoEffort settles a model whose parameter list suggested it takes no level (readModelMeta's guessed), once its
// repeated entries and the capability report are read: it takes none only if none of them said anything of its levels
// and the built-in catalog knows no levels for it (exact or inherited). A wrong "none" costs more than "unknown": the
// launch paths then send no thinking level at all.
func guessNoEffort(m proto.ProviderModel) proto.ProviderModel {
	if reportedLevels(m) {
		return m
	}
	if c, ok := catalogModel(m.ID); ok && len(c.Efforts) > 0 {
		return m
	}
	m.NoEffort = true
	return m
}

// reportedLevels: m says something of its thinking levels, either levels or none.
func reportedLevels(m proto.ProviderModel) bool { return len(m.AllEfforts) > 0 || m.NoEffort }

// settleDiscoveredEfforts turns the gateway's words read into m (readModelMeta) into what discovery answers: Efforts
// as Claude Code takes them (proto.ProjectEfforts), AllEfforts only when the gateway named a level outside those, the
// default as Claude Code takes it and only when it is one of Efforts; a model that takes none keeps no level at all.
func settleDiscoveredEfforts(m proto.ProviderModel) proto.ProviderModel {
	levels := m.AllEfforts
	m.AllEfforts, m.Efforts = nil, nil
	if m.NoEffort {
		m.DefaultEffort = ""
		return m
	}
	if len(levels) > 0 {
		m.Efforts = proto.ProjectEfforts(levels, proto.EffortLevels)
		if slices.ContainsFunc(levels, func(e string) bool { return !slices.Contains(proto.EffortLevels, e) }) {
			m.AllEfforts = levels
		}
	}
	m.DefaultEffort = projectedDefault(m.DefaultEffort, m.Efforts)
	return m
}

// projectedDefault is def as Claude Code takes it, "" unless it is one of efforts (any, while those are unknown).
func projectedDefault(def string, efforts []string) string {
	p := proto.ProjectEfforts([]string{def}, proto.EffortLevels)
	if len(p) == 0 || len(efforts) > 0 && !slices.Contains(efforts, p[0]) {
		return ""
	}
	return p[0]
}

// providerEffortVocab is the thinking levels a provider's agent or API takes: a direct API connection none and
// Claude Code's (purposes send "off" / "none" as no thinking; ai.ApplyReasoningEffort), a Codex launcher Codex's
// (proto.CodexEffortLevels), every other launcher Claude Code's (proto.EffortLevels).
func providerEffortVocab(kind, agent string) []string {
	switch {
	case kind == "api":
		return append([]string{"none"}, proto.EffortLevels...)
	case agent == "codex":
		return proto.CodexEffortLevels
	}
	return proto.EffortLevels
}

// settleSavedEfforts keeps m's levels in the words vocab takes (providerEffortVocab) as a provider is saved:
// AllEfforts only while it adds a level to Efforts and still agrees with them (levels changed by hand leave Efforts the
// truth); NoEffort only while no level is chosen, a default level included (a level chosen undoes the none).
func settleSavedEfforts(m proto.ProviderModel, vocab []string) proto.ProviderModel {
	if len(m.Efforts) > 0 || m.DefaultEffort != "" {
		m.NoEffort = false
	}
	if m.NoEffort {
		m.AllEfforts, m.DefaultEffort = nil, ""
		return m
	}
	all := proto.ProjectEfforts(knownEfforts(m.AllEfforts), vocab)
	if !slices.ContainsFunc(all, func(e string) bool { return !slices.Contains(proto.EffortLevels, e) }) ||
		!slices.Equal(proto.ProjectEfforts(all, proto.EffortLevels), proto.ProjectEfforts(m.Efforts, proto.EffortLevels)) {
		all = nil
	}
	m.AllEfforts = all
	return m
}

// reportedKeys are the ProviderModel fields only a gateway's report fills in.
var reportedKeys = []string{"max_output_tokens", "all_efforts", "no_effort", "image_input", "tool_calls"}

// keepReported is m as a client sends it, with what the gateway reported of it kept from the stored models when the
// client named none of reportedKeys for it (named: launcherInput.modelsNamed): an app from before these fields (the
// iOS and Android editors) resends a model without them, while the web names them for every model, so what it leaves
// out is cleared. settleSavedEfforts then drops what no longer agrees with the levels sent.
func keepReported(m proto.ProviderModel, stored []proto.ProviderModel, named bool) proto.ProviderModel {
	if named || reportedLevels(m) || m.MaxOutputTokens != 0 || m.ImageInput != nil || m.ToolCalls != nil {
		return m
	}
	if i := slices.IndexFunc(stored, func(o proto.ProviderModel) bool { return o.ID == m.ID }); i >= 0 {
		o := stored[i]
		m.MaxOutputTokens, m.AllEfforts, m.NoEffort, m.ImageInput, m.ToolCalls = o.MaxOutputTokens, o.AllEfforts, o.NoEffort, o.ImageInput, o.ToolCalls
	}
	return m
}

// mergeModelMeta fills what first leaves unsaid from next (a repeated entry, the capability report). Levels taken from
// next bring next's default unless first's is one of them.
func mergeModelMeta(first, next proto.ProviderModel) proto.ProviderModel {
	if !reportedLevels(first) && reportedLevels(next) {
		first.AllEfforts, first.NoEffort = next.AllEfforts, next.NoEffort
		if next.DefaultEffort != "" && !slices.Contains(next.AllEfforts, first.DefaultEffort) {
			first.DefaultEffort = next.DefaultEffort
		}
	}
	if first.DefaultEffort == "" && !first.NoEffort {
		first.DefaultEffort = next.DefaultEffort
	}
	if first.MaxOutputTokens == 0 {
		first.MaxOutputTokens = next.MaxOutputTokens
	}
	if first.ImageInput == nil {
		first.ImageInput = next.ImageInput
	}
	if first.ToolCalls == nil {
		first.ToolCalls = next.ToolCalls
	}
	return first
}

// capabilityProtocol is the protocol a gateway's capability report is asked about: the one the provider's
// requests go on (a Codex launcher's Responses whatever its stored api).
func (t probeTarget) capabilityProtocol() string {
	switch {
	case t.agent == "codex" || t.api == "responses":
		return "responses"
	case t.api == "chat":
		return "chat_completions"
	}
	return "messages"
}

// applyCapabilities adds a gateway's capability report (GET <base>/v1/model-capabilities?protocol=…, {"data":
// [{"model_id","protocols":[…],"reasoning":{"status","allowed","default"}}]}) to models read from its list: it fills
// what the list left unsaid, and a model it lists with no protocol (none it can be reached on with the provider's
// protocol) is left out. The report only adds: a gateway without one (404), any failure, an answer it can't read or
// one past capabilitiesTimeout leaves the list as it was. Not asked when the list is empty, or read from an endpoint
// other than the base's (DeepSeek's documented /models), where there is no such report.
func (t *probeTarget) applyCapabilities(ctx context.Context, models []proto.ProviderModel, guessed map[string]bool) []proto.ProviderModel {
	if len(models) == 0 || t.modelsEndpoint() != t.endpoint("models") {
		return models
	}
	ctx, cancel := context.WithTimeout(ctx, capabilitiesTimeout)
	defer cancel()
	res, err := t.send(ctx, http.MethodGet, t.endpoint("model-capabilities")+"?"+url.Values{"protocol": {t.capabilityProtocol()}}.Encode(), nil)
	if err != nil {
		return models
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		return models
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return models
	}
	var report struct {
		Data []map[string]json.RawMessage `json:"data"`
	}
	if json.Unmarshal(raw, &report) != nil {
		return models
	}
	byID := map[string]proto.ProviderModel{}
	unreachable := map[string]bool{}
	for _, fields := range report.Data {
		id := strings.TrimSpace(cmp.Or(rawString(fields["model_id"]), rawString(fields["id"])))
		if id == "" {
			continue
		}
		if _, dup := byID[id]; dup || unreachable[id] {
			continue // the first entry for an id is its answer
		}
		var m proto.ProviderModel
		// protocols: [] is the report saying so; a list it can't read says nothing (rawStrings)
		if protocols, ok := rawStrings(fields["protocols"]); ok && len(protocols) == 0 {
			unreachable[id] = true
			continue
		}
		chat, guess := readModelMeta(fields, &m)
		if !chat {
			unreachable[id] = true
			continue
		}
		if guess {
			guessed[id] = true
		}
		byID[id] = m
	}
	out := models[:0:0]
	for _, m := range models {
		if unreachable[m.ID] {
			continue
		}
		if c, ok := byID[m.ID]; ok {
			m = mergeModelMeta(m, c)
		}
		out = append(out, m)
	}
	return out
}

// rawString is raw as a string, trimmed; "" when it is not one.
func rawString(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return strings.TrimSpace(s)
}

// rawBool is raw as a bool, ok false when it is not one (null included: a field left empty says nothing).
func rawBool(raw json.RawMessage) (b, ok bool) {
	var v *bool
	if len(raw) == 0 || json.Unmarshal(raw, &v) != nil || v == nil {
		return false, false
	}
	return *v, true
}

// rawObject is raw as an object's fields, nil when it is not one.
func rawObject(raw json.RawMessage) map[string]json.RawMessage {
	var o map[string]json.RawMessage
	if len(raw) == 0 || json.Unmarshal(raw, &o) != nil {
		return nil
	}
	return o
}

// rawStrings is raw as a list of words, lowered and trimmed: strings, or objects naming one in effort, value, id or
// name ({"effort":"low"}, Codex's supported_reasoning_levels); a blank string is skipped. ok is false when raw is no
// list (null included), when an entry is of another shape, and when it has entries but only blank ones: a list read
// only in part says nothing, rather than less than it holds.
func rawStrings(raw json.RawMessage) (out []string, ok bool) {
	var list []json.RawMessage
	if len(raw) == 0 || json.Unmarshal(raw, &list) != nil || list == nil {
		return nil, false
	}
	for _, item := range list {
		var s string
		if json.Unmarshal(item, &s) == nil && strings.TrimSpace(s) == "" && string(item) != "null" {
			continue // "" or blanks
		}
		s = rawString(item)
		if s == "" {
			o := rawObject(item)
			s = cmp.Or(rawString(o["effort"]), rawString(o["value"]), rawString(o["id"]), rawString(o["name"]))
		}
		if s == "" {
			return nil, false
		}
		out = append(out, strings.ToLower(s))
	}
	if len(list) > 0 && len(out) == 0 {
		return nil, false
	}
	return out, true
}

// firstStrings is the first of raws that is a list of some words: an empty list, like one it can't read, is one left
// unfilled (modalities, input kinds), so the next spelling is read.
func firstStrings(raws ...json.RawMessage) ([]string, bool) {
	for _, raw := range raws {
		if out, ok := rawStrings(raw); ok && len(out) > 0 {
			return out, true
		}
	}
	return nil, false
}

// firstBool is the first of raws that is a bool, nil when none is.
func firstBool(raws ...json.RawMessage) *bool {
	for _, raw := range raws {
		if b, ok := rawBool(raw); ok {
			return boolPtr(b)
		}
	}
	return nil
}

// firstTokens is the first of raws that is a token count (above 0, at most 1e9), 0 when none is.
func firstTokens(raws ...json.RawMessage) int {
	for _, raw := range raws {
		var f float64
		if len(raw) > 0 && json.Unmarshal(raw, &f) == nil && f > 0 && f <= 1e9 {
			return int(f)
		}
	}
	return 0
}

func boolPtr(b bool) *bool { return &b }
