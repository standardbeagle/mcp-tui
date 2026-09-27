package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// escalationStepDelay spaces escalate_ticket's four steps, so the whole call
// takes about four seconds and its progress is visible.
const escalationStepDelay = time.Second

// Input request keys of the multi-round-trip tools (SEP-2322). On 2026-07-28
// the client fulfils them and retries the call; on earlier protocols the
// SDK asks the client directly and re-invokes the handler with the answers.
const (
	callbackTimeInputKey = "callback_time"
	replyDraftInputKey   = "reply_draft"
)

func registerTools(server *mcp.Server, misbehave bool, logger *slog.Logger) {
	mcp.AddTool(server, &mcp.Tool{
		Name:  "search_tickets",
		Title: "Search tickets",
		Description: "Search the Acme support queue. Filters combine: free-text query, status, tags " +
			"(a ticket must carry every tag) and an optional priority/customer filter.",
		Icons:        []mcp.Icon{{Source: logoDataURI(), MIMEType: "image/png", Sizes: []string{"32x32"}}},
		Annotations:  &mcp.ToolAnnotations{Title: "Search tickets", ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: ptr(false)},
		InputSchema:  searchTicketsInputSchema(),
		OutputSchema: searchTicketsOutputSchema(),
	}, func(ctx context.Context, req *mcp.CallToolRequest, in searchTicketsInput) (*mcp.CallToolResult, searchTicketsOutput, error) {
		out, text := searchTickets(in)
		logToolInfo(ctx, req, logger, fmt.Sprintf("search_tickets matched %d tickets, returned %d", out.Total, len(out.Tickets)))
		return textResult(text), out, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:         "create_ticket",
		Title:        "Create ticket",
		Description:  "Open a new ticket for a customer. The demo desk reports the ticket it would create; the queue itself does not change.",
		Annotations:  &mcp.ToolAnnotations{Title: "Create ticket", DestructiveHint: ptr(false), IdempotentHint: false, OpenWorldHint: ptr(false)},
		InputSchema:  createTicketInputSchema(),
		OutputSchema: objectSchema([]string{"id", "status", "priority"}, "id", stringSchema("New ticket ID"), "status", stringSchema("Always open"), "priority", enumSchema("Priority", ticketPriorities)),
	}, func(ctx context.Context, req *mcp.CallToolRequest, in createTicketInput) (*mcp.CallToolResult, createTicketOutput, error) {
		out := createTicketOutput{ID: nextTicketID, Status: "open", Priority: in.Priority}
		logToolInfo(ctx, req, logger, fmt.Sprintf("create_ticket opened %s for %s", out.ID, in.Customer.Email))
		return textResult(fmt.Sprintf("Created %s (%s) for %s <%s>: %s",
			out.ID, out.Priority, in.Customer.Name, in.Customer.Email, in.Subject)), out, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "delete_ticket",
		Title:       "Delete ticket",
		Description: "Permanently delete a ticket and its history.",
		Annotations: &mcp.ToolAnnotations{Title: "Delete ticket", DestructiveHint: ptr(true), IdempotentHint: true, OpenWorldHint: ptr(false)},
		InputSchema: objectSchema([]string{"ticket_id"}, "ticket_id", ticketIDSchema()),
	}, func(ctx context.Context, req *mcp.CallToolRequest, in ticketRef) (*mcp.CallToolResult, any, error) {
		t, ok := findTicket(in.TicketID)
		if !ok {
			return nil, nil, fmt.Errorf("no ticket %s", in.TicketID)
		}
		logToolInfo(ctx, req, logger, "delete_ticket deleted "+t.ID)
		return textResult(fmt.Sprintf("Deleted %s (%q) and its history.", t.ID, t.Subject)), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "escalate_ticket",
		Title:       "Escalate ticket",
		Description: "Escalate a ticket to the on-call engineer. Runs four steps over about four seconds and reports progress after each.",
		Annotations: &mcp.ToolAnnotations{Title: "Escalate ticket", DestructiveHint: ptr(false), OpenWorldHint: ptr(true)},
		InputSchema: objectSchema([]string{"ticket_id"},
			"ticket_id", ticketIDSchema(),
			"reason", &jsonschema.Schema{Type: "string", Title: "Reason", Description: "Why the ticket needs escalating", MaxLength: ptr(200)}),
	}, func(ctx context.Context, req *mcp.CallToolRequest, in escalateTicketInput) (*mcp.CallToolResult, any, error) {
		return escalateTicket(ctx, req, in, logger)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "lookup_customer",
		Title:       "Look up customer",
		Description: "Find a customer by ID or email. Unknown customers come back as a tool error.",
		Annotations: &mcp.ToolAnnotations{Title: "Look up customer", ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: ptr(false)},
		InputSchema: objectSchema(nil,
			"customer_id", &jsonschema.Schema{Type: "string", Title: "Customer ID", Pattern: "^C-[0-9]{4}$", Examples: []any{"C-1001"}},
			"email", &jsonschema.Schema{Type: "string", Title: "Email", Format: "email"}),
	}, func(ctx context.Context, req *mcp.CallToolRequest, in lookupCustomerInput) (*mcp.CallToolResult, any, error) {
		c, err := lookupCustomer(in)
		if err != nil {
			if misbehave {
				// Deliberately broken: an error result with its content
				// dropped, which the seterror-content probe catches.
				return &mcp.CallToolResult{IsError: true}, nil, nil
			}
			return nil, nil, err
		}
		logToolInfo(ctx, req, logger, "lookup_customer found "+c.ID)
		return textResult(fmt.Sprintf("%s  %s <%s>\nCompany: %s\nPlan:    %s", c.ID, c.Name, c.Email, c.Company, c.Plan)), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:  "schedule_callback",
		Title: "Schedule callback",
		Description: "Book a phone callback with the ticket's customer. Without a time, the desk asks you for one " +
			"(elicitation).",
		Annotations: &mcp.ToolAnnotations{Title: "Schedule callback", DestructiveHint: ptr(false), OpenWorldHint: ptr(false)},
		InputSchema: objectSchema([]string{"ticket_id"},
			"ticket_id", ticketIDSchema(),
			"time", &jsonschema.Schema{Type: "string", Title: "Callback time", Description: "RFC 3339 date-time, e.g. 2026-09-29T15:00:00Z", Format: "date-time"}),
	}, func(ctx context.Context, req *mcp.CallToolRequest, in scheduleCallbackInput) (*mcp.CallToolResult, any, error) {
		return scheduleCallback(ctx, req, in, logger)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "draft_reply",
		Title:       "Draft reply",
		Description: "Draft a reply to the ticket's customer. The desk asks your client's model to write it (sampling).",
		Annotations: &mcp.ToolAnnotations{Title: "Draft reply", ReadOnlyHint: true, OpenWorldHint: ptr(true)},
		InputSchema: objectSchema([]string{"ticket_id"},
			"ticket_id", ticketIDSchema(),
			"tone", withDefault(enumSchema("Tone", replyTones), `"friendly"`)),
	}, func(ctx context.Context, req *mcp.CallToolRequest, in draftReplyInput) (*mcp.CallToolResult, any, error) {
		return draftReply(ctx, req, in, logger)
	})

	if misbehave {
		// Deliberately broken: SEP-986 allows only A-Z a-z 0-9 _ - . in
		// tool names. The SDK logs the violation and serves the tool anyway.
		server.AddTool(&mcp.Tool{
			Name:        "create ticket!",
			Description: "Registered under a name with a space and '!' so the tool-names probe has something to catch.",
			InputSchema: &jsonschema.Schema{Type: "object"},
		}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return textResult("This tool exists only to carry an invalid name."), nil
		})
	}
}

