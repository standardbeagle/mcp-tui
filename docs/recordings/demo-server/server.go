package main

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	serverName    = "acme-support-desk"
	serverVersion = "1.4.0"

	kbURI             = "acme://kb/getting-started.md"
	slaURI            = "acme://config/sla.json"
	logoURI           = "acme://brand/logo.png"
	queueURI          = "acme://status/queue"
	ticketTemplateURI = "acme://tickets/{id}"
	ticketURIPrefix   = "acme://tickets/"

	// queueUpdateInterval is how often the live queue changes while anyone
	// is subscribed to it.
	queueUpdateInterval = 3 * time.Second
)

// liveQueue is the subscribable queue resource: every update advances it
// one step through queueSnapshots.
type liveQueue struct {
	updates atomic.Int64
}

func (q *liveQueue) snapshot() string {
	n := q.updates.Load()
	return fmt.Sprintf("update %d  %s", n, queueSnapshots[n%int64(len(queueSnapshots))])
}

// run advances the queue every queueUpdateInterval and notifies subscribers
// until ctx ends. The SDK sends the notification only to subscribed sessions.
func (q *liveQueue) run(ctx context.Context, server *mcp.Server, logger *slog.Logger) {
	ticker := time.NewTicker(queueUpdateInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			q.updates.Add(1)
			if err := server.ResourceUpdated(ctx, &mcp.ResourceUpdatedNotificationParams{URI: queueURI}); err != nil {
				logger.Warn("queue update notification failed", "error", err)
			}
		}
	}
}

// newDeskServer builds the Acme support desk MCP server. With misbehave set
// it breaks three rules that `mcp-tui verify` checks (see README.md).
func newDeskServer(queue *liveQueue, misbehave bool, logger *slog.Logger) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{
		Name:        serverName,
		Title:       "Acme Support Desk",
		Description: "Demo help desk for recording mcp-tui: tickets, customers and a live queue.",
		Version:     serverVersion,
		WebsiteURL:  "https://acme.example/support",
		Icons:       []mcp.Icon{{Source: logoDataURI(), MIMEType: "image/png", Sizes: []string{"32x32"}}},
	}, &mcp.ServerOptions{
		Instructions: "Acme support desk. Start with search_tickets, look people up with lookup_customer, " +
			"and read acme://kb/getting-started.md for how the desk works.",
		Logger:             logger,
		CompletionHandler:  completeArgument,
		SubscribeHandler:   subscribeToQueue,
		UnsubscribeHandler: func(context.Context, *mcp.UnsubscribeRequest) error { return nil },
	})
	registerTools(server, misbehave, logger)
	registerResources(server, queue)
	registerPrompts(server)
	if misbehave {
		server.AddReceivingMiddleware(reverseEveryOtherToolsList())
	}
	return server
}

// reverseEveryOtherToolsList deliberately breaks the 2026-07-28 SHOULD that
// tools/list keep a stable order: every second answer comes back reversed.
func reverseEveryOtherToolsList() mcp.Middleware {
	var lists atomic.Int64
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			res, err := next(ctx, method, req)
			if method != "tools/list" || err != nil || lists.Add(1)%2 == 1 {
				return res, err
			}
			listed, ok := res.(*mcp.ListToolsResult)
			if !ok {
				return nil, fmt.Errorf("tools/list returned %T", res)
			}
			reversed := *listed
			reversed.Tools = slices.Clone(listed.Tools)
			slices.Reverse(reversed.Tools)
			return &reversed, nil
		}
	}
}

func subscribeToQueue(_ context.Context, req *mcp.SubscribeRequest) error {
	if req.Params.URI != queueURI {
		return fmt.Errorf("only %s changes; %s cannot be subscribed to", queueURI, req.Params.URI)
	}
	return nil
}

func registerResources(server *mcp.Server, queue *liveQueue) {
	server.AddResource(&mcp.Resource{
		URI: kbURI, Name: "getting-started", Title: "Getting started with the desk",
		Description: "How tickets move through the desk and which tool to reach for first.",
		MIMEType:    "text/markdown", Size: int64(len(gettingStartedMarkdown)),
	}, staticText(kbURI, "text/markdown", gettingStartedMarkdown))

	server.AddResource(&mcp.Resource{
		URI: slaURI, Name: "sla", Title: "Service levels",
		Description: "First-response and resolution targets per plan.",
		MIMEType:    "application/json", Size: int64(len(slaJSON)),
	}, staticText(slaURI, "application/json", slaJSON))

	server.AddResource(&mcp.Resource{
		URI: logoURI, Name: "logo", Title: "Acme logo",
		Description: "32x32 PNG brand mark.",
		MIMEType:    "image/png", Size: int64(len(logoPNG())),
	}, func(context.Context, *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: logoURI, MIMEType: "image/png", Blob: logoPNG()}}}, nil
	})

	server.AddResource(&mcp.Resource{
		URI: queueURI, Name: "queue", Title: "Live queue",
		Description: fmt.Sprintf("Queue counters; changes every %s while subscribed.", queueUpdateInterval),
		MIMEType:    "text/plain",
	}, func(context.Context, *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: queueURI, MIMEType: "text/plain", Text: queue.snapshot()}}}, nil
	})

	server.AddResourceTemplate(&mcp.ResourceTemplate{
		URITemplate: ticketTemplateURI, Name: "ticket", Title: "Ticket by ID",
		Description: "One ticket as JSON, e.g. acme://tickets/T-1041.",
		MIMEType:    "application/json",
	}, readTicket)
}

