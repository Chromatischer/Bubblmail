package demo

import (
	"time"

	"github.com/bubblmail/bubblmail/data"
)

// DemoAccount is the account name used in demo mode.
const DemoAccount = "demo@example.com"

// DemoTime returns a time relative to a fixed anchor so demo data always
// looks fresh regardless of when it is run.
func DemoTime(daysAgo int, hour, minute int) time.Time {
	now := time.Now()
	base := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location())
	return base.AddDate(0, 0, -daysAgo)
}

// BuildDemoFolders returns a realistic set of IMAP folders for the demo account.
func BuildDemoFolders() []*data.Folder {
	return []*data.Folder{
		{Name: "INBOX", DisplayName: "Inbox", Delimiter: "/", Depth: 0, Unread: 6, Total: 11, AccountName: DemoAccount},
		{Name: "Work", DisplayName: "Work", Delimiter: "/", Depth: 0, Unread: 0, Total: 8, AccountName: DemoAccount},
		{Name: "Personal", DisplayName: "Personal", Delimiter: "/", Depth: 0, Unread: 0, Total: 3, AccountName: DemoAccount},
		{Name: "Archive", DisplayName: "Archive", Delimiter: "/", Depth: 0, Unread: 0, Total: 47, AccountName: DemoAccount},
		{Name: "Sent", DisplayName: "Sent", Delimiter: "/", Depth: 0, Unread: 0, Total: 22, AccountName: DemoAccount},
		{Name: "Trash", DisplayName: "Trash", Delimiter: "/", Depth: 0, Unread: 0, Total: 5, AccountName: DemoAccount},
	}
}