type searchTicketsInput struct {
	Query  string        `json:"query,omitempty"`
	Status string        `json:"status,omitempty"`
	Limit  int           `json:"limit,omitempty"`
	Tags   []string      `json:"tags,omitempty"`
	Filter *ticketFilter `json:"filter,omitempty"`
}

type ticketFilter struct {
	Priority   string `json:"priority,omitempty"`
	CustomerID string `json:"customer_id,omitempty"`
}

type ticketSummary struct {
	ID         string   `json:"id"`
	Subject    string   `json:"subject"`
	Status     string   `json:"status"`
	Priority   string   `json:"priority"`
	CustomerID string   `json:"customer_id"`
	Tags       []string `json:"tags"`
}

type searchTicketsOutput struct {
	Total   int             `json:"total"`
	Tickets []ticketSummary `json:"tickets"`
}

// searchTickets filters the desk and returns the structured result and its
// text rendering. Limit has already been defaulted by the input schema.
func searchTickets(in searchTicketsInput) (searchTicketsOutput, string) {
	query := strings.ToLower(in.Query)
	out := searchTicketsOutput{Tickets: []ticketSummary{}}
	var lines []string
	for _, t := range tickets {
		switch {
		case in.Status != "" && t.Status != in.Status,
			query != "" && !strings.Contains(strings.ToLower(t.Subject+" "+t.Body), query),
			!containsAll(t.Tags, in.Tags),
			in.Filter != nil && in.Filter.Priority != "" && t.Priority != in.Filter.Priority,
			in.Filter != nil && in.Filter.CustomerID != "" && t.CustomerID != in.Filter.CustomerID:
			continue
		}
		out.Total++
		if len(out.Tickets) < in.Limit {
			out.Tickets = append(out.Tickets, ticketSummary{ID: t.ID, Subject: t.Subject, Status: t.Status,
				Priority: t.Priority, CustomerID: t.CustomerID, Tags: t.Tags})
			lines = append(lines, ticketLine(t))
		}
	}
	header := fmt.Sprintf("%d of %d matching tickets", len(out.Tickets), out.Total)
	if len(lines) == 0 {
		return out, header + "."
	}
	return out, header + ":\n" + strings.Join(lines, "\n")
}

