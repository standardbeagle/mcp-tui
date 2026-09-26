package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"slices"
	"strings"
	"sync"
)

// The Acme support desk is fixed: every ticket, customer and document below
// is compiled in and never changes, so every take of a recording prints the
// same thing. Tools that would change the desk (create, delete, escalate)
// report what they did without altering this data.

// Customer is one Acme customer account.
type Customer struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Email   string `json:"email"`
	Company string `json:"company"`
	Plan    string `json:"plan"`
}

// Ticket is one support ticket.
type Ticket struct {
	ID         string   `json:"id"`
	Subject    string   `json:"subject"`
	Status     string   `json:"status"`
	Priority   string   `json:"priority"`
	CustomerID string   `json:"customer_id"`
	Tags       []string `json:"tags"`
	Opened     string   `json:"opened"`
	Body       string   `json:"body"`
}

// Ticket statuses, priorities and reply tones the desk knows, in display order.
var (
	ticketStatuses   = []string{"open", "pending", "solved", "closed"}
	ticketPriorities = []string{"low", "normal", "high", "urgent"}
	replyTones       = []string{"friendly", "formal", "apologetic"}
)

var customers = []Customer{
	{ID: "C-1001", Name: "Dana Whitfield", Email: "dana@northwind-labs.example", Company: "Northwind Labs", Plan: "Enterprise"},
	{ID: "C-1002", Name: "Marco Ruiz", Email: "marco@bluepeak.example", Company: "Bluepeak Outfitters", Plan: "Pro"},
	{ID: "C-1003", Name: "Priya Natarajan", Email: "priya@lumen-health.example", Company: "Lumen Health", Plan: "Enterprise"},
	{ID: "C-1004", Name: "Tomasz Kowal", Email: "tomasz@kowal-design.example", Company: "Kowal Design", Plan: "Starter"},
}

var tickets = []Ticket{
	{ID: "T-1040", Subject: "Invoice PDF shows the wrong VAT rate", Status: "open", Priority: "high",
		CustomerID: "C-1002", Tags: []string{"billing", "invoices"}, Opened: "2026-09-14T09:12:00Z",
		Body: "Our September invoice lists VAT at 19% but we are billed from Spain, so it should be 21%."},
	{ID: "T-1041", Subject: "SSO login loops back to the sign-in page", Status: "open", Priority: "urgent",
		CustomerID: "C-1001", Tags: []string{"sso", "auth"}, Opened: "2026-09-15T07:48:00Z",
		Body: "Since this morning every agent who signs in with Okta lands back on the sign-in page."},
	{ID: "T-1042", Subject: "CSV export drops the last row", Status: "pending", Priority: "normal",
		CustomerID: "C-1003", Tags: []string{"export", "data"}, Opened: "2026-09-15T13:05:00Z",
		Body: "Exporting the weekly report to CSV always leaves out the final row of the table."},
	{ID: "T-1043", Subject: "Dark mode for the agent console", Status: "open", Priority: "low",
		CustomerID: "C-1004", Tags: []string{"feature-request", "ui"}, Opened: "2026-09-16T10:30:00Z",
		Body: "Our night shift would love a dark theme for the agent console."},
	{ID: "T-1044", Subject: "Webhook retries flood our endpoint", Status: "open", Priority: "high",
		CustomerID: "C-1001", Tags: []string{"webhooks", "api"}, Opened: "2026-09-16T16:22:00Z",
		Body: "A single failed delivery produced 400 retries within a minute."},
	{ID: "T-1045", Subject: "Password reset email never arrives", Status: "solved", Priority: "normal",
		CustomerID: "C-1002", Tags: []string{"email", "auth"}, Opened: "2026-09-17T08:02:00Z",
		Body: "Reset emails to our domain vanish; nothing in spam either."},
	{ID: "T-1046", Subject: "Rate limit headers missing from API responses", Status: "pending", Priority: "normal",
		CustomerID: "C-1003", Tags: []string{"api"}, Opened: "2026-09-18T11:40:00Z",
		Body: "The docs promise X-RateLimit-Remaining but responses do not carry it."},
	{ID: "T-1047", Subject: "Mobile app crashes on attachment upload", Status: "closed", Priority: "high",
		CustomerID: "C-1004", Tags: []string{"mobile", "attachments"}, Opened: "2026-09-19T15:15:00Z",
		Body: "Uploading a photo from the iOS app closes the app immediately."},
}

// nextTicketID is the ID create_ticket reports: the one after the last
// fixed ticket, the same on every call because the desk never changes.
const nextTicketID = "T-1048"

