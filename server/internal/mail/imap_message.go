package mail

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	gomessage "github.com/emersion/go-message"
	gomail "github.com/emersion/go-message/mail"
)

// GetMessage fetches one full message. It never marks the message as seen.
func (s *Service) GetMessage(ctx context.Context, accountID string, uid uint32, mailbox string) (*MailMessage, error) {
	a, err := s.account(accountID)
	if err != nil {
		return nil, err
	}
	if mailbox == "" {
		mailbox = "INBOX"
	}
	var out *MailMessage
	err = s.withConn(ctx, a, func(c *imapclient.Client) error {
		out = nil
		if _, err := c.Select(mailbox, &imap.SelectOptions{ReadOnly: true}).Wait(); err != nil {
			return fmt.Errorf("select %s: %w", mailbox, err)
		}
		msgs, err := c.Fetch(imap.UIDSetNum(imap.UID(uid)), &imap.FetchOptions{
			UID:          true,
			Envelope:     true,
			Flags:        true,
			InternalDate: true,
			RFC822Size:   true,
			BodySection:  []*imap.FetchItemBodySection{{Peek: true}},
		}).Collect()
		if err != nil {
			return fmt.Errorf("fetch: %w", err)
		}
		if len(msgs) == 0 {
			return fmt.Errorf("message %d not found in %s", uid, mailbox)
		}
		out = messageFrom(a, mailbox, msgs[0])
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// messageFrom parses a fully fetched message.
func messageFrom(a *MailAccount, mailbox string, m *imapclient.FetchMessageBuffer) *MailMessage {
	sum := MailSummary{
		AccountID: a.ID,
		Mailbox:   mailbox,
		UID:       uint32(m.UID),
		Seen:      hasFlag(m.Flags, imap.FlagSeen),
		Flagged:   hasFlag(m.Flags, imap.FlagFlagged),
	}
	if m.Envelope != nil {
		sum.Subject = m.Envelope.Subject
		sum.MessageID = m.Envelope.MessageID
		sum.From = firstAddress(m.Envelope.From)
		for _, addr := range m.Envelope.To {
			if e := addr.Addr(); e != "" {
				sum.To = append(sum.To, Address{Name: addr.Name, Email: e})
			}
		}
		if !m.Envelope.Date.IsZero() {
			sum.Date = m.Envelope.Date.UTC().Format(time.RFC3339)
		}
	}
	if sum.Date == "" && !m.InternalDate.IsZero() {
		sum.Date = m.InternalDate.UTC().Format(time.RFC3339)
	}
	msg := &MailMessage{MailSummary: sum, Attachments: []Attachment{}, References: []string{}, Cc: []Address{}}

	var raw []byte
	for _, sec := range m.BodySection {
		if sec.Section != nil && len(sec.Section.Part) == 0 && sec.Section.Specifier == imap.PartSpecifierNone {
			raw = sec.Bytes
		}
	}
	if raw == nil && len(m.BodySection) > 0 {
		raw = m.BodySection[0].Bytes
	}
	parseMessage(raw, msg)
	msg.HasAttach = len(msg.Attachments) > 0
	msg.Snippet = truncateRunes(strings.Join(strings.Fields(msg.Text), " "), snippetRunes)
	if msg.Snippet == "" {
		msg.Snippet = truncateRunes(strings.Join(strings.Fields(stripHTML(msg.HTML)), " "), snippetRunes)
	}
	return msg
}

// parseMessage fills text, html, attachments and threading headers.
func parseMessage(raw []byte, msg *MailMessage) {
	if len(raw) == 0 {
		return
	}
	// Headers first, so threading info survives a body parse failure.
	if h, err := readMailHeader(raw); err == nil {
		if ids, err := h.MsgIDList("In-Reply-To"); err == nil && len(ids) > 0 {
			msg.InReplyTo = ids[len(ids)-1]
		}
		if ids, err := h.MsgIDList("References"); err == nil && len(ids) > 0 {
			msg.References = ids
		}
		if len(msg.To) == 0 {
			if addrs, err := h.AddressList("To"); err == nil {
				for _, a := range addrs {
					msg.To = append(msg.To, Address{Name: a.Name, Email: a.Address})
				}
			}
		}
		if addrs, err := h.AddressList("Cc"); err == nil {
			for _, a := range addrs {
				msg.Cc = append(msg.Cc, Address{Name: a.Name, Email: a.Address})
			}
		}
	}

	r, err := gomail.CreateReader(bytes.NewReader(raw))
	if r == nil {
		_ = err
		return
	}
	defer r.Close()
	var htmlBody string
	attachmentIndex := 0
	for {
		part, err := nextPart(r)
		if err != nil {
			break
		}
		switch h := part.Header.(type) {
		case *gomail.InlineHeader:
			ct, _, _ := h.ContentType()
			body := readAllTolerant(part.Body, 8<<20)
			switch {
			case strings.HasPrefix(ct, "text/plain") && msg.Text == "":
				msg.Text = body
			case strings.HasPrefix(ct, "text/html") && htmlBody == "":
				htmlBody = body
			}
		case *gomail.AttachmentHeader:
			// Downloads count every attachment nextPart returns, including parts whose body cannot be decoded.
			attachmentIndex++
			attachment, err := attachmentMetadata(h, part.Body, attachmentIndex, io.Discard)
			if err == nil {
				msg.Attachments = append(msg.Attachments, attachment)
			}
		}
	}
	if htmlBody != "" {
		msg.HTML = htmlBody
	}
	if msg.Text == "" && msg.HTML != "" {
		msg.Text = stripHTML(msg.HTML)
	}
	if msg.Attachments == nil {
		msg.Attachments = []Attachment{}
	}
}

// readMailHeader parses just the header block of a raw message.
func readMailHeader(raw []byte) (gomail.Header, error) {
	e, err := gomessage.Read(bytes.NewReader(raw))
	if e == nil {
		return gomail.Header{}, err
	}
	return gomail.Header{Header: e.Header}, err
}

// SetFlags updates seen / flagged for one message.
func (s *Service) SetFlags(ctx context.Context, accountID string, uid uint32, req SetFlagsRequest) error {
	a, err := s.account(accountID)
	if err != nil {
		return err
	}
	mailbox := req.Mailbox
	if mailbox == "" {
		mailbox = "INBOX"
	}
	return s.withConn(ctx, a, func(c *imapclient.Client) error {
		if _, err := c.Select(mailbox, nil).Wait(); err != nil {
			return fmt.Errorf("select %s: %w", mailbox, err)
		}
		set := imap.UIDSetNum(imap.UID(uid))
		if req.Seen != nil {
			if err := storeFlag(c, set, imap.FlagSeen, *req.Seen); err != nil {
				return err
			}
		}
		if req.Flagged != nil {
			if err := storeFlag(c, set, imap.FlagFlagged, *req.Flagged); err != nil {
				return err
			}
		}
		return nil
	})
}

func storeFlag(c *imapclient.Client, set imap.UIDSet, flag imap.Flag, on bool) error {
	op := imap.StoreFlagsAdd
	if !on {
		op = imap.StoreFlagsDel
	}
	_, err := c.Store(set, &imap.StoreFlags{Op: op, Silent: true, Flags: []imap.Flag{flag}}, nil).Collect()
	return err
}