func containsAll(have, want []string) bool {
	for _, w := range want {
		if !slices.Contains(have, w) {
			return false
		}
	}
	return true
}

type customerDetails struct {
	Name    string `json:"name"`
	Email   string `json:"email"`
	Company string `json:"company,omitempty"`
}

type createTicketInput struct {
	Subject     string          `json:"subject"`
	Description string          `json:"description"`
	Priority    string          `json:"priority,omitempty"`
	Customer    customerDetails `json:"customer"`
	Tags        []string        `json:"tags,omitempty"`
}

type createTicketOutput struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	Priority string `json:"priority"`
}

type ticketRef struct {
	TicketID string `json:"ticket_id"`
}

type escalateTicketInput struct {
	TicketID string `json:"ticket_id"`
	Reason   string `json:"reason,omitempty"`
}

var escalationSteps = []string{
	"Paging the on-call engineer",
	"Notifying the account manager",
	"Raising priority to urgent",
	"Posting to #support-escalations",
}

// escalateTicket walks the escalation steps, reporting progress (when the
// client sent a progress token) and a log line after each one.
func escalateTicket(ctx context.Context, req *mcp.CallToolRequest, in escalateTicketInput, logger *slog.Logger) (*mcp.CallToolResult, any, error) {
	t, ok := findTicket(in.TicketID)
	if !ok {
		return nil, nil, fmt.Errorf("no ticket %s", in.TicketID)
	}
	token := req.Params.GetProgressToken()
	for i, step := range escalationSteps {
		timer := time.NewTimer(escalationStepDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, nil, fmt.Errorf("escalation of %s stopped after %d of %d steps: %w", t.ID, i, len(escalationSteps), ctx.Err())
		case <-timer.C:
		}
		if token != nil {
			if err := req.Session.NotifyProgress(ctx, &mcp.ProgressNotificationParams{
				ProgressToken: token, Progress: float64(i + 1), Total: float64(len(escalationSteps)), Message: step,
			}); err != nil {
				logger.Warn("progress notification failed", "tool", "escalate_ticket", "error", err)
			}
		}
		logToolInfo(ctx, req, logger, fmt.Sprintf("escalate_ticket %s: %s", t.ID, step))
	}
	reason := in.Reason
	if reason == "" {
		reason = "no reason given"
	}
	return textResult(fmt.Sprintf("Escalated %s (%q) to the on-call engineer: %s.\nSteps: %s.",
		t.ID, t.Subject, reason, strings.Join(escalationSteps, ", "))), nil, nil
}