// BuildDemoMessages returns demo inbox messages with bodies pre-populated.
// Passing the returned slice to store.UpsertMessages will assign IDs; call
// BuildDemoCategories and BuildDemoSuggestedEvent afterwards.
func BuildDemoMessages() []*data.Message {
	me := []data.Address{{Name: "Alice Demo", Address: DemoAccount}}
	sarah := []data.Address{{Name: "Sarah Chen", Address: "sarah.chen@acme-corp.com"}}
	bob := []data.Address{{Name: "Bob Martinez", Address: "bob.martinez@acme-corp.com"}}
	jordan := []data.Address{{Name: "Jordan Lee", Address: "jordan.lee@gmail.com"}}
	github := []data.Address{{Name: "GitHub", Address: "notifications@github.com"}}
	fedex := []data.Address{{Name: "FedEx Delivery", Address: "donotreply@fedex.com"}}
	billing := []data.Address{{Name: "Acme Cloud Billing", Address: "billing@acmecloud.com"}}
	hn := []data.Address{{Name: "Hacker News Digest", Address: "digest@hn.email"}}
	hr := []data.Address{{Name: "HR Team", Address: "hr@acme-corp.com"}}
	security := []data.Address{{Name: "GitHub Security", Address: "security-noreply@github.com"}}
	team := []data.Address{
		{Name: "Sarah Chen", Address: "sarah.chen@acme-corp.com"},
		{Name: "Bob Martinez", Address: "bob.martinez@acme-corp.com"},
		{Name: "Alice Demo", Address: DemoAccount},
	}

	folder := "INBOX"
	acc := DemoAccount

	msgs := []*data.Message{
		// ── Thread: Q1 Planning Meeting ──────────────────────────────────────
		{
			UID:       1,
			MessageID: "<plan-1@acme-corp.com>",
			Subject:   "Q1 Planning Meeting",
			From:      sarah,
			To:        team,
			Date:      DemoTime(5, 9, 15),
			Flags:     []data.Flag{data.FlagSeen, data.FlagFlagged},
			Size:      1240,
			Snippet:   "Hi team, I'd like to schedule our Q1 planning session to align on goals, OKRs, and resource allocation.",
			Body: `Hi team,

I'd like to schedule our Q1 planning session to align on goals, OKRs, and
resource allocation for the next quarter.

I'm thinking Thursday, ` + nextThursday() + ` from 2:00 PM to 4:00 PM in
Conference Room B. We'll cover:

  • Q4 retrospective (30 min)
  • Q1 OKR review (45 min)
  • Resource allocation (30 min)
  • Open discussion (15 min)

Please let me know if this works. I've attached the draft agenda.

See you there,
Sarah`,
			Attachments: []data.Attachment{
				{Filename: "q1-agenda-draft.pdf", ContentType: "application/pdf", Data: []byte("% fake PDF")},
			},
			FolderName:  folder,
			AccountName: acc,
		},
		{
			UID:        2,
			MessageID:  "<plan-2@acme-corp.com>",
			InReplyTo:  "<plan-1@acme-corp.com>",
			References: []string{"<plan-1@acme-corp.com>"},
			Subject:    "Re: Q1 Planning Meeting",
			From:       bob,
			To:         team,
			Date:       DemoTime(4, 10, 2),
			Flags:      []data.Flag{data.FlagSeen},
			Size:       880,
			Snippet:    "Thursday works great for me! I've already blocked the time. A couple of additions to the agenda…",
			Body: `Sarah,

Thursday works great for me! I've already blocked the time.

A couple of additions to the agenda:

  - Can we also discuss the new-hire headcount for Q1?
  - The infrastructure migration should probably get 15 minutes

See you then,
Bob`,
			FolderName:  folder,
			AccountName: acc,
		},
		{
			UID:        3,
			MessageID:  "<plan-3@acme-corp.com>",
			InReplyTo:  "<plan-2@acme-corp.com>",
			References: []string{"<plan-1@acme-corp.com>", "<plan-2@acme-corp.com>"},
			Subject:    "Re: Q1 Planning Meeting",
			From:       me,
			To:         team,
			Date:       DemoTime(3, 14, 30),
			Flags:      []data.Flag{data.FlagSeen},
			Size:       720,
			Snippet:    "All, perfect! I'll send out the calendar invites shortly. Bob, great additions — I'll update the agenda.",
			Body: `All,

Perfect! I'll send out the calendar invites shortly. Bob, great additions —
I've updated the agenda to include both items.

The latest draft is attached. Let me know if you have any other changes.

Best,
Alice`,
			Attachments: []data.Attachment{
				{Filename: "q1-agenda-v2.pdf", ContentType: "application/pdf", Data: []byte("% fake PDF")},
			},
			FolderName:  folder,
			AccountName: acc,
		},

		// ── GitHub PR notification ────────────────────────────────────────────
		{
			UID:       4,
			MessageID: "<github-pr-143@github.com>",
			Subject:   "[bubblmail/bubblmail] feat: add dark mode support (#143)",
			From:      github,
			To:        me,
			Date:      DemoTime(0, 8, 47),
			Flags:     []data.Flag{},
			Size:      1650,
			Snippet:   "johndoe opened pull request #143: feat: add dark mode support. Changes: config/theme.go, ui/styles.go.",
			Body: `johndoe opened a pull request.

Repository: bubblmail/bubblmail
Pull request #143: feat: add dark mode support

  This PR implements automatic dark/light mode detection using the
  terminal's background colour. When running in a dark terminal, the
  theme switches to a darker palette with appropriate contrast ratios.

  Changes:
    • config/theme.go   — dark mode detection logic
    • ui/styles.go      — dark palette definitions
    • README.md         — document theme configuration

Review requested from: @alice, @bob

---
View pull request  https://github.com/bubblmail/bubblmail/pull/143
Unsubscribe        https://github.com/notifications/unsubscribe`,
			FolderName:  folder,
			AccountName: acc,
		},

		// ── Delivery notification ─────────────────────────────────────────────
		{
			UID:       5,
			MessageID: "<fedex-track-8847@fedex.com>",
			Subject:   "Your order #FR-2026-8847 is out for delivery",
			From:      fedex,
			To:        me,
			Date:      DemoTime(1, 7, 12),
			Flags:     []data.Flag{},
			Size:      980,
			Snippet:   "Your package is out for delivery today! Tracking #: 7489 3425 8903. Estimated delivery: today by 8:00 PM.",
			Body: `Your package is out for delivery today!

  Tracking Number:    7489 3425 8903
  Estimated Delivery: Today by 8:00 PM
  Package Contents:   Electronics (1 item)
  Ship From:          San Francisco, CA
  Delivering To:      123 Main St, New York, NY

Track your package:
  https://www.fedex.com/track?id=748934258903

If you won't be home you can:
  • Leave delivery instructions at fedex.com
  • Schedule a pickup at a nearby FedEx location
  • Hold the package at a FedEx facility

FedEx Ground`,
			FolderName:  folder,
			AccountName: acc,
		},

		// ── Invoice ───────────────────────────────────────────────────────────
		{
			UID:       6,
			MessageID: "<invoice-2026-042@acmecloud.com>",
			Subject:   "Invoice #INV-2026-042 — $49.00 due",
			From:      billing,
			To:        me,
			Date:      DemoTime(3, 11, 0),
			Flags:     []data.Flag{},
			Size:      760,
			Snippet:   "Invoice #INV-2026-042. Billing period: Feb 1–Mar 1, 2026. Due: March 31, 2026. Total: $49.00.",
			Body: `Invoice #INV-2026-042

  Account:        demo@example.com
  Billing Period: February 1 – March 1, 2026
  Due Date:       March 31, 2026

ITEMS
──────────────────────────────────────────────
Pro Plan (Monthly)             $39.00
Additional Storage (50 GB)     $10.00
──────────────────────────────────────────────
TOTAL DUE                      $49.00

Payment Method: Visa ending in 4242
You will be charged automatically on March 31, 2026.

Questions? Reply to this email or visit billing.acmecloud.com`,
			FolderName:  folder,
			AccountName: acc,
		},

		// ── Thread: Weekend plans ─────────────────────────────────────────────
		{
			UID:       7,
			MessageID: "<wknd-1@gmail.com>",
			Subject:   "Weekend plans?",
			From:      jordan,
			To:        me,
			Date:      DemoTime(4, 18, 30),
			Flags:     []data.Flag{data.FlagSeen, data.FlagFlagged},
			Size:      620,
			Snippet:   "Hey Alice! Are you free this Saturday? There's a new brunch place that just opened downtown — The Brunch Club.",
			Body: `Hey Alice!

Are you free this Saturday? There's an amazing new brunch place that just
opened downtown — "The Brunch Club" on 5th Avenue. I heard they do incredible
eggs benedict and the mimosas are supposed to be fantastic.

I was thinking we could meet there at 11:00 AM on Saturday? Let me know!

Jordan`,
			FolderName:  folder,
			AccountName: acc,
		},
		// This message has a suggested calendar event seeded separately.
		{
			UID:        8,
			MessageID:  "<wknd-2@gmail.com>",
			InReplyTo:  "<wknd-1@gmail.com>",
			References: []string{"<wknd-1@gmail.com>"},
			Subject:    "Re: Weekend plans?",
			From:       jordan,
			To:         me,
			Date:       DemoTime(2, 9, 5),
			Flags:      []data.Flag{},
			Size:       540,
			Snippet:    "They open at 10am and take reservations. Should I book for 2 at 11:00 AM at The Brunch Club, 247 5th Avenue?",
			Body: `Hey, just following up!

They open at 10 AM and take reservations online.

Should I book a table for 2 at 11:00 AM on Saturday at The Brunch Club
(247 5th Avenue)? The reservation page is at thebrunchclub.com if you
want to do it yourself.

Looking forward to it!
Jordan`,
			FolderName:  folder,
			AccountName: acc,
		},

		// ── Newsletter ────────────────────────────────────────────────────────
		{
			UID:       9,
			MessageID: "<hn-digest-47@hn.email>",
			Subject:   "Your Weekly Tech Digest — Issue #47",
			From:      hn,
			To:        me,
			Date:      DemoTime(7, 6, 0),
			Flags:     []data.Flag{data.FlagSeen},
			Size:      2100,
			Snippet:   "Top stories this week: Why your database choice matters more than your language; I built a terminal email client in Go…",
			Body: `━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
HACKER NEWS DIGEST — Issue #47
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Top stories this week:

1. "Why your database choice matters more than your language"
   The eternal debate, now with data from a 10-year production system.
   https://hn.news/story/123456

2. "I built a terminal email client in Go — here's what I learned"
   A deep dive into TUI applications with Bubble Tea and IMAP.
   https://hn.news/story/789012

3. "The death of the open-plan office"
   Remote-work data from 500 companies shows productivity trends.
   https://hn.news/story/345678

4. "SQLite is not a toy database"
   How and when to use SQLite in production with WAL mode.
   https://hn.news/story/901234

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
To unsubscribe: reply with "unsubscribe"`,
			FolderName:  folder,
			AccountName: acc,
		},

		// ── Performance review (IMPORTANT, with attachment) ───────────────────
		{
			UID:       10,
			MessageID: "<hr-perf-2026@acme-corp.com>",
			Subject:   "Performance review scheduled — March 20",
			From:      hr,
			To:        me,
			Date:      DemoTime(0, 9, 0),
			Flags:     []data.Flag{},
			Size:      1120,
			Snippet:   "Your annual performance review has been scheduled for Thursday, March 20th at 3:00 PM with David Kim.",
			Body: `Hi Alice,

Your annual performance review has been scheduled.

MEETING DETAILS
  Date:     Thursday, March 20, 2026
  Time:     3:00 PM – 4:00 PM
  Location: HR Conference Room (Floor 4)
  Manager:  David Kim

Please complete the attached self-assessment form before the meeting.
Your responses will be shared with your manager 24 hours in advance.

If you need to reschedule, contact hr@acme-corp.com at least 48 hours
in advance.

Best regards,
HR Team — Acme Corporation`,
			Attachments: []data.Attachment{
				{Filename: "self-assessment-form.pdf", ContentType: "application/pdf", Data: []byte("% fake PDF")},
			},
			FolderName:  folder,
			AccountName: acc,
		},

		// ── Security alert ────────────────────────────────────────────────────
		{
			UID:       11,
			MessageID: "<security-signin-1@github.com>",
			Subject:   "New sign-in to your GitHub account",
			From:      security,
			To:        me,
			Date:      DemoTime(0, 7, 33),
			Flags:     []data.Flag{},
			Size:      890,
			Snippet:   "We noticed a new sign-in to your GitHub account from Berlin, Germany. If this was you, no action is needed.",
			Body: `We noticed a new sign-in to your GitHub account.

  Location:  Berlin, Germany
  Device:    Chrome on macOS
  IP:        192.0.2.42
  Time:      Today at 07:28 UTC

If this was you, you can ignore this message.

If you don't recognise this sign-in, secure your account immediately:
  https://github.com/settings/security

GitHub Security`,
			FolderName:  folder,
			AccountName: acc,
		},
	}
	return msgs
}

