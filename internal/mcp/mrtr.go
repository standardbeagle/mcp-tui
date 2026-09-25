package mcp

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/sync/errgroup"

	"github.com/standardbeagle/mcp-tui/internal/debug"
)

// Multi round-trip requests (SEP-2322, protocol 2026-07-28): a server that
// needs sampling, elicitation or roots while serving tools/call, prompts/get
// or resources/read returns InputRequests instead of calling the client; the
// client fulfills them and retries the call with InputResponses and the
// server's RequestState.
//
// The SDK can drive this loop itself, but it does so inside a client
// middleware that wraps every middleware mcp-tui can add, so no round is
// observable. mcp-tui therefore disables it (ClientOptions.MultiRoundTrip)
// and runs the loop here, logging every round. The loop and fulfillment
// mirror go-sdk v1.8.0 mcp/mrtr.go and client.go (createMessage, elicit,
// listRoots) exactly; service_mrtr_parity_test.go pins that behavior and
// TestMRTR_SDKVersionReviewed forces a review when the SDK moves.

// Round limits, identical to go-sdk v1.8.0 maxMultiRoundTripRetries and
// maxLoadSheddingMultiRoundTripRetries.
const (
	maxInputRounds        = 10
	maxLoadSheddingRounds = 3
)

// sendRound issues one round of a multi round-trip call with the responses
// and request state carried from the previous round (nil and "" on the
// first) and returns the server's input requests (nil for a final result)
// and new request state.
type sendRound func(
	ctx context.Context, responses officialMCP.InputResponseMap, state string,
) (officialMCP.InputRequestMap, string, error)

// runInputRounds drives a multi round-trip call until the server returns a
// final result, fulfilling each round's input requests with the handlers
// registered on the client, and returns a summary of every input round (nil
// when the server answered on the first try). method and target (tool/prompt
// name or resource URI) only label the log lines.
func (s *service) runInputRounds(
	ctx context.Context, session *officialMCP.ClientSession, method, target string, send sendRound,
) ([]RoundSummary, error) {
	var (
		responses    officialMCP.InputResponseMap
		state        string
		loadShedding int
		rounds       []RoundSummary
	)
	for round := 1; ; round++ {
		started := time.Now()
		requests, nextState, err := send(ctx, responses, state)
		if err != nil {
			return nil, err
		}
		if requests == nil {
			if round > 1 {
				debug.Debug("MRTR complete",
					debug.F("method", method), debug.F("target", target), debug.F("rounds", round))
			}
			return rounds, nil
		}
		debug.Debug("MRTR input required",
			debug.F("method", method),
			debug.F("target", target),
			debug.F("round", round),
			debug.F("input_requests", describeInputRequests(requests)),
			debug.F("has_request_state", nextState != ""),
			debug.F("load_shedding", len(requests) == 0))
		if len(requests) == 0 {
			loadShedding++
		}
		if loadShedding >= maxLoadSheddingRounds {
			return nil, fmt.Errorf("multi-round-trip: exceeded maximum load-shedding retries (%d)", maxLoadSheddingRounds)
		}
		if round >= maxInputRounds {
			return nil, fmt.Errorf("multi-round-trip: exceeded maximum retries (%d)", maxInputRounds)
		}
		responses, err = s.fulfillInputRequests(ctx, session, method, round, requests)
		if err != nil {
			return nil, err
		}
		state = nextState
		rounds = append(rounds, RoundSummary{
			Round:           round,
			Method:          method,
			InputRequests:   inputExchanges(requests, responses),
			HasRequestState: nextState != "",
			LoadShedding:    len(requests) == 0,
			DurationMs:      float64(time.Since(started).Microseconds()) / 1000,
		})
	}
}