type lookupCustomerInput struct {
	CustomerID string `json:"customer_id,omitempty"`
	Email      string `json:"email,omitempty"`
}

func lookupCustomer(in lookupCustomerInput) (Customer, error) {
	switch {
	case in.CustomerID != "":
		if c, ok := findCustomerByID(in.CustomerID); ok {
			return c, nil
		}
		return Customer{}, fmt.Errorf("no customer with ID %s", in.CustomerID)
	case in.Email != "":
		if c, ok := findCustomerByEmail(in.Email); ok {
			return c, nil
		}
		return Customer{}, fmt.Errorf("no customer with email %s", in.Email)
	default:
		return Customer{}, fmt.Errorf("give a customer_id or an email to look up")
	}
}

type scheduleCallbackInput struct {
	TicketID string `json:"ticket_id"`
	Time     string `json:"time,omitempty"`
}

// scheduleCallback books the callback at the given time, or asks the user
// for one: the first call returns an input request, and the retry carries
// the answer.
func scheduleCallback(ctx context.Context, req *mcp.CallToolRequest, in scheduleCallbackInput, logger *slog.Logger) (*mcp.CallToolResult, any, error) {
	t, ok := findTicket(in.TicketID)
	if !ok {
		return nil, nil, fmt.Errorf("no ticket %s", in.TicketID)
	}
	c, ok := findCustomerByID(t.CustomerID)
	if !ok {
		return nil, nil, fmt.Errorf("ticket %s names unknown customer %s", t.ID, t.CustomerID)
	}
	when, phone := in.Time, ""
	if when == "" {
		resp, answered := req.Params.InputResponses[callbackTimeInputKey]
		if !answered {
			return &mcp.CallToolResult{InputRequests: mcp.InputRequestMap{
				callbackTimeInputKey: callbackTimeElicitation(t, c),
			}}, nil, nil
		}
		elicited, ok := resp.(*mcp.ElicitResult)
		if !ok {
			return nil, nil, fmt.Errorf("input response %q is %T, want an elicitation result", callbackTimeInputKey, resp)
		}
		if elicited.Action != "accept" {
			return textResult(fmt.Sprintf("No callback booked for %s: you chose %q.", t.ID, elicited.Action)), nil, nil
		}
		var hasTime bool
		if when, hasTime = elicited.Content["time"].(string); !hasTime {
			return nil, nil, fmt.Errorf("the accepted callback form has no time: %v", elicited.Content)
		}
		if p, ok := elicited.Content["phone"].(string); ok {
			phone = p
		}
	}
	at, err := time.Parse(time.RFC3339, when)
	if err != nil {
		return nil, nil, fmt.Errorf("callback time %q is not an RFC 3339 date-time (e.g. 2026-09-29T15:00:00Z)", when)
	}
	if phone == "" {
		phone = "the number on file"
	}
	logToolInfo(ctx, req, logger, fmt.Sprintf("schedule_callback booked %s for %s", t.ID, at.UTC().Format(time.RFC3339)))
	return textResult(fmt.Sprintf("Callback booked for %s: %s (%s) on %s at %s UTC, calling %s.",
		t.ID, c.Name, c.Company, at.UTC().Format("Mon 2 Jan 2006"), at.UTC().Format("15:04"), phone)), nil, nil
}