// CategorySeed maps a message UID to the smart-folder category it belongs to.
type CategorySeed struct {
	UID      uint32
	Category string
}

// BuildDemoCategories returns the category assignments for demo messages.
func BuildDemoCategories() []CategorySeed {
	return []CategorySeed{
		{UID: 1, Category: "IMPORTANT"},
		{UID: 2, Category: "IMPORTANT"},
		{UID: 4, Category: "GITHUB"},
		{UID: 5, Category: "DELIVERIES"},
		{UID: 6, Category: "RECEIPTS"},
		{UID: 9, Category: "NEWSLETTERS"},
		{UID: 10, Category: "IMPORTANT"},
		{UID: 11, Category: "GITHUB"},
	}
}

// BuildDemoSuggestedEvent returns the suggested calendar event for the weekend
// thread follow-up (UID 8). messageID must be the SQLite row ID of that message.
func BuildDemoSuggestedEvent(messageID int64) *data.SuggestedEvent {
	// Pick next Saturday from today.
	now := time.Now()
	daysUntilSat := (6 - int(now.Weekday()) + 7) % 7
	if daysUntilSat == 0 {
		daysUntilSat = 7
	}
	saturday := now.AddDate(0, 0, daysUntilSat)
	dateStr := saturday.Format("2006-01-02")

	return &data.SuggestedEvent{
		MessageID:    messageID,
		HasEvent:     true,
		Summary:      "Brunch at The Brunch Club",
		Date:         dateStr,
		Start:        "11:00",
		End:          "13:00",
		Location:     "247 5th Avenue",
		Calendar:     "Personal",
		Status:       "CONFIRMED",
		AllDay:       false,
		Recurring:    false,
		Description:  "Brunch with Jordan at The Brunch Club",
		Model:        "demo",
		GeneratedAt:  time.Now().Unix(),
		SourceHash:   "demo",
		PlainText:    "brunch saturday 11am The Brunch Club",
		JSONText:     `{"hasEvent":true,"summary":"Brunch at The Brunch Club"}`,
		GenerationOK: true,
	}
}

// nextThursday returns a friendly date string for the next Thursday.
func nextThursday() string {
	now := time.Now()
	daysUntil := (4 - int(now.Weekday()) + 7) % 7
	if daysUntil == 0 {
		daysUntil = 7
	}
	t := now.AddDate(0, 0, daysUntil)
	return t.Format("January 2")
}