// inputExchanges pairs each input request with the shape of its answer,
// sorted by key.
func inputExchanges(requests officialMCP.InputRequestMap, responses officialMCP.InputResponseMap) []InputExchange {
	out := make([]InputExchange, 0, len(requests))
	for key, request := range requests {
		out = append(out, InputExchange{
			Key:      key,
			Kind:     inputRequestKind(request),
			Response: describeInputResponse(responses[key]),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// fulfillInputRequests answers every input request of one round concurrently,
// as the SDK does.
func (s *service) fulfillInputRequests(
	ctx context.Context, session *officialMCP.ClientSession, method string, round int,
	requests officialMCP.InputRequestMap,
) (officialMCP.InputResponseMap, error) {
	g, gctx := errgroup.WithContext(ctx)
	var mu sync.Mutex
	responses := make(officialMCP.InputResponseMap, len(requests))
	for key, request := range requests {
		g.Go(func() error {
			response, err := s.fulfillInputRequest(gctx, session, request)
			if err != nil {
				debug.Warn("MRTR input failed",
					debug.F("method", method), debug.F("round", round), debug.F("key", key),
					debug.F("kind", inputRequestKind(request)), debug.F("error", err))
				return fmt.Errorf("fulfilling input request %q: %w", key, err)
			}
			debug.Debug("MRTR input fulfilled",
				debug.F("method", method), debug.F("round", round), debug.F("key", key),
				debug.F("kind", inputRequestKind(request)), debug.F("response", describeInputResponse(response)))
			mu.Lock()
			responses[key] = response
			mu.Unlock()
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, fmt.Errorf("multi round-trip: %w", err)
	}
	return responses, nil
}

// fulfillInputRequest mirrors go-sdk fulfillInputRequest.
func (s *service) fulfillInputRequest(
	ctx context.Context, session *officialMCP.ClientSession, request officialMCP.InputRequest,
) (officialMCP.InputResponse, error) {
	switch p := request.(type) {
	case *officialMCP.ElicitParams:
		return s.elicitForInput(ctx, &officialMCP.ElicitRequest{Session: session, Params: p})
	case *officialMCP.CreateMessageParams:
		return s.createMessageForInput(ctx, &officialMCP.CreateMessageWithToolsRequest{
			Session: session, Params: createMessageParamsToWithTools(p),
		})
	case *officialMCP.CreateMessageWithToolsParams:
		return s.createMessageForInput(ctx, &officialMCP.CreateMessageWithToolsRequest{Session: session, Params: p})
	case *officialMCP.ListRootsParams:
		return &officialMCP.ListRootsResult{Roots: s.rootsForInput()}, nil
	default:
		return nil, fmt.Errorf("unknown input request type: %T", request)
	}
}

// codeUnsupportedMethod is go-sdk's (unexported) error code for a client
// that lacks a handler.
const codeUnsupportedMethod = -31001

// createMessageForInput mirrors go-sdk Client.createMessage, calling the
// handlers createClient registered in ClientOptions.
func (s *service) createMessageForInput(
	ctx context.Context, req *officialMCP.CreateMessageWithToolsRequest,
) (*officialMCP.CreateMessageWithToolsResult, error) {
	opts := s.inputHandlers()
	if opts.CreateMessageWithToolsHandler != nil {
		return opts.CreateMessageWithToolsHandler(ctx, req)
	}
	if opts.CreateMessageHandler != nil {
		base, err := createMessageParamsToBase(req.Params)
		if err != nil {
			return nil, err
		}
		res, err := opts.CreateMessageHandler(ctx, &officialMCP.CreateMessageRequest{Session: req.Session, Params: base})
		if err != nil {
			return nil, err
		}
		return createMessageResultToWithTools(res), nil
	}
	return nil, &jsonrpc.Error{Code: codeUnsupportedMethod, Message: "client does not support CreateMessage"}
}

// rootsForInput mirrors go-sdk Client.listRoots: unique by URI (the last
// added wins, as in the SDK's feature set), sorted by URI, never nil.
func (s *service) rootsForInput() []*officialMCP.Root {
	byURI := map[string]*officialMCP.Root{}
	for _, r := range s.ListRoots() {
		if r != nil {
			byURI[r.URI] = r
		}
	}
	roots := make([]*officialMCP.Root, 0, len(byURI))
	for _, r := range byURI {
		roots = append(roots, r)
	}
	sort.Slice(roots, func(i, j int) bool { return roots[i].URI < roots[j].URI })
	return roots
}

// inputHandlers returns the ClientOptions of the current client.
func (s *service) inputHandlers() *officialMCP.ClientOptions {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.clientOptions == nil {
		return &officialMCP.ClientOptions{}
	}
	return s.clientOptions
}

// createMessageParamsToWithTools mirrors go-sdk createMessageParamsToWithTools.
func createMessageParamsToWithTools(p *officialMCP.CreateMessageParams) *officialMCP.CreateMessageWithToolsParams {
	msgs := make([]*officialMCP.SamplingMessageV2, 0, len(p.Messages))
	for _, m := range p.Messages {
		msgs = append(msgs, &officialMCP.SamplingMessageV2{Content: []officialMCP.Content{m.Content}, Role: m.Role})
	}
	return &officialMCP.CreateMessageWithToolsParams{
		Meta:             p.Meta,
		IncludeContext:   p.IncludeContext,
		MaxTokens:        p.MaxTokens,
		Messages:         msgs,
		Metadata:         p.Metadata,
		ModelPreferences: p.ModelPreferences,
		StopSequences:    p.StopSequences,
		SystemPrompt:     p.SystemPrompt,
		Temperature:      p.Temperature,
	}
}

// createMessageParamsToBase mirrors go-sdk CreateMessageWithToolsParams.toBase.
func createMessageParamsToBase(p *officialMCP.CreateMessageWithToolsParams) (*officialMCP.CreateMessageParams, error) {
	msgs := make([]*officialMCP.SamplingMessage, 0, len(p.Messages))
	for _, m := range p.Messages {
		if len(m.Content) > 1 {
			return nil, fmt.Errorf("message has %d content blocks; "+
				"use CreateMessageWithToolsHandler to support multiple content", len(m.Content))
		}
		var content officialMCP.Content
		if len(m.Content) > 0 {
			content = m.Content[0]
		}
		msgs = append(msgs, &officialMCP.SamplingMessage{Content: content, Role: m.Role})
	}
	return &officialMCP.CreateMessageParams{
		Meta:             p.Meta,
		IncludeContext:   p.IncludeContext,
		MaxTokens:        p.MaxTokens,
		Messages:         msgs,
		Metadata:         p.Metadata,
		ModelPreferences: p.ModelPreferences,
		StopSequences:    p.StopSequences,
		SystemPrompt:     p.SystemPrompt,
		Temperature:      p.Temperature,
	}, nil
}

// createMessageResultToWithTools mirrors go-sdk CreateMessageResult.toWithTools.
func createMessageResultToWithTools(r *officialMCP.CreateMessageResult) *officialMCP.CreateMessageWithToolsResult {
	var content []officialMCP.Content
	if r.Content != nil {
		content = []officialMCP.Content{r.Content}
	}
	return &officialMCP.CreateMessageWithToolsResult{
		Meta:       r.Meta,
		Content:    content,
		Model:      r.Model,
		Role:       r.Role,
		StopReason: r.StopReason,
	}
}

// inputRequestKind names what an input request asks the client for.
func inputRequestKind(request officialMCP.InputRequest) string {
	switch request.(type) {
	case *officialMCP.ElicitParams:
		return "elicitation"
	case *officialMCP.CreateMessageParams, *officialMCP.CreateMessageWithToolsParams:
		return "sampling"
	case *officialMCP.ListRootsParams:
		return "roots"
	default:
		return fmt.Sprintf("%T", request)
	}
}

// describeInputRequests renders a round's requests as sorted "key:kind".
func describeInputRequests(requests officialMCP.InputRequestMap) []string {
	out := make([]string, 0, len(requests))
	for key, request := range requests {
		out = append(out, key+":"+inputRequestKind(request))
	}
	sort.Strings(out)
	return out
}

// describeInputResponse summarizes what the client answered, without the
// answer's content: elicitation action, sampling stop reason and content
// kinds, or the number of roots.
func describeInputResponse(response officialMCP.InputResponse) string {
	switch r := response.(type) {
	case *officialMCP.ElicitResult:
		return r.Action
	case *officialMCP.CreateMessageWithToolsResult:
		kinds := make([]string, 0, len(r.Content))
		for _, c := range r.Content {
			kinds = append(kinds, strings.TrimPrefix(fmt.Sprintf("%T", c), "*mcp."))
		}
		return fmt.Sprintf("stop=%s content=%s", r.StopReason, strings.Join(kinds, ","))
	case *officialMCP.ListRootsResult:
		return fmt.Sprintf("%d roots", len(r.Roots))
	default:
		return fmt.Sprintf("%T", response)
	}
}

// RoundLines renders a round trace for people, one header line per round
// and one indented line per input request. nil when there were no rounds,
// so callers show no section at all.
func RoundLines(rounds []RoundSummary) []string {
	if len(rounds) == 0 {
		return nil
	}
	var lines []string
	for _, r := range rounds {
		state := "no"
		if r.HasRequestState {
			state = "yes"
		}
		header := fmt.Sprintf("round %d · %s · %.1fms · request state: %s", r.Round, r.Method, r.DurationMs, state)
		if r.LoadShedding {
			header += " · load shedding (retry)"
		}
		lines = append(lines, header)
		for _, x := range r.InputRequests {
			lines = append(lines, fmt.Sprintf("  %s: %s → %s", x.Key, x.Kind, x.Response))
		}
	}
	return lines
}