func staticText(uri, mimeType, text string) mcp.ResourceHandler {
	return func(context.Context, *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: uri, MIMEType: mimeType, Text: text}}}, nil
	}
}

func readTicket(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	uri := req.Params.URI
	t, ok := findTicket(strings.TrimPrefix(uri, ticketURIPrefix))
	if !ok {
		return nil, mcp.ResourceNotFoundError(uri)
	}
	body, err := marshalIndented(t)
	if err != nil {
		return nil, err
	}
	return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: uri, MIMEType: "application/json", Text: body}}}, nil
}

func registerPrompts(server *mcp.Server) {
	server.AddPrompt(&mcp.Prompt{
		Name: "triage_ticket", Title: "Triage a ticket",
		Description: "Decide priority, owner and next step for a ticket, and draft the first reply.",
		Arguments: []*mcp.PromptArgument{
			{Name: "ticket_id", Title: "Ticket ID", Description: "e.g. T-1041", Required: true},
			{Name: "tone", Title: "Reply tone", Description: "friendly (default), formal or apologetic"},
		},
	}, triageTicketPrompt)

	server.AddPrompt(&mcp.Prompt{
		Name: "weekly_summary", Title: "Weekly summary",
		Description: "Summarize this week's queue for the team channel.",
	}, weeklySummaryPrompt)
}

func triageTicketPrompt(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	id := req.Params.Arguments["ticket_id"]
	t, ok := findTicket(id)
	if !ok {
		return nil, fmt.Errorf("no ticket %q", id)
	}
	c, ok := findCustomerByID(t.CustomerID)
	if !ok {
		return nil, fmt.Errorf("ticket %s names unknown customer %s", t.ID, t.CustomerID)
	}
	tone := req.Params.Arguments["tone"]
	if tone == "" {
		tone = replyTones[0]
	}
	if !slices.Contains(replyTones, tone) {
		return nil, fmt.Errorf("tone %q is not one of %s", tone, strings.Join(replyTones, ", "))
	}
	return &mcp.GetPromptResult{
		Description: "Triage " + t.ID,
		Messages: []*mcp.PromptMessage{
			{Role: "user", Content: &mcp.TextContent{Text: fmt.Sprintf(
				"Triage ticket %s for %s (%s, %s plan).\n\nSubject: %s\nStatus: %s  Priority: %s  Tags: %s\n\n%s\n\n"+
					"Decide the priority, which team owns it and the next step, then draft the first reply in a %s tone. "+
					"Keep within the service levels below.",
				t.ID, c.Name, c.Company, c.Plan, t.Subject, t.Status, t.Priority, strings.Join(t.Tags, ", "), t.Body, tone)}},
			{Role: "user", Content: &mcp.EmbeddedResource{Resource: &mcp.ResourceContents{URI: slaURI, MIMEType: "application/json", Text: slaJSON}}},
		},
	}, nil
}

func weeklySummaryPrompt(context.Context, *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	counts := map[string]int{}
	lines := make([]string, len(tickets))
	for i := range tickets {
		counts[tickets[i].Status]++
		lines[i] = ticketLine(&tickets[i])
	}
	tally := make([]string, len(ticketStatuses))
	for i, s := range ticketStatuses {
		tally[i] = fmt.Sprintf("%s %d", s, counts[s])
	}
	return &mcp.GetPromptResult{
		Description: "Weekly queue summary",
		Messages: []*mcp.PromptMessage{{Role: "user", Content: &mcp.TextContent{Text: fmt.Sprintf(
			"Summarize this week at the Acme support desk for the #support channel in five bullet points. "+
				"Call out anything urgent.\n\nTotals: %s\n\n%s",
			strings.Join(tally, ", "), strings.Join(lines, "\n"))}}},
	}, nil
}

// completeArgument completes ticket IDs (resource template and triage
// prompt) and the triage prompt's tone.
func completeArgument(_ context.Context, req *mcp.CompleteRequest) (*mcp.CompleteResult, error) {
	ref, arg := req.Params.Ref, req.Params.Argument
	var values []string
	switch {
	case ref.Type == "ref/resource" && ref.URI == ticketTemplateURI && arg.Name == "id",
		ref.Type == "ref/prompt" && ref.Name == "triage_ticket" && arg.Name == "ticket_id":
		values = ticketIDsWithPrefix(arg.Value)
	case ref.Type == "ref/prompt" && ref.Name == "triage_ticket" && arg.Name == "tone":
		values = valuesWithPrefix(replyTones, arg.Value)
	default:
		return nil, fmt.Errorf("nothing to complete for argument %q of %s %s%s", arg.Name, ref.Type, ref.Name, ref.URI)
	}
	if values == nil {
		values = []string{}
	}
	return &mcp.CompleteResult{Completion: mcp.CompletionResultDetails{Values: values, Total: len(values)}}, nil
}