func callbackTimeElicitation(t Ticket, c Customer) *mcp.ElicitParams {
	return &mcp.ElicitParams{
		Message: fmt.Sprintf("When should Acme call %s (%s) about %s?", c.Name, c.Company, t.ID),
		RequestedSchema: objectSchema([]string{"time"},
			"time", &jsonschema.Schema{Type: "string", Title: "Callback time (UTC)", Description: "e.g. 2026-09-29T15:00:00Z", Format: "date-time"},
			"phone", &jsonschema.Schema{Type: "string", Title: "Phone number", Description: "Leave empty to use the number on file"}),
	}
}

type draftReplyInput struct {
	TicketID string `json:"ticket_id"`
	Tone     string `json:"tone,omitempty"`
}

// draftReply asks the client's model for a reply: the first call returns a
// sampling input request, and the retry carries the model's answer.
func draftReply(ctx context.Context, req *mcp.CallToolRequest, in draftReplyInput, logger *slog.Logger) (*mcp.CallToolResult, any, error) {
	t, ok := findTicket(in.TicketID)
	if !ok {
		return nil, nil, fmt.Errorf("no ticket %s", in.TicketID)
	}
	c, ok := findCustomerByID(t.CustomerID)
	if !ok {
		return nil, nil, fmt.Errorf("ticket %s names unknown customer %s", t.ID, t.CustomerID)
	}
	resp, answered := req.Params.InputResponses[replyDraftInputKey]
	if !answered {
		return &mcp.CallToolResult{InputRequests: mcp.InputRequestMap{
			replyDraftInputKey: &mcp.CreateMessageParams{
				SystemPrompt: fmt.Sprintf("You are a support agent at Acme. Write a short, %s reply to the customer. "+
					"Sign it \"The Acme support team\".", in.Tone),
				Messages: []*mcp.SamplingMessage{{Role: "user", Content: &mcp.TextContent{Text: fmt.Sprintf(
					"Ticket %s from %s (%s, %s plan), priority %s.\nSubject: %s\n\n%s",
					t.ID, c.Name, c.Company, c.Plan, t.Priority, t.Subject, t.Body)}}},
				MaxTokens: 400,
			},
		}}, nil, nil
	}
	sampled, ok := resp.(*mcp.CreateMessageWithToolsResult)
	if !ok {
		return nil, nil, fmt.Errorf("input response %q is %T, want a sampling result", replyDraftInputKey, resp)
	}
	var draft *mcp.TextContent
	for _, content := range sampled.Content {
		if text, ok := content.(*mcp.TextContent); ok {
			draft = text
			break
		}
	}
	if draft == nil {
		return nil, nil, fmt.Errorf("the client's model answered without any text")
	}
	logToolInfo(ctx, req, logger, fmt.Sprintf("draft_reply drafted a %s reply for %s", in.Tone, t.ID))
	return textResult(fmt.Sprintf("Draft reply to %s on %s (%s tone):\n\n%s",
		c.Name, t.ID, in.Tone, draft.Text)), nil, nil
}

// logToolInfo sends an info-level log notification for a tool run; the SDK
// drops it unless the client asked for info or lower.
func logToolInfo(ctx context.Context, req *mcp.CallToolRequest, logger *slog.Logger, msg string) {
	if err := req.Session.Log(ctx, &mcp.LoggingMessageParams{Level: "info", Logger: "acme.desk", Data: msg}); err != nil {
		logger.Warn("log notification failed", "error", err)
	}
}

func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}
}

func searchTicketsInputSchema() *jsonschema.Schema {
	return objectSchema(nil,
		"query", &jsonschema.Schema{Type: "string", Title: "Query", Description: "Words to find in the subject or body"},
		"status", enumSchema("Status", ticketStatuses),
		"limit", &jsonschema.Schema{Type: "integer", Title: "Limit", Description: "Most tickets to return",
			Minimum: ptr(1.0), Maximum: ptr(50.0), Default: json.RawMessage("10")},
		"tags", &jsonschema.Schema{Type: "array", Title: "Tags", Description: "Only tickets carrying every one of these tags",
			Items: &jsonschema.Schema{Type: "string", Pattern: "^[a-z0-9-]+$"}, MaxItems: ptr(5), UniqueItems: true},
		"filter", objectSchema(nil,
			"priority", enumSchema("Priority", ticketPriorities),
			"customer_id", &jsonschema.Schema{Type: "string", Title: "Customer ID", Pattern: "^C-[0-9]{4}$", Examples: []any{"C-1001"}}),
	)
}