func findTicket(id string) (Ticket, bool) {
	i := slices.IndexFunc(tickets, func(t Ticket) bool { return t.ID == id })
	if i < 0 {
		return Ticket{}, false
	}
	return tickets[i], true
}

func findCustomerByID(id string) (Customer, bool) {
	i := slices.IndexFunc(customers, func(c Customer) bool { return c.ID == id })
	if i < 0 {
		return Customer{}, false
	}
	return customers[i], true
}

func findCustomerByEmail(email string) (Customer, bool) {
	i := slices.IndexFunc(customers, func(c Customer) bool { return strings.EqualFold(c.Email, email) })
	if i < 0 {
		return Customer{}, false
	}
	return customers[i], true
}

// ticketIDsWithPrefix answers completion for ticket IDs.
func ticketIDsWithPrefix(prefix string) []string {
	var ids []string
	for _, t := range tickets {
		if strings.HasPrefix(t.ID, strings.ToUpper(prefix)) {
			ids = append(ids, t.ID)
		}
	}
	return ids
}

func valuesWithPrefix(values []string, prefix string) []string {
	var out []string
	for _, v := range values {
		if strings.HasPrefix(v, prefix) {
			out = append(out, v)
		}
	}
	return out
}

// ticketLine is the one-line text form of a ticket used in tool results.
func ticketLine(t Ticket) string {
	return fmt.Sprintf("%s  %-7s  %-7s  %s", t.ID, t.Priority, t.Status, t.Subject)
}

const gettingStartedMarkdown = `# Getting started with the Acme support desk

Welcome to the desk. Every ticket belongs to one customer and moves through
four statuses:

| Status  | Meaning                                  |
|---------|------------------------------------------|
| open    | Waiting on an agent                      |
| pending | Waiting on the customer                  |
| solved  | Fixed; reopens if the customer replies   |
| closed  | Archived after 7 days in solved          |

## First steps

1. Search the queue with ` + "`search_tickets`" + ` (try ` + "`status=open`" + `).
2. Look up who you are helping with ` + "`lookup_customer`" + `.
3. Draft a reply with ` + "`draft_reply`" + ` and review it before sending.
4. Anything urgent that is stuck goes to ` + "`escalate_ticket`" + `.

Response targets per plan are in ` + "`acme://config/sla.json`" + `.
`

const slaJSON = `{
  "timezone": "UTC",
  "plans": {
    "Starter":    {"first_response_hours": 24, "resolution_hours": 120},
    "Pro":        {"first_response_hours": 8,  "resolution_hours": 48},
    "Enterprise": {"first_response_hours": 1,  "resolution_hours": 8}
  },
  "escalation": {
    "urgent_after_minutes": 30,
    "on_call": "#support-escalations"
  }
}
`

// queueSnapshots is the sequence the live queue resource walks through, one
// step per update, so a watched queue changes the same way on every take.
var queueSnapshots = []string{
	"open: 4  pending: 2  solved today: 1  oldest open: T-1040 (2h 10m)",
	"open: 5  pending: 2  solved today: 1  oldest open: T-1040 (2h 13m)",
	"open: 4  pending: 3  solved today: 1  oldest open: T-1040 (2h 16m)",
	"open: 3  pending: 3  solved today: 2  oldest open: T-1041 (1h 50m)",
}

// logoPNG is the Acme logo: a 32x32 teal tile with a white "A", drawn in
// code so the demo carries no binary files. Go's PNG encoder is
// deterministic, so the bytes are the same on every run.
var logoPNG = sync.OnceValue(func() []byte {
	const size = 32
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	teal := color.NRGBA{R: 0x0f, G: 0x76, B: 0x6e, A: 0xff}
	white := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	for y := range size {
		for x := range size {
			img.Set(x, y, teal)
		}
	}
	// Two legs of the "A" meeting at the top, and its crossbar.
	for y := 6; y < 27; y++ {
		offset := (y - 6) / 2
		for w := range 3 {
			img.Set(15-offset+w-1, y, white)
			img.Set(16+offset-w+1, y, white)
		}
	}
	for x := 11; x < 21; x++ {
		img.Set(x, 19, white)
		img.Set(x, 20, white)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(fmt.Sprintf("encoding the Acme logo: %v", err))
	}
	return buf.Bytes()
})

// logoDataURI is the logo as a data: URI, used as the server and tool icon.
func logoDataURI() string {
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(logoPNG())
}