func searchTicketsOutputSchema() *jsonschema.Schema {
	summary := objectSchema([]string{"id", "subject", "status", "priority", "customer_id", "tags"},
		"id", stringSchema("Ticket ID"),
		"subject", stringSchema("Subject"),
		"status", enumSchema("Status", ticketStatuses),
		"priority", enumSchema("Priority", ticketPriorities),
		"customer_id", stringSchema("Customer ID"),
		"tags", &jsonschema.Schema{Type: "array", Items: &jsonschema.Schema{Type: "string"}})
	return objectSchema([]string{"total", "tickets"},
		"total", &jsonschema.Schema{Type: "integer", Title: "Matching tickets", Description: "Before the limit"},
		"tickets", &jsonschema.Schema{Type: "array", Items: summary})
}

func createTicketInputSchema() *jsonschema.Schema {
	return objectSchema([]string{"subject", "description", "customer"},
		"subject", &jsonschema.Schema{Type: "string", Title: "Subject", MinLength: ptr(5), MaxLength: ptr(120)},
		"description", &jsonschema.Schema{Type: "string", Title: "Description", Description: "What the customer reported"},
		"priority", withDefault(enumSchema("Priority", ticketPriorities), `"normal"`),
		"customer", objectSchema([]string{"name", "email"},
			"name", &jsonschema.Schema{Type: "string", Title: "Name", MinLength: ptr(1)},
			"email", &jsonschema.Schema{Type: "string", Title: "Email", Format: "email"},
			"company", &jsonschema.Schema{Type: "string", Title: "Company"}),
		"tags", &jsonschema.Schema{Type: "array", Title: "Tags",
			Items: &jsonschema.Schema{Type: "string", Pattern: "^[a-z0-9-]+$"}, MaxItems: ptr(5), UniqueItems: true},
	)
}

func ticketIDSchema() *jsonschema.Schema {
	return &jsonschema.Schema{Type: "string", Title: "Ticket ID", Pattern: "^T-[0-9]{4}$", Examples: []any{"T-1041"}}
}

func stringSchema(title string) *jsonschema.Schema {
	return &jsonschema.Schema{Type: "string", Title: title}
}

func enumSchema(title string, values []string) *jsonschema.Schema {
	enum := make([]any, len(values))
	for i, v := range values {
		enum[i] = v
	}
	return &jsonschema.Schema{Type: "string", Title: title, Enum: enum}
}

func withDefault(s *jsonschema.Schema, defaultJSON string) *jsonschema.Schema {
	s.Default = json.RawMessage(defaultJSON)
	return s
}

// objectSchema builds an object schema from name/schema pairs, keeping the
// properties in the order given so tool descriptions read top to bottom.
func objectSchema(required []string, namesAndSchemas ...any) *jsonschema.Schema {
	s := &jsonschema.Schema{Type: "object", Required: required, Properties: map[string]*jsonschema.Schema{}}
	if len(namesAndSchemas)%2 != 0 {
		panic(fmt.Sprintf("objectSchema: %d arguments, want name/schema pairs", len(namesAndSchemas)))
	}
	for i := 0; i+1 < len(namesAndSchemas); i += 2 {
		name, isName := namesAndSchemas[i].(string)
		schema, isSchema := namesAndSchemas[i+1].(*jsonschema.Schema)
		if !isName || !isSchema {
			panic(fmt.Sprintf("objectSchema: arguments %d and %d are %T and %T, want a name and a *jsonschema.Schema",
				i, i+1, namesAndSchemas[i], namesAndSchemas[i+1]))
		}
		s.Properties[name] = schema
		s.PropertyOrder = append(s.PropertyOrder, name)
	}
	return s
}

func ptr[T any](v T) *T { return &v }
